package postgres

import (
	"context"
	"os"
	"testing"

	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestLitterShiftQueryPlanUsesIndexesAtScale is the at-scale plan proof for the kid stage shift
// reads (docs/decisions/kid-stage-shift-tasks.md). The farm is loaded to the envelope's upper bound
// -- 500k goats, 250k litters of twins, 500k animal identifiers and 500k stage-change events -- then
// ANALYZEd, and each read is EXPLAIN ANALYZEd with the planner left free (no enable_seqscan=off):
//
//   - litterKidsSQL and litterKidViewsSQL are ONE litter's reads: goat_births by
//     goat_births_event_child_unique, goats by primary key, the stage LATERAL by
//     goat_identity_events_goat_timeline_idx and the tag LATERAL by the goat's identifier index.
//     No large table may be sequentially scanned.
//   - littersOwingShiftSQL is the one-shot backfill's keyset page: it walks goat_births in
//     birth_event_id order and probes goats by primary key, so a page never scans the herd.
func TestLitterShiftQueryPlanUsesIndexesAtScale(t *testing.T) {
	if os.Getenv("GOATOS_SCALE_CERT") == "" {
		t.Skip("scale certification gate — set GOATOS_SCALE_CERT=1 (make scale-cert)")
	}
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const tenant = "00000000-0000-4000-8000-00000000a5a1"
	const custodian = "00000000-0000-4000-8000-00000000a5a2"
	// The bulk load runs on ONE connection of this throwaway test database with the user triggers of
	// the seeded tables off: each goat row would otherwise fire the manual-change audit (a full row
	// copy into audit.db_changes) and the herd-register projection, 500k times each, which is what
	// made the first attempt run past ten minutes. Neither is read by the queries under proof, and
	// the triggers are back on before any EXPLAIN.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seeded := []string{"goats", "goat_births", "goat_identifiers", "goat_identity_events"}
	for _, table := range seeded {
		if _, err := conn.Exec(ctx, `ALTER TABLE `+table+` DISABLE TRIGGER USER`); err != nil {
			t.Fatalf("disable %s triggers for the bulk load: %v", table, err)
		}
	}
	for _, stmt := range []string{
		`INSERT INTO tenants (tenant_id, name, status) VALUES ('` + tenant + `'::uuid, 'Litter Scale Tenant', 'active') ON CONFLICT DO NOTHING`,
		`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ('` + custodian + `'::uuid, 'org', 'Litter Scale Custodian', 'active') ON CONFLICT DO NOTHING`,
		// 500k goats: goat 1..500000; odd/even pairs are twins of the litter g/2.
		`INSERT INTO goats (goat_id, tenant_id, species, sex, breed, lifecycle_status, custodian_party_id, origin_type, dob, entry_date, management_stage)
SELECT ('40000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, '` + tenant + `'::uuid, 'goat',
       CASE WHEN g % 2 = 0 THEN 'female' ELSE 'male' END, 'Boer', 'alive', '` + custodian + `'::uuid, 'birth',
       DATE '2026-06-01', DATE '2026-06-01', (ARRAY['K0','K1','K2','K3','F2-Female'])[1 + g % 5]
FROM generate_series(1, 500000) g`,
		// 250k litters of twins, mothered by goat 1 (the mother's identity is not what these reads key on).
		`INSERT INTO goat_births (tenant_id, child_goat_id, mother_goat_id, litter_size, birth_event_id, child_ordinal, count_status, count_approved_at)
SELECT '` + tenant + `'::uuid, ('40000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
       '40000000-0000-4000-8000-000000000001'::uuid, 2,
       ('50000000-0000-4000-8000-' || lpad(((g + 1) / 2)::text, 12, '0'))::uuid, 2 - g % 2, 'approved', now()
FROM generate_series(2, 500000) g`,
		`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
SELECT '` + tenant + `'::uuid, ('40000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, 'animal_identifier_1',
       'rfid-' || g, 'RFID-' || g, 'global', true, 'active', now(), 'test'
FROM generate_series(1, 500000) g`,
		`INSERT INTO goat_identity_events (identity_event_id, tenant_id, goat_id, event_type, event_version, occurred_at, recorded_at, payload, idempotency_key)
SELECT gen_random_uuid(), '` + tenant + `'::uuid, ('40000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
       'goat.stage_changed', 1, now() - (g || ' seconds')::interval, now(),
       jsonb_build_object('management_stage', (ARRAY['K0','K1','K2','K3','F2-Female'])[1 + g % 5]), 'litter-scale-' || g
FROM generate_series(1, 500000) g`,
		`ANALYZE goats`, `ANALYZE goat_births`, `ANALYZE goat_identifiers`, `ANALYZE goat_identity_events`,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed at scale: %v\n%s", err, stmt)
		}
	}
	for _, table := range seeded {
		if _, err := conn.Exec(ctx, `ALTER TABLE `+table+` ENABLE TRIGGER USER`); err != nil {
			t.Fatalf("re-enable %s triggers: %v", table, err)
		}
	}
	conn.Release()

	litter := "50000000-0000-4000-8000-000000123456"
	large := []string{"goats", "goat_births", "goat_identifiers", "goat_identity_events"}

	pgtest.ExplainAnalyzeAtScale(t, ctx, pool, litterKidsSQL, tenant, litter).
		AssertNoSeqScan(t, "litterKidsSQL @500k goats", 200, large...)
	pgtest.ExplainAnalyzeAtScale(t, ctx, pool, litterKidViewsSQL, tenant, litter).
		AssertNoSeqScan(t, "litterKidViewsSQL @500k goats", 200, large...)
	pgtest.ExplainAnalyzeAtScale(t, ctx, pool, littersOwingShiftSQL, tenant, []string{"K0", "K1"}, "", 200,
		countsdomain.GrowthStagesBefore("Non-Pregnant")).
		AssertNoSeqScan(t, "littersOwingShiftSQL page @250k litters", 2000, "goats")
}
