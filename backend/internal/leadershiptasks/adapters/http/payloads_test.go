package http

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/leadershiptasks/domain"
	"github.com/vgoats/goatos/backend/internal/leadershiptasks/ports"
)

// The wire shape is the phone's whole vocabulary: every visible word rides the payload and
// the capability booleans drive every control. This pins the contract field by field for
// both parties so a renamed key or a dropped capability is caught here, not on a phone.
func TestTaskPayloadCarriesBackendCopyAndCapabilitiesPerParty(t *testing.T) {
	seen := time.Date(2026, 9, 4, 6, 0, 0, 0, time.UTC)
	task := domain.Task{
		TaskID:         "22222222-2222-4222-8222-222222222222",
		TaskNo:         12,
		Title:          "Approve the vendor contract",
		Body:           "See the attached draft.",
		Status:         domain.StatusOpen,
		RaisedByUserID: "33333333-3333-4333-8333-333333333333",
		RaisedByName:   "Hemant",
		AssigneeUserID: "44444444-4444-4444-8444-444444444444",
		AssigneeName:   "Ravi",
		RaisedAt:       time.Date(2026, 9, 4, 5, 30, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 9, 4, 5, 30, 0, 0, time.UTC),
		SeenAt:         &seen,
		RowVersion:     3,
		Attachments: []domain.Attachment{{
			AttachmentID: "a1", ProofID: "p1", Kind: domain.AttachmentAudio, MimeType: "audio/mp4", FileName: "note.m4a", SizeBytes: 900, Position: 0,
		}},
	}
	cxo := domain.Actor{UserID: task.AssigneeUserID, CanAct: true}
	director := domain.Actor{UserID: task.RaisedByUserID, CanRaise: true}

	raw, err := json.Marshal(toTaskPayload(task, cxo))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"task_id":           task.TaskID,
		"task_no":           float64(12),
		"number_label":      "#12",
		"title":             task.Title,
		"status":            "open",
		"status_chip":       "Open",
		"raised_by_name":    "Hemant",
		"assignee_name":     "Ravi",
		"raised_on_label":   "04/09/2026",
		"meta_line":         "Raised by Hemant · 04/09/2026",
		"is_seen":           true,
		"is_assignee":       true,
		"is_raiser":         false,
		"row_version":       float64(3),
		"can_edit":          false,
		"can_change_status": true,
		"can_cancel":        false,
		"attachment_count":  float64(1),
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	options := got["status_options"].([]any)
	if len(options) != 2 || options[0].(map[string]any)["key"] != "in_progress" || options[0].(map[string]any)["label"] != "Start" {
		t.Fatalf("assignee status options = %v", options)
	}
	att := got["attachments"].([]any)[0].(map[string]any)
	if att["kind"] != "audio" || att["mime_type"] != "audio/mp4" || att["file_name"] != "note.m4a" || att["proof_id"] != "p1" {
		t.Fatalf("attachment payload = %v", att)
	}

	raw, _ = json.Marshal(toTaskPayload(task, director))
	got = map[string]any{}
	_ = json.Unmarshal(raw, &got)
	if got["can_edit"] != true || got["can_cancel"] != true || got["can_change_status"] != false {
		t.Fatalf("director capabilities = edit %v cancel %v status %v", got["can_edit"], got["can_cancel"], got["can_change_status"])
	}
	if got["is_assignee"] != false || got["is_raiser"] != true {
		t.Fatalf("director party = assignee %v raiser %v", got["is_assignee"], got["is_raiser"])
	}
	if got["meta_line"] != "For Ravi · 04/09/2026" {
		t.Fatalf("director meta line = %v", got["meta_line"])
	}
	options = got["status_options"].([]any)
	if len(options) != 1 || options[0].(map[string]any)["key"] != "cancelled" {
		t.Fatalf("director status options = %v", options)
	}
}

func TestFilterPayloadsAreWholeListCountsWithTheSelectedChipMarked(t *testing.T) {
	page := ports.Page{StatusCounts: map[string]int{"open": 2, "in_progress": 1, "done": 3, "cancelled": 5}}
	filters := toFilterPayloads(domain.FilterDone, page, true)
	if len(filters) != 4 {
		t.Fatalf("filters = %+v", filters)
	}
	if filters[0].Key != "all" || filters[0].Count != 6 || filters[0].Selected {
		t.Fatalf("all chip = %+v", filters[0])
	}
	if filters[3].Key != "done" || filters[3].Count != 3 || !filters[3].Selected || filters[3].EmptyMessage == "" {
		t.Fatalf("done chip = %+v", filters[3])
	}
}
