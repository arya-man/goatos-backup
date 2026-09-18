// Package reporting holds adversarial grain/identity proofs for the ceo_ai.*
// governed reporting views defined in migration 000020. These are the
// projection-review tests the aggregate-projection guard requires: each proves
// a COUNT/GROUP-BY view keeps the correct grain under join fan-out, scope
// nesting, status bucketing, and business-day shifts. Postgres-gated (Docker).
package reporting

import (
	"context"
	"fmt"
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

// --- ceo_ai.mortality_base (migration 000358) -------------------------------
//
// The Ask Mesha operational-query rebuild widened mortality_base from a bare
// per-day death count to the dashboard's mortality tiles: kid/adult split,
// first-week deaths, and cause_established (recorded cause OR an inferred
// death-review case). That added two LEFT JOINs on the per-goat side, so the
// grain must be re-proved: one dead goat is one death, whatever it joins to.

// deadGoatWithID inserts a mortality exit on `exited` with the given management
// stage and date of birth, returning the goat_id so callers can attach
// health_death_causes / health_cases rows to it.
func deadGoatWithID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, parkID, shedID, stage string, dob time.Time, exited time.Time) string {
	t.Helper()
	party := custodian(t, ctx, pool, tenant)
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, management_stage, health_status,
		                    custodian_party_id, park_id, shed_id, origin_type, dob, exited_at, exit_reason, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, 'G-' || lpad((floor(random()*1000000000))::bigint::text, 9, '0'), 'goat', 'female', 'dead', $2, 'healthy',
		         $3, $4, $5, 'birth', $6, $7, 'died', 1, now(), now())
		 RETURNING goat_id::text`,
		tenant, stage, party, parkID, shedID, dob, exited).Scan(&id); err != nil {
		t.Fatalf("insert dead goat: %v", err)
	}
	return id
}

func deathCause(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, goatID, key string) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO health_death_causes (tenant_id, goat_id, cause_key, cause_kind) VALUES ($1, $2, $3, 'register_rule')`,
		tenant, goatID, key); err != nil {
		t.Fatalf("insert death cause: %v", err)
	}
}

// healthCase opens one health case for the goat in the given status, starting
// on start and (when closed is non-nil) closed at that instant.
func healthCase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant, goatID, status string, start time.Time, closed *time.Time) {
	t.Helper()
	var versionID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO health_protocol_versions (tenant_id, disease_key, display_name, age_band, version, duration_days, status, content_hash)
		 VALUES ($1, 'supportive', 'Supportive', 'adult', 1, 3, 'published', 'hash-' || gen_random_uuid()::text)
		 RETURNING health_protocol_version_id::text`, tenant).Scan(&versionID); err != nil {
		t.Fatalf("insert protocol version: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO health_cases (tenant_id, goat_id, health_protocol_version_id, disease_key, disease_name, age_band, start_date, duration_days, status, closed_at, idempotency_key, request_fingerprint)
		 VALUES ($1, $2, $3, 'supportive', 'Supportive', 'adult', $4, 3, $5, $6, gen_random_uuid()::text, 'fp')`,
		tenant, goatID, versionID, start, status, closed); err != nil {
		t.Fatalf("insert health case: %v", err)
	}
}

type mortalityRow struct {
	eventDate        string
	parkLabel        string
	deaths           int64
	kidDeaths        int64
	adultDeaths      int64
	firstWeekDeaths  int64
	causeEstablished int64
	activePopulation int64
}

func mortalityRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenant string) []mortalityRow {
	t.Helper()
	rows, err := pool.Query(ctx,
		`SELECT event_date::text, COALESCE(park_label, ''), deaths, kid_deaths, adult_deaths, first_week_deaths, cause_established, active_population
		   FROM ceo_ai.mortality_base WHERE tenant_id=$1 ORDER BY event_date, park_label`, tenant)
	if err != nil {
		t.Fatalf("query mortality_base: %v", err)
	}
	defer rows.Close()
	var out []mortalityRow
	for rows.Next() {
		var r mortalityRow
		if err := rows.Scan(&r.eventDate, &r.parkLabel, &r.deaths, &r.kidDeaths, &r.adultDeaths, &r.firstWeekDeaths, &r.causeEstablished, &r.activePopulation); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// TestMortalityBaseOneToManyCauseJoins proves the health_death_causes and
// health_cases joins do NOT fan out the death count: a goat with a recorded
// cause AND two death-review cases is still exactly one death and one
// cause_established, and a goat with three health cases but none in a death
// status contributes one death and zero cause_established.
func TestMortalityBaseOneToManyCauseJoins(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park M")
	sh := shed(t, ctx, pool, tenant, pk, "Shed M1", nil)
	dob := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	exit := time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC)

	// Recorded cause + two overlapping death-review cases: every join side is many.
	multi := deadGoatWithID(t, ctx, pool, tenant, pk, sh, "active_adult", dob, exit)
	deathCause(t, ctx, pool, tenant, multi, "PPR")
	healthCase(t, ctx, pool, tenant, multi, "closed_dead", exit.AddDate(0, 0, -3), &exit)
	healthCase(t, ctx, pool, tenant, multi, "held_death_review", exit.AddDate(0, 0, -1), nil)

	// Three cases, none a death status: must be a death with no cause.
	noisy := deadGoatWithID(t, ctx, pool, tenant, pk, sh, "active_adult", dob, exit)
	for _, status := range []string{"active", "recovered", "canceled"} {
		healthCase(t, ctx, pool, tenant, noisy, status, exit.AddDate(0, 0, -5), nil)
	}

	got := mortalityRows(t, ctx, pool, tenant)
	if len(got) != 1 {
		t.Fatalf("expected one (park, day) row, got %d: %+v", len(got), got)
	}
	if got[0].deaths != 2 {
		t.Fatalf("cause joins fanned out deaths: deaths=%d want 2", got[0].deaths)
	}
	if got[0].causeEstablished != 1 {
		t.Fatalf("cause_established=%d want 1 (recorded/inferred goat once, noisy goat never)", got[0].causeEstablished)
	}
	if got[0].adultDeaths != 2 || got[0].kidDeaths != 0 {
		t.Fatalf("adult/kid split wrong: adult=%d kid=%d want 2/0", got[0].adultDeaths, got[0].kidDeaths)
	}
}

// TestMortalityBaseDateShift proves event_date is the Asia/Kolkata business
// day of exited_at: a death at 2026-07-10 20:30 UTC (02:00 IST on the 11th)
// lands on 2026-07-11, and the first-week bucket is measured against that
// business day, not the UTC date.
func TestMortalityBaseDateShift(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park T")
	sh := shed(t, ctx, pool, tenant, pk, "Shed T1", nil)

	lateUTC := time.Date(2026, 7, 10, 20, 30, 0, 0, time.UTC) // 2026-07-11 02:00 IST
	earlyUTC := time.Date(2026, 7, 10, 6, 0, 0, 0, time.UTC)  // 2026-07-10 11:30 IST
	// Born 2026-07-04: 7 days before the IST business day 07-11 (in the first-week
	// bucket), but 6 days before the UTC date 07-10 -- either way in-bucket; the
	// date proof is which ROW it lands in.
	deadGoatWithID(t, ctx, pool, tenant, pk, sh, "kid_preweaning", time.Date(2026, 7, 4, 0, 0, 0, 0, time.UTC), lateUTC)
	// Born 2026-07-02: 8 days before 07-10 IST -- outside first week on the IST day.
	deadGoatWithID(t, ctx, pool, tenant, pk, sh, "kid_preweaning", time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), earlyUTC)

	byDay := map[string]mortalityRow{}
	for _, r := range mortalityRows(t, ctx, pool, tenant) {
		byDay[r.eventDate] = r
	}
	if len(byDay) != 2 {
		t.Fatalf("expected two business-day rows, got %+v", byDay)
	}
	if r := byDay["2026-07-11"]; r.deaths != 1 || r.firstWeekDeaths != 1 || r.kidDeaths != 1 {
		t.Fatalf("late-UTC death did not shift to IST 07-11 row: %+v", r)
	}
	if r := byDay["2026-07-10"]; r.deaths != 1 || r.firstWeekDeaths != 0 || r.kidDeaths != 1 {
		t.Fatalf("07-10 row wrong (first-week must be 0 at 8 days): %+v", r)
	}
	if _, leaked := byDay["2026-07-09"]; leaked {
		t.Fatalf("a UTC-date row leaked into the view")
	}
}

// TestMortalityBaseStatusBuckets proves the death predicate matches the
// Counts dashboard: exit_reason='died' counts; an unexited animal already at
// lifecycle_status='dead' counts (event day falls back to updated_at); sold,
// culled, transferred and lost exits and live animals never appear; and
// active_population excludes every exited/terminal status while the per-day
// death rows share the same whole-park denominator.
func TestMortalityBaseStatusBuckets(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	pk := park(t, ctx, pool, tenant, "Park S")
	sh := shed(t, ctx, pool, tenant, pk, "Shed S1", nil)
	exit := time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC)

	// Two live animals: the denominator.
	goat(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	goat(t, ctx, pool, tenant, pk, sh, "goat", "alive", nil, nil)
	// Every non-death exit status: none is a death, none is population.
	for _, exitStatus := range [][2]string{{"sold", "sold"}, {"culled", "culled"}, {"transferred", "transferred"}, {"lost", "lost"}} {
		party := custodian(t, ctx, pool, tenant)
		if _, err := pool.Exec(ctx,
			`INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, management_stage, health_status,
			                    custodian_party_id, park_id, shed_id, origin_type, exited_at, exit_reason, row_version, created_at, updated_at)
			 VALUES (gen_random_uuid(), $1, 'G-' || lpad((floor(random()*1000000000))::bigint::text, 9, '0'), 'goat', 'female', $2, 'active_adult', 'healthy',
			         $3, $4, $5, 'procured', $6, $7, 1, now(), now())`,
			tenant, exitStatus[0], party, pk, sh, exit, exitStatus[1]); err != nil {
			t.Fatalf("insert %s goat: %v", exitStatus[1], err)
		}
	}
	// One proper death exit.
	deadGoatWithID(t, ctx, pool, tenant, pk, sh, "active_adult", time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), exit)
	// One animal marked dead without an exit row (legacy Counts predicate): counts, dated by updated_at.
	party := custodian(t, ctx, pool, tenant)
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (goat_id, tenant_id, display_id, species, sex, lifecycle_status, management_stage, health_status,
		                    custodian_party_id, park_id, shed_id, origin_type, row_version, created_at, updated_at)
		 VALUES (gen_random_uuid(), $1, 'G-' || lpad((floor(random()*1000000000))::bigint::text, 9, '0'), 'goat', 'female', 'dead', 'active_adult', 'healthy',
		         $2, $3, $4, 'procured', 1, $5, $5)`,
		tenant, party, pk, sh, exit); err != nil {
		t.Fatalf("insert unexited dead goat: %v", err)
	}

	got := mortalityRows(t, ctx, pool, tenant)
	if len(got) != 1 {
		t.Fatalf("expected one (park, day) row, got %d: %+v", len(got), got)
	}
	if got[0].deaths != 2 {
		t.Fatalf("status predicate wrong: deaths=%d want 2 (died exit + unexited dead), sold/culled/transferred/lost must not count", got[0].deaths)
	}
	if got[0].activePopulation != 2 {
		t.Fatalf("active_population=%d want 2 (only live animals)", got[0].activePopulation)
	}
}

// TestMortalityBasePageBoundary proves the natural-SQL consumer's bounded
// `GROUP BY park_label ... LIMIT 50` reads the FULL per-day aggregate before
// paging: 60 parks each with one death yield 60 view rows, the top-50 page is
// an exact prefix of the whole-result ordering, and summing across every row
// (not the page) reproduces the total.
func TestMortalityBasePageBoundary(t *testing.T) {
	ctx := context.Background()
	pool, tenant := newDB(t, ctx)
	dob := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	exit := time.Date(2026, 7, 11, 6, 0, 0, 0, time.UTC)
	const parks = 60
	for i := 0; i < parks; i++ {
		pk := park(t, ctx, pool, tenant, fmt.Sprintf("Park %03d", i))
		sh := shed(t, ctx, pool, tenant, pk, "Shed", nil)
		deadGoatWithID(t, ctx, pool, tenant, pk, sh, "active_adult", dob, exit)
		if i%2 == 0 { // every second park has a second death on the same day
			deadGoatWithID(t, ctx, pool, tenant, pk, sh, "active_adult", dob, exit)
		}
	}
	all := mortalityRows(t, ctx, pool, tenant)
	if len(all) != parks {
		t.Fatalf("expected %d park rows, got %d", parks, len(all))
	}
	var total int64
	for _, r := range all {
		total += r.deaths
	}
	if total != parks+parks/2 {
		t.Fatalf("whole-result deaths=%d want %d", total, parks+parks/2)
	}

	// The consumer shape from natural_sql.go: grouped, ordered, bounded.
	rows, err := pool.Query(ctx,
		`SELECT park_label, sum(deaths) FROM ceo_ai.mortality_base WHERE tenant_id=$1 GROUP BY park_label ORDER BY sum(deaths) DESC, park_label LIMIT 50`, tenant)
	if err != nil {
		t.Fatalf("query page: %v", err)
	}
	defer rows.Close()
	var page int
	var pageTotal int64
	for rows.Next() {
		var label string
		var deaths int64
		if err := rows.Scan(&label, &deaths); err != nil {
			t.Fatalf("scan: %v", err)
		}
		page++
		pageTotal += deaths
		if page <= parks/2 && deaths != 2 {
			t.Fatalf("page row %d (%s) deaths=%d: two-death parks must fill the head of the page", page, label, deaths)
		}
	}
	if page != 50 {
		t.Fatalf("page rows=%d want 50", page)
	}
	if pageTotal >= total {
		t.Fatalf("a bounded page must not equal the whole-result total: page=%d total=%d", pageTotal, total)
	}
}
