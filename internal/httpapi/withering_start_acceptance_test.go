package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"verdant-leaf-fixation-gate/internal/arbiter"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
)

func TestModel_WitheringStartCommitsBeforeSuccess(t *testing.T) {
	type result struct {
		status int
		body   string
	}
	tests := []struct {
		name   string
		taskID domain.TaskID
		run    func(*testing.T, *App, *Server)
	}{
		{
			name:   "successful response observes committed transition and readings can follow",
			taskID: "start-single",
			run: func(t *testing.T, app *App, srv *Server) {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/start-single/withering/start", strings.NewReader(`{"operation_id":"start-single","generation":1}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("start status = %d, body = %s", rec.Code, rec.Body.String())
				}
				got, err := app.Tasks.Load(context.Background(), "start-single")
				if err != nil {
					t.Fatalf("load immediately after response: %v", err)
				}
				if got.State != domain.StateWithering {
					t.Fatalf("successful response preceded commit: state = %s", got.State)
				}

				body := `{"operation_id":"read-single","generation":1,"readings":[` +
					`{"time_point":1,"basket":"basket-1","env_humidity":500,"moisture_content":7000,"leaf_temperature":250,"water_loss":3000,"red_leaf_ratio":100},` +
					`{"time_point":1,"basket":"basket-2","env_humidity":500,"moisture_content":7000,"leaf_temperature":250,"water_loss":3000,"red_leaf_ratio":100},` +
					`{"time_point":2,"basket":"basket-1","env_humidity":500,"moisture_content":7000,"leaf_temperature":250,"water_loss":3000,"red_leaf_ratio":100},` +
					`{"time_point":2,"basket":"basket-2","env_humidity":500,"moisture_content":7000,"leaf_temperature":250,"water_loss":3000,"red_leaf_ratio":100},` +
					`{"time_point":3,"basket":"basket-1","env_humidity":500,"moisture_content":7000,"leaf_temperature":250,"water_loss":3000,"red_leaf_ratio":100},` +
					`{"time_point":3,"basket":"basket-2","env_humidity":500,"moisture_content":7000,"leaf_temperature":250,"water_loss":3000,"red_leaf_ratio":100}]}`
				req = httptest.NewRequest(http.MethodPost, "/api/v1/tasks/start-single/withering/readings", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec = httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("readings status = %d, body = %s", rec.Code, rec.Body.String())
				}
			},
		},
		{
			name:   "concurrent starts expose exactly one committed winner",
			taskID: "start-race",
			run: func(t *testing.T, app *App, srv *Server) {
				ready := make(chan struct{})
				results := make(chan result, 2)
				var wg sync.WaitGroup
				for _, operation := range []string{"start-a", "start-b"} {
					wg.Add(1)
					go func(operation string) {
						defer wg.Done()
						<-ready
						req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/start-race/withering/start", strings.NewReader(`{"operation_id":"`+operation+`","generation":1}`))
						req.Header.Set("Content-Type", "application/json")
						rec := httptest.NewRecorder()
						srv.ServeHTTP(rec, req)
						response, _ := io.ReadAll(rec.Result().Body)
						results <- result{status: rec.Code, body: string(response)}
					}(operation)
				}
				close(ready)
				wg.Wait()
				close(results)

				var statuses []int
				var loserBody string
				for got := range results {
					statuses = append(statuses, got.status)
					if got.status != http.StatusOK {
						loserBody = got.body
					}
				}
				sort.Ints(statuses)
				want := []int{http.StatusOK, http.StatusUnprocessableEntity}
				if len(statuses) != len(want) || statuses[0] != want[0] || statuses[1] != want[1] {
					t.Fatalf("concurrent statuses = %v, want %v", statuses, want)
				}
				if !strings.Contains(loserBody, `"code":"TERMINAL_STATE_REJECTED"`) {
					t.Fatalf("loser response is not stable state rejection: %s", loserBody)
				}
				got, err := app.Tasks.Load(context.Background(), "start-race")
				if err != nil {
					t.Fatalf("load after concurrent starts: %v", err)
				}
				if got.State != domain.StateWithering {
					t.Fatalf("final state = %s, want %s", got.State, domain.StateWithering)
				}
			},
		},
		{
			name:   "stale generation remains rejected",
			taskID: "start-stale",
			run: func(t *testing.T, app *App, srv *Server) {
				if err := app.Arbiter.CreateRejudgment(context.Background(), "start-stale", arbiter.RejudgmentCase{}); err != nil {
					t.Fatalf("advance generation: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/start-stale/withering/start", strings.NewReader(`{"operation_id":"stale-start","generation":1}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"STALE_TASK_GENERATION"`) {
					t.Fatalf("stale start status = %d, body = %s", rec.Code, rec.Body.String())
				}
				got, err := app.Tasks.Load(context.Background(), "start-stale")
				if err != nil {
					t.Fatalf("load after stale start: %v", err)
				}
				if got.State != domain.StateSlotOccupied {
					t.Fatalf("stale start changed state to %s", got.State)
				}
			},
		},
		{
			name:   "terminal task remains rejected",
			taskID: "start-terminal",
			run: func(t *testing.T, app *App, srv *Server) {
				if err := app.Tasks.TransitionTask(context.Background(), "start-terminal", domain.StateSlotOccupied, domain.StateCancelled, "cancel"); err != nil {
					t.Fatalf("cancel task: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks/start-terminal/withering/start", strings.NewReader(`{"operation_id":"late-start","generation":1}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"code":"TERMINAL_STATE_REJECTED"`) {
					t.Fatalf("terminal start status = %d, body = %s", rec.Code, rec.Body.String())
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, device.ImmediateScripts())
			createAndLock(t, app, tc.taskID, domain.BatchNumber("batch-"+string(tc.taskID)))
			if _, err := app.Tasks.SubmitReceipts(context.Background(), tc.taskID, 1, domain.OperationID("receipt-"+string(tc.taskID)), []domain.PersonnelID{"recv-a", "recv-b"}); err != nil {
				t.Fatalf("prepare %s in slot_occupied: %v", tc.taskID, err)
			}
			tc.run(t, app, NewServerWithApp(app))
		})
	}
}
