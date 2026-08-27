package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	oblpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obldomain "github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
	vaccpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

const (
	pcDirector = "9c000000-0000-4000-8000-000000005004"
	pcProtocol = "9c000000-0000-4000-8000-000000007001"
	pcVersion  = "9c000000-0000-4000-8000-000000007002"
	pcRuleETTT = "9c000000-0000-4000-8000-000000007101"
	pcRulePPR  = "9c000000-0000-4000-8000-000000007102"
	pcBatch    = "9c000000-0000-4000-8000-000000008001"
	pcBatchP7  = "9c000000-0000-4000-8000-000000008007"
)

func TestReconcileInventoryVaccineTasksCreatesDirectorTaskFromDriveAssignments(t *testing.T) {
	testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t)
}

func TestReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t *testing.T) {
	testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t)
}

func TestReconcileInventoryVaccineTasksMultipleDimensions(t *testing.T) {
	testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t)
}

func TestReconcileInventoryVaccineTasksPagination(t *testing.T) {
	testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t)
}

func TestReconcileInventoryVaccineTasksScheduledDate(t *testing.T) {
	testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t)
}

func TestReconcileInventoryVaccineTasksParkScope(t *testing.T) {
	testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t)
}

func TestReconcileInventoryVaccineTasksEveryStatus(t *testing.T) {
	testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t)
}

func testReconcileInventoryVaccineTasksOneToManyPageBoundaryDateShiftScopeHierarchyStatusMatrix(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO workforce_members (tenant_id, display_code, display_name, status, primary_role_hint, user_id)
VALUES ($1::uuid, 'PC-DIR', 'Chandrakant', 'active', 'pc_director', $2::uuid)`, pcTenant, pcDirector)
	exec(`INSERT INTO protocol_definitions (tenant_id, protocol_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.inventory.test', 'Vaccination Inventory Test', 'vaccination', 'active')`, pcTenant, pcProtocol)
	exec(`INSERT INTO protocol_versions (tenant_id, protocol_version_id, protocol_id, version, version_label, status, effective_from, rule_dsl)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '2026-01-01', '{"vaccine":{"name":"Fallback"}}'::jsonb)`, pcTenant, pcVersion, pcProtocol)
	exec(`INSERT INTO protocol_rules (tenant_id, protocol_version_id, rule_id, dose_code, trigger_type)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'ET_TT', 'calendar'),
       ($1::uuid, $2::uuid, $4::uuid, 'PPR', 'calendar')`, pcTenant, pcVersion, pcRuleETTT, pcRulePPR)
	exec(`INSERT INTO protocol_rule_dimensions (tenant_id, protocol_version_id, rule_id, category, selector_key, vaccine_code, vaccine_type)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'vaccination', 'all', 'ET_TT', ''),
       ($1::uuid, $2::uuid, $4::uuid, 'vaccination', 'all', 'PPR', '')`, pcTenant, pcVersion, pcRuleETTT, pcRulePPR)
	exec(`INSERT INTO obligation_batches (tenant_id, batch_id, protocol_version_id, scope_type, scope_id, planned_date, status, estimated_targets)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4::uuid, '2026-08-26', 'planned', 3)`, pcTenant, pcBatch, pcVersion, pcShedA)
	exec(`INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key
) VALUES
  ($1::uuid, $2::uuid, $3::uuid, $5::uuid, 'goat', '9c000000-0000-4000-8000-000000009001'::uuid, 'shed', $6::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-et-1'),
  ($1::uuid, $2::uuid, $3::uuid, $5::uuid, 'goat', '9c000000-0000-4000-8000-000000009002'::uuid, 'shed', $6::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-et-2'),
  ($1::uuid, $2::uuid, $4::uuid, $5::uuid, 'goat', '9c000000-0000-4000-8000-000000009003'::uuid, 'shed', $6::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-ppr-1'),
  ($1::uuid, $2::uuid, $4::uuid, $5::uuid, 'goat', '9c000000-0000-4000-8000-000000009004'::uuid, 'shed', $6::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-ppr-2'),
  ($1::uuid, $2::uuid, $4::uuid, $5::uuid, 'goat', '9c000000-0000-4000-8000-000000009005'::uuid, 'shed', $6::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-ppr-3')`,
		pcTenant, pcVersion, pcRuleETTT, pcRulePPR, pcBatch, pcShedA)
	exec(`INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses
) VALUES (
  $1::uuid, $2::uuid, '2026-08-26', $3::uuid, $4::uuid, 'Castro', 'whole', 5, ARRAY[$5::uuid,$6::uuid], 5
)`, pcTenant, pcBatch, pcPark, pcShedA, pcRuleETTT, pcRulePPR)

	early, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 18))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks eight-days-before: %v", err)
	}
	if early.TasksCreated != 0 || early.RequirementsUpserted != 0 {
		t.Fatalf("eight-days-before result = %+v, want no task or requirements before the 7-day trigger window", early)
	}

	result, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 20))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks late direct DB catch-up: %v", err)
	}
	if result.TasksCreated != 1 || result.DirectorAssigneeCount != 1 || result.AssigneesInserted != 1 || result.RequirementsUpserted != 2 {
		t.Fatalf("late catch-up result = %+v, want one task/director and two requirements", result)
	}

	page, err := repo.ListTasks(ctx, portsList(pcTenant, "2026-08-19", domain.CategoryInventoryVaccine, pcDirector))
	if err != nil {
		t.Fatalf("ListTasks inventory: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("inventory page size = %d, want 1", len(page.Items))
	}
	task := page.Items[0]
	if task.Category != domain.CategoryInventoryVaccine || task.PlannedBusinessDate != "2026-08-19" || task.DueBusinessDate != "2026-08-19" {
		t.Fatalf("task = %+v, want inventory task due 2026-08-19", task)
	}
	got := map[string]int32{}
	for _, req := range task.InventoryRequirements {
		got[req.VaccineLabel] = req.RequiredDoses
	}
	if got["ET+TT"] != 2 || got["PPR"] != 3 {
		t.Fatalf("requirements = %#v, want ET+TT=2 PPR=3", got)
	}
	monitorPage, err := repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:        pcTenant,
		DueBusinessDate: "2026-08-19",
		TenantWide:      true,
		Category:        domain.CategoryInventoryVaccine,
		Limit:           20,
	})
	if err != nil {
		t.Fatalf("ListTasks inventory monitor: %v", err)
	}
	if len(monitorPage.Items) != 1 {
		t.Fatalf("inventory monitor page size = %d, want 1", len(monitorPage.Items))
	}
	monitorTask := monitorPage.Items[0]
	if monitorTask.TaskID != task.TaskID || len(monitorTask.AssigneeUserIDs) != 1 || monitorTask.AssigneeUserIDs[0] != pcDirector {
		t.Fatalf("monitor task = %+v, want director inventory task", monitorTask)
	}
	monitorReqs := map[string]int32{}
	for _, req := range monitorTask.InventoryRequirements {
		monitorReqs[req.VaccineLabel] = req.RequiredDoses
	}
	if monitorReqs["ET+TT"] != 2 || monitorReqs["PPR"] != 3 {
		t.Fatalf("monitor requirements = %#v, want ET+TT=2 PPR=3", monitorReqs)
	}

	replay, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 19))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks replay: %v", err)
	}
	if replay.TasksCreated != 0 || replay.RequirementsUpserted != 2 {
		t.Fatalf("replay result = %+v, want no duplicate task and refreshed requirements", replay)
	}
	catchupReplay, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 20))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks catch-up replay: %v", err)
	}
	if catchupReplay.TasksCreated != 0 || catchupReplay.RequirementsUpserted != 2 {
		t.Fatalf("catch-up replay result = %+v, want no duplicate task and refreshed requirements on canonical T-7 task", catchupReplay)
	}

	exec(`UPDATE obligation_instances
SET status='canceled'
WHERE tenant_id=$1::uuid AND batch_id=$2::uuid AND rule_id=$3::uuid`,
		pcTenant, pcBatch, pcRuleETTT)
	refresh, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 19))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks after direct DB removal: %v", err)
	}
	if refresh.TasksCreated != 0 || refresh.RequirementsUpserted != 1 {
		t.Fatalf("refresh result = %+v, want no duplicate task and one surviving requirement", refresh)
	}
	refreshedPage, err := repo.ListTasks(ctx, portsList(pcTenant, "2026-08-19", domain.CategoryInventoryVaccine, pcDirector))
	if err != nil {
		t.Fatalf("ListTasks refreshed inventory: %v", err)
	}
	if len(refreshedPage.Items) != 1 {
		t.Fatalf("refreshed inventory page size = %d, want 1", len(refreshedPage.Items))
	}
	refreshedReqs := map[string]int32{}
	for _, req := range refreshedPage.Items[0].InventoryRequirements {
		refreshedReqs[req.VaccineLabel] = req.RequiredDoses
	}
	if _, ok := refreshedReqs["ET+TT"]; ok || refreshedReqs["PPR"] != 3 || len(refreshedReqs) != 1 {
		t.Fatalf("refreshed requirements = %#v, want stale ET+TT deleted and PPR=3", refreshedReqs)
	}

	sweep, err := repo.SweepTaskRollForward(ctx, pcTenant, pcBusinessDay(2026, time.August, 20), 50, 5)
	if err != nil {
		t.Fatalf("SweepTaskRollForward inventory carry-over: %v", err)
	}
	if sweep.RolledForward != 1 || sweep.Truncated {
		t.Fatalf("sweep result = %+v, want the unfinished 24-hour inventory task carried forward once", sweep)
	}
	carry, err := repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:        pcTenant,
		DueBusinessDate: "2026-08-20",
		TenantWide:      true,
		Category:        domain.CategoryInventoryVaccine,
		AssigneeUserID:  pcDirector,
		CurrentOrCarry:  true,
		Limit:           20,
	})
	if err != nil {
		t.Fatalf("ListTasks inventory carry-over: %v", err)
	}
	if len(carry.Items) != 1 {
		t.Fatalf("carry-over inventory page size = %d, want 1", len(carry.Items))
	}
	carried := carry.Items[0]
	if carried.TaskID != task.TaskID || carried.WorkState != domain.WorkStateDelayed || carried.PlannedBusinessDate != "2026-08-19" || carried.DueBusinessDate != "2026-08-20" {
		t.Fatalf("carried task = %+v, want same inventory task delayed from 2026-08-19 to 2026-08-20", carried)
	}

	exec(`UPDATE pc_care_tasks
SET due_business_date='2026-08-27', work_state='delayed', updated_at=now(), row_version=row_version+1
WHERE tenant_id=$1::uuid AND task_id=$2::uuid`, pcTenant, task.TaskID)
	exec(`UPDATE obligation_instances
SET status='canceled'
WHERE tenant_id=$1::uuid AND batch_id=$2::uuid AND rule_id=$3::uuid`,
		pcTenant, pcBatch, pcRulePPR)
	empty, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 28))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks after all source obligations removed: %v", err)
	}
	if empty.TasksCreated != 0 || empty.RequirementsUpserted != 0 {
		t.Fatalf("all-source-removed result = %+v, want no duplicate task and no surviving requirements", empty)
	}
	canceled, err := repo.ListTasks(ctx, ports.ListTasksQuery{
		TenantID:        pcTenant,
		DueBusinessDate: "2026-08-28",
		TenantWide:      true,
		Category:        domain.CategoryInventoryVaccine,
		AssigneeUserID:  pcDirector,
		CurrentOrCarry:  true,
		Limit:           20,
	})
	if err != nil {
		t.Fatalf("ListTasks after all source removed: %v", err)
	}
	if len(canceled.Items) != 0 {
		t.Fatalf("all-source-removed inventory page size = %d, want canceled task hidden; rows=%+v", len(canceled.Items), canceled.Items)
	}
}

func TestInventoryVaccineTaskFollowsGeneratedProcuredAdultObligation(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO workforce_members (tenant_id, display_code, display_name, status, primary_role_hint, user_id)
VALUES ($1::uuid, 'PC-DIR-GEN', 'Chandrakant', 'active', 'pc_director', $2::uuid)`, pcTenant, pcDirector)

	proto := protopg.NewRepository(pool, 5*time.Second)
	obl := oblpg.NewRepository(pool, 5*time.Second)
	vacc := vaccpg.NewRepository(pool, 5*time.Second)
	protoID, err := proto.CreateDefinition(ctx, protodomain.NewDefinition{
		TenantID: pcTenant, Code: "vaccination.inventory.generated", Name: "Inventory Generated", Category: "vaccination", Status: "draft",
	})
	if err != nil {
		t.Fatalf("definition: %v", err)
	}
	versionID, err := proto.CreateVersion(ctx, protodomain.NewVersion{
		TenantID: pcTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
		EffectiveFrom: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		RuleDsl:       []byte(`{"eligibility":{},"vaccine":{"code":"PPR","name":"PPR","type":"live","pathogen_class":"viral"}}`),
		ProofPolicy:   []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	ruleID, err := proto.CreateRule(ctx, protodomain.NewRule{
		TenantID: pcTenant, ProtocolVersionID: versionID, DoseCode: "ppr_adult_w1", Sequence: 1,
		TriggerType: "post_arrival", OffsetDays: 7, Repeat: "none", CatchUp: "pc_approval",
		EligibilityJSON: []byte(`{"vaccine":{"code":"PPR","name":"PPR","type":"live","pathogen_class":"viral"}}`),
		ProofPolicy:     []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("rule: %v", err)
	}
	if err := proto.ReplaceProtocolRuleDimensions(ctx, pcTenant, versionID, []protodomain.RuleDimension{{
		ProtocolVersionID: versionID, RuleID: ruleID, Category: "vaccination", SelectorKey: "adult_procured", VaccineCode: "PPR", VaccineType: "live",
	}}); err != nil {
		t.Fatalf("rule dimensions: %v", err)
	}
	if err := proto.PublishVersion(ctx, pcTenant, versionID, nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	goatID := "9c000000-0000-4000-8000-000000009101"
	seedPCCareProcuredAdultGoat(t, ctx, pool, goatID, time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))
	gen := vaccapp.NewGenerationService(proto, vacc, obl)
	res, err := gen.GenerateForGoat(ctx, pcTenant, goatID, pcBusinessDay(2026, time.August, 26))
	if err != nil {
		t.Fatalf("generate procured adult obligation: %v", err)
	}
	if res.Generated != 1 {
		t.Fatalf("generation result = %+v, want one age/procurement-rule obligation", res)
	}

	obligationID := scanPCCareText(t, ctx, pool,
		`SELECT obligation_id::text FROM obligation_instances WHERE tenant_id=$1::uuid AND target_id=$2::uuid AND protocol_version_id=$3::uuid`,
		pcTenant, goatID, versionID)
	plannedDate := pcBusinessDay(2026, time.August, 26)
	batchID, attached, err := obl.CreateBatchWithObligations(ctx, obldomain.NewBatch{
		TenantID: pcTenant, ProtocolVersionID: versionID, ScopeType: "shed", ScopeID: pcShedA,
		PlannedDate: &plannedDate, Status: "planned", EstimatedTargets: 1,
	}, []string{obligationID})
	if err != nil {
		t.Fatalf("create generated obligation batch: %v", err)
	}
	if attached != 1 {
		t.Fatalf("attached obligations = %d, want 1", attached)
	}
	exec(`INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses
) VALUES (
  $1::uuid, $2::uuid, '2026-08-26', $3::uuid, $4::uuid, 'Castro', 'whole', 1, ARRAY[$5::uuid], 1
)`, pcTenant, batchID, pcPark, pcShedA, ruleID)

	result, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 19))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks from generated obligation: %v", err)
	}
	if result.TasksCreated != 1 || result.RequirementsUpserted != 1 {
		t.Fatalf("result = %+v, want one generated-source inventory task and one requirement", result)
	}
	page, err := repo.ListTasks(ctx, portsList(pcTenant, "2026-08-19", domain.CategoryInventoryVaccine, pcDirector))
	if err != nil {
		t.Fatalf("ListTasks generated inventory: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("generated inventory page size = %d, want 1", len(page.Items))
	}
	reqs := page.Items[0].InventoryRequirements
	if len(reqs) != 1 || reqs[0].VaccineLabel != "PPR" || reqs[0].RequiredDoses != 1 {
		t.Fatalf("generated inventory requirements = %+v, want PPR=1", reqs)
	}
}

func TestInventoryVaccineTaskUsesDriveAssignmentPartitionGrain(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}

	exec(`INSERT INTO workforce_members (tenant_id, display_code, display_name, status, primary_role_hint, user_id)
VALUES ($1::uuid, 'PC-DIR-P7', 'Chandrakant', 'active', 'pc_director', $2::uuid)`, pcTenant, pcDirector)
	exec(`INSERT INTO protocol_definitions (tenant_id, protocol_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.inventory.partition', 'Vaccination Inventory Partition', 'vaccination', 'active')`, pcTenant, pcProtocol)
	exec(`INSERT INTO protocol_versions (tenant_id, protocol_version_id, protocol_id, version, version_label, status, effective_from, rule_dsl)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '2026-01-01', '{"vaccine":{"name":"Fallback"}}'::jsonb)`, pcTenant, pcVersion, pcProtocol)
	exec(`INSERT INTO protocol_rules (tenant_id, protocol_version_id, rule_id, dose_code, trigger_type)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'PPR', 'calendar')`, pcTenant, pcVersion, pcRulePPR)
	exec(`INSERT INTO protocol_rule_dimensions (tenant_id, protocol_version_id, rule_id, category, selector_key, vaccine_code, vaccine_type)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'vaccination', 'all', 'PPR', '')`, pcTenant, pcVersion, pcRulePPR)
	exec(`INSERT INTO obligation_batches (tenant_id, batch_id, protocol_version_id, scope_type, scope_id, planned_date, status, estimated_targets)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4::uuid, '2026-08-26', 'planned', 4)`, pcTenant, pcBatchP7, pcVersion, pcShedA)
	exec(`INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key
) VALUES
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009701'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-p7-1'),
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009702'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-p7-2'),
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009703'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-p7-3'),
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009704'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-p7-4')`,
		pcTenant, pcVersion, pcRulePPR, pcBatchP7, pcShedA)
	exec(`INSERT INTO vaccination_drive_assignments (
  tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses
) VALUES (
  $1::uuid, $2::uuid, '2026-08-26', $3::uuid, $4::uuid, 'Castro', 'Part 7', 4, ARRAY[$5::uuid], 4
)`, pcTenant, pcBatchP7, pcPark, pcShedA, pcRulePPR)

	result, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 19))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks partition drive: %v", err)
	}
	if result.TasksCreated != 1 || result.RequirementsUpserted != 1 {
		t.Fatalf("partition result = %+v, want one partition-grain task and one requirement", result)
	}
	page, err := repo.ListTasks(ctx, portsList(pcTenant, "2026-08-19", domain.CategoryInventoryVaccine, pcDirector))
	if err != nil {
		t.Fatalf("ListTasks partition inventory: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("partition inventory page size = %d, want 1", len(page.Items))
	}
	task := page.Items[0]
	if task.PartitionLabel != "Part 7" {
		t.Fatalf("task partition = %q, want Part 7 from vaccination_drive_assignments", task.PartitionLabel)
	}
	if len(task.InventoryRequirements) != 1 || task.InventoryRequirements[0].VaccineLabel != "PPR" || task.InventoryRequirements[0].RequiredDoses != 4 {
		t.Fatalf("partition requirements = %+v, want PPR=4", task.InventoryRequirements)
	}
}

func TestInventoryVaccineRequirementsUseExactDriveAssignmentMembers(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}

	const (
		splitBatch = "9c000000-0000-4000-8000-000000008100"
		assignP1   = "9c000000-0000-4000-8000-000000008101"
		assignP2   = "9c000000-0000-4000-8000-000000008102"
	)
	exec(`INSERT INTO workforce_members (tenant_id, display_code, display_name, status, primary_role_hint, user_id)
VALUES ($1::uuid, 'PC-DIR-SPLIT', 'Chandrakant', 'active', 'pc_director', $2::uuid)`, pcTenant, pcDirector)
	exec(`INSERT INTO protocol_definitions (tenant_id, protocol_id, code, name, category, status)
VALUES ($1::uuid, $2::uuid, 'vaccination.inventory.split', 'Vaccination Inventory Split', 'vaccination', 'active')`, pcTenant, pcProtocol)
	exec(`INSERT INTO protocol_versions (tenant_id, protocol_version_id, protocol_id, version, version_label, status, effective_from, rule_dsl)
VALUES ($1::uuid, $2::uuid, $3::uuid, 1, 'v1', 'published', '2026-01-01', '{"vaccine":{"name":"Fallback"}}'::jsonb)`, pcTenant, pcVersion, pcProtocol)
	exec(`INSERT INTO protocol_rules (tenant_id, protocol_version_id, rule_id, dose_code, trigger_type)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'PPR', 'calendar')`, pcTenant, pcVersion, pcRulePPR)
	exec(`INSERT INTO protocol_rule_dimensions (tenant_id, protocol_version_id, rule_id, category, selector_key, vaccine_code, vaccine_type)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'vaccination', 'all', 'PPR', '')`, pcTenant, pcVersion, pcRulePPR)
	exec(`INSERT INTO obligation_batches (tenant_id, batch_id, protocol_version_id, scope_type, scope_id, planned_date, status, estimated_targets)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'shed', $4::uuid, '2026-08-26', 'planned', 4)`, pcTenant, splitBatch, pcVersion, pcShedA)
	for _, goatID := range []string{
		"9c000000-0000-4000-8000-000000009811",
		"9c000000-0000-4000-8000-000000009812",
		"9c000000-0000-4000-8000-000000009813",
		"9c000000-0000-4000-8000-000000009814",
	} {
		seedPCCareProcuredAdultGoat(t, ctx, pool, goatID, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	}
	exec(`INSERT INTO obligation_instances (
  tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, window_start, window_end, status, idempotency_key
) VALUES
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009811'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-split-1'),
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009812'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-split-2'),
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009813'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-split-3'),
  ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'goat', '9c000000-0000-4000-8000-000000009814'::uuid, 'shed', $5::uuid, '2026-08-26 00:00:00+05:30', '2026-08-26 00:00:00+05:30', '2026-08-27 00:00:00+05:30', 'scheduled', 'pc-inv-split-4')`,
		pcTenant, pcVersion, pcRulePPR, splitBatch, pcShedA)
	exec(`INSERT INTO vaccination_drive_assignments (
  assignment_id, tenant_id, batch_id, planned_date, park_id, shed_id, physical_shed, partition_label, animal_count, vaccine_rule_ids, total_doses
) VALUES
  ($1::uuid, $2::uuid, $3::uuid, '2026-08-26', $4::uuid, $5::uuid, 'Castro', 'Part 1', 1, ARRAY[$6::uuid], 1),
  ($7::uuid, $2::uuid, $3::uuid, '2026-08-26', $4::uuid, $5::uuid, 'Castro', 'Part 2', 3, ARRAY[$6::uuid], 3)`,
		assignP1, pcTenant, splitBatch, pcPark, pcShedA, pcRulePPR, assignP2)
	exec(`INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT tenant_id, $1::uuid, obligation_id, target_id
FROM obligation_instances
WHERE tenant_id=$2::uuid AND idempotency_key='pc-inv-split-1'`, assignP1, pcTenant)
	exec(`INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT tenant_id, $1::uuid, obligation_id, target_id
FROM obligation_instances
WHERE tenant_id=$2::uuid AND idempotency_key IN ('pc-inv-split-2','pc-inv-split-3','pc-inv-split-4')`, assignP2, pcTenant)

	result, err := repo.ReconcileInventoryVaccineTasks(ctx, pcTenant, pcBusinessDay(2026, time.August, 19))
	if err != nil {
		t.Fatalf("ReconcileInventoryVaccineTasks split members: %v", err)
	}
	if result.TasksCreated != 2 || result.RequirementsUpserted != 2 {
		t.Fatalf("split result = %+v, want two partition tasks and two exact requirements", result)
	}
	page, err := repo.ListTasks(ctx, portsList(pcTenant, "2026-08-19", domain.CategoryInventoryVaccine, pcDirector))
	if err != nil {
		t.Fatalf("ListTasks split inventory: %v", err)
	}
	got := map[string]int32{}
	for _, task := range page.Items {
		if len(task.InventoryRequirements) != 1 || task.InventoryRequirements[0].VaccineLabel != "PPR" {
			t.Fatalf("split task requirements = %+v, want one PPR row", task.InventoryRequirements)
		}
		got[task.PartitionLabel] = task.InventoryRequirements[0].RequiredDoses
	}
	if got["Part 1"] != 1 || got["Part 2"] != 3 {
		t.Fatalf("split requirements = %#v, want Part 1=1 Part 2=3", got)
	}
}

func TestInventoryVaccineChildTablesRejectTenantTaskMismatch(t *testing.T) {
	ctx := context.Background()
	_, pool := setupPCCareDB(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}

	const taskID = "9c000000-0000-4000-8000-000000008900"
	const otherTenant = "9c000000-0000-4000-8000-000000000099"
	exec(`INSERT INTO pc_care_tasks (
  task_id, tenant_id, category, park_id, shed_id, planned_business_date, due_business_date, idempotency_key, created_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, '2026-08-19', '2026-08-19', 'pc-inv-child-parity', $6::uuid
)`, taskID, pcTenant, domain.CategoryInventoryVaccine, pcPark, pcShedA, pcOperator1)

	if _, err := pool.Exec(ctx, `INSERT INTO pc_care_task_proofs (
  tenant_id, task_id, slot_key, proof_ref, captured_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3, 'proof-cross-tenant', $4::uuid, 'idem-cross-tenant-proof'
)`, otherTenant, taskID, domain.SlotStockFridgeVideo, pcOperator1); err == nil {
		t.Fatal("pc_care_task_proofs accepted tenant/task mismatch, want FK violation")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO pc_care_task_inventory_requirements (
  tenant_id, task_id, vaccine_label, required_doses
) VALUES (
  $1::uuid, $2::uuid, 'PPR', 1
)`, otherTenant, taskID); err == nil {
		t.Fatal("pc_care_task_inventory_requirements accepted tenant/task mismatch, want FK violation")
	}
}

func portsList(tenantID, dueDate, category, assignee string) ports.ListTasksQuery {
	return ports.ListTasksQuery{
		TenantID:        tenantID,
		DueBusinessDate: dueDate,
		TenantWide:      true,
		Category:        category,
		AssigneeUserID:  assignee,
		Limit:           20,
	}
}

func seedPCCareProcuredAdultGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, entryDate time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx,
		`INSERT INTO parties (party_id, tenant_id, party_type, display_name, status)
		 VALUES ('9c000000-0000-4000-8000-000000002001'::uuid, $1::uuid, 'farm', 'Mesha CPT', 'active')
		 ON CONFLICT (party_id) DO NOTHING`, pcTenant); err != nil {
		t.Fatalf("seed party: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO goats (
		   goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
		   current_location_id, park_id, shed_id, management_stage, dob, origin_type, entry_date
		 ) VALUES (
		   $1::uuid, $2::uuid, 'alive', 'goat', '9c000000-0000-4000-8000-000000002001'::uuid, 'female',
		   $3::uuid, $4::uuid, $3::uuid, 'adult', DATE '2025-01-01', 'procured', $5::date
		 )`, id, pcTenant, pcShedA, pcPark, entryDate); err != nil {
		t.Fatalf("seed procured adult goat %s: %v", id, err)
	}
}

func scanPCCareText(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
		t.Fatalf("scan text: %v\nsql: %s", err, sql)
	}
	return out
}
