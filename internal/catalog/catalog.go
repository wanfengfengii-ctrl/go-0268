// Package catalog implements the garden-and-processing-rule directory: garden
// plots, picking rounds, published rule versions with stable digests,
// threshold scales, withering time templates, resource capabilities, and
// personnel qualification. It owns lock-time consistency validation.
package catalog

import (
	"context"

	"verdant-leaf-fixation-gate/internal/domain"
)

// RuleDigest is a stable content digest of a published processing rule
// version. A lock may only reference a digest that is still current.
type RuleDigest string

// GardenPlot is a tea garden plot (小区) available for intake.
type GardenPlot struct {
	ID     string
	Name   string
	Rounds []string // allowed picking rounds
}

// PickingRound is an allowed picking round for a garden plot.
type PickingRound struct {
	ID   string
	Name string
}

// ProcessingRuleVersion is a versioned processing rule with thresholds,
// withering time template, and publication state.
type ProcessingRuleVersion struct {
	Version    string
	Digest     RuleDigest
	Published  bool
	Current    bool
	Thresholds ThresholdSet
}

// ThresholdSet names the fixed-point scales and explicit intervals that gate
// progression through withering, tenderness, and assay phases.
type ThresholdSet struct {
	MoistureContent domain.Interval `json:"moisture_content"` // 含水率，万分比
	LeafTemperature domain.Interval `json:"leaf_temperature"` // 叶温，1 位小数
	EnvHumidity     domain.Interval `json:"env_humidity"`     // 环境湿度，1 位小数
	WaterLoss       domain.Interval `json:"water_loss"`       // 失水率，万分比
	RedLeafRatio    domain.Interval `json:"red_leaf_ratio"`   // 红变比例，万分比
	AssayInhibition domain.Interval `json:"assay_inhibition"` // 农残抑制率，万分比
}

// WitheringTemplate lists the ordered withering time points that form the
// coverage matrix's full set.
type WitheringTemplate struct {
	TimePoints []domain.LogicalTime
}

// ResourceCapability describes the finite resource pool for leases.
type ResourceCapability struct {
	ResourceType string
	IDs          []string
}

// Personnel is a worker with a qualification flag and active role.
type Personnel struct {
	ID        domain.PersonnelID
	Name      string
	Qualified bool
}

// Catalog is the read-side contract used during task locking. Implementations
// must reject plot/round mismatches and stale digests before any resource
// lease is acquired.
type Catalog interface {
	// CurrentRule returns the current published rule version for a plot/round.
	CurrentRule(ctx context.Context, plotID, roundID string) (ProcessingRuleVersion, error)
	// ValidateLock verifies that the plot and round match and that the supplied
	// digest is the current published digest. It returns a *domain.DomainError
	// on mismatch or staleness.
	ValidateLock(ctx context.Context, plotID, roundID string, digest RuleDigest) error
	// Personnel returns the qualified personnel list for a plot/round.
	Personnel(ctx context.Context, plotID, roundID string) ([]Personnel, error)
	// WitheringTemplate returns the time-point template for a rule version.
	WitheringTemplate(ctx context.Context, digest RuleDigest) (WitheringTemplate, error)
}

var _ Catalog = (*Service)(nil)
