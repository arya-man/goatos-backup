package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// The visit video goes to the verifier (maintainer decision 2026-09-12): the parent care task
// closes only "when all these videos are verified by the verifier", and the next-day visit is
// the last of those videos. Two consumers, the PC Care shape:
//
//	pen_visit.submitted                       -> EnqueuePenVisitVerification  (ONE item, ONE clip)
//	verification.verdict.approved (our item)  -> ApplyVerified   (emits pen_visit.verified in-tx)
//	verification.verdict.rework   (our item)  -> BounceForRework (the visitor records again)
//	pen_visit.verified                        -> close the parents (PC Care task -> completed)
//
// All filter STRICTLY on the module / ref_type this module owns, so a vaccination, feed or
// PC Care verdict is ignored here exactly as ours is ignored there. Parent closure rides its
// own durable event rather than the verdict transaction so a parent module that is down when
// the verdict lands is retried by the relay, not lost.
const (
	eventVisitSubmitted              = "pen_visit.submitted"
	eventVisitVerified               = "pen_visit.verified"
	eventVerificationVerdictApproved = "verification.verdict.approved"
	eventVerificationVerdictRework   = "verification.verdict.rework"

	// VerificationVertical / VerificationModule / VerificationCategory / VerificationRefType are
	// the visit's identity in the verifier queue. Category pen_visit sits in the Preventive
	// Care verify tab beside the PC Care pages; the ref type is what the verdict consumer keys on.
	VerificationVertical = "preventive_care"
	VerificationModule   = "pen_visits"
	VerificationCategory = "pen_visit"
	VerificationRefType  = "pen_visit_task"
)

// VerificationEnqueueRequest is what the bridge needs to mint the verifier item.
type VerificationEnqueueRequest struct {
	TenantID       string
	TaskID         string
	ParkID         string
	ParkName       string
	ShedID         string
	PartitionLabel string
	PenLabel       string
	Reasons        []string
	SourceDate     string
	ProofRef       string
	SubmittedBy    string
	CapturedAt     time.Time
	IdempotencyKey string
}

// VerificationEnqueuer is the seam into the verification module (adapters/verificationbridge).
type VerificationEnqueuer interface {
	EnqueuePenVisitVerification(ctx context.Context, in VerificationEnqueueRequest) error
}

// ParentCloser is the seam a parent module registers so an approved visit can close the work
// that raised it. PC Care registers one for SourceKindPCCareTask; vaccination needs none (its
// shed reads derive closure from the visit row). A kind with no closer is left alone -- the
// visit is still verified, and the parent surface still reads it through ForSources.
type ParentCloser interface {
	// PenVisitVerified closes the parents named by refIDs. It MUST be idempotent: the event
	// consumer is at-least-once, so a closer that failed part-way is re-run on redelivery.
	PenVisitVerified(ctx context.Context, tenantID string, refIDs []string, visitTaskID, traceID string) error
}

// submittedPayload is the subset of the pen_visit.submitted payload the enqueuer reads.
type submittedPayload struct {
	TaskID         string   `json:"task_id"`
	ParkID         string   `json:"park_id"`
	ParkName       string   `json:"park_name"`
	ShedID         string   `json:"shed_id"`
	PartitionLabel string   `json:"partition_label"`
	PenLabel       string   `json:"pen_label"`
	Reasons        []string `json:"reasons"`
	SourceDate     string   `json:"source_business_date"`
	Status         string   `json:"status"`
	ProofRef       string   `json:"proof_ref"`
	RowVersion     int      `json:"row_version"`
	ChangedBy      string   `json:"changed_by_user_id"`
}

// PendingVerificationHandler turns the module-owned transactional submit event into the generic
// verifier item. If verification is temporarily down, the outbox/domain consumer retries this
// event; the idempotency key is stable at (task, row version) grain, so a re-shoot after a
// rework mints a fresh item while a retry collapses onto one.
type PendingVerificationHandler struct {
	enqueuer VerificationEnqueuer
	log      *slog.Logger
}

// NewPendingVerificationHandler constructs the enqueue consumer.
func NewPendingVerificationHandler(enqueuer VerificationEnqueuer, log *slog.Logger) *PendingVerificationHandler {
	return &PendingVerificationHandler{enqueuer: enqueuer, log: log}
}

var _ eventbus.Handler = (*PendingVerificationHandler)(nil)

// Register subscribes to the submit event.
func (h *PendingVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVisitSubmitted, h)
}

// HandleEvent enqueues one verifier item per submit.
func (h *PendingVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventVisitSubmitted || h.enqueuer == nil {
		return nil
	}
	var p submittedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	taskID := strings.TrimSpace(p.TaskID)
	proof := strings.TrimSpace(p.ProofRef)
	if taskID == "" || proof == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	capturedAt := time.Now().UTC()
	if !e.OccurredAt.IsZero() {
		capturedAt = e.OccurredAt.UTC()
	}
	return h.enqueuer.EnqueuePenVisitVerification(ctx, VerificationEnqueueRequest{
		TenantID:       e.TenantID,
		TaskID:         taskID,
		ParkID:         strings.TrimSpace(p.ParkID),
		ParkName:       strings.TrimSpace(p.ParkName),
		ShedID:         strings.TrimSpace(p.ShedID),
		PartitionLabel: strings.TrimSpace(p.PartitionLabel),
		PenLabel:       strings.TrimSpace(p.PenLabel),
		Reasons:        p.Reasons,
		SourceDate:     strings.TrimSpace(p.SourceDate),
		ProofRef:       proof,
		SubmittedBy:    strings.TrimSpace(p.ChangedBy),
		CapturedAt:     capturedAt,
		IdempotencyKey: "pen-visit-verification:" + taskID + ":" + strconv.Itoa(p.RowVersion),
	})
}

// verdictPayload is the subset of the verdict payload the applier reads.
type verdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// VerdictStore is the narrow slice of the repository the applier drives.
type VerdictStore interface {
	ApplyVerified(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error)
	BounceForRework(ctx context.Context, p ports.VerdictParams) (ports.VerdictResult, error)
}

// VerificationHandler applies a verifier's verdict to the visit it judged. An approve emits
// pen_visit.verified inside the same transaction; VerifiedHandler below closes the parents
// from it.
type VerificationHandler struct {
	store VerdictStore
	log   *slog.Logger
}

// NewVerificationHandler constructs the applier over the verdict store.
func NewVerificationHandler(store VerdictStore, log *slog.Logger) *VerificationHandler {
	return &VerificationHandler{store: store, log: log}
}

var _ eventbus.Handler = (*VerificationHandler)(nil)

// Register subscribes to both verdict event types.
func (h *VerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVerificationVerdictApproved, h)
	bus.Subscribe(eventVerificationVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a pen_visits item to the matching write.
func (h *VerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventVerificationVerdictApproved && e.Type != eventVerificationVerdictRework {
		return nil
	}
	var p verdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	if p.Source.Module != VerificationModule || p.Source.RefType != VerificationRefType {
		return nil
	}
	taskID := strings.TrimSpace(p.Source.RefID)
	if taskID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	params := ports.VerdictParams{
		TenantID:   e.TenantID,
		TaskID:     taskID,
		VerifiedBy: strings.TrimSpace(p.VerifiedBy),
		Reason:     strings.TrimSpace(p.Reason),
		TraceID:    e.ID,
	}
	if e.Type == eventVerificationVerdictRework {
		_, err := h.store.BounceForRework(ctx, params)
		return err
	}
	_, err := h.store.ApplyVerified(ctx, params)
	return err
}

// verifiedPayload is the subset of the pen_visit.verified payload the parent closer reads.
type verifiedPayload struct {
	TaskID  string   `json:"task_id"`
	Status  string   `json:"status"`
	Sources []string `json:"sources"`
}

// VerifiedHandler closes the parents an approved visit was the last step of. Registered in
// eventwiring beside the verdict appliers so the API bus, the relay and the Pub/Sub consumer
// cannot drift apart.
type VerifiedHandler struct {
	closers map[string]ParentCloser
	log     *slog.Logger
}

// NewVerifiedHandler constructs the parent-closure consumer.
func NewVerifiedHandler(log *slog.Logger) *VerifiedHandler {
	return &VerifiedHandler{closers: map[string]ParentCloser{}, log: log}
}

// WithParentCloser registers the closer for one source kind.
func (h *VerifiedHandler) WithParentCloser(kind string, closer ParentCloser) *VerifiedHandler {
	if closer != nil {
		h.closers[kind] = closer
	}
	return h
}

var _ eventbus.Handler = (*VerifiedHandler)(nil)

// Register subscribes to the verified event.
func (h *VerifiedHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVisitVerified, h)
}

// HandleEvent closes every parent the visit names, per source kind.
func (h *VerifiedHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventVisitVerified {
		return nil
	}
	var p verifiedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	taskID := strings.TrimSpace(p.TaskID)
	if taskID == "" || strings.TrimSpace(e.TenantID) == "" || p.Status != domain.StatusCompleted {
		return nil
	}
	byKind := map[string][]string{}
	for _, src := range p.Sources {
		kind, ref, ok := strings.Cut(src, ":")
		if !ok || kind == "" || ref == "" {
			continue
		}
		byKind[kind] = append(byKind[kind], ref)
	}
	for kind, refs := range byKind {
		closer, ok := h.closers[kind]
		if !ok {
			continue
		}
		if err := closer.PenVisitVerified(ctx, e.TenantID, refs, taskID, e.ID); err != nil {
			return err
		}
	}
	return nil
}
