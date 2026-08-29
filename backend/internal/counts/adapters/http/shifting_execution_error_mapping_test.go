package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
)

func TestShiftingExecutionErrorMapsIdentityWriteConflictToConflict(t *testing.T) {
	handler := &AppWriteHandler{}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/app/counts/shifting-events/event-1/complete", nil)

	wrapped := fmt.Errorf("identity: relocate goats: verify expected source placement: %w", identityports.ErrWriteConflict)
	handler.writeShiftingExecutionError(rec, req, wrapped)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d for stale source-placement conflict", rec.Code, http.StatusConflict)
	}
	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error envelope: %v (body=%q)", err, rec.Body.String())
	}
	if body.Code != "shifting_source_changed" {
		t.Fatalf("code = %q, want shifting_source_changed", body.Code)
	}
	if body.Message == "" || body.Message == "internal server error" {
		t.Fatalf("message = %q, want an operator-readable stale movement message", body.Message)
	}
}
