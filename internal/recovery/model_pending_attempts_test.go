package recovery_test

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/ledger"
	"verdant-leaf-fixation-gate/internal/recovery"
	"verdant-leaf-fixation-gate/internal/store"
	"verdant-leaf-fixation-gate/internal/task"
	"verdant-leaf-fixation-gate/internal/withering"
)

func TestModel_RecoveryExcludesAttemptsForSucceededCall(t *testing.T) {
	type attempt struct {
		taskID      domain.TaskID
		callKey     string
		attemptSeq  int64
		logicalTime domain.LogicalTime
		kind        device.Kind
	}

	tests := []struct {
		name     string
		attempts []attempt
		want     []recovery.PendingAttempt
	}{
		{
			name: "three retryable probe failures followed by success leave no recovery work",
			attempts: []attempt{
				{taskID: "task-complete", callKey: "quick-screen", attemptSeq: 1, logicalTime: 11, kind: device.KindProbe},
				{taskID: "task-complete", callKey: "quick-screen", attemptSeq: 2, logicalTime: 12, kind: device.KindProbe},
				{taskID: "task-complete", callKey: "quick-screen", attemptSeq: 3, logicalTime: 13, kind: device.KindProbe},
				{taskID: "task-complete", callKey: "quick-screen", attemptSeq: 4, logicalTime: 14, kind: device.KindProbe},
			},
		},
		{
			name: "success is scoped to the same task and call key",
			attempts: []attempt{
				{taskID: "task-a", callKey: "quick-screen", attemptSeq: 1, logicalTime: 10, kind: device.KindProbe},
				{taskID: "task-a", callKey: "quick-screen", attemptSeq: 4, logicalTime: 40, kind: device.KindProbe},
				{taskID: "task-a", callKey: "retest", attemptSeq: 2, logicalTime: 30, kind: device.KindProbe},
				{taskID: "task-b", callKey: "quick-screen", attemptSeq: 3, logicalTime: 20, kind: device.KindProbe},
			},
			want: []recovery.PendingAttempt{
				{TaskID: "task-b", CallKey: "quick-screen", AttemptSeq: 3, LogicalTime: 20, DeviceKind: "probe"},
				{TaskID: "task-a", CallKey: "retest", AttemptSeq: 2, LogicalTime: 30, DeviceKind: "probe"},
			},
		},
		{
			name: "calls with only failures recover in logical-time order",
			attempts: []attempt{
				{taskID: "task-pending", callKey: "late", attemptSeq: 2, logicalTime: 50, kind: device.KindProbe},
				{taskID: "task-pending", callKey: "early", attemptSeq: 3, logicalTime: 20, kind: device.KindProbe},
				{taskID: "task-pending", callKey: "early", attemptSeq: 1, logicalTime: 20, kind: device.KindProbe},
			},
			want: []recovery.PendingAttempt{
				{TaskID: "task-pending", CallKey: "early", AttemptSeq: 1, LogicalTime: 20, DeviceKind: "probe"},
				{TaskID: "task-pending", CallKey: "early", AttemptSeq: 3, LogicalTime: 20, DeviceKind: "probe"},
				{TaskID: "task-pending", CallKey: "late", AttemptSeq: 2, LogicalTime: 50, DeviceKind: "probe"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "recovery.db")
			st, err := store.Open(path)
			if err != nil {
				t.Fatalf("open store: %v", err)
			}

			tasks := task.NewService(st, catalog.NewService(st), ledger.NewService(st))
			created := make(map[domain.TaskID]bool)
			for _, a := range tt.attempts {
				if created[a.taskID] {
					continue
				}
				batch := domain.BatchNumber(fmt.Sprintf("batch-%s", a.taskID))
				if _, err := tasks.Create(ctx, a.taskID, batch, "plot-east", "round-spring", "create"); err != nil {
					st.Close()
					t.Fatalf("create task %s: %v", a.taskID, err)
				}
				created[a.taskID] = true
			}

			adapter := device.NewAdapter(device.DefaultScripts())
			attemptLedger := withering.NewService(st)
			for _, a := range tt.attempts {
				outcome := adapter.Call(a.kind, int(a.attemptSeq)).Outcome
				if err := attemptLedger.RecordAttempt(ctx, a.taskID, withering.DeviceAttempt{
					CallKey:     a.callKey,
					AttemptSeq:  a.attemptSeq,
					LogicalTime: a.logicalTime,
					DeviceKind:  string(a.kind),
					Retryable:   outcome.Retryable(),
					Succeeded:   outcome == device.OutcomeSuccess,
				}); err != nil {
					st.Close()
					t.Fatalf("record attempt %+v: %v", a, err)
				}
			}
			if err := st.Close(); err != nil {
				t.Fatalf("close before restart: %v", err)
			}

			restarted, err := store.Open(path)
			if err != nil {
				t.Fatalf("reopen store: %v", err)
			}
			defer restarted.Close()

			got, err := recovery.NewService(restarted).Recover(ctx)
			if err != nil {
				t.Fatalf("recover: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("pending attempts mismatch\n got: %+v\nwant: %+v", got, tt.want)
			}

			var auditCount int
			if err := restarted.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_attempts").Scan(&auditCount); err != nil {
				t.Fatalf("count audit attempts: %v", err)
			}
			if auditCount != len(tt.attempts) {
				t.Fatalf("recovery changed audit records: got %d, want %d", auditCount, len(tt.attempts))
			}
		})
	}
}
