// Package recovery implements the startup recovery scan. It rebuilds the
// valid lease view for open tasks and re-surfaces pending, retryable device
// attempts ordered by logical time so that a crash or restart never loses
// in-flight work or silently frees a lease.
package recovery

import (
	"context"

	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/store"
)

// PendingAttempt is one retryable device invocation that has not yet
// succeeded and must be resumed after restart.
type PendingAttempt struct {
	TaskID      domain.TaskID
	CallKey     string
	AttemptSeq  int64
	LogicalTime domain.LogicalTime
	DeviceKind  string
}

// Service runs the recovery scan over the store.
type Service struct {
	store *store.Store
}

// NewService builds a recovery service.
func NewService(s *store.Store) *Service { return &Service{store: s} }

// Recover rebuilds the open-task lease view and returns the pending retryable
// device attempts ordered by logical time. Leases are never silently released
// by wall clock, so the scan simply confirms their continued validity.
func (s *Service) Recover(ctx context.Context) ([]PendingAttempt, error) {
	if err := s.rebuildLeases(ctx); err != nil {
		return nil, err
	}
	return s.pendingAttempts(ctx)
}

// rebuildLeases confirms that every open task's leases remain active. Any
// lease that was released solely because of a prior crash marker is
// re-activated; explicit terminal releases are preserved.
func (s *Service) rebuildLeases(ctx context.Context) error {
	_, err := s.store.DB.ExecContext(ctx, `
		UPDATE resource_leases
		SET released = 0, release_reason = ''
		WHERE released = 1 AND release_reason = 'restart-pending'`)
	return err
}

// pendingAttempts returns retryable, not-yet-succeeded device attempts in
// logical-time order.
func (s *Service) pendingAttempts(ctx context.Context) ([]PendingAttempt, error) {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT task_id, call_key, attempt_seq, logical_time, device_kind
		FROM device_attempts
		WHERE retryable = 1 AND succeeded = 0
		ORDER BY logical_time, attempt_seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingAttempt
	for rows.Next() {
		var a PendingAttempt
		if err := rows.Scan(&a.TaskID, &a.CallKey, &a.AttemptSeq, &a.LogicalTime, &a.DeviceKind); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// OpenTaskIDs returns the ids of all non-terminal tasks, used to rebuild the
// lease view and confirm no open task lost its resources.
func (s *Service) OpenTaskIDs(ctx context.Context) ([]domain.TaskID, error) {
	rows, err := s.store.DB.QueryContext(ctx, `
		SELECT id FROM tasks
		WHERE state NOT IN ('fixed', 'risk_isolated', 'cancelled')
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TaskID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, domain.TaskID(id))
	}
	return out, rows.Err()
}
