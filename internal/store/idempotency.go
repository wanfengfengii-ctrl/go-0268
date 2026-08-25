package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"verdant-leaf-fixation-gate/internal/domain"
)

// idempotencyRecord is the persisted request/response summary for one client
// operation. It enables safe retries after client timeouts.
type idempotencyRecord struct {
	OperationID   string
	TaskID        string
	Generation    int64
	RequestDigest string
	ResponseJSON  []byte
}

// runIdempotent executes work inside a transaction with idempotency
// semantics. work receives the open transaction and returns the JSON response
// to persist and return to the client. Replaying the same operation id with
// the same request digest returns the stored response without re-running
// work; the same operation id with a different digest is rejected with
// OPERATION_CONTENT_CONFLICT.
func (s *Store) RunIdempotent(
	ctx context.Context,
	taskID string,
	op domain.OperationID,
	gen domain.TaskGeneration,
	reqDigest string,
	work func(tx *sql.Tx) (any, error),
) ([]byte, error) {
	var out []byte
	err := s.WithTx(ctx, func(tx *sql.Tx) error {
		rec, found, err := lookupIdempotency(ctx, tx, string(op))
		if err != nil {
			return err
		}
		if found {
			if rec.RequestDigest != reqDigest {
				return &domain.DomainError{
					Code:           domain.CodeOperationContentConflict,
					OperationID:    op,
					TaskGeneration: gen,
					Reasons: []domain.Reason{{
						Code:    domain.CodeOperationContentConflict,
						Message: "operation id reused with different content",
					}},
				}
			}
			out = rec.ResponseJSON
			return nil
		}

		resp, err := work(tx)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if err := insertIdempotency(ctx, tx, idempotencyRecord{
			OperationID:   string(op),
			TaskID:        taskID,
			Generation:    int64(gen),
			RequestDigest: reqDigest,
			ResponseJSON:  raw,
		}); err != nil {
			return err
		}
		out = raw
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func lookupIdempotency(ctx context.Context, tx *sql.Tx, op string) (idempotencyRecord, bool, error) {
	var rec idempotencyRecord
	var resp []byte
	err := tx.QueryRowContext(ctx, `
		SELECT operation_id, task_id, generation, request_digest, response_json
		FROM idempotency_records WHERE operation_id = ?`, op).
		Scan(&rec.OperationID, &rec.TaskID, &rec.Generation, &rec.RequestDigest, &resp)
	if err == sql.ErrNoRows {
		return idempotencyRecord{}, false, nil
	}
	if err != nil {
		return idempotencyRecord{}, false, err
	}
	rec.ResponseJSON = resp
	return rec, true, nil
}

func insertIdempotency(ctx context.Context, tx *sql.Tx, rec idempotencyRecord) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO idempotency_records(operation_id, task_id, generation, request_digest, response_json)
		VALUES (?, ?, ?, ?, ?)`,
		rec.OperationID, rec.TaskID, rec.Generation, rec.RequestDigest, string(rec.ResponseJSON))
	return err
}
