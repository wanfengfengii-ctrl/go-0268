// Package device models the scripted external instruments (probes, assay
// readers, and moisture meters) that the service calls. Each instrument is
// driven by a deterministic outcome script so that reject, disconnect,
// timeout, malformed, and eventual success are reproducible across restarts.
package device

// Outcome is the deterministic result of one instrument invocation.
type Outcome string

const (
	OutcomeSuccess    Outcome = "success"
	OutcomeReject     Outcome = "reject"
	OutcomeDisconnect Outcome = "disconnect"
	OutcomeTimeout    Outcome = "timeout"
	OutcomeMalformed  Outcome = "malformed"
)

// Kind identifies the instrument class.
type Kind string

const (
	KindProbe         Kind = "probe"
	KindAssayReader   Kind = "assay_reader"
	KindMoistureMeter Kind = "moisture_meter"
)

// Retryable reports whether an outcome permits a later retry.
func (o Outcome) Retryable() bool {
	switch o {
	case OutcomeSuccess, OutcomeMalformed:
		return false
	default:
		return true
	}
}

// Script is a fixed, ordered list of outcomes. The outcome for attempt n is
// steps[(n-1) % len(steps)], making the sequence restart-safe: a given attempt
// number always yields the same outcome. Because the result depends only on
// the attempt number — not on how many times this script has been invoked or
// which other call keys have drawn from it — concurrent batches are isolated.
// Each (task, call key) tracks its own 1-based attempt sequence in the ledger,
// so one batch's retries can never advance another batch's outcome.
type Script struct {
	steps []Outcome
}

// NewScript builds a script from an ordered outcome list.
func NewScript(steps ...Outcome) *Script {
	return &Script{steps: steps}
}

// Outcome returns the deterministic outcome for a 1-based attempt number.
// It is a pure function of the attempt number, so two batches that each pass
// attempt 1 land on steps[0] regardless of how many times this script has been
// invoked elsewhere — concurrent batches never advance each other's outcomes.
func (s *Script) Outcome(attempt int) Outcome {
	if len(s.steps) == 0 {
		return OutcomeSuccess
	}
	if attempt < 1 {
		attempt = 1
	}
	idx := (attempt - 1) % len(s.steps)
	return s.steps[idx]
}

// Result is one instrument invocation result.
type Result struct {
	Outcome Outcome
	// Reading carries a parsed payload on success.
	Reading map[string]any
}

// Adapter routes calls to per-kind scripts.
type Adapter struct {
	scripts map[Kind]*Script
}

// NewAdapter builds an adapter with the given per-kind scripts.
func NewAdapter(scripts map[Kind]*Script) *Adapter {
	return &Adapter{scripts: scripts}
}

// Call invokes the instrument for a kind and attempt, returning the
// deterministic outcome. A missing script defaults to immediate success.
func (a *Adapter) Call(kind Kind, attempt int) Result {
	sc, ok := a.scripts[kind]
	if !ok {
		return Result{Outcome: OutcomeSuccess, Reading: map[string]any{}}
	}
	out := sc.Outcome(attempt)
	if out != OutcomeSuccess {
		return Result{Outcome: out}
	}
	return Result{Outcome: out, Reading: map[string]any{"value": attempt}}
}
