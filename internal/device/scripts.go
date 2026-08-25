package device

// DefaultScripts returns the deterministic scripts used for the demo and
// integration flow. Each instrument rejects, disconnects, times out, and then
// succeeds in a fixed order so that acceptance scenario 9 is reproducible.
func DefaultScripts() map[Kind]*Script {
	failing := NewScript(
		OutcomeReject,
		OutcomeDisconnect,
		OutcomeTimeout,
		OutcomeSuccess,
	)
	return map[Kind]*Script{
		KindProbe:         failing,
		KindAssayReader:   failing,
		KindMoistureMeter: failing,
	}
}

// ImmediateScripts returns scripts that succeed on the first attempt, used by
// tests that need to progress without retries.
func ImmediateScripts() map[Kind]*Script {
	return map[Kind]*Script{
		KindProbe:         NewScript(OutcomeSuccess),
		KindAssayReader:   NewScript(OutcomeSuccess),
		KindMoistureMeter: NewScript(OutcomeSuccess),
	}
}
