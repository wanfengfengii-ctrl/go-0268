package device

import (
	"sync"
	"testing"
)

func TestModel_DeviceOutcomeDependsOnlyOnAttemptSequence(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{
			name: "default_order",
			run: func(t *testing.T) {
				adapter := NewAdapter(DefaultScripts())
				want := []Outcome{OutcomeReject, OutcomeDisconnect, OutcomeTimeout, OutcomeSuccess}
				for i, expected := range want {
					if got := adapter.Call(KindAssayReader, i+1).Outcome; got != expected {
						t.Fatalf("attempt %d: got %q, want %q", i+1, got, expected)
					}
				}
			},
		},
		{
			name: "another_call_does_not_inherit_progress",
			run: func(t *testing.T) {
				adapter := NewAdapter(DefaultScripts())
				for attempt := 1; attempt <= 3; attempt++ {
					adapter.Call(KindAssayReader, attempt)
				}
				if got := adapter.Call(KindAssayReader, 1).Outcome; got != OutcomeReject {
					t.Fatalf("independent call at attempt 1 got %q, want %q", got, OutcomeReject)
				}
			},
		},
		{
			name: "other_device_kinds_do_not_advance_the_script",
			run: func(t *testing.T) {
				adapter := NewAdapter(DefaultScripts())
				adapter.Call(KindProbe, 1)
				adapter.Call(KindMoistureMeter, 2)
				if got := adapter.Call(KindAssayReader, 1).Outcome; got != OutcomeReject {
					t.Fatalf("assay attempt 1 got %q after other device calls, want %q", got, OutcomeReject)
				}
			},
		},
		{
			name: "same_persisted_sequence_replays_after_restart",
			run: func(t *testing.T) {
				beforeRestart := NewAdapter(DefaultScripts()).Call(KindAssayReader, 3).Outcome
				afterRestart := NewAdapter(DefaultScripts()).Call(KindAssayReader, 3).Outcome
				if beforeRestart != OutcomeTimeout || afterRestart != beforeRestart {
					t.Fatalf("attempt 3 replay: before restart %q, after restart %q, want %q", beforeRestart, afterRestart, OutcomeTimeout)
				}
			},
		},
		{
			name: "concurrent_calls_do_not_compete_for_a_cursor",
			run: func(t *testing.T) {
				adapter := NewAdapter(DefaultScripts())
				const calls = 32
				got := make([]Outcome, calls)
				var wg sync.WaitGroup
				for i := range got {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						got[i] = adapter.Call(KindAssayReader, 1).Outcome
					}(i)
				}
				wg.Wait()
				for i, outcome := range got {
					if outcome != OutcomeReject {
						t.Fatalf("concurrent attempt 1 call %d got %q, want %q", i, outcome, OutcomeReject)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}
