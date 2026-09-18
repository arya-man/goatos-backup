package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/notificationcentre/app"
	"github.com/vgoats/goatos/backend/internal/notificationcentre/domain"
	"github.com/vgoats/goatos/backend/internal/permissions"
)

// TestRoutePatternsAreByteIdenticalToThePermissionTable is the guard the leadership-tasks
// handler warns about in prose: the permission table is matched by method + pattern, so a
// pattern that drifts by ONE BYTE from the mux pattern serves the route with no gate at all.
func TestRoutePatternsAreByteIdenticalToThePermissionTable(t *testing.T) {
	cases := []struct{ method, pattern, operationID string }{
		{http.MethodGet, ListRoute, "listAppNotifications"},
		{http.MethodPost, MarkReadRoute, "markAppNotificationsRead"},
	}
	for _, c := range cases {
		route, ok := permissions.Match(c.method, c.pattern)
		if !ok {
			t.Fatalf("%s %s is mounted but absent from permissions/routes.go -- it would serve UNGATED", c.method, c.pattern)
		}
		if route.Pattern != c.pattern {
			t.Errorf("pattern drift: mux %q vs table %q", c.pattern, route.Pattern)
		}
		if route.OperationID != c.operationID {
			t.Errorf("%s %s resolved to operation %q, want %q", c.method, c.pattern, route.OperationID, c.operationID)
		}
	}
	// And the mux really does mount exactly those two patterns.
	mux := http.NewServeMux()
	Register(mux, NewHandler(&stubService{}, nil))
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.pattern, strings.NewReader(`{"notification_request_ids":["x"]}`))
		if _, pattern := mux.Handler(req); pattern != c.method+" "+c.pattern {
			t.Errorf("%s %s mounted as %q", c.method, c.pattern, pattern)
		}
	}
}

type stubService struct {
	page    domain.Page
	listErr error

	readCount int
	readErr   error
	gotRead   app.MarkReadRequest
	gotList   app.ListRequest
}

func (s *stubService) List(_ context.Context, req app.ListRequest) (domain.Page, error) {
	s.gotList = req
	return s.page, s.listErr
}

func (s *stubService) MarkRead(_ context.Context, req app.MarkReadRequest) (int, error) {
	s.gotRead = req
	return s.readCount, s.readErr
}

// TestListRendersTheContractShape pins the exact JSON the already-built web bell is coded
// against: items[] with the context envelope, unread_count, next_cursor and trace_id, with
// every optional field OMITTED rather than sent empty.
func TestListRendersTheContractShape(t *testing.T) {
	svc := &stubService{page: domain.Page{
		Items: []domain.Notification{{
			NotificationRequestID: "11111111-1111-4111-8111-111111111111",
			NotificationType:      "rework",
			Title:                 "Weighing bounced",
			Body:                  "Shed 3 needs a reweigh.",
			Status:                "sent",
			RequestedAt:           "2026-09-18T04:00:00Z",
			ActorName:             "Ravi",
			Context: domain.Context{
				TaskID:     "22222222-2222-4222-8222-222222222222",
				TaskNo:     "12",
				Screen:     "weighing",
				GroupKey:   "weighing.shed",
				Priority:   "high",
				MessageKey: "weighing.verdict.rework",
				Target:     "/weighing",
				Status:     "rework",
			},
		}, {
			NotificationRequestID: "33333333-3333-4333-8333-333333333333",
			NotificationType:      "reminder",
			Title:                 "Pen visit due",
			Status:                "queued",
			RequestedAt:           "2026-09-18T03:00:00Z",
		}},
		UnreadCount: 7,
		NextCursor:  "Y3Vyc29y",
	}}
	rec := httptest.NewRecorder()
	NewHandler(svc, nil).List(rec, httptest.NewRequest(http.MethodGet, "/app/notifications?limit=2&cursor=abc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, key := range []string{"items", "unread_count", "next_cursor", "trace_id"} {
		if _, ok := body[key]; !ok {
			t.Errorf("response is missing %q: %s", key, rec.Body.String())
		}
	}
	if body["unread_count"].(float64) != 7 {
		t.Errorf("unread_count = %v", body["unread_count"])
	}
	items := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	first := items[0].(map[string]any)
	if first["read_at"] != nil {
		t.Errorf("an unread notification must OMIT read_at, got %v", first["read_at"])
	}
	if first["actor_name"] != "Ravi" {
		t.Errorf("actor_name = %v", first["actor_name"])
	}
	ctx := first["context"].(map[string]any)
	for key, want := range map[string]string{
		"task_id": "22222222-2222-4222-8222-222222222222", "task_no": "12", "screen": "weighing",
		"group_key": "weighing.shed", "priority": "high", "message_key": "weighing.verdict.rework",
		"target": "/weighing", "status": "rework",
	} {
		if ctx[key] != want {
			t.Errorf("context.%s = %v, want %q", key, ctx[key], want)
		}
	}
	// The second row names no task and no actor: those keys must be absent, not empty.
	second := items[1].(map[string]any)
	if _, present := second["actor_name"]; present {
		t.Error("actor_name must be omitted when the producer recorded no actor")
	}
	if len(second["context"].(map[string]any)) != 0 {
		t.Errorf("an empty context must render as {}, got %v", second["context"])
	}
	// The caller's identity came from the session, and the query only carried paging.
	if svc.gotList.Limit != 2 || svc.gotList.Cursor != "abc" {
		t.Errorf("list request = %+v", svc.gotList)
	}
}

// TestMarkReadRequiresAnIdempotencyKey pins the header the contract mandates.
func TestMarkReadRequiresAnIdempotencyKey(t *testing.T) {
	svc := &stubService{readCount: 3}
	h := NewHandler(svc, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/notifications/read", strings.NewReader(`{"notification_request_ids":["11111111-1111-4111-8111-111111111111"]}`))
	h.MarkRead(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a mark-as-read with no Idempotency-Key must be 400, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing_idempotency_key") {
		t.Errorf("body = %s", rec.Body.String())
	}
	if svc.gotRead.IdempotencyKey != "" {
		t.Error("the service must not be reached without an idempotency key")
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/app/notifications/read", strings.NewReader(`{"notification_request_ids":["11111111-1111-4111-8111-111111111111"]}`))
	req.Header.Set("Idempotency-Key", "bell-mark-read-1")
	h.MarkRead(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["read_count"].(float64) != 3 {
		t.Errorf("read_count = %v", body["read_count"])
	}
	if svc.gotRead.IdempotencyKey != "bell-mark-read-1" || len(svc.gotRead.IDs) != 1 {
		t.Errorf("mark read request = %+v", svc.gotRead)
	}
}

// TestMarkReadRefusesAnUnknownBodyField stops a client silently sending a field the server
// ignores -- e.g. a "workforce_member_id" someone adds later hoping to mark ANOTHER
// person's notifications read.
func TestMarkReadRefusesAnUnknownBodyField(t *testing.T) {
	svc := &stubService{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/notifications/read",
		strings.NewReader(`{"notification_request_ids":["11111111-1111-4111-8111-111111111111"],"workforce_member_id":"44444444-4444-4444-8444-444444444444"}`))
	req.Header.Set("Idempotency-Key", "bell-mark-read-2")
	NewHandler(svc, nil).MarkRead(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an unknown body field must be 400, got %d body %s", rec.Code, rec.Body.String())
	}
	if svc.gotRead.IdempotencyKey != "" {
		t.Error("the service must not be reached with an unknown field in the body")
	}
}

// TestListRejectsAnUnparseableOrZeroLimit: the RANGE check lives in the app service (see
// app.TestClampLimitRefusesOutOfRangePageSizes); the transport owns only the shapes it can
// see -- a non-numeric limit, and an explicit zero, which downstream reads as "absent".
func TestListRejectsAnUnparseableOrZeroLimit(t *testing.T) {
	for _, raw := range []string{"0", "not-a-number"} {
		rec := httptest.NewRecorder()
		NewHandler(&stubService{}, nil).List(rec, httptest.NewRequest(http.MethodGet, "/app/notifications?limit="+raw, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit=%s gave %d, want 400", raw, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "invalid_limit") {
			t.Errorf("limit=%s body = %s", raw, rec.Body.String())
		}
	}
}
