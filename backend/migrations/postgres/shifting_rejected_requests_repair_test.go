package postgres

import (
	"context"
	"os"
	"testing"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// 000435 retires the movements a rejected pen-move approval left stranded at pending/pending, and
// touches nothing else: not a movement whose request is still pending, not an authorized movement
// whose (older) rejected request was superseded, not another tenant's row. A second run is a no-op.
func TestShiftingRejectedRequestsRepairRetiresOnlyStrandedMovements(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)

	const (
		tenantA     = "e4280000-0000-4000-8000-000000000001"
		tenantB     = "e4280000-0000-4000-8000-000000000002"
		parkA       = "e4280000-0000-4000-8000-0000000000a1"
		shedA       = "e4280000-0000-4000-8000-0000000000a2"
		parkB       = "e4280000-0000-4000-8000-0000000000b1"
		shedB       = "e4280000-0000-4000-8000-0000000000b2"
		user        = "e4280000-0000-4000-8000-0000000000ff"
		stranded    = "e4280000-0000-4000-8000-000000000101"
		stillOpen   = "e4280000-0000-4000-8000-000000000102"
		authorized  = "e4280000-0000-4000-8000-000000000103"
		otherTenant = "e4280000-0000-4000-8000-000000000104"
	)
	for _, tn := range []string{tenantA, tenantB} {
		if _, err := pool.Exec(ctx, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Repair '||$1, 'active')`, tn); err != nil {
			t.Fatalf("seed tenant: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, name, status) VALUES
 ($1::uuid, $5::uuid, 'park', 'Repair Park A', 'active'), ($2::uuid, $5::uuid, 'shed', 'Repair Shed A', 'active'),
 ($3::uuid, $6::uuid, 'park', 'Repair Park B', 'active'), ($4::uuid, $6::uuid, 'shed', 'Repair Shed B', 'active')`,
		parkA, shedA, parkB, shedB, tenantA, tenantB); err != nil {
		t.Fatalf("seed locations: %v", err)
	}
	event := func(id, tenant, park, shed, auth, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO shifting_events (shifting_event_id, tenant_id, logical_shifting_event_key, priority, category,
  destination_park_id, destination_shed_id, raised_at, effective_at, authorization_state, event_status,
  authorized_at, authorized_by, source_system, source_ref, payload_hash, idempotency_key, request_fingerprint)
VALUES ($1::uuid, $2::uuid, 'k-'||$1, 'low', 'normal', $3::uuid, $4::uuid, now(), now(), $5, $6,
  CASE WHEN $5 = 'authorized' THEN now() END, CASE WHEN $5 = 'authorized' THEN $7::uuid END,
  'goatos_canonical', 'test', 'h', 'i-'||$1, 'f-'||$1)`, id, tenant, park, shed, auth, status, user); err != nil {
			t.Fatalf("seed shifting event %s: %v", id, err)
		}
	}
	request := func(eventID, tenant, status, key string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
INSERT INTO counts_approval_requests (tenant_id, request_type, payload, shifting_event_id, status,
  raised_by_user_id, raised_at, decided_by_user_id, decided_at, decision_reason,
  applied_result_type, applied_result_id, idempotency_key, request_fingerprint)
VALUES ($1::uuid, 'shifting', '{}'::jsonb, $2::uuid, $3, $4::uuid, now(),
  CASE WHEN $3 <> 'pending' THEN $4::uuid END, CASE WHEN $3 <> 'pending' THEN now() END,
  CASE WHEN $3 = 'rejected' THEN 'Pen full' END,
  CASE WHEN $3 = 'approved' THEN 'shifting_event' END, CASE WHEN $3 = 'approved' THEN $2::uuid END,
  $5, $5||'-fp')`, tenant, eventID, status, user, key); err != nil {
			t.Fatalf("seed request %s: %v", key, err)
		}
	}
	event(stranded, tenantA, parkA, shedA, "pending", "pending")
	request(stranded, tenantA, "rejected", "r-stranded")
	event(stillOpen, tenantA, parkA, shedA, "pending", "pending")
	request(stillOpen, tenantA, "pending", "r-open")
	event(authorized, tenantA, parkA, shedA, "authorized", "authorized")
	request(authorized, tenantA, "rejected", "r-old")
	request(authorized, tenantA, "approved", "r-new")
	event(otherTenant, tenantB, parkB, shedB, "pending", "pending")
	request(otherTenant, tenantB, "rejected", "r-other")

	migration, err := os.ReadFile("000435_shifting_rejected_requests_retire_movement.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for run := 1; run <= 2; run++ {
		if _, err := pool.Exec(ctx, migrationUp(string(migration))); err != nil {
			t.Fatalf("apply migration (run %d): %v", run, err)
		}
	}

	want := map[string][3]any{
		stranded:    {"rejected", "rejected", 2},
		stillOpen:   {"pending", "pending", 1},
		authorized:  {"authorized", "authorized", 1},
		otherTenant: {"rejected", "rejected", 2},
	}
	for id, w := range want {
		var auth, status string
		var version int
		if err := pool.QueryRow(ctx, `SELECT authorization_state, event_status, row_version FROM shifting_events WHERE shifting_event_id = $1::uuid`, id).
			Scan(&auth, &status, &version); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if auth != w[0] || status != w[1] || version != w[2] {
			t.Fatalf("event %s = %s/%s v%d, want %s/%s v%d (run twice: idempotent)", id, auth, status, version, w[0], w[1], w[2])
		}
	}
}
