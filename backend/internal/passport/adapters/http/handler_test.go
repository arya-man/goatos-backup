package http

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vgoats/goatos/backend/internal/passport/app"
)

func TestGetPassportRejectsBadGoatID(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, NewHandler(app.NewService(nil, nil, nil))) // readers unused: uuid check fails first

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/goats/not-a-uuid/passport", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for non-uuid goat_id, got %d", rec.Code)
	}
}
