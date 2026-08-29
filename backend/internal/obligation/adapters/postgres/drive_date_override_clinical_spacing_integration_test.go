package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
)

func TestDriveDateOverrideAutoShiftsLiveVaccineNearOverlappingLiveObligation(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.clinical.override", 2)
	pprVersion, poxVersion := versions[0], versions[1]

	const (
		goatID    = "10000000-0000-4000-8000-00000000fc01"
		shedID    = "00000000-0000-4000-8000-00000000dc01"
		operatorA = "20000000-0000-4000-8000-000000000c01"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "clinical-spacing-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code,
  vaccine_type, pathogen_class, min_gap_days, vaccine_json
) VALUES
  ($1, $4, $2, 'vaccination', 'clinical-ppr', 'ppr_adult_w1', 'PPR',
   'live', 'viral', 0, '{"course_type":"single"}'::jsonb),
  ($1, $5, $3, 'vaccination', 'clinical-sheep-pox', 'sheep_pox_adult_w1', 'SHEEP_POX',
   'live', 'viral', 0, '{"course_type":"single"}'::jsonb)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code,
  vaccine_type = EXCLUDED.vaccine_type,
  pathogen_class = EXCLUDED.pathogen_class,
  min_gap_days = EXCLUDED.min_gap_days,
  vaccine_json = EXCLUDED.vaccine_json`,
		tenantID, pprVersion.ruleID, poxVersion.ruleID, pprVersion.versionID, poxVersion.versionID); err != nil {
		t.Fatalf("seed clinical rule metadata: %v", err)
	}

	source := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	requested := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	conflictDate := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	wantApplied := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'CSP-A', 'Clinical Spacing Operator A', 'active', 'operator', $3);
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($2, 'clinical_spacing_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active');
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($2, $1, 'center', $3, 'clinical_spacing_operator_a', 'manager', 'friday', 50, 'active', '2026-01-01')`,
		pgx.QueryExecModeSimpleProtocol, operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	pprObligation, pprBatch := seedShotOnDate(t, ctx, repo, pprVersion, goatID, "shed", shedID, source, source, "clinical-ppr")
	seedShotOnDate(t, ctx, repo, poxVersion, goatID, "shed", shedID, conflictDate, conflictDate, "clinical-sheep-pox")
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: pprBatch, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Clinical Shed", PartitionLabel: "1", AnimalCount: 1,
		VaccineRuleIDs: []string{pprVersion.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed PPR assignment: %v", err)
	}

	out, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: requested, Reason: "move near sheep pox", CreatedBy: operatorA,
	})
	if err != nil {
		t.Fatalf("UpsertVaccinationDriveDateOverride: %v", err)
	}
	if !out.AutoShifted || out.ShiftReason != "clinical_spacing_auto_shift" {
		t.Fatalf("shift metadata = shifted:%v reason:%q", out.AutoShifted, out.ShiftReason)
	}
	if !out.RequestedOverrideDate.Equal(requested) || !out.OverrideDate.Equal(wantApplied) {
		t.Fatalf("requested/applied = %s/%s, want %s/%s", out.RequestedOverrideDate, out.OverrideDate, requested, wantApplied)
	}
	if out.ConflictVaccineCode != "SHEEP_POX" || out.ConflictRule != "live_live_min_gap" {
		t.Fatalf("conflict metadata = vaccine:%q rule:%q", out.ConflictVaccineCode, out.ConflictRule)
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, wantApplied, pprVersion.ruleID); len(got) == 0 {
		t.Fatalf("moved PPR assignment for obligation %s did not land on applied safe date %s", pprObligation, wantApplied.Format("2006-01-02"))
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, requested, pprVersion.ruleID); len(got) != 0 {
		t.Fatalf("moved PPR assignment landed on unsafe requested date: %+v", got)
	}
}

func TestDriveDateOverrideOverflowUsesOnlyClinicallySafeDates(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.clinical.overflow", 2)
	pprVersion, blueTongueVersion := versions[0], versions[1]

	const (
		goatA     = "10000000-0000-4000-8000-00000000fc11"
		goatB     = "10000000-0000-4000-8000-00000000fc12"
		shedID    = "00000000-0000-4000-8000-00000000dc11"
		operatorA = "20000000-0000-4000-8000-000000000c11"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "clinical-overflow-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatA, goatB)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code,
  vaccine_type, pathogen_class, min_gap_days, vaccine_json
) VALUES
  ($1, $4, $2, 'vaccination', 'clinical-overflow-ppr', 'ppr_adult_w1', 'PPR',
   'live', 'viral', 0, '{"course_type":"single"}'::jsonb),
  ($1, $5, $3, 'vaccination', 'clinical-overflow-bt', 'blue_tongue_adult_w1', 'BLUE_TONGUE',
   'killed', 'viral', 0, '{"course_type":"booster"}'::jsonb)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code,
  vaccine_type = EXCLUDED.vaccine_type,
  pathogen_class = EXCLUDED.pathogen_class,
  min_gap_days = EXCLUDED.min_gap_days,
  vaccine_json = EXCLUDED.vaccine_json`,
		tenantID, pprVersion.ruleID, blueTongueVersion.ruleID, pprVersion.versionID, blueTongueVersion.versionID); err != nil {
		t.Fatalf("seed clinical rule metadata: %v", err)
	}

	source := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	requested := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	wantOverflow := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'CSO-A', 'Clinical Spill Operator A', 'active', 'operator', $3);
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($2, 'clinical_spill_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active');
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($2, $1, 'center', $3, 'clinical_spill_operator_a', 'manager', 'friday', 1, 'active', '2026-01-01')`,
		pgx.QueryExecModeSimpleProtocol, operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	pprObligations := make([]string, 0, 2)
	for _, row := range []struct {
		goat string
		key  string
	}{
		{goatA, "clinical-overflow-ppr-a"},
		{goatB, "clinical-overflow-ppr-b"},
	} {
		oblID, applied, err := repo.InsertObligation(ctx, domain.NewObligation{
			TenantID: tenantID, ProtocolVersionID: pprVersion.versionID, RuleID: pprVersion.ruleID,
			TargetType: "goat", TargetID: row.goat, ScopeType: "shed", ScopeID: shedID,
			DueAt: source, Status: "scheduled", IdempotencyKey: row.key, Sequence: 1,
		})
		if err != nil || !applied {
			t.Fatalf("seed PPR %s: applied=%v err=%v", row.key, applied, err)
		}
		pprObligations = append(pprObligations, oblID)
		seedShotOnDate(t, ctx, repo, blueTongueVersion, row.goat, "shed", shedID, requested, requested, row.key+"-bt")
	}
	pd := source
	pprBatch, attached, err := repo.CreateBatchWithObligations(ctx, domain.NewBatch{
		TenantID: tenantID, ProtocolVersionID: pprVersion.versionID, ScopeType: "shed", ScopeID: shedID,
		Session: "clinical-overflow-ppr", PlannedDate: &pd, Status: "planned",
		EstimatedTargets: 2, PlannedQuantity: "2", QuantityUnit: "dose",
	}, pprObligations)
	if err != nil {
		t.Fatalf("create PPR batch: %v", err)
	}
	if attached != 2 {
		t.Fatalf("attached=%d want 2", attached)
	}
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: pprBatch, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Clinical Spill Shed", PartitionLabel: "1", AnimalCount: 2,
		VaccineRuleIDs: []string{pprVersion.ruleID}, TotalDoses: 2, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed PPR assignment: %v", err)
	}

	out, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: requested, Reason: "move onto approved PPR + Blue Tongue combo day", CreatedBy: operatorA,
	})
	if err != nil {
		t.Fatalf("UpsertVaccinationDriveDateOverride: %v", err)
	}
	if out.AutoShifted {
		t.Fatalf("requested approved combo date should remain applied, got shift to %s", out.OverrideDate.Format("2006-01-02"))
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, requested, pprVersion.ruleID); len(got) != 1 || got[0].animalCount != 1 {
		t.Fatalf("requested date PPR rows = %+v, want exactly 1 animal due operator cap", got)
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, requested.AddDate(0, 0, 1), pprVersion.ruleID); len(got) != 0 {
		t.Fatalf("overflow used clinically unsafe next day: %+v", got)
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, wantOverflow, pprVersion.ruleID); len(got) != 1 || got[0].animalCount != 1 {
		t.Fatalf("safe overflow rows on %s = %+v, want exactly 1 animal", wantOverflow.Format("2006-01-02"), got)
	}

	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: source, Reason: "clear overflow move", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("clear overflow UpsertVaccinationDriveDateOverride: %v", err)
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, wantOverflow, pprVersion.ruleID); len(got) != 0 {
		t.Fatalf("clearing override stranded far overflow row on %s: %+v", wantOverflow.Format("2006-01-02"), got)
	}
	// Clearing re-plans against the ORIGINAL date's real capacity rather than restoring the
	// seeded row verbatim: the one operator available on the source date has a cap of 1, so the
	// second animal comes back unassigned and flagged for a capacity decision instead of being
	// silently re-booked over that cap. What must hold is that both animals come back.
	restoredSource := driveAssignmentRowsForRule(t, ctx, pool, source, pprVersion.ruleID)
	restoredAnimals := 0
	for _, row := range restoredSource {
		restoredAnimals += row.animalCount
	}
	if restoredAnimals != 2 {
		t.Fatalf("clearing override restored %d animals on the source date, want both: %+v", restoredAnimals, restoredSource)
	}
}

func TestDriveDateOverrideEditDoesNotSelfConflictWithPriorOverrideRows(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.clinical.edit", 1)
	pprVersion := versions[0]

	const (
		goatID    = "10000000-0000-4000-8000-00000000fc21"
		shedID    = "00000000-0000-4000-8000-00000000dc21"
		operatorA = "20000000-0000-4000-8000-000000000c21"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "clinical-edit-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code,
  vaccine_type, pathogen_class, min_gap_days, vaccine_json
) VALUES
  ($1, $3, $2, 'vaccination', 'clinical-edit-ppr', 'ppr_adult_w1', 'PPR',
   'live', 'viral', 0, '{"course_type":"single"}'::jsonb)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code,
  vaccine_type = EXCLUDED.vaccine_type,
  pathogen_class = EXCLUDED.pathogen_class,
  min_gap_days = EXCLUDED.min_gap_days,
  vaccine_json = EXCLUDED.vaccine_json`,
		tenantID, pprVersion.ruleID, pprVersion.versionID); err != nil {
		t.Fatalf("seed PPR metadata: %v", err)
	}

	source := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	firstOverride := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	secondOverride := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'CSE-A', 'Clinical Edit Operator A', 'active', 'operator', $3);
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($2, 'clinical_edit_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active');
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($2, $1, 'center', $3, 'clinical_edit_operator_a', 'manager', 'friday', 50, 'active', '2026-01-01')`,
		pgx.QueryExecModeSimpleProtocol, operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	pprObligation, pprBatch := seedShotOnDate(t, ctx, repo, pprVersion, goatID, "shed", shedID, source, source, "clinical-edit-ppr")
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: pprBatch, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Clinical Edit Shed", PartitionLabel: "1", AnimalCount: 1,
		VaccineRuleIDs: []string{pprVersion.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed PPR assignment: %v", err)
	}

	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: firstOverride, Reason: "first move", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("first UpsertVaccinationDriveDateOverride: %v", err)
	}
	out, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: secondOverride, Reason: "edit move", CreatedBy: operatorA,
	})
	if err != nil {
		t.Fatalf("second UpsertVaccinationDriveDateOverride: %v", err)
	}
	if out.AutoShifted {
		t.Fatalf("edit self-conflicted against prior moved row for obligation %s; shifted to %s", pprObligation, out.OverrideDate.Format("2006-01-02"))
	}
	if !out.OverrideDate.Equal(secondOverride) {
		t.Fatalf("applied edit date = %s, want %s", out.OverrideDate.Format("2006-01-02"), secondOverride.Format("2006-01-02"))
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, secondOverride, pprVersion.ruleID); len(got) != 1 || got[0].animalCount != 1 {
		t.Fatalf("edited override rows on %s = %+v, want one moved animal", secondOverride.Format("2006-01-02"), got)
	}
	if got := driveAssignmentRowsForRule(t, ctx, pool, firstOverride, pprVersion.ruleID); len(got) != 0 {
		t.Fatalf("old override rows remained on %s: %+v", firstOverride.Format("2006-01-02"), got)
	}
}

func TestDriveDateOverrideDoesNotExcludeSeparateFutureSameVaccineBooster(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.clinical.futurebooster", 2)
	primaryVersion, boosterVersion := versions[0], versions[1]

	const (
		goatID    = "10000000-0000-4000-8000-00000000fc31"
		shedID    = "00000000-0000-4000-8000-00000000dc31"
		operatorA = "20000000-0000-4000-8000-000000000c31"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "clinical-future-booster-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code,
  vaccine_type, pathogen_class, min_gap_days, vaccine_json
) VALUES
  ($1, $4, $2, 'vaccination', 'clinical-future-ppr-primary', 'ppr_adult_w1', 'PPR',
   'live', 'viral', 28, '{"course_type":"single"}'::jsonb),
  ($1, $5, $3, 'vaccination', 'clinical-future-ppr-booster', 'ppr_adult_w2', 'PPR',
   'live', 'viral', 28, '{"course_type":"single"}'::jsonb)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code,
  vaccine_type = EXCLUDED.vaccine_type,
  pathogen_class = EXCLUDED.pathogen_class,
  min_gap_days = EXCLUDED.min_gap_days,
  vaccine_json = EXCLUDED.vaccine_json`,
		tenantID, primaryVersion.ruleID, boosterVersion.ruleID, primaryVersion.versionID, boosterVersion.versionID); err != nil {
		t.Fatalf("seed PPR metadata: %v", err)
	}

	source := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	requested := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	futureBooster := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	wantApplied := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'CSF-A', 'Clinical Future Operator A', 'active', 'operator', $3);
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($2, 'clinical_future_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active');
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($2, $1, 'center', $3, 'clinical_future_operator_a', 'manager', 'friday', 50, 'active', '2026-01-01')`,
		pgx.QueryExecModeSimpleProtocol, operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	primaryObligation, primaryBatch := seedShotOnDate(t, ctx, repo, primaryVersion, goatID, "shed", shedID, source, source, "clinical-future-ppr-primary")
	seedShotOnDate(t, ctx, repo, boosterVersion, goatID, "shed", shedID, futureBooster, futureBooster, "clinical-future-ppr-booster")
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: primaryBatch, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Clinical Future Shed", PartitionLabel: "1", AnimalCount: 1,
		VaccineRuleIDs: []string{primaryVersion.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed primary assignment: %v", err)
	}

	out, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: requested, Reason: "move near future booster", CreatedBy: operatorA,
	})
	if err != nil {
		t.Fatalf("UpsertVaccinationDriveDateOverride: %v", err)
	}
	if !out.AutoShifted || out.ConflictRule != "booster_min_gap" {
		t.Fatalf("future same-vaccine booster was not treated as conflict for obligation %s: shifted=%v rule=%q", primaryObligation, out.AutoShifted, out.ConflictRule)
	}
	if !out.OverrideDate.Equal(wantApplied) {
		t.Fatalf("applied date = %s, want %s", out.OverrideDate.Format("2006-01-02"), wantApplied.Format("2006-01-02"))
	}
}

func TestDriveDateOverrideEditDoesNotExcludeSeparateFutureSameVaccineBooster(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	proto := protopg.NewRepository(pool, 5*time.Second)
	repo := NewRepository(pool, 5*time.Second)
	versions := seedShotCapVersions(t, ctx, proto, "vaccination.clinical.editfuturebooster", 2)
	primaryVersion, boosterVersion := versions[0], versions[1]

	const (
		goatID    = "10000000-0000-4000-8000-00000000fc41"
		shedID    = "00000000-0000-4000-8000-00000000dc41"
		operatorA = "20000000-0000-4000-8000-000000000c41"
	)
	seedParkConsolidationShed(t, ctx, pool, shedID, "clinical-edit-future-booster-shed")
	seedReserveGoats(t, ctx, pool, shedID, cbePark, goatID)

	if _, err := pool.Exec(ctx, `
INSERT INTO protocol_rule_dimensions (
  tenant_id, protocol_version_id, rule_id, category, selector_key, dose_code, vaccine_code,
  vaccine_type, pathogen_class, min_gap_days, vaccine_json
) VALUES
  ($1, $4, $2, 'vaccination', 'clinical-editfuture-ppr-primary', 'ppr_adult_w1', 'PPR',
   'live', 'viral', 28, '{"course_type":"single"}'::jsonb),
  ($1, $5, $3, 'vaccination', 'clinical-editfuture-ppr-booster', 'ppr_adult_w2', 'PPR',
   'live', 'viral', 28, '{"course_type":"single"}'::jsonb)
ON CONFLICT (tenant_id, protocol_version_id, rule_id, selector_key) DO UPDATE SET
  vaccine_code = EXCLUDED.vaccine_code,
  vaccine_type = EXCLUDED.vaccine_type,
  pathogen_class = EXCLUDED.pathogen_class,
  min_gap_days = EXCLUDED.min_gap_days,
  vaccine_json = EXCLUDED.vaccine_json`,
		tenantID, primaryVersion.ruleID, boosterVersion.ruleID, primaryVersion.versionID, boosterVersion.versionID); err != nil {
		t.Fatalf("seed PPR metadata: %v", err)
	}

	source := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	activeOverride := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	requestedEdit := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	futureBooster := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	wantApplied := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)

	if _, err := pool.Exec(ctx, `
INSERT INTO workforce_members (workforce_member_id, tenant_id, display_code, display_name, status, primary_role_hint, primary_location_id)
VALUES ($1, $2, 'CEF-A', 'Clinical Edit Future Operator A', 'active', 'operator', $3);
INSERT INTO position_module_duties (tenant_id, position_code, module_code, duty_type, capability_code, effective_from, status)
VALUES ($2, 'clinical_edit_future_operator_a', 'vaccination', 'execute', 'vaccination.drive.execute', '2026-01-01', 'active');
INSERT INTO workforce_positions (tenant_id, workforce_member_id, scope_type, scope_id, position_code, position_tier, week_off_weekday, vaccination_daily_animal_cap, status, valid_from)
VALUES ($2, $1, 'center', $3, 'clinical_edit_future_operator_a', 'manager', 'friday', 50, 'active', '2026-01-01')`,
		pgx.QueryExecModeSimpleProtocol, operatorA, tenantID, cbePark); err != nil {
		t.Fatalf("seed operator: %v", err)
	}

	_, primaryBatch := seedShotOnDate(t, ctx, repo, primaryVersion, goatID, "shed", shedID, source, source, "clinical-editfuture-ppr-primary")
	seedShotOnDate(t, ctx, repo, boosterVersion, goatID, "shed", shedID, futureBooster, futureBooster, "clinical-editfuture-ppr-booster")
	if err := repo.UpsertVaccinationDriveAssignments(ctx, tenantID, []domain.DriveAssignment{{
		BatchID: primaryBatch, PlannedDate: source, OperatorID: testStringPtr(operatorA), ParkID: cbePark,
		ShedID: testStringPtr(shedID), PhysicalShed: "Clinical Edit Future Shed", PartitionLabel: "1", AnimalCount: 1,
		VaccineRuleIDs: []string{primaryVersion.ruleID}, TotalDoses: 1, CapacityStatus: "within_cap",
	}}); err != nil {
		t.Fatalf("seed primary assignment: %v", err)
	}

	if _, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: activeOverride, Reason: "first move", CreatedBy: operatorA,
	}); err != nil {
		t.Fatalf("first UpsertVaccinationDriveDateOverride: %v", err)
	}
	out, err := repo.UpsertVaccinationDriveDateOverride(ctx, domain.VaccineDriveDateOverride{
		TenantID: tenantID, ParkID: cbePark, VaccineCode: "PPR", OriginalDriveDate: source,
		OverrideDate: requestedEdit, Reason: "edit near future booster", CreatedBy: operatorA,
	})
	if err != nil {
		t.Fatalf("edit UpsertVaccinationDriveDateOverride: %v", err)
	}
	if !out.AutoShifted || out.ConflictRule != "booster_min_gap" {
		t.Fatalf("future same-vaccine booster was excluded during active edit: shifted=%v rule=%q", out.AutoShifted, out.ConflictRule)
	}
	if !out.OverrideDate.Equal(wantApplied) {
		t.Fatalf("applied edit date = %s, want %s", out.OverrideDate.Format("2006-01-02"), wantApplied.Format("2006-01-02"))
	}
}
