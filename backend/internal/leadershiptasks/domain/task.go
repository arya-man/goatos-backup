// Package domain is the rulebook of the Leadership Tasks module (maintainer decision
// 2026-09-04).
//
// A DIRECTOR raises a task for ONE CXO: a title, a written brief, and optional attachments
// (a voice note recorded in the app, photos or videos picked from the gallery, any file).
// It is not operational work -- no shed, no animal, no proof -- it is a director asking the
// leadership desk for something. The CXO it is addressed to sees it, opens it (which marks
// it SEEN, the fact the drawer badge counts), and moves it open -> in_progress -> done. The
// raiser may edit the brief and its attachments, or cancel the task, only while it is still
// open or in progress; a finished task is history and is never rewritten.
//
// Every task carries a per-tenant running NUMBER ("#12"), which is how two people on a call
// refer to it. The number is minted inside the raise transaction and never reused.
//
// All copy a screen shows -- status chips, the meta line, the number label -- is composed
// HERE and rendered verbatim by the phone. There is no comment thread in v1.
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Statuses. The vocabulary is closed; the SQL CHECK constraint mirrors it.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusCancelled  = "cancelled"
)

// Attachment kinds. The phone names the kind it captured; the server keeps it as the label
// the detail screen groups by. The bytes themselves live in the proof store.
const (
	AttachmentAudio = "audio"
	AttachmentVideo = "video"
	AttachmentPhoto = "photo"
	AttachmentFile  = "file"
)

// Limits. Generous for a brief, tight enough that a pasted document is refused.
const (
	MaxTitleRunes   = 80
	MaxBodyRunes    = 4000
	MaxAttachments  = 12
	MaxFileNameRune = 200
	MaxCommentRunes = 2000
)

// Sentinel errors. The transport maps each to a stable code and a farm-worded message.
var (
	ErrTitleRequired            = errors.New("leadership task: title is required")
	ErrTitleTooLong             = errors.New("leadership task: title too long")
	ErrBodyTooLong              = errors.New("leadership task: body too long")
	ErrAssigneeRequired         = errors.New("leadership task: assignee is required")
	ErrTooManyAttachments       = errors.New("leadership task: too many attachments")
	ErrInvalidAttachmentKind    = errors.New("leadership task: invalid attachment kind")
	ErrDuplicateAttachment      = errors.New("leadership task: duplicate attachment")
	ErrInvalidStatus            = errors.New("leadership task: invalid status")
	ErrInvalidStatusTransition  = errors.New("leadership task: invalid status transition")
	ErrTaskClosed               = errors.New("leadership task: task is closed")
	ErrNotRaiser                = errors.New("leadership task: caller did not raise this task")
	ErrNotAssignee              = errors.New("leadership task: caller is not the assignee")
	ErrSelfAssignment           = errors.New("leadership task: a task cannot be raised for oneself")
	ErrAssigneeNotCXO           = errors.New("leadership task: assignee is not a CXO")
	ErrFileNameTooLong          = errors.New("leadership task: file name too long")
	ErrAttachmentProofRequired  = errors.New("leadership task: attachment proof is required")
	ErrAttachmentNotCompleted   = errors.New("leadership task: attachment upload is not complete")
	ErrAttachmentNotAnAttachmnt = errors.New("leadership task: proof is not an attachment")
	ErrCommentTooLong           = errors.New("leadership task: comment too long")
)

// Task is one raised task with its attachments, as stored.
type Task struct {
	TaskID         string
	TenantID       string
	TaskNo         int64
	Title          string
	Body           string
	Status         string
	RaisedByUserID string
	RaisedByName   string
	AssigneeUserID string
	AssigneeName   string
	RaisedAt       time.Time
	UpdatedAt      time.Time
	DoneAt         *time.Time
	CancelledAt    *time.Time
	SeenAt         *time.Time
	// AssigneeComment is the CXO's note back on the task: one field its owner overwrites.
	AssigneeComment string
	RowVersion      int
	Attachments     []Attachment
	AttachmentCount int
}

// Attachment is one stored attachment of a task. ProofID points at the proof store row that
// holds the bytes; everything else is what the list and detail screens label it with.
type Attachment struct {
	AttachmentID string
	ProofID      string
	Kind         string
	MimeType     string
	FileName     string
	SizeBytes    int64
	DurationMS   *int64
	Position     int
}

// AttachmentRef is what a raise/edit request names: the proof the phone already uploaded,
// the kind it captured, and the name to show. Size, mime and duration are read from the
// proof store, never trusted from the client.
type AttachmentRef struct {
	ProofID  string
	Kind     string
	FileName string
}

// IsOpenForWork reports whether the task can still be worked or edited.
func IsOpenForWork(status string) bool {
	return status == StatusOpen || status == StatusInProgress
}

// IsKnownStatus reports whether s is in the closed vocabulary.
func IsKnownStatus(s string) bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusDone, StatusCancelled:
		return true
	}
	return false
}

// IsKnownAttachmentKind reports whether k is in the closed vocabulary.
func IsKnownAttachmentKind(k string) bool {
	switch k {
	case AttachmentAudio, AttachmentVideo, AttachmentPhoto, AttachmentFile:
		return true
	}
	return false
}

// ValidateBrief checks the title, body and attachment refs of a raise or edit.
func ValidateBrief(title, body string, refs []AttachmentRef) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return ErrTitleRequired
	}
	if len([]rune(title)) > MaxTitleRunes {
		return ErrTitleTooLong
	}
	if len([]rune(strings.TrimSpace(body))) > MaxBodyRunes {
		return ErrBodyTooLong
	}
	if len(refs) > MaxAttachments {
		return ErrTooManyAttachments
	}
	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.ProofID) == "" {
			return ErrAttachmentProofRequired
		}
		if !IsKnownAttachmentKind(ref.Kind) {
			return ErrInvalidAttachmentKind
		}
		if len([]rune(ref.FileName)) > MaxFileNameRune {
			return ErrFileNameTooLong
		}
		if _, dup := seen[ref.ProofID]; dup {
			return ErrDuplicateAttachment
		}
		seen[ref.ProofID] = struct{}{}
	}
	return nil
}

// Actor is who is looking at, or acting on, a task: their id and the two authorities the
// route table already resolved. Read authority is implied by reaching the handler.
type Actor struct {
	UserID   string
	CanRaise bool
	CanAct   bool
}

// IsRaiser reports whether the actor raised the task.
func (t Task) IsRaiser(a Actor) bool { return a.UserID != "" && a.UserID == t.RaisedByUserID }

// IsAssignee reports whether the task is addressed to the actor.
func (t Task) IsAssignee(a Actor) bool { return a.UserID != "" && a.UserID == t.AssigneeUserID }

// CanEdit: the raiser, while the task is still open for work.
func (t Task) CanEdit(a Actor) bool { return a.CanRaise && t.IsRaiser(a) && IsOpenForWork(t.Status) }

// CanCancel: the raiser, while the task is still open for work.
func (t Task) CanCancel(a Actor) bool { return t.CanEdit(a) }

// CanChangeStatus: the assignee, while the task is not cancelled. A done task can be
// reopened by the assignee (they marked it done by mistake); a cancelled one cannot.
func (t Task) CanChangeStatus(a Actor) bool {
	return a.CanAct && t.IsAssignee(a) && t.Status != StatusCancelled
}

// CanComment: the assignee, while the task is not cancelled. The raiser reads it.
func (t Task) CanComment(a Actor) bool { return t.CanChangeStatus(a) }

// ValidateComment bounds the note.
func ValidateComment(comment string) error {
	if len([]rune(strings.TrimSpace(comment))) > MaxCommentRunes {
		return ErrCommentTooLong
	}
	return nil
}

// StatusOption is one status the caller may move the task to, with its button label.
type StatusOption struct {
	Key   string
	Label string
}

// StatusOptionsFor lists the transitions THIS actor may make, in the order the screen shows
// them. The raiser only ever cancels; the assignee walks the ladder. Empty means read-only.
func StatusOptionsFor(t Task, a Actor) []StatusOption {
	var out []StatusOption
	if t.CanChangeStatus(a) {
		// The assignee's dropdown holds exactly two words, Doing and Done (maintainer
		// instruction 2026-09-04); the current one is the field's value, the other is the move.
		switch t.Status {
		case StatusOpen:
			out = append(out, StatusOption{Key: StatusInProgress, Label: StatusChip(StatusInProgress)}, StatusOption{Key: StatusDone, Label: StatusChip(StatusDone)})
		case StatusInProgress:
			out = append(out, StatusOption{Key: StatusDone, Label: StatusChip(StatusDone)})
		case StatusDone:
			out = append(out, StatusOption{Key: StatusInProgress, Label: StatusChip(StatusInProgress)})
		}
	}
	if t.CanCancel(a) {
		out = append(out, StatusOption{Key: StatusCancelled, Label: "Cancel task"})
	}
	return out
}

// CheckTransition validates one status change by the actor against the options above --
// the options ARE the rule, so the screen and the write can never disagree.
func CheckTransition(t Task, a Actor, to string) error {
	if !IsKnownStatus(to) {
		return ErrInvalidStatus
	}
	if to == t.Status {
		return ErrInvalidStatusTransition
	}
	for _, opt := range StatusOptionsFor(t, a) {
		if opt.Key == to {
			return nil
		}
	}
	if to == StatusCancelled {
		if !t.IsRaiser(a) {
			return ErrNotRaiser
		}
		return ErrTaskClosed
	}
	if !t.IsAssignee(a) {
		return ErrNotAssignee
	}
	if t.Status == StatusCancelled {
		return ErrTaskClosed
	}
	return ErrInvalidStatusTransition
}

// StatusChip is the label the card and detail show for a status.
func StatusChip(status string) string {
	switch status {
	case StatusOpen:
		return "Open"
	case StatusInProgress:
		return "Doing"
	case StatusDone:
		return "Done"
	case StatusCancelled:
		return "Cancelled"
	}
	return status
}

// NumberLabel is how the farm refers to a task: "#12".
func NumberLabel(taskNo int64) string { return fmt.Sprintf("#%d", taskNo) }

// RaisedOnLabel is the farm-readable raise date in IST.
func RaisedOnLabel(raisedAt time.Time) string { return biztime.FarmDate(raisedAt) }

// MetaLine names the OTHER party and the date, from the viewer's side: the CXO reads who
// asked, the director reads whom they asked. A third party (neither) reads both names.
func MetaLine(t Task, a Actor) string {
	date := RaisedOnLabel(t.RaisedAt)
	switch {
	case t.IsAssignee(a):
		return fmt.Sprintf("Raised by %s · %s", nameOr(t.RaisedByName, "a director"), date)
	case t.IsRaiser(a):
		return fmt.Sprintf("For %s · %s", nameOr(t.AssigneeName, "the leadership desk"), date)
	default:
		return fmt.Sprintf("%s → %s · %s", nameOr(t.RaisedByName, "A director"), nameOr(t.AssigneeName, "the leadership desk"), date)
	}
}

func nameOr(name, fallback string) string {
	if strings.TrimSpace(name) == "" {
		return fallback
	}
	return strings.TrimSpace(name)
}

// Filters. The client names a KEY; the backend owns which statuses that means, so "open"
// is one definition across every surface.
const (
	FilterAll        = "all"
	FilterOpen       = "open"
	FilterInProgress = "in_progress"
	FilterDone       = "done"
)

// FilterKeys is the chip order.
var FilterKeys = []string{FilterAll, FilterOpen, FilterInProgress, FilterDone}

// FilterKeyOrDefault normalizes a requested key, falling back to All so a stale client
// still sees its list rather than an empty screen.
func FilterKeyOrDefault(key string) string {
	switch strings.TrimSpace(key) {
	case FilterOpen, FilterInProgress, FilterDone:
		return strings.TrimSpace(key)
	}
	return FilterAll
}

// StatusesForFilter resolves a chip key to the statuses it lists. All hides cancelled tasks:
// a cancelled task is noise on the desk, and the raiser who cancelled it knows they did.
func StatusesForFilter(key string) []string {
	switch key {
	case FilterOpen:
		return []string{StatusOpen}
	case FilterInProgress:
		return []string{StatusInProgress}
	case FilterDone:
		return []string{StatusDone}
	}
	return []string{StatusOpen, StatusInProgress, StatusDone}
}

// FilterLabel is the chip text.
func FilterLabel(key string) string {
	switch key {
	case FilterOpen:
		return "Open"
	case FilterInProgress:
		return "Doing"
	case FilterDone:
		return "Done"
	}
	return "All"
}

// FilterEmptyMessage is the line shown when a slice has nothing in it, from the viewer's
// side: a CXO's empty desk is good news, a director's empty list is an invitation.
func FilterEmptyMessage(key string, canRaise bool) string {
	if canRaise {
		switch key {
		case FilterOpen:
			return "Nothing open. Tap + to ask the leadership team for something."
		case FilterInProgress:
			return "Nothing is being worked on right now."
		case FilterDone:
			return "Nothing has been completed yet."
		}
		return "No tasks yet. Tap + to raise one."
	}
	switch key {
	case FilterOpen:
		return "Nothing open. Your desk is clear."
	case FilterInProgress:
		return "Nothing in progress."
	case FilterDone:
		return "Nothing completed yet."
	}
	return "No tasks for you yet."
}

// FilterCount resolves a chip's count from whole-list status counts.
func FilterCount(key string, statusCounts map[string]int) int {
	total := 0
	for _, s := range StatusesForFilter(key) {
		total += statusCounts[s]
	}
	return total
}
