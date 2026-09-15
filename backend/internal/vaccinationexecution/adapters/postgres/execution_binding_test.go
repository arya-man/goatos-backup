package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

func TestExecutionScanTaskBindingOneToManyDateShiftStatusMatrix(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedVaccinationExecutionProjection(t, ctx, pool)
	execProjectionSQL(t, ctx, pool, "task", `INSERT INTO sop_tasks(task_id,tenant_id,sop_id,sop_version_id,task_type,title,state,assigned_to,scope_type,scope_id,context) VALUES($1,$2,$3,$4,'vaccination_drive','Scan binding','in_progress',$5,'shed',$6,'{}')`, testTask, testTenant, testVaccinationSOP, testVaccinationSOPVer, testOperator, testShed)
	execProjectionSQL(t, ctx, pool, "batch fallback", `UPDATE obligation_batches SET sop_task_id=$1 WHERE tenant_id=$2 AND batch_id=$3`, testTask, testTenant, testBatch)
	execProjectionSQL(t, ctx, pool, "scan duplicates exact and legacy", `INSERT INTO sop_task_scan_captures(tenant_id,task_id,field_key,tag,normalized_tag,goat_id,obligation_id,captured_by,idempotency_key) VALUES($1,$2,'goat_ids','scan-a','scan-a',$3,$4,$5,'binding-a'),($1,$2,'__scan_roster__','scan-b','scan-b',$3,NULL,$5,'binding-b')`, testTenant, testTask, testGoat, testObl, testOperator)
	q := domain.ExecutionQuery{TenantID: testTenant, AsOf: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC), DueBefore: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Limit: 200}
	check := func(want int) {
		t.Helper()
		p, e := NewRepository(pool, 5*time.Second).ListVaccinationExecutionPage(ctx, q)
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range p.Rows {
			if r.ShedID == testShed {
				if r.ScannedCount != want {
					t.Fatalf("scanned=%d want%d", r.ScannedCount, want)
				}
				return
			}
		}
		if want >= 0 {
			t.Fatal("missing shed")
		}
	}
	check(1)
	execProjectionSQL(t, ctx, pool, "direct task identity", `UPDATE obligation_instances SET sop_task_id=$1 WHERE tenant_id=$2 AND obligation_id=$3`, testTask, testTenant, testObl)
	check(1)
	execProjectionSQL(t, ctx, pool, "remove legacy and misbind exact", `DELETE FROM sop_task_scan_captures WHERE tenant_id=$1 AND field_key='__scan_roster__'`, testTenant)
	execProjectionSQL(t, ctx, pool, "wrong obligation cannot match goat", `UPDATE sop_task_scan_captures SET obligation_id=$1 WHERE tenant_id=$2 AND task_id=$3`, testCanceledObligation, testTenant, testTask)
	check(0)
	q.DueBefore = time.Date(2026, 6, 23, 0, 0, 0, 0, time.UTC)
	check(-1)
	q.DueBefore = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	execProjectionSQL(t, ctx, pool, "canceled task stays excluded", `UPDATE sop_tasks SET state='canceled' WHERE tenant_id=$1 AND task_id=$2`, testTenant, testTask)
	check(-1)
}
