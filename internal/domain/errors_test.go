package domain

import "testing"

func TestReasonSortingDeterministic(t *testing.T) {
	err := &DomainError{
		Code:           CodeCoverageMissing,
		OperationID:    "op-1",
		TaskGeneration: 3,
		Reasons: []Reason{
			{Code: CodeCoverageDuplicate, BasketSeal: "b", GardenPlot: "p2"},
			{Code: CodeCoverageMissing, BasketSeal: "a", GardenPlot: "p1"},
			{Code: CodeCountNotConserved, GardenPlot: "p1", BasketSeal: "a"},
		},
	}
	sorted := err.SortedReasons()
	if len(sorted) != 3 {
		t.Fatalf("expected 3 reasons, got %d", len(sorted))
	}
	// Deterministic order: garden plot p1 before p2; both p1 reasons share
	// basket "a" and are ordered by code as the final tie-breaker.
	if sorted[0].GardenPlot != "p1" || sorted[0].BasketSeal != "a" {
		t.Fatalf("unexpected first reason: %+v", sorted[0])
	}
	if sorted[1].GardenPlot != "p1" || sorted[1].BasketSeal != "a" {
		t.Fatalf("unexpected second reason: %+v", sorted[1])
	}
	if sorted[2].GardenPlot != "p2" || sorted[2].BasketSeal != "b" {
		t.Fatalf("unexpected third reason: %+v", sorted[2])
	}
}

func TestSortedReasonsReturnsCopy(t *testing.T) {
	err := &DomainError{Reasons: []Reason{{Code: CodeCoverageMissing}}}
	sorted := err.SortedReasons()
	sorted[0].Code = CodeRoleOverlap
	if err.Reasons[0].Code != CodeCoverageMissing {
		t.Fatal("expected SortedReasons to return a copy, not mutate the source")
	}
}

func TestNewError(t *testing.T) {
	e := NewError(CodeStaleTaskGeneration, "op-1", 5, "stale generation")
	if e.Code != CodeStaleTaskGeneration {
		t.Fatalf("unexpected code %q", e.Code)
	}
	if len(e.Reasons) != 1 {
		t.Fatalf("expected 1 reason, got %d", len(e.Reasons))
	}
	if e.Error() == "" {
		t.Fatal("expected non-empty error string")
	}
}
