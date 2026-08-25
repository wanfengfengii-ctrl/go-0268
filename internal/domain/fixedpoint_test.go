package domain

import (
	"math"
	"testing"
)

func TestCheckedAddOverflow(t *testing.T) {
	if _, err := CheckedAdd(math.MaxInt64, 1); err != ErrOverflow {
		t.Fatalf("expected overflow, got %v", err)
	}
	if _, err := CheckedAdd(math.MinInt64, -1); err != ErrOverflow {
		t.Fatalf("expected overflow, got %v", err)
	}
	if got, err := CheckedAdd(2, 3); err != nil || got != 5 {
		t.Fatalf("expected 5, got %d err=%v", got, err)
	}
}

func TestCheckedMulOverflow(t *testing.T) {
	if _, err := CheckedMul(math.MaxInt64, 2); err != ErrOverflow {
		t.Fatalf("expected overflow, got %v", err)
	}
	if got, err := CheckedMul(0, math.MaxInt64); err != nil || got != 0 {
		t.Fatalf("expected 0, got %d err=%v", got, err)
	}
	if got, err := CheckedMul(6, 7); err != nil || got != 42 {
		t.Fatalf("expected 42, got %d err=%v", got, err)
	}
}

func TestCheckedDivZero(t *testing.T) {
	if _, err := CheckedDiv(1, 0); err != ErrDivisionByZero {
		t.Fatalf("expected division by zero, got %v", err)
	}
	if _, err := CheckedDiv(math.MinInt64, -1); err != ErrOverflow {
		t.Fatalf("expected overflow, got %v", err)
	}
	if got, err := CheckedDiv(10, 2); err != nil || got != 5 {
		t.Fatalf("expected 5, got %d err=%v", got, err)
	}
}

func TestFixedScaleValidation(t *testing.T) {
	if _, err := NewFixed(1, 3); err != ErrScaleMismatch {
		t.Fatalf("expected scale mismatch, got %v", err)
	}
	if f, err := NewFixed(100, ScalePerTenThousand); err != nil || f.Scale != ScalePerTenThousand {
		t.Fatalf("expected valid fixed, got %v err=%v", f, err)
	}
}

func TestFixedCompareCrossScale(t *testing.T) {
	a := Fixed{Raw: 5000, Scale: ScalePerTenThousand} // 0.5
	b := Fixed{Raw: 5, Scale: ScaleDecimal1}          // 0.5
	got, err := a.Compare(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 0 {
		t.Fatalf("expected equal, got %d", got)
	}
}

func TestFixedAdd(t *testing.T) {
	a := Fixed{Raw: 1000, Scale: ScalePerTenThousand}
	b := Fixed{Raw: 2000, Scale: ScalePerTenThousand}
	sum, err := a.Add(b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum.Raw != 3000 {
		t.Fatalf("expected 3000, got %d", sum.Raw)
	}
}
