package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
)

func TestModel_WitheringFailedSubmissionLeavesNoCoverageEvidence(t *testing.T) {
	type testCase struct {
		name       string
		setupState domain.TaskState
		generation int64
		mutate     func([]witheringReadingDTO) []witheringReadingDTO
		wantStatus int
		wantState  domain.TaskState
		wantCells  int
		replay     bool
	}

	fullMatrix := func() []witheringReadingDTO {
		var readings []witheringReadingDTO
		for _, tp := range []int64{1, 2, 3} {
			for _, basket := range []string{"basket-1", "basket-2"} {
				readings = append(readings, witheringReadingDTO{
					TimePoint:       tp,
					Basket:          basket,
					EnvHumidity:     500,
					MoistureContent: 7000,
					LeafTemperature: 250,
					WaterLoss:       3000,
					RedLeafRatio:    100,
				})
			}
		}
		return readings
	}

	cases := []testCase{
		{
			name:       "locked but receipts and withering start are incomplete",
			setupState: domain.StatePendingReceipt,
			generation: 1,
			wantStatus: http.StatusUnprocessableEntity,
			wantState:  domain.StatePendingReceipt,
			wantCells:  0,
		},
		{
			name:       "stale generation",
			setupState: domain.StateWithering,
			generation: -1,
			wantStatus: http.StatusConflict,
			wantState:  domain.StateWithering,
			wantCells:  0,
		},
		{
			name:       "terminal task",
			setupState: domain.StateCancelled,
			generation: 1,
			wantStatus: http.StatusUnprocessableEntity,
			wantState:  domain.StateCancelled,
			wantCells:  0,
		},
		{
			name:       "missing cell",
			setupState: domain.StateWithering,
			generation: 1,
			mutate: func(readings []witheringReadingDTO) []witheringReadingDTO {
				return readings[:len(readings)-1]
			},
			wantStatus: http.StatusUnprocessableEntity,
			wantState:  domain.StateWithering,
			wantCells:  0,
		},
		{
			name:       "duplicate cell",
			setupState: domain.StateWithering,
			generation: 1,
			mutate: func(readings []witheringReadingDTO) []witheringReadingDTO {
				readings[len(readings)-1] = readings[0]
				return readings
			},
			wantStatus: http.StatusConflict,
			wantState:  domain.StateWithering,
			wantCells:  0,
		},
		{
			name:       "valid full matrix is immutable after acceptance",
			setupState: domain.StateWithering,
			generation: 1,
			wantStatus: http.StatusOK,
			wantState:  domain.StateTenderness,
			wantCells:  6,
			replay:     true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, device.ImmediateScripts())
			ctx := context.Background()
			createAndLock(t, app, "model-withering", "model-batch")

			switch tc.setupState {
			case domain.StateWithering:
				if _, err := app.Tasks.SubmitReceipts(ctx, "model-withering", 1, "op-receipts", []domain.PersonnelID{"recv-a", "recv-b"}); err != nil {
					t.Fatalf("submit receipts: %v", err)
				}
				if err := app.Tasks.TransitionTask(ctx, "model-withering", domain.StateSlotOccupied, domain.StateWithering, "op-start"); err != nil {
					t.Fatalf("start withering: %v", err)
				}
			case domain.StateCancelled:
				if err := app.Tasks.TransitionTask(ctx, "model-withering", domain.StatePendingReceipt, domain.StateCancelled, "op-cancel"); err != nil {
					t.Fatalf("cancel task: %v", err)
				}
			}

			readings := fullMatrix()
			if tc.mutate != nil {
				readings = tc.mutate(readings)
			}
			body, err := json.Marshal(readingsRequest{
				OperationID: "op-readings",
				Generation:  tc.generation,
				Readings:    readings,
			})
			if err != nil {
				t.Fatalf("marshal readings request: %v", err)
			}
			srv := NewServerWithApp(app)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/model-withering/withering/readings", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp := httptest.NewRecorder()
			srv.ServeHTTP(resp, req)
			if resp.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", resp.Code, tc.wantStatus, resp.Body.String())
			}

			cells, err := app.Withering.SubmittedCells(ctx, "model-withering")
			if err != nil {
				t.Fatalf("count submitted cells: %v", err)
			}
			if cells != tc.wantCells {
				t.Fatalf("submitted cells = %d, want %d", cells, tc.wantCells)
			}
			taskState, err := app.Tasks.Load(ctx, "model-withering")
			if err != nil {
				t.Fatalf("load task: %v", err)
			}
			if taskState.State != tc.wantState {
				t.Fatalf("state = %s, want %s", taskState.State, tc.wantState)
			}

			if tc.replay {
				replayReq := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/model-withering/withering/readings", bytes.NewReader(body))
				replayReq.Header.Set("Content-Type", "application/json")
				replayResp := httptest.NewRecorder()
				srv.ServeHTTP(replayResp, replayReq)
				if replayResp.Code < 400 {
					t.Fatalf("replay status = %d, want rejection", replayResp.Code)
				}
				cells, err = app.Withering.SubmittedCells(ctx, "model-withering")
				if err != nil {
					t.Fatalf("count cells after replay: %v", err)
				}
				if cells != tc.wantCells {
					t.Fatalf("submitted cells after replay = %d, want %d", cells, tc.wantCells)
				}
			}
		})
	}
}
