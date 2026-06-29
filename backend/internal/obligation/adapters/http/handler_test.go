package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

type fakeDue struct {
	gotStatus    string
	gotDueBefore time.Time
	gotLimit     int32
	rows         []domain.DueObligation
}

func (f *fakeDue) ListDue(_ context.Context, _, status string, dueBefore time.Time, limit int32) ([]domain.DueObligation, error) {
	f.gotStatus, f.gotDueBefore, f.gotLimit = status, dueBefore, limit
	return f.rows, nil
}

func TestListDueDefaultsAndShape(t *testing.T) {
	fake := &fakeDue{rows: []domain.DueObligation{
		{ObligationID: "o1", Status: "due", DueAt: time.Now().Add(24 * time.Hour)},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/action-center/obligations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotStatus != "scheduled_or_due" || fake.gotLimit != 100 {
		t.Fatalf("defaults: status=%s limit=%d", fake.gotStatus, fake.gotLimit)
	}
	var resp dueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Items) != 1 || resp.Items[0].ObligationID != "o1" || resp.Items[0].WorkState != "due" {
		t.Fatalf("response: %+v err=%v", resp, err)
	}
}

func TestListDueOverdueUsesNowNotDueBefore(t *testing.T) {
	asOf := time.Date(2026, time.June, 29, 9, 0, 0, 0, time.UTC)
	fake := &fakeDue{rows: []domain.DueObligation{
		{ObligationID: "scheduled-window-future", Status: "scheduled", DueAt: asOf.Add(-15 * time.Minute)},
		{ObligationID: "due-future", Status: "due", DueAt: asOf.Add(time.Hour)},
	}}
	now := asOf.Add(-30 * time.Minute)
	handler := NewHandler(fake)
	handler.now = func() time.Time { return now }
	mux := http.NewServeMux()
	Register(mux, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/action-center/obligations?work_state=overdue&due_before=2026-06-29T09:00:00Z", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotStatus != "scheduled_or_due_overdue" {
		t.Fatalf("repo status=%s, want scheduled_or_due_overdue", fake.gotStatus)
	}
	var resp dueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items=%+v, want none because overdue is computed against now, not due_before", resp.Items)
	}
}

func TestListDueClampsLimitAndValidates(t *testing.T) {
	fake := &fakeDue{}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake))

	// limit over max → clamped to 500
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/action-center/obligations?limit=9000", nil))
	if rec.Code != http.StatusOK || fake.gotLimit != 500 {
		t.Fatalf("limit clamp: code=%d limit=%d", rec.Code, fake.gotLimit)
	}
	// unknown status → 400
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/action-center/obligations?status=not-a-state", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status: want 400, got %d", rec.Code)
	}
	// bad due_before → 400
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/action-center/obligations?due_before=not-a-time", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad due_before: want 400, got %d", rec.Code)
	}
}
