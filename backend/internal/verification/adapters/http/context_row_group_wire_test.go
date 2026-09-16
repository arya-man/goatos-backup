package http

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// A producer's section header rides every context row to both clients; an ungrouped row still
// marshals exactly as it did before groups existed (no "group" key at all).
func TestQueueItemContextRowGroupRidesTheWire(t *testing.T) {
	row := domain.QueueRow{Item: domain.Item{
		Source: domain.SourceRef{Module: "feed", RefType: "feed_distribution_completion", RefID: "c1"},
		ContextRows: []domain.ContextRow{
			{Label: "Pen", Value: "Castro 2"},
			{Label: "Was the trough clean?", Value: "Yes", Group: "Crew answers"},
		},
	}}
	body, err := json.Marshal(toQueueItemResponse(row, nil))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ContextRows []map[string]string `json:"context_rows"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.ContextRows) != 2 || decoded.ContextRows[1]["group"] != "Crew answers" {
		t.Fatalf("context_rows = %v, want the group on the answer row", decoded.ContextRows)
	}
	if _, present := decoded.ContextRows[0]["group"]; present {
		t.Fatalf("an ungrouped row must omit group, got %v", decoded.ContextRows[0])
	}
	if !strings.Contains(string(body), `"group":"Crew answers"`) {
		t.Fatalf("wire = %s", body)
	}
}
