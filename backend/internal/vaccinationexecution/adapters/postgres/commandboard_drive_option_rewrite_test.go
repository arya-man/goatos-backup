package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// The drive-option catalogue was rewritten on 2026-08-12 for speed alone: every many-side is now
// pre-aggregated to the (batch, park) grain in its own CTE and joined 1:1, instead of one wide join
// de-duplicated afterwards with COUNT(DISTINCT) / jsonb_agg(DISTINCT) and a LATERAL that ran once
// per fanned row. On the STG clone that was 528ms of a ~900ms request to produce 52 options.
//
// A performance rewrite that changes an answer is not a performance rewrite, so the ORIGINAL query
// is kept here as an executable oracle. This test runs both against the same rows and requires the
// projections to agree exactly. If a future change to the fast query alters a count, a dose code, a
// shed list, a partition label, an operator-day rollup, or the row order, this goes red with the
// legacy answer next to the new one.
//
// The fixture is deliberately the shape that broke the old query's cost model and the shape most
// likely to break a grain rewrite: ONE batch spanning TWO parks, a shed carrying TWO partitions,
// several animals per shed (the per-goat partition join is the fan-out multiplier), and completions
// on two different business days so operator_days has more than one entry to order.
const legacyDriveOptionsSQL = `
SELECT
  b.batch_id,
  COALESCE(park.location_id::text, '') AS park_id,
  COALESCE(park.name, '') AS park_name,
  b.status,
  b.planned_date,
  b.window_start,
  b.window_end,
  array_agg(DISTINCT pr.dose_code) AS dose_codes,
  COUNT(DISTINCT oi.target_id)::int AS target_count,
  COUNT(DISTINCT oi.obligation_id)::int AS dose_count,
  COALESCE(operator_days.days, '[]'::jsonb) AS operator_days,
  COALESCE(array_agg(DISTINCT loc.name) FILTER (WHERE loc.name IS NOT NULL), ARRAY[]::text[]) AS shed_names,
  COALESCE(
    jsonb_agg(DISTINCT jsonb_build_object(
      'shedId', loc.location_id::text,
      'shedName', COALESCE(NULLIF(loc.name, ''), loc.location_code, ''),
      'partition_label', CASE
        WHEN lower(btrim(COALESCE(gsp.partition_label, 'whole'))) IN ('', 'whole') THEN ''
        ELSE btrim(gsp.partition_label)
      END
    )) FILTER (WHERE loc.location_id IS NOT NULL),
    '[]'::jsonb
  ) AS shed_locations
FROM obligation_batches b
JOIN obligation_instances oi ON oi.batch_id = b.batch_id AND oi.tenant_id = b.tenant_id
JOIN protocol_rules pr ON oi.rule_id = pr.rule_id AND oi.tenant_id = pr.tenant_id
LEFT JOIN locations loc ON oi.scope_id = loc.location_id AND oi.tenant_id = loc.tenant_id
LEFT JOIN goat_shed_partitions gsp ON gsp.tenant_id = oi.tenant_id AND gsp.goat_id = oi.target_id AND gsp.shed_id = oi.scope_id
LEFT JOIN locations park ON park.location_id = loc.parent_location_id AND park.tenant_id = loc.tenant_id
LEFT JOIN LATERAL (
  SELECT jsonb_agg(
    jsonb_build_object(
      'date', to_char(day_row.day, 'YYYY-MM-DD'),
      'targetCount', day_row.target_count,
      'doseCount', day_row.dose_count
    )
    ORDER BY day_row.day
  ) AS days
  FROM (
    SELECT
      (vc.administered_at AT TIME ZONE 'Asia/Kolkata')::date AS day,
      COUNT(DISTINCT vc.goat_id)::int AS target_count,
      COUNT(DISTINCT vc.obligation_id)::int AS dose_count
    FROM vaccination_completions vc
    JOIN obligation_instances day_oi ON day_oi.obligation_id = vc.obligation_id AND day_oi.tenant_id = vc.tenant_id
    LEFT JOIN locations day_loc ON day_oi.scope_id = day_loc.location_id AND day_oi.tenant_id = day_loc.tenant_id
    WHERE vc.tenant_id = b.tenant_id
      AND vc.batch_id = b.batch_id
      AND (park.location_id IS NULL OR day_loc.parent_location_id = park.location_id)
    GROUP BY 1
  ) day_row
) operator_days ON true
WHERE b.tenant_id = $1::uuid
  AND (COALESCE($2::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR loc.parent_location_id = $2::uuid)
GROUP BY b.batch_id, park.location_id, park.name, b.status, b.planned_date, b.window_start, b.window_end, operator_days.days
ORDER BY
  CASE b.status
    WHEN 'in_progress' THEN 0
    WHEN 'completed' THEN 1
    ELSE 2
  END,
  b.planned_date DESC NULLS LAST,
  b.window_start DESC NULLS LAST,
  b.batch_id,
  park.name NULLS LAST
LIMIT $3
`

// legacyDriveOptionRow is the raw projection, compared column for column rather than through the
// domain type: a field the scanner drops would otherwise make two different queries look equal.
type legacyDriveOptionRow struct {
	BatchID       string
	ParkID        string
	ParkName      string
	Status        string
	DoseCodes     []string
	TargetCount   int
	DoseCount     int
	OperatorDays  string
	ShedNames     []string
	ShedLocations string
}

// scanDriveOptionProjection runs a drive-options query and reads its raw columns. Compared column
// for column rather than through the domain type, so a field the scanner drops cannot make two
// different queries look equal.
func scanDriveOptionProjection(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql, tenantID string, limit int) []legacyDriveOptionRow {
	t.Helper()
	rows, err := pool.Query(ctx, sql, tenantID, nil, limit)
	if err != nil {
		t.Fatalf("legacy drive options query: %v", err)
	}
	defer rows.Close()

	out := []legacyDriveOptionRow{}
	for rows.Next() {
		var row legacyDriveOptionRow
		var plannedDate pgtype.Date
		var windowStart, windowEnd pgtype.Timestamptz
		var operatorDays, shedLocations []byte
		if err := rows.Scan(
			&row.BatchID, &row.ParkID, &row.ParkName, &row.Status,
			&plannedDate, &windowStart, &windowEnd,
			&row.DoseCodes, &row.TargetCount, &row.DoseCount,
			&operatorDays, &row.ShedNames, &shedLocations,
		); err != nil {
			t.Fatalf("scan legacy drive option: %v", err)
		}
		row.OperatorDays = string(operatorDays)
		row.ShedLocations = string(shedLocations)
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("legacy drive option rows: %v", err)
	}
	return out
}

func TestDriveOptionsRewriteMatchesLegacyShape(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000e1"
	parkCBE := "70000000-0000-4000-8000-0000010000e1"
	parkCPT := "70000000-0000-4000-8000-0000010000e2"
	shedCBE := "70000000-0000-4000-8000-0000020000e1"
	shedCPT := "70000000-0000-4000-8000-0000020000e2"
	custodianPartyID := "70000000-0000-4000-8000-00000a0000e1"
	protocolID := "70000000-0000-4000-8000-0000060000e0"
	protocolVersionID := "70000000-0000-4000-8000-0000060000e1"
	ruleOne := "70000000-0000-4000-8000-0000070000e1"
	ruleTwo := "70000000-0000-4000-8000-0000070000e2"
	batchID := "70000000-0000-4000-8000-0000040000e1"

	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Rewrite Org', 'active')`, tenantID)
	execProjectionSQL(t, ctx, pool, "park CBE",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'CBE', 'park', NULL, 'active')`, parkCBE, tenantID)
	execProjectionSQL(t, ctx, pool, "park CPT",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'CPT', 'park', NULL, 'active')`, parkCPT, tenantID)
	// Same shed NAME in both parks, because that is the farm's real shape and it is exactly what a
	// grouping mistake collapses.
	execProjectionSQL(t, ctx, pool, "shed CBE",
		`INSERT INTO locations (location_id, tenant_id, name, location_code, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Castro', 'castro-cbe', 'shed', $3, 'active')`, shedCBE, tenantID, parkCBE)
	execProjectionSQL(t, ctx, pool, "shed CPT",
		`INSERT INTO locations (location_id, tenant_id, name, location_code, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Castro', 'castro-cpt', 'shed', $3, 'active')`, shedCPT, tenantID, parkCPT)
	execProjectionSQL(t, ctx, pool, "custodian party",
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1, 'org', 'Custodian E1', 'active')`,
		custodianPartyID)

	// Several animals per shed: the per-goat partition join is what multiplied the old query, so a
	// single animal per shed would hide a fan-out regression entirely.
	type goatFixture struct{ id, shed, partition string }
	goats := []goatFixture{
		{"70000000-0000-4000-8000-0000030000e1", shedCBE, "1"},
		{"70000000-0000-4000-8000-0000030000e2", shedCBE, "2"},
		{"70000000-0000-4000-8000-0000030000e3", shedCBE, "whole"},
		{"70000000-0000-4000-8000-0000030000e4", shedCPT, "Part 3"},
		// No partition row at all: the real "undivided" case, which exercises the LEFT JOIN's NULL
		// branch through COALESCE(gsp.partition_label, 'whole').
		{"70000000-0000-4000-8000-0000030000e5", shedCPT, ""},
	}
	for _, g := range goats {
		execProjectionSQL(t, ctx, pool, "goat",
			`INSERT INTO goats (goat_id, tenant_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
			 VALUES ($1, $2, 'female', 'alive', 'Non-Pregnant', $3, $4, '2024-01-01')`,
			g.id, tenantID, g.shed, custodianPartyID)
		if g.partition == "" {
			continue
		}
		execProjectionSQL(t, ctx, pool, "goat shed partition",
			`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
			 VALUES ($1, $2, $3, $4, 'Castro')`, tenantID, g.id, g.shed, g.partition)
	}

	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_e1', 'Vaccination E1', 'vaccination', 'active')`, protocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`, protocolVersionID, tenantID, protocolID)
	// TWO dose codes, so array_agg(DISTINCT dose_code) has something to order and de-duplicate.
	execProjectionSQL(t, ctx, pool, "rule one",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'et_tt_adult_w1', 'birth_age')`, ruleOne, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "rule two",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1, $2, $3, 'ppr_booster', 'birth_age')`, ruleTwo, tenantID, protocolVersionID)
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)

	asOf := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	execProjectionSQL(t, ctx, pool, "batch",
		`INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, session, planned_date, status, context)
		 VALUES ($1, $2, $3, 'tenant', $2, 'drive:e1', DATE '2026-07-25', 'planned', '{}'::jsonb)`,
		batchID, tenantID, protocolVersionID)

	obligations := []struct{ id, goat, shed, rule, key string }{
		{"70000000-0000-4000-8000-0000080000e1", goats[0].id, shedCBE, ruleOne, "rewrite-cbe-1"},
		{"70000000-0000-4000-8000-0000080000e2", goats[1].id, shedCBE, ruleTwo, "rewrite-cbe-2"},
		{"70000000-0000-4000-8000-0000080000e3", goats[2].id, shedCBE, ruleOne, "rewrite-cbe-3"},
		{"70000000-0000-4000-8000-0000080000e4", goats[3].id, shedCPT, ruleOne, "rewrite-cpt-1"},
		{"70000000-0000-4000-8000-0000080000e5", goats[4].id, shedCPT, ruleTwo, "rewrite-cpt-2"},
		// A SECOND dose for an animal that already has one, in the same batch and park. Without it
		// every animal holds exactly one obligation, distinct and non-distinct counting agree, and
		// the fixture cannot tell "animals in this drive" from "doses in this drive" -- the exact
		// confusion COUNT(DISTINCT target_id) exists to prevent.
		{"70000000-0000-4000-8000-0000080000e6", goats[0].id, shedCBE, ruleTwo, "rewrite-cbe-4"},
	}
	for _, o := range obligations {
		execProjectionSQL(t, ctx, pool, "obligation",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, batch_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
			 VALUES ($1, $2, $3, $4, $5, 'goat', 'shed', $6, $7, 'scheduled', $8::timestamptz, $9)`,
			o.id, tenantID, batchID, protocolVersionID, o.goat, o.shed, o.rule, asOf, o.key)
	}

	// Completions on TWO business days in ONE park, so operator_days is a multi-entry ordered array
	// and the park predicate inside it actually discriminates.
	completions := []struct {
		obligation, goat, administered string
	}{
		{obligations[0].id, goats[0].id, "2026-07-25T04:30:00Z"},
		{obligations[1].id, goats[1].id, "2026-07-25T05:30:00Z"},
		{obligations[2].id, goats[2].id, "2026-07-26T04:30:00Z"},
		{obligations[3].id, goats[3].id, "2026-07-26T05:30:00Z"},
	}
	for i, c := range completions {
		execProjectionSQL(t, ctx, pool, "completion",
			`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, batch_id, goat_id, status, administered_at, idempotency_key)
			 VALUES (gen_random_uuid(), $1, $2, $3, $4, 'accepted', $5::timestamptz, $6)`,
			tenantID, c.obligation, batchID, c.goat, c.administered, "rewrite-completion-"+string(rune('a'+i)))
	}

	// Both queries, same rows, same bound. Compared as RAW PROJECTIONS rather than through the
	// domain type: the scanner drops columns the type has no field for (dose codes become the
	// drive's name, the JSON blobs get parsed), so comparing after scanning could call two
	// genuinely different queries equal.
	const limit = 201
	legacy := scanDriveOptionProjection(t, ctx, pool, legacyDriveOptionsSQL, tenantID, limit)
	rewritten := scanDriveOptionProjection(t, ctx, pool, driveOptionsSQL, tenantID, limit)
	if len(legacy) == 0 {
		t.Fatalf("legacy query returned no drive options; the fixture proves nothing")
	}
	if len(rewritten) != len(legacy) {
		t.Fatalf("rewritten query returned %d option(s), legacy %d", len(rewritten), len(legacy))
	}
	for i := range legacy {
		want, got := legacy[i], rewritten[i]
		if got.BatchID != want.BatchID || got.ParkID != want.ParkID || got.ParkName != want.ParkName || got.Status != want.Status {
			t.Fatalf("row %d identity/order differs:\n legacy    = %+v\n rewritten = %+v", i, want, got)
		}
		if got.TargetCount != want.TargetCount || got.DoseCount != want.DoseCount {
			t.Fatalf("row %d counts differ: legacy (%d targets, %d doses), rewritten (%d, %d)",
				i, want.TargetCount, want.DoseCount, got.TargetCount, got.DoseCount)
		}
		if !equalStrings(got.DoseCodes, want.DoseCodes) {
			t.Fatalf("row %d dose codes differ: legacy %v, rewritten %v", i, want.DoseCodes, got.DoseCodes)
		}
		if !equalStrings(got.ShedNames, want.ShedNames) {
			t.Fatalf("row %d shed names differ: legacy %v, rewritten %v", i, want.ShedNames, got.ShedNames)
		}
		if got.OperatorDays != want.OperatorDays {
			t.Fatalf("row %d operator days differ:\n legacy    = %s\n rewritten = %s", i, want.OperatorDays, got.OperatorDays)
		}
		if got.ShedLocations != want.ShedLocations {
			t.Fatalf("row %d shed locations differ:\n legacy    = %s\n rewritten = %s", i, want.ShedLocations, got.ShedLocations)
		}
	}

	// The served response must also be non-empty and ordered the same way, so the comparison above
	// is about the query the board actually calls rather than an unused string constant.
	repo := NewRepository(pool, 20*time.Second)
	resp, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}
	if len(resp.DriveOptions) != len(legacy) {
		t.Fatalf("served drive options = %d, legacy query = %d", len(resp.DriveOptions), len(legacy))
	}
	for i, want := range legacy {
		got := resp.DriveOptions[i]
		if got.DriveBatchID != want.BatchID || got.ParkID != want.ParkID || got.TargetCount != want.TargetCount {
			t.Fatalf("served option[%d] = (%s,%s,%d), legacy = (%s,%s,%d)",
				i, got.DriveBatchID, got.ParkID, got.TargetCount, want.BatchID, want.ParkID, want.TargetCount)
		}
	}

	// drive_park_id must narrow the SECTIONS without shrinking the PICKER. That asymmetry is the
	// whole reason one request can now do what two did: a catalogue scoped to the selected drive's
	// park would delete every other park's drive from the dropdown.
	selected := legacy[0]
	narrowed, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{
		TenantID:     tenantID,
		AsOf:         asOf,
		DriveBatchID: &selected.BatchID,
		DriveParkID:  &selected.ParkID,
	})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard(DriveParkID) error = %v", err)
	}
	if len(narrowed.DriveOptions) != len(legacy) {
		t.Fatalf("narrowing by drive park shrank the picker to %d option(s), want all %d — the other park's drive must stay selectable",
			len(narrowed.DriveOptions), len(legacy))
	}
	// ...while the board itself is that park's only. The fixture puts a different animal count in
	// each park, so a section that ignored drive_park_id would report the wrong total here.
	otherPark := legacy[1]
	if selected.ParkID == otherPark.ParkID {
		t.Fatalf("fixture did not produce two distinct parks; the narrowing assertion is vacuous")
	}
	if narrowed.KPIs.Targets != selected.TargetCount {
		t.Fatalf("board narrowed to park %s reports %d targets, want that park's %d",
			selected.ParkID, narrowed.KPIs.Targets, selected.TargetCount)
	}

	// And the fixture must actually have exercised the multi-park, multi-day, multi-partition
	// shape -- a rewrite proof over a degenerate single-row fixture proves nothing.
	if len(legacy) < 2 {
		t.Fatalf("fixture produced %d option(s); the two-park shape did not materialise", len(legacy))
	}
	sawDays := false
	for _, row := range legacy {
		if row.OperatorDays != "[]" && row.OperatorDays != "" {
			sawDays = true
		}
	}
	if !sawDays {
		t.Fatalf("no operator-day rollup in the fixture; the LATERAL replacement is untested")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
