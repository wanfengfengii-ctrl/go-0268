package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
)

func TestModel_RejectedTerminalDoesNotMintFixationCredential(t *testing.T) {
	tests := []struct {
		name             string
		command          string
		prepare          func(*testing.T, *App, domain.TaskID)
		wantTerminalCode string
		wantConfirmed    bool
	}{
		{
			name:    "release rejected for missing coverage",
			command: "release",
			prepare: func(t *testing.T, app *App, id domain.TaskID) {
				createAndLock(t, app, id, "batch-missing")
			},
			wantTerminalCode: "COVERAGE_MISSING",
		},
		{
			name:    "isolate",
			command: "isolate",
			prepare: func(t *testing.T, app *App, id domain.TaskID) {
				createAndLock(t, app, id, "batch-isolate")
			},
			wantTerminalCode: `"command":"isolate"`,
		},
		{
			name:    "cancel",
			command: "cancel",
			prepare: func(t *testing.T, app *App, id domain.TaskID) {
				createAndLock(t, app, id, "batch-cancel")
			},
			wantTerminalCode: `"command":"cancel"`,
		},
		{
			name:    "valid release",
			command: "release",
			prepare: func(t *testing.T, app *App, id domain.TaskID) {
				driveToFixationReady(t, app, id, "batch-valid")
			},
			wantTerminalCode: `"fixation_credential":"cred-valid-release"`,
			wantConfirmed:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, device.ImmediateScripts())
			srv := NewServerWithApp(app)
			id := domain.TaskID(strings.ReplaceAll(tc.name, " ", "-"))
			if tc.wantConfirmed {
				id = "valid-release"
			}
			tc.prepare(t, app, id)

			post := func(path, body string) string {
				t.Helper()
				req := httptest.NewRequest("POST", path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				res := httptest.NewRecorder()
				srv.ServeHTTP(res, req)
				return res.Body.String()
			}

			terminal := post("/api/v1/tasks/"+string(id)+"/terminal",
				`{"generation":1,"command":"`+tc.command+`","operation_id":"op-terminal"}`)
			if !strings.Contains(terminal, tc.wantTerminalCode) {
				t.Fatalf("terminal response %q does not contain %q", terminal, tc.wantTerminalCode)
			}
			if !tc.wantConfirmed && strings.Contains(terminal, "fixation_credential") {
				t.Fatalf("non-release terminal response exposed a credential: %s", terminal)
			}

			confirm := post("/api/v1/tasks/"+string(id)+"/fixation/confirm",
				`{"credential_id":"cred-`+string(id)+`","operation_id":"op-confirm"}`)
			if got := strings.Contains(confirm, `"confirmed":true`); got != tc.wantConfirmed {
				t.Fatalf("confirm response %q: confirmed=%v, want %v", confirm, got, tc.wantConfirmed)
			}
			if !tc.wantConfirmed && !strings.Contains(confirm, "TERMINAL_STATE_REJECTED") {
				t.Fatalf("credential unexpectedly usable or rejection was unstable: %s", confirm)
			}
		})
	}
}
