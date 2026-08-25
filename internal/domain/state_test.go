package domain

import "testing"

func TestValidStatesCount(t *testing.T) {
	got := ValidStates()
	if len(got) != 12 {
		t.Fatalf("expected 12 states, got %d", len(got))
	}
	seen := map[TaskState]bool{}
	for _, s := range got {
		if seen[s] {
			t.Fatalf("duplicate state %q", s)
		}
		seen[s] = true
	}
}

func TestCanTransitionLegal(t *testing.T) {
	cases := []struct{ from, to TaskState }{
		{StatePendingLock, StatePendingReceipt},
		{StatePendingReceipt, StateSlotOccupied},
		{StateSlotOccupied, StateWithering},
		{StateWithering, StateTenderness},
		{StateTenderness, StateAssayScreening},
		{StateAssayScreening, StateRetesting},
		{StateRetesting, StatePendingReview},
		{StatePendingReview, StateFixationReady},
		{StateFixationReady, StateFixed},
	}
	for _, c := range cases {
		if !CanTransition(c.from, c.to) {
			t.Errorf("expected %s -> %s to be legal", c.from, c.to)
		}
	}
}

func TestCanTransitionIllegal(t *testing.T) {
	if CanTransition(StatePendingLock, StateFixed) {
		t.Error("expected pending_lock -> fixed to be illegal")
	}
	if CanTransition(StatePendingLock, StatePendingLock) {
		t.Error("expected self-transition to be illegal")
	}
	if CanTransition(StateFixed, StateCancelled) {
		t.Error("expected fixed -> cancelled to be illegal")
	}
}

func TestTerminalStates(t *testing.T) {
	for _, s := range []TaskState{StateFixed, StateRiskIsolated, StateCancelled} {
		if !IsTerminal(s) {
			t.Errorf("expected %s to be terminal", s)
		}
	}
	for _, s := range []TaskState{StatePendingLock, StateWithering, StateFixationReady} {
		if IsTerminal(s) {
			t.Errorf("expected %s to be non-terminal", s)
		}
	}
}

func TestCompletedState(t *testing.T) {
	if !IsCompleted(StateFixed) {
		t.Error("expected fixed to be completed")
	}
	if IsCompleted(StateRiskIsolated) {
		t.Error("expected risk_isolated not to be completed")
	}
}
