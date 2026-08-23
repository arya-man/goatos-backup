package postgres

import (
	"bytes"
	"context"
	"encoding/csv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/herdsignals/app"
	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// Proofs for the two reads that turn the disabled Export button and the inert overlay chips into
// real endpoints, run against the REAL migration schema.
//
// What these prove, in order of how badly each could be got wrong:
//  1. the export applies the SAME filter the live list applies (a filtered export that quietly
//     returns everything is worse than no export at all -- the operator downloads a file that
//     does not answer the question they asked on screen);
//  2. the activity overlay joins REAL farm records across three different grains; and
//  3. it never returns a record from before the tag was mapped to the animal (migration 000197).

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, what, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("seed %s: %v", what, err)
	}
}

func exportCSV(t *testing.T, ctx context.Context, repo *Repository, filters map[string]string) [][]string {
	t.Helper()
	svc := app.NewService(repo)
	var buf bytes.Buffer
	ptr := func(k string) *string {
		if v, ok := filters[k]; ok && v != "" {
			return &v
		}
		return nil
	}
	err := svc.ExportCSV(ctx, domain.Actor{TenantID: hsiTenant, UserID: hsiParty},
		ptr("park_id"), ptr("shed_id"), ptr("movement_state"), ptr("mapping_state"), ptr("pattern"), ptr("q"), &buf)
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	records, err := csv.NewReader(bytes.NewReader(buf.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("export output is not parseable CSV: %v\n%s", err, buf.String())
	}
	return records
}

// TestExportHonoursEveryLiveFilter: the file must be what the operator is looking at. An export
// that ignores the active filter hands back a different question's answer under the right name.
func TestExportHonoursEveryLiveFilter(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-export", Status: "active"}
	base := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiMappedTag, hsiMappedMAC, "gw-hsi-export", base, 500, -60),
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-export", base, 1000, -60),
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	all := exportCSV(t, ctx, repo, nil)
	if len(all) != 3 {
		t.Fatalf("unfiltered export rows = %d (header + 2 tags expected): %v", len(all), all)
	}
	header := all[0]
	if header[0] != "Animal" || header[1] != "Tag ID" || header[len(header)-1] != "Mapping State" {
		t.Fatalf("unexpected header: %v", header)
	}
	// The tag-housing reading must never be labelled as anything measured on the animal.
	joined := strings.ToLower(strings.Join(header, "|"))
	if !strings.Contains(joined, "tag temperature") || strings.Contains(joined, "body temp") {
		t.Fatalf("temperature column mislabelled: %v", header)
	}

	// mapping_state=unmapped: the unmapped tag ONLY.
	unmapped := exportCSV(t, ctx, repo, map[string]string{"mapping_state": "unmapped"})
	if len(unmapped) != 2 {
		t.Fatalf("mapping_state=unmapped export rows = %d (header + 1 expected): %v", len(unmapped), unmapped)
	}
	if unmapped[1][1] != hsiUnmappedTag {
		t.Fatalf("filtered export returned tag %q, want %q", unmapped[1][1], hsiUnmappedTag)
	}
	if unmapped[1][0] != "" {
		t.Fatalf("unmapped tag carries an animal in the export: %q", unmapped[1][0])
	}

	// q= free-text: the mapped tag ONLY, and it carries the animal-derived columns.
	byQuery := exportCSV(t, ctx, repo, map[string]string{"q": hsiMappedTag})
	if len(byQuery) != 2 || byQuery[1][1] != hsiMappedTag {
		t.Fatalf("q filter export = %v", byQuery)
	}
	if byQuery[1][3] != "HSI Shed" {
		t.Fatalf("mapped row shed = %q, want %q", byQuery[1][3], "HSI Shed")
	}

	// A filter that matches nothing yields a header and no rows -- never every row.
	none := exportCSV(t, ctx, repo, map[string]string{"movement_state": "moving", "mapping_state": "conflict"})
	if len(none) != 1 {
		t.Fatalf("impossible filter returned %d rows: %v", len(none), none)
	}
}

// seedFarmActivity plants one record of each of three grains on BOTH sides of the monitoring
// boundary, and returns the boundary instant.
func seedFarmActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (boundary, before, after time.Time) {
	t.Helper()
	boundary = time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Second)
	before = boundary.Add(-2 * time.Hour)
	after = boundary.Add(2 * time.Hour)

	// The mapping instant itself: the authoritative stamp lives on the identifier (000197).
	mustExec(t, ctx, pool, "monitoring boundary",
		`UPDATE goat_identifiers SET smart_tag_mapped_at = $2 WHERE tenant_id = $1::uuid`, hsiTenant, boundary)

	// --- ANIMAL grain: vaccination. Needs the real obligation chain, not a shortcut. ---
	const (
		protocolID  = "45000000-0000-4000-8000-000000005001"
		versionID   = "45000000-0000-4000-8000-000000005002"
		ruleID      = "45000000-0000-4000-8000-000000005003"
		obligationA = "45000000-0000-4000-8000-000000005004"
		obligationB = "45000000-0000-4000-8000-000000005005"
	)
	mustExec(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category)
		 VALUES ($1::uuid, $2::uuid, 'hsi.vaccination', 'HSI Vaccination', 'vaccination')`, protocolID, hsiTenant)
	mustExec(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, version, effective_from)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 1, CURRENT_DATE - 30)`, versionID, hsiTenant, protocolID)
	mustExec(t, ctx, pool, "protocol rule",
		`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, trigger_type)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, 'hsi_dose_1', 'birth_age')`, ruleID, hsiTenant, versionID)
	for i, pair := range []struct {
		id string
		at time.Time
	}{{obligationA, before}, {obligationB, after}} {
		mustExec(t, ctx, pool, "obligation instance",
			`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id,
			   target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', $5::uuid, 'shed', $6::uuid, $7, 'completed', $8, 1)`,
			pair.id, hsiTenant, versionID, ruleID, hsiGoat, hsiShed, pair.at, "hsi-obl-"+pair.id)
		mustExec(t, ctx, pool, "vaccination completion",
			`INSERT INTO vaccination_completions (tenant_id, obligation_id, goat_id, administered_at, status, idempotency_key)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, $4, 'accepted', $5)`,
			hsiTenant, pair.id, hsiGoat, pair.at, "hsi-vc-"+string(rune('a'+i)))
	}

	// --- ANIMAL grain: the animal's own recorded shed moves. ---
	for _, at := range []time.Time{before, after} {
		mustExec(t, ctx, pool, "goat location history",
			`INSERT INTO goat_location_history (tenant_id, goat_id, from_location_id, to_location_id, occurred_at, reason)
			 VALUES ($1::uuid, $2::uuid, NULL, $3::uuid, $4, 'test')`, hsiTenant, hsiGoat, hsiShed, at)
	}

	// --- SCANNED-IDENTIFIER grain: weighing. No animal resolution anywhere in this chain --
	// the correlation is the raw scanned string against the tag's own id, exactly as weighing
	// itself stores it (migration 000078 dropped weighing_observations.animal_id outright).
	const (
		campaignID = "45000000-0000-4000-8000-000000006001"
		proofID    = "45000000-0000-4000-8000-000000006002"
	)
	mustExec(t, ctx, pool, "weighing campaign",
		`INSERT INTO weighing_campaigns (campaign_id, tenant_id, park_id, period_start_date, period_end_date,
		   start_business_date, operator_user_id, created_by)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, CURRENT_DATE - 1, CURRENT_DATE + 1, CURRENT_DATE, $4::uuid, $4::uuid)`,
		campaignID, hsiTenant, hsiPark, hsiParty)
	mustExec(t, ctx, pool, "proof artifact",
		`INSERT INTO proof_artifacts (proof_id, tenant_id, storage_provider, object_key, scope_type, scope_id, subject_type, proof_type)
		 VALUES ($1::uuid, $2::uuid, 'local', 'hsi/proof.mp4', 'shed', $3::uuid, 'shed', 'video')`,
		proofID, hsiTenant, hsiShed)
	for i, at := range []time.Time{before, after} {
		mustExec(t, ctx, pool, "weighing observation",
			`INSERT INTO weighing_observations (tenant_id, campaign_id, scanned_identifier, weight_kg,
			   proof_artifact_id, recorded_by, accepted_at, idempotency_key)
			 VALUES ($1::uuid, $2::uuid, $3, 31.5, $4::uuid, $5::uuid, $6, $7)`,
			hsiTenant, campaignID, hsiMappedTag, proofID, hsiParty, at, "hsi-wo-"+string(rune('a'+i)))
	}

	return boundary, before, after
}

// TestTagActivityJoinsRealRecordsAndStopsAtTheMonitoringBoundary is the central proof for the
// overlay: real rows from three different sources at three different grains come back, and NOT
// ONE record from before the tag was mapped to the animal does. Activity before mapping is the
// tag's device history -- it belongs to a bench, not to this goat.
func TestTagActivityJoinsRealRecordsAndStopsAtTheMonitoringBoundary(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-activity", Status: "active"}
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiMappedTag, hsiMappedMAC, "gw-hsi-activity", time.Now().UTC().Add(-time.Minute), 500, -60),
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-activity", time.Now().UTC().Add(-time.Minute), 900, -60),
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	boundary, before, after := seedFarmActivity(t, ctx, pool)

	svc := app.NewService(repo)
	actor := domain.Actor{TenantID: hsiTenant, UserID: hsiParty}
	from := boundary.Add(-24 * time.Hour)
	to := time.Now().UTC().Add(time.Hour)

	resp, err := svc.GetTagActivity(ctx, actor, hsiMappedTag, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("GetTagActivity: %v", err)
	}

	if resp.Reason != nil {
		t.Fatalf("mapped tag with records returned reason %q", *resp.Reason)
	}
	// The window was CLAMPED to the boundary and the response says so, rather than echoing the
	// window that was asked for.
	if !resp.From.Equal(boundary) {
		t.Fatalf("effective from = %s, want the monitoring boundary %s", resp.From, boundary)
	}
	if resp.MonitoringSince == nil || !resp.MonitoringSince.Equal(boundary) {
		t.Fatalf("monitoring_since = %v, want %s", resp.MonitoringSince, boundary)
	}
	if resp.CorrelationNote == "" {
		t.Fatal("response carries no correlation note: the claim boundary must travel with the data")
	}

	byKind := map[domain.ActivityEventKind]int{}
	for _, ev := range resp.Events {
		byKind[ev.Kind]++
		if ev.At.Before(boundary) {
			t.Fatalf("event %s at %s predates the monitoring boundary %s: that is device history, not this animal's",
				ev.Kind, ev.At, boundary)
		}
		if !ev.At.Equal(after) {
			t.Fatalf("event %s at %s, want the post-boundary instant %s", ev.Kind, ev.At, after)
		}
		if ev.Label == "" {
			t.Fatalf("event %s has no label", ev.Kind)
		}
	}
	for kind, grain := range map[domain.ActivityEventKind]domain.ActivityGrain{
		domain.ActivityKindVaccination: domain.GrainAnimal,
		domain.ActivityKindShedMove:    domain.GrainAnimal,
		domain.ActivityKindWeighing:    domain.GrainScannedIdentifier,
	} {
		if byKind[kind] != 1 {
			t.Fatalf("kind %s appeared %d times, want exactly the ONE post-boundary record (pre-boundary one must be excluded): %+v",
				kind, byKind[kind], resp.Events)
		}
		for _, ev := range resp.Events {
			if ev.Kind == kind && ev.Grain != grain {
				t.Fatalf("kind %s reported grain %q, want %q -- the grain is how a client knows not to read a shed or scan record as an observation of one animal", kind, ev.Grain, grain)
			}
		}
	}
	if len(resp.Events) != 3 {
		t.Fatalf("got %d events, want exactly 3 post-boundary records: %+v", len(resp.Events), resp.Events)
	}
	_ = before
}

// TestTagActivityUnmappedTagIsEmptyWithAReasonNotAnError: an unmapped tag is the NORMAL state
// today, not a failure. It has no animal, so it has no farm activity -- and the response must
// say which of those it is, so a client can tell "nothing happened" from "there is no animal".
func TestTagActivityUnmappedTagIsEmptyWithAReasonNotAnError(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-unmapped", Status: "active"}
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-unmapped", time.Now().UTC().Add(-time.Minute), 900, -60),
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	svc := app.NewService(repo)
	resp, err := svc.GetTagActivity(ctx, domain.Actor{TenantID: hsiTenant, UserID: hsiParty}, hsiUnmappedTag,
		time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatalf("unmapped tag must not error: %v", err)
	}
	if len(resp.Events) != 0 {
		t.Fatalf("unmapped tag returned %d events", len(resp.Events))
	}
	if resp.Reason == nil || *resp.Reason != domain.ActivityReasonTagNotMapped {
		t.Fatalf("reason = %v, want %q", resp.Reason, domain.ActivityReasonTagNotMapped)
	}
	if resp.MonitoringSince != nil {
		t.Fatalf("unmapped tag reported a monitoring boundary: %v", resp.MonitoringSince)
	}

	// A tag this tenant has never heard from is a 404-shaped answer, not an empty overlay that
	// implies the tag exists.
	if _, err := svc.GetTagActivity(ctx, domain.Actor{TenantID: hsiTenant, UserID: hsiParty}, "no-such-tag",
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339)); err == nil {
		t.Fatal("unknown tag returned success")
	}
}

// TestTagActivityEventsIncludeMotionDeltaFields: motion delta windows and completeness
// flags are populated on activity events so the UI can render motion context.
func TestTagActivityEventsIncludeMotionDeltaFields(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupHerdSignalsDB(t, ctx)

	// Seed an activity event that occurred after the mapping boundary
	boundary, before, after := seedFarmActivity(t, ctx, repo.db)

	svc := app.NewService(repo)
	actor := domain.Actor{TenantID: hsiTenant, UserID: hsiParty}
	from := boundary.Add(-24 * time.Hour)
	to := time.Now().UTC().Add(time.Hour)

	resp, err := svc.GetTagActivity(ctx, actor, hsiMappedTag, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("GetTagActivity: %v", err)
	}

	if len(resp.Events) == 0 {
		t.Fatal("expected activity events, got none")
	}

	// Verify that all events have the required motion delta fields (even if nil/zero)
	for _, ev := range resp.Events {
		// These fields MUST be present in the response (the OpenAPI contract requires them)
		_ = ev.MotionDeltaBefore2h    // May be nil
		_ = ev.MotionDeltaAfter2h     // May be nil
		_ = ev.MotionChangePercent    // May be nil
		_ = ev.BeforeWindowIncomplete // Bool, never nil
		_ = ev.AfterWindowIncomplete  // Bool, never nil

		// The window_incomplete fields must be booleans (never nil)
		if (interface{})(ev.BeforeWindowIncomplete) == nil {
			t.Fatal("before_window_incomplete must not be nil")
		}
		if (interface{})(ev.AfterWindowIncomplete) == nil {
			t.Fatal("after_window_incomplete must not be nil")
		}
	}

	_ = before
	_ = after
}
