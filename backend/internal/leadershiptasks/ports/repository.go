// Package ports declares the seams the Leadership Tasks module depends on.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
)

// Sentinel errors the adapters return; the transport maps them.
var (
	ErrTaskNotFound        = errors.New("leadership task: not found")
	ErrVersionConflict     = errors.New("leadership task: version conflict")
	ErrIdempotencyConflict = errors.New("leadership task: idempotency conflict")
	ErrInvalidArgument     = errors.New("leadership task: invalid argument")
	ErrInvalidAttachment   = errors.New("leadership task: invalid attachment")
)

// Assignee is one person a task may be raised for: a CXO with an active grant and an
// active roster profile.
type Assignee struct {
	UserID string
	Name   string
}

// ListParams selects one page of the caller's tasks.
//
// The caller sees the tasks THEY are party to: a director sees the ones they raised, a CXO
// sees the ones addressed to them. Both predicates are ORed so a person who is both (a CXO
// who also raised one) sees both sides on one list.
type ListParams struct {
	TenantID string
	UserID   string
	Statuses []string
	Limit    int
	Cursor   string
}

// Page is one page plus the whole-list counts the chips show.
type Page struct {
	Rows       []domain.Task
	NextCursor string
	// StatusCounts range over the SAME party predicate as the rows (never page-local, never
	// tenant-wide), keyed by status.
	StatusCounts map[string]int
	// UnseenCount is the number of tasks addressed to the caller they have not opened yet,
	// excluding cancelled ones. The drawer badge shows the same number.
	UnseenCount int
}

// RaiseParams raises a task.
type RaiseParams struct {
	TenantID       string
	ActorID        string
	AssigneeUserID string
	Title          string
	Body           string
	// Refs is what the client named; Attachments is what the service resolved against the
	// proof store (mime, size, duration read from the stored artifact). The repository
	// writes Attachments and never trusts Refs for anything but position and file name.
	Refs           []domain.AttachmentRef
	Attachments    []domain.Attachment
	IdempotencyKey string
}

// EditParams replaces the brief and the attachment list of an open task.
type EditParams struct {
	TenantID       string
	ActorID        string
	TaskID         string
	Title          string
	Body           string
	Refs           []domain.AttachmentRef
	Attachments    []domain.Attachment
	RowVersion     int
	IdempotencyKey string
}

// StatusParams moves a task along its status ladder.
type StatusParams struct {
	TenantID       string
	Actor          domain.Actor
	TaskID         string
	Status         string
	RowVersion     int
	IdempotencyKey string
}

// CommentParams sets the assignee's note on a task.
type CommentParams struct {
	TenantID       string
	Actor          domain.Actor
	TaskID         string
	Comment        string
	IdempotencyKey string
}

// Repository persists tasks.
type Repository interface {
	ListAssignees(ctx context.Context, tenantID string) ([]Assignee, error)
	ListTasks(ctx context.Context, p ListParams) (Page, error)
	GetTask(ctx context.Context, tenantID, taskID string) (domain.Task, error)
	Raise(ctx context.Context, p RaiseParams) (domain.Task, error)
	Edit(ctx context.Context, p EditParams) (domain.Task, error)
	ChangeStatus(ctx context.Context, p StatusParams) (domain.Task, error)
	SetComment(ctx context.Context, p CommentParams) (domain.Task, error)
	// MarkSeen stamps seen_at once; a replay is a no-op that returns the task.
	MarkSeen(ctx context.Context, tenantID, taskID, userID string) (domain.Task, error)
	// UnseenCount answers the drawer badge for one person.
	UnseenCount(ctx context.Context, tenantID, userID string) (int, error)
}

// AttachmentResolver checks the proofs a raise/edit names against the proof store and
// returns the stored facts (mime, size, duration) for each; any gap is ErrInvalidAttachment.
type AttachmentResolver interface {
	ResolveAttachments(ctx context.Context, tenantID, uploaderID string, refs []domain.AttachmentRef) ([]domain.Attachment, error)
}

// AttachmentDownloader mints a short-lived URL for a stored proof artifact after this module
// has already proved the caller may see the task that carries it.
type AttachmentDownloader interface {
	DownloadURL(ctx context.Context, tenantID, proofID string) (string, error)
}
