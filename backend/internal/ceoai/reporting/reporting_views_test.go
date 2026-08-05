// Package reporting holds adversarial grain/identity proofs for the ceo_ai.*
// governed reporting views defined in migration 000020. These are the
// projection-review tests the aggregate-projection guard requires: each proves
// a COUNT/GROUP-BY view keeps the correct grain under join fan-out, scope
// nesting, status bucketing, and business-day shifts. Postgres-gated (Docker).
package reporting

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

func newDB(t *testing.T, ctx context.Context) (*pgxpool.Pool, string) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	var tenant string
	if err := pool.QueryRow(ctx,
		`INSERT INTO tenants (tenant_id, name, status) VALUES (gen_random_uuid(), 'Report Tenant', 'active') RETURNING tenant_id::text`,
	).Scan(&tenant); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return pool, tenant
}

func custodian(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO parties (party_id, party_type, display_name, status)
		 VALUES (gen_random_uuid(), 'org', 'Custodian', 'active') RETURNING party_id::text`,
	).Scan(&id); err != nil {
		t.Fatalf("insert party: %v", err)
	}
	return id
}

func park(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, name, country, timezone, status, row_version, display_order)
		 VALUES (gen_random_uuid(), $1, 'park', $2, 'IN', 'Asia/Kolkata', 'active', 1, 0) RETURNING location_id::text`,
		tenant, name).Scan(&id); err != nil {
		t.Fatalf("insert park: %v", err)
	}
	return id
}

func shed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, parkID, name string, capacity *int) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO locations (location_id, tenant_id, location_type, name, parent_location_id, country, timezone, status, row_version, display_order)
		 VALUES (gen_random_uuid(), $1, 'shed', $2, $3, 'IN', 'Asia/Kolkata', 'active', 1, 0) RETURNING location_id::text`,
		tenant, name, parkID).Scan(&id); err != nil {
		t.Fatalf("insert shed: %v", err)
	}
	if capacity != nil {
		if _, err := pool.Exec(ctx,
			`INSERT INTO shed_profiles (location_id, tenant_id, capacity, has_icu, notes, context, row_version)
			 VALUES ($1, $2, $3, false, '', '{}'::jsonb, 1)`,
			id, tenant, *capacity); err != nil {
			t.Fatalf("insert shed_profile: %v", err)
		}
	}
	return id
}

// goat inserts one animal. originBirth stamps origin_type='birth' with entry_date;
// when exited is non-nil the animal is a mortality exit on that instant.
func goat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, parkID, shedID, species, lifecycle string, entry *time.Time, exited *time.Time) {
	t.Helper()
	origin := "procured"
	if entry != nil {
		origin = "birth"
	}
	exitReason := (*string)(nil)
	if exited != nil {
		r := "died"
		exitReason = &r
	}
	party := custodian(t, ctx, pool, tenant)
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, management_stage, health_status,
		                    custodian_party_id, park_id, shed_id, origin_type, entry_date, exited_at, exit_reason, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, 'G-' || lpad((floor(random()*1000000000))::bigint::text, 9, '0'), $2, 'female', $3, 'active_adult', 'healthy',
		         $10, $4, $5, $6, $7, $8, $9, 1, now(), now())`,
		tenant, species, lifecycle, parkID, shedID, origin, entry, exited, exitReason, party); err != nil {
		t.Fatalf("insert goat: %v", err)
	}
}

// goatWithID is goat() but returns the inserted goat_id, needed by callers that
// must also insert a goat_shed_partitions row for the same animal.
func goatWithID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, parkID, shedID, species, lifecycle string, entry *time.Time, exited *time.Time) string {
	t.Helper()
	origin := "procured"
	if entry != nil {
		origin = "birth"
	}
	exitReason := (*string)(nil)
	if exited != nil {
		r := "died"
		exitReason = &r
	}
	party := custodian(t, ctx, pool, tenant)
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, management_stage, health_status,
		                    custodian_party_id, park_id, shed_id, origin_type, entry_date, exited_at, exit_reason, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, 'G-' || lpad((floor(random()*1000000000))::bigint::text, 9, '0'), $2, 'female', $3, 'active_adult', 'healthy',
		         $10, $4, $5, $6, $7, $8, $9, 1, now(), now())
		 RETURNING goat_id::text`,
		tenant, species, lifecycle, parkID, shedID, origin, entry, exited, exitReason, party).Scan(&id); err != nil {
		t.Fatalf("insert goat: %v", err)
	}
	return id
}

// partition assigns a goat to a partition of the shed it already sits in
// (goat_shed_partitions is a "which sub-location inside its shed" fact, keyed
// (tenant_id, goat_id)). label may be the numeric convention ("2") or the
// "Part N" convention -- both are stored verbatim.
func partition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, goatID, shedID, label, sourceShedName string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name, updated_at)
		 VALUES ($1, $2, $3, $4, $5, now())`,
		tenant, goatID, shedID, label, sourceShedName); err != nil {
		t.Fatalf("insert goat_shed_partitions: %v", err)
	}
}

func seat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, shedID, name string, backup bool) {
	t.Helper()
	var memberID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, metadata, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, encode(gen_random_bytes(4),'hex'), $2, 'active', 'operator', '{}'::jsonb, 1, now(), now()) RETURNING workforce_member_id::text`,
		tenant, name).Scan(&memberID); err != nil {
		t.Fatalf("insert workforce member: %v", err)
	}
	code := "shed_manager"
	if backup {
		code = "shed_backup"
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO workforce_positions (position_id, tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, is_backup_slot, status, valid_from, valid_to, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, $2, 'shed', $3, $4, 'manager', $5, 'active', now() - interval '1 day', NULL, 1, now(), now())`,
		tenant, memberID, shedID, code, backup); err != nil {
		t.Fatalf("insert workforce position: %v", err)
	}
}

// TestShedCapacityOneToMany proves the owner+backup seat LEFT JOINs do NOT
// fan out the occupancy COUNT: a shed with 3 animals and both a manager and a
// backup seat still reports animals=3, not 6.
func TestShedCapacityOneToMany(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	cap10 := 10
	pk := park(t, ctx, pool, tenant, "Park A")
	sh := shed(t, ctx, pool, tenant, pk, "Shed A1", &cap10)
	for i := 0; i < 3; i++ {
		goat(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	}
	seat(t, ctx, pool, tenant, sh, "Manager", false)
	seat(t, ctx, pool, tenant, sh, "Backup", true)

	var animals int64
	var owner, backup string
	if err := pool.QueryRow(ctx,
		`SELECT animals, owner_label, backup_label FROM ceo_ai.shed_capacity_current WHERE tenant_id=$1 AND shed_label='Shed A1'`,
		tenant).Scan(&animals, &owner, &backup); err != nil {
		t.Fatalf("query view: %v", err)
	}
	if animals != 3 {
		t.Fatalf("seat fan-out inflated occupancy: animals=%d want 3", animals)
	}
	if owner != "Manager" || backup != "Backup" {
		t.Fatalf("seat labels wrong: owner=%q backup=%q", owner, backup)
	}
}

// TestShedStatusMatrix proves the capacity status bucket is computed per shed
// from occupancy vs capacity across every bucket (over/at/under/unknown).
func TestShedStatusMatrix(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park S")
	cap2 := 2
	over := shed(t, ctx, pool, tenant, pk, "Over", &cap2)
	at := shed(t, ctx, pool, tenant, pk, "At", &cap2)
	under := shed(t, ctx, pool, tenant, pk, "Under", &cap2)
	unknown := shed(t, ctx, pool, tenant, pk, "Unknown", nil)
	place := func(sh string, n int) {
		for i := 0; i < n; i++ {
			goat(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
		}
	}
	place(over, 3)  // > capacity
	place(at, 2)    // == capacity
	place(under, 1) // < capacity
	place(unknown, 5)

	want := map[string]string{"Over": "over_capacity", "At": "at_capacity", "Under": "under_capacity", "Unknown": "unknown_capacity"}
	rows, err := pool.Query(ctx,
		`SELECT shed_label, status FROM ceo_ai.shed_capacity_current WHERE tenant_id=$1`, tenant)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var label, status string
		if err := rows.Scan(&label, &status); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[label] = status
	}
	for label, exp := range want {
		if got[label] != exp {
			t.Fatalf("shed %q status=%q want %q", label, got[label], exp)
		}
	}
}

// TestShedScopeHierarchy proves park→shed scope is preserved: identical shed
// names in two different parks are distinct rows with the right park_label and
// their own occupancy count (no cross-park collapse).
func TestShedScopeHierarchy(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	cap10 := 10
	p1 := park(t, ctx, pool, tenant, "North")
	p2 := park(t, ctx, pool, tenant, "South")
	s1 := shed(t, ctx, pool, tenant, p1, "S1", &cap10)
	s2 := shed(t, ctx, pool, tenant, p2, "S1", &cap10)
	for i := 0; i < 2; i++ {
		goat(t, ctx, pool, tenant, p1, s1, "goat", "alive", nil, nil)
	}
	for i := 0; i < 5; i++ {
		goat(t, ctx, pool, tenant, p2, s2, "goat", "alive", nil, nil)
	}
	type row struct{ animals int64 }
	got := map[string]int64{}
	rows, err := pool.Query(ctx,
		`SELECT park_label, animals FROM ceo_ai.shed_capacity_current WHERE tenant_id=$1 AND shed_label='S1'`, tenant)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var pl string
		var a int64
		if err := rows.Scan(&pl, &a); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[pl] = a
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 distinct park/shed rows, got %d", count)
	}
	if got["North"] != 2 || got["South"] != 5 {
		t.Fatalf("scope collapse: North=%d (want 2) South=%d (want 5)", got["North"], got["South"])
	}
}

// TestCountsMovementDateShift proves counts_movement_daily buckets births and
// deaths on the correct Asia/Kolkata business day — two births on different
// entry_dates land in different day rows and a mortality exit lands as a death.
func TestCountsMovementDateShift(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park D")
	sh := shed(t, ctx, pool, tenant, pk, "Shed D1", nil)
	day1 := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, 7, 11, 0, 0, 0, 0, time.UTC)
	// Two births on day1, one on day2.
	goat(t, ctx, pool, tenant, pk, sh, "goat", "alive", &day1, nil)
	goat(t, ctx, pool, tenant, pk, sh, "goat", "alive", &day1, nil)
	goat(t, ctx, pool, tenant, pk, sh, "goat", "alive", &day2, nil)
	// One mortality exit on day2.
	exit := time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC)
	goat(t, ctx, pool, tenant, pk, sh, "goat", "dead", nil, &exit)

	births := map[string]int64{}
	deaths := map[string]int64{}
	rows, err := pool.Query(ctx,
		`SELECT event_date::text, births, deaths FROM ceo_ai.counts_movement_daily WHERE tenant_id=$1 AND shed_label='Shed D1' ORDER BY event_date`,
		tenant)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		var b, dth int64
		if err := rows.Scan(&d, &b, &dth); err != nil {
			t.Fatalf("scan: %v", err)
		}
		births[d] = b
		deaths[d] = dth
	}
	if births["2026-07-10"] != 2 {
		t.Fatalf("births day1=%d want 2", births["2026-07-10"])
	}
	if births["2026-07-11"] != 1 {
		t.Fatalf("births day2=%d want 1", births["2026-07-11"])
	}
	if deaths["2026-07-11"] != 1 {
		t.Fatalf("deaths day2=%d want 1", deaths["2026-07-11"])
	}
}

// TestAnimalCurrentScopePartitionLabel proves ceo_ai.animal_current_scope
// (migration 000110) carries partition_label per goat: two goats in the SAME
// physical shed but different partitions come back as distinct labels, and a
// goat in a non-partitioned shed comes back with a NULL/bare label -- never the
// "whole" sentinel.
func TestAnimalCurrentScopePartitionLabel(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park P")
	partitioned := shed(t, ctx, pool, tenant, pk, "Castro", nil)
	bare := shed(t, ctx, pool, tenant, pk, "Yashoda", nil)

	g1 := goatWithID(t, ctx, pool, tenant, pk, partitioned, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g1, partitioned, "1", "Castro 1")
	g2 := goatWithID(t, ctx, pool, tenant, pk, partitioned, "goat", "alive", nil, nil)
	partition(t, ctx, pool, tenant, g2, partitioned, "2", "Castro 2")
	g3 := goatWithID(t, ctx, pool, tenant, pk, bare, "goat", "alive", nil, nil)

	got := map[string]string{}
	rows, err := pool.Query(ctx,
		`SELECT animal_id::text, COALESCE(partition_label, '') FROM ceo_ai.animal_current_scope WHERE tenant_id=$1`, tenant)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, label string
		if err := rows.Scan(&id, &label); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[id] = label
	}
	if got[g1] != "1" {
		t.Fatalf("g1 partition_label=%q want %q", got[g1], "1")
	}
	if got[g2] != "2" {
		t.Fatalf("g2 partition_label=%q want %q", got[g2], "2")
	}
	if label, ok := got[g3]; !ok || label != "" {
		t.Fatalf("g3 (non-partitioned shed) partition_label=%q want empty, never the whole sentinel", label)
	}
}

// TestShedCapacityCurrentPartitionRows proves ceo_ai.shed_capacity_current
// (migration 000110) adds distinct rows per partition of a shed WITHOUT
// changing the pre-existing bare-shed row: Castro has 5 animals total across
// two partitions (2 in "1", 3 in "2"); the bare row still reports 5 (whole-shed,
// unchanged from before this migration) and two new rows report 2 and 3.
func TestShedCapacityCurrentPartitionRows(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	cap10 := 10
	pk := park(t, ctx, pool, tenant, "Park Q")
	sh := shed(t, ctx, pool, tenant, pk, "Castro", &cap10)

	for i := 0; i < 2; i++ {
		g := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
		partition(t, ctx, pool, tenant, g, sh, "1", "Castro 1")
	}
	for i := 0; i < 3; i++ {
		g := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
		partition(t, ctx, pool, tenant, g, sh, "2", "Castro 2")
	}

	rows, err := pool.Query(ctx,
		`SELECT COALESCE(partition_label, ''), animals, capacity FROM ceo_ai.shed_capacity_current
		 WHERE tenant_id=$1 AND shed_label='Castro' ORDER BY COALESCE(partition_label, '')`, tenant)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	type row struct {
		label    string
		animals  int64
		capacity int64
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.label, &r.animals, &r.capacity); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 rows (bare + 2 partitions), got %d: %+v", len(got), got)
	}
	want := map[string]int64{"": 5, "1": 2, "2": 3}
	for _, r := range got {
		if r.animals != want[r.label] {
			t.Errorf("partition %q animals=%d want %d", r.label, r.animals, want[r.label])
		}
		// no per-partition capacity column exists anywhere in the schema, so
		// every row -- bare or partitioned -- carries the SAME shed-level cap.
		if r.capacity != 10 {
			t.Errorf("partition %q capacity=%d want 10 (shed-level, not per-partition)", r.label, r.capacity)
		}
	}
}

// TestCountsMovementDailyPartitionLabel proves the birth/death branches of
// ceo_ai.counts_movement_daily (migration 000110) attribute per-goat events to
// the correct partition: two births on the same day in different partitions of
// the same shed land as two distinct rows, not one merged shed-total row.
func TestCountsMovementDailyPartitionLabel(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park R")
	sh := shed(t, ctx, pool, tenant, pk, "Godel 1", nil)
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	g1 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", &day, nil)
	partition(t, ctx, pool, tenant, g1, sh, "Part 3", "Godel 1 - Part 3")
	g2 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", &day, nil)
	partition(t, ctx, pool, tenant, g2, sh, "Part 3", "Godel 1 - Part 3")
	g3 := goatWithID(t, ctx, pool, tenant, pk, sh, "goat", "alive", &day, nil)
	partition(t, ctx, pool, tenant, g3, sh, "Part 5", "Godel 1 - Part 5")

	got := map[string]int64{}
	rows, err := pool.Query(ctx,
		`SELECT COALESCE(partition_label, ''), births FROM ceo_ai.counts_movement_daily
		 WHERE tenant_id=$1 AND shed_label='Godel 1' AND event_date='2026-07-20'`, tenant)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var label string
		var b int64
		if err := rows.Scan(&label, &b); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[label] = b
	}
	if got["Part 3"] != 2 {
		t.Fatalf("Part 3 births=%d want 2", got["Part 3"])
	}
	if got["Part 5"] != 1 {
		t.Fatalf("Part 5 births=%d want 1", got["Part 5"])
	}
}
