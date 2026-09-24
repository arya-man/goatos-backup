package postgres

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"testing"

	herdapp "github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// F5 equivalence proof: the persisted classification written by RecomputeRisk must equal the
// retired per-request in-memory classification (whole-cohort enrichment + applyRiskSignals with
// cohort pen medians), which still backs the CSV export's risk filter, tag for tag and state
// for state, over a varied herd: mapped/unmapped, several pens, own-baseline windows inside and
// before the monitoring boundary, reconnect gaps, stale tags, sensor faults, temperature
// outliers, and patterns.
func TestPersistedRiskMatchesInMemoryClassification(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	const n, pens = 1500, 12
	stmts := []string{
		fmt.Sprintf(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 SELECT ('46000000-0000-4000-8000-' || lpad(p::text, 12, '0'))::uuid, '%s', 'shed', 'EQ' || p, 'Eq Pen ' || p, '%s', 'active'
		 FROM generate_series(1, %d) p`, hsiTenant, hsiPark, pens),
		fmt.Sprintf(`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex)
		 SELECT ('47000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, '%s', 'alive', 'goat', '%s',
		        ('46000000-0000-4000-8000-' || lpad((g %% %d + 1)::text, 12, '0'))::uuid, '%s',
		        ('46000000-0000-4000-8000-' || lpad((g %% %d + 1)::text, 12, '0'))::uuid, 'Boer', 'female'
		 FROM generate_series(0, %d) g`, hsiTenant, hsiParty, pens, hsiPark, pens, n-1),
		// every 10th tag stays unmapped (no identifier)
		fmt.Sprintf(`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable)
		 SELECT '%s', ('47000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid, 'animal_identifier_1', 'EQ' || lpad(g::text, 6, '0'), 'EQ' || lpad(g::text, 6, '0'), 'global', true, 'active', now(), 'test_v1', true
		 FROM generate_series(0, %d) g WHERE g %% 10 <> 0`, hsiTenant, n-1),
		fmt.Sprintf(`INSERT INTO herd_signal_tag_latest (tenant_id, tag_id, gateway_id, last_seen_at, signal_state, battery_state, tag_temperature_c,
		   motion_count, motion_delta, motion_window_seconds, movement_state, pattern_state, mapping_state, gap_delta,
		   temperature_sensor_ok, accelerometer_sensor_ok, animal_monitoring_since)
		 SELECT '%s', 'EQ' || lpad(g::text, 6, '0'), 'gw-eq',
		        CASE WHEN g %% 23 = 0 THEN now() - interval '2 hours' ELSE now() - (g %% 90) * interval '1 second' END,
		        'strong', 'healthy', 37.0 + ((g * 7) %% 11) * 0.25,
		        1000 + g, CASE WHEN g %% 19 = 0 THEN NULL ELSE (g * 13) %% 120 END,
		        CASE WHEN g %% 5 = 0 THEN 600 WHEN g %% 7 = 0 THEN 0 ELSE 900 END,
		        (ARRAY['moving','quiet','not_moving'])[g %% 3 + 1],
		        (ARRAY['normal','inactive','quiet_watch','spike','normal','no_movement'])[g %% 6 + 1],
		        CASE WHEN g %% 10 = 0 THEN 'unmapped' ELSE 'mapped' END,
		        g %% 13 = 0,
		        CASE WHEN g %% 17 = 0 THEN false ELSE true END, CASE WHEN g %% 29 = 0 THEN false END,
		        CASE WHEN g %% 10 = 0 THEN NULL ELSE now() - interval '20 hours' END
		 FROM generate_series(0, %d) g`, hsiTenant, n-1),
		// 24h of 300s-tier windows; the oldest four hours fall before the monitoring boundary.
		fmt.Sprintf(`INSERT INTO herd_signal_activity_windows (tenant_id, tag_id, bucket_start, bucket_seconds, motion_delta, packet_count, gap_delta, first_seen_at, last_seen_at)
		 SELECT '%s', 'EQ' || lpad(g::text, 6, '0'), date_trunc('hour', now()) - h * interval '1 hour', 300,
		        ((g * 3 + h * 5) %% 60), CASE WHEN (g + h) %% 31 = 0 THEN 0 ELSE 6 END, (g + h) %% 37 = 0,
		        date_trunc('hour', now()) - h * interval '1 hour', date_trunc('hour', now()) - h * interval '1 hour' + interval '4 minutes'
		 FROM generate_series(0, %d) g, generate_series(0, 23) h`, hsiTenant, n-1),
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}

	svc := herdapp.NewService(repo)
	if _, err := svc.RecomputeRisk(ctx, hsiTenant); err != nil {
		t.Fatalf("RecomputeRisk: %v", err)
	}
	persisted := map[string]string{}
	cursor := ""
	for {
		tags, next, err := repo.ListTagsLatestKeyset(ctx, hsiTenant, nil, nil, nil, nil, nil, nil, nil, nil, cursor, 500, domain.LiveSort{Key: "smart_tag", Dir: "asc"})
		if err != nil {
			t.Fatal(err)
		}
		for _, tag := range tags {
			if tag.RiskEvaluatedAt == nil {
				t.Fatalf("tag %s never evaluated", tag.TagID)
			}
			if tag.RiskState != nil {
				persisted[tag.TagID] = *tag.RiskState
			}
		}
		if next == nil {
			break
		}
		cursor = *next
	}

	// The old in-memory classification, per state, via the export's retained cohort path.
	actor := domain.Actor{TenantID: hsiTenant, UserID: "10000000-0000-4000-8000-000000000001"}
	inMemory := map[string]string{}
	for _, state := range []string{"low", "watch", "high"} {
		var buf bytes.Buffer
		st := state
		if err := svc.ExportCSV(ctx, actor, nil, nil, nil, nil, nil, nil, &st, nil, domain.LiveSort{}, &buf); err != nil {
			t.Fatalf("export %s: %v", state, err)
		}
		recs, err := csv.NewReader(&buf).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		for _, rec := range recs[1:] {
			inMemory[rec[1]] = state
		}
	}
	counts := map[string]int{}
	for tag, st := range inMemory {
		counts[st]++
		if persisted[tag] != st {
			t.Errorf("tag %s: in-memory %q, persisted %q", tag, st, persisted[tag])
		}
	}
	for tag, st := range persisted {
		if inMemory[tag] != st {
			t.Errorf("tag %s: persisted %q, in-memory %q", tag, st, inMemory[tag])
		}
	}
	if counts["low"] == 0 || counts["watch"] == 0 || counts["high"] == 0 {
		t.Fatalf("seed does not exercise every state: %v", counts)
	}
	t.Logf("equivalent over %d tags: %v (attention %d)", n, counts, len(persisted))

	// Idempotent: a second pass over an unchanged herd writes nothing.
	if changed, err := svc.RecomputeRisk(ctx, hsiTenant); err != nil || changed != 0 {
		t.Fatalf("second pass changed %d rows (err %v), want 0", changed, err)
	}
}
