package task

import (
	"testing"

	"verdant-leaf-fixation-gate/internal/domain"
)

func newTask() *LeafIntakeTask {
	return &LeafIntakeTask{
		ID:         "t-1",
		Generation: 1,
		State:      domain.StatePendingLock,
	}
}

func TestTransitionAppliesLegalStep(t *testing.T) {
	tk := newTask()
	if err := tk.Transition(domain.StatePendingReceipt, "op-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tk.State != domain.StatePendingReceipt {
		t.Fatalf("expected pending_receipt, got %s", tk.State)
	}
}

func TestTransitionRejectsIllegalStep(t *testing.T) {
	tk := newTask()
	err := tk.Transition(domain.StateFixed, "op-1")
	de, ok := err.(*domain.DomainError)
	if !ok {
		t.Fatalf("expected DomainError, got %T", err)
	}
	if de.Code != domain.CodeTerminalStateRejected {
		t.Fatalf("expected TERMINAL_STATE_REJECTED, got %s", de.Code)
	}
}

func TestTransitionRejectsFromTerminal(t *testing.T) {
	tk := newTask()
	tk.State = domain.StateRiskIsolated
	err := tk.Transition(domain.StateCancelled, "op-1")
	de, ok := err.(*domain.DomainError)
	if !ok {
		t.Fatalf("expected DomainError, got %T", err)
	}
	if de.Code != domain.CodeTerminalStateRejected {
		t.Fatalf("expected TERMINAL_STATE_REJECTED, got %s", de.Code)
	}
}

func TestStaleGeneration(t *testing.T) {
	tk := newTask()
	tk.Generation = 5
	if !tk.StaleGeneration(4) {
		t.Fatal("expected generation 4 to be stale against 5")
	}
	if tk.StaleGeneration(5) {
		t.Fatal("expected generation 5 not to be stale")
	}
}
