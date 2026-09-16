// Package app is the Pen Routines use-case layer: the assignee's list, one task, the check-in
// and the submit that carries the answers and captures; and the CEO's authoring of the rule.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/penroutines/domain"
	"github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// Page sizes: a phone renders ~20 rows; nothing on this list needs more.
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// ErrIdempotencyKeyRequired reports a mutating call with no Idempotency-Key header.
var ErrIdempotencyKeyRequired = errors.New("pen routine: idempotency key required")

// Service is the assignee-facing application service.
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

// ListMine pages the caller's own tasks for one chip.
func (s *Service) ListMine(ctx context.Context, tenantID, userID, filterKey string, limit int, cursor string) (ports.Page, error) {
	return s.repo.ListMine(ctx, ports.ListParams{
		TenantID: tenantID,
		UserID:   userID,
		States:   domain.StatesForFilter(domain.FilterKeyOrDefault(filterKey)),
		Limit:    ClampPageSize(limit),
		Cursor:   strings.TrimSpace(cursor),
	})
}

// GetTask reads one task the caller is assigned to. Anyone else's task reads as not found:
// the list never shows it, and a guessed id must not open it.
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

// RecordPresence records a check-in or check-out on a task.
func (s *Service) RecordPresence(ctx context.Context, p ports.PresenceParams) (domain.Task, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return domain.Task{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(p.TaskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	p.EventType = strings.TrimSpace(p.EventType)
	if p.EventType != domain.PresenceEnter && p.EventType != domain.PresenceLeave {
		return domain.Task{}, domain.ErrPresenceState
	}
	if p.CapturedAt.IsZero() {
		p.CapturedAt = s.now()
	}
	return s.repo.RecordPresence(ctx, p)
}

// Submit records the answers and captures. Every capture is checked against the proof store
// BEFORE the write -- finished, tenant-owned, in-app-camera, of the kind the submit claims --
// and the answer / count / presence / assignee rules are re-run under the row lock inside
// the repository.
func (s *Service) Submit(ctx context.Context, p ports.SubmitParams) (domain.Task, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return domain.Task{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(p.TaskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	proofs := make([]domain.ProofItem, 0, len(p.Proofs))
	for _, item := range p.Proofs {
		item.Ref = strings.TrimSpace(item.Ref)
		item.Kind = strings.TrimSpace(item.Kind)
		if item.Ref == "" || !uuidutil.IsUUIDString(item.Ref) {
			return domain.Task{}, domain.ErrInvalidProof
		}
		if item.Kind != domain.ProofKindPhoto && item.Kind != domain.ProofKindVideo {
			return domain.Task{}, domain.ErrInvalidProof
		}
		proofs = append(proofs, item)
	}
	p.Proofs = proofs
	if s.proofs != nil && len(proofs) > 0 {
		if err := s.proofs.ValidateLiveCameraProofs(ctx, p.TenantID, proofs); err != nil {
			return domain.Task{}, err
		}
	}
	return s.repo.Submit(ctx, p)
}

// OpenCount answers the badge for one person: checks still to do.
func (s *Service) OpenCount(ctx context.Context, tenantID, userID string) (int, error) {
	return s.repo.OpenCount(ctx, tenantID, userID)
}
