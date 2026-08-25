package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"verdant-leaf-fixation-gate/internal/arbiter"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/withering"
)

func TestModel_reviewSignaturesAreBoundToPendingReview(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	srv := NewServerWithApp(app)
	ctx := context.Background()
	const taskID = domain.TaskID("review-phase-gate")
	createAndLock(t, app, taskID, "batch-review-phase-gate")
	postReview := func(t *testing.T, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+string(taskID)+"/reviews", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		srv.ServeHTTP(httptest.NewRecorder(), req)
	}

	premature := []struct {
		name     string
		personID string
	}{
		{name: "first reviewer", personID: "rev-x"},
		{name: "second reviewer", personID: "rev-y"},
	}
	for i, tc := range premature {
		t.Run("premature/"+tc.name, func(t *testing.T) {
			postReview(t, fmt.Sprintf(
				`{"operation_id":"op-early-%d","generation":1,"personnel_id":%q,"approved":true}`,
				i+1, tc.personID))
			count, err := app.Arbiter.ReviewCount(ctx, taskID)
			if err != nil {
				t.Fatalf("count reviews: %v", err)
			}
			if count != 0 {
				t.Fatalf("premature review was persisted: count = %d, want 0", count)
			}
		})
	}

	if _, err := app.Tasks.SubmitReceipts(ctx, taskID, 1, "op-receipt", []domain.PersonnelID{"recv-a", "recv-b"}); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, taskID, domain.StateSlotOccupied, domain.StateWithering, "op-start"); err != nil {
		t.Fatalf("start withering: %v", err)
	}
	if err := app.Withering.SubmitReadings(ctx, taskID, 1, coverageReadings()); err != nil {
		t.Fatalf("readings: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, taskID, domain.StateWithering, domain.StateTenderness, "op-readings"); err != nil {
		t.Fatalf("to tenderness: %v", err)
	}
	counts := withering.TendernessCounts{SingleBud: 10, OneBudOneLeaf: 5, OldLeaf: 2, RedLeaf: 1, TotalSamples: 18}
	if _, err := app.Withering.SubmitTenderness(ctx, taskID, 1, counts, "op-tenderness"); err != nil {
		t.Fatalf("tenderness: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, taskID, domain.StateTenderness, domain.StateAssayScreening, "op-to-assay"); err != nil {
		t.Fatalf("to assay: %v", err)
	}
	if err := app.Arbiter.Seal(ctx, taskID, 1, "op-seal"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := app.Arbiter.Reveal(ctx, taskID, 1, "op-reveal"); err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if err := app.Arbiter.SubmitAssay(ctx, taskID, 1, arbiter.AssayEvidence{
		Well: "well-1", BlindCode: "blind-1", Generation: 1,
		Inhibition: domain.Fixed{Raw: 100, Scale: domain.ScalePerTenThousand},
	}); err != nil {
		t.Fatalf("assay: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, taskID, domain.StateAssayScreening, domain.StateRetesting, "op-to-retest"); err != nil {
		t.Fatalf("to retest: %v", err)
	}
	if err := app.Arbiter.SubmitRetest(ctx, taskID, 1, arbiter.RetestEvidence{
		Generation: 1, LeafTemperature: domain.Fixed{Raw: 250, Scale: domain.ScaleDecimal1},
		MoistureContent: domain.Fixed{Raw: 7000, Scale: domain.ScalePerTenThousand},
	}); err != nil {
		t.Fatalf("retest: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, taskID, domain.StateRetesting, domain.StatePendingReview, "op-to-review"); err != nil {
		t.Fatalf("to review: %v", err)
	}

	t.Run("release cannot reuse premature signatures", func(t *testing.T) {
		decision, err := app.Arbiter.Terminal(ctx, taskID, 1, arbiter.CommandRelease, "op-early-release")
		if err == nil {
			t.Fatalf("release succeeded with premature reviews: %+v", decision)
		}
		if decision.FixationCredential != nil {
			t.Fatalf("premature reviews produced credential %+v", decision.FixationCredential)
		}
	})

	valid := []struct {
		name      string
		personID  string
		wantState domain.TaskState
	}{
		{name: "first valid reviewer keeps review pending", personID: "rev-x", wantState: domain.StatePendingReview},
		{name: "second valid reviewer enables fixation", personID: "rev-y", wantState: domain.StateFixationReady},
	}
	for i, tc := range valid {
		t.Run("valid/"+tc.name, func(t *testing.T) {
			postReview(t, fmt.Sprintf(
				`{"operation_id":"op-valid-%d","generation":1,"personnel_id":%q,"approved":true}`,
				i+1, tc.personID))
			got, err := app.Tasks.Load(ctx, taskID)
			if err != nil {
				t.Fatalf("load task: %v", err)
			}
			if got.State != tc.wantState {
				t.Fatalf("state = %s, want %s", got.State, tc.wantState)
			}
		})
	}

	t.Run("release accepts current valid signatures", func(t *testing.T) {
		decision, err := app.Arbiter.Terminal(ctx, taskID, 1, arbiter.CommandRelease, "op-release")
		if err != nil {
			t.Fatalf("release: %v", err)
		}
		if decision.FixationCredential == nil {
			t.Fatal("release did not produce a fixation credential")
		}
	})
}
