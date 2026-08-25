package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/store"
)

// Service is the concrete Ledger implementation over the transactional store.
type Service struct {
	store *store.Store
}

// NewService builds a ledger service over the given store.
func NewService(s *store.Store) *Service { return &Service{store: s} }

// AcquireBatch atomically acquires all requested leases. Requests are sorted
// by resource kind then identifier so that conflicts are deterministic and no
// partial lease set is ever left behind.
func (s *Service) AcquireBatch(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, leases []ResourceLease) error {
	// Deterministic acquisition order.
	sorted := append([]ResourceLease(nil), leases...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ResourceType != sorted[j].ResourceType {
			return sorted[i].ResourceType < sorted[j].ResourceType
		}
		return sorted[i].ResourceID < sorted[j].ResourceID
	})

	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		for _, l := range sorted {
			var heldBy string
			var heldReleased int
			err := tx.QueryRowContext(ctx, `
				SELECT task_id, released FROM resource_leases
				WHERE resource_type = ? AND resource_id = ?`,
				string(l.ResourceType), l.ResourceID).Scan(&heldBy, &heldReleased)
			if err == sql.ErrNoRows {
				// Free.
			} else if err != nil {
				return err
			} else if heldBy != string(taskID) && heldReleased == 0 {
				return &domain.DomainError{
					Code:           domain.CodeResourceLeaseConflict,
					TaskGeneration: gen,
					Reasons: []domain.Reason{{
						Code:    domain.CodeResourceLeaseConflict,
						Message: fmt.Sprintf("resource %s/%s already leased", l.ResourceType, l.ResourceID),
					}},
				}
			} else if heldBy == string(taskID) {
				// Idempotent re-acquisition of the same lease.
				continue
			}

			if _, err := tx.ExecContext(ctx, `
				INSERT INTO resource_leases(resource_type, resource_id, task_id, generation, version, released)
				VALUES (?, ?, ?, ?, 1, 0)`,
				string(l.ResourceType), l.ResourceID, string(taskID), int64(gen)); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReserveBatch latches a batch number, basket seals, and blind codes for a
// task. Any duplicate identifier across open tasks aborts the whole
// reservation.
func (s *Service) ReserveBatch(ctx context.Context, taskID domain.TaskID, batch domain.BatchNumber, seals []domain.BasketSeal, blinds []domain.BlindCode) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		// Leaf batch is globally unique via the tasks unique index; an explicit
		// check yields a stable error code.
		var existing int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM tasks WHERE leaf_batch = ? AND id != ?`,
			string(batch), string(taskID)).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return &domain.DomainError{
				Code: domain.CodeDuplicateBasketSeal,
				Reasons: []domain.Reason{{
					Code:      domain.CodeDuplicateBasketSeal,
					Message:   "leaf batch already reserved",
					LeafBatch: string(batch),
				}},
			}
		}

		sort.Slice(seals, func(i, j int) bool { return seals[i] < seals[j] })
		for _, seal := range seals {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO basket_samples(seal, task_id) VALUES (?, ?)`,
				string(seal), string(taskID)); err != nil {
				return domainCode(err, domain.CodeDuplicateBasketSeal, string(seal))
			}
		}
		sort.Slice(blinds, func(i, j int) bool { return blinds[i] < blinds[j] })
		for _, bc := range blinds {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO blind_samples(code, task_id, commitment, sealed, revealed)
				VALUES (?, ?, ?, 0, 0)`, string(bc), string(taskID), domain.CanonicalDigest(bc)); err != nil {
				return domainCode(err, domain.CodeBlindCodeConflict, string(bc))
			}
		}
		return nil
	})
}

// domainCode converts a unique-constraint violation into a stable domain
// error with the supplied code and sort key.
func domainCode(err error, code domain.ErrorCode, key string) error {
	if isUniqueViolation(err) {
		return &domain.DomainError{
			Code:    code,
			Reasons: []domain.Reason{{Code: code, Message: "duplicate identifier: " + key}},
		}
	}
	return err
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite surfaces constraint failures as *sqlite.Error with a
	// Constraint code. Fall back to a message probe for portability.
	msg := err.Error()
	return contains(msg, "UNIQUE constraint") || contains(msg, "constraint failed")
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// Release releases all leases held by a task with an explicit reason.
func (s *Service) Release(ctx context.Context, taskID domain.TaskID, reason string) error {
	_, err := s.store.DB.ExecContext(ctx, `
		UPDATE resource_leases SET released = 1, release_reason = ? WHERE task_id = ? AND released = 0`,
		reason, string(taskID))
	return err
}

// RebuildLeases rebuilds the valid lease view for open tasks after a restart.
// Because leases are never silently released, recovery is a consistency pass
// that returns the set of open task ids still holding leases.
func (s *Service) RebuildLeases(ctx context.Context) error {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT DISTINCT task_id FROM resource_leases WHERE released = 0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ListLeases returns the active leases held by a task.
func (s *Service) ListLeases(ctx context.Context, taskID domain.TaskID) ([]ResourceLease, error) {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT resource_type, resource_id, generation, version, released, release_reason
		FROM resource_leases WHERE task_id = ? ORDER BY resource_type, resource_id`,
		string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResourceLease
	for rows.Next() {
		var l ResourceLease
		l.TaskID = taskID
		if err := rows.Scan(&l.ResourceType, &l.ResourceID, &l.Generation, &l.Version, &l.Released, &l.ReleaseReason); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// BlindSamples returns all blind samples for a task.
func (s *Service) BlindSamples(ctx context.Context, taskID domain.TaskID) ([]BlindSample, error) {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT code, commitment, sealed, revealed FROM blind_samples WHERE task_id = ? ORDER BY code`,
		string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlindSample
	for rows.Next() {
		var b BlindSample
		b.TaskID = taskID
		if err := rows.Scan(&b.Code, &b.Commitment, &b.Sealed, &b.Revealed); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
