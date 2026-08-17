package appanalyticshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// TestRecordEventClientEventIdDedup verifies that duplicate client_event_ids within
// the same tenant result in only one stored event (ON CONFLICT DO NOTHING).
func TestRecordEventClientEventIdDedup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}

	ctx := context.Background()
	dsn := os.Getenv("GOATOS_ANALYTICS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOATOS_ANALYTICS_TEST_DATABASE_URL unset; dedupe test needs a real Postgres")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	defer pool.Close()

	h := NewHandler(pool)

	tenantID := "550e8400-e29b-41d4-a716-446655440000"
	deviceID := "device-001"
	clientEventID := "client-event-dedup-001"
	eventName := "test_event_dedup"

	// Prepare request body
	reqBody := recordEventRequest{
		EventName:         eventName,
		Properties:        map[string]string{"key": "value"},
		ClientEventTimeMS: 1000000,
		Flavor:            "test",
		AppVersionName:    "1.0.0",
		AppVersionCode:    1,
		ClientEventID:     clientEventID,
	}

	// First request
	bodyBytes, _ := json.Marshal(reqBody)
	req1 := httptest.NewRequest("POST", "/app/analytics/events", strings.NewReader(string(bodyBytes)))
	req1 = req1.WithContext(httpmiddleware.WithTenantID(ctx, tenantID))
	req1 = req1.WithContext(httpmiddleware.WithDeviceID(req1.Context(), deviceID))
	w1 := httptest.NewRecorder()
	h.RecordEvent(w1, req1)

	if w1.Code != http.StatusOK {
		t.Errorf("first request failed with status %d", w1.Code)
	}

	// Second request with same client_event_id and tenant_id
	req2 := httptest.NewRequest("POST", "/app/analytics/events", strings.NewReader(string(bodyBytes)))
	req2 = req2.WithContext(httpmiddleware.WithTenantID(ctx, tenantID))
	req2 = req2.WithContext(httpmiddleware.WithDeviceID(req2.Context(), deviceID))
	w2 := httptest.NewRecorder()
	h.RecordEvent(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("second request failed with status %d", w2.Code)
	}

	// Verify only one event was stored
	var count int64
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM analytics.app_events
		WHERE tenant_id = $1 AND client_event_id = $2
	`, tenantID, clientEventID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query event count: %v", err)
	}

	if count != 1 {
		t.Errorf("expected 1 event, got %d; dedup failed", count)
	}
}
