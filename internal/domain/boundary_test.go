package domain

import (
	"math"
	"testing"
)

func TestIntervalClosedBounds(t *testing.T) {
	in := Interval{
		Lower: &Bound{Value: 0, Kind: BoundClosed},
		Upper: &Bound{Value: 10000, Kind: BoundClosed},
	}
	for _, v := range []int64{0, 5000, 10000} {
		if !in.Contains(v) {
			t.Errorf("expected %d inside closed [0,10000]", v)
		}
	}
	if in.Contains(-1) || in.Contains(10001) {
		t.Error("expected out-of-range values rejected")
	}
}

func TestIntervalOpenBounds(t *testing.T) {
	in := Interval{
		Lower: &Bound{Value: 0, Kind: BoundOpen},
		Upper: &Bound{Value: 10000, Kind: BoundOpen},
	}
	if in.Contains(0) || in.Contains(10000) {
		t.Error("expected open endpoints excluded")
	}
	if !in.Contains(1) || !in.Contains(9999) {
		t.Error("expected interior values included")
	}
}

func TestIntervalUnbounded(t *testing.T) {
	lowerOnly := Interval{Lower: &Bound{Value: 0, Kind: BoundClosed}}
	if !lowerOnly.Contains(math.MaxInt64) {
		t.Error("expected unbounded upper to include large values")
	}
	if lowerOnly.Contains(-1) {
		t.Error("expected lower bound enforced")
	}
}
