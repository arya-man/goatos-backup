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
	repo       ports.Repository
	proofs     ports.ProofValidator
	procedures ports.ProcedureSource
	now        func() time.Time
}

// NewService wires the service over its persistence and proof seams.
func NewService(repo ports.Repository, proofs ports.ProofValidator) *Service {
	return &Service{repo: repo, proofs: proofs, now: time.Now}
}

// WithProcedureSource attaches the authored procedure reader (THE TOXIN PROCEDURE IS AUTHORED,
// 2026-09-20). Without one the service runs the SEEDED procedure -- the seven steps the engine
// ran as Go constants -- so a deployment that has not wired the source behaves exactly as it did
// before, rather than serving a round with no steps at all.
func (s *Service) WithProcedureSource(src ports.ProcedureSource) *Service {
	s.procedures = src
	return s
}

// procedureFor resolves the procedure a ROUND runs: its own stamped version, never the latest.
// Rounds on the same page usually share a version, so the caller passes a per-request cache.
func (s *Service) procedureFor(ctx context.Context, tenantID string, version int, cache map[int]domain.Procedure) (domain.Procedure, error) {
	if version <= 0 {
		version = 1
	}
	if proc, ok := cache[version]; ok {
		return proc, nil
	}
	proc := domain.SeededProcedure()
	if s.procedures != nil {
		resolved, err := s.procedures.ProcedureVersion(ctx, tenantID, version)
		if err != nil {
			return domain.Procedure{}, err
		}
		proc = resolved
	}
	if cache != nil {
		cache[version] = proc
	}
	return proc, nil
}

// attachProcedures fills each row's procedure, reading each distinct version ONCE per request.
func (s *Service) attachProcedures(ctx context.Context, tenantID string, rows []ports.TaskRow) error {
	cache := map[int]domain.Procedure{}
	for i := range rows {
		proc, err := s.procedureFor(ctx, tenantID, rows[i].Task.SOPVersion, cache)
		if err != nil {
			return err
		}
		rows[i].Procedure = proc
	}
	return nil
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
	page, err := s.repo.ListTasks(ctx, ports.ListTasksParams{
		TenantID: tenantID,
		Statuses: cleaned,
		Limit:    ClampPageSize(limit),
		Cursor:   strings.TrimSpace(cursor),
	})
	if err != nil {
		return page, err
	}
	if err := s.attachProcedures(ctx, tenantID, page.Rows); err != nil {
		return ports.TaskPage{}, err
	}
	return page, nil
}

// GetTask reads one task with its step completions and the procedure that round runs.
func (s *Service) GetTask(ctx context.Context, tenantID, taskID string) (ports.TaskRow, error) {
	row, err := s.repo.GetTask(ctx, tenantID, strings.TrimSpace(taskID))
	if err != nil {
		return row, err
	}
	rows := []ports.TaskRow{row}
	if err := s.attachProcedures(ctx, tenantID, rows); err != nil {
		return ports.TaskRow{}, err
	}
	return rows[0], nil
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
	// The step is judged against THIS ROUND's procedure, not the latest one: a farm that
	// published a shorter test this morning must not invalidate a round opened yesterday.
	row, err := s.repo.GetTask(ctx, p.TenantID, p.TaskID)
	if err != nil {
		return ports.TaskRow{}, err
	}
	proc, err := s.procedureFor(ctx, p.TenantID, row.Task.SOPVersion, nil)
	if err != nil {
		return ports.TaskRow{}, err
	}
	spec, ok := proc.StepSpecFor(p.StepNo)
	if !ok {
		return ports.TaskRow{}, domain.ErrUnknownStep
	}
	if spec.Kind != domain.StepKindVideo {
		// A waiting row has nothing to complete, and the READING step goes through
		// SubmitReading with the reading attached.
		return ports.TaskRow{}, domain.ErrStepNotCompletable
	}
	p.Procedure = proc
	if err := s.proofs.ValidateToxinStepVideo(ctx, p.TenantID, p.ProofRef); err != nil {
		return ports.TaskRow{}, err
	}
	p.Now = s.now()
	out, err := s.repo.CompleteStep(ctx, p)
	if err != nil {
		return out, err
	}
	out.Procedure = proc
	return out, nil
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
	row, err := s.repo.GetTask(ctx, p.TenantID, p.TaskID)
	if err != nil {
		return ports.TaskRow{}, err
	}
	p.Procedure, err = s.procedureFor(ctx, p.TenantID, row.Task.SOPVersion, nil)
	if err != nil {
		return ports.TaskRow{}, err
	}
	p.Now = s.now()
	out, err := s.repo.SubmitReading(ctx, p)
	if err != nil {
		return out, err
	}
	out.Procedure = p.Procedure
	return out, nil
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
	row, err := s.repo.GetTask(ctx, p.TenantID, p.TaskID)
	if err != nil {
		return ports.TaskRow{}, err
	}
	proc, err := s.procedureFor(ctx, p.TenantID, row.Task.SOPVersion, nil)
	if err != nil {
		return ports.TaskRow{}, err
	}
	out, err := s.repo.RecordVerdict(ctx, p)
	if err != nil {
		return out, err
	}
	out.Procedure = proc
	return out, nil
}
