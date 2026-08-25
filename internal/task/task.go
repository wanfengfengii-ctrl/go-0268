// Package task implements the leaf-intake task aggregate: the twelve-state
// machine, task generation, dual receipt confirmation, operation idempotency,
// reviewer eligibility, and the single-writer terminal barrier.
package task

import (
	"context"

	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/domain"
)

// TerminalOutcome captures the single, immutable terminal decision produced
// by the compare-and-swap barrier.
type TerminalOutcome struct {
	Command   string             `json:"command"`
	Operation domain.OperationID `json:"operation"`
	At        domain.LogicalTime `json:"at"`
}

// LockSnapshot is the immutable lock-time snapshot referenced by a task. Once
// written it must never change.
type LockSnapshot struct {
	GardenPlot     string               `json:"garden_plot"`
	PickingRound   string               `json:"picking_round"`
	RuleDigest     catalog.RuleDigest   `json:"rule_digest"`
	BasketSeals    []domain.BasketSeal  `json:"basket_seals"`
	BlindCodes     []domain.BlindCode   `json:"blind_codes"`
	TendernessPts  []string             `json:"tenderness_points"`
	AssayWells     []string             `json:"assay_wells"`
	FixationSlots  []string             `json:"fixation_slots"`
	AirBranches    []string             `json:"air_branches"`
	WitheringSlots []string             `json:"withering_slots"`
	ReceiptPersons []domain.PersonnelID `json:"receipt_persons"`
	ReviewPersons  []domain.PersonnelID `json:"review_persons"`
	Thresholds     catalog.ThresholdSet `json:"thresholds"`
	LockedAt       domain.LogicalTime   `json:"locked_at"`
}

// LeafIntakeTask is the task aggregate root.
type LeafIntakeTask struct {
	ID           domain.TaskID
	LeafBatch    domain.BatchNumber
	GardenPlot   string
	PickingRound string
	RuleDigest   catalog.RuleDigest
	Generation   domain.TaskGeneration
	State        domain.TaskState
	Snapshot     *LockSnapshot
	Version      int64
	LogicalTime  domain.LogicalTime
	Terminal     *TerminalOutcome
}

// Transition validates and applies a single state-machine step. It rejects
// terminal states and undeclared transitions.
func (t *LeafIntakeTask) Transition(next domain.TaskState, op domain.OperationID) error {
	if domain.IsTerminal(t.State) {
		return &domain.DomainError{
			Code:           domain.CodeTerminalStateRejected,
			OperationID:    op,
			TaskGeneration: t.Generation,
			Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "terminal state rejects operations"}},
		}
	}
	if !domain.CanTransition(t.State, next) {
		return &domain.DomainError{
			Code:           domain.CodeTerminalStateRejected,
			OperationID:    op,
			TaskGeneration: t.Generation,
			Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "illegal state transition"}},
		}
	}
	t.State = next
	return nil
}

// StaleGeneration reports whether a supplied generation is older than the
// task's current generation.
func (t *LeafIntakeTask) StaleGeneration(gen domain.TaskGeneration) bool {
	return gen < t.Generation
}

// Service is the write-side contract for the task aggregate. Every mutation
// carries a task generation and operation id for idempotent replay.
type Service interface {
	// Create registers a new pending-lock task.
	Create(ctx context.Context, id domain.TaskID, batch domain.BatchNumber, plotID, roundID string, op domain.OperationID) (*LeafIntakeTask, error)
	// Lock atomically fixes the snapshot and acquires all resource leases.
	Lock(ctx context.Context, id domain.TaskID, op domain.OperationID, snap LockSnapshot) (*LeafIntakeTask, error)
	// Get returns the current aggregate, including coverage progress.
	Get(ctx context.Context, id domain.TaskID) (*LeafIntakeTask, error)
}

var _ Service = (*TaskService)(nil)
