package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vaccexechttp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/http"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
)

type rescheduleHTTPResult struct {
	ObligationID     string `json:"obligation_id"`
	IdempotentReplay bool   `json:"idempotent_replay"`
}

func rescheduleObligationViaHTTP(t *testing.T, fx *Fixture, obligationID, key string, dueAt, windowStart time.Time, windowEnd *time.Time) (rescheduleHTTPResult, int, string) {
	// Default clock: one day before the requested due date, so a fixed future-dated reschedule stays
	// deterministically "in the future" regardless of the wall clock (no date-relative flakiness).
	return rescheduleObligationViaHTTPAt(t, fx, obligationID, key, dueAt.Add(-24*time.Hour), dueAt, windowStart, windowEnd)
}

func rescheduleObligationViaHTTPAt(t *testing.T, fx *Fixture, obligationID, key string, now, dueAt, windowStart time.Time, windowEnd *time.Time) (rescheduleHTTPResult, int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"due_at": dueAt, "window_start": windowStart, "window_end": windowEnd})
	if err != nil {
		t.Fatalf("marshal reschedule request: %v", err)
	}
	mux := http.NewServeMux()
	vaccexechttp.Register(mux, vaccexechttp.NewHandler(vaccexecapp.NewService(fx.VaccExec), fx.Obl).WithClock(func() time.Time { return now }))
	req := httptest.NewRequest(http.MethodPost, "/app/vaccination/obligations/"+obligationID+"/reschedule", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), fxTenant))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var result rescheduleHTTPResult
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode reschedule response: %v", err)
		}
	}
	return result, rec.Code, fmt.Sprintf("HTTP %d: %s", rec.Code, rec.Body.String())
}
