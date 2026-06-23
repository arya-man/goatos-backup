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
		{ObligationID: "o1", Status: "due", DueAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
	}}
	mux := http.NewServeMux()
	Register(mux, NewHandler(fake))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/action-center/obligations", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if fake.gotStatus != "due" || fake.gotLimit != 100 {
		t.Fatalf("defaults: status=%s limit=%d", fake.gotStatus, fake.gotLimit)
	}
	var resp dueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Items) != 1 || resp.Items[0].ObligationID != "o1" {
		t.Fatalf("response: %+v err=%v", resp, err)
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
	// bad status → 400
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/action-center/obligations?status=completed", nil))
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
