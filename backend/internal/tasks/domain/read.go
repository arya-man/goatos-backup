package domain

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// List filters for GET /app/workflows. Buckets are derived from workflow_instances columns alone
// (compute-on-write card fields), so filter and chips read the same key set.
const (
	FilterAll           = "all"
	FilterOverdue       = "overdue"
	FilterDue           = "due"
	FilterCompleted     = "completed"
	FilterAwaitingVideo = "awaiting_video"
)

// MaxWorkflowPageSize caps the mobile list read (one phone screen of cards).
const MaxWorkflowPageSize = 20

// ErrInvalidCursor is a malformed keyset cursor (HTTP 400).
var ErrInvalidCursor = errors.New("tasks: invalid cursor")

// WorkflowCursor is the keyset position for the list read: (next_due_at ASC NULLS LAST,
// workflow_id ASC). DueIsNull marks a position inside the NULLS LAST tail.
type WorkflowCursor struct {
	NextDueAt  time.Time
	DueIsNull  bool
	WorkflowID string
}

const cursorNullSentinel = "-"

// EncodeWorkflowCursor serializes a cursor position (base64 of "due|workflow_id").
func EncodeWorkflowCursor(c WorkflowCursor) string {
	due := cursorNullSentinel
	if !c.DueIsNull {
		due = c.NextDueAt.UTC().Format(time.RFC3339Nano)
	}
	return base64.RawURLEncoding.EncodeToString([]byte(due + "|" + c.WorkflowID))
}

// DecodeWorkflowCursor parses a client-supplied cursor. Empty input is a nil cursor (first page).
func DecodeWorkflowCursor(raw string) (*WorkflowCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return nil, ErrInvalidCursor
	}
	cursor := WorkflowCursor{WorkflowID: parts[1]}
	if parts[0] == cursorNullSentinel {
		cursor.DueIsNull = true
		return &cursor, nil
	}
	due, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, ErrInvalidCursor
	}
	cursor.NextDueAt = due
	return &cursor, nil
}

// WorkflowListQuery is the list read input. EventDate is the Asia/Kolkata business date the list is
// scoped to; Now anchors the overdue derivation.
type WorkflowListQuery struct {
	TenantID  string
	Module    string
	EventDate string // YYYY-MM-DD
	Filter    string
	PageSize  int
	Cursor    *WorkflowCursor
	Now       time.Time
}

// WorkflowSubject is the card's animal header (backend-owned display fields).
type WorkflowSubject struct {
	GoatID    string `json:"goat_id"`
	DisplayID string `json:"display_id"`
	Tag       string `json:"tag"`
	RoleLabel string `json:"role_label"`
	Sex       string `json:"sex"`
	Breed     string `json:"breed"`
}

// WorkflowNextAction is the card's next-step summary.
type WorkflowNextAction struct {
	Key     string     `json:"key"`
	Title   string     `json:"title"`
	DueAt   *time.Time `json:"due_at"`
	Overdue bool       `json:"overdue"`
}

// WorkflowCard is one list row (read from workflow_instances alone plus bounded display joins).
type WorkflowCard struct {
	WorkflowID           string
	Module               string
	TemplateKey          string
	Subject              WorkflowSubject
	EventAt              time.Time
	EventDate            string
	ParkLabel            string
	ShedLabel            string
	ActionsDone          int
	ActionsTotal         int
	NextAction           *WorkflowNextAction
	AwaitingVerification bool
	State                string
	// Keyset position of this row (for next_cursor derivation).
	NextDueAt *time.Time
}

// WorkflowChips is the per-day chip count strip. All buckets group the same
// (tenant_id, module, event_date) workflow_instances key set.
type WorkflowChips struct {
	All           int `json:"all"`
	Overdue       int `json:"overdue"`
	Due           int `json:"due"`
	Completed     int `json:"completed"`
	AwaitingVideo int `json:"awaiting_video"`
}

// WorkflowListPage is one keyset page plus the day's chips.
type WorkflowListPage struct {
	Items      []WorkflowCard
	Chips      WorkflowChips
	NextCursor *string
}

// WorkflowFact is one context line on the detail header.
type WorkflowFact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// WorkflowDetail is the full per-goat detail: card header + facts + every action row.
type WorkflowDetail struct {
	Card    WorkflowCard
	Facts   []WorkflowFact
	Actions []WorkflowAction
}

// RoleLabelForTemplate is the operator-facing subject role on the card.
func RoleLabelForTemplate(templateKey string) string {
	switch templateKey {
	case TemplateKeyBirthKid:
		return "Kid"
	case TemplateKeyBirthMother:
		return "Mother"
	case TemplateKeyDeath:
		return "Died"
	}
	return ""
}
