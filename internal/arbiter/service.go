package arbiter

import (
	"context"
	"database/sql"
	"encoding/json"

	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/store"
)

// Service is the concrete arbiter implementation: sample sealing/reveal,
// assay and retest evidence, re-judgment generation chains, independent
// review, and the single-writer terminal barrier.
type Service struct {
	store *store.Store
}

// NewService builds an arbiter service over the store.
func NewService(s *store.Store) *Service { return &Service{store: s} }

// Seal marks all blind samples for a task as sealed.
func (s *Service) Seal(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, op domain.OperationID) error {
	_, err := s.store.DB.ExecContext(ctx, `UPDATE blind_samples SET sealed = 1 WHERE task_id = ?`, string(taskID))
	return err
}

// Reveal reveals sealed blind codes, rejecting an early reveal before sealing.
func (s *Service) Reveal(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, op domain.OperationID) ([]domain.BlindCode, error) {
	var sealedCount int
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM blind_samples WHERE task_id = ? AND sealed = 1`, string(taskID)).Scan(&sealedCount); err != nil {
		return nil, err
	}
	var total int
	if err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM blind_samples WHERE task_id = ?`, string(taskID)).Scan(&total); err != nil {
		return nil, err
	}
	if sealedCount < total {
		return nil, &domain.DomainError{
			Code:           domain.CodeBlindCodeEarlyReveal,
			OperationID:    op,
			TaskGeneration: gen,
			Reasons: []domain.Reason{{
				Code:    domain.CodeBlindCodeEarlyReveal,
				Message: "blind codes cannot be revealed before samples are sealed",
			}},
		}
	}
	return s.reveal(ctx, taskID)
}

func (s *Service) reveal(ctx context.Context, taskID domain.TaskID) ([]domain.BlindCode, error) {
	var out []domain.BlindCode
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT code FROM blind_samples WHERE task_id = ? ORDER BY code`, string(taskID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			out = append(out, domain.BlindCode(c))
		}
		if err := rows.Err(); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE blind_samples SET revealed = 1 WHERE task_id = ?`, string(taskID))
		return err
	})
	return out, err
}

// SubmitAssay appends an immutable pesticide screening evidence version.
func (s *Service) SubmitAssay(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, e AssayEvidence) error {
	if e.Inhibition.Scale != domain.ScalePerTenThousand {
		return &domain.DomainError{
			Code: domain.CodeFixedPointOverflow,
			Reasons: []domain.Reason{{
				Code:    domain.CodeFixedPointOverflow,
				Message: "assay inhibition must use per-ten-thousand scale",
			}},
		}
	}
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM assay_evidence WHERE task_id = ?`, string(taskID)).Scan(&next); err != nil {
			return err
		}
		e.Version = next
		e.Digest = domain.CanonicalDigest(e)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO assay_evidence(task_id, version, well, blind_code, generation, inhibition, digest)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(taskID), next, e.Well, string(e.BlindCode), int64(gen), fixedJSON(e.Inhibition), e.Digest)
		return err
	})
}

// SubmitRetest appends an immutable moisture/temperature retest evidence.
func (s *Service) SubmitRetest(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, e RetestEvidence) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var next int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM retest_evidence WHERE task_id = ?`, string(taskID)).Scan(&next); err != nil {
			return err
		}
		e.Version = next
		e.Digest = domain.CanonicalDigest(e)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO retest_evidence(task_id, version, generation, leaf_temperature, moisture_content, digest)
			VALUES (?, ?, ?, ?, ?, ?)`,
			string(taskID), next, int64(gen), fixedJSON(e.LeafTemperature), fixedJSON(e.MoistureContent), e.Digest)
		return err
	})
}

// CreateRejudgment records a new active re-judgment generation and bumps the
// task generation, chaining to the previous generation.
func (s *Service) CreateRejudgment(ctx context.Context, taskID domain.TaskID, c RejudgmentCase) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var current int64
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT generation, state FROM tasks WHERE id = ?`, string(taskID)).Scan(&current, &state); err != nil {
			return err
		}
		prev := domain.TaskGeneration(current)
		c.Generation = prev + 1
		c.PrevGeneration = &prev
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO rejudgment_cases(task_id, generation, prev_generation, affected_json)
			VALUES (?, ?, ?, ?)`, string(taskID), int64(c.Generation), int64(prev), string(raw)); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks SET generation = ?, logical_time = logical_time + 1 WHERE id = ?`,
			int64(c.Generation), string(taskID)); err != nil {
			return err
		}
		return nil
	})
}

// SubmitReview records one independent reviewer decision, enforcing role
// separation from receivers, authorization, and a two-reviewer limit.
func (s *Service) SubmitReview(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, d ReviewDecision) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		snap, err := loadSnapshotTx(ctx, tx, taskID)
		if err != nil {
			return err
		}
		for _, r := range snap.ReceiptPersons {
			if r == d.PersonnelID {
				return &domain.DomainError{
					Code:           domain.CodeRoleOverlap,
					TaskGeneration: gen,
					Reasons: []domain.Reason{{
						Code:    domain.CodeRoleOverlap,
						Message: "reviewer overlaps with a receipt person",
					}},
				}
			}
		}
		authorized := false
		for _, r := range snap.ReviewPersons {
			if r == d.PersonnelID {
				authorized = true
			}
		}
		if !authorized {
			return &domain.DomainError{
				Code:           domain.CodeRoleOverlap,
				TaskGeneration: gen,
				Reasons: []domain.Reason{{
					Code:    domain.CodeRoleOverlap,
					Message: "reviewer is not authorized for this task",
				}},
			}
		}

		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_decisions WHERE task_id = ?`, string(taskID)).Scan(&n); err != nil {
			return err
		}
		if n >= 2 {
			return &domain.DomainError{
				Code:           domain.CodeRoleOverlap,
				TaskGeneration: gen,
				Reasons: []domain.Reason{{
					Code:    domain.CodeRoleOverlap,
					Message: "two independent reviews already recorded",
				}},
			}
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO review_decisions(task_id, personnel_id, approved, generation)
			VALUES (?, ?, ?, ?)`, string(taskID), string(d.PersonnelID), d.Approved, int64(gen))
		return err
	})
}

// ReviewCount returns the number of review decisions recorded.
func (s *Service) ReviewCount(ctx context.Context, taskID domain.TaskID) (int, error) {
	var n int
	err := s.store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM review_decisions WHERE task_id = ?`, string(taskID)).Scan(&n)
	return n, err
}

// Terminal performs the single-writer compare-and-swap to settle the task.
func (s *Service) Terminal(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, cmd TerminalCommand, op domain.OperationID) (TerminalDecision, error) {
	var out TerminalDecision
	var fixationCredential *FixationCredential
	if cmd == CommandRelease {
		var lt int64
		if err := s.store.DB.QueryRowContext(ctx, `SELECT logical_time FROM tasks WHERE id = ?`, string(taskID)).Scan(&lt); err != nil {
			return out, err
		}
		credID := "cred-" + string(taskID)
		if _, err := s.store.DB.ExecContext(ctx, `
			INSERT INTO fixation_credentials(id, task_id, issued_at) VALUES (?, ?, ?)`,
			credID, string(taskID), lt); err != nil {
			return out, err
		}
		fixationCredential = &FixationCredential{ID: credID, TaskID: taskID, IssuedAt: domain.LogicalTime(lt)}
	}
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		t, err := loadTaskTx(ctx, tx, taskID)
		if err != nil {
			return err
		}
		if domain.IsTerminal(t.State) {
			return &domain.DomainError{
				Code:           domain.CodeTerminalStateRejected,
				OperationID:    op,
				TaskGeneration: t.Generation,
				Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "task already terminal"}},
			}
		}

		if cmd == CommandRelease {
			// Release requires full closure and passing reviews.
			if err := s.checkClosure(ctx, tx, taskID); err != nil {
				return err
			}
			ok, err := s.reviewsApprovedTx(ctx, tx, taskID)
			if err != nil {
				return err
			}
			if !ok {
				return &domain.DomainError{
					Code:           domain.CodeRoleOverlap,
					OperationID:    op,
					TaskGeneration: t.Generation,
					Reasons:        []domain.Reason{{Code: domain.CodeRoleOverlap, Message: "independent reviews not complete or not approved"}},
				}
			}
		}

		// Single-writer barrier: only the first writer transitions the state.
		target := terminalState(cmd)
		res, err := tx.ExecContext(ctx, `
			UPDATE tasks SET state = ?, terminal_cmd = ?, terminal_op = ?, terminal_at = logical_time + 1,
			       logical_time = logical_time + 1, version = version + 1
			WHERE id = ? AND state NOT IN ('fixed', 'risk_isolated', 'cancelled')`,
			string(target), string(cmd), string(op), string(taskID))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return &domain.DomainError{
				Code:           domain.CodeTerminalStateRejected,
				OperationID:    op,
				TaskGeneration: t.Generation,
				Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "terminal decision already taken"}},
			}
		}

		out = TerminalDecision{Command: cmd, Operation: op, FixationCredential: fixationCredential}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO terminal_decisions(task_id, command, operation, fixation_credential_id)
			VALUES (?, ?, ?, ?)`, string(taskID), string(cmd), string(op), credentialID(out)); err != nil {
			return err
		}
		return nil
	})
	return out, err
}

// ConfirmFixation confirms the unique fixation credential as completed.
func (s *Service) ConfirmFixation(ctx context.Context, taskID domain.TaskID, credentialID string, op domain.OperationID) error {
	res, err := s.store.DB.ExecContext(ctx, `
		UPDATE fixation_credentials SET confirmed = 1 WHERE id = ? AND task_id = ?`,
		credentialID, string(taskID))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return &domain.DomainError{
			Code:        domain.CodeTerminalStateRejected,
			OperationID: op,
			Reasons: []domain.Reason{{
				Code:    domain.CodeTerminalStateRejected,
				Message: "fixation credential not found",
			}},
		}
	}
	return nil
}

// checkClosure verifies all evidence is closed and thresholds satisfied.
func (s *Service) checkClosure(ctx context.Context, tx *sql.Tx, taskID domain.TaskID) error {
	var coverage, tenderness, assay, retest, revealed int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM withering_cells WHERE task_id = ?`, string(taskID)).Scan(&coverage); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenderness_evidence WHERE task_id = ?`, string(taskID)).Scan(&tenderness); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM assay_evidence WHERE task_id = ?`, string(taskID)).Scan(&assay); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM retest_evidence WHERE task_id = ?`, string(taskID)).Scan(&retest); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM blind_samples WHERE task_id = ? AND revealed = 1`, string(taskID)).Scan(&revealed); err != nil {
		return err
	}
	if coverage == 0 || tenderness == 0 || assay == 0 || retest == 0 || revealed == 0 {
		return &domain.DomainError{
			Code: domain.CodeCoverageMissing,
			Reasons: []domain.Reason{{
				Code:    domain.CodeCoverageMissing,
				Message: "evidence not fully closed before release",
			}},
		}
	}
	return nil
}

func (s *Service) reviewsApprovedTx(ctx context.Context, tx *sql.Tx, taskID domain.TaskID) (bool, error) {
	var n, approved int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(approved),0) FROM review_decisions WHERE task_id = ?`, string(taskID)).Scan(&n, &approved); err != nil {
		return false, err
	}
	return n >= 2 && approved == n, nil
}

func terminalState(cmd TerminalCommand) domain.TaskState {
	switch cmd {
	case CommandRelease:
		return domain.StateFixed
	case CommandIsolate:
		return domain.StateRiskIsolated
	default:
		return domain.StateCancelled
	}
}

func credentialID(d TerminalDecision) string {
	if d.FixationCredential != nil {
		return d.FixationCredential.ID
	}
	return ""
}

func fixedJSON(f domain.Fixed) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func loadSnapshotTx(ctx context.Context, tx *sql.Tx, taskID domain.TaskID) (*taskSnapshot, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT snapshot_json FROM tasks WHERE id = ?`, string(taskID)).Scan(&raw); err != nil {
		return nil, err
	}
	var snap taskSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// taskSnapshot is the subset of the lock snapshot needed for role checks.
type taskSnapshot struct {
	ReceiptPersons []domain.PersonnelID `json:"receipt_persons"`
	ReviewPersons  []domain.PersonnelID `json:"review_persons"`
}

func loadTaskTx(ctx context.Context, tx *sql.Tx, taskID domain.TaskID) (*taskState, error) {
	var ts taskState
	if err := tx.QueryRowContext(ctx, `SELECT generation, state FROM tasks WHERE id = ?`, string(taskID)).Scan(&ts.Generation, &ts.State); err != nil {
		return nil, err
	}
	return &ts, nil
}

type taskState struct {
	Generation domain.TaskGeneration
	State      domain.TaskState
}
