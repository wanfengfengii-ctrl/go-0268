package withering

import "testing"

func TestTendernessConserved(t *testing.T) {
	c := TendernessCounts{
		SingleBud:     10,
		OneBudOneLeaf: 15,
		OldLeaf:       3,
		RedLeaf:       2,
		TotalSamples:  30,
	}
	if !c.Conserved() {
		t.Fatalf("expected conserved counts, got %+v", c)
	}
}

func TestTendernessSumMismatch(t *testing.T) {
	c := TendernessCounts{
		SingleBud:     10,
		OneBudOneLeaf: 15,
		OldLeaf:       3,
		RedLeaf:       2,
		TotalSamples:  31,
	}
	if c.Conserved() {
		t.Fatalf("expected non-conserved counts, got %+v", c)
	}
}

func TestTendernessNegativeRejected(t *testing.T) {
	c := TendernessCounts{
		SingleBud:     -1,
		OneBudOneLeaf: 1,
		TotalSamples:  0,
	}
	if c.Conserved() {
		t.Fatalf("expected negative count rejected, got %+v", c)
	}
}

func TestTendernessZeroTotalConserved(t *testing.T) {
	var c TendernessCounts
	if !c.Conserved() {
		t.Fatalf("expected all-zero counts conserved, got %+v", c)
	}
}
