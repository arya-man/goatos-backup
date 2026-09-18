// Package app is the Leadership Tasks use-case layer: validation, authority checks that
// need the stored row, and the attachment handshake with the proof store, over the
// repository port.
package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
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

// ListAssignees is the raise form's picker: every active worker a task may be addressed to.
func (s *Service) ListAssignees(ctx context.Context, tenantID string) ([]ports.Assignee, error) {
	return s.repo.ListAssignees(ctx, tenantID)
}

// ListRequest is one list read as the transport received it: raw strings and the actor. The
// app layer owns validation and normalization so the codes the screen reads (invalid_sort,
// invalid_date_range, invalid_filter, invalid_query) are decided in ONE place and can be
// tested without a request.
type ListRequest struct {
	TenantID string
	UserID   string
	ScopeKey string
	// FilterKey and ScopeKey are normalized (an unknown value falls back), because a stale
	// client must keep working. Everything below is VALIDATED (an unknown value is a 400),
	// because a leader who asked for a narrowed list and silently got the whole one would read
	// the wrong list as the truth.
	FilterKey      string
	Limit          int
	Cursor         string
	Query          string
	AssigneeUserID string
	RaisedBy       string
	DeadlineFrom   string
	DeadlineTo     string
	RaisedFrom     string
	RaisedTo       string
	Sort           string
	Actor          domain.Actor
}

// ListTasks pages the caller's tasks for one chip, under the request's filters and sort.
func (s *Service) ListTasks(ctx context.Context, req ListRequest) (ports.Page, error) {
	params, err := s.listParams(req)
	if err != nil {
		return ports.Page{}, err
	}
	return s.repo.ListTasks(ctx, params)
}

// listParams validates the request and lowers it onto the repository port.
func (s *Service) listParams(req ListRequest) (ports.ListParams, error) {
	scope := domain.ScopeKeyOrDefault(req.ScopeKey, req.Actor)
	sortKey := ports.SortOrDefault(strings.TrimSpace(req.Sort))
	if !ports.IsSortKey(sortKey) {
		return ports.ListParams{}, BadRequest("invalid_sort", "That sort order is not one of the ones on offer.")
	}
	text := strings.TrimSpace(req.Query)
	if len([]rune(text)) > ports.MaxQueryLen {
		return ports.ListParams{}, BadRequest("invalid_query", "Search with fewer than 120 characters.")
	}
	var taskNo *int64
	if n, err := strconv.ParseInt(text, 10, 64); err == nil && n >= 1 {
		// "15" is how a leader names task #15 out loud; the number is matched as well as the text.
		taskNo = &n
	}
	assignee, err := optionalUUID(req.AssigneeUserID)
	if err != nil {
		return ports.ListParams{}, err
	}
	raiser, err := optionalUUID(req.RaisedBy)
	if err != nil {
		return ports.ListParams{}, err
	}
	// A scope that already pins a person ignores the matching filter rather than refusing it:
	// the filter bar is shared across the tabs, and switching tab must not error.
	switch scope {
	case domain.ScopeAssignedToMe:
		assignee = ""
	case domain.ScopeAssignedByMe:
		raiser = ""
	}
	deadlineFrom, deadlineTo, err := instantRange(req.DeadlineFrom, req.DeadlineTo)
	if err != nil {
		return ports.ListParams{}, err
	}
	raisedFrom, raisedTo, err := instantRange(req.RaisedFrom, req.RaisedTo)
	if err != nil {
		return ports.ListParams{}, err
	}
	filterKey := domain.FilterKeyOrDefault(req.FilterKey)
	now := s.now()
	var overdueBefore *time.Time
	if filterKey == domain.FilterOverdue {
		overdueBefore = &now
	}
	return ports.ListParams{
		TenantID:       req.TenantID,
		UserID:         req.UserID,
		Scope:          scope,
		Statuses:       domain.StatusesForFilter(filterKey),
		OverdueBefore:  overdueBefore,
		OverdueAt:      now,
		Limit:          ClampPageSize(req.Limit),
		Cursor:         strings.TrimSpace(req.Cursor),
		Query:          text,
		QueryTaskNo:    taskNo,
		AssigneeUserID: assignee,
		RaisedBy:       raiser,
		DeadlineFrom:   deadlineFrom,
		DeadlineTo:     deadlineTo,
		RaisedFrom:     raisedFrom,
		RaisedTo:       raisedTo,
		Sort:           sortKey,
	}, nil
}

// optionalUUID accepts an absent person filter and refuses a malformed one.
func optionalUUID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if !uuidutil.IsUUIDString(trimmed) {
		return "", BadRequest("invalid_filter", "That filter is not valid. Pick the person from the list.")
	}
	return trimmed, nil
}

// instantRange reads an INCLUSIVE date range. BOTH ends are required together
// (verification/ports/ports.go:53-98 precedent): a half-open range would have to invent the
// missing end, and "today" and "the beginning of time" mean opposite things to a reader. A
// bare date is read as that whole day in UTC -- the start for the lower end, the last instant
// for the upper -- so "from 2026-09-01 to 2026-09-01" is one full day, not an empty range.
func instantRange(fromRaw, toRaw string) (*time.Time, *time.Time, error) {
	from := strings.TrimSpace(fromRaw)
	to := strings.TrimSpace(toRaw)
	if from == "" && to == "" {
		return nil, nil, nil
	}
	if from == "" || to == "" {
		return nil, nil, BadRequest("invalid_date_range", "Give both a start and an end date for that range.")
	}
	lower, err := parseRangeEnd(from, false)
	if err != nil {
		return nil, nil, err
	}
	upper, err := parseRangeEnd(to, true)
	if err != nil {
		return nil, nil, err
	}
	if lower.After(*upper) {
		return nil, nil, BadRequest("invalid_date_range", "That range starts after it ends.")
	}
	return lower, upper, nil
}

// parseRangeEnd reads one end of a range: an RFC3339 instant, or a bare YYYY-MM-DD widened to
// the start or the very end of that UTC day.
func parseRangeEnd(raw string, upper bool) (*time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		utc := t.UTC()
		return &utc, nil
	}
	day, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		// The screen reads the code and the farm-worded message; the parse cause rides along
		// for the request log, the way the repository surfaces a bad cursor.
		return nil, fmt.Errorf("%w: %q is neither an RFC3339 instant nor a YYYY-MM-DD date: %v",
			BadRequest("invalid_date_range", "That date is not valid."), raw, err)
	}
	if upper {
		end := day.Add(24*time.Hour - time.Nanosecond)
		return &end, nil
	}
	return &day, nil
}

// GetTask reads one task the caller is party to, or a task the caller may monitor through
// the Team progress scope. A task outside both shapes reads as not found.
func (s *Service) GetTask(ctx context.Context, tenantID string, actor domain.Actor, taskID string) (domain.Task, error) {
	if !uuidutil.IsUUIDString(taskID) {
		return domain.Task{}, ports.ErrTaskNotFound
	}
	task, err := s.repo.GetTask(ctx, tenantID, taskID)
	if err != nil {
		return domain.Task{}, err
	}
	// ONE visibility rule, domain.Task.CanRead: raiser, assignee, or a monitor. The mention
	// check reads the same predicate, so a mention can never reach past the task itself.
	if !task.CanRead(actor) {
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
	// The raise instant is the service clock; the repository stamps raised_at from the same
	// clock, so a deadline that passes here is after the stored raise too.
	if err := domain.ValidateDeadline(p.DeadlineAt, s.now(), !p.DeadlineOptional); err != nil {
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
	// A present deadline is checked against the stored raise instant under the row lock in
	// the repository, where the row is; nil keeps what is stored.
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
	// The mention list is normalized here (trimmed, self-mention dropped, deduped, bounded)
	// and MALFORMED ids are refused up front. Whether each id names someone who may see this
	// task is decided in the repository, under the task's row lock, where the row is.
	mentions, err := domain.NormalizeMentionTargets(p.MentionUserIDs, p.Actor.UserID)
	if err != nil {
		return domain.Task{}, err
	}
	for _, id := range mentions {
		if !uuidutil.IsUUIDString(id) {
			return domain.Task{}, domain.ErrInvalidMention
		}
	}
	// A mention lives ON a note. A mention list with no words to attach it to is a client
	// defect, and storing it would leave a notification pointing at nothing to read.
	if len(mentions) > 0 && p.Comment == "" {
		return domain.Task{}, BadRequest("mention_without_note", "Write the note before naming someone in it.")
	}
	p.MentionUserIDs = mentions
	return s.repo.SetComment(ctx, p)
}

// ListMentionableUsers answers the `@` autocomplete for one task. The caller must be able to
// read the task itself first (GetTask's rule), so the list cannot be used to enumerate the
// leadership roster from a task nobody showed you.
func (s *Service) ListMentionableUsers(ctx context.Context, tenantID string, actor domain.Actor, taskID string) ([]domain.MentionableUser, error) {
	if _, err := s.GetTask(ctx, tenantID, actor, taskID); err != nil {
		return nil, err
	}
	return s.repo.ListMentionableUsers(ctx, tenantID, strings.TrimSpace(taskID))
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

// AttachmentDownload carries the selected proof artifact plus its explicit-open URL.
type AttachmentDownload struct {
	Artifact proofdomain.Artifact
	URL      string
}

// AttachmentDownloadURL returns an artifact and URL only when the caller can read the task and
// the proof is one of that task's stored attachments.
func (s *Service) AttachmentDownloadURL(ctx context.Context, tenantID string, actor domain.Actor, taskID, proofID string) (AttachmentDownload, error) {
	if !uuidutil.IsUUIDString(proofID) {
		return AttachmentDownload{}, ports.ErrTaskNotFound
	}
	task, err := s.GetTask(ctx, tenantID, actor, taskID)
	if err != nil {
		return AttachmentDownload{}, err
	}
	if s.downloader == nil {
		return AttachmentDownload{}, ports.ErrInvalidAttachment
	}
	proofID = strings.TrimSpace(proofID)
	found := false
	for _, attachment := range task.Attachments {
		if strings.TrimSpace(attachment.ProofID) == proofID {
			found = true
			break
		}
	}
	if !found {
		return AttachmentDownload{}, ports.ErrTaskNotFound
	}
	artifact, url, err := s.downloader.DownloadArtifact(ctx, tenantID, proofID) // scale-guard:ignore: proofID is validated against this one task's already-loaded attachment set; only the selected attachment receives a signed URL.
	if err != nil {
		return AttachmentDownload{}, err
	}
	return AttachmentDownload{Artifact: artifact, URL: url}, nil
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
