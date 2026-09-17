// Package ports declares the seams the Leadership Tasks module depends on.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
)

// Sentinel errors the adapters return; the transport maps them.
var (
	ErrTaskNotFound        = errors.New("leadership task: not found")
	ErrVersionConflict     = errors.New("leadership task: version conflict")
	ErrIdempotencyConflict = errors.New("leadership task: idempotency conflict")
	ErrInvalidArgument     = errors.New("leadership task: invalid argument")
	ErrInvalidAttachment   = errors.New("leadership task: invalid attachment")
)

// Assignee is one active app-backed worker a task may be raised for.
type Assignee struct {
	UserID string
	Name   string
	// Title is what the picker SHOWS (maintainer request 2026-09-11): the person's HRMS
	// business title ("CEO", "Preventive Care Director"), falling back to their designation's
	// catalog label. Never blank for a listed assignee.
	Title string
}

// ListParams selects one page of the caller's tasks.
//
// The caller sees the tasks THEY are party to: the ones they raised and the ones addressed
// to them. Both predicates are ORed so a person who is both sees both sides on one list.
type ListParams struct {
	TenantID string
	UserID   string
	Scope    string
	Statuses []string
	Limit    int
	Cursor   string

	// Query is the trimmed free-text box: a case-insensitive substring of the title OR the
	// body, served by the pg_trgm GIN indexes on both columns (migration 000345). When the
	// same text is a bare integer the list ALSO matches task_no exactly, so typing "15" finds
	// "#15" -- that is how a leader refers to a task out loud.
	Query string
	// QueryTaskNo is set by the app layer when Query parses as a bare positive integer. The
	// repository binds it as the extra task_no arm of the search predicate; nil means the text
	// is not a number and only title/body are searched.
	QueryTaskNo *int64
	// AssigneeUserID / RaisedBy narrow the list to one person. The app layer DROPS whichever
	// one the active scope already pins (assigned_to_me pins the assignee, assigned_by_me pins
	// the raiser), so the same filter never appears twice in one predicate. The scope-count
	// query re-applies that rule per branch, so each tab's badge counts what that tab shows.
	AssigneeUserID string
	RaisedBy       string
	// DeadlineFrom/DeadlineTo and RaisedFrom/RaisedTo are INCLUSIVE instants on both ends. Each
	// pair is required together (verification/ports/ports.go:53-98 precedent): a half-open range
	// would have to invent the missing end, and the two plausible inventions mean opposite
	// things to a reader. A deadline range never matches a task with no deadline.
	DeadlineFrom *time.Time
	DeadlineTo   *time.Time
	RaisedFrom   *time.Time
	RaisedTo     *time.Time
	// Sort is a CLOSED enum; the empty string means SortRaisedAtDesc.
	Sort string
}

// The list's four sort orders. A cursor carries the name of the sort it was minted under, so
// changing the sort mid-page is refused rather than served as a wrong page.
const (
	SortRaisedAtDesc = "raised_at_desc"
	SortRaisedAtAsc  = "raised_at_asc"
	SortDeadlineAsc  = "deadline_asc"
	SortDeadlineDesc = "deadline_desc"
)

// SortKeys is the order the picker offers.
var SortKeys = []string{SortRaisedAtDesc, SortRaisedAtAsc, SortDeadlineAsc, SortDeadlineDesc}

// IsSortKey reports whether a requested sort is one of the four. Unknown values are an
// explicit 400 (invalid_sort), never a silent fallback: a leader who asked for the deadline
// order and silently got the raise order would read the wrong list as the truth.
func IsSortKey(key string) bool {
	switch key {
	case SortRaisedAtDesc, SortRaisedAtAsc, SortDeadlineAsc, SortDeadlineDesc:
		return true
	}
	return false
}

// SortOrDefault normalizes the empty string to the default sort. It does NOT normalize an
// unknown value; callers validate with IsSortKey first.
func SortOrDefault(key string) string {
	if key == "" {
		return SortRaisedAtDesc
	}
	return key
}

// MaxQueryLen bounds the free-text box. Longer text is a 400 (invalid_query), not a silent
// truncation that would return rows the caller did not ask for.
const MaxQueryLen = 120

// Page is one page plus the whole-list counts the chips show.
type Page struct {
	Rows       []domain.Task
	NextCursor string
	// StatusCounts range over the SAME party predicate as the rows (never page-local, never
	// tenant-wide), keyed by status.
	StatusCounts map[string]int
	ScopeCounts  map[string]int
	// UnseenCount is the number of tasks addressed to the caller they have not opened yet,
	// excluding cancelled ones. The drawer badge shows the same number.
	UnseenCount int
}

// RaiseParams raises a task.
type RaiseParams struct {
	TenantID         string
	ActorID          string
	ActorDesignation string
	AssigneeUserID   string
	Title            string
	Body             string
	// Refs is what the client named; Attachments is what the service resolved against the
	// proof store (mime, size, duration read from the stored artifact). The repository
	// writes Attachments and never trusts Refs for anything but position and file name.
	Refs           []domain.AttachmentRef
	Attachments    []domain.Attachment
	IdempotencyKey string
	// DeadlineAt is the date and time the raiser asked for the task by (domain/deadline.go).
	// The task form REQUIRES it; DeadlineOptional is set only by a programmatic raise that has
	// no form to ask (the Work Board flag), whose task then shows no counter until its raiser
	// sets one.
	DeadlineAt       *time.Time
	DeadlineOptional bool
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
	// DeadlineAt replaces the deadline when present. Nil KEEPS the stored deadline: an older
	// phone that does not know the field must not wipe it by editing the brief.
	DeadlineAt *time.Time
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

// CommentParams appends one note to a task (and, for the assignee, overwrites the legacy
// single-comment field).
type CommentParams struct {
	TenantID string
	Actor    domain.Actor
	TaskID   string
	Comment  string
	// MentionUserIDs are the EXPLICIT mention targets the client sent alongside the text
	// (domain/mentions.go). The app layer normalizes them; the repository re-validates every
	// one under the task's row lock against domain.Task.CanRead before storing a row, so an id
	// from a stale or hostile client can never reach someone who cannot see the task.
	MentionUserIDs []string
	IdempotencyKey string
}

// Repository persists tasks.
type Repository interface {
	ListAssignees(ctx context.Context, tenantID string) ([]Assignee, error)
	// ListMentionableUsers answers the `@` autocomplete for ONE task: everyone who can already
	// read that task. Not the same list as ListAssignees (see domain.MentionableUser).
	ListMentionableUsers(ctx context.Context, tenantID, taskID string) ([]domain.MentionableUser, error)
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
	DownloadArtifact(ctx context.Context, tenantID, proofID string) (proofdomain.Artifact, string, error)
}
