package httpapi

import (
	"context"
	"errors"
	"sync"
	"testing"

	"verdant-leaf-fixation-gate/internal/arbiter"
	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/store"
	"verdant-leaf-fixation-gate/internal/task"
	"verdant-leaf-fixation-gate/internal/withering"
)

func newTestApp(t *testing.T, scripts map[device.Kind]*device.Script) *App {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	app := NewApp(st, device.NewAdapter(scripts))
	if err := app.SeedDemo(context.Background()); err != nil {
		t.Fatalf("seed demo: %v", err)
	}
	return app
}

func lockSnapshot() task.LockSnapshot {
	return task.LockSnapshot{
		GardenPlot:     "plot-east",
		PickingRound:   "round-spring",
		RuleDigest:     catalog.RuleDigest("rule-demo-v1"),
		BasketSeals:    []domain.BasketSeal{"basket-1", "basket-2"},
		BlindCodes:     []domain.BlindCode{"blind-1", "blind-2", "blind-3"},
		TendernessPts:  []string{"tp-1"},
		AssayWells:     []string{"well-1"},
		FixationSlots:  []string{"fix-1"},
		AirBranches:    []string{"air-a"},
		WitheringSlots: []string{"slot-1"},
		ReceiptPersons: []domain.PersonnelID{"recv-a", "recv-b", "recv-c"},
		ReviewPersons:  []domain.PersonnelID{"rev-x", "rev-y", "rev-z"},
	}
}

func createAndLock(t *testing.T, app *App, id domain.TaskID, batch domain.BatchNumber) *task.LeafIntakeTask {
	t.Helper()
	ctx := context.Background()
	if _, err := app.Tasks.Create(ctx, id, batch, "plot-east", "round-spring", "op-create"); err != nil {
		t.Fatalf("create: %v", err)
	}
	locked, err := app.Tasks.Lock(ctx, id, "op-lock", lockSnapshot())
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	return locked
}

func TestLockSnapshotImmutability(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	locked := createAndLock(t, app, "t-1", "batch-1")
	if locked.Snapshot == nil {
		t.Fatal("expected snapshot after lock")
	}
	if locked.Snapshot.RuleDigest != "rule-demo-v1" {
		t.Fatalf("unexpected digest %q", locked.Snapshot.RuleDigest)
	}
	if len(locked.Snapshot.BasketSeals) != 2 || len(locked.Snapshot.BlindCodes) != 3 {
		t.Fatalf("unexpected snapshot sizes")
	}
	if locked.State != domain.StatePendingReceipt {
		t.Fatalf("expected pending_receipt, got %s", locked.State)
	}
	// Resource summary must be visible.
	leases, _ := app.Ledger.ListLeases(ctx, "t-1")
	if len(leases) == 0 {
		t.Fatal("expected leases after lock")
	}
}

func TestLockRejectsStaleDigest(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	if _, err := app.Tasks.Create(ctx, "t-1", "batch-1", "plot-east", "round-spring", "op"); err != nil {
		t.Fatalf("create: %v", err)
	}
	snap := lockSnapshot()
	snap.RuleDigest = "stale-digest"
	_, err := app.Tasks.Lock(ctx, "t-1", "op-lock", snap)
	assertCode(t, err, domain.CodeStaleRuleDigest)
	// No leases must remain.
	if leases, _ := app.Ledger.ListLeases(ctx, "t-1"); len(leases) != 0 {
		t.Fatal("expected no partial leases after stale digest")
	}
}

func TestLockRejectsPlotRoundMismatch(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	if _, err := app.Tasks.Create(ctx, "t-1", "batch-1", "plot-east", "round-nonexistent", "op"); err != nil {
		t.Fatalf("create: %v", err)
	}
	snap := lockSnapshot()
	snap.RuleDigest = "rule-demo-v1"
	_, err := app.Tasks.Lock(ctx, "t-1", "op-lock", snap)
	assertCode(t, err, domain.CodePlotRoundMismatch)
}

func TestConcurrentLockSingleWinner(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	// Two tasks with distinct batches compete for the same resource set; only
	// one acquires the leases.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := domain.TaskID([]string{"c-1", "c-2"}[i])
			batch := domain.BatchNumber([]string{"batch-shared-1", "batch-shared-2"}[i])
			if _, err := app.Tasks.Create(ctx, id, batch, "plot-east", "round-spring", "op"); err != nil {
				errs[i] = err
				return
			}
			_, errs[i] = app.Tasks.Lock(ctx, id, "op-lock", lockSnapshot())
		}(i)
	}
	wg.Wait()
	var success int
	for _, err := range errs {
		if err == nil {
			success++
		} else {
			if !isCode(err, domain.CodeResourceLeaseConflict) && !isCode(err, domain.CodeDuplicateBasketSeal) {
				t.Fatalf("unexpected loser error: %v", err)
			}
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly one winner, got %d", success)
	}
}

func isCode(err error, want domain.ErrorCode) bool {
	var de *domain.DomainError
	if !errors.As(err, &de) {
		return false
	}
	return de.Code == want
}

func TestIdempotentReceipt(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	createAndLock(t, app, "t-1", "batch-1")
	persons := []domain.PersonnelID{"recv-a", "recv-b"}
	res1, err := app.Tasks.SubmitReceipts(ctx, "t-1", 1, "op-receipt", persons)
	if err != nil {
		t.Fatalf("first receipt: %v", err)
	}
	res2, err := app.Tasks.SubmitReceipts(ctx, "t-1", 1, "op-receipt", persons)
	if err != nil {
		t.Fatalf("replayed receipt: %v", err)
	}
	if res1.State != res2.State {
		t.Fatalf("expected identical replay, got %s vs %s", res1.State, res2.State)
	}
	// Different content with the same operation id must conflict.
	_, err = app.Tasks.SubmitReceipts(ctx, "t-1", 1, "op-receipt", []domain.PersonnelID{"recv-a", "recv-c"})
	assertCode(t, err, domain.CodeOperationContentConflict)
}

func TestBlindEarlyRevealRejected(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	createAndLock(t, app, "t-1", "batch-1")
	_, err := app.Arbiter.Reveal(ctx, "t-1", 1, "op-reveal")
	assertCode(t, err, domain.CodeBlindCodeEarlyReveal)
}

func TestCoverageMatrixValidation(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	createAndLock(t, app, "t-1", "batch-1")
	// Missing one cell (5 instead of 6) must be rejected.
	_, err := app.Tasks.SubmitReceipts(ctx, "t-1", 1, "op-receipt", []domain.PersonnelID{"recv-a", "recv-b"})
	if err != nil {
		t.Fatalf("receipt: %v", err)
	}
	_ = app.Tasks.TransitionTask(ctx, "t-1", domain.StateSlotOccupied, domain.StateWithering, "op-start")

	partial := coverageReadings()[:5]
	if err := app.Withering.SubmitReadings(ctx, "t-1", 1, partial); err == nil {
		t.Fatal("expected missing coverage rejection")
	} else {
		assertCode(t, err, domain.CodeCoverageMissing)
	}
	// Full matrix succeeds atomically.
	if err := app.Withering.SubmitReadings(ctx, "t-1", 1, coverageReadings()); err != nil {
		t.Fatalf("full coverage: %v", err)
	}
	// Duplicate cell on a second submission must fail.
	if err := app.Withering.SubmitReadings(ctx, "t-1", 1, coverageReadings()); err == nil {
		t.Fatal("expected duplicate coverage rejection")
	}
}

func TestTendernessConservation(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	createAndLock(t, app, "t-1", "batch-1")
	bad := withering.TendernessCounts{SingleBud: 10, OneBudOneLeaf: 5, OldLeaf: 1, RedLeaf: 1, TotalSamples: 18}
	if _, err := app.Withering.SubmitTenderness(ctx, "t-1", 1, bad, "op-ten"); err == nil {
		t.Fatal("expected conservation rejection")
	} else {
		assertCode(t, err, domain.CodeCountNotConserved)
	}
	// Valid counts succeed.
	good := withering.TendernessCounts{SingleBud: 10, OneBudOneLeaf: 5, OldLeaf: 2, RedLeaf: 1, TotalSamples: 18}
	if _, err := app.Withering.SubmitTenderness(ctx, "t-1", 1, good, "op-ten"); err != nil {
		t.Fatalf("valid tenderness: %v", err)
	}
}

func TestDeviceScriptRetry(t *testing.T) {
	app := newTestApp(t, device.DefaultScripts())
	// Default scripts reject, disconnect, timeout, then succeed, keyed by the
	// 1-based attempt number.
	for attempt := 1; attempt <= 4; attempt++ {
		out := app.Device.Call(device.KindProbe, attempt)
		if attempt < 4 {
			if out.Outcome == device.OutcomeSuccess {
				t.Fatalf("attempt %d unexpectedly succeeded", attempt)
			}
		} else if out.Outcome != device.OutcomeSuccess {
			t.Fatalf("attempt %d should succeed, got %s", attempt, out.Outcome)
		}
	}
}

func TestTerminalBarrierSingleDecision(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	driveToFixationReady(t, app, "t-1", "batch-1")

	var wg sync.WaitGroup
	results := make([]arbiter.TerminalDecision, 0, 3)
	var mu sync.Mutex
	for _, cmd := range []arbiter.TerminalCommand{arbiter.CommandRelease, arbiter.CommandIsolate, arbiter.CommandCancel} {
		wg.Add(1)
		go func(c arbiter.TerminalCommand) {
			defer wg.Done()
			dec, err := app.Arbiter.Terminal(ctx, "t-1", 1, c, domain.OperationID("op-term-"+string(c)))
			if err == nil {
				mu.Lock()
				results = append(results, dec)
				mu.Unlock()
			}
		}(cmd)
	}
	wg.Wait()
	if len(results) != 1 {
		t.Fatalf("expected exactly one terminal decision, got %d", len(results))
	}
}

func TestTerminalStateRejectsFurtherOps(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	driveToFixationReady(t, app, "t-1", "batch-1")
	if _, err := app.Arbiter.Terminal(ctx, "t-1", 1, arbiter.CommandRelease, "op-term"); err != nil {
		t.Fatalf("terminal: %v", err)
	}
	// Further normal operations must be rejected.
	if err := app.Tasks.TransitionTask(ctx, "t-1", domain.StateFixed, domain.StateCancelled, "op-x"); err == nil {
		t.Fatal("expected terminal rejection")
	} else {
		assertCode(t, err, domain.CodeTerminalStateRejected)
	}
}

func TestRoleOverlapRejected(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	ctx := context.Background()
	driveToFixationReady(t, app, "t-1", "batch-1")
	// A receipt person cannot be a reviewer.
	err := app.Arbiter.SubmitReview(ctx, "t-1", 1, arbiter.ReviewDecision{PersonnelID: "recv-a", Approved: true, Generation: 1})
	assertCode(t, err, domain.CodeRoleOverlap)
}

// ---- helpers ----

func coverageReadings() []withering.WitheringReading {
	var out []withering.WitheringReading
	for _, tp := range []int64{1, 2, 3} {
		for _, b := range []string{"basket-1", "basket-2"} {
			out = append(out, withering.WitheringReading{
				Key:             withering.CoverageKey{TimePoint: domain.LogicalTime(tp), Basket: domain.BasketSeal(b)},
				EnvHumidity:     domain.Fixed{Raw: 500, Scale: domain.ScaleDecimal1},
				MoistureContent: domain.Fixed{Raw: 7000, Scale: domain.ScalePerTenThousand},
				LeafTemperature: domain.Fixed{Raw: 250, Scale: domain.ScaleDecimal1},
				WaterLoss:       domain.Fixed{Raw: 3000, Scale: domain.ScalePerTenThousand},
				RedLeafRatio:    domain.Fixed{Raw: 100, Scale: domain.ScalePerTenThousand},
			})
		}
	}
	return out
}

// driveToFixationReady runs the full happy path up to the fixation-ready state.
func driveToFixationReady(t *testing.T, app *App, id domain.TaskID, batch domain.BatchNumber) {
	t.Helper()
	ctx := context.Background()
	createAndLock(t, app, id, batch)
	if _, err := app.Tasks.SubmitReceipts(ctx, id, 1, "op-receipt", []domain.PersonnelID{"recv-a", "recv-b"}); err != nil {
		t.Fatalf("receipt: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, id, domain.StateSlotOccupied, domain.StateWithering, "op-start"); err != nil {
		t.Fatalf("start withering: %v", err)
	}
	if err := app.Withering.SubmitReadings(ctx, id, 1, coverageReadings()); err != nil {
		t.Fatalf("readings: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, id, domain.StateWithering, domain.StateTenderness, "op-r"); err != nil {
		t.Fatalf("to tenderness: %v", err)
	}
	counts := withering.TendernessCounts{SingleBud: 10, OneBudOneLeaf: 5, OldLeaf: 2, RedLeaf: 1, TotalSamples: 18}
	if _, err := app.Withering.SubmitTenderness(ctx, id, 1, counts, "op-ten"); err != nil {
		t.Fatalf("tenderness: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, id, domain.StateTenderness, domain.StateAssayScreening, "op-t"); err != nil {
		t.Fatalf("to assay: %v", err)
	}
	if err := app.Arbiter.Seal(ctx, id, 1, "op-seal"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := app.Arbiter.Reveal(ctx, id, 1, "op-reveal"); err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if err := app.Arbiter.SubmitAssay(ctx, id, 1, arbiter.AssayEvidence{
		Well: "well-1", BlindCode: "blind-1", Generation: 1,
		Inhibition: domain.Fixed{Raw: 100, Scale: domain.ScalePerTenThousand},
	}); err != nil {
		t.Fatalf("assay: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, id, domain.StateAssayScreening, domain.StateRetesting, "op-a"); err != nil {
		t.Fatalf("to retest: %v", err)
	}
	if err := app.Arbiter.SubmitRetest(ctx, id, 1, arbiter.RetestEvidence{
		Generation: 1, LeafTemperature: domain.Fixed{Raw: 250, Scale: domain.ScaleDecimal1},
		MoistureContent: domain.Fixed{Raw: 7000, Scale: domain.ScalePerTenThousand},
	}); err != nil {
		t.Fatalf("retest: %v", err)
	}
	if err := app.Tasks.TransitionTask(ctx, id, domain.StateRetesting, domain.StatePendingReview, "op-rt"); err != nil {
		t.Fatalf("to review: %v", err)
	}
	for _, p := range []domain.PersonnelID{"rev-x", "rev-y"} {
		if err := app.Arbiter.SubmitReview(ctx, id, 1, arbiter.ReviewDecision{PersonnelID: p, Approved: true, Generation: 1}); err != nil {
			t.Fatalf("review: %v", err)
		}
	}
	if err := app.Tasks.TransitionTask(ctx, id, domain.StatePendingReview, domain.StateFixationReady, "op-rev"); err != nil {
		t.Fatalf("to fixation ready: %v", err)
	}
}

func assertCode(t *testing.T, err error, want domain.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", want)
	}
	var de *domain.DomainError
	if !errors.As(err, &de) {
		t.Fatalf("expected DomainError, got %T: %v", err, err)
	}
	if de.Code != want {
		t.Fatalf("expected code %s, got %s", want, de.Code)
	}
}

func TestHTTPCreateLockGetFlow(t *testing.T) {
	app := newTestApp(t, device.ImmediateScripts())
	srv := NewServerWithApp(app)
	ts := newTestServer(t, srv)

	create := postJSON(t, ts, "/api/v1/tasks", `{"id":"h-1","leaf_batch":"batch-h","garden_plot":"plot-east","picking_round":"round-spring","operation_id":"op-create"}`)
	if !contains(create, `"id":"h-1"`) {
		t.Fatalf("create response: %s", create)
	}

	lock := postJSON(t, ts, "/api/v1/tasks/h-1/lock", `{"operation_id":"op-lock","rule_digest":"rule-demo-v1","basket_seals":["basket-1","basket-2"],"blind_codes":["blind-1","blind-2","blind-3"],"tenderness_points":["tp-1"],"assay_wells":["well-1"],"fixation_slots":["fix-1"],"air_branches":["air-a"],"withering_slots":["slot-1"],"receipt_persons":["recv-a","recv-b"],"review_persons":["rev-x","rev-y"]}`)
	if !contains(lock, `"state":"pending_receipt"`) {
		t.Fatalf("lock response: %s", lock)
	}

	get := getJSON(t, ts, "/api/v1/tasks/h-1")
	if !contains(get, `"generation":1`) || !contains(get, `"pending_receipt"`) {
		t.Fatalf("get response: %s", get)
	}
}
