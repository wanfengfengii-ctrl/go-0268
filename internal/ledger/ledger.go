// Package ledger implements the basket-sample and resource-occupancy ledger:
// one-time latches for batch numbers, basket seals, and blind codes, plus
// exclusive interval leases for slots, air branches, fixation slots, and
// assay wells. It owns re-slotting, renewal, release, and restart recovery.
package ledger

import (
	"context"

	"verdant-leaf-fixation-gate/internal/domain"
)

// ResourceType enumerates the four resource classes subject to interval
// leasing.
type ResourceType string

const (
	ResourceWitheringSlot ResourceType = "withering_slot"
	ResourceAirBranch     ResourceType = "air_branch"
	ResourceFixationSlot  ResourceType = "fixation_slot"
	ResourceAssayWell     ResourceType = "assay_well"
)

// BasketSample ties a one-time basket seal to an open task.
type BasketSample struct {
	Seal   domain.BasketSeal
	TaskID domain.TaskID
}

// BlindSample holds a blind code with its sealed/revealed state. A blind code
// must not be revealed before sealing.
type BlindSample struct {
	Code       domain.BlindCode
	TaskID     domain.TaskID
	Commitment string
	Sealed     bool
	Revealed   bool
}

// TendernessPoint is a tenderness sampling position locked for a task.
type TendernessPoint struct {
	ID     string
	TaskID domain.TaskID
}

// ResourceLease records an exclusive interval lease for one resource.
type ResourceLease struct {
	ResourceType  ResourceType
	ResourceID    string
	TaskID        domain.TaskID
	Generation    domain.TaskGeneration
	Version       int64
	Released      bool
	ReleaseReason string
}

// Ledger is the persistence-side contract for latches and leases. A batch
// acquisition sorts requests by resource kind and identifier and must leave
// no partial leases on any conflict.
type Ledger interface {
	// AcquireBatch atomically acquires all requested leases for a task.
	AcquireBatch(ctx context.Context, taskID domain.TaskID, gen domain.TaskGeneration, leases []ResourceLease) error
	// ReserveBatch latches batch number, basket seals, and blind codes.
	ReserveBatch(ctx context.Context, taskID domain.TaskID, batch domain.BatchNumber, seals []domain.BasketSeal, blinds []domain.BlindCode) error
	// Release releases the leases held by a task with an explicit reason.
	Release(ctx context.Context, taskID domain.TaskID, reason string) error
	// RebuildLeases rebuilds the valid lease view for open tasks after restart.
	RebuildLeases(ctx context.Context) error
}

var _ Ledger = (*Service)(nil)
