package httpapi

import (
	"context"
	"testing"

	"verdant-leaf-fixation-gate/internal/arbiter"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
)

func TestModel_TerminalLeaseSettlement(t *testing.T) {
	tests := []struct {
		name              string
		command           arbiter.TerminalCommand
		driveReady        bool
		wantTerminalError domain.ErrorCode
		wantReuse         bool
		wantCredential    bool
	}{
		{name: "cancel releases leases after winning terminal barrier", command: arbiter.CommandCancel, wantReuse: true},
		{name: "isolate releases leases after winning terminal barrier", command: arbiter.CommandIsolate, wantReuse: true},
		{name: "release frees leases and returns one credential", command: arbiter.CommandRelease, driveReady: true, wantReuse: true, wantCredential: true},
		{name: "rejected release keeps leases active", command: arbiter.CommandRelease, wantTerminalError: domain.CodeCoverageMissing},
		{name: "open task keeps leases active"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newTestApp(t, device.ImmediateScripts())
			ctx := context.Background()
			if tt.driveReady {
				driveToFixationReady(t, app, "t-lease-1", "batch-lease-1")
			} else {
				createAndLock(t, app, "t-lease-1", "batch-lease-1")
			}

			if tt.command != "" {
				decision, err := app.Arbiter.Terminal(ctx, "t-lease-1", 1, tt.command, "op-terminal-1")
				if tt.wantTerminalError != "" {
					if !isCode(err, tt.wantTerminalError) {
						t.Fatalf("terminal error = %v, want code %s", err, tt.wantTerminalError)
					}
				} else {
					if err != nil {
						t.Fatalf("terminal %s: %v", tt.command, err)
					}
					if (decision.FixationCredential != nil) != tt.wantCredential {
						t.Fatalf("credential presence = %v, want %v", decision.FixationCredential != nil, tt.wantCredential)
					}
					if tt.wantCredential && decision.FixationCredential.ID == "" {
						t.Fatal("release returned an empty fixation credential")
					}
					if _, err := app.Arbiter.Terminal(ctx, "t-lease-1", 1, tt.command, "op-terminal-2"); !isCode(err, domain.CodeTerminalStateRejected) {
						t.Fatalf("second terminal error = %v, want %s", err, domain.CodeTerminalStateRejected)
					}
				}
			}

			if _, err := app.Tasks.Create(ctx, "t-lease-2", "batch-lease-2", "plot-east", "round-spring", "op-create-2"); err != nil {
				t.Fatalf("create replacement task: %v", err)
			}
			replacement := lockSnapshot()
			replacement.BasketSeals = []domain.BasketSeal{"basket-21", "basket-22"}
			replacement.BlindCodes = []domain.BlindCode{"blind-21", "blind-22", "blind-23"}
			replacement.TendernessPts = []string{"tp-2"}
			_, err := app.Tasks.Lock(ctx, "t-lease-2", "op-lock-2", replacement)
			if tt.wantReuse {
				if err != nil {
					t.Fatalf("replacement task could not reuse terminal resources: %v", err)
				}
				leases, err := app.Ledger.ListLeases(ctx, "t-lease-2")
				if err != nil {
					t.Fatalf("list replacement leases: %v", err)
				}
				if len(leases) != 4 {
					t.Fatalf("replacement active leases = %d, want 4", len(leases))
				}
			} else if !isCode(err, domain.CodeResourceLeaseConflict) {
				t.Fatalf("replacement lock error = %v, want %s", err, domain.CodeResourceLeaseConflict)
			}
		})
	}
}
