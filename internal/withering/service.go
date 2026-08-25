package withering

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/store"
)

// Service is the concrete withering/tenderness ledger implementation.
type Service struct {
	store *store.Store
}

// NewService builds a withering service over the store.
func NewService(s *store.Store) *Service { return &Service{store: s} }

// SubmitReadings validates and atomically writes a full coverage batch. It
// rejects any missing, duplicate, unknown, or invalid cell without leaving
// partial cells behind.
func (s *Service) SubmitReadings(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, readings []WitheringReading) error {
	timePoints, baskets, err := s.coverageUniverse(ctx, taskID)
	if err != nil {
		return err
	}

	expected := len(timePoints) * len(baskets)
	if len(readings) != expected {
		return &domain.DomainError{
			Code:           domain.CodeCoverageMissing,
			TaskGeneration: gen,
			Reasons: []domain.Reason{{
				Code:    domain.CodeCoverageMissing,
				Message: fmt.Sprintf("coverage incomplete: expected %d cells, got %d", expected, len(readings)),
			}},
		}
	}

	seen := make(map[string]bool, len(readings))
	for _, r := range readings {
		if err := validateScales(r); err != nil {
			return err
		}
		key := fmt.Sprintf("%d|%s", r.Key.TimePoint, r.Key.Basket)
		if seen[key] {
			return &domain.DomainError{
				Code:           domain.CodeCoverageDuplicate,
				TaskGeneration: gen,
				Reasons: []domain.Reason{{
					Code:       domain.CodeCoverageDuplicate,
					Message:    "duplicate coverage cell",
					BasketSeal: string(r.Key.Basket),
				}},
			}
		}
		seen[key] = true
		if !hasBasket(baskets, r.Key.Basket) {
			return &domain.DomainError{
				Code:           domain.CodeCoverageMissing,
				TaskGeneration: gen,
				Reasons: []domain.Reason{{
					Code:       domain.CodeCoverageMissing,
					Message:    "cell references an unlocked basket",
					BasketSeal: string(r.Key.Basket),
				}},
			}
		}
		if !hasTime(timePoints, r.Key.TimePoint) {
			return &domain.DomainError{
				Code:           domain.CodeCoverageMissing,
				TaskGeneration: gen,
				Reasons: []domain.Reason{{
					Code:    domain.CodeCoverageMissing,
					Message: "cell references an unknown time point",
				}},
			}
		}
	}

	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		for _, r := range readings {
			digest := domain.CanonicalDigest(r)
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO withering_cells(task_id, time_point, basket, env_humidity, moisture_content,
					leaf_temperature, water_loss, red_leaf_ratio, supplemental, digest)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				string(taskID), int64(r.Key.TimePoint), string(r.Key.Basket),
				fixedJSON(r.EnvHumidity), fixedJSON(r.MoistureContent), fixedJSON(r.LeafTemperature),
				fixedJSON(r.WaterLoss), fixedJSON(r.RedLeafRatio), r.Supplemental, digest); err != nil {
				return err
			}
		}
		return nil
	})
}

// SubmitTenderness validates conservation and writes an immutable evidence
// version. Non-conserved counts are rejected without writing evidence.
func (s *Service) SubmitTenderness(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, counts TendernessCounts, op domain.OperationID) (TendernessEvidence, error) {
	if !counts.Conserved() {
		return TendernessEvidence{}, &domain.DomainError{
			Code:           domain.CodeCountNotConserved,
			OperationID:    op,
			TaskGeneration: gen,
			Reasons: []domain.Reason{{
				Code:    domain.CodeCountNotConserved,
				Message: "tenderness counts do not conserve the total sample count",
			}},
		}
	}

	var out TendernessEvidence
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(version), 0) + 1 FROM tenderness_evidence WHERE task_id = ?`,
			string(taskID)).Scan(&next); err != nil {
			return err
		}
		out = TendernessEvidence{
			Version:    next,
			Operation:  op,
			Generation: gen,
			Counts:     counts,
			Digest:     domain.CanonicalDigest(counts),
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO tenderness_evidence(task_id, version, operation, generation, single_bud,
				one_bud_one_leaf, old_leaf, red_leaf, total, digest)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			string(taskID), next, string(op), int64(gen), counts.SingleBud, counts.OneBudOneLeaf,
			counts.OldLeaf, counts.RedLeaf, counts.TotalSamples, out.Digest)
		return err
	})
	return out, err
}

// RecordAttempt appends a device attempt without advancing business phase.
func (s *Service) RecordAttempt(ctx context.Context, taskID domain.TaskID, attempt DeviceAttempt) error {
	_, err := s.store.DB.ExecContext(ctx, `
		INSERT INTO device_attempts(task_id, call_key, attempt_seq, logical_time, device_kind, retryable, succeeded, result_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(taskID), attempt.CallKey, attempt.AttemptSeq, int64(attempt.LogicalTime),
		attempt.DeviceKind, attempt.Retryable, attempt.Succeeded, "")
	return err
}

// NextAttemptSeq returns the next attempt sequence number for a call key.
func (s *Service) NextAttemptSeq(ctx context.Context, taskID domain.TaskID, callKey string) (int64, error) {
	var n int64
	err := s.store.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM device_attempts WHERE task_id = ? AND call_key = ?`,
		string(taskID), callKey).Scan(&n)
	return n + 1, err
}

// coverageUniverse returns the ordered withering time points and basket seals
// that define the full coverage matrix for a task.
func (s *Service) coverageUniverse(ctx context.Context, taskID domain.TaskID) ([]domain.LogicalTime, []domain.BasketSeal, error) {
	var digest string
	if err := s.store.DB.QueryRowContext(ctx, `SELECT rule_digest FROM tasks WHERE id = ?`, string(taskID)).Scan(&digest); err != nil {
		return nil, nil, err
	}

	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT time_point FROM withering_templates WHERE digest = ? ORDER BY ordinal`, digest)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var timePoints []domain.LogicalTime
	for rows.Next() {
		var tp int64
		if err := rows.Scan(&tp); err != nil {
			return nil, nil, err
		}
		timePoints = append(timePoints, domain.LogicalTime(tp))
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	bRows, err := s.store.DB.QueryContext(ctx, `
		SELECT seal FROM basket_samples WHERE task_id = ? ORDER BY seal`, string(taskID))
	if err != nil {
		return nil, nil, err
	}
	defer bRows.Close()
	var baskets []domain.BasketSeal
	for bRows.Next() {
		var b string
		if err := bRows.Scan(&b); err != nil {
			return nil, nil, err
		}
		baskets = append(baskets, domain.BasketSeal(b))
	}
	sort.Slice(baskets, func(i, j int) bool { return baskets[i] < baskets[j] })
	return timePoints, baskets, bRows.Err()
}

// SubmittedCells returns the number of valid coverage cells written so far.
func (s *Service) SubmittedCells(ctx context.Context, taskID domain.TaskID) (int, error) {
	var n int
	err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM withering_cells WHERE task_id = ?`, string(taskID)).Scan(&n)
	return n, err
}

func validateScales(r WitheringReading) error {
	if r.EnvHumidity.Scale != domain.ScaleDecimal1 || r.LeafTemperature.Scale != domain.ScaleDecimal1 {
		return scaleErr("leaf temperature and humidity must use one decimal place")
	}
	if r.MoistureContent.Scale != domain.ScalePerTenThousand ||
		r.WaterLoss.Scale != domain.ScalePerTenThousand ||
		r.RedLeafRatio.Scale != domain.ScalePerTenThousand {
		return scaleErr("moisture, water loss, and red leaf ratio must use per-ten-thousand scale")
	}
	return nil
}

func scaleErr(msg string) error {
	return &domain.DomainError{
		Code:    domain.CodeFixedPointOverflow,
		Reasons: []domain.Reason{{Code: domain.CodeFixedPointOverflow, Message: msg}},
	}
}

func fixedJSON(f domain.Fixed) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func hasBasket(baskets []domain.BasketSeal, b domain.BasketSeal) bool {
	for _, x := range baskets {
		if x == b {
			return true
		}
	}
	return false
}

func hasTime(ts []domain.LogicalTime, t domain.LogicalTime) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}
