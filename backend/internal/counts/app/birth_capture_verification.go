package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
)

// BIRTH REPORT → VERIFIER (maintainer decisions 4-6, 2026-09-16; per-slot correction the same
// day). Birth is verified PER RECORDING: the Add birth form's proofs are ONE birth_evidence item
// PER FORM PROOF SLOT (ref_type birth_capture, ref_id birth_event_id for grouping, the slot and
// its current proof in the item key), born from counts.birth.reported -- the event the submit
// transaction writes beside the request and its canonical children. Each item carries only that
// slot's proof plus the form's answers as context rows. A report with answers and NO media never
// creates an empty verifier item. A verdict lands on that slot's review (fenced on the slot's
// current ref) and the approval row's capture_review_status is the rollup.
//
// Decision 5: a REJECT of one slot opens a "Re-shoot report proof" step for THAT slot on the
// litter's birth track (CaptureReshootEngine, the tasks engine); recording it swaps that proof in
// the snapshot and queues a fresh item for that slot only (BirthCaptureReshootService).

// CaptureMediaMeta / CaptureContextRow are what the enqueue request carries per proof / answer
// (the bridge maps them onto verification's shared MediaMeta / ContextRow).
type CaptureMediaMeta struct {
	Label string
	Kind  string
}

type CaptureContextRow struct {
	Label string
	Value string
	Group string
}

// BirthCaptureVerificationEnqueueRequest is one litter's report proofs handed to the verifier.
type BirthCaptureVerificationEnqueueRequest struct {
	TenantID     string
	BirthEventID string
	SlotKey      string
	OperatorID   string
	ParkID       string
	ShedID       string
	// PartitionLabel is the pen inside ShedID the report named (blank = undivided shed).
	PartitionLabel string
	ProofRefs      []string
	MediaMeta      []CaptureMediaMeta
	ContextRows    []CaptureContextRow
	SubjectLabel   string
	CapturedAt     time.Time
	// IdempotencyKey is domain.BirthCaptureKey: "counts-birth-capture:<event>:<slot>:<ref>". A
	// replayed event collapses (CreateItem is ON CONFLICT DO NOTHING) while a re-shoot (new ref)
	// mints a fresh item and the rejected one stays history.
	IdempotencyKey string
}

// BirthCaptureVerificationEnqueuer is the composition-layer seam to the verification module.
type BirthCaptureVerificationEnqueuer interface {
	EnqueueBirthCaptureVerification(ctx context.Context, in BirthCaptureVerificationEnqueueRequest) error
}

type birthReportedPayload struct {
	ApprovalRequestID string            `json:"approval_request_id"`
	BirthEventID      string            `json:"birth_event_id"`
	ParkID            string            `json:"park_id"`
	ShedID            string            `json:"shed_id"`
	PartitionLabel    string            `json:"partition_label"`
	GoatIDs           []string          `json:"goat_ids"`
	RaisedByUserID    string            `json:"raised_by_user_id"`
	CaptureEvidence   authored.Evidence `json:"capture_evidence"`
}

// BirthReportedVerificationHandler consumes counts.birth.reported.
type BirthReportedVerificationHandler struct {
	enqueuer BirthCaptureVerificationEnqueuer
	now      func() time.Time
}

// NewBirthReportedVerificationHandler constructs the consumer.
func NewBirthReportedVerificationHandler(enqueuer BirthCaptureVerificationEnqueuer, now func() time.Time) *BirthReportedVerificationHandler {
	if now == nil {
		now = time.Now
	}
	return &BirthReportedVerificationHandler{enqueuer: enqueuer, now: now}
}

var _ eventbus.Handler = (*BirthReportedVerificationHandler)(nil)

// Register subscribes the consumer.
func (h *BirthReportedVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(domain.EventBirthReported, h)
}

// HandleEvent enqueues the litter's report proofs. Idempotent on the refs-keyed item.
func (h *BirthReportedVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != domain.EventBirthReported {
		return nil
	}
	var p birthReportedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	birthEventID := strings.TrimSpace(p.BirthEventID)
	if birthEventID == "" {
		birthEventID = strings.TrimSpace(p.ApprovalRequestID)
	}
	if birthEventID == "" {
		birthEventID = strings.TrimSpace(e.Key)
	}
	if birthEventID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	reqs := withPen(BirthCaptureEnqueueRequests(e.TenantID, birthEventID, p.OperatorID(), p.ParkID, p.ShedID, p.CaptureEvidence, e.OccurredAt, h.now()), p.PartitionLabel)
	if len(reqs) == 0 {
		return nil
	}
	if h.enqueuer == nil {
		return ErrBirthCaptureEnqueuerNotWired
	}
	for _, req := range reqs {
		// scale-guard:ignore: bounded -- one item per authored form proof slot (max 8 per card)
		if err := h.enqueuer.EnqueueBirthCaptureVerification(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

func (p birthReportedPayload) OperatorID() string { return strings.TrimSpace(p.RaisedByUserID) }

// BirthCaptureEnqueueRequests composes ONE verifier item per captured form proof slot: that
// slot's proof under its authored title with the register's kind, and the form's answers (plus
// the older-app note) as context rows grouped "At report". Exported so the re-shoot path composes
// the identical item for one slot.
func BirthCaptureEnqueueRequests(tenantID, birthEventID, operatorID, parkID, shedID string, capture authored.Evidence, occurredAt, now time.Time) []BirthCaptureVerificationEnqueueRequest {
	var rows []CaptureContextRow
	for _, r := range capture.Rows {
		if strings.TrimSpace(r.Value) == "" {
			continue
		}
		group := strings.TrimSpace(r.Group)
		if group == "" {
			group = domain.CaptureEvidenceGroup
		}
		rows = append(rows, CaptureContextRow{Label: strings.TrimSpace(r.Label), Value: strings.TrimSpace(r.Value), Group: group})
	}
	if note := strings.TrimSpace(capture.MissingNote); note != "" {
		rows = append(rows, CaptureContextRow{Label: authored.MissingNoteOlderApp, Value: note, Group: domain.CaptureEvidenceGroup})
	}
	at := occurredAt
	if at.IsZero() {
		at = now
	}
	out := make([]BirthCaptureVerificationEnqueueRequest, 0, len(capture.Media))
	for _, m := range capture.Media {
		ref := strings.TrimSpace(m.Ref)
		slot := strings.TrimSpace(m.Key)
		if ref == "" || slot == "" {
			continue
		}
		label := strings.TrimSpace(m.Label)
		out = append(out, BirthCaptureVerificationEnqueueRequest{
			TenantID: tenantID, BirthEventID: birthEventID, SlotKey: slot, OperatorID: operatorID,
			ParkID: strings.TrimSpace(parkID), ShedID: strings.TrimSpace(shedID),
			ProofRefs:      []string{ref},
			MediaMeta:      []CaptureMediaMeta{{Label: label, Kind: strings.TrimSpace(m.Kind)}},
			ContextRows:    rows,
			SubjectLabel:   "Birth report · " + label,
			CapturedAt:     at.UTC(),
			IdempotencyKey: domain.BirthCaptureKey(birthEventID, slot, ref),
		})
	}
	return out
}

// withPen stamps the pen the report named on every item: the verifier reads "Castro 1", never the
// bare shed (E2E 2026-09-17). 'whole' is a matching key, never a pen.
func withPen(reqs []BirthCaptureVerificationEnqueueRequest, partitionLabel string) []BirthCaptureVerificationEnqueueRequest {
	label := strings.TrimSpace(partitionLabel)
	if strings.EqualFold(label, "whole") {
		label = ""
	}
	for i := range reqs {
		reqs[i].PartitionLabel = label
	}
	return reqs
}

// ErrBirthCaptureEnqueuerNotWired is a composition bug surfaced loudly (the event retries).
var ErrBirthCaptureEnqueuerNotWired = errors.New("counts: birth capture verification enqueuer is not wired")

// captureReviewStore is the slice of the counts repository the verdict consumer drives. It
// applies a slot verdict only when the slot still holds ref (applied=false otherwise) and returns
// the report so a rework can open the re-shoot.
type captureReviewStore interface {
	SetCaptureSlotReview(ctx context.Context, tenantID, approvalRequestID, slotKey, ref, status, reason string) (domain.ApprovalRequest, bool, error)
}

// BirthCaptureVerdictHandler lands the verifier's verdict on the report's approval row. It
// filters strictly on source.module=counts + ref_type=birth_capture, so birth STEP verdicts
// (workflow_birth_action), death bundles and shifting pass through untouched.
type BirthCaptureVerdictHandler struct {
	store  captureReviewStore
	engine CaptureReshootEngine
	now    func() time.Time
}

// CaptureReshootEngine is the tasks-engine seam that appends the re-shoot steps; indexes names
// the capture proofs to re-shoot (nil = all).
type CaptureReshootEngine interface {
	OpenBirthCaptureReshoot(ctx context.Context, tenantID, birthEventID string, capture authored.Evidence, indexes []int, recordingKey, reason string) error
}

// WithReshootEngine wires decision 5. Without it a rejection is recorded but opens no step.
func (h *BirthCaptureVerdictHandler) WithReshootEngine(engine CaptureReshootEngine) *BirthCaptureVerdictHandler {
	h.engine = engine
	return h
}

// NewBirthCaptureVerdictHandler constructs the consumer.
func NewBirthCaptureVerdictHandler(store captureReviewStore, now func() time.Time) *BirthCaptureVerdictHandler {
	if now == nil {
		now = time.Now
	}
	return &BirthCaptureVerdictHandler{store: store, now: now}
}

var _ eventbus.Handler = (*BirthCaptureVerdictHandler)(nil)

// Register subscribes both verdict types.
func (h *BirthCaptureVerdictHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventVerificationVerdictApproved, h)
	bus.Subscribe(EventVerificationVerdictRework, h)
}

// HandleEvent stamps approved / rework (+ reason) on the approval row.
func (h *BirthCaptureVerdictHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != EventVerificationVerdictApproved && e.Type != EventVerificationVerdictRework {
		return nil
	}
	var p penReconciliationVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != domain.VerificationModuleCounts || p.Source.RefType != domain.VerificationRefTypeBirthCapture {
		return nil
	}
	refID := strings.TrimSpace(p.Source.RefID)
	if refID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	eventID, slotKey, ref, ok := domain.ParseBirthCaptureKey(strings.TrimSpace(p.Source.RecordingKey))
	if !ok || eventID != refID {
		// Fenced: a verdict that does not name one slot's recording applies to nothing.
		return nil
	}
	status, reason := domain.CaptureReviewApproved, ""
	if e.Type == EventVerificationVerdictRework {
		status, reason = domain.CaptureReviewRework, strings.TrimSpace(p.Reason)
	}
	report, applied, err := h.store.SetCaptureSlotReview(ctx, e.TenantID, refID, slotKey, ref, status, reason)
	if err != nil || !applied {
		// !applied: the slot no longer holds this ref (superseded by a re-shoot) -- history.
		return err
	}
	if e.Type != EventVerificationVerdictRework || h.engine == nil {
		return nil
	}
	for i, m := range report.Capture.Media {
		if m.Key == slotKey {
			return h.engine.OpenBirthCaptureReshoot(ctx, e.TenantID, refID, report.Capture, []int{i}, strings.TrimSpace(p.Source.RecordingKey), reason)
		}
	}
	return nil
}

// BirthCaptureReshootService is the counts side of a recorded report re-shoot: swap the proof in
// the approval row's snapshot (review back to pending) and queue a fresh report item.
type BirthCaptureReshootService struct {
	store    captureProofReplacer
	enqueuer BirthCaptureVerificationEnqueuer
}

type captureProofReplacer interface {
	ReplaceCaptureProof(ctx context.Context, tenantID, approvalRequestID string, index int, proof tasksdomain.ProofItem) (domain.ApprovalRequest, error)
}

// NewBirthCaptureReshootService constructs the listener.
func NewBirthCaptureReshootService(store captureProofReplacer, enqueuer BirthCaptureVerificationEnqueuer) *BirthCaptureReshootService {
	return &BirthCaptureReshootService{store: store, enqueuer: enqueuer}
}

// OnBirthCaptureReshot implements tasks/app.CaptureReshootListener. Idempotent: replacing with the
// same proof writes the same snapshot and the refs-keyed item collapses.
func (s *BirthCaptureReshootService) OnBirthCaptureReshot(ctx context.Context, tenantID, birthEventID string, index int, proof tasksdomain.ProofItem) error {
	report, err := s.store.ReplaceCaptureProof(ctx, tenantID, birthEventID, index, proof)
	if err != nil {
		return err
	}
	if s.enqueuer == nil {
		return ErrBirthCaptureEnqueuerNotWired
	}
	var form struct {
		ParkID         string `json:"park_id"`
		ShedID         string `json:"shed_id"`
		PartitionLabel string `json:"partition_label"`
	}
	if len(report.Payload) > 0 {
		if err := json.Unmarshal(report.Payload, &form); err != nil {
			return fmt.Errorf("counts: birth capture re-shoot: decode report payload: %w", err)
		}
	}
	if index < 0 || index >= len(report.Capture.Media) {
		return nil
	}
	// A re-shoot replaces exactly ONE form proof, so it enqueues at most one item: the request
	// built from that single slot (nothing when its ref or key is blank).
	one := report.Capture
	one.Media = []authored.EvidenceMedia{report.Capture.Media[index]}
	reqs := withPen(BirthCaptureEnqueueRequests(tenantID, birthEventID, report.RaisedByUserID, form.ParkID, form.ShedID, one, time.Time{}, time.Now()), form.PartitionLabel)
	if len(reqs) == 0 {
		return nil
	}
	return s.enqueuer.EnqueueBirthCaptureVerification(ctx, reqs[0])
}

// BirthCaptureVerificationWithdrawer retires the still-PENDING birth_capture items of one litter
// (ref_type birth_capture, ref_id birth_event_id). countsbridge implements it over verification's
// own WithdrawItemsBySource seam; counts never writes verification's tables.
type BirthCaptureVerificationWithdrawer interface {
	WithdrawBirthCaptureVerification(ctx context.Context, tenantID, birthEventID string) error
}

// BirthRejectedCaptureWithdrawHandler is counts' own consumer of counts.birth.rejected (maintainer
// decision 2026-09-25): the report's form proofs still waiting for the verifier are withdrawn, a
// verdict already cast stays history. The producer of those items withdraws them, exactly as
// weighing withdraws its own. A redelivery withdraws nothing more.
type BirthRejectedCaptureWithdrawHandler struct {
	withdrawer BirthCaptureVerificationWithdrawer
}

// NewBirthRejectedCaptureWithdrawHandler constructs the consumer.
func NewBirthRejectedCaptureWithdrawHandler(withdrawer BirthCaptureVerificationWithdrawer) *BirthRejectedCaptureWithdrawHandler {
	return &BirthRejectedCaptureWithdrawHandler{withdrawer: withdrawer}
}

var _ eventbus.Handler = (*BirthRejectedCaptureWithdrawHandler)(nil)

// Register subscribes the consumer.
func (h *BirthRejectedCaptureWithdrawHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(domain.EventBirthRejected, h)
}

// HandleEvent withdraws the rejected litter's pending capture items.
func (h *BirthRejectedCaptureWithdrawHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != domain.EventBirthRejected {
		return nil
	}
	var p struct {
		ApprovalRequestID string `json:"approval_request_id"`
		BirthEventID      string `json:"birth_event_id"`
	}
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	birthEventID := strings.TrimSpace(p.BirthEventID)
	if birthEventID == "" {
		birthEventID = strings.TrimSpace(p.ApprovalRequestID)
	}
	if birthEventID == "" {
		birthEventID = strings.TrimSpace(e.Key)
	}
	if birthEventID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	if h.withdrawer == nil {
		// Loud, retried and dead-lettered rather than a silent drop: a pending item for a rejected
		// birth would otherwise sit in the verifier's queue for ever.
		return errors.New("counts: birth capture withdraw seam is not wired")
	}
	return h.withdrawer.WithdrawBirthCaptureVerification(ctx, e.TenantID, birthEventID)
}
