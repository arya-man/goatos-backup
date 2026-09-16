package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/alerts/domain"
)

// Uses session-local temporary fixtures only; no application tables are changed.
func TestEventReadersPostgres(t *testing.T) {
	dsn := os.Getenv("GOATOS_ALERTS_TEST_DSN")
	if dsn == "" {
		t.Skip("GOATOS_ALERTS_TEST_DSN is required for SQL execution proof")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("test database unavailable")
	}
	defer pool.Close()
	exec := func(sql string) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exec(`
 CREATE TEMP TABLE goats (tenant_id uuid,goat_id uuid,park_id uuid,shed_id uuid,created_at timestamptz,exited_at timestamptz,lifecycle_status text,exit_reason text,breed text,sex text,origin_type text);
 CREATE TEMP TABLE goat_births (tenant_id uuid,child_goat_id uuid,mother_goat_id uuid,litter_size smallint,created_at timestamptz);
 CREATE TEMP TABLE goat_identifiers (tenant_id uuid,goat_id uuid,identifier_value text,identifier_type text,status text,is_primary_for_goat boolean,created_at timestamptz);
 CREATE TEMP TABLE locations (tenant_id uuid,location_id uuid,name text);
 CREATE TEMP TABLE goat_shed_partitions (tenant_id uuid,goat_id uuid,partition_label text);
 CREATE TEMP TABLE shifting_events (tenant_id uuid,shifting_event_id uuid,source_park_id uuid,destination_park_id uuid,source_shed_id uuid,destination_shed_id uuid,source_partition_label text,destination_partition_label text,event_status text,priority text,raised_at timestamptz,authorized_at timestamptz);
 CREATE TEMP TABLE shifting_event_impacts (tenant_id uuid,shifting_event_id uuid,head_count bigint);
 CREATE TEMP TABLE feed_purchases (tenant_id uuid,park_id uuid,feed_purchase_id uuid,feed_item_label text,quantity_kg numeric,vendor text,delivery_status text,purchase_date date,created_at timestamptz);
 INSERT INTO goats SELECT '00000000-0000-4000-8000-000000000001',md5(i::text)::uuid,'00000000-0000-4000-8000-000000000002',NULL,'2026-09-15 18:30Z','2026-09-15 18:30Z','dead','','','', 'imported' FROM generate_series(1,30) i;
 INSERT INTO goat_births SELECT tenant_id,goat_id,goat_id,1,created_at FROM goats;
 INSERT INTO goat_identifiers SELECT tenant_id,goat_id,'BLE-only','smart_ble_tag','active',true,created_at FROM goats;
 INSERT INTO goat_identifiers SELECT tenant_id,goat_id,'RFID-2','animal_identifier_2','active',false,created_at FROM goats;
 INSERT INTO goat_identifiers SELECT tenant_id,goat_id,'RFID-1','animal_identifier_1','active',false,created_at FROM goats;
 INSERT INTO shifting_events SELECT tenant_id,goat_id,park_id,park_id,NULL,goat_id,'','','authorized','normal',created_at,created_at FROM goats;
 INSERT INTO shifting_event_impacts SELECT tenant_id,shifting_event_id,3 FROM shifting_events;
 INSERT INTO feed_purchases SELECT tenant_id,park_id,goat_id,'Feed',50,'','received','2026-09-16',created_at FROM goats;
 `)
	repo := NewRepository(pool, 5*time.Second)
	tenant, park := "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"
	for _, kind := range []domain.EventKind{domain.EventBirthRecorded, domain.EventDeathRecorded, domain.EventAnimalAdded, domain.EventShiftingRaised, domain.EventShiftingApproved, domain.EventFeedPurchaseRecorded} {
		page, err := repo.Events(ctx, tenant, park, kind, "2026-09-16")
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if len(page.Rows) != 25 || page.Total != 30 {
			t.Fatalf("%s rows=%d total=%d", kind, len(page.Rows), page.Total)
		}
		if kind == domain.EventAnimalAdded && page.Rows[0].Subject != "tag RFID-1" {
			t.Fatal(page.Rows[0].Subject)
		}
		prior, err := repo.Events(ctx, tenant, park, kind, "2026-09-15")
		if err != nil || len(prior.Rows) != 0 {
			t.Fatalf("IST boundary %s: %+v %v", kind, prior, err)
		}
	}
	exec(`UPDATE goats SET lifecycle_status='sold'; DELETE FROM goat_identifiers WHERE identifier_type='animal_identifier_1'`)
	page, err := repo.Events(ctx, tenant, park, domain.EventAnimalSold, "2026-09-16")
	if err != nil || len(page.Rows) != 25 || page.Rows[0].Subject != "tag RFID-2" {
		t.Fatalf("secondary tag: %+v %v", page, err)
	}
	exec(`DELETE FROM goat_identifiers WHERE identifier_type='animal_identifier_2'`)
	page, err = repo.Events(ctx, tenant, park, domain.EventAnimalAdded, "2026-09-16")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range page.Rows {
		if !strings.Contains(e.Subject, "no tag on record") {
			t.Fatalf("BLE must not be called RFID: %s", e.Subject)
		}
	}
	page, err = repo.Events(ctx, tenant, "00000000-0000-4000-8000-000000000003", domain.EventAnimalAdded, "2026-09-16")
	if err != nil || page.Total != 0 || len(page.Rows) != 0 {
		t.Fatalf("park scope: %+v %v", page, err)
	}
	// A bulk import must still transfer only the preview, with an exact total.
	exec(`INSERT INTO goats SELECT '00000000-0000-4000-8000-000000000001',md5(i::text)::uuid,'00000000-0000-4000-8000-000000000002',NULL,'2026-09-15 18:30Z',NULL,'alive','','','', 'imported' FROM generate_series(31,50000) i`)
	started := time.Now()
	page, err = repo.Events(ctx, tenant, park, domain.EventAnimalAdded, "2026-09-16")
	if err != nil || page.Total != 50000 || len(page.Rows) != 25 {
		t.Fatalf("bulk preview: rows=%d total=%d err=%v", len(page.Rows), page.Total, err)
	}
	t.Logf("bulk fixture: 50000 matching records, %d returned, total %d, elapsed %s", len(page.Rows), page.Total, time.Since(started))

}
