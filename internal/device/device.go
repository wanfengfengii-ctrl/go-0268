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
// number always yields the same outcome.
type Script struct {
	steps []Outcome
	next  int
}

// NewScript builds a script from an ordered outcome list.
func NewScript(steps ...Outcome) *Script {
	return &Script{steps: steps}
}

// Outcome returns the deterministic outcome for a 1-based attempt number.
func (s *Script) Outcome(attempt int) Outcome {
	if len(s.steps) == 0 {
		return OutcomeSuccess
	}
	idx := s.next % len(s.steps)
	s.next++
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
