package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"verdant-leaf-fixation-gate/internal/arbiter"
	"verdant-leaf-fixation-gate/internal/catalog"
	"verdant-leaf-fixation-gate/internal/device"
	"verdant-leaf-fixation-gate/internal/domain"
	"verdant-leaf-fixation-gate/internal/task"
	"verdant-leaf-fixation-gate/internal/withering"
)

// taskView is the public aggregate projection returned by GET and mutation
// endpoints.
type taskView struct {
	ID           string           `json:"id"`
	LeafBatch    string           `json:"leaf_batch"`
	GardenPlot   string           `json:"garden_plot"`
	PickingRound string           `json:"picking_round"`
	State        domain.TaskState `json:"state"`
	Generation   int64            `json:"generation"`
	Version      int64            `json:"version"`
	Coverage     coverageView     `json:"coverage"`
	Leases       []leaseView      `json:"leases"`
	Terminal     *terminalView    `json:"terminal,omitempty"`
	Blinds       []blindView      `json:"blinds,omitempty"`
}

type coverageView struct {
	Submitted int `json:"submitted"`
	Total     int `json:"total"`
}

type leaseView struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

type terminalView struct {
	Command string `json:"command"`
	At      int64  `json:"at"`
}

type blindView struct {
	Code     string `json:"code"`
	Sealed   bool   `json:"sealed"`
	Revealed bool   `json:"revealed"`
}

// ---- request DTOs ----

type createTaskRequest struct {
	ID           string `json:"id"`
	LeafBatch    string `json:"leaf_batch"`
	GardenPlot   string `json:"garden_plot"`
	PickingRound string `json:"picking_round"`
	OperationID  string `json:"operation_id"`
}

type lockRequest struct {
	OperationID    string   `json:"operation_id"`
	Generation     int64    `json:"generation,omitempty"`
	RuleDigest     string   `json:"rule_digest"`
	BasketSeals    []string `json:"basket_seals"`
	BlindCodes     []string `json:"blind_codes"`
	TendernessPts  []string `json:"tenderness_points"`
	AssayWells     []string `json:"assay_wells"`
	FixationSlots  []string `json:"fixation_slots"`
	AirBranches    []string `json:"air_branches"`
	WitheringSlots []string `json:"withering_slots"`
	ReceiptPersons []string `json:"receipt_persons"`
	ReviewPersons  []string `json:"review_persons"`
}

type receiptsRequest struct {
	OperationID string   `json:"operation_id"`
	Generation  int64    `json:"generation,omitempty"`
	Personnel   []string `json:"personnel"`
}

type opRequest struct {
	OperationID string `json:"operation_id"`
	Generation  int64  `json:"generation,omitempty"`
}

type readingsRequest struct {
	OperationID string                `json:"operation_id"`
	Generation  int64                 `json:"generation,omitempty"`
	Readings    []witheringReadingDTO `json:"readings"`
}

type witheringReadingDTO struct {
	TimePoint       int64  `json:"time_point"`
	Basket          string `json:"basket"`
	EnvHumidity     int64  `json:"env_humidity"`
	MoistureContent int64  `json:"moisture_content"`
	LeafTemperature int64  `json:"leaf_temperature"`
	WaterLoss       int64  `json:"water_loss"`
	RedLeafRatio    int64  `json:"red_leaf_ratio"`
	Supplemental    bool   `json:"supplemental,omitempty"`
}

type tendernessRequest struct {
	OperationID   string `json:"operation_id"`
	Generation    int64  `json:"generation,omitempty"`
	SingleBud     int64  `json:"single_bud"`
	OneBudOneLeaf int64  `json:"one_bud_one_leaf"`
	OldLeaf       int64  `json:"old_leaf"`
	RedLeaf       int64  `json:"red_leaf"`
	TotalSamples  int64  `json:"total_samples"`
}

type assayReadRequest struct {
	OperationID string `json:"operation_id"`
	Generation  int64  `json:"generation,omitempty"`
	Well        string `json:"well"`
	BlindCode   string `json:"blind_code"`
	Inhibition  int64  `json:"inhibition"`
}

type retestReadRequest struct {
	OperationID     string `json:"operation_id"`
	Generation      int64  `json:"generation,omitempty"`
	LeafTemperature int64  `json:"leaf_temperature"`
	MoistureContent int64  `json:"moisture_content"`
}

type rejudgmentRequest struct {
	OperationID     string   `json:"operation_id"`
	Generation      int64    `json:"generation,omitempty"`
	AffectedBaskets []string `json:"affected_baskets"`
	AffectedBlinds  []string `json:"affected_blinds"`
	AffectedSlots   []string `json:"affected_slots"`
	AffectedWells   []string `json:"affected_wells"`
}

type reviewRequest struct {
	OperationID string `json:"operation_id"`
	Generation  int64  `json:"generation,omitempty"`
	PersonnelID string `json:"personnel_id"`
	Approved    bool   `json:"approved"`
}

type terminalRequest struct {
	OperationID string `json:"operation_id"`
	Generation  int64  `json:"generation,omitempty"`
	Command     string `json:"command"`
}

type confirmFixationRequest struct {
	OperationID  string `json:"operation_id"`
	CredentialID string `json:"credential_id"`
}

// ---- handlers ----

func (a *App) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Create(r.Context(), domain.TaskID(req.ID), domain.BatchNumber(req.LeafBatch),
		req.GardenPlot, req.PickingRound, domain.OperationID(req.OperationID))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, a.buildView(r.Context(), t.ID))
}

func (a *App) handleLock(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req lockRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	snap := task.LockSnapshot{
		RuleDigest:     catalog.RuleDigest(req.RuleDigest),
		BasketSeals:    toSeals(req.BasketSeals),
		BlindCodes:     toBlinds(req.BlindCodes),
		TendernessPts:  req.TendernessPts,
		AssayWells:     req.AssayWells,
		FixationSlots:  req.FixationSlots,
		AirBranches:    req.AirBranches,
		WitheringSlots: req.WitheringSlots,
		ReceiptPersons: toPersons(req.ReceiptPersons),
		ReviewPersons:  toPersons(req.ReviewPersons),
	}
	// The snapshot's plot/round come from the task itself.
	if t, err := a.Tasks.Load(r.Context(), id); err == nil {
		snap.GardenPlot = t.GardenPlot
		snap.PickingRound = t.PickingRound
	}
	t, err := a.Tasks.Lock(r.Context(), id, domain.OperationID(req.OperationID), snap)
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, a.buildView(r.Context(), t.ID))
}

func (a *App) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	v := a.buildView(r.Context(), id)
	if v == nil {
		WriteError(w, notFoundErr())
		return
	}
	WriteJSON(w, http.StatusOK, v)
}

func (a *App) handleReceipts(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req receiptsRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	res, err := a.Tasks.SubmitReceipts(r.Context(), id, t.Generation, domain.OperationID(req.OperationID), toPersons(req.Personnel))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, res)
}

func (a *App) handleWitheringStart(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req opRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.Tasks.TransitionTask(r.Context(), id, domain.StateSlotOccupied, domain.StateWithering, domain.OperationID(req.OperationID)); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"state": string(domain.StateWithering)})
}

func (a *App) handleWitheringReadings(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req readingsRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	readings := make([]withering.WitheringReading, 0, len(req.Readings))
	for _, d := range req.Readings {
		readings = append(readings, withering.WitheringReading{
			Key:             withering.CoverageKey{TimePoint: domain.LogicalTime(d.TimePoint), Basket: domain.BasketSeal(d.Basket)},
			EnvHumidity:     domain.Fixed{Raw: d.EnvHumidity, Scale: domain.ScaleDecimal1},
			MoistureContent: domain.Fixed{Raw: d.MoistureContent, Scale: domain.ScalePerTenThousand},
			LeafTemperature: domain.Fixed{Raw: d.LeafTemperature, Scale: domain.ScaleDecimal1},
			WaterLoss:       domain.Fixed{Raw: d.WaterLoss, Scale: domain.ScalePerTenThousand},
			RedLeafRatio:    domain.Fixed{Raw: d.RedLeafRatio, Scale: domain.ScalePerTenThousand},
			Supplemental:    d.Supplemental,
		})
	}
	if err := a.Withering.SubmitReadings(r.Context(), id, t.Generation, readings); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.Tasks.TransitionTask(r.Context(), id, domain.StateWithering, domain.StateTenderness, domain.OperationID(req.OperationID)); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]int{"accepted": len(readings)})
}

func (a *App) handleTenderness(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req tendernessRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	counts := withering.TendernessCounts{
		SingleBud:     req.SingleBud,
		OneBudOneLeaf: req.OneBudOneLeaf,
		OldLeaf:       req.OldLeaf,
		RedLeaf:       req.RedLeaf,
		TotalSamples:  req.TotalSamples,
	}
	ev, err := a.Withering.SubmitTenderness(r.Context(), id, t.Generation, counts, domain.OperationID(req.OperationID))
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.Tasks.TransitionTask(r.Context(), id, domain.StateTenderness, domain.StateAssayScreening, domain.OperationID(req.OperationID)); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, ev)
}

func (a *App) handleSeal(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req opRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.Arbiter.Seal(r.Context(), id, t.Generation, domain.OperationID(req.OperationID)); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"sealed": true})
}

func (a *App) handleReveal(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req opRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	codes, err := a.Arbiter.Reveal(r.Context(), id, t.Generation, domain.OperationID(req.OperationID))
	if err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string][]domain.BlindCode{"blind_codes": codes})
}

func (a *App) handleAssaysStart(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req opRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"state": string(t.State)})
}

func (a *App) handleAssayRead(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req assayReadRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	callKey := "assay:" + req.Well + ":" + req.BlindCode
	outcome := a.runDevice(r, id, device.KindAssayReader, callKey)
	if outcome != device.OutcomeSuccess {
		WriteError(w, a.deviceRetryable(req.OperationID, t.Generation))
		return
	}
	ev := arbiter.AssayEvidence{
		Well:       req.Well,
		BlindCode:  domain.BlindCode(req.BlindCode),
		Generation: t.Generation,
		Inhibition: domain.Fixed{Raw: req.Inhibition, Scale: domain.ScalePerTenThousand},
	}
	if err := a.Arbiter.SubmitAssay(r.Context(), id, t.Generation, ev); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.Tasks.TransitionTask(r.Context(), id, domain.StateAssayScreening, domain.StateRetesting, domain.OperationID(req.OperationID)); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"state": string(domain.StateRetesting)})
}

func (a *App) handleRetestRead(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req retestReadRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	callKey := "retest:" + string(id)
	outcome := a.runDevice(r, id, device.KindMoistureMeter, callKey)
	if outcome != device.OutcomeSuccess {
		WriteError(w, a.deviceRetryable(req.OperationID, t.Generation))
		return
	}
	ev := arbiter.RetestEvidence{
		Generation:      t.Generation,
		LeafTemperature: domain.Fixed{Raw: req.LeafTemperature, Scale: domain.ScaleDecimal1},
		MoistureContent: domain.Fixed{Raw: req.MoistureContent, Scale: domain.ScalePerTenThousand},
	}
	if err := a.Arbiter.SubmitRetest(r.Context(), id, t.Generation, ev); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.Tasks.TransitionTask(r.Context(), id, domain.StateRetesting, domain.StatePendingReview, domain.OperationID(req.OperationID)); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"state": string(domain.StatePendingReview)})
}

func (a *App) handleRejudgment(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req rejudgmentRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	c := arbiter.RejudgmentCase{
		AffectedBaskets: toSeals(req.AffectedBaskets),
		AffectedBlinds:  toBlinds(req.AffectedBlinds),
		AffectedSlots:   req.AffectedSlots,
		AffectedWells:   req.AffectedWells,
	}
	if err := a.Arbiter.CreateRejudgment(r.Context(), id, c); err != nil {
		WriteError(w, err)
		return
	}
	updated, _ := a.Tasks.Load(r.Context(), id)
	WriteJSON(w, http.StatusOK, map[string]int64{"generation": int64(updated.Generation)})
}

func (a *App) handleReview(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req reviewRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkTerminal(t, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	d := arbiter.ReviewDecision{PersonnelID: domain.PersonnelID(req.PersonnelID), Approved: req.Approved, Generation: t.Generation}
	if err := a.Arbiter.SubmitReview(r.Context(), id, t.Generation, d); err != nil {
		WriteError(w, err)
		return
	}
	if n, _ := a.Arbiter.ReviewCount(r.Context(), id); n >= 2 {
		if err := a.Tasks.TransitionTask(r.Context(), id, domain.StatePendingReview, domain.StateFixationReady, domain.OperationID(req.OperationID)); err != nil {
			WriteError(w, err)
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]string{"state": string(domain.StateFixationReady)})
}

func (a *App) handleTerminal(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req terminalRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	t, err := a.Tasks.Load(r.Context(), id)
	if err != nil {
		WriteError(w, err)
		return
	}
	if err := a.checkGeneration(t, req.Generation, req.OperationID); err != nil {
		WriteError(w, err)
		return
	}
	dec, err := a.Arbiter.Terminal(r.Context(), id, t.Generation, arbiter.TerminalCommand(req.Command), domain.OperationID(req.OperationID))
	if err != nil {
		WriteError(w, err)
		return
	}
	resp := map[string]any{"command": string(dec.Command)}
	if dec.FixationCredential != nil {
		resp["fixation_credential"] = dec.FixationCredential.ID
	}
	WriteJSON(w, http.StatusOK, resp)
}

func (a *App) handleConfirmFixation(w http.ResponseWriter, r *http.Request) {
	id := taskIDFromPath(r)
	var req confirmFixationRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	if err := a.Arbiter.ConfirmFixation(r.Context(), id, req.CredentialID, domain.OperationID(req.OperationID)); err != nil {
		WriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"confirmed": true})
}

func (a *App) handleDeviceRetry(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TaskID  string `json:"task_id"`
		CallKey string `json:"call_key"`
		Kind    string `json:"kind"`
	}
	if err := decodeJSON(r, &req); err != nil {
		WriteError(w, err)
		return
	}
	kind := parseDeviceKind(req.Kind)
	outcome := a.runDevice(r, domain.TaskID(req.TaskID), kind, req.CallKey)
	if outcome != device.OutcomeSuccess {
		WriteError(w, a.deviceRetryable("", 0))
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"outcome": "success"})
}

// ---- helpers ----

func (a *App) buildView(ctx context.Context, id domain.TaskID) *taskView {
	t, err := a.Tasks.Load(ctx, id)
	if err != nil {
		return nil
	}
	v := &taskView{
		ID:           string(t.ID),
		LeafBatch:    string(t.LeafBatch),
		GardenPlot:   t.GardenPlot,
		PickingRound: t.PickingRound,
		State:        t.State,
		Generation:   int64(t.Generation),
		Version:      t.Version,
	}
	if t.Terminal != nil {
		v.Terminal = &terminalView{Command: t.Terminal.Command, At: int64(t.Terminal.At)}
	}
	if submitted, err := a.Withering.SubmittedCells(ctx, id); err == nil {
		v.Coverage.Submitted = submitted
	}
	if t.Snapshot != nil {
		if tmpl, err := a.Catalog.WitheringTemplate(ctx, t.RuleDigest); err == nil {
			v.Coverage.Total = len(tmpl.TimePoints) * len(t.Snapshot.BasketSeals)
		}
	}
	if leases, err := a.Ledger.ListLeases(ctx, id); err == nil {
		for _, l := range leases {
			v.Leases = append(v.Leases, leaseView{ResourceType: string(l.ResourceType), ResourceID: l.ResourceID})
		}
	}
	if blinds, err := a.Ledger.BlindSamples(ctx, id); err == nil {
		for _, b := range blinds {
			v.Blinds = append(v.Blinds, blindView{Code: string(b.Code), Sealed: b.Sealed, Revealed: b.Revealed})
		}
	}
	return v
}

func (a *App) checkGeneration(t *task.LeafIntakeTask, gen int64, op string) error {
	if gen != 0 && t.StaleGeneration(domain.TaskGeneration(gen)) {
		return &domain.DomainError{
			Code:           domain.CodeStaleTaskGeneration,
			OperationID:    domain.OperationID(op),
			TaskGeneration: t.Generation,
			Reasons:        []domain.Reason{{Code: domain.CodeStaleTaskGeneration, Message: "stale task generation"}},
		}
	}
	return nil
}

func (a *App) checkTerminal(t *task.LeafIntakeTask, op string) error {
	if domain.IsTerminal(t.State) {
		return &domain.DomainError{
			Code:           domain.CodeTerminalStateRejected,
			OperationID:    domain.OperationID(op),
			TaskGeneration: t.Generation,
			Reasons:        []domain.Reason{{Code: domain.CodeTerminalStateRejected, Message: "terminal state rejects operations"}},
		}
	}
	return nil
}

func parseDeviceKind(s string) device.Kind {
	switch strings.ToLower(s) {
	case "assay", "assay_reader":
		return device.KindAssayReader
	case "moisture", "moisture_meter", "retest":
		return device.KindMoistureMeter
	default:
		return device.KindProbe
	}
}

// runDevice invokes the scripted instrument, records the attempt, and returns
// the deterministic outcome.
func (a *App) runDevice(r *http.Request, taskID domain.TaskID, kind device.Kind, callKey string) device.Outcome {
	ctx := r.Context()
	seq, err := a.Withering.NextAttemptSeq(ctx, taskID, callKey)
	if err != nil {
		return device.OutcomeSuccess
	}
	res := a.Device.Call(kind, int(seq))
	_ = a.Withering.RecordAttempt(ctx, taskID, withering.DeviceAttempt{
		CallKey:    callKey,
		AttemptSeq: seq,
		DeviceKind: string(kind),
		Retryable:  res.Outcome.Retryable(),
		Succeeded:  res.Outcome == device.OutcomeSuccess,
	})
	return res.Outcome
}

func (a *App) deviceRetryable(op string, gen domain.TaskGeneration) error {
	return &domain.DomainError{
		Code:           domain.CodeDeviceRetryable,
		OperationID:    domain.OperationID(op),
		TaskGeneration: gen,
		Reasons:        []domain.Reason{{Code: domain.CodeDeviceRetryable, Message: "device call not yet successful; retry"}},
	}
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return &domain.DomainError{Code: "BAD_REQUEST", Reasons: []domain.Reason{{Code: "BAD_REQUEST", Message: "invalid JSON: " + err.Error()}}}
	}
	return nil
}

func taskIDFromPath(r *http.Request) domain.TaskID {
	// Task routes are always /api/v1/tasks/{id}[/...]; the id is the fourth
	// segment, never the trailing action segment.
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) >= 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "tasks" {
		return domain.TaskID(parts[3])
	}
	return domain.TaskID(parts[len(parts)-1])
}

func notFoundErr() error {
	return &domain.DomainError{Code: "NOT_FOUND", Reasons: []domain.Reason{{Code: "NOT_FOUND", Message: "task not found"}}}
}

func toSeals(in []string) []domain.BasketSeal {
	out := make([]domain.BasketSeal, 0, len(in))
	for _, s := range in {
		out = append(out, domain.BasketSeal(s))
	}
	return out
}

func toBlinds(in []string) []domain.BlindCode {
	out := make([]domain.BlindCode, 0, len(in))
	for _, s := range in {
		out = append(out, domain.BlindCode(s))
	}
	return out
}

func toPersons(in []string) []domain.PersonnelID {
	out := make([]domain.PersonnelID, 0, len(in))
	for _, s := range in {
		out = append(out, domain.PersonnelID(s))
	}
	return out
}
