// Package arbiter implements the pesticide re-judgment and terminal arbiter:
// sample sealing and blind reveal, assay and physical evidence version
// chains, the single active re-judgment generation, independent review, and
// the unique fixation credential or other terminal outcome.
package arbiter

import (
	"context"

	"verdant-leaf-fixation-gate/internal/domain"
)

// TerminalCommand enumerates the three commands competing for the terminal
// barrier.
type TerminalCommand string

const (
	CommandRelease TerminalCommand = "release"
	CommandIsolate TerminalCommand = "isolate"
	CommandCancel  TerminalCommand = "cancel"
)

// AssayEvidence is an immutable pesticide screening reading.
type AssayEvidence struct {
	Version    int64
	Well       string
	BlindCode  domain.BlindCode
	Generation domain.TaskGeneration
	Inhibition domain.Fixed
	Digest     string
}

// RetestEvidence is an immutable moisture/temperature retest reading.
type RetestEvidence struct {
	Version         int64
	Generation      domain.TaskGeneration
	LeafTemperature domain.Fixed
	MoistureContent domain.Fixed
	Digest          string
}

// RejudgmentCase is the single active re-judgment for the current generation.
type RejudgmentCase struct {
	Generation      domain.TaskGeneration
	PrevGeneration  *domain.TaskGeneration
	AffectedBaskets []domain.BasketSeal
	AffectedBlinds  []domain.BlindCode
	AffectedSlots   []string
	AffectedWells   []string
}

// ReviewDecision is one independent reviewer's signed conclusion.
type ReviewDecision struct {
	PersonnelID domain.PersonnelID
	Approved    bool
	Generation  domain.TaskGeneration
}

// FixationCredential is the unique kill-green (杀青) credential. It can only
// be generated once per task.
type FixationCredential struct {
	ID       string
	TaskID   domain.TaskID
	IssuedAt domain.LogicalTime
}

// TerminalDecision is the single compare-and-swap terminal outcome.
type TerminalDecision struct {
	Command            TerminalCommand
	Operation          domain.OperationID
	FixationCredential *FixationCredential
}

// Arbiter is the persistence-side contract for sample sealing/reveal, assay
// and retest evidence, re-judgment, review, and the terminal barrier.
type Arbiter interface {
	Seal(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, op domain.OperationID) error
	Reveal(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, op domain.OperationID) ([]domain.BlindCode, error)
	SubmitAssay(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, e AssayEvidence) error
	SubmitRetest(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, e RetestEvidence) error
	CreateRejudgment(ctx context.Context, taskID domain.TaskID, c RejudgmentCase) error
	SubmitReview(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, d ReviewDecision) error
	// Terminal performs the single-writer compare-and-swap to settle the task.
	Terminal(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, cmd TerminalCommand, op domain.OperationID) (TerminalDecision, error)
	// ConfirmFixation confirms the unique fixation credential as completed.
	ConfirmFixation(ctx context.Context, taskID domain.TaskID, credentialID string, op domain.OperationID) error
}

var _ Arbiter = (*Service)(nil)
