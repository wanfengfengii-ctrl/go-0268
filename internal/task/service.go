package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/ledger"
	"verdant-leaf-fixation-gate/internal/store"
)

// Service is the concrete task aggregate service. It owns task creation, the
// single-transaction lock barrier, and aggregate reads, coordinating the
// catalog and ledger for consistency validation and resource reservation.
type TaskService struct {
	store   *store.Store
	catalog *catalog.Service
	ledger  *ledger.Service
}

// NewService builds a task service.
func NewService(s *store.Store, c *catalog.Service, l *ledger.Service) *TaskService {
	return &TaskService{store: s, catalog: c, ledger: l}
}

// Store exposes the underlying store to closely-coupled orchestration code.
func (s *TaskService) Store() *store.Store { return s.store }

// Create registers a new pending-lock task.
func (s *TaskService) Create(ctx context.Context, id domain.TaskID, batch domain.BatchNumber, plotID, roundID string, op domain.OperationID) (*LeafIntakeTask, error) {
	var t *LeafIntakeTask
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks WHERE leaf_batch = ?`, string(batch)).Scan(&existing); err != nil {
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
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tasks(id, leaf_batch, garden_plot, picking_round, generation, state, logical_time)
			VALUES (?, ?, ?, ?, 1, ?, 1)`,
			string(id), string(batch), plotID, roundID, string(domain.StatePendingLock)); err != nil {
			return err
		}
		t = &LeafIntakeTask{
			ID:           id,
			LeafBatch:    batch,
			GardenPlot:   plotID,
			PickingRound: roundID,
			Generation:   1,
			State:        domain.StatePendingLock,
			LogicalTime:  1,
		}
		return nil
	})
	return t, err
}

// Lock atomically fixes the lock snapshot, validates catalog consistency, and
// acquires every lease and latch in one transaction. Any mismatch, stale
// digest, duplicate identifier, or resource conflict rolls back the entire
// operation, leaving no partial leases or reservations.
func (s *TaskService) Lock(ctx context.Context, id domain.TaskID, op domain.OperationID, snap LockSnapshot) (*LeafIntakeTask, error) {
	var out *LeafIntakeTask
	err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		t, err := loadTaskTx(ctx, tx, id)
		if err != nil {
			return fmt.Errorf("loadTaskTx: %w", err)
		}
		if t.State != domain.StatePendingLock {
			return &domain.DomainError{
				Code:           domain.CodeTerminalStateRejected,
				OperationID:    op,
				TaskGeneration: t.Generation,
				Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "task is not pending lock"}},
			}
		}

		// Catalog consistency validation.
		if err := validateLockTx(ctx, tx, t.GardenPlot, t.PickingRound, snap.RuleDigest); err != nil {
			return err
		}
		ts, err := loadThresholdsTx(ctx, tx, snap.RuleDigest)
		if err != nil {
			return fmt.Errorf("loadThresholdsTx: %w", err)
		}

		// Reserve batch, seals, and blind codes.
		if err := reserveBatchTx(ctx, tx, id, t.LeafBatch, snap.BasketSeals, snap.BlindCodes); err != nil {
			return err
		}
		// Reserve tenderness points.
		for _, pt := range snap.TendernessPts {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO tenderness_points(id, task_id) VALUES (?, ?)`, pt, string(id)); err != nil {
				return err
			}
		}
		// Acquire all leases.
		leases := lockLeases(snap)
		if err := acquireLeasesTx(ctx, tx, id, t.Generation, leases); err != nil {
			return err
		}

		snap.Thresholds = ts
		snap.LockedAt = t.LogicalTime + 1
		raw, err := json.Marshal(snap)
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks SET rule_digest = ?, state = ?, snapshot_json = ?, logical_time = ?, version = version + 1
			WHERE id = ? AND state = ?`,
			string(snap.RuleDigest), string(domain.StatePendingReceipt), string(raw), int64(snap.LockedAt),
			string(id), string(domain.StatePendingLock)); err != nil {
			return err
		}

		t.RuleDigest = snap.RuleDigest
		t.State = domain.StatePendingReceipt
		t.Snapshot = &snap
		t.LogicalTime = snap.LockedAt
		out = t
		return nil
	})
	return out, err
}

// lockLeases expands a lock snapshot into the ordered lease set across the
// four resource classes.
func lockLeases(snap LockSnapshot) []ledger.ResourceLease {
	var leases []ledger.ResourceLease
	for _, r := range snap.WitheringSlots {
		leases = append(leases, ledger.ResourceLease{ResourceType: ledger.ResourceWitheringSlot, ResourceID: r})
	}
	for _, r := range snap.AirBranches {
		leases = append(leases, ledger.ResourceLease{ResourceType: ledger.ResourceAirBranch, ResourceID: r})
	}
	for _, r := range snap.FixationSlots {
		leases = append(leases, ledger.ResourceLease{ResourceType: ledger.ResourceFixationSlot, ResourceID: r})
	}
	for _, r := range snap.AssayWells {
		leases = append(leases, ledger.ResourceLease{ResourceType: ledger.ResourceAssayWell, ResourceID: r})
	}
	return leases
}

// Get loads the current aggregate, including coverage progress and resource
// summary.
func (s *TaskService) Get(ctx context.Context, id domain.TaskID) (*LeafIntakeTask, error) {
	return s.loadTask(ctx, id)
}

func (s *TaskService) loadTask(ctx context.Context, id domain.TaskID) (*LeafIntakeTask, error) {
	t, err := loadTaskTx(ctx, s.store.DB, id)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// Load exposes the aggregate loader for orchestration code.
func (s *TaskService) Load(ctx context.Context, id domain.TaskID) (*LeafIntakeTask, error) {
	return s.loadTask(ctx, id)
}

// TransitionTask applies a state transition atomically, using an optimistic
// version check to enforce single-writer semantics.
func (s *TaskService) TransitionTask(ctx context.Context, id domain.TaskID, from, to domain.TaskState, op domain.OperationID) error {
	return s.store.WithTx(ctx, func(tx *sql.Tx) error {
		t, err := loadTaskTx(ctx, tx, id)
		if err != nil {
			return fmt.Errorf("loadTaskTx: %w", err)
		}
		if err := t.Transition(to, op); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE tasks SET state = ?, logical_time = logical_time + 1, version = version + 1
			WHERE id = ? AND state = ?`,
			string(to), string(id), string(from))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return &domain.DomainError{
				Code:           domain.CodeTerminalStateRejected,
				OperationID:    op,
				TaskGeneration: t.Generation,
				Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "concurrent state change"}},
			}
		}
		return nil
	})
}

// loadTaskTx reads a task row and its snapshot.
func loadTaskTx(ctx context.Context, q interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, id domain.TaskID) (*LeafIntakeTask, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, leaf_batch, garden_plot, picking_round, rule_digest, generation, state,
		       snapshot_json, version, logical_time, terminal_cmd, terminal_op, terminal_at,
		       fixation_credential_id, fixation_confirmed
		FROM tasks WHERE id = ?`, string(id))
	var t LeafIntakeTask
	var snapJSON string
	var terminalCmd, terminalOp, credID string
	var terminalAt int64
	var confirmed int
	if err := row.Scan(&t.ID, &t.LeafBatch, &t.GardenPlot, &t.PickingRound, &t.RuleDigest,
		&t.Generation, &t.State, &snapJSON, &t.Version, &t.LogicalTime,
		&terminalCmd, &terminalOp, &terminalAt, &credID, &confirmed); err != nil {
		return nil, err
	}
	if snapJSON != "" && snapJSON != "{}" {
		var snap LockSnapshot
		if err := json.Unmarshal([]byte(snapJSON), &snap); err != nil {
			return nil, fmt.Errorf("unmarshal snapshot: %w", err)
		}
		t.Snapshot = &snap
	}
	if terminalCmd != "" {
		t.Terminal = &TerminalOutcome{
			Command:   terminalCmd,
			Operation: domain.OperationID(terminalOp),
			At:        domain.LogicalTime(terminalAt),
		}
	}
	return &t, nil
}

func validateLockTx(ctx context.Context, tx *sql.Tx, plotID, roundID string, digest catalog.RuleDigest) error {
	var matched int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM plot_rounds WHERE plot_id = ? AND round_id = ?`, plotID, roundID).Scan(&matched); err != nil {
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
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM processing_rule_versions r
		JOIN rule_plot_rounds pr ON pr.digest = r.digest
		WHERE r.current = 1 AND r.published = 1 AND pr.plot_id = ? AND pr.round_id = ? AND r.digest = ?`,
		plotID, roundID, string(digest)).Scan(&current); err != nil {
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

func loadThresholdsTx(ctx context.Context, tx *sql.Tx, digest catalog.RuleDigest) (catalog.ThresholdSet, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT thresholds FROM processing_rule_versions WHERE digest = ?`, string(digest)).Scan(&raw); err != nil {
		return catalog.ThresholdSet{}, err
	}
	var ts catalog.ThresholdSet
	if err := json.Unmarshal([]byte(raw), &ts); err != nil {
		return catalog.ThresholdSet{}, err
	}
	return ts, nil
}

func reserveBatchTx(ctx context.Context, tx *sql.Tx, taskID domain.TaskID, batch domain.BatchNumber, seals []domain.BasketSeal, blinds []domain.BlindCode) error {
	for _, seal := range seals {
		if _, err := tx.ExecContext(ctx, `INSERT INTO basket_samples(seal, task_id) VALUES (?, ?)`, string(seal), string(taskID)); err != nil {
			return domainCode(err, domain.CodeDuplicateBasketSeal, string(seal))
		}
	}
	for _, bc := range blinds {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO blind_samples(code, task_id, commitment, sealed, revealed) VALUES (?, ?, ?, 0, 0)`,
			string(bc), string(taskID), domain.CanonicalDigest(bc)); err != nil {
			return domainCode(err, domain.CodeBlindCodeConflict, string(bc))
		}
	}
	return nil
}

func acquireLeasesTx(ctx context.Context, tx *sql.Tx, taskID domain.TaskID, gen domain.TaskGeneration, leases []ledger.ResourceLease) error {
	for _, l := range leases {
		var heldBy string
		var released int
		err := tx.QueryRowContext(ctx, `
			SELECT task_id, released FROM resource_leases WHERE resource_type = ? AND resource_id = ?`,
			string(l.ResourceType), l.ResourceID).Scan(&heldBy, &released)
		if err == sql.ErrNoRows {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO resource_leases(resource_type, resource_id, task_id, generation, version, released)
				VALUES (?, ?, ?, ?, 1, 0)`,
				string(l.ResourceType), l.ResourceID, string(taskID), int64(gen)); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if released == 0 {
			return &domain.DomainError{
				Code:           domain.CodeResourceLeaseConflict,
				TaskGeneration: gen,
				Reasons: []domain.Reason{{
					Code:    domain.CodeResourceLeaseConflict,
					Message: fmt.Sprintf("resource %s/%s already leased", l.ResourceType, l.ResourceID),
				}},
			}
		}
		// Re-acquire a previously released slot atomically.
		if _, err := tx.ExecContext(ctx, `
			UPDATE resource_leases SET task_id = ?, generation = ?, version = version + 1, released = 0, release_reason = ''
			WHERE resource_type = ? AND resource_id = ?`,
			string(taskID), int64(gen), string(l.ResourceType), l.ResourceID); err != nil {
			return err
		}
	}
	return nil
}

// domainCode converts a unique-constraint violation into a stable domain
// error with the supplied code.
func domainCode(err error, code domain.ErrorCode, key string) error {
	if isUnique(err) {
		return &domain.DomainError{
			Code:    code,
			Reasons: []domain.Reason{{Code: code, Message: "duplicate identifier: " + key}},
		}
	}
	return err
}

func isUnique(err error) bool {
	msg := err.Error()
	return containsStr(msg, "UNIQUE constraint") || containsStr(msg, "constraint failed")
}

func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// ReceiptResult is the outcome of a dual-receipt confirmation.
type ReceiptResult struct {
	TaskID     domain.TaskID        `json:"task_id"`
	State      domain.TaskState     `json:"state"`
	Receiptors []domain.PersonnelID `json:"receiptors"`
}

// SubmitReceipts confirms the dual receipt with idempotency semantics. Two
// distinct qualified receivers must confirm the same batch and generation.
func (s *TaskService) SubmitReceipts(ctx context.Context, id domain.TaskID, gen domain.TaskGeneration, op domain.OperationID, persons []domain.PersonnelID) (*ReceiptResult, error) {
	reqDigest := domain.CanonicalDigest(persons)
	var result ReceiptResult
	resp, err := s.store.RunIdempotent(ctx, string(id), op, gen, reqDigest, func(tx *sql.Tx) (any, error) {
		t, err := loadTaskTx(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if domain.IsTerminal(t.State) {
			return nil, terminalErr(op, t.Generation)
		}
		if t.State != domain.StatePendingReceipt {
			return nil, &domain.DomainError{
				Code:           domain.CodeTerminalStateRejected,
				OperationID:    op,
				TaskGeneration: t.Generation,
				Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "task is not pending receipt"}},
			}
		}
		if err := validateReceiptors(ctx, tx, t.Snapshot, persons); err != nil {
			return nil, err
		}
		for _, p := range persons {
			if _, err := tx.ExecContext(ctx, `INSERT INTO receipts(task_id, personnel_id) VALUES (?, ?)`, string(id), string(p)); err != nil {
				return nil, err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks SET state = ?, logical_time = logical_time + 1, version = version + 1 WHERE id = ?`,
			string(domain.StateSlotOccupied), string(id)); err != nil {
			return nil, err
		}
		result = ReceiptResult{TaskID: id, State: domain.StateSlotOccupied, Receiptors: persons}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func validateReceiptors(ctx context.Context, tx *sql.Tx, snap *LockSnapshot, persons []domain.PersonnelID) error {
	if len(persons) != 2 {
		return &domain.DomainError{
			Code:    domain.CodeRoleOverlap,
			Reasons: []domain.Reason{{Code: domain.CodeRoleOverlap, Message: "exactly two distinct receivers are required"}},
		}
	}
	if persons[0] == persons[1] {
		return &domain.DomainError{
			Code:    domain.CodeRoleOverlap,
			Reasons: []domain.Reason{{Code: domain.CodeRoleOverlap, Message: "receivers must be distinct"}},
		}
	}
	allowed := map[domain.PersonnelID]bool{}
	if snap != nil {
		for _, p := range snap.ReceiptPersons {
			allowed[p] = true
		}
	}
	for _, p := range persons {
		if snap != nil && !allowed[p] {
			return &domain.DomainError{
				Code:    domain.CodeRoleOverlap,
				Reasons: []domain.Reason{{Code: domain.CodeRoleOverlap, Message: "receiver is not authorized for this task"}},
			}
		}
		var qualified int
		if err := tx.QueryRowContext(ctx, `SELECT qualified FROM personnel WHERE id = ?`, string(p)).Scan(&qualified); err != nil {
			return err
		}
		if qualified == 0 {
			return &domain.DomainError{
				Code:    domain.CodeRoleOverlap,
				Reasons: []domain.Reason{{Code: domain.CodeRoleOverlap, Message: "receiver is not qualified"}},
			}
		}
	}
	return nil
}

func terminalErr(op domain.OperationID, gen domain.TaskGeneration) error {
	return &domain.DomainError{
		Code:           domain.CodeTerminalStateRejected,
		OperationID:    op,
		TaskGeneration: gen,
		Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "terminal state rejects operations"}},
	}
}
