package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
)

// TestListGoldenJSON pins the feed's exact JSON (2026-09-19: + context.load_id for the
// "Open load" deep link, + whole-feed total_count for the All/Archived chips). trace_id is
// request-minted and is the only key not compared literally. Optional fields stay OMITTED.
func TestListGoldenJSON(t *testing.T) {
	svc := &stubService{page: domain.Page{
		Items: []domain.Notification{{
			NotificationRequestID: "11111111-1111-4111-8111-111111111111",
			NotificationType:      "loadwise",
			Title:                 "Load L-042 accepted",
			Body:                  "38 animals moved to the herd.",
			Status:                "sent",
			RequestedAt:           "2026-09-18T04:00:00Z",
			ReadAt:                "2026-09-18T05:00:00Z",
			ActorName:             "Ravi",
			Context: domain.Context{
				Screen: "loadwise", Target: "/sales/loads", MessageKey: "procurement.load.accepted",
				LoadID: "44444444-4444-4444-8444-444444444444",
			},
		}, {
			NotificationRequestID: "33333333-3333-4333-8333-333333333333",
			NotificationType:      "reminder",
			Title:                 "Pen visit due",
			Status:                "queued",
			RequestedAt:           "2026-09-18T03:00:00Z",
		}},
		UnreadCount: 1,
		TotalCount:  9,
		NextCursor:  "Y3Vyc29y",
	}}
	rec := httptest.NewRecorder()
	NewHandler(svc, nil).List(rec, httptest.NewRequest(http.MethodGet, "/app/notifications?limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := got["trace_id"].(string); !ok {
		t.Fatalf("trace_id missing: %s", rec.Body.String())
	}
	delete(got, "trace_id")
	const golden = `{
  "items": [
    {
      "notification_request_id": "11111111-1111-4111-8111-111111111111",
      "notification_type": "loadwise",
      "title": "Load L-042 accepted",
      "body": "38 animals moved to the herd.",
      "status": "sent",
      "requested_at": "2026-09-18T04:00:00Z",
      "read_at": "2026-09-18T05:00:00Z",
      "actor_name": "Ravi",
      "context": {
        "screen": "loadwise",
        "message_key": "procurement.load.accepted",
        "target": "/sales/loads",
        "load_id": "44444444-4444-4444-8444-444444444444"
      }
    },
    {
      "notification_request_id": "33333333-3333-4333-8333-333333333333",
      "notification_type": "reminder",
      "title": "Pen visit due",
      "body": "",
      "status": "queued",
      "requested_at": "2026-09-18T03:00:00Z",
      "context": {}
    }
  ],
  "unread_count": 1,
  "total_count": 9,
  "next_cursor": "Y3Vyc29y"
}`
	var want map[string]any
	if err := json.Unmarshal([]byte(golden), &want); err != nil {
		t.Fatalf("golden: %v", err)
	}
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	wantJSON, _ := json.MarshalIndent(want, "", "  ")
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("feed JSON drifted from golden:\n--- got\n%s\n--- want\n%s", gotJSON, wantJSON)
	}
}
