// Package withering implements the withering loss and tenderness collection
// ledger: the per-basket/per-time-point coverage matrix, fixed-point physical
// quantities, tenderness conservation, and device attempts. Valid evidence is
// append-only and immutable.
package withering

import (
	"context"

	"verdant-leaf-fixation-gate/internal/domain"
)

// CoverageKey identifies a single cell of the coverage matrix: a withering
// time point cross a basket seal.
type CoverageKey struct {
	TimePoint domain.LogicalTime
	Basket    domain.BasketSeal
}

// WitheringReading is one cell's fixed-point physical quantities.
type WitheringReading struct {
	Key             CoverageKey
	EnvHumidity     domain.Fixed
	MoistureContent domain.Fixed
	LeafTemperature domain.Fixed
	WaterLoss       domain.Fixed
	RedLeafRatio    domain.Fixed
	Supplemental    bool
}

// TendernessCounts holds the per-category sample counts. All counts must be
// non-negative and must sum to the total.
type TendernessCounts struct {
	SingleBud     int64
	OneBudOneLeaf int64
	OldLeaf       int64
	RedLeaf       int64
	TotalSamples  int64
}

// Conserved reports whether the tenderness counts are valid: every category
// non-negative and the sum exactly equals the total sample count.
func (c TendernessCounts) Conserved() bool {
	if c.SingleBud < 0 || c.OneBudOneLeaf < 0 || c.OldLeaf < 0 || c.RedLeaf < 0 || c.TotalSamples < 0 {
		return false
	}
	return c.SingleBud+c.OneBudOneLeaf+c.OldLeaf+c.RedLeaf == c.TotalSamples
}

// TendernessEvidence is an immutable, versioned tenderness result.
type TendernessEvidence struct {
	Version    int64
	Operation  domain.OperationID
	Generation domain.TaskGeneration
	Counts     TendernessCounts
	Digest     string
}

// DeviceAttempt is one scripted, auditable device invocation.
type DeviceAttempt struct {
	CallKey     string
	AttemptSeq  int64
	LogicalTime domain.LogicalTime
	DeviceKind  string
	Retryable   bool
	Succeeded   bool
}

// Ledger is the persistence-side contract for withering coverage, tenderness
// evidence, and device attempts. Failed submissions must not leave partial
// cells or evidence.
type Ledger interface {
	// SubmitReadings atomically writes a full coverage batch or rejects it
	// entirely on any missing/duplicate/invalid cell.
	SubmitReadings(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, readings []WitheringReading) error
	// SubmitTenderness writes a conserved tenderness evidence version.
	SubmitTenderness(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, counts TendernessCounts, op domain.OperationID) (TendernessEvidence, error)
	// RecordAttempt appends a device attempt without advancing business phase.
	RecordAttempt(ctx context.Context, taskID domain.TaskID, attempt DeviceAttempt) error
}

var _ Ledger = (*Service)(nil)
