package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/health/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

var (
	ErrInvalidInput = errors.New("health: invalid input")
	ErrInvalidDate  = errors.New("health: invalid date")
	// ErrVerificationEnqueuerNotWired fails a proof-carrying completion closed when the
	// composition layer forgot the verification seam: accepting evidence that can never reach
	// the verifier queue is the silent-drop this error exists to prevent.
	ErrVerificationEnqueuerNotWired = errors.New("health: verification enqueuer not wired")
)

// TreatmentVerificationEnqueuer hands one completed treatment session's proof video to the
// verification queue. The composition layer adapts verification's CreateItem to this narrow port
// so health never touches verification's tables directly.
type TreatmentVerificationEnqueuer interface {
	EnqueueTreatmentVerification(ctx context.Context, in TreatmentVerificationEnqueueRequest) error
}

// TreatmentVerificationEnqueueRequest is one treatment-proof video handed to the verification queue.
type TreatmentVerificationEnqueueRequest struct {
	TenantID  string
	SessionID string
	// AgeBand routes the item to its verifier page (health_adults / health_kids).
	AgeBand    string
	OperatorID string
	ParkID     string
	ShedID     string
	MediaRefs  []string
	// Captures names each ref, POSITIONAL against MediaRefs. One item now carries every step's
	// clip, so without a name per clip the verifier sees twelve videos and cannot tell which of
	// six injections each one is.
	Captures     []verificationdomain.ProofCapture
	SubjectLabel string
	CapturedAt   time.Time
	// IdempotencyKey carries the proof set so transport retries heal idempotently while a
	// rework re-shoot with a new video creates the replacement review item.
	IdempotencyKey string
}

type Service struct {
	repo     ports.Repository
	enqueuer TreatmentVerificationEnqueuer
}

func NewService(repo ports.Repository) *Service { return &Service{repo: repo} }

// WithVerificationEnqueuer wires the evidence-review enqueue seam.
func (s *Service) WithVerificationEnqueuer(enqueuer TreatmentVerificationEnqueuer) *Service {
	s.enqueuer = enqueuer
	return s
}

func (s *Service) OpenCase(ctx context.Context, in domain.OpenCaseInput) (domain.OpenCaseResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.GoatID = strings.TrimSpace(in.GoatID)
	in.DiseaseKey = strings.ToLower(strings.TrimSpace(in.DiseaseKey))
	in.AgeBand = strings.ToLower(strings.TrimSpace(in.AgeBand))
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.GoatID) ||
		in.DiseaseKey == "" || !validAgeBand(in.AgeBand) || in.IdempotencyKey == "" || in.StartDate.IsZero() {
		return domain.OpenCaseResult{}, ErrInvalidInput
	}
	return s.repo.OpenCase(ctx, in)
}
func (s *Service) ListWorkItems(ctx context.Context, f domain.ListFilter) (domain.WorkItemPage, error) {
	f.TenantID = strings.TrimSpace(f.TenantID)
	f.AgeBand = strings.ToLower(strings.TrimSpace(f.AgeBand))
	f.Status = strings.ToLower(strings.TrimSpace(f.Status))
	f.DiseaseKey = strings.ToLower(strings.TrimSpace(f.DiseaseKey))
	f.ParkID = strings.TrimSpace(f.ParkID)
	f.ShedID = strings.TrimSpace(f.ShedID)
	f.Session = strings.ToLower(strings.TrimSpace(f.Session))
	if !validUUID(f.TenantID) || !validAgeBand(f.AgeBand) {
		return domain.WorkItemPage{}, ErrInvalidInput
	}
	if _, err := time.Parse("2006-01-02", f.Date); err != nil {
		return domain.WorkItemPage{}, ErrInvalidDate
	}
	if f.Status == "held" {
		f.Status = "held_death_review"
	}
	if f.Status != "" && !validWorkStatus(f.Status) {
		return domain.WorkItemPage{}, ErrInvalidInput
	}
	if (f.ParkID != "" && !validUUID(f.ParkID)) || (f.ShedID != "" && !validUUID(f.ShedID)) || !validSessionFilter(f.Session) {
		return domain.WorkItemPage{}, ErrInvalidInput
	}
	if f.Limit <= 0 || f.Limit > domain.MaxPageSize {
		f.Limit = domain.MaxPageSize
	}
	return s.repo.ListWorkItems(ctx, f)
}
func (s *Service) GetWorkItem(ctx context.Context, tenantID, sessionID string) (domain.WorkItemDetail, error) {
	if !validUUID(strings.TrimSpace(tenantID)) || !validUUID(strings.TrimSpace(sessionID)) {
		return domain.WorkItemDetail{}, ErrInvalidInput
	}
	return s.repo.GetWorkItem(ctx, tenantID, sessionID)
}
// RecordStepProof attaches ONE step's video, on its own, before the session is submitted.
//
// Separate from the submit because a blob reaching storage is not the business fact (the proof
// business-ack contract). THIS is the business fact for that step: it retries by itself, and a
// failure here after a successful upload retries only this small write -- the video is never
// re-uploaded to repair the link.
func (s *Service) RecordStepProof(ctx context.Context, in domain.RecordStepProofInput) (domain.StepProof, error) {
	if err := in.Validate(); err != nil {
		return domain.StepProof{}, err
	}
	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.SessionID) || !validUUID(in.StepID) {
		return domain.StepProof{}, ErrInvalidInput
	}
	return s.repo.RecordStepProof(ctx, in)
}

// StepProofs is a session's recorded clips, in step order.
func (s *Service) StepProofs(ctx context.Context, tenantID, sessionID string) ([]domain.StepProof, error) {
	if !validUUID(strings.TrimSpace(tenantID)) || !validUUID(strings.TrimSpace(sessionID)) {
		return nil, ErrInvalidInput
	}
	return s.repo.StepProofs(ctx, tenantID, sessionID)
}

func (s *Service) CompleteWorkItem(ctx context.Context, in domain.CompleteInput) (domain.CompleteResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.SessionID = strings.TrimSpace(in.SessionID)
	in.ProofRef = strings.TrimSpace(in.ProofRef)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.SessionID) || in.IdempotencyKey == "" {
		return domain.CompleteResult{}, ErrInvalidInput
	}
	if in.ProofRef != "" && s.enqueuer == nil {
		// Fail closed BEFORE the write: a proof accepted with no review path is a silent drop.
		// A proof-less completion (still allowed for compatibility) has nothing to review and
		// passes through.
		return domain.CompleteResult{}, ErrVerificationEnqueuerNotWired
	}
	res, err := s.repo.CompleteWorkItem(ctx, in)
	if err != nil {
		return domain.CompleteResult{}, err
	}
	// Enqueue one evidence-review item, on replays too: CreateItem is idempotent on the key, so a
	// retry that crashed between commit and enqueue heals here instead of stranding the video.
	//
	// THE GATE IS "IS THERE EVIDENCE", NOT "IS THERE A SESSION VIDEO". A per-step card sends no
	// session proof of its own -- the clips hang off the STEPS -- so gating on `in.ProofRef`
	// alone completed the session and queued nothing: five clips on the server and no reviewer.
	if in.ProofRef != "" || len(res.StepMedia) > 0 {
		subject := "Day " + strconv.Itoa(res.DayNo) + " · " + res.DiseaseName + " · " + res.GoatDisplayID
		if loc := (oploc.OperationalLocation{ShedName: res.ShedLabel, PartitionLabel: res.PartitionLabel}).Display(); loc != "" {
			subject += " · " + loc
		}
		// ONE ITEM, EVERY STEP'S CLIP (maintainer decision 2026-09-23). The review grain is
		// unchanged -- one item per session, tag and disease -- and the verifier steps through the
		// set inside it. A legacy one-video completion carries its single ref exactly as before.
		mediaRefs := []string{in.ProofRef}
		var captures []verificationdomain.ProofCapture
		if len(res.StepMedia) > 0 {
			mediaRefs = mediaRefs[:0]
			for _, m := range res.StepMedia {
				mediaRefs = append(mediaRefs, m.ProofRef)
				captures = append(captures, verificationdomain.ProofCapture{
					Title: m.Label,
					Kind:  verificationdomain.MediaKindVideo,
				})
			}
		}
		if err := s.enqueuer.EnqueueTreatmentVerification(ctx, TreatmentVerificationEnqueueRequest{
			TenantID:     in.TenantID,
			SessionID:    in.SessionID,
			AgeBand:      res.AgeBand,
			OperatorID:   in.ActorID,
			ParkID:       res.ParkID,
			ShedID:       res.ShedID,
			MediaRefs:    mediaRefs,
			Captures:     captures,
			SubjectLabel: subject,
			CapturedAt:   res.CompletedAt,
			// Keyed to the SESSION + proof so a retry collapses onto one queue item while a
			// rework re-shoot (new proof ref) creates the replacement item.
			// The key carries WHAT was filmed, so a retry replays onto one row while a re-shoot
			// is a NEW item. For a per-step card that is the step clip set; an empty tail would
			// collapse every rework of this session onto one row, and CreateItem is ON CONFLICT
			// DO NOTHING -- the replacement would be dropped and never reviewed.
			IdempotencyKey: "health-treatment-verification:" + in.SessionID + ":" + strings.Join(mediaRefs, ","),
		}); err != nil {
			return domain.CompleteResult{}, err
		}
	}
	return res, nil
}

// CloseCase records the clinical outcome of an open case. Gated on health.diagnose at the route:
// closing a course is the same clinical authority as opening one.
func (s *Service) CloseCase(ctx context.Context, in domain.CloseCaseInput) (domain.CloseCaseResult, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ActorID = strings.TrimSpace(in.ActorID)
	in.CaseID = strings.TrimSpace(in.CaseID)
	in.Outcome = strings.ToLower(strings.TrimSpace(in.Outcome))
	in.Note = strings.TrimSpace(in.Note)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if !validUUID(in.TenantID) || !validUUID(in.ActorID) || !validUUID(in.CaseID) ||
		in.IdempotencyKey == "" || !validCaseOutcome(in.Outcome) {
		return domain.CloseCaseResult{}, ErrInvalidInput
	}
	return s.repo.CloseCase(ctx, in)
}
func validAgeBand(v string) bool { return v == domain.AgeBandAdult || v == domain.AgeBandKid }
func validCaseOutcome(v string) bool {
	switch v {
	case domain.CaseOutcomeRecovered, domain.CaseOutcomeReferred, domain.CaseOutcomeCanceled:
		return true
	default:
		return false
	}
}
func validUUID(v string) bool { _, err := uuid.Parse(v); return err == nil }
func validWorkStatus(v string) bool {
	switch v {
	case "scheduled", "due", "in_progress", "completed", "rework", "held_death_review", "canceled_death":
		return true
	default:
		return false
	}
}
func validSessionFilter(v string) bool {
	switch v {
	case "", domain.SessionMorning, domain.SessionAfternoon, domain.SessionEvening, domain.SessionUnscheduled:
		return true
	default:
		return false
	}
}
