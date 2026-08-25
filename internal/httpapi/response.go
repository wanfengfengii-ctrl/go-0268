// Package httpapi implements the Go HTTP API: JSON contracts, middleware,
// transaction boundaries, stable error responses, deterministic reason
// ordering, health checks, and the startup recovery entry point. It contains
// no standalone frontend.
package httpapi

import (
	"encoding/json"
	"net/http"

	"verdant-leaf-fixation-gate/internal/domain"
)

// ErrorResponse is the stable JSON envelope returned for every rejection. It
// always carries a code and, when available, the operation id, task
// generation, and deterministically sorted reasons.
type ErrorResponse struct {
	Code           domain.ErrorCode `json:"code"`
	OperationID    string           `json:"operation_id,omitempty"`
	TaskGeneration int64            `json:"task_generation,omitempty"`
	Reasons        []domain.Reason  `json:"reasons"`
}

// WriteError renders a *domain.DomainError as the stable error envelope with
// sorted reasons and an appropriate HTTP status.
func WriteError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var resp ErrorResponse
	if de, ok := err.(*domain.DomainError); ok {
		status = statusForCode(de.Code)
		resp = ErrorResponse{
			Code:           de.Code,
			OperationID:    string(de.OperationID),
			TaskGeneration: int64(de.TaskGeneration),
			Reasons:        de.SortedReasons(),
		}
	} else {
		resp = ErrorResponse{
			Code:    "INTERNAL_ERROR",
			Reasons: []domain.Reason{{Code: "INTERNAL_ERROR", Message: err.Error()}},
		}
	}
	WriteJSON(w, status, resp)
}

// statusForCode maps the documented stable codes to HTTP statuses.
func statusForCode(c domain.ErrorCode) int {
	switch c {
	case domain.CodeOperationContentConflict,
		domain.CodeStaleTaskGeneration,
		domain.CodeResourceLeaseConflict,
		domain.CodeBlindCodeConflict,
		domain.CodeBlindCodeEarlyReveal,
		domain.CodeDuplicateBasketSeal,
		domain.CodeCoverageDuplicate:
		return http.StatusConflict
	case domain.CodeTerminalStateRejected,
		domain.CodePlotRoundMismatch,
		domain.CodeStaleRuleDigest,
		domain.CodeRoleOverlap,
		domain.CodeRejudgmentGenerationConflict,
		domain.CodeCountNotConserved,
		domain.CodeCoverageMissing,
		domain.CodeThresholdBoundaryConflict,
		domain.CodeFixedPointOverflow:
		return http.StatusUnprocessableEntity
	case domain.CodeDeviceRetryable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusBadRequest
	}
}

// WriteJSON marshals v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
