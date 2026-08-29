package appanalyticshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

func TestFeedDirectionForensicEventsReachOCI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping OCI-backed integration test in short mode")
	}
	dsn := os.Getenv("GOATOS_ANALYTICS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOATOS_ANALYTICS_TEST_DATABASE_URL unset")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect analytics db: %v", err)
	}

	var tenantID string
	if err := pool.QueryRow(ctx, `SELECT tenant_id::text FROM tenants ORDER BY tenant_id LIMIT 1`).Scan(&tenantID); err != nil {
		t.Fatalf("load tenant: %v", err)
	}
	var actorID string
	if err := pool.QueryRow(ctx, `
SELECT user_id::text
FROM user_scope_grants
WHERE tenant_id = $1::uuid
  AND role = 'operator'
  AND status = 'active'
  AND user_id IS NOT NULL
ORDER BY valid_from DESC, grant_id DESC
LIMIT 1`, tenantID).Scan(&actorID); err != nil {
		t.Fatalf("load operator actor: %v", err)
	}

	h := NewHandler(pool)
	clientEventID := "throwaway-feed-direction-e2e-" + time.Now().UTC().Format("20060102T150405.000000000")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `
DELETE FROM analytics.app_events
WHERE tenant_id = $1::uuid
  AND client_event_id = $2`, tenantID, clientEventID)
		pool.Close()
	})
	reqBody := recordEventRequest{
		EventName: "feed_distribution_capture_tapped",
		Properties: map[string]string{
			"kind":      "feed_video",
			"reason":    "throwaway_oci_e2e",
			"slot_mask": "feed_video",
		},
		ClientEventTimeMS: time.Now().UnixMilli(),
		Flavor:            "stg",
		AppVersionName:    "0.1.31-stg",
		AppVersionCode:    32,
		ClientEventID:     clientEventID,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest("POST", "/app/analytics/events", strings.NewReader(string(bodyBytes)))
	req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), tenantID))
	req = req.WithContext(httpmiddleware.WithActorID(req.Context(), actorID))
	req = req.WithContext(httpmiddleware.WithDeviceID(req.Context(), "throwaway-oci-e2e-poco"))
	req = req.WithContext(httpmiddleware.WithClientInfo(req.Context(), httpmiddleware.ClientInfo{
		AppVersion:     "0.1.31-stg",
		AppVersionCode: "32",
		BuildType:      "stgDebug",
		DeviceID:       "throwaway-oci-e2e-poco",
		Platform:       "android",
		OSVersion:      "Android 14",
		SDKVersion:     "34",
		DeviceModel:    "Xiaomi 22101320I",
	}))

	rec := httptest.NewRecorder()
	h.RecordEvent(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("record event status=%d body=%s", rec.Code, rec.Body.String())
	}

	var count int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM analytics.app_events
WHERE tenant_id = $1::uuid
  AND actor_id = $2::uuid
  AND device_id = 'throwaway-oci-e2e-poco'
  AND event_name = 'feed_distribution_capture_tapped'
  AND client_event_id = $3
  AND properties->>'kind' = 'feed_video'
  AND client_info->>'device_model' = 'Xiaomi 22101320I'`, tenantID, actorID, clientEventID).Scan(&count); err != nil {
		t.Fatalf("query inserted event: %v", err)
	}
	if count != 1 {
		t.Fatalf("inserted event count=%d, want 1", count)
	}
}
