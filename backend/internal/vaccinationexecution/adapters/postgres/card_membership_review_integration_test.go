package postgres

import (
	"context"
	"encoding/json"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"testing"
	"time"
)

func TestCardMembershipIncludesOffPageAssignments(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	const batch2 = "70000000-0000-4000-8000-000000000802"
	const goat2 = "70000000-0000-4000-8000-000000000803"
	const obl2 = "70000000-0000-4000-8000-000000000805"
	insertProjectionGoat(t, ctx, pool, goat2, testShed, testPark)
	insertProjectionBatch(t, ctx, pool, batch2, "planned")
	insertProjectionObligation(t, ctx, pool, obl2, batch2, goat2, "scheduled", "2026-06-25 00:00:00+00", "off-page-member")
	for _, b := range []string{testBatch, batch2} {
		execProjectionSQL(t, ctx, pool, "dated assignment", `INSERT INTO vaccination_drive_assignments (tenant_id,batch_id,planned_date,operator_id,park_id,shed_id,physical_shed,partition_label,animal_count) VALUES ($1,$2,'2026-06-24',$3,$4,$5,'K1 Shed','whole',1)`, testTenant, b, testOperator, testPark, testShed)
	}
	repo := NewRepository(pool, 10*time.Second)
	q := domain.ExecutionQuery{TenantID: testTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 1}
	page, combined, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 1 || page.NextCursor == nil {
		t.Fatalf("fixture must straddle a page: %#v", page)
	}
	separate, err := repo.VaccinationExecutionCardSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for mode, summaries := range map[string]map[string]*domain.ShedCardSummary{"combined": combined, "separate": separate} {
		batches := map[string]bool{}
		total, done, open := 0, 0, 0
		for _, summary := range summaries {
			for _, member := range summary.RosterMemberships {
				total += member.TargetCount
				done += member.DoneCount
				open += member.OpenCount
				if member.BatchID != nil {
					batches[*member.BatchID] = true
				}
				if member.PlannedDate != "2026-06-24" || !member.IncludeWhenOverdue {
					t.Fatalf("%s: wrong dated open membership: %#v", mode, member)
				}
			}
		}
		if total != 2 || done != 1 || open != 1 {
			t.Fatalf("%s page-independent totals = %d/%d/%d, want target/done/open 2/1/1", mode, total, done, open)
		}
		assertOperatorDayCounts(t, mode, summaries, 2, 1, 1)
		if !batches[testBatch] || !batches[batch2] {
			t.Fatalf("%s omitted off-page membership: %#v", mode, batches)
		}
	}
}

// One operational assignment owns two source tasks and three doses for two animals.
// Membership stats must retain animal grain and complete task identities without a second query.
func TestDatedMembershipSummariesKeepDistinctAnimalsAndAllSourceTasks(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	const (
		assignment = "70000000-0000-4000-8000-000000000891"
		batchB     = "70000000-0000-4000-8000-000000000892"
		goatB      = "70000000-0000-4000-8000-000000000893"
		oblB       = "70000000-0000-4000-8000-000000000894"
		oblC       = "70000000-0000-4000-8000-000000000895"
		taskB      = "70000000-0000-4000-8000-000000000896"
	)
	insertProjectionBatch(t, ctx, pool, batchB, "planned")
	insertProjectionGoat(t, ctx, pool, goatB, testShed, testPark)
	insertProjectionObligation(t, ctx, pool, oblB, batchB, goatB, "scheduled", "2026-06-24 00:00:00+00", "dated-task-b")
	execProjectionSQL(t, ctx, pool, "additional vaccine rule", `INSERT INTO protocol_rules (rule_id,tenant_id,protocol_version_id,dose_code,sequence,trigger_type,eligibility_json,proof_policy) VALUES ('70000000-0000-4000-8000-000000000897',$1,$2,'PPR',2,'birth_age','{}','{}')`, testTenant, testVersion)
	execProjectionSQL(t, ctx, pool, "additional vaccine dose", `INSERT INTO obligation_instances (obligation_id,tenant_id,protocol_version_id,rule_id,batch_id,target_type,target_id,scope_type,scope_id,due_at,status,idempotency_key,sequence) VALUES ($1,$2,$3,'70000000-0000-4000-8000-000000000897',$4,'goat',$5,'shed',$6,'2026-06-24','scheduled','dated-dose-c',2)`, oblC, testTenant, testVersion, batchB, testGoat, testShed)
	for _, pair := range [][2]string{{testTask, testBatch}, {taskB, batchB}} {
		execProjectionSQL(t, ctx, pool, "source task", `INSERT INTO sop_tasks (task_id,tenant_id,sop_id,sop_version_id,task_type,title,state,assigned_to,scope_type,scope_id) VALUES ($1,$2,$3,$4,'vaccination_drive','Dated card','in_progress',$5,'shed',$6)`, pair[0], testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed)
		execProjectionSQL(t, ctx, pool, "link source", `UPDATE obligation_batches SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, pair[0], testTenant, pair[1])
	}
	execProjectionSQL(t, ctx, pool, "dated assignment", `INSERT INTO vaccination_drive_assignments (assignment_id,tenant_id,batch_id,planned_date,operator_id,park_id,shed_id,physical_shed,partition_label,animal_count) VALUES ($1,$2,$3,'2026-06-24',$4,$5,$6,'K1 Shed','whole',2)`, assignment, testTenant, testBatch, testOperator, testPark, testShed)
	execProjectionSQL(t, ctx, pool, "exact members", `INSERT INTO vaccination_drive_assignment_members (tenant_id,assignment_id,obligation_id,goat_id) VALUES ($1,$2,$3,$4),($1,$2,$5,$6),($1,$2,$7,$4)`, testTenant, assignment, testObl, testGoat, oblB, goatB, oblC)
	repo := NewRepository(pool, 10*time.Second)
	q := domain.ExecutionQuery{TenantID: testTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 1}
	started := time.Now()
	page, combined, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	combinedElapsed := time.Since(started)
	payload, _ := json.Marshal(combined)
	started = time.Now()
	separate, err := repo.VaccinationExecutionCardSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture animals=2 doses=3 source_tasks=2 page_rows=%d summaries=%d payload_bytes=%d combined_ms=%.3f separate_ms=%.3f", len(page.Rows), len(combined), len(payload), float64(combinedElapsed.Microseconds())/1000, float64(time.Since(started).Microseconds())/1000)
	for mode, summaries := range map[string]map[string]*domain.ShedCardSummary{"combined": combined, "separate": separate} {
		found := false
		for _, summary := range summaries {
			for _, member := range summary.RosterMemberships {
				if member.AssignmentID == nil || *member.AssignmentID != assignment {
					continue
				}
				found = true
				if member.PlannedDate != "2026-06-24" || member.TargetCount != 2 || member.OpenCount != 1 || member.DoneCount != 1 || member.Status != domain.WorkStateDue {
					t.Fatalf("%s wrong dated animal stats: %#v", mode, member)
				}
				if len(member.TaskIDs) != 2 || member.TaskIDs[0] != testTask || member.TaskIDs[1] != taskB {
					t.Fatalf("%s lost source tasks: %#v", mode, member.TaskIDs)
				}
				doses := 0
				for _, group := range member.VaccineGroups {
					doses += group.DoseCount
				}
				if doses != 3 {
					t.Fatalf("%s dose count %d; groups=%#v", mode, doses, member.VaccineGroups)
				}
			}
		}
		if !found {
			t.Fatalf("%s missing assignment", mode)
		}
		assertOperatorDayCounts(t, mode, summaries, 2, 0, 2)
	}
	// A submitted source task must not mark an assignment read-only while its sibling is unsent.
	execProjectionSQL(t, ctx, pool, "submit only source A", `UPDATE sop_tasks SET state='submitted' WHERE tenant_id=$1 AND task_id=$2`, testTenant, testTask)
	insertProjectionCompletion(t, ctx, pool, "70000000-0000-4000-8000-000000000899", oblB, batchB, goatB, "source-b-ready")
	insertProjectionCompletion(t, ctx, pool, "70000000-0000-4000-8000-000000000900", oblC, batchB, testGoat, "source-c-ready")
	_, ready, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	for _, summary := range ready {
		for _, member := range summary.RosterMemberships {
			if member.AssignmentID != nil && *member.AssignmentID == assignment && member.RecordOnly {
				t.Fatalf("mixed submitted/unsent tasks became read-only: %#v", member)
			}
		}
	}

	// Accepted vaccine A must not hide overdue pending vaccine B on the same animal.
	execProjectionSQL(t, ctx, pool, "restore outstanding sibling", `DELETE FROM vaccination_completions WHERE tenant_id=$1 AND completion_id='70000000-0000-4000-8000-000000000900'`, testTenant)
	execProjectionSQL(t, ctx, pool, "exclude other animal for overdue sibling", `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND obligation_id=$2`, testTenant, oblB)
	execProjectionSQL(t, ctx, pool, "accept first vaccine", `UPDATE vaccination_completions SET status='accepted' WHERE tenant_id=$1 AND completion_id=$2`, testTenant, testComplete)
	execProjectionSQL(t, ctx, pool, "complete first vaccine", `UPDATE obligation_instances SET status='completed' WHERE tenant_id=$1 AND obligation_id=$2`, testTenant, testObl)
	tomorrowQ := q
	tomorrowQ.OperatorScopeActorID = testOperator
	tomorrowQ.AsOf = time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	overduePage, overdue, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, tomorrowQ)
	if err != nil {
		t.Fatal(err)
	}
	if len(overduePage.Rows) == 0 {
		t.Fatal("operator page lost overdue pending sibling")
	}
	overdueFound := false
	for _, summary := range overdue {
		for _, member := range summary.RosterMemberships {
			if member.AssignmentID != nil && *member.AssignmentID == assignment && (!member.IncludeWhenOverdue || member.RecordOnly) {
				t.Fatalf("unfinished overdue sibling omitted or locked: %#v", member)
			}
		}
		for _, day := range summary.OperatorDaySummaries {
			if day.BusinessDate == "2026-06-25" {
				overdueFound = true
				if day.TargetCount != 1 || day.DoneCount != 0 || day.OpenCount != 1 || day.Status != domain.WorkStateOverdue {
					t.Fatalf("overdue pending sibling stats: %#v", day)
				}
			}
		}
	}
	if !overdueFound {
		t.Fatal("today lost overdue pending sibling")
	}

	// A deferred sibling can reduce the old display counters to zero; retain the pending membership.
	execProjectionSQL(t, ctx, pool, "defer first sibling", `UPDATE obligation_instances SET status='deferred' WHERE tenant_id=$1 AND obligation_id=$2`, testTenant, testObl)
	execProjectionSQL(t, ctx, pool, "clear done sibling fixture", `DELETE FROM vaccination_completions WHERE tenant_id=$1 AND completion_id=$2`, testTenant, testComplete)
	deferredPage, deferred, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, tomorrowQ)
	if err != nil {
		t.Fatal(err)
	}
	deferredFound := false
	for _, summary := range deferred {
		for _, day := range summary.OperatorDaySummaries {
			if day.BusinessDate == "2026-06-25" {
				deferredFound = day.TargetCount == 1 && day.OpenCount == 1 && day.DoneCount == 0
			}
		}
	}
	if len(deferredPage.Rows) == 0 || !deferredFound {
		t.Fatalf("deferred sibling hid outstanding membership: %#v", deferred)
	}
	execProjectionSQL(t, ctx, pool, "restore completed sibling", `UPDATE obligation_instances SET status='completed' WHERE tenant_id=$1 AND obligation_id=$2`, testTenant, testObl)
	insertProjectionCompletion(t, ctx, pool, testComplete, testObl, testBatch, testGoat, "restore-split-done")
	// Move the other vaccine for the SAME goat to another assignment; pending still wins.
	execProjectionSQL(t, ctx, pool, "second assignment", `INSERT INTO vaccination_drive_assignments (assignment_id,tenant_id,batch_id,planned_date,operator_id,park_id,shed_id,physical_shed,partition_label,animal_count) VALUES ('70000000-0000-4000-8000-000000000898',$1,$2,'2026-06-24',$3,$4,$5,'K1 Shed','whole',1)`, testTenant, batchB, testOperator, testPark, testShed)
	execProjectionSQL(t, ctx, pool, "move outstanding member", `UPDATE vaccination_drive_assignment_members SET assignment_id='70000000-0000-4000-8000-000000000898' WHERE tenant_id=$1 AND obligation_id=$2`, testTenant, oblC)
	execProjectionSQL(t, ctx, pool, "exclude other fixture animal", `UPDATE obligation_instances SET status='canceled' WHERE tenant_id=$1 AND obligation_id=$2`, testTenant, oblB)
	_, split, err := repo.ListVaccinationExecutionFirstPageWithSummaries(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	assertOperatorDayCounts(t, "split same goat", split, 1, 0, 1)
}

func assertOperatorDayCounts(t *testing.T, mode string, summaries map[string]*domain.ShedCardSummary, target, done, open int) {
	t.Helper()
	found := 0
	for _, summary := range summaries {
		for _, day := range summary.OperatorDaySummaries {
			if day.BusinessDate != "2026-06-24" {
				continue
			}
			found++
			if day.TargetCount != target || day.DoneCount != done || day.OpenCount != open {
				t.Fatalf("%s day totals=%#v want target/done/open %d/%d/%d", mode, day, target, done, open)
			}
		}
	}
	if found != 1 {
		t.Fatalf("%s expected one pen/day summary, got %d", mode, found)
	}
}
