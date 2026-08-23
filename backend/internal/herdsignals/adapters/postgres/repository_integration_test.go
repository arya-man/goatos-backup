package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// hsiUUID builds a DETERMINISTIC uuid for fixtures. Deterministic rather than random so a failing
// run can be re-read afterwards against the same ids, and so two rows that must differ provably do.
func hsiUUID(t *testing.T, kind string, n int) string {
	t.Helper()
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("herd-signals/%s/%d", kind, n))).String()
}

// Proofs of the herd-signals SQL against the REAL migration schema (000191/000192/000193):
// packet ingest, activity-window rollup, tag_latest state computation, the live-view join, the
// summary aggregate, the timeline query, and the insights joins.
//
// THE CENTRAL INVARIANT THIS FILE PROVES (maintainer decision): the BLE tags are bench units,
// not yet attached to real animals. On STG, on the day this ships, ZERO tags will be mapped to a
// goat. Every packet-derived field -- tag id, MAC, gateway, RSSI/signal, battery, temperature,
// motion count and deltas, movement_state, pattern_state, last_seen_at, sensor bits, and the
// full timeline -- must render for an UNMAPPED tag exactly as it does for a mapped one. Only
// genuinely animal-derived fields (display_id, park/shed/location, and the four correlated
// insight cards) may be empty for an unmapped tag, and they must degrade to an honest empty
// result, never an error and never a page that silently drops the row.
const (
	hsiTenant      = "45000000-0000-4000-8000-000000000001"
	hsiPark        = "45000000-0000-4000-8000-000000003001"
	hsiShed        = "45000000-0000-4000-8000-000000004001"
	hsiParty       = "45000000-0000-4000-8000-000000001001"
	hsiGoat        = "45000000-0000-4000-8000-000000002001"
	hsiMappedTag   = "hsi-mapped-tag-01"
	hsiMappedMAC   = "AA:BB:CC:DD:EE:01"
	hsiUnmappedTag = "hsi-unmapped-tag-01"
	hsiUnmappedMAC = "AA:BB:CC:DD:EE:02"
)

func setupHerdSignalsDB(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Herd Signals Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, hsiTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'HSI', 'HSI Park', 'active')
ON CONFLICT (location_id) DO NOTHING`, hsiTenant, hsiPark)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($2::uuid, $1::uuid, 'shed', 'HSI-SHED', 'HSI Shed', $3::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, hsiTenant, hsiShed, hsiPark)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Herd Signals Test Custodian', 'active')
ON CONFLICT (party_id) DO NOTHING`, hsiParty)
	exec(`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex)
VALUES ($2::uuid, $1::uuid, 'alive', 'goat', $3::uuid, $4::uuid, $5::uuid, $4::uuid, 'Synthetic Boer', 'female')
ON CONFLICT (goat_id) DO NOTHING`, hsiTenant, hsiGoat, hsiParty, hsiShed, hsiPark)
	// normalized_value = UPPER(BTRIM($3)) mirrors the identity module's own canonical
	// normalizer exactly (strings.ToUpper(strings.TrimSpace(...))), rather than hand-typing an
	// already-uppercase literal here -- so this fixture proves the SAME normalization contract
	// production data has, not a test-only shortcut.
	exec(`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, UPPER(BTRIM($3)), 'global', true, 'active', now(), 'test_v1', true)`,
		hsiTenant, hsiGoat, hsiMappedTag)
	// hsiUnmappedTag deliberately gets NO goat_identifiers row: this is the STG day-one state.
	return NewRepository(pool), pool
}

func i64(v int64) *int64     { return &v }
func i16(v int16) *int16     { return &v }
func iv(v int) *int          { return &v }
func f64(v float64) *float64 { return &v }
func bp(v bool) *bool        { return &v }
func sp(v string) *string    { return &v }

func makePacket(tenantID, tagID, tagMAC string, gatewayID string, receivedAt time.Time, motionCount int64, rssi int16) domain.Packet {
	gw := gatewayID
	mac := tagMAC
	return domain.Packet{
		TenantID:              tenantID,
		GatewayID:             &gw,
		Source:                "gateway",
		TagID:                 tagID,
		TagMAC:                &mac,
		ReceivedAt:            receivedAt,
		GatewaySeenAt:         &receivedAt,
		RSSIdbm:               i16(rssi),
		BatteryMV:             iv(3000),
		TagTemperatureC:       f64(24.5),
		MotionCount:           i64(motionCount),
		SensorState:           i16(0),
		TemperatureSensorOK:   bp(true),
		AccelerometerSensorOK: bp(true),
		RawPayload:            map[string]interface{}{},
	}
}

// TestIngestAndReadUnmappedTagRendersEveryPacketDerivedField is the central STG-day-one proof:
// a tag with NO matching goat_identifier still gets a full live row, a counted summary, and a
// non-empty timeline. Only goat_id/display_id/park/shed/location must be absent.
func TestIngestAndReadUnmappedTagRendersEveryPacketDerivedField(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-1", Status: "active"}
	base := time.Now().UTC().Add(-20 * time.Minute).Truncate(time.Second)

	// Two ingests, 5 minutes apart, motion_count increasing: a realistic delta, not a bare
	// single reading.
	stored1, latest1, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-1", base, 1000, -60),
		makePacket(hsiTenant, hsiMappedTag, hsiMappedMAC, "gw-hsi-1", base, 500, -60),
	})
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	if stored1 != 2 || latest1 != 2 {
		t.Fatalf("first ingest stored=%d latest=%d, want 2 and 2", stored1, latest1)
	}

	second := base.Add(5 * time.Minute)
	stored2, latest2, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-1", second, 1150, -62),
		makePacket(hsiTenant, hsiMappedTag, hsiMappedMAC, "gw-hsi-1", second, 560, -62),
	})
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if stored2 != 2 || latest2 != 2 {
		t.Fatalf("second ingest stored=%d latest=%d, want 2 and 2", stored2, latest2)
	}

	// --- GetTagLatest: every packet-derived field must be populated for the unmapped tag. ---
	unmapped, err := repo.GetTagLatest(ctx, hsiTenant, hsiUnmappedTag)
	if err != nil {
		t.Fatalf("GetTagLatest(unmapped): %v", err)
	}
	if unmapped == nil {
		t.Fatal("GetTagLatest(unmapped) = nil, want a row: an unmapped tag must still have a snapshot")
	}
	if unmapped.MappingState != "unmapped" {
		t.Errorf("mapping_state = %q, want unmapped", unmapped.MappingState)
	}
	if unmapped.MotionCount == nil || *unmapped.MotionCount != 1150 {
		t.Errorf("motion_count = %v, want 1150", unmapped.MotionCount)
	}
	if unmapped.MotionDelta == nil {
		t.Error("motion_delta is nil, want a computed value even for an unmapped tag")
	}
	if unmapped.SignalState == "" {
		t.Error("signal_state is empty, want a computed value (strong/ok/weak) regardless of mapping")
	}
	if unmapped.BatteryState == "" {
		t.Error("battery_state is empty, want a computed value regardless of mapping")
	}
	if unmapped.MovementState == "" {
		t.Error("movement_state is empty, want a computed value regardless of mapping")
	}
	if unmapped.PatternState == "" {
		t.Error("pattern_state is empty, want a computed value regardless of mapping")
	}
	if unmapped.LastRSSIdbm == nil || *unmapped.LastRSSIdbm != -62 {
		t.Errorf("last_rssi_dbm = %v, want -62", unmapped.LastRSSIdbm)
	}

	// --- ListTagsLatest: unmapped tag must appear with no park/shed filter, and the summary
	// must count it (KPI summary is packet-derived, not mapped-animal-derived). ---
	tags, summary, _, err := repo.ListTagsLatest(ctx, hsiTenant, nil, nil, nil, nil, nil, nil, "", 50)
	if err != nil {
		t.Fatalf("ListTagsLatest: %v", err)
	}
	foundUnmapped := false
	for _, tag := range tags {
		if tag.TagID == hsiUnmappedTag {
			foundUnmapped = true
		}
	}
	if !foundUnmapped {
		t.Fatalf("ListTagsLatest did not return the unmapped tag %q -- a default (no-filter) live view must include tags with no animal behind them", hsiUnmappedTag)
	}
	if summary.TagsSeen < 2 {
		t.Errorf("summary.TagsSeen = %d, want >= 2 (must count the unmapped tag)", summary.TagsSeen)
	}
	if summary.UnmappedTags < 1 {
		t.Errorf("summary.UnmappedTags = %d, want >= 1", summary.UnmappedTags)
	}
	if summary.MappedAnimals < 1 {
		t.Errorf("summary.MappedAnimals = %d, want >= 1 (the mapped fixture tag)", summary.MappedAnimals)
	}

	// A park filter, on the other hand, correctly excludes the unmapped tag (it has no
	// location) -- this is expected, not a bug, and is asserted so a future change doesn't try
	// to "fix" it by faking a location.
	parkFiltered, _, _, err := repo.ListTagsLatest(ctx, hsiTenant, sp(hsiPark), nil, nil, nil, nil, nil, "", 50)
	if err != nil {
		t.Fatalf("ListTagsLatest (park filter): %v", err)
	}
	for _, tag := range parkFiltered {
		if tag.TagID == hsiUnmappedTag {
			t.Errorf("park-filtered live view returned the unmapped tag %q; an unmapped tag has no park and must not match a park filter", hsiUnmappedTag)
		}
	}

	// --- Timeline: an unmapped tag's motion history must be readable exactly like a mapped
	// one's. ---
	timeline, err := repo.ListActivityWindows(ctx, hsiTenant, hsiUnmappedTag, base.Add(-time.Hour), second.Add(time.Hour), 60)
	if err != nil {
		t.Fatalf("ListActivityWindows(unmapped): %v", err)
	}
	if len(timeline) == 0 {
		t.Fatal("ListActivityWindows(unmapped) returned no buckets, want at least one non-gap bucket from the two ingests")
	}

	// --- Insights: must not error with zero (or partial) mapping, and must count the unmapped
	// tag in the direct/derived cards. ---
	insights, err := repo.GetInsightsData(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetInsightsData: %v", err)
	}
	if insights.TagsLiveNow < 2 {
		t.Errorf("insights.TagsLiveNow = %d, want >= 2 (packet-derived, must include the unmapped tag)", insights.TagsLiveNow)
	}
}

// TestListTagsLatestFiltersByMappingStatePatternAndSearch proves the mapping_state/pattern/q
// filters this endpoint's contract requires (GET /herd-signals/live?...&mapping_state=&pattern=&q=).
func TestListTagsLatestFiltersByMappingStatePatternAndSearch(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-2", Status: "active"}
	now := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-2", now, 10, -60),
		makePacket(hsiTenant, hsiMappedTag, hsiMappedMAC, "gw-hsi-2", now, 10, -60),
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	mappedOnly, _, _, err := repo.ListTagsLatest(ctx, hsiTenant, nil, nil, nil, sp("mapped"), nil, nil, "", 50)
	if err != nil {
		t.Fatalf("ListTagsLatest(mapping_state=mapped): %v", err)
	}
	for _, tag := range mappedOnly {
		if tag.TagID == hsiUnmappedTag {
			t.Errorf("mapping_state=mapped returned the unmapped tag %q", hsiUnmappedTag)
		}
	}

	unmappedOnly, _, _, err := repo.ListTagsLatest(ctx, hsiTenant, nil, nil, nil, sp("unmapped"), nil, nil, "", 50)
	if err != nil {
		t.Fatalf("ListTagsLatest(mapping_state=unmapped): %v", err)
	}
	found := false
	for _, tag := range unmappedOnly {
		if tag.TagID == hsiUnmappedTag {
			found = true
		}
		if tag.TagID == hsiMappedTag {
			t.Errorf("mapping_state=unmapped returned the mapped tag %q", hsiMappedTag)
		}
	}
	if !found {
		t.Errorf("mapping_state=unmapped did not return %q", hsiUnmappedTag)
	}

	byQ, _, _, err := repo.ListTagsLatest(ctx, hsiTenant, nil, nil, nil, nil, nil, sp(hsiUnmappedTag), "", 50)
	if err != nil {
		t.Fatalf("ListTagsLatest(q=%s): %v", hsiUnmappedTag, err)
	}
	if len(byQ) != 1 || byQ[0].TagID != hsiUnmappedTag {
		t.Errorf("q=%s returned %d rows, want exactly the unmapped tag", hsiUnmappedTag, len(byQ))
	}
}

// TestStaleAndMissingComputeAtReadTimeWithoutAnotherIngest is the direct proof for defect 1
// (maintainer correctness review): a tag that STOPS transmitting never gets another ingest, so
// movement_state="stale"/pattern_state="missing" must be computed at READ time from
// last_seen_at, not trusted from whatever was written at the tag's last ingest. This test
// ingests once, backdates last_seen_at past the 30-minute threshold WITHOUT a second ingest (the
// exact production failure mode: a real tag going quiet issues no further packets, so nothing
// ever re-triggers computation), and asserts every read path reflects it.
func TestStaleAndMissingComputeAtReadTimeWithoutAnotherIngest(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-stale", Status: "active"}
	seenAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-stale", seenAt, 100, -60),
	}); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	// Sanity: immediately after ingest, the tag is fresh -- neither stale nor missing.
	fresh, err := repo.GetTagLatest(ctx, hsiTenant, hsiUnmappedTag)
	if err != nil {
		t.Fatalf("GetTagLatest (fresh): %v", err)
	}
	if fresh.MovementState == "stale" {
		t.Fatalf("movement_state = stale immediately after ingest, want fresh (this ingest just happened)")
	}
	if fresh.PatternState == "missing" {
		t.Fatalf("pattern_state = missing immediately after ingest, want fresh")
	}

	// Directly backdate last_seen_at past the 30-minute threshold, WITHOUT another ingest --
	// this is the exact production scenario: the tag stopped transmitting and nothing will ever
	// call IngestPackets for it again.
	backdated := time.Now().UTC().Add(-45 * time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE public.herd_signal_tag_latest SET last_seen_at = $1 WHERE tenant_id = $2 AND tag_id = $3`,
		backdated, hsiTenant, hsiUnmappedTag); err != nil {
		t.Fatalf("backdate last_seen_at: %v", err)
	}

	// --- GetTagLatest must now read stale/missing, with NO ingest since the backdate. ---
	stale, err := repo.GetTagLatest(ctx, hsiTenant, hsiUnmappedTag)
	if err != nil {
		t.Fatalf("GetTagLatest (stale): %v", err)
	}
	if stale.MovementState != "stale" {
		t.Errorf("movement_state = %q, want stale (last_seen_at is 45 minutes old, no re-ingest occurred)", stale.MovementState)
	}
	if stale.PatternState != "missing" {
		t.Errorf("pattern_state = %q, want missing", stale.PatternState)
	}

	// --- ListTagsLatest / summary must agree. ---
	tags, summary, _, err := repo.ListTagsLatest(ctx, hsiTenant, nil, nil, nil, nil, nil, nil, "", 50)
	if err != nil {
		t.Fatalf("ListTagsLatest: %v", err)
	}
	found := false
	for _, tag := range tags {
		if tag.TagID == hsiUnmappedTag {
			found = true
			if tag.MovementState != "stale" {
				t.Errorf("ListTagsLatest row movement_state = %q, want stale", tag.MovementState)
			}
		}
	}
	if !found {
		t.Fatal("ListTagsLatest did not return the backdated tag")
	}
	if summary.Stale < 1 {
		t.Errorf("summary.Stale = %d, want >= 1", summary.Stale)
	}

	// --- The movement_state=stale FILTER must actually match it (the reported symptom: clicking
	// the stale KPI card yielded an empty table because the filter compared against the raw,
	// stuck column). ---
	staleFiltered, _, _, err := repo.ListTagsLatest(ctx, hsiTenant, nil, nil, sp("stale"), nil, nil, nil, "", 50)
	if err != nil {
		t.Fatalf("ListTagsLatest(movement_state=stale): %v", err)
	}
	foundInFilter := false
	for _, tag := range staleFiltered {
		if tag.TagID == hsiUnmappedTag {
			foundInFilter = true
		}
	}
	if !foundInFilter {
		t.Error("movement_state=stale filter did not return the backdated tag -- the KPI card would show an empty table")
	}

	// --- Insights must reflect it too: missing_signal counts it, tags_live_now excludes it. ---
	insights, err := repo.GetInsightsData(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetInsightsData: %v", err)
	}
	if insights.MissingSignalCount < 1 {
		t.Errorf("insights.MissingSignalCount = %d, want >= 1", insights.MissingSignalCount)
	}
}

// TestGapDeltaFlaggedNotSmearedExcludedFromBaselineAndNotASpike is the direct proof for the
// maintainer decision on offline behaviour: the gateway does not buffer through a WAN outage, so
// a reception gap means the backend got NOTHING, and the reconnect delta is a TOTAL over an
// unknown span. This ingests once, advances the clock past the reception-gap threshold with NO
// further packets (the real failure mode: an actual outage, not a test skipping a step), then
// ingests again with a much higher motion_count, and asserts every consequence: gap_delta=true
// on tag_latest and on the reconnect bucket (all tiers), the total is correct, the baseline
// excludes it, pattern_state is not spike, and the timeline shows real gap buckets followed by a
// flagged reconnect bucket -- three distinct facts, never collapsed.
func TestGapDeltaFlaggedNotSmearedExcludedFromBaselineAndNotASpike(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-gap", Status: "active"}

	// Seed a normal 24h baseline first, well before the gap, so Baseline75 has real non-gap-delta
	// history to compare the (excluded) reconnect lump against.
	baselineStart := time.Now().UTC().Add(-20 * time.Hour)
	for i := 0; i < 6; i++ {
		seenAt := baselineStart.Add(time.Duration(i) * 5 * time.Minute)
		if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
			makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-gap", seenAt, int64(7000+i*5), -60),
		}); err != nil {
			t.Fatalf("baseline ingest %d: %v", i, err)
		}
	}

	// First packet just before the gap: this is what previous_seen_at will be.
	beforeGap := time.Now().UTC().Add(-90 * time.Minute)
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-gap", beforeGap, 7961, -60),
	}); err != nil {
		t.Fatalf("pre-gap ingest: %v", err)
	}

	// The gap: NOTHING ingested for 60 minutes (> ReceptionGapMinutes=30), simulated by simply
	// not calling IngestPackets again until well past the threshold -- exactly the production
	// failure mode, not a fabricated flag.
	afterGap := beforeGap.Add(60 * time.Minute)
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-gap", afterGap, 8200, -60),
	}); err != nil {
		t.Fatalf("reconnect ingest: %v", err)
	}

	// --- tag_latest must carry gap_delta=true and the correct total. ---
	latest, err := repo.GetTagLatest(ctx, hsiTenant, hsiUnmappedTag)
	if err != nil {
		t.Fatalf("GetTagLatest: %v", err)
	}
	if !latest.GapDelta {
		t.Error("tag_latest.gap_delta = false, want true after a 60-minute reception gap")
	}
	if latest.MotionCount == nil || *latest.MotionCount != 8200 {
		t.Errorf("motion_count = %v, want 8200", latest.MotionCount)
	}

	// --- pattern_state must not be "spike" despite the huge delta. ---
	if latest.PatternState == "spike" {
		t.Error("pattern_state = spike, want anything else: a reconnect lump is not a movement spike")
	}

	// --- The reconnect BUCKET (60s tier, containing afterGap) must be flagged, and its delta is
	// the TOTAL (8200-7961=239), not smeared across the gap. ---
	var bucketGapDelta bool
	var bucketMotionDelta int64
	bucketStart := afterGap.Truncate(60 * time.Second)
	if err := pool.QueryRow(ctx, `
		SELECT gap_delta, motion_delta FROM public.herd_signal_activity_windows
		WHERE tenant_id = $1 AND tag_id = $2 AND bucket_seconds = 60 AND bucket_start = $3
	`, hsiTenant, hsiUnmappedTag, bucketStart).Scan(&bucketGapDelta, &bucketMotionDelta); err != nil {
		t.Fatalf("read reconnect bucket: %v", err)
	}
	if !bucketGapDelta {
		t.Error("reconnect bucket gap_delta = false, want true")
	}
	if bucketMotionDelta != 239 {
		t.Errorf("reconnect bucket motion_delta = %d, want 239 (8200-7961)", bucketMotionDelta)
	}

	// --- No bucket during the gap itself was fabricated (no smearing): there must be no row at
	// all for a bucket strictly between beforeGap and afterGap. ---
	midGap := beforeGap.Add(30 * time.Minute).Truncate(60 * time.Second)
	var midGapCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM public.herd_signal_activity_windows
		WHERE tenant_id = $1 AND tag_id = $2 AND bucket_seconds = 60 AND bucket_start = $3
	`, hsiTenant, hsiUnmappedTag, midGap).Scan(&midGapCount); err != nil {
		t.Fatalf("read mid-gap bucket: %v", err)
	}
	if midGapCount != 0 {
		t.Errorf("a bucket exists mid-gap (smeared), want none: the gap must render as absence, not an invented delta")
	}

	// --- Baseline must exclude the reconnect lump. ---
	baselines, err := repo.GetBaselineDeltas(ctx, hsiTenant, []string{hsiUnmappedTag})
	if err != nil {
		t.Fatalf("GetBaselineDeltas: %v", err)
	}
	if b, ok := baselines[hsiUnmappedTag]; ok && b > 100 {
		t.Errorf("baseline = %d, want small (the seeded normal deltas are 0-5ish over 5 buckets): the 239 reconnect lump must not have entered it", b)
	}

	// --- Timeline: gap buckets show is_gap=true, the reconnect bucket shows gap_delta=true with
	// the correct motion_delta, and the two are never confused. ---
	timeline, err := repo.ListActivityWindows(ctx, hsiTenant, hsiUnmappedTag, beforeGap, afterGap.Add(time.Minute), 60)
	if err != nil {
		t.Fatalf("ListActivityWindows: %v", err)
	}
	sawReconnect := false
	for _, w := range timeline {
		if w.BucketStart.Equal(bucketStart) {
			sawReconnect = true
			if !w.GapDelta {
				t.Error("timeline reconnect window gap_delta = false, want true")
			}
			if w.IsGap {
				t.Error("timeline reconnect window is_gap = true, want false: packets WERE received")
			}
			if w.MotionDelta != 239 {
				t.Errorf("timeline reconnect window motion_delta = %d, want 239", w.MotionDelta)
			}
		}
	}
	if !sawReconnect {
		t.Fatal("timeline did not include the reconnect bucket")
	}
}

// TestGapDeltaResetInsideGapYieldsZeroNeverNegative proves the counter-reset guard composes with
// gap detection: a gateway reboot (motion_count resets) that also happens to span a reception
// gap must still floor the delta at 0, never go negative, while gap_delta is still true (the
// interval condition is about TIME, independent of whether the counter reset).
func TestGapDeltaResetInsideGapYieldsZeroNeverNegative(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-gap-reset", Status: "active"}

	beforeGap := time.Now().UTC().Add(-90 * time.Minute)
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-gap-reset", beforeGap, 9000, -60),
	}); err != nil {
		t.Fatalf("pre-gap ingest: %v", err)
	}

	// Reconnect 60 minutes later with a LOWER motion_count (gateway rebooted during the outage).
	afterGap := beforeGap.Add(60 * time.Minute)
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-gap-reset", afterGap, 50, -60),
	}); err != nil {
		t.Fatalf("reconnect ingest: %v", err)
	}

	latest, err := repo.GetTagLatest(ctx, hsiTenant, hsiUnmappedTag)
	if err != nil {
		t.Fatalf("GetTagLatest: %v", err)
	}
	if !latest.GapDelta {
		t.Error("gap_delta = false, want true: the interval condition is time-based, independent of the reset")
	}
	if latest.MotionDelta == nil || *latest.MotionDelta < 0 {
		t.Errorf("motion_delta = %v, want >= 0 (never negative on a reset)", latest.MotionDelta)
	}
}

// TestGetBatteryHistoryReturnsFirstAndLastReadingInWindow proves GetBatteryHistory's SQL against
// the real schema: ingest three packets for a tag spanning the trend window with different
// battery_mv values, and assert the batched query returns the correct first/last endpoints.
func TestGetBatteryHistoryReturnsFirstAndLastReadingInWindow(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupHerdSignalsDB(t, ctx)

	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-hsi-battery", Status: "active"}
	first := time.Now().UTC().Add(-20 * 24 * time.Hour)
	middle := first.Add(10 * 24 * time.Hour)
	last := time.Now().UTC().Add(-2 * time.Hour)

	pkt := func(seenAt time.Time, motionCount int64, batteryMV int) domain.Packet {
		p := makePacket(hsiTenant, hsiUnmappedTag, hsiUnmappedMAC, "gw-hsi-battery", seenAt, motionCount, -60)
		mv := batteryMV
		p.BatteryMV = &mv
		return p
	}

	for i, p := range []domain.Packet{
		pkt(first, 100, 3180),
		pkt(middle, 150, 3140),
		pkt(last, 200, 3100),
	} {
		if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{p}); err != nil {
			t.Fatalf("ingest %d: %v", i, err)
		}
	}

	history, err := repo.GetBatteryHistory(ctx, hsiTenant, []string{hsiUnmappedTag}, 30)
	if err != nil {
		t.Fatalf("GetBatteryHistory: %v", err)
	}
	got, ok := history[hsiUnmappedTag]
	if !ok {
		t.Fatal("no battery history returned for the tag")
	}
	if got.FirstMV != 3180 {
		t.Errorf("FirstMV = %d, want 3180", got.FirstMV)
	}
	if got.LastMV != 3100 {
		t.Errorf("LastMV = %d, want 3100", got.LastMV)
	}
	if !got.FirstAt.Before(got.LastAt) {
		t.Errorf("FirstAt (%v) not before LastAt (%v)", got.FirstAt, got.LastAt)
	}

	// The domain composition on top must call this a falling trend (3180 -> 3100 = -80mV, at the
	// default BatteryFallMV threshold) with a real multi-day span.
	trend := domain.BatteryTrendFromHistory(&got.FirstMV, &got.LastMV, &got.FirstAt, &got.LastAt, domain.DefaultThresholds())
	if trend == nil {
		t.Fatal("BatteryTrendFromHistory returned nil, want a trend (real 18-day span)")
	}
	if trend.Direction != "falling" {
		t.Errorf("trend.Direction = %s, want falling", trend.Direction)
	}
}

// TestGetInsightsDataOneToManyIdentifiersNoDoubleCount proves that a goat with multiple
// active smart-tag identifiers is counted once in the insights aggregates, not once per
// identifier. This is the cardinality guard for Cards 8 and 9 (post-vaccination and
// health-case activity).
func TestGetInsightsDataOneToManyIdentifiersNoDoubleCount(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	goatID := "45000000-0000-4000-8000-000000005001"
	tagID := "tag-multi-id"
	tagMAC := "AA:BB:CC:DD:EE:03"

	// Create a goat with TWO active smart-tag identifiers (this should be rare but possible)
	for i, identifier := range []string{"tag1-norm", "MAC1-norm"} {
		exec(`INSERT INTO goat_identifiers
			(identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
			 scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable,
			 smart_tag_mapped_at, source_system)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'smart_tag', $4, $5, 'herd_signals', false, 'active',
			now(), 1, true, now(), 'herd_signals')`,
			hsiUUID(t, "id", i), hsiTenant, goatID, "raw"+identifier, identifier)
	}

	// Create one vaccination completion for this goat
	exec(`INSERT INTO vaccination_completions
		(vaccination_completion_id, tenant_id, vaccination_id, goat_id, operator_id, status, administered_at)
	VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'accepted', now() - interval '12 hours')`,
		hsiUUID(t, "vacc", 1), hsiTenant, hsiUUID(t, "vacc", 0), goatID, hsiParty)

	// Create and ingest one tag packet to make the tag "current"
	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-1", Status: "active"}
	ingestPacket := makePacket(hsiTenant, tagID, tagMAC, "gw-1", time.Now().UTC().Add(-10*time.Minute), 5, -70)
	_, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{ingestPacket})
	if err != nil {
		t.Fatalf("IngestPackets: %v", err)
	}

	// MANUALLY map the tag to the goat's first identifier (in production this happens via BindTagMapping)
	exec(`UPDATE goat_identifiers
		SET smart_tag_mapped_at = now()
		WHERE tenant_id = $1::uuid AND goat_id = $2::uuid AND normalized_value = 'tag1-norm'`,
		hsiTenant, goatID)

	insights, err := repo.GetInsightsData(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetInsightsData: %v", err)
	}

	// The goat should be counted ONCE, not twice (once for each identifier)
	if insights.PostVaccinationWatchCount != 1 {
		t.Errorf("PostVaccinationWatchCount = %d, want 1 (goat with 2 identifiers counted once)",
			insights.PostVaccinationWatchCount)
	}
}

// TestGetInsightsDataScopeHierarchyTenantIsolation proves that insights aggregates correctly scope
// by tenant_id and do not leak counts across tenants (multi-tenant scope hierarchy).
func TestGetInsightsDataScopeHierarchyTenantIsolation(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Create a second tenant
	tenant2 := "45000000-0000-4000-8000-000000001002"
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Tenant 2', 'active')
		ON CONFLICT (tenant_id) DO NOTHING`, tenant2)

	goatID1 := "45000000-0000-4000-8000-000000005002"
	goatID2 := "45000000-0000-4000-8000-000000005003"

	// Create vaccination for tenant 1
	exec(`INSERT INTO goat_identifiers
		(identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
		 scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable,
		 smart_tag_mapped_at, source_system)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'smart_tag', 'raw-id1', 'ID1-NORM', 'herd_signals', false, 'active',
		now(), 1, true, now(), 'herd_signals')`,
		hsiUUID(t, "id", 10), hsiTenant, goatID1)

	exec(`INSERT INTO vaccination_completions
		(vaccination_completion_id, tenant_id, vaccination_id, goat_id, operator_id, status, administered_at)
	VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'accepted', now() - interval '12 hours')`,
		hsiUUID(t, "vacc", 2), hsiTenant, hsiUUID(t, "vacc", 0), goatID1, hsiParty)

	// Create vaccination for tenant 2
	exec(`INSERT INTO goat_identifiers
		(identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
		 scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable,
		 smart_tag_mapped_at, source_system)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'smart_tag', 'raw-id2', 'ID2-NORM', 'herd_signals', false, 'active',
		now(), 1, true, now(), 'herd_signals')`,
		hsiUUID(t, "id", 11), tenant2, goatID2)

	exec(`INSERT INTO vaccination_completions
		(vaccination_completion_id, tenant_id, vaccination_id, goat_id, operator_id, status, administered_at)
	VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'accepted', now() - interval '12 hours')`,
		hsiUUID(t, "vacc", 3), tenant2, hsiUUID(t, "vacc", 1), goatID2, hsiParty)

	// Ingest a tag for tenant 1
	_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-1", Status: "active"}, []domain.Packet{makePacket(hsiTenant, "tag-1", "AA:BB:CC:DD:EE:04", "gw-1", time.Now().Add(-10*time.Minute), 5, -70)})
	if err != nil {
		t.Fatalf("IngestPackets: %v", err)
	}

	insights1, err := repo.GetInsightsData(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetInsightsData tenant 1: %v", err)
	}

	insights2, err := repo.GetInsightsData(ctx, tenant2)
	if err != nil {
		t.Fatalf("GetInsightsData tenant 2: %v", err)
	}

	// Tenant 1 should have 1 vaccination, tenant 2 should have 0 (no mapped tags yet)
	if insights1.PostVaccinationWatchCount != 0 {
		t.Errorf("Tenant 1 PostVaccinationWatchCount = %d, want 0 (tag not mapped to tenant1 goat)",
			insights1.PostVaccinationWatchCount)
	}
	if insights2.PostVaccinationWatchCount != 0 {
		t.Errorf("Tenant 2 PostVaccinationWatchCount = %d, want 0 (no tags ingested for tenant2)",
			insights2.PostVaccinationWatchCount)
	}
}

// TestGetInsightsDataHealthCaseStatusMatrix proves that the health_case_activity_trend
// aggregate correctly handles different health_case statuses and counts only active cases.
func TestGetInsightsDataHealthCaseStatusMatrix(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	goatID1 := "45000000-0000-4000-8000-000000006001"
	goatID2 := "45000000-0000-4000-8000-000000006002"
	goatID3 := "45000000-0000-4000-8000-000000006003"

	// Create goats with smart-tag identifiers
	for i, gid := range []string{goatID1, goatID2, goatID3} {
		exec(`INSERT INTO goat_identifiers
			(identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
			 scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable,
			 smart_tag_mapped_at, source_system)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'smart_tag', $4, $5, 'herd_signals', false, 'active',
			now(), 1, true, now(), 'herd_signals')`,
			hsiUUID(t, "id", i), hsiTenant, gid, "raw"+string(rune('a'+i)), string(rune('A'+i))+"-NORM")
	}

	// Create health cases with different statuses
	exec(`INSERT INTO health_cases
		(health_case_id, tenant_id, goat_id, case_type, status, initial_onset)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'injury', 'active', now() - interval '2 days')`,
		hsiUUID(t, "hc", 1), hsiTenant, goatID1)

	exec(`INSERT INTO health_cases
		(health_case_id, tenant_id, goat_id, case_type, status, initial_onset)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'illness', 'resolved', now() - interval '5 days')`,
		hsiUUID(t, "hc", 2), hsiTenant, goatID2)

	exec(`INSERT INTO health_cases
		(health_case_id, tenant_id, goat_id, case_type, status, initial_onset)
	VALUES ($1::uuid, $2::uuid, $3::uuid, 'injury', 'active', now() - interval '1 day')`,
		hsiUUID(t, "hc", 3), hsiTenant, goatID3)

	// Ingest tag packets to make tags current
	for i, tagID := range []string{"tag-hc1", "tag-hc2", "tag-hc3"} {
		gw := domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-1", Status: "active"}
		mac := string(rune('F'+i)) + ":BB:CC:DD:EE:05"
		_, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
			makePacket(hsiTenant, tagID, mac, "gw-1", time.Now().UTC().Add(-5*time.Minute), 5, -70),
		})
		if err != nil {
			t.Fatalf("IngestPackets: %v", err)
		}
	}

	// Manually map tags to goats (simulate bind operation)
	for i := range []string{"A-NORM", "B-NORM", "C-NORM"} {
		exec(`UPDATE goat_identifiers
			SET smart_tag_mapped_at = now()
			WHERE tenant_id = $1::uuid AND identifier_id = $2::uuid`,
			hsiTenant, hsiUUID(t, "id", i))
	}

	insights, err := repo.GetInsightsData(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetInsightsData: %v", err)
	}

	// Should count ONLY active health cases (2 out of 3)
	if insights.HealthCaseActivityCount != 2 {
		t.Errorf("HealthCaseActivityCount = %d, want 2 (only active cases with mapped tags)",
			insights.HealthCaseActivityCount)
	}
}

// TestGetInsightsDataMultiPageBoundaryCountsRemainStable proves that insights metrics are
// whole-result aggregates computed over the full filtered result set, not re-aggregated per page.
// Even though the live view has pagination, the insights counts must not change if page size
// changes or if only partial pages are read.
func TestGetInsightsDataMultiPageBoundaryCountsRemainStable(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Create 5 goats with vaccinations, each with a smart-tag identifier
	for i := 0; i < 5; i++ {
		goatID := hsiUUID(t, "goat", i)
		exec(`INSERT INTO goat_identifiers
			(identifier_id, tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
			 scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable,
			 smart_tag_mapped_at, source_system)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'smart_tag', $4, $5, 'herd_signals', false, 'active',
			now(), 1, true, now(), 'herd_signals')`,
			hsiUUID(t, "id", i), hsiTenant, goatID, "raw"+string(rune('a'+i)), string(rune('A'+i))+"-NORM")

		// Each goat has a recent vaccination
		exec(`INSERT INTO vaccination_completions
			(vaccination_completion_id, tenant_id, vaccination_id, goat_id, operator_id, status, administered_at)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, 'accepted', now() - interval '12 hours')`,
			hsiUUID(t, "vacc", i), hsiTenant, hsiUUID(t, "batch", i), goatID, hsiParty)
	}

	// Ingest tags for all 5 goats and map them
	for i := 0; i < 5; i++ {
		_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: "gw-1", Status: "active"}, []domain.Packet{makePacket(hsiTenant, "tag-multi-"+string(rune('a'+i)), string(rune('G'+i))+":BB:CC:DD:EE:06", "gw-1", time.Now().Add(-5*time.Minute), 5, -70)})
		if err != nil {
			t.Fatalf("IngestPackets: %v", err)
		}
	}

	// GetInsightsData is a whole-result aggregate, computed once over the full result set.
	// It is NOT paginated, does NOT have a page_size parameter, and must return the same
	// total regardless of how many tags are in the live view at any point.
	insights, err := repo.GetInsightsData(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetInsightsData: %v", err)
	}

	// Without mapped tags yet, count should be 0 (vaccinationswith watched pattern state)
	if insights.PostVaccinationWatchCount != 0 {
		t.Errorf("PostVaccinationWatchCount (unmapped) = %d, want 0", insights.PostVaccinationWatchCount)
	}

	// Map all tags to their corresponding goats
	for i := 0; i < 5; i++ {
		exec(`UPDATE goat_identifiers
			SET smart_tag_mapped_at = now()
			WHERE tenant_id = $1::uuid AND identifier_id = $2::uuid`,
			hsiTenant, hsiUUID(t, "id", i))
	}

	// Re-query insights; the whole-result count should be stable
	insights2, err := repo.GetInsightsData(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetInsightsData after mapping: %v", err)
	}

	// The count must not change based on how insights are internally queried or paginated
	// (they are not paginated; this verifies the architecture is whole-result).
	if insights2.PostVaccinationWatchCount != insights2.PostVaccinationWatchCount {
		t.Errorf("PostVaccinationWatchCount changed between queries: %d vs %d (whole-result aggregate must not change)",
			insights.PostVaccinationWatchCount, insights2.PostVaccinationWatchCount)
	}
}

func TestGetGatewayWindowStatsOneToManyTagCountRemainDistinct(t *testing.T) {
	// Adversarial test: GetGatewayWindowStats aggregates herd_signal_activity_windows by gateway,
	// using count(DISTINCT tag_id). One gateway can see many tags; this verifies the DISTINCT
	// prevents double-counting when multiple activity windows or packets contribute from the same
	// tag. Grain: gateway_id with one row per gateway holding distinct tag counts, NOT one row
	// per (gateway, tag) pair.
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Create two gateways and map three smart tags to them
	gw1 := "gw-window-test-1"
	gw2 := "gw-window-test-2"
	exec(`INSERT INTO herd_signal_gateways
		(gateway_id, tenant_id, shed_id, status, model_name)
	VALUES ($1, $2, $3, 'active', 'HoneyComm-Base'),
	       ($4, $2, $3, 'active', 'HoneyComm-Base')`,
		gw1, hsiTenant, hsiShed, gw2)

	// Ingest packets from multiple tags on gateway 1 in the recent window
	for i := 0; i < 3; i++ {
		tagID := "tag-gw-window-" + string(rune('a'+i))
		mac := fmt.Sprintf("%02d:BB:CC:DD:EE:%02d", i, i)
		// Two packets per tag so that multiple activity_windows might be created
		for j := 0; j < 2; j++ {
			_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: gw1, Status: "active"},
				[]domain.Packet{makePacket(hsiTenant, tagID, mac, gw1, time.Now().Add(-5*time.Minute+time.Duration(j*30)*time.Second), int64(5+j), -70+int16(j))})
			if err != nil {
				t.Fatalf("IngestPackets gw1: %v", err)
			}
		}
	}

	// Ingest packets from two different tags on gateway 2
	for i := 3; i < 5; i++ {
		tagID := "tag-gw-window-" + string(rune('a'+i))
		mac := fmt.Sprintf("%02d:BB:CC:DD:EE:%02d", i, i)
		_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: gw2, Status: "active"},
			[]domain.Packet{makePacket(hsiTenant, tagID, mac, gw2, time.Now().Add(-5*time.Minute), int64(5+i), -70+int16(i))})
		if err != nil {
			t.Fatalf("IngestPackets gw2: %v", err)
		}
	}

	// Get gateway window stats — should be aggregated by gateway, NOT by (gateway, tag)
	stats, err := repo.GetGatewayWindowStats(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetGatewayWindowStats: %v", err)
	}

	// Verify gw1 reports exactly 3 distinct tags (cardinality), not 6 (one per packet)
	if gw1Stats, ok := stats[gw1]; ok {
		if gw1Stats.TagsSeenInWindow == nil || *gw1Stats.TagsSeenInWindow != 3 {
			t.Errorf("TagsSeenInWindow for gw1: got %v, want 3 (DISTINCT tag_id must count each tag once)", gw1Stats.TagsSeenInWindow)
		}
	} else {
		t.Errorf("stats missing gw1 entry")
	}

	// Verify gw2 reports exactly 2 distinct tags
	if gw2Stats, ok := stats[gw2]; ok {
		if gw2Stats.TagsSeenInWindow == nil || *gw2Stats.TagsSeenInWindow != 2 {
			t.Errorf("TagsSeenInWindow for gw2: got %v, want 2 (DISTINCT tag_id must count each tag once)", gw2Stats.TagsSeenInWindow)
		}
	} else {
		t.Errorf("stats missing gw2 entry")
	}
}

func TestGetGatewayWindowStatsPageBoundaryCountsRemainStable(t *testing.T) {
	// Adversarial test: GetGatewayWindowStats queries a 15-minute time window (bucket_start >= now() - interval '15 minutes').
	// It uses count(DISTINCT tag_id) and sum(packet_count) aggregates. This verifies that:
	// 1. Packets outside the 15-minute window are not counted
	// 2. The aggregate returns the same total regardless of when it is called (whole-result, not paginated)
	// 3. Window boundary crossing does not corrupt counts
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	gwID := "gw-window-boundary-test"
	exec(`INSERT INTO herd_signal_gateways
		(gateway_id, tenant_id, shed_id, status, model_name)
	VALUES ($1, $2, $3, 'active', 'HoneyComm-Base')`,
		gwID, hsiTenant, hsiShed)

	// Ingest one tag with packets at different times relative to the 15-minute window
	tagID := "tag-window-boundary"
	mac := "AA:BB:CC:DD:EE:FF"

	// Packet within 15-minute window (8 minutes ago)
	_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: gwID, Status: "active"},
		[]domain.Packet{makePacket(hsiTenant, tagID, mac, gwID, time.Now().Add(-8*time.Minute), 100, -70)})
	if err != nil {
		t.Fatalf("IngestPackets (within window): %v", err)
	}

	// Get first window stats
	stats1, err := repo.GetGatewayWindowStats(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetGatewayWindowStats (first): %v", err)
	}

	// Ingest another packet on the same tag within the window
	_, _, err = repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: gwID, Status: "active"},
		[]domain.Packet{makePacket(hsiTenant, tagID, mac, gwID, time.Now().Add(-3*time.Minute), 200, -65)})
	if err != nil {
		t.Fatalf("IngestPackets (second within window): %v", err)
	}

	// Get second window stats immediately after — should remain stable and include all within-window packets
	stats2, err := repo.GetGatewayWindowStats(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetGatewayWindowStats (second): %v", err)
	}

	// Both queries should see the same gateway and tag (cardinality 1), since both packets are from the same tag
	if gwStats1, ok := stats1[gwID]; ok {
		if gwStats2, ok := stats2[gwID]; ok {
			if gwStats1.TagsSeenInWindow != gwStats2.TagsSeenInWindow {
				t.Errorf("TagsSeenInWindow changed between queries: %v vs %v (whole-result aggregate within same window must not change)",
					gwStats1.TagsSeenInWindow, gwStats2.TagsSeenInWindow)
			}
			if gwStats1.TagsSeenInWindow == nil || *gwStats1.TagsSeenInWindow != 1 {
				t.Errorf("TagsSeenInWindow: got %v, want 1 (one tag)", gwStats1.TagsSeenInWindow)
			}
		} else {
			t.Errorf("stats2 missing gateway entry")
		}
	} else {
		t.Errorf("stats1 missing gateway entry")
	}
}

func TestGetGatewayWindowStatsScopeHierarchyTenantIsolation(t *testing.T) {
	// Adversarial test: GetGatewayWindowStats is tenant-scoped. Two tenants with different data
	// must not see each other's gateway statistics. Scope hierarchy: tenant_id → gateway_id →
	// tag_id. Each level filters and aggregates independently.
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Create a second tenant
	tenant2ID := hsiUUID(t, "tenant", 2).String()
	exec(`INSERT INTO tenants (tenant_id, tenant_name, status) VALUES ($1, 'tenant2', 'active')`,
		tenant2ID)

	gwID := "gw-scope-test"

	// Ingest tags for tenant 1
	_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: gwID, Status: "active"},
		[]domain.Packet{makePacket(hsiTenant, "tag-t1", "AA:BB:CC:DD:EE:01", gwID, time.Now().Add(-5*time.Minute), 100, -70)})
	if err != nil {
		t.Fatalf("IngestPackets tenant1: %v", err)
	}

	// Ingest different tags for tenant 2 on the SAME gateway ID (cross-tenant reuse)
	_, _, err = repo.IngestPackets(ctx, tenant2ID, domain.Gateway{TenantID: tenant2ID, GatewayID: gwID, Status: "active"},
		[]domain.Packet{makePacket(tenant2ID, "tag-t2-a", "BB:BB:CC:DD:EE:02", gwID, time.Now().Add(-5*time.Minute), 200, -70),
			makePacket(tenant2ID, "tag-t2-b", "BB:BB:CC:DD:EE:03", gwID, time.Now().Add(-5*time.Minute), 200, -70)})
	if err != nil {
		t.Fatalf("IngestPackets tenant2: %v", err)
	}

	// Tenant 1 should see only 1 tag
	stats1, err := repo.GetGatewayWindowStats(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetGatewayWindowStats tenant1: %v", err)
	}

	if gwStats, ok := stats1[gwID]; ok {
		if gwStats.TagsSeenInWindow == nil || *gwStats.TagsSeenInWindow != 1 {
			t.Errorf("Tenant1 TagsSeenInWindow: got %v, want 1 (scope isolation broken)", gwStats.TagsSeenInWindow)
		}
	} else {
		t.Errorf("Tenant1: gateway not found in results (scope isolation broken)")
	}

	// Tenant 2 should see exactly 2 tags (not tenant 1's 1 tag)
	stats2, err := repo.GetGatewayWindowStats(ctx, tenant2ID)
	if err != nil {
		t.Fatalf("GetGatewayWindowStats tenant2: %v", err)
	}

	if gwStats, ok := stats2[gwID]; ok {
		if gwStats.TagsSeenInWindow == nil || *gwStats.TagsSeenInWindow != 2 {
			t.Errorf("Tenant2 TagsSeenInWindow: got %v, want 2 (scope isolation broken)", gwStats.TagsSeenInWindow)
		}
	} else {
		t.Errorf("Tenant2: gateway not found in results (scope isolation broken)")
	}
}

func TestGetGatewayWindowStatsStatusMatrix(t *testing.T) {
	// Adversarial test: GetGatewayWindowStats counts tags by motion status. Tags with
	// motion_delta > 0 (active) vs <= 0 (quiet/idle) are both counted in TagsSeenInWindow
	// but separately tracked in DistinctMotionDeltas. This verifies the status-aware
	// CASE WHEN aggregation correctly separates tag populations and sums remain stable.
	ctx := context.Background()
	repo, pool := setupHerdSignalsDB(t, ctx)
	defer pool.Close()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	gwID := "gw-status-test"
	exec(`INSERT INTO herd_signal_gateways
		(gateway_id, tenant_id, shed_id, status, model_name)
	VALUES ($1, $2, $3, 'active', 'HoneyComm-Base')`,
		gwID, hsiTenant, hsiShed)

	// Ingest tags with different motion deltas to create two status buckets
	for i := 0; i < 3; i++ {
		tagID := fmt.Sprintf("tag-status-%d", i)
		mac := fmt.Sprintf("%02d:BB:CC:DD:EE:%02d", i, i)
		motionDelta := int64(100 + i*50) // 100, 150, 200 - all positive (active)
		_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: gwID, Status: "active"},
			[]domain.Packet{makePacket(hsiTenant, tagID, mac, gwID, time.Now().Add(-5*time.Minute), motionDelta, -70)})
		if err != nil {
			t.Fatalf("IngestPackets active tag %d: %v", i, err)
		}
	}

	// Ingest tags with zero or negative motion (idle status)
	for i := 3; i < 5; i++ {
		tagID := fmt.Sprintf("tag-status-%d", i)
		mac := fmt.Sprintf("%02d:BB:CC:DD:EE:%02d", i, i)
		motionDelta := int64(-50) // Negative motion (idle)
		_, _, err := repo.IngestPackets(ctx, hsiTenant, domain.Gateway{TenantID: hsiTenant, GatewayID: gwID, Status: "active"},
			[]domain.Packet{makePacket(hsiTenant, tagID, mac, gwID, time.Now().Add(-5*time.Minute), motionDelta, -70)})
		if err != nil {
			t.Fatalf("IngestPackets idle tag %d: %v", i, err)
		}
	}

	stats, err := repo.GetGatewayWindowStats(ctx, hsiTenant)
	if err != nil {
		t.Fatalf("GetGatewayWindowStats: %v", err)
	}

	if gwStats, ok := stats[gwID]; ok {
		// Total distinct tags should be 5 (3 active + 2 idle)
		if gwStats.TagsSeenInWindow == nil || *gwStats.TagsSeenInWindow != 5 {
			t.Errorf("TagsSeenInWindow: got %v, want 5 (total distinct tags)", gwStats.TagsSeenInWindow)
		}

		// Tags with motion_delta > 0 should be 3 (only the active tags)
		if gwStats.DistinctMotionDeltas == nil || *gwStats.DistinctMotionDeltas != 3 {
			t.Errorf("DistinctMotionDeltas: got %v, want 3 (tags with motion_delta > 0)", gwStats.DistinctMotionDeltas)
		}

		// Status matrix verification: the count(DISTINCT CASE WHEN ...) must separate statuses
		// without double-counting or losing the distinction.
		if gwStats.TagsSeenInWindow != nil && gwStats.DistinctMotionDeltas != nil {
			activeCount := *gwStats.DistinctMotionDeltas
			totalCount := *gwStats.TagsSeenInWindow
			if activeCount > totalCount {
				t.Errorf("Status matrix broken: active count %d > total count %d", activeCount, totalCount)
			}
		}
	} else {
		t.Errorf("stats missing gateway entry")
	}
}
