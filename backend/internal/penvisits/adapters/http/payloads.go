package http

import (
	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
)

// Wire shapes. Every visible string is composed by the domain and rendered verbatim by the
// phone; the client never composes a chip, a reason line or a date of its own.

type visitPayload struct {
	TaskID       string   `json:"task_id"`
	Title        string   `json:"title"`
	ParkID       string   `json:"park_id"`
	ParkName     string   `json:"park_name"`
	ShedID       string   `json:"shed_id"`
	ShedName     string   `json:"shed_name"`
	Partition    string   `json:"partition_label"`
	PenLabel     string   `json:"operational_location_display"`
	Reasons      []string `json:"reasons"`
	ReasonLabels []string `json:"reason_labels"`
	ReasonLine   string   `json:"reason_line"`
	SourceDate   string   `json:"source_business_date"`
	PlannedDate  string   `json:"planned_business_date"`
	DueDate      string   `json:"due_business_date"`
	WorkState    string   `json:"work_state"`
	StateChip    string   `json:"state_chip"`
	StateTone    string   `json:"state_tone"`
	Instruction  string   `json:"instruction"`
	DoneLine     string   `json:"done_line"`
	CanSubmit    bool     `json:"can_submit"`
	ProofRef     *string  `json:"proof_ref"`
	SubmittedAt  *string  `json:"submitted_at"`
	RowVersion   int      `json:"row_version"`
}

type visitDetailPayload struct {
	Task    visitPayload `json:"task"`
	TraceID string       `json:"trace_id"`
}

type filterPayload struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Count        int    `json:"count"`
	Selected     bool   `json:"selected"`
	EmptyMessage string `json:"empty_message"`
}

type visitPagePayload struct {
	Title      string          `json:"title"`
	Rows       []visitPayload  `json:"rows"`
	NextCursor *string         `json:"next_cursor"`
	Filters    []filterPayload `json:"filters"`
	OpenCount  int             `json:"open_count"`
	TraceID    string          `json:"trace_id"`
}

type submitPayload struct {
	ProofRef   string `json:"proof_ref"`
	RowVersion int    `json:"row_version"`
}

const listTitle = "For me"

func toVisitPayload(t domain.Task, actor domain.Actor, today string) visitPayload {
	reasons := domain.SortReasons(t.Reasons)
	labels := make([]string, 0, len(reasons))
	for _, r := range reasons {
		labels = append(labels, domain.ReasonLabel(r))
	}
	var submitted *string
	if t.SubmittedAt != nil {
		s := t.SubmittedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		submitted = &s
	}
	return visitPayload{
		TaskID:       t.TaskID,
		Title:        domain.Title(t),
		ParkID:       t.ParkID,
		ParkName:     t.ParkName,
		ShedID:       t.ShedID,
		ShedName:     t.ShedName,
		Partition:    t.Partition,
		PenLabel:     t.PenLabel,
		Reasons:      reasons,
		ReasonLabels: labels,
		ReasonLine:   domain.ReasonLine(t, today),
		SourceDate:   t.SourceDate,
		PlannedDate:  t.PlannedDate,
		DueDate:      t.DueDate,
		WorkState:    t.WorkState,
		StateChip:    domain.StateChip(t, today),
		StateTone:    domain.StateTone(t),
		Instruction:  domain.Instruction(t),
		DoneLine:     domain.DoneLine(t),
		CanSubmit:    t.CanSubmit(actor),
		ProofRef:     t.ProofRef,
		SubmittedAt:  submitted,
		RowVersion:   t.RowVersion,
	}
}

func toFilterPayloads(selected string, counts map[string]int) []filterPayload {
	out := make([]filterPayload, 0, len(domain.FilterKeys))
	for _, key := range domain.FilterKeys {
		out = append(out, filterPayload{
			Key:          key,
			Label:        domain.FilterLabel(key),
			Count:        domain.FilterCount(key, counts),
			Selected:     key == selected,
			EmptyMessage: domain.FilterEmptyMessage(key),
		})
	}
	return out
}
