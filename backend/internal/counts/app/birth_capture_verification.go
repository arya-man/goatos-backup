package app

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/sop/authored"
)

// BIRTH REPORT → VERIFIER (maintainer decisions 4-6, 2026-09-16). The Add birth form's own
// captures (the SOP capture card's proofs) are reviewed as ONE birth_evidence item per litter,
// ref_type birth_capture / ref_id birth_event_id, born from counts.birth.reported -- the event
// the submit transaction writes beside the request and its canonical children. A report with
// answers and NO media never creates an empty verifier item: the answers reach the approver
// through the row's capture snapshot instead. The verdict lands on the approval row
// (capture_review_status / reason) so the approver sees the report's proof state.
//
// Decision 5 (a rejection opens a "re-shoot report proof" step on the birth workflow) is NOT
// wired here yet; the verdict is recorded and shown, the re-shoot step is follow-up work.

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
	OperatorID   string
	ParkID       string
	ShedID       string
	ProofRefs    []string
	MediaMeta    []CaptureMediaMeta
	ContextRows  []CaptureContextRow
	SubjectLabel string
	CapturedAt   time.Time
	// IdempotencyKey is "counts-birth-capture:<birth_event_id>:<refs…>": a replayed event
	// collapses (CreateItem is ON CONFLICT DO NOTHING) while a re-shoot with new refs mints a
	// fresh item.
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
	req := BirthCaptureEnqueueRequest(e.TenantID, birthEventID, p.OperatorID(), p.ParkID, p.ShedID, len(p.GoatIDs), p.CaptureEvidence, e.OccurredAt, h.now())
	if len(req.ProofRefs) == 0 {
		return nil
	}
	if h.enqueuer == nil {
		return ErrBirthCaptureEnqueuerNotWired
	}
	return h.enqueuer.EnqueueBirthCaptureVerification(ctx, req)
}

func (p birthReportedPayload) OperatorID() string { return strings.TrimSpace(p.RaisedByUserID) }

// BirthCaptureEnqueueRequest composes the verifier item from the capture snapshot: every proof
// under its authored title with the register's kind, every answer as a row, the older-app note
// as a row. Exported so the recovery/rebuild paths compose the identical item.
func BirthCaptureEnqueueRequest(tenantID, birthEventID, operatorID, parkID, shedID string, kids int, capture authored.Evidence, occurredAt, now time.Time) BirthCaptureVerificationEnqueueRequest {
	out := BirthCaptureVerificationEnqueueRequest{TenantID: tenantID, BirthEventID: birthEventID, OperatorID: operatorID, ParkID: strings.TrimSpace(parkID), ShedID: strings.TrimSpace(shedID)}
	key := "counts-birth-capture:" + birthEventID
	for _, m := range capture.Media {
		ref := strings.TrimSpace(m.Ref)
		if ref == "" {
			continue
		}
		out.ProofRefs = append(out.ProofRefs, ref)
		out.MediaMeta = append(out.MediaMeta, CaptureMediaMeta{Label: strings.TrimSpace(m.Label), Kind: strings.TrimSpace(m.Kind)})
		key += ":" + ref
	}
	for _, r := range capture.Rows {
		if strings.TrimSpace(r.Value) == "" {
			continue
		}
		group := strings.TrimSpace(r.Group)
		if group == "" {
			group = domain.CaptureEvidenceGroup
		}
		out.ContextRows = append(out.ContextRows, CaptureContextRow{Label: strings.TrimSpace(r.Label), Value: strings.TrimSpace(r.Value), Group: group})
	}
	if note := strings.TrimSpace(capture.MissingNote); note != "" {
		out.ContextRows = append(out.ContextRows, CaptureContextRow{Label: authored.MissingNoteOlderApp, Value: note, Group: domain.CaptureEvidenceGroup})
	}
	at := occurredAt
	if at.IsZero() {
		at = now
	}
	out.CapturedAt = at.UTC()
	kidsLabel := "1 kid"
	if kids != 1 {
		kidsLabel = strconv.Itoa(kids) + " kids"
	}
	out.SubjectLabel = "Birth report · " + kidsLabel + " · " + biztime.BusinessDate(at)
	out.IdempotencyKey = key
	return out
}

// ErrBirthCaptureEnqueuerNotWired is a composition bug surfaced loudly (the event retries).
var ErrBirthCaptureEnqueuerNotWired = errors.New("counts: birth capture verification enqueuer is not wired")

// captureReviewStore is the slice of the counts repository the verdict consumer drives.
type captureReviewStore interface {
	SetCaptureReviewStatus(ctx context.Context, tenantID, approvalRequestID, status, reason string) error
}

// BirthCaptureVerdictHandler lands the verifier's verdict on the report's approval row. It
// filters strictly on source.module=counts + ref_type=birth_capture, so birth STEP verdicts
// (workflow_birth_action), death bundles and shifting pass through untouched.
type BirthCaptureVerdictHandler struct {
	store captureReviewStore
	now   func() time.Time
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
	status, reason := domain.CaptureReviewApproved, ""
	if e.Type == EventVerificationVerdictRework {
		status, reason = domain.CaptureReviewRework, strings.TrimSpace(p.Reason)
	}
	return h.store.SetCaptureReviewStatus(ctx, e.TenantID, refID, status, reason)
}
