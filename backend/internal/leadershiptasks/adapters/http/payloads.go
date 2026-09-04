package http

import (
	"time"

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
	// Comment is the CXO's note back on the task; CanComment says whether the caller may write it.
	Comment         string                `json:"comment"`
	CanComment      bool                  `json:"can_comment"`
	StatusOptions   []statusOptionPayload `json:"status_options"`
	AttachmentCount int                   `json:"attachment_count"`
	Attachments     []attachmentPayload   `json:"attachments"`
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
	UnseenCount int             `json:"unseen_count"`
	CanRaise    bool            `json:"can_raise"`
	TraceID     string          `json:"trace_id"`
}

type assigneePayload struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
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
}

type editPayload struct {
	Title       string                 `json:"title"`
	Body        string                 `json:"body"`
	Attachments []attachmentRefPayload `json:"attachments"`
	RowVersion  int                    `json:"row_version"`
}

type commentPayload struct {
	Comment string `json:"comment"`
}

type statusPayload struct {
	Status     string `json:"status"`
	RowVersion int    `json:"row_version"`
}

// listTitle is the page title the L0 header shows; it mirrors the nav label.
const listTitle = "Tasks"

func toTaskPayload(t domain.Task, actor domain.Actor) taskPayload {
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
	return taskPayload{
		TaskID:          t.TaskID,
		TaskNo:          t.TaskNo,
		NumberLabel:     domain.NumberLabel(t.TaskNo),
		Title:           t.Title,
		Body:            t.Body,
		Status:          t.Status,
		StatusChip:      domain.StatusChip(t.Status),
		RaisedByUserID:  t.RaisedByUserID,
		RaisedByName:    t.RaisedByName,
		AssigneeUserID:  t.AssigneeUserID,
		AssigneeName:    t.AssigneeName,
		RaisedAt:        t.RaisedAt.UTC().Format(time.RFC3339),
		RaisedOnLabel:   domain.RaisedOnLabel(t.RaisedAt),
		UpdatedAt:       t.UpdatedAt.UTC().Format(time.RFC3339),
		DoneAt:          rfc3339Ptr(t.DoneAt),
		SeenAt:          rfc3339Ptr(t.SeenAt),
		IsSeen:          t.SeenAt != nil,
		IsAssignee:      t.IsAssignee(actor),
		IsRaiser:        t.IsRaiser(actor),
		MetaLine:        domain.MetaLine(t, actor),
		RowVersion:      t.RowVersion,
		CanEdit:         t.CanEdit(actor),
		CanChangeStatus: t.CanChangeStatus(actor),
		CanCancel:       t.CanCancel(actor),
		Comment:         t.AssigneeComment,
		CanComment:      t.CanComment(actor),
		StatusOptions:   optionPayloads,
		AttachmentCount: len(t.Attachments),
		Attachments:     attachments,
	}
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

func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}
