// Package app is the Pen Visit use-case layer: the park head's list, one visit, and the submit
// that carries the video, over the repository port.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
	"github.com/vgoats/goatos/backend/internal/penvisits/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// Page sizes: a phone renders ~20 rows; nothing on this list needs more.
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// ErrIdempotencyKeyRequired reports a mutating call with no Idempotency-Key header.
var ErrIdempotencyKeyRequired = errors.New("pen visit: idempotency key required")

// Service is the module's application service.
type Service struct {
	repo   ports.Repository
	proofs ports.ProofValidator
	now    func() time.Time
}

// NewService wires the service.
func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithProofValidator wires proof-honesty validation (optional in pure unit tests, wired in
// production).
func (s *Service) WithProofValidator(v ports.ProofValidator) *Service {
	s.proofs = v
	return s
}

// WithClock pins the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Today is the IST business date the copy is composed against.
func (s *Service) Today() string { return biztime.BusinessDate(s.now()) }

// ClampPageSize bounds a requested page size.
func ClampPageSize(limit int) int {
	if limit <= 0 {
		return DefaultPageSize
	}
	if limit > MaxPageSize {
		return MaxPageSize
	}
	return limit
}

// ListMine pages the caller's own visits for one chip.
func (s *Service) ListMine(ctx context.Context, tenantID, userID, filterKey string, limit int, cursor string) (ports.Page, error) {
	return s.repo.ListMine(ctx, ports.ListParams{
		TenantID: tenantID,
		UserID:   userID,
		States:   domain.StatesForFilter(domain.FilterKeyOrDefault(filterKey)),
		Limit:    ClampPageSize(limit),
		Cursor:   strings.TrimSpace(cursor),
	})
}

// GetTask reads one visit the caller owns. Anyone else's visit reads as not found: the list
// never shows it, and a guessed id must not open it.
func (s *Service) GetTask(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error) {
	if !uuidutil.IsUUIDString(taskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	task, err := s.repo.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if !task.IsAssignee(actor) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	return task, nil
}

// Submit records the visit's video and completes the task. The proof is checked against the
// proof store BEFORE the write -- a finished, tenant-owned, in-app-camera video -- and the
// assignee/state rule is re-run under the row lock inside the repository.
func (s *Service) Submit(ctx context.Context, p ports.SubmitParams) (domain.Task, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return domain.Task{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(p.TaskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	p.ProofRef = strings.TrimSpace(p.ProofRef)
	if p.ProofRef == "" {
		return domain.Task{}, domain.ErrProofRequired
	}
	if !uuidutil.IsUUIDString(p.ProofRef) {
		return domain.Task{}, domain.ErrInvalidProof
	}
	if s.proofs != nil {
		if err := s.proofs.ValidateLiveCameraVideos(ctx, p.TenantID, []string{p.ProofRef}); err != nil {
			return domain.Task{}, err
		}
	}
	return s.repo.Submit(ctx, p)
}

// OpenCount answers the badge for one person: visits still to record.
func (s *Service) OpenCount(ctx context.Context, tenantID, userID string) (int, error) {
	return s.repo.OpenCount(ctx, tenantID, userID)
}

// OpenReasons is the badge split: the reasons of every visit still to record.
func (s *Service) OpenReasons(ctx context.Context, tenantID, userID string) ([][]string, error) {
	return s.repo.OpenReasons(ctx, tenantID, userID)
}
