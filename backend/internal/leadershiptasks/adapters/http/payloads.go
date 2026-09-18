package http

import (
	"strings"
	"time"
	"unicode"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
)

// Backend-owned wire shapes. Every visible word (chips, meta line, number label, filter
// labels, empty messages, status button labels) is composed here from domain rules and
// rendered verbatim by the phone. The contract of record is contracts/openapi/app-api.yaml.

type attachmentPayload struct {
	AttachmentID string `json:"attachment_id"`
	ProofID      string `json:"proof_id"`
	Kind         string `json:"kind"`
	MimeType     string `json:"mime_type"`
	FileName     string `json:"file_name"`
	SizeBytes    int64  `json:"size_bytes"`
	DurationMS   *int64 `json:"duration_ms"`
	Position     int    `json:"position"`
}

type statusOptionPayload struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type notePayload struct {
	NoteID     string `json:"note_id"`
	AuthorID   string `json:"author_user_id"`
	AuthorName string `json:"author_name"`
	Body       string `json:"body"`
	CreatedAt  string `json:"created_at"`
	// Mentions are the people this note named, resolved and stored at write time. The body
	// stays PLAIN TEXT exactly as it was typed -- there is no inline token format, because an
	// older phone would render one raw to a reader -- so the client overlays these as chips by
	// matching the names it is given.
	Mentions []mentionPayload `json:"mentions"`
}

// activityPayload is one row of the task's history feed (migration 000349), newest first.
// Every word is composed server-side: the actor's name and initials, the two ends of the
// change as the screen shows them ("Open" → "Doing", "17/09/2026 17:00" → "20/09/2026 17:00")
// and the one-line sentence. Kind is the closed vocabulary the console keys its tabs and
// chip rendering on; a `commented` row names its note in note_id and the note carries the
// text, so a comment is never stored twice.
type activityPayload struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	OccurredAt    string `json:"occurred_at"`
	OccurredLabel string `json:"occurred_label"`
	ActorUserID   string `json:"actor_user_id"`
	ActorName     string `json:"actor_name"`
	ActorInitials string `json:"actor_initials"`
	FromLabel     string `json:"from_label"`
	ToLabel       string `json:"to_label"`
	// FromValue / ToValue are the stored KEYS behind the two labels (a status key, an RFC3339
	// instant). The console picks a status chip's tone token from the key, the same way every
	// other chip on the desk does; it never parses a label.
	FromValue string `json:"from_value"`
	ToValue   string `json:"to_value"`
	NoteID    string `json:"note_id"`
	Summary   string `json:"summary"`
}

type mentionPayload struct {
	MentionID string `json:"mention_id"`
	UserID    string `json:"user_id"`
	Name      string `json:"name"`
}

type mentionableUserPayload struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	// Relation is why this person may be named: "raiser", "assignee" or "leadership".
	Relation string `json:"relation"`
}

type mentionableUsersPayload struct {
	Users   []mentionableUserPayload `json:"users"`
	TraceID string                   `json:"trace_id"`
}

// mentionRefPayload is what the client SENDS: the user id it resolved from its own picker.
// The server re-validates every id under the task's row lock, so this is a request, never an
// authority.
type mentionRefPayload struct {
	UserID string `json:"user_id"`
}

type taskPayload struct {
	TaskID         string  `json:"task_id"`
	TaskNo         int64   `json:"task_no"`
	NumberLabel    string  `json:"number_label"`
	Title          string  `json:"title"`
	Body           string  `json:"body"`
	Status         string  `json:"status"`
	StatusChip     string  `json:"status_chip"`
	RaisedByUserID string  `json:"raised_by_user_id"`
	RaisedByName   string  `json:"raised_by_name"`
	AssigneeUserID string  `json:"assignee_user_id"`
	AssigneeName   string  `json:"assignee_name"`
	RaisedAt       string  `json:"raised_at"`
	RaisedOnLabel  string  `json:"raised_on_label"`
	UpdatedAt      string  `json:"updated_at"`
	DoneAt         *string `json:"done_at"`
	SeenAt         *string `json:"seen_at"`
	IsSeen         bool    `json:"is_seen"`
	// The deadline and the countdown (domain/deadline.go), composed here from the server clock
	// and rendered verbatim: DeadlineAt / DeadlineLabel name the deadline; DaysLeft is the big
	// number (days to the deadline day, 0 = due today, negative = overdue; nil without a
	// deadline) with DaysLeftLabel its worded form ("5 days left" / "Due today" / "3 days
	// over"); DeadlineTone is "ok" (green) / "near" / "over" (both red) / "" (no deadline);
	// DeadlineStateLabel is the sentence beneath the number.
	DeadlineAt         *string `json:"deadline_at"`
	DeadlineLabel      string  `json:"deadline_label"`
	DaysLeft           *int    `json:"days_left"`
	DaysLeftLabel      string  `json:"days_left_label"`
	DeadlineTone       string  `json:"deadline_tone"`
	DeadlineStateLabel string  `json:"deadline_state_label"`
	// IsAssignee / IsRaiser name the caller's party to the task explicitly. The phone holds
	// no user id to compare against, and deriving party from can_change_status conflates
	// "may act" with "is the person this is for".
	IsAssignee      bool   `json:"is_assignee"`
	IsRaiser        bool   `json:"is_raiser"`
	MetaLine        string `json:"meta_line"`
	RowVersion      int    `json:"row_version"`
	CanEdit         bool   `json:"can_edit"`
	CanChangeStatus bool   `json:"can_change_status"`
	CanCancel       bool   `json:"can_cancel"`
	// Comment is the compatibility assignee note field; Notes is the chronological activity.
	Comment         string                `json:"comment"`
	CanComment      bool                  `json:"can_comment"`
	StatusOptions   []statusOptionPayload `json:"status_options"`
	AttachmentCount int                   `json:"attachment_count"`
	Attachments     []attachmentPayload   `json:"attachments"`
	Notes           []notePayload         `json:"notes"`
	// Activity is the Jira-style history feed, newest first (activityPayload).
	Activity []activityPayload `json:"activity"`
}

type taskDetailPayload struct {
	Task    taskPayload `json:"task"`
	TraceID string      `json:"trace_id"`
}

type filterPayload struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Count        int    `json:"count"`
	Selected     bool   `json:"selected"`
	EmptyMessage string `json:"empty_message"`
}

type taskPagePayload struct {
	Title       string          `json:"title"`
	Rows        []taskPayload   `json:"rows"`
	NextCursor  *string         `json:"next_cursor"`
	Filters     []filterPayload `json:"filters"`
	Scopes      []filterPayload `json:"scopes"`
	UnseenCount int             `json:"unseen_count"`
	CanRaise    bool            `json:"can_raise"`
	TraceID     string          `json:"trace_id"`
}

type assigneePayload struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	// Title is what the picker lists; Name is revealed beneath once a title is chosen.
	Title string `json:"title"`
}

type assigneesPayload struct {
	Assignees []assigneePayload `json:"assignees"`
	TraceID   string            `json:"trace_id"`
}

type downloadPayload struct {
	DownloadURL string `json:"download_url"`
	TraceID     string `json:"trace_id"`
}

type attachmentRefPayload struct {
	ProofID  string `json:"proof_id"`
	Kind     string `json:"kind"`
	FileName string `json:"file_name"`
}

type raisePayload struct {
	Title          string                 `json:"title"`
	Body           string                 `json:"body"`
	AssigneeUserID string                 `json:"assignee_user_id"`
	Attachments    []attachmentRefPayload `json:"attachments"`
	// DeadlineAt is RFC3339 with an offset; required on the form path (the service refuses a
	// raise without one).
	DeadlineAt string `json:"deadline_at"`
}

type editPayload struct {
	Title       string                 `json:"title"`
	Body        string                 `json:"body"`
	Attachments []attachmentRefPayload `json:"attachments"`
	RowVersion  int                    `json:"row_version"`
	// DeadlineAt replaces the deadline when present; absent or blank keeps the stored one.
	DeadlineAt string `json:"deadline_at"`
}

type commentPayload struct {
	Comment string `json:"comment"`
	// Mentions carries EXPLICIT targets alongside the text. The server does NOT parse "@Ravi"
	// out of the body: two active people can share a display name, and a regex over free text
	// cannot tell a mention from a quoted handle.
	Mentions []mentionRefPayload `json:"mentions"`
}

type statusPayload struct {
	Status     string `json:"status"`
	RowVersion int    `json:"row_version"`
}

// listTitle is the page title the L0 header shows; it mirrors the nav label.
const listTitle = "Tasks"

func toTaskPayload(t domain.Task, actor domain.Actor, now time.Time) taskPayload {
	attachments := make([]attachmentPayload, 0, len(t.Attachments))
	for _, a := range t.Attachments {
		attachments = append(attachments, attachmentPayload{
			AttachmentID: a.AttachmentID,
			ProofID:      a.ProofID,
			Kind:         a.Kind,
			MimeType:     a.MimeType,
			FileName:     a.FileName,
			SizeBytes:    a.SizeBytes,
			DurationMS:   a.DurationMS,
			Position:     a.Position,
		})
	}
	options := domain.StatusOptionsFor(t, actor)
	optionPayloads := make([]statusOptionPayload, 0, len(options))
	for _, o := range options {
		optionPayloads = append(optionPayloads, statusOptionPayload{Key: o.Key, Label: o.Label})
	}
	notes := make([]notePayload, 0, len(t.Notes))
	for _, n := range t.Notes {
		mentions := make([]mentionPayload, 0, len(n.Mentions))
		for _, m := range n.Mentions {
			mentions = append(mentions, mentionPayload{MentionID: m.MentionID, UserID: m.UserID, Name: m.Name})
		}
		notes = append(notes, notePayload{
			NoteID:     n.NoteID,
			AuthorID:   n.AuthorID,
			AuthorName: n.AuthorName,
			Body:       n.Body,
			CreatedAt:  n.CreatedAt.UTC().Format(time.RFC3339),
			Mentions:   mentions,
		})
	}
	activity := make([]activityPayload, 0, len(t.Activity))
	for _, e := range t.Activity {
		activity = append(activity, activityPayload{
			ID:            e.EventID,
			Kind:          e.Kind,
			OccurredAt:    e.OccurredAt.UTC().Format(time.RFC3339),
			OccurredLabel: domain.EventOccurredLabel(e.OccurredAt),
			ActorUserID:   e.ActorUserID,
			ActorName:     e.ActorName,
			ActorInitials: initialsOf(e.ActorName),
			FromLabel:     domain.EventValueLabel(e.Kind, e.FromValue),
			ToLabel:       domain.EventValueLabel(e.Kind, e.ToValue),
			FromValue:     e.FromValue,
			ToValue:       e.ToValue,
			NoteID:        e.NoteID,
			Summary:       domain.EventSummary(e),
		})
	}
	return taskPayload{
		TaskID:             t.TaskID,
		TaskNo:             t.TaskNo,
		NumberLabel:        domain.NumberLabel(t.TaskNo),
		Title:              t.Title,
		Body:               t.Body,
		Status:             t.Status,
		StatusChip:         domain.StatusChip(t.Status),
		RaisedByUserID:     t.RaisedByUserID,
		RaisedByName:       t.RaisedByName,
		AssigneeUserID:     t.AssigneeUserID,
		AssigneeName:       t.AssigneeName,
		RaisedAt:           t.RaisedAt.UTC().Format(time.RFC3339),
		RaisedOnLabel:      domain.RaisedOnLabel(t.RaisedAt),
		UpdatedAt:          t.UpdatedAt.UTC().Format(time.RFC3339),
		DoneAt:             rfc3339Ptr(t.DoneAt),
		SeenAt:             rfc3339Ptr(t.SeenAt),
		IsSeen:             t.SeenAt != nil,
		DeadlineAt:         rfc3339Ptr(t.DeadlineAt),
		DeadlineLabel:      domain.DeadlineLabel(t.DeadlineAt),
		DaysLeft:           daysLeftPtr(t, now),
		DaysLeftLabel:      domain.DaysLeftLabel(t, now),
		DeadlineTone:       domain.DeadlineTone(t, now),
		DeadlineStateLabel: domain.DeadlineStateLabel(t, now),
		IsAssignee:         t.IsAssignee(actor),
		IsRaiser:           t.IsRaiser(actor),
		MetaLine:           domain.MetaLine(t, actor),
		RowVersion:         t.RowVersion,
		CanEdit:            t.CanEdit(actor),
		CanChangeStatus:    t.CanChangeStatus(actor),
		CanCancel:          t.CanCancel(actor),
		Comment:            t.AssigneeComment,
		CanComment:         t.CanComment(actor),
		StatusOptions:      optionPayloads,
		AttachmentCount:    len(t.Attachments),
		Attachments:        attachments,
		Notes:              notes,
		Activity:           activity,
	}
}

// initialsOf is the avatar's two letters, the same rule the console's `initials()` applies
// to every other name on the Tasks desk: first letter of the first two words, upper-cased.
// Blank in, blank out -- the client draws a generic mark for a name it does not have.
func initialsOf(name string) string {
	var out []rune
	for _, part := range strings.Fields(name) {
		for _, r := range part {
			out = append(out, unicode.ToUpper(r))
			break
		}
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

func toFilterPayloads(selected string, page ports.Page, canRaise bool) []filterPayload {
	out := make([]filterPayload, 0, len(domain.FilterKeys))
	for _, key := range domain.FilterKeys {
		out = append(out, filterPayload{
			Key:          key,
			Label:        domain.FilterLabel(key),
			Count:        domain.FilterCount(key, page.StatusCounts),
			Selected:     key == selected,
			EmptyMessage: domain.FilterEmptyMessage(key, canRaise),
		})
	}
	return out
}

func toScopePayloads(selected string, page ports.Page, actor domain.Actor) []filterPayload {
	out := make([]filterPayload, 0, len(domain.ScopeKeys))
	for _, key := range domain.ScopeKeys {
		if key == domain.ScopeAssignedByMe && !actor.CanRaise {
			continue
		}
		if key == domain.ScopeTeamProgress && !actor.CanMonitor {
			continue
		}
		out = append(out, filterPayload{
			Key:          key,
			Label:        domain.ScopeLabel(key),
			Count:        page.ScopeCounts[key],
			Selected:     key == selected,
			EmptyMessage: domain.ScopeEmptyMessage(key),
		})
	}
	return out
}

// daysLeftPtr is the big number, or nil when the task has no deadline and so shows none.
func daysLeftPtr(t domain.Task, now time.Time) *int {
	if t.DeadlineAt == nil {
		return nil
	}
	days := domain.DaysLeft(t, now)
	return &days
}

func toMentionUserIDs(in []mentionRefPayload) []string {
	out := make([]string, 0, len(in))
	for _, m := range in {
		out = append(out, strings.TrimSpace(m.UserID))
	}
	return out
}

// parseDeadline reads an RFC3339 deadline off a request; blank means none was sent.
func parseDeadline(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	utc := parsed.UTC()
	return &utc, nil
}

func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
