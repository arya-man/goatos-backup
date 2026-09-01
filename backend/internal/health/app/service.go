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
	AgeBand      string
	OperatorID   string
	ParkID       string
	ShedID       string
	MediaRefs    []string
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
	if in.ProofRef != "" {
		subject := "Day " + strconv.Itoa(res.DayNo) + " · " + res.DiseaseName + " · " + res.GoatDisplayID
		if loc := (oploc.OperationalLocation{ShedName: res.ShedLabel, PartitionLabel: res.PartitionLabel}).Display(); loc != "" {
			subject += " · " + loc
		}
		if err := s.enqueuer.EnqueueTreatmentVerification(ctx, TreatmentVerificationEnqueueRequest{
			TenantID:     in.TenantID,
			SessionID:    in.SessionID,
			AgeBand:      res.AgeBand,
			OperatorID:   in.ActorID,
			ParkID:       res.ParkID,
			ShedID:       res.ShedID,
			MediaRefs:    []string{in.ProofRef},
			SubjectLabel: subject,
			CapturedAt:   res.CompletedAt,
			// Keyed to the SESSION + proof so a retry collapses onto one queue item while a
			// rework re-shoot (new proof ref) creates the replacement item.
			IdempotencyKey: "health-treatment-verification:" + in.SessionID + ":" + in.ProofRef,
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
