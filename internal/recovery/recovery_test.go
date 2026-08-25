package recovery

import (
	"context"
	"path/filepath"
	"testing"

	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/ledger"
	"verdant-leaf-fixation-gate/internal/store"
	"verdant-leaf-fixation-gate/internal/task"
	"verdant-leaf-fixation-gate/internal/withering"
)

func TestRestartRecoveryResumesPendingAttempts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "recovery.db")

	// First run: seed, lock a task, and record a retryable device attempt.
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	cat := catalog.NewService(st)
	if err := catalog.SeedDemo(ctx, cat); err != nil {
		t.Fatalf("seed: %v", err)
	}
	led := ledger.NewService(st)
	tasks := task.NewService(st, cat, led)
	wither := withering.NewService(st)

	if _, err := tasks.Create(ctx, "t-1", "batch-1", "plot-east", "round-spring", "op"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := tasks.Lock(ctx, "t-1", "op-lock", testLockSnapshot()); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if err := wither.RecordAttempt(ctx, "t-1", withering.DeviceAttempt{
		CallKey:    "probe:1",
		AttemptSeq: 1,
		DeviceKind: "probe",
		Retryable:  true,
		Succeeded:  false,
	}); err != nil {
		t.Fatalf("record attempt: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Second run: reopen and recover; leases and pending work must survive.
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	rec := NewService(st2)

	pending, err := rec.Recover(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending attempt, got %d", len(pending))
	}
	if pending[0].CallKey != "probe:1" || pending[0].TaskID != "t-1" {
		t.Fatalf("unexpected pending attempt: %+v", pending[0])
	}

	led2 := ledger.NewService(st2)
	leases, _ := led2.ListLeases(ctx, "t-1")
	if len(leases) == 0 {
		t.Fatal("expected leases to survive restart")
	}
	if err := led2.AcquireBatch(ctx, "t-2", 1, []ledger.ResourceLease{
		{ResourceType: ledger.ResourceWitheringSlot, ResourceID: "slot-1"},
	}); err == nil {
		t.Fatal("expected lease conflict after restart")
	}
}

func TestOpenTaskIDsExcludesTerminal(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	cat := catalog.NewService(st)
	_ = catalog.SeedDemo(ctx, cat)
	tasks := task.NewService(st, cat, ledger.NewService(st))

	if _, err := tasks.Create(ctx, "t-1", "batch-1", "plot-east", "round-spring", "op"); err != nil {
		t.Fatalf("create: %v", err)
	}
	rec := NewService(st)
	ids, err := rec.OpenTaskIDs(ctx)
	if err != nil {
		t.Fatalf("open ids: %v", err)
	}
	if len(ids) != 1 || ids[0] != "t-1" {
		t.Fatalf("expected open task t-1, got %v", ids)
	}
}

func testLockSnapshot() task.LockSnapshot {
	return task.LockSnapshot{
		GardenPlot:     "plot-east",
		PickingRound:   "round-spring",
		RuleDigest:     catalog.RuleDigest("rule-demo-v1"),
		BasketSeals:    []domain.BasketSeal{"basket-1", "basket-2"},
		BlindCodes:     []domain.BlindCode{"blind-1", "blind-2", "blind-3"},
		TendernessPts:  []string{"tp-1"},
		AssayWells:     []string{"well-1"},
		FixationSlots:  []string{"fix-1"},
		AirBranches:    []string{"air-a"},
		WitheringSlots: []string{"slot-1"},
		ReceiptPersons: []domain.PersonnelID{"recv-a", "recv-b", "recv-c"},
		ReviewPersons:  []domain.PersonnelID{"rev-x", "rev-y", "rev-z"},
	}
}
