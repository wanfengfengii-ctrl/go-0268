package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
)

func TestModel_DeviceRetryClosure(t *testing.T) {
	tests := []struct {
		name          string
		kind          device.Kind
		outcomes      []device.Outcome
		retry         bool
		retrySucceeds bool
		wantState     domain.TaskState
		wantEvidence  int
	}{
		{name: "assay retry success closes read", kind: device.KindAssayReader, outcomes: []device.Outcome{device.OutcomeReject, device.OutcomeSuccess}, retry: true, retrySucceeds: true, wantState: domain.StateRetesting, wantEvidence: 1},
		{name: "retest retry success closes read", kind: device.KindMoistureMeter, outcomes: []device.Outcome{device.OutcomeReject, device.OutcomeSuccess}, retry: true, retrySucceeds: true, wantState: domain.StatePendingReview, wantEvidence: 1},
		{name: "assay retry failure remains retryable", kind: device.KindAssayReader, outcomes: []device.Outcome{device.OutcomeReject, device.OutcomeTimeout}, retry: true, wantState: domain.StateAssayScreening},
		{name: "retest retry failure remains retryable", kind: device.KindMoistureMeter, outcomes: []device.Outcome{device.OutcomeReject, device.OutcomeTimeout}, retry: true, wantState: domain.StateRetesting},
		{name: "direct assay success still closes read", kind: device.KindAssayReader, outcomes: []device.Outcome{device.OutcomeSuccess}, wantState: domain.StateRetesting, wantEvidence: 1},
		{name: "direct retest success still closes read", kind: device.KindMoistureMeter, outcomes: []device.Outcome{device.OutcomeSuccess}, wantState: domain.StatePendingReview, wantEvidence: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, map[device.Kind]*device.Script{tc.kind: device.NewScript(tc.outcomes...)})
			createAndLock(t, app, "retry-task", "retry-batch")
			ctx := context.Background()
			transitions := [][2]domain.TaskState{
				{domain.StatePendingReceipt, domain.StateSlotOccupied},
				{domain.StateSlotOccupied, domain.StateWithering},
				{domain.StateWithering, domain.StateTenderness},
				{domain.StateTenderness, domain.StateAssayScreening},
			}
			if tc.kind == device.KindMoistureMeter {
				transitions = append(transitions, [2]domain.TaskState{domain.StateAssayScreening, domain.StateRetesting})
			}
			for i, transition := range transitions {
				if err := app.Tasks.TransitionTask(ctx, "retry-task", transition[0], transition[1], domain.OperationID(tc.name)); err != nil {
					t.Fatalf("arrange transition %d: %v", i, err)
				}
			}

			srv := NewServerWithApp(app)
			post := func(path, body string) (int, map[string]any) {
				t.Helper()
				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				var response map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatalf("decode %s response %q: %v", path, rec.Body.String(), err)
				}
				return rec.Code, response
			}

			readPath := "/api/v1/tasks/retry-task/assays/read"
			readBody := `{"operation_id":"read-op","generation":1,"well":"well-1","blind_code":"blind-1","inhibition":321}`
			callKey := "assay:well-1:blind-1"
			kind := "assay_reader"
			retryPayload := `,"well":"well-1","blind_code":"blind-1","inhibition":321`
			if tc.kind == device.KindMoistureMeter {
				readPath = "/api/v1/tasks/retry-task/retests/read"
				readBody = `{"operation_id":"read-op","generation":1,"leaf_temperature":247,"moisture_content":6789}`
				callKey = "retest:retry-task"
				kind = "moisture_meter"
				retryPayload = `,"leaf_temperature":247,"moisture_content":6789`
			}

			status, response := post(readPath, readBody)
			if tc.retry {
				if status != http.StatusServiceUnavailable || response["code"] != string(domain.CodeDeviceRetryable) {
					t.Fatalf("initial read = status %d response %#v, want DEVICE_RETRYABLE", status, response)
				}
				retryBody := `{"task_id":"retry-task","call_key":"` + callKey + `","kind":"` + kind + `","operation_id":"retry-op","generation":1` + retryPayload + `}`
				status, response = post("/api/v1/device-attempts/attempt-1/retry", retryBody)
				if tc.retrySucceeds {
					if status != http.StatusOK || response["outcome"] != "success" || response["state"] != string(tc.wantState) {
						t.Fatalf("successful retry = status %d response %#v", status, response)
					}
				} else if status != http.StatusServiceUnavailable || response["code"] != string(domain.CodeDeviceRetryable) {
					t.Fatalf("failed retry = status %d response %#v, want DEVICE_RETRYABLE", status, response)
				}
			} else if status != http.StatusOK || response["state"] != string(tc.wantState) {
				t.Fatalf("direct read = status %d response %#v", status, response)
			}

			getReq := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/retry-task", nil)
			getRec := httptest.NewRecorder()
			srv.ServeHTTP(getRec, getReq)
			var view map[string]any
			if err := json.Unmarshal(getRec.Body.Bytes(), &view); err != nil {
				t.Fatalf("decode task view: %v", err)
			}
			if getRec.Code != http.StatusOK || view["state"] != string(tc.wantState) {
				t.Fatalf("task view = status %d response %#v, want state %s", getRec.Code, view, tc.wantState)
			}

			table := "assay_evidence"
			if tc.kind == device.KindMoistureMeter {
				table = "retest_evidence"
			}
			var evidence int
			if err := app.Store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE task_id = ?", "retry-task").Scan(&evidence); err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
			if evidence != tc.wantEvidence {
				t.Fatalf("%s count = %d, want %d", table, evidence, tc.wantEvidence)
			}
		})
	}
}
