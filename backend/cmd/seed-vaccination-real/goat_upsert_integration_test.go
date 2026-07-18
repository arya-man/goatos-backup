package main

import (
	"context"
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
