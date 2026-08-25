package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/store"
)

// Service is the concrete Catalog implementation backed by the transactional
// store. It owns lock-time consistency validation and directory registration.
type Service struct {
	store *store.Store
}

// NewService builds a catalog service over the given store.
func NewService(s *store.Store) *Service { return &Service{store: s} }

// ---- registration (directory administration) ----

// RegisterPlot inserts a garden plot.
func (s *Service) RegisterPlot(ctx context.Context, id, name string) error {
	_, err := s.store.DB.ExecContext(ctx,
		`INSERT INTO garden_plots(id, name) VALUES (?, ?)`, id, name)
	return err
}

// RegisterRound inserts a picking round.
func (s *Service) RegisterRound(ctx context.Context, id, name string) error {
	_, err := s.store.DB.ExecContext(ctx,
		`INSERT INTO picking_rounds(id, name) VALUES (?, ?)`, id, name)
	return err
}

// AddPlotRound declares that a round is allowed on a plot.
func (s *Service) AddPlotRound(ctx context.Context, plotID, roundID string) error {
	_, err := s.store.DB.ExecContext(ctx,
		`INSERT OR IGNORE INTO plot_rounds(plot_id, round_id) VALUES (?, ?)`, plotID, roundID)
	return err
}

// PublishRule publishes a processing rule version and marks it current,
// clearing the current flag on any previous version.
func (s *Service) PublishRule(ctx context.Context, version string, digest RuleDigest, ts ThresholdSet) error {
	raw, err := json.Marshal(ts)
	if err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE processing_rule_versions SET current = 0`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO processing_rule_versions(version, digest, published, current, thresholds)
			VALUES (?, ?, 1, 1, ?)`, version, string(digest), string(raw))
		return err
	})
}

// SetRulePlotRound binds a rule digest to a plot/round applicability.
func (s *Service) SetRulePlotRound(ctx context.Context, digest RuleDigest, plotID, roundID string) error {
	_, err := s.store.DB.ExecContext(ctx, `
		INSERT OR IGNORE INTO rule_plot_rounds(digest, plot_id, round_id) VALUES (?, ?, ?)`,
		string(digest), plotID, roundID)
	return err
}

// SetWitheringTemplate records the ordered withering time points for a rule.
func (s *Service) SetWitheringTemplate(ctx context.Context, digest RuleDigest, points []domain.LogicalTime) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM withering_templates WHERE digest = ?`, string(digest)); err != nil {
			return err
		}
		for i, p := range points {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO withering_templates(digest, ordinal, time_point) VALUES (?, ?, ?)`,
				string(digest), i, int64(p)); err != nil {
				return err
			}
		}
		return nil
	})
}

// RegisterResource inserts a resource capability.
func (s *Service) RegisterResource(ctx context.Context, resourceType, resourceID string) error {
	_, err := s.store.DB.ExecContext(ctx, `
		INSERT OR IGNORE INTO resource_capabilities(resource_type, resource_id) VALUES (?, ?)`,
		resourceType, resourceID)
	return err
}

// RegisterPersonnel inserts a worker with qualification status.
func (s *Service) RegisterPersonnel(ctx context.Context, id domain.PersonnelID, name string, qualified bool) error {
	_, err := s.store.DB.ExecContext(ctx, `
		INSERT INTO personnel(id, name, qualified) VALUES (?, ?, ?)`,
		string(id), name, qualified)
	return err
}

// AddPersonnelRound grants a worker qualification for a plot/round.
func (s *Service) AddPersonnelRound(ctx context.Context, id domain.PersonnelID, plotID, roundID string) error {
	_, err := s.store.DB.ExecContext(ctx, `
		INSERT OR IGNORE INTO personnel_rounds(personnel_id, plot_id, round_id) VALUES (?, ?, ?)`,
		string(id), plotID, roundID)
	return err
}

// ---- read-side contract ----

// CurrentRule returns the current published rule version for a plot/round.
func (s *Service) CurrentRule(ctx context.Context, plotID, roundID string) (ProcessingRuleVersion, error) {
	var version string
	var digest string
	var raw string
	err := s.store.DB.QueryRowContext(ctx, `
		SELECT r.version, r.digest, r.thresholds
		FROM processing_rule_versions r
		JOIN rule_plot_rounds pr ON pr.digest = r.digest
		WHERE r.current = 1 AND r.published = 1 AND pr.plot_id = ? AND pr.round_id = ?`,
		plotID, roundID).Scan(&version, &digest, &raw)
	if err != nil {
		return ProcessingRuleVersion{}, err
	}
	var ts ThresholdSet
	if err := json.Unmarshal([]byte(raw), &ts); err != nil {
		return ProcessingRuleVersion{}, err
	}
	return ProcessingRuleVersion{
		Version:    version,
		Digest:     RuleDigest(digest),
		Published:  true,
		Current:    true,
		Thresholds: ts,
	}, nil
}

// ValidateLock verifies plot/round matching and digest currency.
func (s *Service) ValidateLock(ctx context.Context, plotID, roundID string, digest RuleDigest) error {
	var matched int
	if err := s.store.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM plot_rounds WHERE plot_id = ? AND round_id = ?`,
		plotID, roundID).Scan(&matched); err != nil {
		return err
	}
	if matched == 0 {
		return &domain.DomainError{
			Code: domain.CodePlotRoundMismatch,
			Reasons: []domain.Reason{{
				Code:       domain.CodePlotRoundMismatch,
				Message:    "garden plot and picking round do not match",
				GardenPlot: plotID,
			}},
		}
	}

	var current int
	if err := s.store.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM processing_rule_versions r
		JOIN rule_plot_rounds pr ON pr.digest = r.digest
		WHERE r.current = 1 AND r.published = 1 AND pr.plot_id = ? AND pr.round_id = ?
		  AND r.digest = ?`, plotID, roundID, string(digest)).Scan(&current); err != nil {
		return err
	}
	if current == 0 {
		return &domain.DomainError{
			Code: domain.CodeStaleRuleDigest,
			Reasons: []domain.Reason{{
				Code:       domain.CodeStaleRuleDigest,
				Message:    "rule digest is stale or not current",
				GardenPlot: plotID,
			}},
		}
	}
	return nil
}

// Personnel returns the qualified personnel for a plot/round.
func (s *Service) Personnel(ctx context.Context, plotID, roundID string) ([]Personnel, error) {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT p.id, p.name, p.qualified
		FROM personnel p
		JOIN personnel_rounds pr ON pr.personnel_id = p.id
		WHERE p.qualified = 1 AND pr.plot_id = ? AND pr.round_id = ?`,
		plotID, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Personnel
	for rows.Next() {
		var p Personnel
		if err := rows.Scan(&p.ID, &p.Name, &p.Qualified); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// WitheringTemplate returns the time-point template for a rule digest.
func (s *Service) WitheringTemplate(ctx context.Context, digest RuleDigest) (WitheringTemplate, error) {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT time_point FROM withering_templates WHERE digest = ? ORDER BY ordinal`, string(digest))
	if err != nil {
		return WitheringTemplate{}, err
	}
	defer rows.Close()
	var out WitheringTemplate
	for rows.Next() {
		var tp int64
		if err := rows.Scan(&tp); err != nil {
			return WitheringTemplate{}, err
		}
		out.TimePoints = append(out.TimePoints, domain.LogicalTime(tp))
	}
	return out, rows.Err()
}

// Thresholds loads the threshold set for a digest.
func (s *Service) Thresholds(ctx context.Context, digest RuleDigest) (ThresholdSet, error) {
	var raw string
	if err := s.store.DB.QueryRowContext(ctx, `
		SELECT thresholds FROM processing_rule_versions WHERE digest = ?`, string(digest)).Scan(&raw); err != nil {
		return ThresholdSet{}, err
	}
	var ts ThresholdSet
	if err := json.Unmarshal([]byte(raw), &ts); err != nil {
		return ThresholdSet{}, fmt.Errorf("unmarshal thresholds: %w", err)
	}
	return ts, nil
}
