package domain

// IntervalKind marks whether a boundary value is included (closed) or
// excluded (open). Callers must not reinterpret boundaries themselves.
type IntervalKind string

const (
	BoundClosed IntervalKind = "closed" // inclusive
	BoundOpen   IntervalKind = "open"   // exclusive
)

// Bound is a single endpoint of an interval.
type Bound struct {
	Value int64        `json:"value"`
	Kind  IntervalKind `json:"kind"`
}

// Interval is a threshold interval with explicit open/closed endpoints. A nil
// endpoint means the interval is unbounded on that side.
type Interval struct {
	Lower *Bound `json:"lower,omitempty"`
	Upper *Bound `json:"upper,omitempty"`
}

// Contains reports whether v satisfies the interval under the explicit
// closed/open flags. It never applies a caller-defined interpretation.
func (in Interval) Contains(v int64) bool {
	if in.Lower != nil {
		if in.Lower.Kind == BoundClosed {
			if v < in.Lower.Value {
				return false
			}
		} else if v <= in.Lower.Value {
			return false
		}
	}
	if in.Upper != nil {
		if in.Upper.Kind == BoundClosed {
			if v > in.Upper.Value {
				return false
			}
		} else if v >= in.Upper.Value {
			return false
		}
	}
	return true
}
