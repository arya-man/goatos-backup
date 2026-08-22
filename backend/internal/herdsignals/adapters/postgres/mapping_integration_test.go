package postgres

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
)

// Postgres-backed proofs of the three mapping verbs -- MAP, REPLACE, UNMAP -- against the REAL
// migration schema, and of the monitoring boundary they stamp.
//
// EVERY assertion about "is it mapped" goes through the READ PATH (ResolveTagMapping /
// ListTagsLatest / GetTagLatest), never by inspecting the goat_identifiers row the write just
// made. A write that produced a row satisfying some other predicate than the one the live view
// actually uses would still pass a row-inspection test and would still leave the dashboard
// saying "unmapped".

const (
	hsmGoatB   = "45000000-0000-4000-8000-000000002002"
	hsmTagA    = "hsm-tag-a"
	hsmTagAMAC = "AA:BB:CC:DD:EF:01"
	hsmTagB    = "hsm-tag-b"
	hsmTagBMAC = "AA:BB:CC:DD:EF:02"
)

// setupMappingDB provides the migrated Postgres these proofs run against.
//
// Default: the repo's own containerised harness (setupHerdSignalsDB), same as every other
// integration test here. Escape hatch: GOATOS_HERD_SIGNALS_TEST_DSN points at an already-migrated
// database instead -- used to run these proofs against the OCI dev instance, which carries ~46k
// REAL gateway packets, when a local container engine is unavailable. The DSN path scrubs the
// synthetic test tenant first so a re-run starts from the same state a fresh container would:
// goat_identifiers_lifetime_value_unique claims a tag value for the tenant's LIFETIME, so a
// second run would otherwise collide with its own first run's bindings. The goats themselves are
// NOT deleted -- goat_hard_delete_blocked forbids it, correctly -- they are re-seeded idempotently.
func setupMappingDB(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("GOATOS_HERD_SIGNALS_TEST_DSN"))
	if dsn == "" {
		return setupHerdSignalsDB(t, ctx)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to GOATOS_HERD_SIGNALS_TEST_DSN: %v", err)
	}
	t.Cleanup(pool.Close)
	// Scoped to the synthetic test tenant ONLY. Never widen this.
	for _, table := range []string{
		"herd_signal_activity_windows", "herd_signal_packets", "herd_signal_tag_latest",
		"herd_signal_gateways", "goat_identifiers",
	} {
		if _, err := pool.Exec(ctx, "DELETE FROM public."+table+" WHERE tenant_id = $1::uuid", hsiTenant); err != nil {
			t.Fatalf("scrub %s for the test tenant: %v", table, err)
		}
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Herd Signals Test', 'active') ON CONFLICT (tenant_id) DO NOTHING`, hsiTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'HSI', 'HSI Park', 'active') ON CONFLICT (location_id) DO NOTHING`, hsiTenant, hsiPark)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
VALUES ($2::uuid, $1::uuid, 'shed', 'HSI-SHED', 'HSI Shed', $3::uuid, 'active') ON CONFLICT (location_id) DO NOTHING`, hsiTenant, hsiShed, hsiPark)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status)
VALUES ($1::uuid, 'org', 'Herd Signals Test Custodian', 'active') ON CONFLICT (party_id) DO NOTHING`, hsiParty)
	exec(`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex)
VALUES ($2::uuid, $1::uuid, 'alive', 'goat', $3::uuid, $4::uuid, $5::uuid, $4::uuid, 'Synthetic Boer', 'female')
ON CONFLICT (goat_id) DO NOTHING`, hsiTenant, hsiGoat, hsiParty, hsiShed, hsiPark)
	exec(`INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version, smart_tag_capable)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, UPPER(BTRIM($3)), 'global', true, 'active', now(), 'test_v1', true)`, hsiTenant, hsiGoat, hsiMappedTag)
	return NewRepository(pool), pool
}

// seedSecondGoat adds an animal with NO identifiers, so a bind against it starts from the real
// staging state: a tag broadcasting, an animal on the ground, and nothing joining them.
func seedSecondGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	mustExec(t, ctx, pool, "second goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex)
		 VALUES ($2::uuid, $1::uuid, 'alive', 'goat', $3::uuid, $4::uuid, $5::uuid, $4::uuid, 'Synthetic Boer', 'male')
		 ON CONFLICT (goat_id) DO NOTHING`,
		hsiTenant, hsmGoatB, hsiParty, hsiShed, hsiPark)
}

func ingestTag(t *testing.T, ctx context.Context, repo *Repository, gatewayID, tagID, tagMAC string, at time.Time, motion int64) {
	t.Helper()
	gw := domain.Gateway{TenantID: hsiTenant, GatewayID: gatewayID, Status: "active"}
	if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{
		makePacket(hsiTenant, tagID, tagMAC, gatewayID, at, motion, -60),
	}); err != nil {
		t.Fatalf("ingest %s at %s: %v", tagID, at, err)
	}
}

func mappingStateFromLive(t *testing.T, ctx context.Context, repo *Repository, tagID string) string {
	t.Helper()
	tags, _, _, err := repo.ListTagsLatest(ctx, hsiTenant, nil, nil, nil, nil, nil, nil, "", 200)
	if err != nil {
		t.Fatalf("ListTagsLatest: %v", err)
	}
	for _, tag := range tags {
		if tag.TagID == tagID {
			return tag.MappingState
		}
	}
	t.Fatalf("tag %q is missing from the live view entirely", tagID)
	return ""
}

// liveSmartTagValues reads, FROM THE DATABASE, every identifier value still bound as a live smart
// tag for an animal.
//
// This exists because asserting the read path is not enough, and that gap shipped a real bug: a
// unmap that released only the tag-id row left the MAC row live, the tag READ as unmapped, every
// API surface looked right, and the animal was silently pinned to a dead binding it could never
// be freed from through the product. The API's own view of "is this mapped" is exactly the view
// that was wrong, so the binding must be checked underneath it.
func liveSmartTagValues(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT normalized_value FROM goat_identifiers
		WHERE tenant_id = $1::uuid AND goat_id = $2::uuid
		      AND status = 'active' AND smart_tag_capable IS TRUE
		ORDER BY normalized_value
	`, hsiTenant, goatID)
	if err != nil {
		t.Fatalf("read live smart tag values: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, v)
	}
	return out
}

// identifierRowsForValues reads EVERY goat_identifiers row holding these values, whatever its
// status. The coordinator's second symptom was invisible to a status='active' query: an unmap
// left RETIRED rows behind, which still claim the value under the lifetime-unique index and then
// refuse every future MAP and REPLACE on that animal, with nothing in the product able to clear
// them. So the assertion has to be "no row survives", not "no ACTIVE row survives".
func identifierRowsForValues(t *testing.T, ctx context.Context, pool *pgxpool.Pool, values ...string) []string {
	t.Helper()
	norm := make([]string, len(values))
	for i, v := range values {
		norm[i] = domain.NormalizeTagIdentifier(v)
	}
	rows, err := pool.Query(ctx, `
		SELECT normalized_value || ' ' || status || ' smart_tag_capable=' || COALESCE(smart_tag_capable::text, 'null')
		FROM goat_identifiers
		WHERE tenant_id = $1::uuid AND normalized_value = ANY($2)
		ORDER BY normalized_value
	`, hsiTenant, norm)
	if err != nil {
		t.Fatalf("read identifier rows: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, v)
	}
	return out
}

// TestMapTagToAnimalFlipsTheLiveReadToMapped is the primary proof: the action the Tag Mapping
// screen could not perform at all now makes the live view say "mapped", through the read path's
// own predicate.
func TestMapTagToAnimalFlipsTheLiveReadToMapped(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)

	ingestTag(t, ctx, repo, "gw-hsm-1", hsmTagA, hsmTagAMAC, time.Now().UTC().Add(-2*time.Minute), 100)
	if got := mappingStateFromLive(t, ctx, repo, hsmTagA); got != "unmapped" {
		t.Fatalf("before mapping: live mapping_state = %q, want unmapped (this is the staging day-one state)", got)
	}

	resp, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{
		GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC,
	})
	if err != nil {
		t.Fatalf("BindTagMapping: %v", err)
	}
	if resp.MappingState != "mapped" || resp.MonitoringSince == nil {
		t.Fatalf("bind response = %+v, want mapped with a monitoring boundary stamped", resp)
	}
	if len(resp.IdentifierIDs) != 2 {
		t.Errorf("bind claimed %d identifiers, want 2 (the tag id AND its distinct MAC -- the read path matches either, so claiming one would leave half this tag's packets resolving to nothing)", len(resp.IdentifierIDs))
	}

	// THE ACTUAL PROOF: the read path, not the row.
	if got := mappingStateFromLive(t, ctx, repo, hsmTagA); got != "mapped" {
		t.Errorf("after mapping: live mapping_state = %q, want mapped", got)
	}
	state, goatID, err := repo.ResolveTagMapping(ctx, hsiTenant, sp(hsmTagA), sp(hsmTagAMAC))
	if err != nil {
		t.Fatalf("ResolveTagMapping: %v", err)
	}
	if state != "mapped" || goatID == nil || *goatID != hsmGoatB {
		t.Errorf("ResolveTagMapping = (%q, %v), want (mapped, %s)", state, goatID, hsmGoatB)
	}
	// A lowercase MAC from a device must resolve too: normalization is the identity module's
	// canonical one, not hand-rolled per call site.
	byLowerMAC, err := repo.ResolveTagsBatch(ctx, hsiTenant, []string{"aa:bb:cc:dd:ef:01"})
	if err != nil {
		t.Fatalf("ResolveTagsBatch: %v", err)
	}
	if len(byLowerMAC) != 1 {
		t.Errorf("a lowercase device MAC resolved to %d identifiers, want 1", len(byLowerMAC))
	}

	// And a subsequent packet keeps it mapped: the ingest path re-resolves and must agree with
	// what the write did, including the denormalised boundary.
	ingestTag(t, ctx, repo, "gw-hsm-1", hsmTagA, hsmTagAMAC, time.Now().UTC().Add(-time.Minute), 120)
	latest, err := repo.GetTagLatest(ctx, hsiTenant, hsmTagA)
	if err != nil || latest == nil {
		t.Fatalf("GetTagLatest: %v", err)
	}
	if latest.MappingState != "mapped" {
		t.Errorf("after a post-mapping packet, mapping_state = %q, want mapped", latest.MappingState)
	}
}

// TestBindRefusesTheStatesItExistsToPrevent: a tag on another animal, and an animal that already
// carries a live tag. Both are 409-class refusals, not silent reconciliations.
func TestBindRefusesTheStatesItExistsToPrevent(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)

	if _, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA}); err != nil {
		t.Fatalf("first bind: %v", err)
	}

	// Same tag, different animal -- this is exactly the "conflict" state the UI renders.
	_, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsiGoat, TagID: hsmTagA})
	if !errors.Is(err, domain.ErrMappingConflict) {
		t.Errorf("binding a tag already on another animal returned %v, want a conflict", err)
	}

	// Same animal, a second tag -- that is REPLACE, not MAP.
	_, err = repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagB})
	if !errors.Is(err, domain.ErrMappingConflict) {
		t.Errorf("binding a second live tag to the same animal returned %v, want a conflict directing the caller to replace", err)
	}

	// An animal that does not exist is a 404-class failure, not a conflict and not a 500.
	_, err = repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{
		GoatID: "45000000-0000-4000-8000-0000000029ff", TagID: "hsm-tag-nobody",
	})
	if !errors.Is(err, domain.ErrMappingNotFound) {
		t.Errorf("binding to an unknown animal returned %v, want not-found", err)
	}

	// Re-binding the SAME tag to the SAME animal is idempotent, not a conflict: an operator who
	// double-taps must not be told they broke something.
	if _, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA}); err != nil {
		t.Errorf("re-binding the same tag to the same animal returned %v, want an idempotent success", err)
	}
}

// TestReplaceLeavesExactlyOneLiveBinding is the re-tagging proof: a tag falls off, a new one goes
// on. The animal must never end up with two live smart tags (ambiguous telemetry) or none
// (silently unmonitored), and the old tag must go back to being an ordinary broadcasting device.
func TestReplaceLeavesExactlyOneLiveBinding(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)

	ingestTag(t, ctx, repo, "gw-hsm-2", hsmTagA, hsmTagAMAC, time.Now().UTC().Add(-3*time.Minute), 100)
	ingestTag(t, ctx, repo, "gw-hsm-2", hsmTagB, hsmTagBMAC, time.Now().UTC().Add(-3*time.Minute), 200)

	first, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC})
	if err != nil {
		t.Fatalf("bind old tag: %v", err)
	}

	// A replace to a tag the animal does not have yet.
	time.Sleep(5 * time.Millisecond) // so the new monitoring period is distinguishable from the old
	second, err := repo.ReplaceTagMapping(ctx, hsiTenant, domain.ReplaceTagMappingRequest{
		GoatID: hsmGoatB, NewTagID: hsmTagB, NewTagMAC: hsmTagBMAC,
	})
	if err != nil {
		t.Fatalf("ReplaceTagMapping: %v", err)
	}
	if len(second.UnboundIdentifierIDs) != 2 {
		t.Errorf("replace released %d identifiers, want the old tag's 2 (id and MAC)", len(second.UnboundIdentifierIDs))
	}
	if second.MonitoringSince == nil || !second.MonitoringSince.After(*first.MonitoringSince) {
		t.Errorf("replace monitoring_since = %v, want a NEW period strictly after the old one (%v): the replacement tag's bench history is not this animal's history",
			second.MonitoringSince, first.MonitoringSince)
	}

	// Exactly one live binding, through the read path.
	if got := mappingStateFromLive(t, ctx, repo, hsmTagB); got != "mapped" {
		t.Errorf("new tag live mapping_state = %q, want mapped", got)
	}
	if got := mappingStateFromLive(t, ctx, repo, hsmTagA); got != "unmapped" {
		t.Errorf("old tag live mapping_state = %q, want unmapped: a tag that fell off must stop being attributed to the animal", got)
	}

	// DATABASE-level, not API-level: exactly the NEW tag's two values, and not one row left over
	// from the old tag. A leftover MAC row would be invisible to every read path and would pin
	// this animal to a tag that is no longer on it -- the defect unmap shipped with.
	live := liveSmartTagValues(t, ctx, pool, hsmGoatB)
	wantLive := []string{domain.NormalizeTagIdentifier(hsmTagB), domain.NormalizeTagIdentifier(hsmTagBMAC)}
	sort.Strings(wantLive)
	if len(live) != len(wantLive) {
		t.Fatalf("animal carries live smart-tag values %v, want exactly %v (nothing from the old tag may survive a replace)", live, wantLive)
	}
	for i := range live {
		if live[i] != wantLive[i] {
			t.Errorf("live smart-tag values = %v, want %v", live, wantLive)
			break
		}
	}

	// And a re-tag AFTER a re-tag must still work: an animal that accumulated a dead MAC binding
	// on every swap would fail here on the second one.
	if _, err := repo.ReplaceTagMapping(ctx, hsiTenant, domain.ReplaceTagMappingRequest{
		GoatID: hsmGoatB, NewTagID: hsmTagA, NewTagMAC: hsmTagAMAC,
	}); err != nil {
		t.Fatalf("second replace (swapping back to the original tag): %v", err)
	}
	if live := liveSmartTagValues(t, ctx, pool, hsmGoatB); len(live) != 2 {
		t.Errorf("after a second replace the animal carries %v, want exactly the 2 values of the tag now on it: dead bindings are accumulating on every swap", live)
	}

	// The old tag's history is intact -- it was unbound, never deleted.
	oldLatest, err := repo.GetTagLatest(ctx, hsiTenant, hsmTagA)
	if err != nil || oldLatest == nil {
		t.Fatalf("old tag lost its snapshot after replace: %v", err)
	}
	if oldLatest.MotionCount == nil {
		t.Error("old tag's motion history was cleared by the replace; unbinding must not delete device telemetry")
	}

	// Replacing when there is nothing to replace is a refusal, not an accidental bind: an
	// operator who reaches for REPLACE on an unmapped animal must be told to MAP instead.
	const hsmGoatC = "45000000-0000-4000-8000-000000002003"
	mustExec(t, ctx, pool, "third goat",
		`INSERT INTO goats (goat_id, tenant_id, lifecycle_status, species, custodian_party_id, current_location_id, park_id, shed_id, breed, sex)
		 VALUES ($2::uuid, $1::uuid, 'alive', 'goat', $3::uuid, $4::uuid, $5::uuid, $4::uuid, 'Synthetic Boer', 'male')
		 ON CONFLICT (goat_id) DO NOTHING`,
		hsiTenant, hsmGoatC, hsiParty, hsiShed, hsiPark)
	if _, err := repo.ReplaceTagMapping(ctx, hsiTenant, domain.ReplaceTagMappingRequest{GoatID: hsmGoatC, NewTagID: "hsm-tag-c"}); !errors.Is(err, domain.ErrMappingConflict) {
		t.Errorf("replace on an animal with no live smart tag returned %v, want a conflict directing the caller to map", err)
	}
}

// TestUnmapReturnsTheTagToDeviceTelemetry: released, not deleted.
func TestUnmapReturnsTheTagToDeviceTelemetry(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)

	ingestTag(t, ctx, repo, "gw-hsm-3", hsmTagA, hsmTagAMAC, time.Now().UTC().Add(-2*time.Minute), 100)
	if _, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC}); err != nil {
		t.Fatalf("bind: %v", err)
	}

	resp, err := repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsmTagA, TagMAC: hsmTagAMAC})
	if err != nil {
		t.Fatalf("UnmapTagMapping: %v", err)
	}
	if resp.MappingState != "unmapped" || resp.MonitoringSince != nil {
		t.Errorf("unmap response = %+v, want unmapped with NO monitoring boundary", resp)
	}
	if got := mappingStateFromLive(t, ctx, repo, hsmTagA); got != "unmapped" {
		t.Errorf("after unmap, live mapping_state = %q, want unmapped", got)
	}

	// Packets keep flowing and keep rendering: the tag is still a real device.
	ingestTag(t, ctx, repo, "gw-hsm-3", hsmTagA, hsmTagAMAC, time.Now().UTC().Add(-time.Minute), 150)
	latest, err := repo.GetTagLatest(ctx, hsiTenant, hsmTagA)
	if err != nil || latest == nil {
		t.Fatalf("GetTagLatest after unmap: %v", err)
	}
	if latest.MotionCount == nil || *latest.MotionCount != 150 {
		t.Errorf("motion_count after unmap = %v, want 150: an unmapped tag is still fully rendered device telemetry", latest.MotionCount)
	}
	var boundary *time.Time
	if err := pool.QueryRow(ctx, `SELECT animal_monitoring_since FROM herd_signal_tag_latest WHERE tenant_id = $1::uuid AND tag_id = $2`, hsiTenant, hsmTagA).Scan(&boundary); err != nil {
		t.Fatalf("read boundary: %v", err)
	}
	if boundary != nil {
		t.Errorf("animal_monitoring_since = %v after unmap, want NULL: NULL is what makes 'no animal-attributed value' enforceable", boundary)
	}

	// Unmapping something that is not mapped is a named refusal, not a silent no-op -- the
	// operator believes they just released a binding.
	if _, err := repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsmTagA}); !errors.Is(err, domain.ErrMappingConflict) {
		t.Errorf("unmapping an unmapped tag returned %v, want a conflict", err)
	}
}

// TestUnmapReleasesTheWHOLEBindingNotJustTheValueNamed is the regression test for a defect that
// shipped past a green suite: MAP creates one identifier row per value the tag reports (its id AND
// its MAC), and UNMAP released only the rows matching the value the caller happened to name.
//
// The tag then READ as unmapped -- so the API, the live view and the previous version of this
// test all looked correct -- while the animal stayed pinned to the other half of the binding.
// Nothing in the product could release it, because unmap had already reported success. The animal
// could never be re-tagged through the UI again.
//
// So this test asserts the DATABASE, and then the sequence a real re-tagging actually performs.
func TestUnmapReleasesTheWHOLEBindingNotJustTheValueNamed(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)

	bind, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{
		GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC,
	})
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if len(bind.IdentifierIDs) != 2 {
		t.Fatalf("map created %d identifiers, want 2 (id and MAC) -- this test is about releasing BOTH", len(bind.IdentifierIDs))
	}

	// Unmap naming ONLY the tag id. The MAC row must go too.
	unmap, err := repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsmTagA})
	if err != nil {
		t.Fatalf("unmap: %v", err)
	}
	if len(unmap.UnboundIdentifierIDs) != 2 {
		t.Errorf("unmap released %d identifiers, want 2: naming one value must release the whole binding, not the half that value matched", len(unmap.UnboundIdentifierIDs))
	}
	if live := liveSmartTagValues(t, ctx, pool, hsmGoatB); len(live) != 0 {
		t.Fatalf("after unmap the animal still carries live smart-tag values %v, want NONE. The tag reads as unmapped, so nothing in the product can see or release this -- the animal is stuck", live)
	}
	// And NOT ONE row of ANY status survives. A retired leftover is just as fatal as an active
	// one: it keeps the value claimed under goat_identifiers_lifetime_value_unique, and then
	// every future MAP and REPLACE on this animal is refused with no product-side escape.
	if rows := identifierRowsForValues(t, ctx, pool, hsmTagA, hsmTagAMAC); len(rows) != 0 {
		t.Fatalf("after unmap these identifier rows survive: %v -- want none. A row this module created to carry a binding is deleted when the binding ends; leaving it in ANY status makes the animal impossible to re-tag through the product", rows)
	}

	// THE ROUND TRIP a real re-tagging performs: map A, unmap A, map B to the SAME animal.
	// This is what was impossible: the leftover row made the second map a 409.
	if _, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{
		GoatID: hsmGoatB, TagID: hsmTagB, TagMAC: hsmTagBMAC,
	}); err != nil {
		t.Fatalf("map a DIFFERENT tag to the same animal after unmapping the first: %v\nthis is exactly what a farm does when a tag falls off and is replaced, and a half-released binding makes it permanently impossible", err)
	}
	if live := liveSmartTagValues(t, ctx, pool, hsmGoatB); len(live) != 2 {
		t.Errorf("after the round trip the animal carries %v, want exactly the new tag's 2 values", live)
	}

	// And unmapping by MAC releases just as completely as unmapping by id.
	if _, err := repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsmTagBMAC}); err != nil {
		t.Fatalf("unmap by MAC: %v", err)
	}
	if live := liveSmartTagValues(t, ctx, pool, hsmGoatB); len(live) != 0 {
		t.Errorf("unmapping by MAC left %v behind; either value must release the whole binding", live)
	}

	// Re-mapping the SAME tag to the SAME animal after an unmap must work too: an operator who
	// unmaps by mistake has to be able to put it straight back.
	again, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{
		GoatID: hsmGoatB, TagID: hsmTagB, TagMAC: hsmTagBMAC,
	})
	if err != nil {
		t.Fatalf("re-map the SAME tag to the same animal after unmapping it: %v", err)
	}
	if again.MonitoringSince == nil {
		t.Fatal("re-map produced no monitoring boundary")
	}
	// A re-map after a genuine unmap starts a NEW monitoring period: the tag was off the animal
	// in between, and whatever it recorded then is not this animal's history.
	if !again.MonitoringSince.After(*bind.MonitoringSince) {
		t.Errorf("re-map monitoring_since = %v, want a new period after the original %v: the unmap ended the old one",
			again.MonitoringSince, bind.MonitoringSince)
	}
}

// TestMonitoringBoundaryKeepsBenchHistoryOutOfTheAnimalsBaseline is the boundary proof, and the
// reason migration 000197 exists at all.
//
// A tag is commissioned, powered up and broadcasting long before it goes on an animal. If the
// 24h p75 baseline is allowed to reach back past the mapping instant, the first thing the system
// tells a farm about a newly tagged goat is derived from a tag rattling in a box.
func TestMonitoringBoundaryKeepsBenchHistoryOutOfTheAnimalsBaseline(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)

	// BENCH PERIOD: the tag is on a desk, being carried and jostled. Big motion deltas, real
	// packets, hours before anyone maps it.
	benchBase := time.Now().UTC().Add(-6 * time.Hour)
	for i, motion := range []int64{1000, 1500, 2000, 2500} {
		ingestTag(t, ctx, repo, "gw-hsm-4", hsmTagA, hsmTagAMAC, benchBase.Add(time.Duration(i)*10*time.Minute), motion)
	}

	// An UNMAPPED tag has no animal, so it has no baseline at all -- not a zero, not a default.
	baselines, err := repo.GetBaselineDeltas(ctx, hsiTenant, []string{hsmTagA})
	if err != nil {
		t.Fatalf("GetBaselineDeltas (unmapped): %v", err)
	}
	if _, ok := baselines[hsmTagA]; ok {
		t.Errorf("an UNMAPPED tag was given a baseline (%d); with no animal behind it there is nothing to attribute a baseline to", baselines[hsmTagA])
	}

	// MAP IT. Animal monitoring starts now; everything above is device history.
	bind, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC})
	if err != nil {
		t.Fatalf("bind: %v", err)
	}

	baselines, err = repo.GetBaselineDeltas(ctx, hsiTenant, []string{hsmTagA})
	if err != nil {
		t.Fatalf("GetBaselineDeltas (just mapped): %v", err)
	}
	if b, ok := baselines[hsmTagA]; ok {
		t.Errorf("baseline = %d immediately after mapping, want NONE: every bucket on record predates the boundary and is bench movement, so there is nothing yet to build this animal's baseline from", b)
	}

	// REAL ANIMAL PERIOD: modest movement, well after the boundary.
	postBase := bind.MonitoringSince.Add(10 * time.Minute)
	for i, motion := range []int64{2502, 2504, 2506} {
		ingestTag(t, ctx, repo, "gw-hsm-4", hsmTagA, hsmTagAMAC, postBase.Add(time.Duration(i)*10*time.Minute), motion)
	}

	baselines, err = repo.GetBaselineDeltas(ctx, hsiTenant, []string{hsmTagA})
	if err != nil {
		t.Fatalf("GetBaselineDeltas (mapped, with post-boundary history): %v", err)
	}
	b, ok := baselines[hsmTagA]
	if !ok {
		t.Fatal("no baseline after post-boundary packets arrived; a mapped tag with its own history must have one")
	}
	// The bench buckets carried deltas of ~500. The animal's own buckets carry ~2. If the
	// boundary were not enforced the p75 would be in the hundreds.
	if b > 10 {
		t.Errorf("baseline = %d, want a small value derived ONLY from post-mapping buckets; a value in the hundreds means bench movement leaked into this animal's baseline", b)
	}

	// And the boundary is on the hot read path too, not only in the identifier table.
	var boundary *time.Time
	if err := pool.QueryRow(ctx, `SELECT animal_monitoring_since FROM herd_signal_tag_latest WHERE tenant_id = $1::uuid AND tag_id = $2`, hsiTenant, hsmTagA).Scan(&boundary); err != nil {
		t.Fatalf("read denormalised boundary: %v", err)
	}
	if boundary == nil {
		t.Fatal("animal_monitoring_since is NULL on the live row of a mapped tag; the read path would have to join to decide whether a number may be attributed to an animal")
	}

	// UNMAP ends the period: the baseline goes away again, because there is no animal.
	if _, err := repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsmTagA, TagMAC: hsmTagAMAC}); err != nil {
		t.Fatalf("unmap: %v", err)
	}
	baselines, err = repo.GetBaselineDeltas(ctx, hsiTenant, []string{hsmTagA})
	if err != nil {
		t.Fatalf("GetBaselineDeltas (after unmap): %v", err)
	}
	if _, ok := baselines[hsmTagA]; ok {
		t.Error("an unmapped tag still has a baseline; ending the monitoring period must end animal attribution")
	}
}

// TestGatewayHeartbeatAndPacketLossAccounting proves the two payload-audit findings: heartbeats
// are recorded (so "up but hearing no tags" is distinguishable from "down"), and pkt_sn accrues
// loss on a forward jump while a decrease is a REBOOT, never a negative.
func TestGatewayHeartbeatAndPacketLossAccounting(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)

	const gwID = "gw-hsm-hb"
	base := time.Now().UTC().Add(-10 * time.Minute)

	ingestWithPktSN := func(at time.Time, motion, pktSN int64) {
		t.Helper()
		p := makePacket(hsiTenant, hsmTagA, hsmTagAMAC, gwID, at, motion, -60)
		sn := pktSN
		p.PktSN = &sn
		gw := domain.Gateway{TenantID: hsiTenant, GatewayID: gwID, Status: "active", LastSeenAt: &at, LastPktSN: &sn}
		if _, _, err := repo.IngestPackets(ctx, hsiTenant, gw, []domain.Packet{p}); err != nil {
			t.Fatalf("ingest pkt_sn=%d: %v", pktSN, err)
		}
	}

	ingestWithPktSN(base, 10, 100)
	ingestWithPktSN(base.Add(time.Minute), 20, 101)   // contiguous: no loss
	ingestWithPktSN(base.Add(2*time.Minute), 30, 105) // jumped 4 ahead: 3 reports never arrived
	ingestWithPktSN(base.Add(3*time.Minute), 40, 2)   // DECREASED: the gateway rebooted

	var missed, reboots int64
	var lastSN *int64
	if err := pool.QueryRow(ctx, `
		SELECT packets_missed_total, pkt_sn_reboot_count, last_pkt_sn
		FROM herd_signal_gateways WHERE tenant_id = $1::uuid AND gateway_id = $2
	`, hsiTenant, gwID).Scan(&missed, &reboots, &lastSN); err != nil {
		t.Fatalf("read gateway loss counters: %v", err)
	}
	if missed != 3 {
		t.Errorf("packets_missed_total = %d, want 3 (105 - 101 - 1); this is the only packet-loss instrument the protocol gives us", missed)
	}
	if reboots != 1 {
		t.Errorf("pkt_sn_reboot_count = %d, want 1: a DECREASE is a gateway reboot, not negative loss", reboots)
	}
	if missed < 0 {
		t.Error("loss went negative; a reboot must re-anchor, never subtract")
	}

	// The sequence number is a real column now, not a crumb inside raw_payload jsonb.
	var storedSNs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM herd_signal_packets WHERE tenant_id = $1::uuid AND tag_id = $2 AND pkt_sn IS NOT NULL`, hsiTenant, hsmTagA).Scan(&storedSNs); err != nil {
		t.Fatalf("count stored pkt_sn: %v", err)
	}
	if storedSNs != 4 {
		t.Errorf("stored pkt_sn on %d packets, want 4", storedSNs)
	}

	// HEARTBEATS. A gateway with fresh heartbeats and no tag packets is "up but hearing
	// nothing" -- previously indistinguishable from "down", because these were discarded.
	hbAt := time.Now().UTC()
	ticks := int64(5000)
	rebooted, err := repo.RecordGatewayHeartbeat(ctx, hsiTenant, domain.GatewayHeartbeatRequest{
		GatewayID: gwID, State: domain.GatewayHeartbeatState, TicksCnt: &ticks,
	}, hbAt)
	if err != nil {
		t.Fatalf("RecordGatewayHeartbeat: %v", err)
	}
	if rebooted {
		t.Error("first heartbeat reported a reboot; there was no previous ticks_cnt to fall from")
	}

	lower := int64(12)
	rebooted, err = repo.RecordGatewayHeartbeat(ctx, hsiTenant, domain.GatewayHeartbeatRequest{
		GatewayID: gwID, State: domain.GatewayHeartbeatState, TicksCnt: &lower,
	}, hbAt.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("RecordGatewayHeartbeat (reboot): %v", err)
	}
	if !rebooted {
		t.Error("ticks_cnt fell from 5000 to 12 and no reboot was reported; that is exactly the reboot signal")
	}

	var lastHeartbeat *time.Time
	var hbReboots int
	if err := pool.QueryRow(ctx, `
		SELECT last_heartbeat_at, heartbeat_reboot_count
		FROM herd_signal_gateways WHERE tenant_id = $1::uuid AND gateway_id = $2
	`, hsiTenant, gwID).Scan(&lastHeartbeat, &hbReboots); err != nil {
		t.Fatalf("read heartbeat columns: %v", err)
	}
	if lastHeartbeat == nil {
		t.Fatal("last_heartbeat_at is NULL after two heartbeats; without it a silent gateway and a deaf one look identical")
	}
	if hbReboots != 1 {
		t.Errorf("heartbeat_reboot_count = %d, want 1", hbReboots)
	}
}

// TestOrdinaryRetaggingRoundTripNeedsNoSQL walks the exact sequence a farm performs and that the
// product could not complete: MAP A -> UNMAP A -> MAP A again -> REPLACE A with B -> UNMAP B.
//
// Every step must succeed with no hand-written SQL in between. Before the release fix, step 3
// was refused with mapping_conflict and step 4 with "held by a retired identifier ... must be
// reactivated through the identity module" -- and REPLACE's success path had therefore never once
// been observed end to end, by any test or by anyone driving the API.
func TestOrdinaryRetaggingRoundTripNeedsNoSQL(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)
	seedSecondGoat(t, ctx, pool)

	step := func(n int, what string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("step %d (%s) failed: %v\nordinary re-tagging must need no manual intervention", n, what, err)
		}
	}

	first, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC})
	step(1, "MAP A", err)

	_, err = repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsmTagA})
	step(2, "UNMAP A", err)
	if rows := identifierRowsForValues(t, ctx, pool, hsmTagA, hsmTagAMAC); len(rows) != 0 {
		t.Fatalf("after step 2 these rows survive: %v -- they are what refuses step 3", rows)
	}

	again, err := repo.BindTagMapping(ctx, hsiTenant, domain.BindTagMappingRequest{GoatID: hsmGoatB, TagID: hsmTagA, TagMAC: hsmTagAMAC})
	step(3, "MAP A again to the same animal", err)
	if again.MonitoringSince == nil || !again.MonitoringSince.After(*first.MonitoringSince) {
		t.Errorf("step 3 monitoring_since = %v, want a NEW period after %v", again.MonitoringSince, first.MonitoringSince)
	}

	_, err = repo.ReplaceTagMapping(ctx, hsiTenant, domain.ReplaceTagMappingRequest{GoatID: hsmGoatB, NewTagID: hsmTagB, NewTagMAC: hsmTagBMAC})
	step(4, "REPLACE A with B", err)
	if rows := identifierRowsForValues(t, ctx, pool, hsmTagA, hsmTagAMAC); len(rows) != 0 {
		t.Fatalf("after step 4 the OLD tag's rows survive: %v -- an animal would accumulate a dead binding on every re-tag", rows)
	}
	if live := liveSmartTagValues(t, ctx, pool, hsmGoatB); len(live) != 2 {
		t.Fatalf("after step 4 the animal carries %v, want exactly the new tag's 2 values", live)
	}

	_, err = repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsmTagB})
	step(5, "UNMAP B", err)
	if rows := identifierRowsForValues(t, ctx, pool, hsmTagA, hsmTagAMAC, hsmTagB, hsmTagBMAC); len(rows) != 0 {
		t.Fatalf("after the full round trip these rows survive: %v -- want a clean slate", rows)
	}
	if live := liveSmartTagValues(t, ctx, pool, hsmGoatB); len(live) != 0 {
		t.Fatalf("after the full round trip the animal still carries %v", live)
	}
}

// TestReleaseNeverDestroysTheAnimalsOwnIdentity is the guard on the destructive direction of the
// same decision.
//
// Release DELETES rows this module invented to carry a binding. It must never delete -- or
// retire -- a pre-existing identity row that a bind merely FLAGGED as smart-tag capable. That row
// is the animal's real ear tag, and this module does not own it. Getting this backwards would
// silently destroy identity every time an operator unmapped a tag.
func TestReleaseNeverDestroysTheAnimalsOwnIdentity(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupMappingDB(t, ctx)

	// hsiMappedTag is seeded as the fixture animal's OWN animal_identifier_1 -- created outside
	// this module, exactly like a real ear tag imported by the identity module.
	var beforeStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM goat_identifiers WHERE tenant_id = $1::uuid AND normalized_value = $2`,
		hsiTenant, domain.NormalizeTagIdentifier(hsiMappedTag)).Scan(&beforeStatus); err != nil {
		t.Fatalf("read the fixture identity row: %v", err)
	}

	if _, err := repo.UnmapTagMapping(ctx, hsiTenant, domain.UnmapTagMappingRequest{TagID: hsiMappedTag}); err != nil {
		t.Fatalf("unmap the animal's own ear-tag identifier: %v", err)
	}

	var afterStatus string
	var capable *bool
	var mappedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT status, smart_tag_capable, smart_tag_mapped_at FROM goat_identifiers WHERE tenant_id = $1::uuid AND normalized_value = $2`,
		hsiTenant, domain.NormalizeTagIdentifier(hsiMappedTag)).Scan(&afterStatus, &capable, &mappedAt); err != nil {
		t.Fatalf("the animal's OWN identity row was DELETED by an unmap: %v\nthis module flags and unflags identity it did not create -- it must never destroy it", err)
	}
	if afterStatus != beforeStatus {
		t.Errorf("the animal's own identity row changed status %q -> %q on unmap; releasing a BLE binding must not retire an ear tag", beforeStatus, afterStatus)
	}
	if capable == nil || *capable {
		t.Errorf("smart_tag_capable = %v after unmap, want false: the binding ended", capable)
	}
	if mappedAt != nil {
		t.Errorf("smart_tag_mapped_at = %v after unmap, want NULL: no animal-attributed value may be produced for an unmapped tag", mappedAt)
	}
}
