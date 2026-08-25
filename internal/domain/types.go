// Package domain holds the stable, shared domain types, invariants, and
// value semantics that every business package depends on. It deliberately
// carries no persistence or transport concerns.
package domain

// TaskID identifies a single leaf intake task aggregate.
type TaskID string

// BatchNumber is the globally unique fresh-leaf batch number that forms the
// primary aggregation boundary together with basket seals.
type BatchNumber string

// BasketSeal is a one-time latch identifying a physical tea basket.
type BasketSeal string

// BlindCode is the concealed sample code. It must not be revealed before the
// triplicate sample is sealed.
type BlindCode string

// OperationID identifies a client write operation for idempotent replay.
type OperationID string

// TaskGeneration is the monotonically increasing generation (代次) of a task.
type TaskGeneration int64

// LogicalTime is the persistent logical clock value used to order operations
// and recover pending work after restart.
type LogicalTime int64

// PersonnelID identifies a qualified worker.
type PersonnelID string
