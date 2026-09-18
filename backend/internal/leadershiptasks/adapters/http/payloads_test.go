package http

import (
	"encoding/json"
	"strings"
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
	now := time.Date(2026, 9, 8, 5, 30, 0, 0, time.UTC)

	raw, err := json.Marshal(toTaskPayload(task, cxo, now))
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
		"status_chip":       "To do",
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
		// No deadline: no counter, no tone, blank labels -- the screen draws nothing.
		"deadline_at":          nil,
		"deadline_label":       "",
		"days_left":            nil,
		"days_left_label":      "",
		"deadline_tone":        "",
		"deadline_state_label": "",
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	options := got["status_options"].([]any)
	if got["can_comment"] != true || got["comment"] != "" {
		t.Fatalf("assignee comment = %v can_comment %v", got["comment"], got["can_comment"])
	}
	if len(options) != 2 || options[0].(map[string]any)["key"] != "in_progress" || options[0].(map[string]any)["label"] != "In progress" {
		t.Fatalf("assignee status options = %v", options)
	}
	att := got["attachments"].([]any)[0].(map[string]any)
	if att["kind"] != "audio" || att["mime_type"] != "audio/mp4" || att["file_name"] != "note.m4a" || att["proof_id"] != "p1" {
		t.Fatalf("attachment payload = %v", att)
	}

	raw, _ = json.Marshal(toTaskPayload(task, director, now))
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

	// A leadership monitor who is neither party (the CEO opening a director's task) reads
	// every capability and the full ladder plus cancel (CEO decision 2026-09-18). Before
	// this the payload answered all-false with status_options: [] -- a dead status pill.
	leader := domain.Actor{UserID: "66666666-6666-4666-8666-666666666666", CanRaise: true, CanAct: true, CanMonitor: true}
	monitorCases := []struct {
		status                                          string
		canEdit, canCancel, canChangeStatus, canComment bool
		options                                         []string
	}{
		{domain.StatusOpen, true, true, true, true, []string{"in_progress", "done", "cancelled"}},
		{domain.StatusInProgress, true, true, true, true, []string{"done", "cancelled"}},
		{domain.StatusDone, false, false, true, true, []string{"in_progress"}},
		{domain.StatusCancelled, false, false, false, false, nil},
	}
	for _, c := range monitorCases {
		task.Status = c.status
		raw, _ = json.Marshal(toTaskPayload(task, leader, now))
		got = map[string]any{}
		_ = json.Unmarshal(raw, &got)
		if got["can_edit"] != c.canEdit || got["can_cancel"] != c.canCancel || got["can_change_status"] != c.canChangeStatus || got["can_comment"] != c.canComment {
			t.Fatalf("monitor/%s capabilities = edit %v cancel %v status %v comment %v", c.status, got["can_edit"], got["can_cancel"], got["can_change_status"], got["can_comment"])
		}
		if got["is_assignee"] != false || got["is_raiser"] != false {
			t.Fatalf("monitor/%s party = assignee %v raiser %v", c.status, got["is_assignee"], got["is_raiser"])
		}
		options, _ = got["status_options"].([]any)
		if len(options) != len(c.options) {
			t.Fatalf("monitor/%s status options = %v, want %v", c.status, options, c.options)
		}
		for i, want := range c.options {
			if options[i].(map[string]any)["key"] != want {
				t.Fatalf("monitor/%s status option %d = %v, want %s", c.status, i, options[i], want)
			}
		}
	}
	if got["meta_line"] != "Hemant → Ravi · 04/09/2026" {
		t.Fatalf("monitor meta line = %v", got["meta_line"])
	}
}

func TestFilterPayloadsAreWholeListCountsWithTheSelectedChipMarked(t *testing.T) {
	page := ports.Page{StatusCounts: map[string]int{"open": 2, "in_progress": 1, "done": 3, "cancelled": 5}, OverdueCount: 1}
	filters := toFilterPayloads(domain.FilterDone, page, true)
	if len(filters) != 5 {
		t.Fatalf("filters = %+v", filters)
	}
	// The overdue chip is the LATE subset of open + in_progress (3 here), never their sum: its
	// count is the page's own, measured against the request clock.
	if filters[4].Key != "overdue" || filters[4].Label != "Overdue" || filters[4].Count != 1 || filters[4].Selected || filters[4].EmptyMessage == "" {
		t.Fatalf("overdue chip = %+v", filters[4])
	}
	if filters[0].Key != "all" || filters[0].Count != 6 || filters[0].Selected {
		t.Fatalf("all chip = %+v", filters[0])
	}
	if filters[3].Key != "done" || filters[3].Count != 3 || !filters[3].Selected || filters[3].EmptyMessage == "" {
		t.Fatalf("done chip = %+v", filters[3])
	}
}

// The deadline and the day counter ride the payload composed from the server clock: the big
// number, its worded form, the green/red tone and the sentence beneath it. The phone and
// admin-web render these verbatim and count nothing themselves.
func TestTaskPayloadCarriesTheDeadlineAndTheCountdown(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	deadline := time.Date(2026, 9, 15, 17, 0, 0, 0, ist)
	task := domain.Task{
		TaskID:         "22222222-2222-4222-8222-222222222222",
		TaskNo:         12,
		Title:          "Approve the vendor contract",
		Status:         domain.StatusInProgress,
		RaisedByUserID: "33333333-3333-4333-8333-333333333333",
		AssigneeUserID: "44444444-4444-4444-8444-444444444444",
		RaisedAt:       time.Date(2026, 9, 10, 9, 0, 0, 0, ist),
		DeadlineAt:     &deadline,
	}
	actor := domain.Actor{UserID: task.AssigneeUserID, CanAct: true}

	decode := func(now time.Time) map[string]any {
		raw, err := json.Marshal(toTaskPayload(task, actor, now))
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		return got
	}

	far := decode(time.Date(2026, 9, 11, 9, 0, 0, 0, ist))
	for key, want := range map[string]any{
		"deadline_at":          "2026-09-15T11:30:00Z",
		"deadline_label":       "15/09/2026 17:00",
		"days_left":            float64(4),
		"days_left_label":      "4 days left",
		"deadline_tone":        "ok",
		"deadline_state_label": "Due in 4 days",
	} {
		if far[key] != want {
			t.Errorf("far: %s = %v, want %v", key, far[key], want)
		}
	}
	near := decode(time.Date(2026, 9, 14, 9, 0, 0, 0, ist))
	if near["days_left"] != float64(1) || near["deadline_tone"] != "near" || near["days_left_label"] != "1 day left" {
		t.Fatalf("near payload = days %v tone %v label %v", near["days_left"], near["deadline_tone"], near["days_left_label"])
	}
	over := decode(time.Date(2026, 9, 18, 9, 0, 0, 0, ist))
	if over["days_left"] != float64(-3) || over["deadline_tone"] != "over" || over["deadline_state_label"] != "Overdue by 3 days" {
		t.Fatalf("over payload = days %v tone %v state %v", over["days_left"], over["deadline_tone"], over["deadline_state_label"])
	}
}

// The request side: a blank deadline is "none sent" (the service decides whether that is
// allowed), an offset-bearing RFC3339 instant is read in its own zone, and garbage is refused.
func TestParseDeadlineReadsRFC3339WithOffset(t *testing.T) {
	if got, err := parseDeadline(" "); err != nil || got != nil {
		t.Fatalf("blank = %v, %v", got, err)
	}
	got, err := parseDeadline("2026-09-15T17:00:00+05:30")
	if err != nil || got == nil || !got.Equal(time.Date(2026, 9, 15, 11, 30, 0, 0, time.UTC)) {
		t.Fatalf("offset = %v, %v", got, err)
	}
	if _, err := parseDeadline("15/09/2026 17:00"); err == nil {
		t.Fatal("farm-formatted string must be refused on the wire")
	}
}

// The activity feed rides the task payload newest first with every word composed here: the
// actor's initials for the avatar, the farm-clock time, the two ends of the change as labels
// AND as keys (the console picks a chip tone from the key), and the sentence.
func TestTaskPayloadCarriesTheActivityFeedNewestFirstWithServerSideLabels(t *testing.T) {
	task := domain.Task{
		TaskID: "22222222-2222-4222-8222-222222222222", TaskNo: 12, Status: domain.StatusInProgress,
		RaisedAt: time.Date(2026, 9, 17, 3, 56, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC),
		Activity: []domain.Event{
			{EventID: "e2", Kind: domain.EventStatusChanged, OccurredAt: time.Date(2026, 9, 18, 5, 0, 0, 0, time.UTC), ActorUserID: "u1", ActorName: "Ravi Teja", FromValue: domain.StatusOpen, ToValue: domain.StatusInProgress},
			{EventID: "e1", Kind: domain.EventCreated, OccurredAt: time.Date(2026, 9, 17, 3, 56, 0, 0, time.UTC), ActorUserID: "u2", ActorName: "Hemant"},
		},
	}
	raw, err := json.Marshal(toTaskPayload(task, domain.Actor{UserID: "u1"}, time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Activity []map[string]any `json:"activity"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Activity) != 2 {
		t.Fatalf("activity = %+v", got.Activity)
	}
	first := got.Activity[0]
	for key, want := range map[string]any{
		"id": "e2", "kind": "status_changed", "occurred_at": "2026-09-18T05:00:00Z", "occurred_label": "18/09/2026 10:30",
		"actor_user_id": "u1", "actor_name": "Ravi Teja", "actor_initials": "RT",
		"from_label": "To do", "to_label": "In progress", "from_value": "open", "to_value": "in_progress", "note_id": "",
		"summary": "Ravi Teja changed the status To do → In progress",
	} {
		if first[key] != want {
			t.Errorf("activity[0].%s = %v, want %v", key, first[key], want)
		}
	}
	if got.Activity[1]["summary"] != "Hemant created the task" || got.Activity[1]["actor_initials"] != "H" {
		t.Fatalf("activity[1] = %+v", got.Activity[1])
	}
	// An empty feed is an empty array on the wire, never null.
	rawEmpty, _ := json.Marshal(toTaskPayload(domain.Task{TaskID: "x"}, domain.Actor{}, time.Now()))
	if !strings.Contains(string(rawEmpty), `"activity":[]`) {
		t.Fatalf("empty activity must serialize as []: %s", rawEmpty)
	}
}
