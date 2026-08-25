package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/task"
)

func TestModel_ReceiptOperationIsScopedToTask(t *testing.T) {
	tests := []struct {
		name        string
		taskIDs     []domain.TaskID
		submissions []domain.TaskID
		wantTaskID  domain.TaskID
	}{
		{
			name:        "same task replay returns its original result",
			taskIDs:     []domain.TaskID{"receipt-a"},
			submissions: []domain.TaskID{"receipt-a", "receipt-a"},
			wantTaskID:  "receipt-a",
		},
		{
			name:        "same operation and personnel on another task advances that task",
			taskIDs:     []domain.TaskID{"receipt-a", "receipt-b"},
			submissions: []domain.TaskID{"receipt-a", "receipt-b"},
			wantTaskID:  "receipt-b",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, device.ImmediateScripts())
			ctx := context.Background()
			for i, id := range tc.taskIDs {
				if _, err := app.Tasks.Create(ctx, id, domain.BatchNumber("batch-"+string(id)), "plot-east", "round-spring", "create-op"); err != nil {
					t.Fatalf("create %s: %v", id, err)
				}
				snapshot := task.LockSnapshot{
					GardenPlot:     "plot-east",
					PickingRound:   "round-spring",
					RuleDigest:     catalog.RuleDigest("rule-demo-v1"),
					BasketSeals:    []domain.BasketSeal{domain.BasketSeal("basket-" + string(id))},
					BlindCodes:     []domain.BlindCode{domain.BlindCode("blind-" + string(id))},
					TendernessPts:  []string{"tenderness-" + string(id)},
					AssayWells:     []string{[]string{"well-1", "well-2"}[i]},
					AirBranches:    []string{[]string{"air-a", "air-b"}[i]},
					WitheringSlots: []string{[]string{"slot-1", "slot-2"}[i]},
					ReceiptPersons: []domain.PersonnelID{"recv-a", "recv-b"},
					ReviewPersons:  []domain.PersonnelID{"rev-x", "rev-y"},
				}
				if _, err := app.Tasks.Lock(ctx, id, "lock-op-"+domain.OperationID(id), snapshot); err != nil {
					t.Fatalf("lock %s: %v", id, err)
				}
			}

			srv := NewServerWithApp(app)
			var got task.ReceiptResult
			for _, id := range tc.submissions {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/"+string(id)+"/receipts", strings.NewReader(`{"operation_id":"shared-receipt-op","generation":1,"personnel":["recv-a","recv-b"]}`))
				req.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				srv.ServeHTTP(response, req)
				if response.Code != http.StatusOK {
					t.Fatalf("receipt for %s returned status %d; body=%s", id, response.Code, response.Body.String())
				}
				if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
					t.Fatalf("decode receipt response for %s: %v; body=%s", id, err, response.Body.String())
				}
			}

			if got.TaskID != tc.wantTaskID {
				t.Fatalf("last receipt returned task_id %q, want %q", got.TaskID, tc.wantTaskID)
			}
			var view taskView
			getResponse := httptest.NewRecorder()
			srv.ServeHTTP(getResponse, httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+string(tc.wantTaskID), nil))
			if getResponse.Code != http.StatusOK {
				t.Fatalf("GET task returned status %d; body=%s", getResponse.Code, getResponse.Body.String())
			}
			if err := json.Unmarshal(getResponse.Body.Bytes(), &view); err != nil {
				t.Fatalf("decode GET response: %v; body=%s", err, getResponse.Body.String())
			}
			if view.State != domain.StateSlotOccupied {
				t.Fatalf("task %s remained in state %q, want %q", tc.wantTaskID, view.State, domain.StateSlotOccupied)
			}
		})
	}
}
