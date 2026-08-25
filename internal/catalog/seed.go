package catalog

import (
	"context"

	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/ledger"
)

// SeedDemo populates a complete, self-consistent directory for local demos and
// integration tests: one garden plot, two picking rounds, a single published
// rule with permissive-but-explicit thresholds, a withering template, a full
// resource pool, and six qualified workers. It is idempotent with respect to
// re-registration and never overwrites a digest.
func SeedDemo(ctx context.Context, s *Service) error {
	const digest = RuleDigest("rule-demo-v1")

	// Garden and rounds.
	if err := s.RegisterPlot(ctx, "plot-east", "东山小区"); err != nil {
		return err
	}
	for _, r := range []struct{ id, name string }{
		{"round-spring", "春茶头采"},
		{"round-autumn", "秋茶轮次"},
	} {
		if err := s.RegisterRound(ctx, r.id, r.name); err != nil {
			return err
		}
		if err := s.AddPlotRound(ctx, "plot-east", r.id); err != nil {
			return err
		}
	}

	// Rule with explicit closed intervals (permissive for demo flow).
	ts := ThresholdSet{
		MoistureContent: closed(0, 9000),
		LeafTemperature: closed(-100, 600),
		EnvHumidity:     closed(0, 1000),
		WaterLoss:       closed(0, 9000),
		RedLeafRatio:    closed(0, 500),
		AssayInhibition: closed(0, 10000),
	}
	if err := s.PublishRule(ctx, "v1", digest, ts); err != nil {
		return err
	}
	if err := s.SetRulePlotRound(ctx, digest, "plot-east", "round-spring"); err != nil {
		return err
	}
	if err := s.SetWitheringTemplate(ctx, digest, []domain.LogicalTime{1, 2, 3}); err != nil {
		return err
	}

	// Resource pool: two withering slots, two air branches, one fixation slot,
	// and three assay wells.
	for _, slot := range []string{"slot-1", "slot-2"} {
		if err := s.RegisterResource(ctx, string(ledger.ResourceWitheringSlot), slot); err != nil {
			return err
		}
	}
	for _, branch := range []string{"air-a", "air-b"} {
		if err := s.RegisterResource(ctx, string(ledger.ResourceAirBranch), branch); err != nil {
			return err
		}
	}
	if err := s.RegisterResource(ctx, string(ledger.ResourceFixationSlot), "fix-1"); err != nil {
		return err
	}
	for _, well := range []string{"well-1", "well-2", "well-3"} {
		if err := s.RegisterResource(ctx, string(ledger.ResourceAssayWell), well); err != nil {
			return err
		}
	}

	// Six qualified workers: three receivers and three reviewers.
	for _, p := range []struct {
		id   domain.PersonnelID
		name string
	}{
		{"recv-a", "收青员甲"},
		{"recv-b", "收青员乙"},
		{"recv-c", "收青员丙"},
		{"rev-x", "复核员甲"},
		{"rev-y", "复核员乙"},
		{"rev-z", "复核员丙"},
	} {
		if err := s.RegisterPersonnel(ctx, p.id, p.name, true); err != nil {
			return err
		}
		if err := s.AddPersonnelRound(ctx, p.id, "plot-east", "round-spring"); err != nil {
			return err
		}
	}
	return nil
}

func closed(lo, hi int64) domain.Interval {
	return domain.Interval{
		Lower: &domain.Bound{Value: lo, Kind: domain.BoundClosed},
		Upper: &domain.Bound{Value: hi, Kind: domain.BoundClosed},
	}
}
