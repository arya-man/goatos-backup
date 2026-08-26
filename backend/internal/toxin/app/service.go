// Package app holds the toxin module's application service: the 7-step aflatoxin test
// flow (per-step video proof, hard-blocked waits), the step-7 strip reading, and the
// CEO/CXO-only accept/reject.
package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/toxin/domain"
	"github.com/vgoats/goatos/backend/internal/toxin/ports"
)

// DefaultPageSize matches the mobile keyset page contract (~20 rows per page).
const DefaultPageSize = 20

// MaxPageSize bounds a caller-supplied limit.
const MaxPageSize = 50

// Service is the toxin module's application service.
type Service struct {
	repo   ports.Repository
	proofs ports.ProofValidator
	now    func() time.Time
}

// NewService wires the service over its persistence and proof seams.
func NewService(repo ports.Repository, proofs ports.ProofValidator) *Service {
	return &Service{repo: repo, proofs: proofs, now: time.Now}
}

// WithClock pins the clock for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// ClampPageSize normalizes a caller-supplied page size onto the keyset contract.
func ClampPageSize(limit int) int {
	if limit <= 0 {
		return DefaultPageSize
	}
	if limit > MaxPageSize {
		return MaxPageSize
	}
	return limit
}

// ListTasks pages the tenant's toxin tests by keyset, newest first.
func (s *Service) ListTasks(ctx context.Context, tenantID string, statuses []string, limit int, cursor string) (ports.TaskPage, error) {
	cleaned := make([]string, 0, len(statuses))
	for _, st := range statuses {
		switch st {
		case domain.StatusInProgress, domain.StatusPendingReview, domain.StatusAccepted, domain.StatusCancelled:
			cleaned = append(cleaned, st)
		case "":
		default:
			return ports.TaskPage{}, BadRequest("invalid_status", "That status filter is not recognised.")
		}
	}
	return s.repo.ListTasks(ctx, ports.ListTasksParams{
		TenantID: tenantID,
		Statuses: cleaned,
		Limit:    ClampPageSize(limit),
		Cursor:   strings.TrimSpace(cursor),
	})
}

// ReportWindowDays is the analytics window behind the KPI strip, the weekly series and
// the supplier rollup. Thirty days is the farm's own review cadence; the loads TABLE is
// deliberately unwindowed so an old untested load cannot fall off the page.
const ReportWindowDays = 30

// LoadReport serves the /feed/toxin leadership read.
func (s *Service) LoadReport(ctx context.Context, tenantID, filter string, limit int, cursor string) (ports.ReportPage, error) {
	return s.repo.LoadReport(ctx, ports.ReportParams{
		TenantID:   tenantID,
		Filter:     domain.ReportFilterKeyOrDefault(filter),
		Limit:      ClampPageSize(limit),
		Cursor:     strings.TrimSpace(cursor),
		WindowDays: ReportWindowDays,
	})
}

// GetTask reads one task with its step completions.
func (s *Service) GetTask(ctx context.Context, tenantID, taskID string) (ports.TaskRow, error) {
	return s.repo.GetTask(ctx, tenantID, strings.TrimSpace(taskID))
}

// Now exposes the service clock so transports compose wait copy off the same instant
// the gates were checked against.
func (s *Service) Now() time.Time { return s.now() }

// CompleteStep records one working step's video. The proof is validated BEFORE the
// write; order and wait gates are re-checked inside the transaction under the row lock.
func (s *Service) CompleteStep(ctx context.Context, p ports.CompleteStepParams) (ports.TaskRow, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return ports.TaskRow{}, ErrIdempotencyKeyRequired
	}
	p.ProofRef = strings.TrimSpace(p.ProofRef)
	if p.ProofRef == "" {
		return ports.TaskRow{}, domain.ErrProofRequired
	}
	spec, ok := domain.StepSpecFor(p.StepNo)
	if !ok {
		return ports.TaskRow{}, domain.ErrUnknownStep
	}
	if spec.Kind != domain.StepKindVideo {
		// The wait row has nothing to complete, and step 7 goes through SubmitReading
		// with the reading attached.
		return ports.TaskRow{}, domain.ErrStepNotCompletable
	}
	if err := s.proofs.ValidateToxinStepVideo(ctx, p.TenantID, p.ProofRef); err != nil {
		return ports.TaskRow{}, err
	}
	p.Now = s.now()
	return s.repo.CompleteStep(ctx, p)
}

// SubmitReading records step 7: the strip photo plus the reading. An Invalid strip
// cancels the round and mints its retest.
func (s *Service) SubmitReading(ctx context.Context, p ports.SubmitParams) (ports.TaskRow, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return ports.TaskRow{}, ErrIdempotencyKeyRequired
	}
	p.StripPhotoRef = strings.TrimSpace(p.StripPhotoRef)
	if p.StripPhotoRef == "" {
		return ports.TaskRow{}, domain.ErrProofRequired
	}
	if err := domain.ValidateOutcome(p.Outcome); err != nil {
		return ports.TaskRow{}, err
	}
	if err := s.proofs.ValidateToxinStripPhoto(ctx, p.TenantID, p.StripPhotoRef); err != nil {
		return ports.TaskRow{}, err
	}
	p.Now = s.now()
	return s.repo.SubmitReading(ctx, p)
}

// RecordVerdict records the CEO/CXO accept/reject, fenced on the row_version the
// reviewer had on screen. A reject cancels the round and mints its retest.
func (s *Service) RecordVerdict(ctx context.Context, p ports.VerdictParams) (ports.TaskRow, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return ports.TaskRow{}, ErrIdempotencyKeyRequired
	}
	// Decision/reason validity is pure; check it before touching storage. The
	// current-status half is re-checked inside the transaction.
	if _, err := domain.VerdictDecision(domain.StatusPendingReview, p.Decision, p.Reason); err != nil {
		return ports.TaskRow{}, err
	}
	p.TaskID = strings.TrimSpace(p.TaskID)
	p.Reason = strings.TrimSpace(p.Reason)
	return s.repo.RecordVerdict(ctx, p)
}
