package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

func TestSeedGoatUpsertRefreshesGenerationFactsOnRerun(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	goatID := detUUID("goat", defaultTenantID, "rerun-source-correction")
	shedID := detUUID("shed", defaultTenantID, "rerun-source-correction")
	correctedCustodianID := detUUID("party", defaultTenantID, "rerun-source-correction-custodian")
	mustExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-rerun-test','active') ON CONFLICT (tenant_id) DO NOTHING`, defaultTenantID)
	mustExec(t, ctx, pool, `
		INSERT INTO parties (party_id, party_type, display_name, status)
		VALUES ($1,'org','Corrected Custodian','active')
		ON CONFLICT (party_id) DO UPDATE SET display_name=EXCLUDED.display_name, status='active', updated_at=now()`, correctedCustodianID)
	mustExec(t, ctx, pool, `
		INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
			parent_location_id, country, state_region, timezone, status, updated_at)
		VALUES ($1,$2,'shed','SEED-RERUN-SHED','Seed Rerun Shed',$3,'IN','Tamil Nadu','Asia/Kolkata','active',now())
		ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`,
		shedID, defaultTenantID, testCbePark)

	upsert := func(row seedGoatUpsertRow, custodianID string) {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin goat upsert: %v", err)
		}
		committed := false
		defer func() {
			if !committed {
				_ = tx.Rollback(ctx)
			}
		}()
		if err := upsertSeedGoats(ctx, tx, defaultTenantID, []seedGoatUpsertRow{row}, custodianID); err != nil {
			t.Fatalf("upsert goat: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit goat upsert: %v", err)
		}
		committed = true
	}

	base := seedGoatUpsertRow{
		goatID:    goatID,
		animalKey: "rerun-source-correction",
		species:   "goat",
		breed:     "Sojat",
		sex:       "female",
		lifecycle: "alive",
		stage:     "Adult",
		age:       "Adult",
		shedID:    shedID,
		parkID:    testCbePark,
	}
	upsert(seedGoatUpsertRow{
		goatID:     base.goatID,
		animalKey:  base.animalKey,
		species:    base.species,
		breed:      base.breed,
		sex:        base.sex,
		lifecycle:  base.lifecycle,
		originType: "procured",
		stage:      base.stage,
		age:        base.age,
		shedID:     base.shedID,
		parkID:     base.parkID,
		dob:        "2026-01-01",
		entryDate:  "2026-01-15",
	}, testMeshaParty)

	repo := vaccinationpg.NewRepository(pool, 5*time.Second)
	asOf := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	first, ok, err := repo.GetGoatForGeneration(ctx, defaultTenantID, goatID)
	if err != nil || !ok {
		t.Fatalf("get first generation goat: ok=%v err=%v", ok, err)
	}
	if got := vaccinationapp.SchedulePathForGoat(first, vaccinationapp.SchedulePathProcurementPolicy{
		KidsNormalScheduleUntilWeeks: seedKidsNormalScheduleUntilWeeks,
	}, asOf, nil); got != vaccinationapp.SchedulePathAdultProcurement {
		t.Fatalf("first generation path = %q, want adult procurement", got)
	}

	upsert(seedGoatUpsertRow{
		goatID:     base.goatID,
		animalKey:  base.animalKey,
		species:    base.species,
		breed:      base.breed,
		sex:        base.sex,
		lifecycle:  base.lifecycle,
		originType: "birth",
		stage:      "K1",
		age:        "Kid",
		shedID:     base.shedID,
		parkID:     base.parkID,
		dob:        "2026-05-01",
		entryDate:  "2026-05-01",
	}, correctedCustodianID)

	second, ok, err := repo.GetGoatForGeneration(ctx, defaultTenantID, goatID)
	if err != nil || !ok {
		t.Fatalf("get corrected generation goat: ok=%v err=%v", ok, err)
	}
	if second.OriginType != "birth" {
		t.Fatalf("corrected origin_type = %q, want birth", second.OriginType)
	}
	if second.DOB == nil || second.DOB.Format("2006-01-02") != "2026-05-01" {
		t.Fatalf("corrected DOB = %v, want 2026-05-01", second.DOB)
	}
	if got := vaccinationapp.SchedulePathForGoat(second, vaccinationapp.SchedulePathProcurementPolicy{
		KidsNormalScheduleUntilWeeks: seedKidsNormalScheduleUntilWeeks,
	}, asOf, nil); got != vaccinationapp.SchedulePathKid {
		t.Fatalf("corrected generation path = %q, want kid", got)
	}
	var gotCustodian string
	if err := pool.QueryRow(ctx, `SELECT custodian_party_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, defaultTenantID, goatID).Scan(&gotCustodian); err != nil {
		t.Fatalf("read custodian: %v", err)
	}
	if gotCustodian != correctedCustodianID {
		t.Fatalf("custodian_party_id = %q, want %q", gotCustodian, correctedCustodianID)
	}
}

// testCptPark is the second migration-seeded baseline park (Channapatna), distinct
// from testCbePark (Coimbatore) declared in seed_ledger_integration_test.go. Used
// only to prove a real park-to-park change, never a fixture the seed itself creates.
const testCptPark = "00000000-0000-4000-8000-000000003002"

// TestSeedGoatUpsertRejectsCrossParkChangeOnRerun is FIX PEND-5: goats never change
// park (maintainer decision 2026-07-19; runtime equivalent identity/
// ports.ErrCrossParkMove). upsertSeedGoats' ON CONFLICT (goat_id) DO UPDATE ...
// park_id=EXCLUDED.park_id previously had no comparison at all, so a reseed with a
// corrected/different source park silently moved an already-placed goat. The fix
// (checkNoCrossParkMoves) must reject the WHOLE upsert batch before any row is
// written when an existing, non-null park_id would change.
func TestSeedGoatUpsertRejectsCrossParkChangeOnRerun(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	goatID := detUUID("goat", defaultTenantID, "cross-park-reject")
	shedID := detUUID("shed", defaultTenantID, "cross-park-reject")
	mustExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-cross-park-test','active') ON CONFLICT (tenant_id) DO NOTHING`, defaultTenantID)
	mustExec(t, ctx, pool, `
		INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
			parent_location_id, country, state_region, timezone, status, updated_at)
		VALUES ($1,$2,'shed','SEED-CROSS-PARK-SHED','Seed Cross Park Shed',$3,'IN','Tamil Nadu','Asia/Kolkata','active',now())
		ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`,
		shedID, defaultTenantID, testCbePark)

	row := seedGoatUpsertRow{
		goatID:    goatID,
		animalKey: "cross-park-reject",
		species:   "goat",
		breed:     "Sojat",
		sex:       "female",
		lifecycle: "alive",
		stage:     "Adult",
		age:       "Adult",
		shedID:    shedID,
		parkID:    testCbePark,
		dob:       "2026-01-01",
		entryDate: "2026-01-15",
	}

	// First upsert: initial placement in testCbePark. Must succeed and commit.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin first upsert: %v", err)
	}
	if err := upsertSeedGoats(ctx, tx, defaultTenantID, []seedGoatUpsertRow{row}, testMeshaParty); err != nil {
		t.Fatalf("first upsert (initial placement) should succeed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit first upsert: %v", err)
	}

	// Second upsert: same goat, DIFFERENT park (testCptPark). This must be rejected
	// before the batch ever runs, and the transaction must be rolled back by the
	// caller (mirroring how seed()'s deferred rollback fires on any error return).
	movedRow := row
	movedRow.parkID = testCptPark
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin second upsert: %v", err)
	}
	err = upsertSeedGoats(ctx, tx2, defaultTenantID, []seedGoatUpsertRow{movedRow}, testMeshaParty)
	if err == nil {
		t.Fatal("cross-park move upsert = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "cross-park") {
		t.Fatalf("cross-park move error = %v, want a cross-park rejection message", err)
	}
	if rerr := tx2.Rollback(ctx); rerr != nil {
		t.Fatalf("rollback rejected upsert: %v", rerr)
	}

	var gotParkID string
	if err := pool.QueryRow(ctx, `SELECT park_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, defaultTenantID, goatID).Scan(&gotParkID); err != nil {
		t.Fatalf("read park_id: %v", err)
	}
	if gotParkID != testCbePark {
		t.Fatalf("park_id after rejected cross-park upsert = %q, want unchanged %q", gotParkID, testCbePark)
	}
}

// TestSeedGoatUpsertAllowsInitialPlacementWhenExistingParkIsNull is the exemption
// companion to the cross-park rejection above: a goat row with NO prior park (an
// existing NULL park_id -- e.g. a terminal-exit placeholder, or a row created
// before placement completed) is an initial placement, not a move, and must
// succeed.
func TestSeedGoatUpsertAllowsInitialPlacementWhenExistingParkIsNull(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	goatID := detUUID("goat", defaultTenantID, "cross-park-null-exempt")
	shedID := detUUID("shed", defaultTenantID, "cross-park-null-exempt")
	mustExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-cross-park-null-test','active') ON CONFLICT (tenant_id) DO NOTHING`, defaultTenantID)
	mustExec(t, ctx, pool, `
		INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
			parent_location_id, country, state_region, timezone, status, updated_at)
		VALUES ($1,$2,'shed','SEED-CROSS-PARK-NULL-SHED','Seed Cross Park Null Shed',$3,'IN','Tamil Nadu','Asia/Kolkata','active',now())
		ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`,
		shedID, defaultTenantID, testCbePark)

	// Pre-existing goat row with park_id NULL -- not yet placed / a terminal exit.
	mustExec(t, ctx, pool, `
		INSERT INTO goats (goat_id, tenant_id, custodian_party_id, sex, lifecycle_status, park_id, shed_id, current_location_id)
		VALUES ($1,$2,$3,'female','alive',NULL,NULL,NULL)
		ON CONFLICT (goat_id) DO UPDATE SET park_id=NULL, shed_id=NULL, current_location_id=NULL`,
		goatID, defaultTenantID, testMeshaParty)

	row := seedGoatUpsertRow{
		goatID:    goatID,
		animalKey: "cross-park-null-exempt",
		species:   "goat",
		breed:     "Sojat",
		sex:       "female",
		lifecycle: "alive",
		stage:     "Adult",
		age:       "Adult",
		shedID:    shedID,
		parkID:    testCbePark,
		dob:       "2026-01-01",
		entryDate: "2026-01-15",
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin upsert: %v", err)
	}
	if err := upsertSeedGoats(ctx, tx, defaultTenantID, []seedGoatUpsertRow{row}, testMeshaParty); err != nil {
		t.Fatalf("initial placement from NULL park should succeed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var gotParkID string
	if err := pool.QueryRow(ctx, `SELECT park_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, defaultTenantID, goatID).Scan(&gotParkID); err != nil {
		t.Fatalf("read park_id: %v", err)
	}
	if gotParkID != testCbePark {
		t.Fatalf("park_id after initial placement = %q, want %q", gotParkID, testCbePark)
	}
}

// TestSeedGoatUpsertAllowsIntraParkShedMove is the shed-move companion to
// TestSeedGoatUpsertRejectsCrossParkChangeOnRerun: checkNoCrossParkMoves
// compares ONLY park_id, so a goat that stays in the SAME park but is
// reassigned to a DIFFERENT shed within that park (an ordinary in-park
// pen/shed reassignment -- per the maintainer's 2026-07-19 movement rule that
// shed moves exist only within one park) must NOT be rejected as a cross-park
// move.
func TestSeedGoatUpsertAllowsIntraParkShedMove(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	goatID := detUUID("goat", defaultTenantID, "intra-park-shed-move")
	shedAID := detUUID("shed", defaultTenantID, "intra-park-shed-move-a")
	shedBID := detUUID("shed", defaultTenantID, "intra-park-shed-move-b")
	mustExec(t, ctx, pool, `INSERT INTO tenants (tenant_id, name, status) VALUES ($1,'seed-intra-park-shed-move-test','active') ON CONFLICT (tenant_id) DO NOTHING`, defaultTenantID)
	mustExec(t, ctx, pool, `
		INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
			parent_location_id, country, state_region, timezone, status, updated_at)
		VALUES ($1,$2,'shed','SEED-INTRA-PARK-SHED-A','Seed Intra Park Shed A',$3,'IN','Tamil Nadu','Asia/Kolkata','active',now())
		ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`,
		shedAID, defaultTenantID, testCbePark)
	mustExec(t, ctx, pool, `
		INSERT INTO locations (location_id, tenant_id, location_type, location_code, name,
			parent_location_id, country, state_region, timezone, status, updated_at)
		VALUES ($1,$2,'shed','SEED-INTRA-PARK-SHED-B','Seed Intra Park Shed B',$3,'IN','Tamil Nadu','Asia/Kolkata','active',now())
		ON CONFLICT (tenant_id, location_code) DO UPDATE SET status='active', updated_at=now()`,
		shedBID, defaultTenantID, testCbePark)

	row := seedGoatUpsertRow{
		goatID:    goatID,
		animalKey: "intra-park-shed-move",
		species:   "goat",
		breed:     "Sojat",
		sex:       "female",
		lifecycle: "alive",
		stage:     "Adult",
		age:       "Adult",
		shedID:    shedAID,
		parkID:    testCbePark,
		dob:       "2026-01-01",
		entryDate: "2026-01-15",
	}

	// First upsert: initial placement in shed A, park testCbePark. Must succeed and commit.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin first upsert: %v", err)
	}
	if err := upsertSeedGoats(ctx, tx, defaultTenantID, []seedGoatUpsertRow{row}, testMeshaParty); err != nil {
		t.Fatalf("first upsert (initial placement) should succeed: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit first upsert: %v", err)
	}

	// Second upsert: same goat, SAME park (testCbePark), DIFFERENT shed (shed B).
	// Must succeed -- checkNoCrossParkMoves only compares park_id, and this is not
	// a cross-park move.
	movedRow := row
	movedRow.shedID = shedBID
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin second upsert: %v", err)
	}
	if err := upsertSeedGoats(ctx, tx2, defaultTenantID, []seedGoatUpsertRow{movedRow}, testMeshaParty); err != nil {
		t.Fatalf("intra-park shed move upsert should succeed, got error: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit second upsert: %v", err)
	}

	var gotParkID, gotShedID string
	if err := pool.QueryRow(ctx, `SELECT park_id::text, shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, defaultTenantID, goatID).Scan(&gotParkID, &gotShedID); err != nil {
		t.Fatalf("read park_id/shed_id: %v", err)
	}
	if gotParkID != testCbePark {
		t.Fatalf("park_id after intra-park shed move = %q, want unchanged %q", gotParkID, testCbePark)
	}
	if gotShedID != shedBID {
		t.Fatalf("shed_id after intra-park shed move = %q, want %q", gotShedID, shedBID)
	}
}
