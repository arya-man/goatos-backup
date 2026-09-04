// Package app is the Leadership Tasks use-case layer: validation, authority checks that
// need the stored row, and the attachment handshake with the proof store, over the
// repository port.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
)

// Page sizes: a phone renders ~20 rows; nothing on this list needs more.
const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

// ErrIdempotencyKeyRequired reports a mutating call with no Idempotency-Key header.
var ErrIdempotencyKeyRequired = errors.New("leadership task: idempotency key required")

// Service is the module's application service.
type Service struct {
	repo        ports.Repository
	attachments ports.AttachmentResolver
	downloader  ports.AttachmentDownloader
	now         func() time.Time
}

// NewService wires the service.
func NewService(repo ports.Repository, attachments ports.AttachmentResolver) *Service {
	return &Service{repo: repo, attachments: attachments, now: time.Now}
}

// WithAttachmentDownloader wires proof URL minting after leadership-task authorization.
func (s *Service) WithAttachmentDownloader(downloader ports.AttachmentDownloader) *Service {
	s.downloader = downloader
	return s
}

// WithClock pins the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Now is the service clock.
func (s *Service) Now() time.Time { return s.now() }

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

// ListAssignees is the raise form's picker: every CXO a task may be addressed to.
func (s *Service) ListAssignees(ctx context.Context, tenantID string) ([]ports.Assignee, error) {
	return s.repo.ListAssignees(ctx, tenantID)
}

// ListTasks pages the caller's tasks for one chip.
func (s *Service) ListTasks(ctx context.Context, tenantID, userID, filterKey string, limit int, cursor string) (ports.Page, error) {
	return s.repo.ListTasks(ctx, ports.ListParams{
		TenantID: tenantID,
		UserID:   userID,
		Statuses: domain.StatusesForFilter(domain.FilterKeyOrDefault(filterKey)),
		Limit:    ClampPageSize(limit),
		Cursor:   strings.TrimSpace(cursor),
	})
}

// GetTask reads one task the caller is party to. A task the caller neither raised nor was
// addressed to reads as not found: the list never shows it, and a guessed id must not open it.
func (s *Service) GetTask(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error) {
	if !uuidutil.IsUUIDString(taskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	task, err := s.repo.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if !task.IsRaiser(actor) && !task.IsAssignee(actor) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	return task, nil
}

// Raise validates and records a new task. The attachments are resolved against the proof
// store BEFORE the write so a task never points at bytes that are not there.
func (s *Service) Raise(ctx context.Context, p ports.RaiseParams) (domain.Task, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return domain.Task{}, ErrIdempotencyKeyRequired
	}
	p.Title = strings.TrimSpace(p.Title)
	p.Body = strings.TrimSpace(p.Body)
	p.AssigneeUserID = strings.TrimSpace(p.AssigneeUserID)
	if p.AssigneeUserID == "" || !uuidutil.IsUUIDString(p.AssigneeUserID) {
		return domain.Task{}, domain.ErrAssigneeRequired
	}
	if p.AssigneeUserID == p.ActorID {
		return domain.Task{}, domain.ErrSelfAssignment
	}
	if err := domain.ValidateBrief(p.Title, p.Body, p.Refs); err != nil {
		return domain.Task{}, err
	}
	resolved, err := s.resolveAttachments(ctx, p.TenantID, p.ActorID, p.Refs)
	if err != nil {
		return domain.Task{}, err
	}
	p.Attachments = resolved
	return s.repo.Raise(ctx, p)
}

// Edit replaces the brief and attachment list of a task the caller raised.
func (s *Service) Edit(ctx context.Context, p ports.EditParams) (domain.Task, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return domain.Task{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(p.TaskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	p.Title = strings.TrimSpace(p.Title)
	p.Body = strings.TrimSpace(p.Body)
	if err := domain.ValidateBrief(p.Title, p.Body, p.Refs); err != nil {
		return domain.Task{}, err
	}
	resolved, err := s.resolveAttachments(ctx, p.TenantID, p.ActorID, p.Refs)
	if err != nil {
		return domain.Task{}, err
	}
	p.Attachments = resolved
	return s.repo.Edit(ctx, p)
}

// ChangeStatus moves a task along its ladder. The transition rule is checked again under
// the row lock inside the repository; this is the fast refusal for a malformed request.
func (s *Service) ChangeStatus(ctx context.Context, p ports.StatusParams) (domain.Task, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return domain.Task{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(p.TaskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	p.Status = strings.TrimSpace(p.Status)
	if !domain.IsKnownStatus(p.Status) {
		return domain.Task{}, domain.ErrInvalidStatus
	}
	return s.repo.ChangeStatus(ctx, p)
}

// SetComment records the assignee's note back on the task.
func (s *Service) SetComment(ctx context.Context, p ports.CommentParams) (domain.Task, error) {
	if strings.TrimSpace(p.IdempotencyKey) == "" {
		return domain.Task{}, ErrIdempotencyKeyRequired
	}
	if !uuidutil.IsUUIDString(p.TaskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	p.Comment = strings.TrimSpace(p.Comment)
	if err := domain.ValidateComment(p.Comment); err != nil {
		return domain.Task{}, err
	}
	return s.repo.SetComment(ctx, p)
}

// MarkSeen stamps the task seen by its assignee. Anyone else opening it is a no-op that
// returns the task unchanged -- the raiser reading their own task is not "seen by the CXO".
func (s *Service) MarkSeen(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error) {
	task, err := s.GetTask(ctx, tenantID, actor, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	if !task.IsAssignee(actor) || task.SeenAt != nil {
		return task, nil
	}
	return s.repo.MarkSeen(ctx, tenantID, taskID, actor.UserID)
}

// AttachmentDownloadURL returns a URL only when the caller is party to the task and the proof
// is one of that task's stored attachments.
func (s *Service) AttachmentDownloadURL(ctx context.Context, tenantID string, actor domain.Actor, taskID, proofID string) (string, error) {
	if !uuidutil.IsUUIDString(proofID) {
		return "", ports.ErrTaskNotFound
	}
	task, err := s.GetTask(ctx, tenantID, actor, taskID)
	if err != nil {
		return "", err
	}
	for _, attachment := range task.Attachments {
		if strings.TrimSpace(attachment.ProofID) == strings.TrimSpace(proofID) {
			if s.downloader == nil {
				return "", ports.ErrInvalidAttachment
			}
			return s.downloader.DownloadURL(ctx, tenantID, proofID) // scale-guard:ignore: proofID is validated against this one task's already-loaded attachment set; only the selected attachment receives a signed URL.
		}
	}
	return "", ports.ErrTaskNotFound
}

// UnseenCount answers the drawer badge.
func (s *Service) UnseenCount(ctx context.Context, tenantID, userID string) (int, error) {
	return s.repo.UnseenCount(ctx, tenantID, userID)
}

// resolveAttachments turns the client's refs into stored facts. With no resolver wired
// (tests), the refs pass through with the kind and name the client gave.
func (s *Service) resolveAttachments(ctx context.Context, tenantID, uploaderID string, refs []domain.AttachmentRef) ([]domain.Attachment, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if s.attachments == nil {
		out := make([]domain.Attachment, 0, len(refs))
		for i, ref := range refs {
			out = append(out, domain.Attachment{ProofID: ref.ProofID, Kind: ref.Kind, FileName: strings.TrimSpace(ref.FileName), Position: i})
		}
		return out, nil
	}
	return s.attachments.ResolveAttachments(ctx, tenantID, uploaderID, refs)
}
