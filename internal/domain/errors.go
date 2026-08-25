package domain

import (
	"sort"
	"strings"
)

// ErrorCode is a stable, machine-readable rejection code surfaced in every
// structured error response.
type ErrorCode string

const (
	CodePlotRoundMismatch            ErrorCode = "PLOT_ROUND_MISMATCH"
	CodeStaleRuleDigest              ErrorCode = "STALE_RULE_DIGEST"
	CodeDuplicateBasketSeal          ErrorCode = "DUPLICATE_BASKET_SEAL"
	CodeBlindCodeConflict            ErrorCode = "BLIND_CODE_CONFLICT"
	CodeBlindCodeEarlyReveal         ErrorCode = "BLIND_CODE_EARLY_REVEAL"
	CodeResourceLeaseConflict        ErrorCode = "RESOURCE_LEASE_CONFLICT"
	CodeCoverageMissing              ErrorCode = "COVERAGE_MISSING"
	CodeCoverageDuplicate            ErrorCode = "COVERAGE_DUPLICATE"
	CodeCountNotConserved            ErrorCode = "COUNT_NOT_CONSERVED"
	CodeFixedPointOverflow           ErrorCode = "FIXED_POINT_OVERFLOW"
	CodeThresholdBoundaryConflict    ErrorCode = "THRESHOLD_BOUNDARY_CONFLICT"
	CodeDeviceRetryable              ErrorCode = "DEVICE_RETRYABLE"
	CodeRejudgmentGenerationConflict ErrorCode = "REJUDGMENT_GENERATION_CONFLICT"
	CodeRoleOverlap                  ErrorCode = "ROLE_OVERLAP"
	CodeOperationContentConflict     ErrorCode = "OPERATION_CONTENT_CONFLICT"
	CodeStaleTaskGeneration          ErrorCode = "STALE_TASK_GENERATION"
	CodeTerminalStateRejected        ErrorCode = "TERMINAL_STATE_REJECTED"
)

// Reason is a single structured rejection cause. Its sort keys follow the
// documented deterministic order: garden plot, leaf batch, basket seal,
// withering slot, then assay well.
type Reason struct {
	Code          ErrorCode `json:"code"`
	Message       string    `json:"message"`
	GardenPlot    string    `json:"garden_plot,omitempty"`
	LeafBatch     string    `json:"leaf_batch,omitempty"`
	BasketSeal    string    `json:"basket_seal,omitempty"`
	WitheringSlot string    `json:"withering_slot,omitempty"`
	AssayWell     string    `json:"assay_well,omitempty"`
}

// DomainError is a structured rejection carrying a stable code, the offending
// operation and generation, and a deterministically sortable set of reasons.
type DomainError struct {
	Code           ErrorCode
	OperationID    OperationID
	TaskGeneration TaskGeneration
	Reasons        []Reason
}

func (e *DomainError) Error() string {
	parts := make([]string, 0, len(e.Reasons)+1)
	parts = append(parts, string(e.Code))
	for _, r := range e.Reasons {
		parts = append(parts, r.Message)
	}
	return strings.Join(parts, ": ")
}

// NewError builds a single-cause domain error with no sort keys.
func NewError(code ErrorCode, op OperationID, gen TaskGeneration, msg string) *DomainError {
	return &DomainError{
		Code:           code,
		OperationID:    op,
		TaskGeneration: gen,
		Reasons:        []Reason{{Code: code, Message: msg}},
	}
}

// SortedReasons returns a stable, deterministically ordered copy of the
// reasons using the documented comparator.
func (e *DomainError) SortedReasons() []Reason {
	out := append([]Reason(nil), e.Reasons...)
	sort.SliceStable(out, func(i, j int) bool { return reasonLess(out[i], out[j]) })
	return out
}

// reasonLess implements the deterministic reason comparator: reasons are
// ordered by garden plot, leaf batch, basket seal, withering slot, assay
// well, and finally error code.
func reasonLess(a, b Reason) bool {
	keys := [](func(Reason) string){
		func(r Reason) string { return r.GardenPlot },
		func(r Reason) string { return r.LeafBatch },
		func(r Reason) string { return r.BasketSeal },
		func(r Reason) string { return r.WitheringSlot },
		func(r Reason) string { return r.AssayWell },
		func(r Reason) string { return string(r.Code) },
	}
	for _, key := range keys {
		ka, kb := key(a), key(b)
		if ka != kb {
			return ka < kb
		}
	}
	return false
}

func (c ErrorCode) String() string { return string(c) }

var _ error = (*DomainError)(nil)
