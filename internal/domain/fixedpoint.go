package domain

import (
	"errors"
	"math"
)

// Fixed-point arithmetic errors. These map to stable rejection codes at the
// transport boundary.
var (
	ErrOverflow       = errors.New("fixed-point overflow")
	ErrDivisionByZero = errors.New("division by zero")
	ErrScaleMismatch  = errors.New("fixed-point scale mismatch")
)

// Scale is the fixed-point denominator. Rates such as moisture content, water
// loss, and assay inhibition are stored per ten-thousand; leaf temperature
// and humidity use a smaller decimal scale; sample mass uses whole units.
type Scale int32

const (
	ScaleWhole          Scale = 1
	ScaleDecimal1       Scale = 10
	ScalePerTenThousand Scale = 10000
)

// Fixed is a checked fixed-point quantity: Raw / Scale. Raw is always an
// integer; Scale is a positive power-of-ten denominator.
type Fixed struct {
	Raw   int64 `json:"raw"`
	Scale Scale `json:"scale"`
}

// NewFixed validates the scale and returns a fixed value.
func NewFixed(raw int64, scale Scale) (Fixed, error) {
	if scale != ScaleWhole && scale != ScaleDecimal1 && scale != ScalePerTenThousand {
		return Fixed{}, ErrScaleMismatch
	}
	return Fixed{Raw: raw, Scale: scale}, nil
}

// Compare returns -1, 0, or +1 comparing a and b after normalizing scales.
// It returns an error on overflow during normalization.
func (a Fixed) Compare(b Fixed) (int, error) {
	ar, br, err := normalize(a, b)
	if err != nil {
		return 0, err
	}
	switch {
	case ar < br:
		return -1, nil
	case ar > br:
		return 1, nil
	default:
		return 0, nil
	}
}

// normalize expresses both values on a common scale without losing precision
// and without overflowing.
func normalize(a, b Fixed) (int64, int64, error) {
	switch {
	case a.Scale == b.Scale:
		return a.Raw, b.Raw, nil
	case a.Scale < b.Scale:
		mul, err := CheckedMul(int64(b.Scale/a.Scale), a.Raw)
		if err != nil {
			return 0, 0, err
		}
		return mul, b.Raw, nil
	default:
		mul, err := CheckedMul(int64(a.Scale/b.Scale), b.Raw)
		if err != nil {
			return 0, 0, err
		}
		return a.Raw, mul, nil
	}
}

// CheckedAdd returns a+b or ErrOverflow.
func CheckedAdd(a, b int64) (int64, error) {
	r := a + b
	if (r > a) != (b > 0) {
		return 0, ErrOverflow
	}
	return r, nil
}

// CheckedMul returns a*b or ErrOverflow. Zero inputs short-circuit before the
// multiplication to avoid the MinInt64 edge case.
func CheckedMul(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	r := a * b
	if r/b != a {
		return 0, ErrOverflow
	}
	return r, nil
}

// CheckedDiv returns a/b, rejecting division by zero and the MinInt64/-1
// overflow case.
func CheckedDiv(a, b int64) (int64, error) {
	if b == 0 {
		return 0, ErrDivisionByZero
	}
	if a == math.MinInt64 && b == -1 {
		return 0, ErrOverflow
	}
	return a / b, nil
}

// Add returns a+b after scale normalization.
func (a Fixed) Add(b Fixed) (Fixed, error) {
	ar, br, err := normalize(a, b)
	if err != nil {
		return Fixed{}, err
	}
	sum, err := CheckedAdd(ar, br)
	if err != nil {
		return Fixed{}, err
	}
	return Fixed{Raw: sum, Scale: a.Scale}, nil
}

// Mul returns a*b with the resulting scale set to a.Scale. Callers must pass
// a plain integer multiplier expressed in a compatible scale.
func (a Fixed) Mul(multiplier int64) (Fixed, error) {
	r, err := CheckedMul(a.Raw, multiplier)
	if err != nil {
		return Fixed{}, err
	}
	return Fixed{Raw: r, Scale: a.Scale}, nil
}
