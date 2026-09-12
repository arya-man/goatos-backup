package http

import (
	"github.com/vgoats/goatos/backend/internal/penvisits/domain"
)

// Wire shapes. Every visible string is composed by the domain and rendered verbatim by the
// phone; the client never composes a chip, a reason line or a date of its own.

// visitPayload is the shared step shape (domain.Step): the visit's own detail renders exactly
// what the parent card renders.
type visitPayload = domain.Step

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

const listTitle = "Pen visits"

func toVisitPayload(t domain.Task, actor domain.Actor, today string) visitPayload {
	return domain.StepFor(t, actor, today)
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
