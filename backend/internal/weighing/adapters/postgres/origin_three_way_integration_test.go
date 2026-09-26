package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// THREE ORIGIN COHORTS (maintainer decision 2026-09-26): Farm born, Procured (no load), Procured
// (load). The defect these pin: the two-way filter answered "farm born" with "carries no load
// row", so every animal the register marks PROCURED but that sits on no recorded load -- 659 of
// the live herd on STG that day -- was filed as farm born. Each test below fails on the two-way
// resolver.

var originWindowFrom = time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
var originWindowTo = time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)

// seedOriginGoat inserts one alive-or-not animal with an explicit origin_type ("" = NULL) and,
// when tag is non-empty, its RFID.
func seedOriginGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, goatID, displayID, shedID, originType, lifecycle, tag string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, breed, sex, age_band, lifecycle_status, management_stage, custodian_party_id, current_location_id, park_id, shed_id, origin_type)
VALUES ($1::uuid, $2::uuid, $3, 'Beetal', 'male', 'kid', $8, 'kid', $4::uuid, $5::uuid, $6::uuid, $5::uuid, NULLIF($7, ''))
ON CONFLICT (goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, origin_type = EXCLUDED.origin_type, lifecycle_status = EXCLUDED.lifecycle_status`,
		goatID, repoTenant, displayID, repoParty, shedID, repoPark, originType, lifecycle)
	if tag == "" {
		return
	}
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO goat_identifiers (tenant_id, goat_id, identifier_type, identifier_value, normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
VALUES ($1::uuid, $2::uuid, 'animal_identifier_1', $3, $3, 'global', true, 'active', now(), 'test')
ON CONFLICT (tenant_id, normalized_value) DO UPDATE SET goat_id = EXCLUDED.goat_id`, repoTenant, goatID, tag)
}

func seedOriginShed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, shedID, name string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
VALUES ($1::uuid, $2::uuid, $3, 'shed', $4::uuid, 'active')
ON CONFLICT (location_id) DO NOTHING`, shedID, repoTenant, name, repoPark)
}

func seedOriginLoad(t *testing.T, ctx context.Context, pool *pgxpool.Pool, loadID, key string, goatIDs ...string) {
	t.Helper()
	execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_loads (load_id, tenant_id, source_party_id, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3::uuid, $4)
ON CONFLICT (load_id) DO NOTHING`, loadID, repoTenant, repoParty, key)
	for _, goatID := range goatIDs {
		execWeighingTestSQL(t, ctx, pool, `
INSERT INTO procurement_load_goats (load_goat_id, tenant_id, load_id, goat_id)
VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid)`, repoTenant, loadID, goatID)
	}
}

func resolveAllOrigins(t *testing.T, ctx context.Context, repo *Repository, parkIDs []string) map[string]ReportScope {
	t.Helper()
	out := map[string]ReportScope{}
	for _, origin := range []string{OriginFarmBorn, OriginProcuredNoLoad, OriginProcuredLoad} {
		scope, err := repo.resolveOriginScope(ctx, repoTenant, parkIDs, origin, originWindowFrom, originWindowTo)
		if err != nil {
			t.Fatalf("resolveOriginScope(%s): %v", origin, err)
		}
		out[origin] = scope
	}
	return out
}

// claimedBy lists which cohorts claim a tag (tag != "") or a whole-shed pen (tag == "").
func claimedBy(scopes map[string]ReportScope, tag, shedID string) []string {
	var out []string
	for _, origin := range []string{OriginFarmBorn, OriginProcuredNoLoad, OriginProcuredLoad} {
		scope := scopes[origin]
		if (tag != "" && hasTag(scope, tag)) || (tag == "" && originScopeClaimsBucket(scope, shedID)) {
			out = append(out, origin)
		}
	}
	return out
}

func originScopeClaimsBucket(scope ReportScope, locationID string) bool {
	for _, id := range scope.LocationIDs {
		if id == locationID {
			return true
		}
	}
	return false
}

func wantOnly(t *testing.T, what string, got []string, want string) {
	t.Helper()
	if want == "" {
		if len(got) != 0 {
			t.Fatalf("%s must be claimed by NO cohort, got %v", what, got)
		}
		return
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("%s must be claimed by exactly %s, got %v", what, want, got)
	}
}

// Procured without a load is its own cohort, on both arms; no recorded origin is none of the three.
func TestOriginScopeKeepsProcuredWithoutALoadOutOfFarmBorn(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		noLoadShed  = "00000000-0000-4000-8000-00000009c0a1"
		blankShed   = "00000000-0000-4000-8000-00000009c0a2"
		scanShed    = "00000000-0000-4000-8000-00000009c0a3"
		noLoadPen   = "00000000-0000-4000-8000-00000009c0b1"
		blankPen    = "00000000-0000-4000-8000-00000009c0b2"
		scanBucket  = "00000000-0000-4000-8000-00000009c0b3"
		lumpCampaig = "00000000-0000-4000-8000-00000009c0b4"
	)
	seedOriginShed(t, ctx, pool, noLoadShed, "Deneb North")
	seedOriginShed(t, ctx, pool, blankShed, "Deneb South")
	seedOriginShed(t, ctx, pool, scanShed, "Deneb East")
	// A whole pen bought without a load, and a whole pen whose origin nobody recorded.
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c1a1", "G-970001", noLoadShed, "procured", "alive", "")
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c1a2", "G-970002", noLoadShed, "procured", "alive", "")
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c1a3", "G-970003", blankShed, "", "alive", "")
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c1a4", "G-970004", blankShed, "", "alive", "")
	// Three scanned kids in one pen, one per register answer.
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c1a5", "G-970005", scanShed, "birth", "alive", "born-kid")
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c1a6", "G-970006", scanShed, "procured", "alive", "noload-kid")
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c1a7", "G-970007", scanShed, "", "alive", "blank-kid")

	seedShedWeightsCampaign(t, ctx, pool, lumpCampaig, "2026-07-22")
	seedLoadBucket(t, ctx, pool, noLoadPen, lumpCampaig, noLoadShed, "per_shed_partition")
	seedLoadBucket(t, ctx, pool, blankPen, lumpCampaig, blankShed, "per_shed_partition")
	seedLoadBucket(t, ctx, pool, scanBucket, repoCampaign, scanShed, "individual_animal")
	for _, tag := range []string{"born-kid", "noload-kid", "blank-kid"} {
		seedOriginScan(t, ctx, pool, scanBucket, tag, 20)
	}

	scopes := resolveAllOrigins(t, ctx, repo, []string{repoPark})
	wantOnly(t, "a kid born here", claimedBy(scopes, "born-kid", ""), OriginFarmBorn)
	// THE DEFECT: the two-way resolver put this kid under Farm born.
	wantOnly(t, "a kid bought without a load", claimedBy(scopes, "noload-kid", ""), OriginProcuredNoLoad)
	wantOnly(t, "a kid with no recorded origin", claimedBy(scopes, "blank-kid", ""), "")
	wantOnly(t, "a pen bought without a load", claimedBy(scopes, "", noLoadShed), OriginProcuredNoLoad)
	wantOnly(t, "a pen with no recorded origin", claimedBy(scopes, "", blankShed), "")
}

// OneToMany: an animal on two load lines is ONE procured-load animal, and the load wins even over
// a register that says the animal was born here.
func TestOriginScopeOneToManyLoadLinesWinOverARecordedBirth(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		shed   = "00000000-0000-4000-8000-00000009c2a1"
		bucket = "00000000-0000-4000-8000-00000009c2b1"
		goat   = "00000000-0000-4000-8000-00000009c2c1"
	)
	seedOriginShed(t, ctx, pool, shed, "Mira North")
	seedOriginGoat(t, ctx, pool, goat, "G-972001", shed, "birth", "alive", "twice-bought")
	seedOriginLoad(t, ctx, pool, "00000000-0000-4000-8000-00000009c2d1", "origin3:load:a", goat)
	seedOriginLoad(t, ctx, pool, "00000000-0000-4000-8000-00000009c2d2", "origin3:load:b", goat)
	seedLoadBucket(t, ctx, pool, bucket, repoCampaign, shed, "individual_animal")
	seedOriginScan(t, ctx, pool, bucket, "twice-bought", 21)

	scopes := resolveAllOrigins(t, ctx, repo, []string{repoPark})
	wantOnly(t, "an animal on two loads", claimedBy(scopes, "twice-bought", ""), OriginProcuredLoad)
	if n := len(scopes[OriginProcuredLoad].Tags); n != 1 {
		t.Fatalf("two load lines must not fan one animal out into %d tags", n)
	}
}

// ParkScope: a cohort is resolved inside the parks asked for and nowhere else.
func TestOriginScopeParkScopeReadsOnlyTheSelectedParks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		shed   = "00000000-0000-4000-8000-00000009c3a1"
		bucket = "00000000-0000-4000-8000-00000009c3b1"
		other  = "00000000-0000-4000-8000-00000009c3ff"
	)
	seedOriginShed(t, ctx, pool, shed, "Castor North")
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c3c1", "G-973001", shed, "procured", "alive", "park-kid")
	seedLoadBucket(t, ctx, pool, bucket, repoCampaign, shed, "individual_animal")
	seedOriginScan(t, ctx, pool, bucket, "park-kid", 19)

	inPark := resolveAllOrigins(t, ctx, repo, []string{repoPark})
	wantOnly(t, "the kid in the selected park", claimedBy(inPark, "park-kid", ""), OriginProcuredNoLoad)
	elsewhere := resolveAllOrigins(t, ctx, repo, []string{other})
	for origin, scope := range elsewhere {
		if len(scope.Tags) != 0 || len(scope.LocationIDs) != 0 {
			t.Fatalf("another park's %s scope must be empty, got %+v", origin, scope)
		}
	}
}

// EveryStatus: only ALIVE residents decide a whole pen. One resident in each other lifecycle
// status from goats_lifecycle_status_check, each marked procured, must not pull a born-here pen
// out of Farm born.
func TestOriginScopeEveryStatusOnlyAliveResidentsDecideAPen(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	repo := NewRepository(pool, 5*time.Second)

	const (
		shed     = "00000000-0000-4000-8000-00000009c4a1"
		bucket   = "00000000-0000-4000-8000-00000009c4b1"
		campaign = "00000000-0000-4000-8000-00000009c4b2"
	)
	seedOriginShed(t, ctx, pool, shed, "Pollux North")
	seedOriginGoat(t, ctx, pool, "00000000-0000-4000-8000-00000009c4c0", "G-974000", shed, "birth", "alive", "")
	statuses := []string{"sick", "under_treatment", "quarantine", "icu", "dead", "sold", "culled", "transferred", "lost", "merged", "inactive"}
	for i, status := range statuses {
		seedOriginGoat(t, ctx, pool, fmt.Sprintf("00000000-0000-4000-8000-00000009c4%02x", 0xd0+i), fmt.Sprintf("G-9741%02d", i), shed, "procured", status, "")
	}
	seedShedWeightsCampaign(t, ctx, pool, campaign, "2026-07-22")
	seedLoadBucket(t, ctx, pool, bucket, campaign, shed, "per_shed_partition")

	scopes := resolveAllOrigins(t, ctx, repo, []string{repoPark})
	wantOnly(t, "a pen whose only alive resident was born here", claimedBy(scopes, "", shed), OriginFarmBorn)
}
