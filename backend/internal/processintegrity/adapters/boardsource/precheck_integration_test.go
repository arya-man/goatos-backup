package boardsource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// legacyVaccinationDueWorkPrecheckSQL is the pre-2026-09-24 precheck, kept as the equivalence
// oracle with ONE correction of 2026-09-25: its due_at clause counts UNBATCHED obligations only,
// as the canonical read does (a batched obligation belongs to its drive's planned day). Otherwise
// verbatim: one EXISTS whose day window is OR'ed across obligation_instances,
// obligation_batches and the two drive-assignment tables, which leaves only tenant_id sargable
// and so walks every obligation of the tenant (86k rows / 228 ms on the stg clone) whenever the
// day has no vaccination work.
const legacyVaccinationDueWorkPrecheckSQL = `
SELECT EXISTS (
  SELECT 1
  FROM obligation_instances oi
  LEFT JOIN obligation_batches ob ON ob.tenant_id = oi.tenant_id AND ob.batch_id = oi.batch_id
  JOIN protocol_versions pv ON pv.tenant_id = oi.tenant_id AND pv.protocol_version_id = oi.protocol_version_id
  JOIN protocol_definitions pd ON pd.tenant_id = oi.tenant_id AND pd.protocol_id = pv.protocol_id AND pd.category = 'vaccination'
  JOIN goats g ON g.tenant_id = oi.tenant_id AND g.goat_id = oi.target_id AND g.park_id = $2::uuid
  WHERE oi.tenant_id = $1::uuid
    AND oi.target_type = 'goat'
    AND oi.status <> 'canceled'
    AND ($5::boolean OR oi.status <> 'completed')
    AND (
      (oi.batch_id IS NULL AND oi.due_at >= $3::timestamptz AND oi.due_at < $4::timestamptz)
      OR ((ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') >= $3::timestamptz
        AND (ob.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $4::timestamptz)
      OR EXISTS (
        SELECT 1
        FROM vaccination_drive_assignment_members vdam
        JOIN vaccination_drive_assignments vda ON vda.tenant_id = vdam.tenant_id AND vda.assignment_id = vdam.assignment_id
        WHERE vdam.tenant_id = oi.tenant_id AND vdam.obligation_id = oi.obligation_id
          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') >= $3::timestamptz
          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $4::timestamptz)
      OR EXISTS (
        SELECT 1
        FROM vaccination_drive_assignments vda
        WHERE vda.tenant_id = oi.tenant_id AND vda.batch_id = oi.batch_id AND vda.park_id = $2::uuid
          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') >= $3::timestamptz
          AND (vda.planned_date::timestamp AT TIME ZONE 'Asia/Kolkata') < $4::timestamptz)
    )
  LIMIT 1
)`

func precheckArgs(park, day string, includeCompleted bool) []any {
	dayStart, err := time.ParseInLocation("2006-01-02", day, biztime.DefaultLocation())
	if err != nil {
		panic(err)
	}
	return vaccinationDueWorkPrecheckArgs(stTenant, park, dayStart, includeCompleted)
}

func precheckAnswer(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args []any) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(ctx, sql, args...).Scan(&ok); err != nil {
		t.Fatalf("precheck: %v", err)
	}
	return ok
}

// TestVaccinationDueWorkPrecheckAnswersLikeTheLegacyORAcrossEveryDateSource isolates each of the
// four "this goat has vaccination work on the day" sources in turn (its own due date, its
// batch's planned date, a member assignment's planned date, a park batch assignment's planned
// date) and requires the indexed UNION ALL rewrite to answer exactly as the legacy OR did, for
// every day around them, both parks and both completed-work modes.
func TestVaccinationDueWorkPrecheckAnswersLikeTheLegacyORAcrossEveryDateSource(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedDrivePen(t, ctx, pool)

	// Four separable date sources: due_at 09-10, batch planned 09-12, member assignment 09-14
	// (on a second batch), park assignment of the first batch 09-16.
	execST(t, ctx, pool, `UPDATE obligation_batches SET planned_date = DATE '2026-09-12' WHERE batch_id = $1::uuid`, stBatch)
	execST(t, ctx, pool, `UPDATE vaccination_drive_assignments SET planned_date = DATE '2026-09-16' WHERE assignment_id = $1::uuid`, stAssign)
	// The member assignment rides a SECOND batch (planned on a day nothing else uses), so the
	// park-batch arm cannot answer for it: only the member arm links obligation d708 to 09-14.
	execST(t, ctx, pool, `
INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date)
VALUES ('00000000-0000-4000-8000-00000000d902'::uuid, $1::uuid, $2::uuid, 'shed', $3::uuid, 'in_progress', DATE '2026-09-20')`, stTenant, stVersion, stShed)
	execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
VALUES ('00000000-0000-4000-8000-00000000d912'::uuid, $1::uuid, '00000000-0000-4000-8000-00000000d902'::uuid, DATE '2026-09-14', $2::uuid, $3::uuid, $4::uuid, 'Godel 1', 'Part 3', 1)`,
		stTenant, stMember, stPark, stShed)
	execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
VALUES ($1::uuid, '00000000-0000-4000-8000-00000000d912'::uuid, '00000000-0000-4000-8000-00000000d708'::uuid, '00000000-0000-4000-8000-00000000d608'::uuid)`, stTenant)

	days := []string{"2026-09-09", "2026-09-10", "2026-09-11", "2026-09-12", "2026-09-13", "2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17", "2026-09-20"}
	check := func(label string, wantAnyTrue bool) {
		t.Helper()
		anyTrue := false
		for _, park := range []string{stPark, stOtherPk} {
			for _, day := range days {
				for _, inc := range []bool{true, false} {
					args := precheckArgs(park, day, inc)
					legacyArgs := args[:5]
					want := precheckAnswer(t, ctx, pool, legacyVaccinationDueWorkPrecheckSQL, legacyArgs)
					got := precheckAnswer(t, ctx, pool, vaccinationDueWorkPrecheckSQL, args)
					if got != want {
						t.Fatalf("%s: park %s day %s includeCompleted %v: precheck %v, legacy %v", label, park, day, inc, got, want)
					}
					anyTrue = anyTrue || got
				}
			}
		}
		if anyTrue != wantAnyTrue {
			t.Fatalf("%s: some day true = %v, want %v (scenario did not exercise the arm)", label, anyTrue, wantAnyTrue)
		}
	}
	check("all four sources", true)

	// 2026-09-25: an obligation due on a day whose DRIVE is planned for another day is not work
	// on its due day -- the canonical read files it under the drive's day. Before the fix the
	// precheck said "work today" and the board paid the canonical read for nothing (Channapatna,
	// 25/09: two obligations due that day, both in drives planned elsewhere). The seeded
	// obligations are batched (batch 09-12, park assignment 09-16), so their due day 09-10 is empty.
	for _, inc := range []bool{true, false} {
		if precheckAnswer(t, ctx, pool, vaccinationDueWorkPrecheckSQL, precheckArgs(stPark, "2026-09-10", inc)) {
			t.Fatalf("includeCompleted %v: a batched obligation's due day (its drive is 09-12) must not read as work", inc)
		}
	}
	// ...while an obligation with NO drive is work on its due day.
	execST(t, ctx, pool, `UPDATE obligation_instances SET batch_id = NULL WHERE tenant_id = $1::uuid AND obligation_id <> '00000000-0000-4000-8000-00000000d708'::uuid`, stTenant)
	if !precheckAnswer(t, ctx, pool, vaccinationDueWorkPrecheckSQL, precheckArgs(stPark, "2026-09-10", true)) {
		t.Fatal("an unbatched obligation due 09-10 must read as work on 09-10")
	}
	check("unbatched due day", true)

	// Only completed work left: the includeCompleted flag decides.
	execST(t, ctx, pool, `UPDATE obligation_instances SET status = 'completed' WHERE tenant_id = $1::uuid`, stTenant)
	check("completed only", true)
	// Canceled work never counts, whatever the date source.
	execST(t, ctx, pool, `UPDATE obligation_instances SET status = 'canceled' WHERE tenant_id = $1::uuid AND obligation_id <> '00000000-0000-4000-8000-00000000d708'::uuid`, stTenant)
	execST(t, ctx, pool, `UPDATE obligation_instances SET status = 'scheduled', due_at = '2026-12-01 00:00+05:30' WHERE obligation_id = '00000000-0000-4000-8000-00000000d708'::uuid`)
	check("member assignment only", true)
	// A non-vaccination protocol hides everything.
	execST(t, ctx, pool, `UPDATE protocol_definitions SET category = 'deworming' WHERE protocol_id = $1::uuid`, stProtocol)
	check("not vaccination", false)
}

// TestVaccinationDueWorkPrecheckNeverWalksTheTenantsObligations is the plan-shape guard: over a
// seeded two-year obligation history, every read of obligation_instances must be bounded by the day (due_at) or by a batch / obligation key from an
// already day-bounded arm -- never an index walk with only tenant_id bound, which is what the
// legacy OR produced on stg.
func TestVaccinationDueWorkPrecheckNeverWalksTheTenantsObligations(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedDrivePen(t, ctx, pool)

	// History at a realistic shape: ~12k goat obligations (a third batched) spread over eight months plus
	// 300 past batches with their obligations, so the planner costs the arms against a tenant
	// history (a handful of seeded rows would make any whole-table join look cheapest).
	execST(t, ctx, pool, `
INSERT INTO obligation_batches (batch_id, tenant_id, protocol_version_id, scope_type, scope_id, status, planned_date)
SELECT gen_random_uuid(), $1::uuid, $2::uuid, 'shed', $3::uuid, 'completed', DATE '2024-01-01' + i
FROM generate_series(1, 300) i`, stTenant, stVersion, stShed)
	execST(t, ctx, pool, `
INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, rule_id, batch_id, target_type, target_id, scope_type, scope_id, due_at, status, idempotency_key, sequence)
SELECT gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid,
       CASE WHEN i % 3 = 0 THEN (SELECT batch_id FROM obligation_batches WHERE tenant_id = $1::uuid AND planned_date = DATE '2024-01-01' + (1 + i % 300)) END,
       'goat', g.goat_id, 'shed', $4::uuid,
       TIMESTAMPTZ '2024-06-01 00:00+05:30' + i * interval '4 hours',
       CASE WHEN i % 5 = 0 THEN 'canceled' ELSE 'completed' END,
       'hist-' || i || '-' || g.goat_id, 100 + i
FROM generate_series(1, 1500) i
CROSS JOIN (SELECT goat_id FROM goats WHERE tenant_id = $1::uuid) g`, stTenant, stVersion, stRule, stShed)
	// Every past batch had its drive: one assignment on its planned day whose members are the
	// batch's obligations -- the member table is as long as the batched history, as on stg.
	execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignments (assignment_id, tenant_id, batch_id, planned_date, operator_id, park_id, shed_id, physical_shed, partition_label, animal_count)
SELECT gen_random_uuid(), $1::uuid, ob.batch_id, ob.planned_date, $2::uuid, $3::uuid, $4::uuid, 'Godel 1', 'whole', 1
FROM obligation_batches ob
WHERE ob.tenant_id = $1::uuid AND ob.status = 'completed'`, stTenant, stMember, stPark, stShed)
	execST(t, ctx, pool, `
INSERT INTO vaccination_drive_assignment_members (tenant_id, assignment_id, obligation_id, goat_id)
SELECT oi.tenant_id, vda.assignment_id, oi.obligation_id, oi.target_id
FROM obligation_instances oi
JOIN vaccination_drive_assignments vda ON vda.tenant_id = oi.tenant_id AND vda.batch_id = oi.batch_id
WHERE oi.tenant_id = $1::uuid AND oi.idempotency_key LIKE 'hist-%'`, stTenant)

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	for _, stmt := range []string{`ANALYZE obligation_instances`, `ANALYZE obligation_batches`, `ANALYZE vaccination_drive_assignments`, `ANALYZE vaccination_drive_assignment_members`, `ANALYZE goats`} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	unbounded, usedPartial, planJSON := obligationReadsOfPrecheckPlan(t, ctx, conn.Conn(), vaccinationDueWorkPrecheckSQL, precheckArgs(stPark, "2026-09-24", true))
	if unbounded != "" {
		t.Fatalf("obligation_instances read with no day/batch/obligation bound (%s):\n%s", unbounded, planJSON)
	}
	if !usedPartial {
		t.Fatalf("the due-date arm must use obligation_instances_goat_live_due_idx:\n%s", planJSON)
	}
	// The guard is not vacuous: over the same data the legacy single-OR form is the tenant walk.
	if legacyUnbounded, _, legacyPlan := obligationReadsOfPrecheckPlan(t, ctx, conn.Conn(), legacyVaccinationDueWorkPrecheckSQL, precheckArgs(stPark, "2026-09-24", true)[:5]); legacyUnbounded == "" {
		t.Fatalf("legacy precheck unexpectedly bounded; the plan guard no longer discriminates:\n%s", legacyPlan)
	}
}

// obligationReadsOfPrecheckPlan EXPLAINs sql and reports the first obligation_instances read whose
// index condition bounds neither the day (due_at) nor a batch / obligation key, and whether the
// partial due index was used.
func obligationReadsOfPrecheckPlan(t *testing.T, ctx context.Context, conn *pgx.Conn, sql string, args []any) (unbounded string, usedPartial bool, planJSON []byte) {
	t.Helper()
	if err := conn.QueryRow(ctx, `EXPLAIN (FORMAT JSON) `+sql, args...).Scan(&planJSON); err != nil {
		t.Fatal(err)
	}
	var plan []map[string]any
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		t.Fatal(err)
	}
	var nodes []map[string]any
	var walk func(n map[string]any)
	walk = func(n map[string]any) {
		if rel, _ := n["Relation Name"].(string); rel == "obligation_instances" {
			nodes = append(nodes, n)
		}
		kids, _ := n["Plans"].([]any)
		for _, k := range kids {
			if km, ok := k.(map[string]any); ok {
				walk(km)
			}
		}
	}
	walk(plan[0]["Plan"].(map[string]any))
	if len(nodes) == 0 {
		t.Fatalf("no obligation_instances access in plan: %s", planJSON)
	}
	for _, n := range nodes {
		cond, _ := n["Index Cond"].(string)
		if cond == "" {
			cond, _ = n["Recheck Cond"].(string)
		}
		bounded := strings.Contains(cond, "due_at") || strings.Contains(cond, "batch_id") || strings.Contains(cond, "obligation_id")
		if !bounded && unbounded == "" {
			unbounded = fmt.Sprintf("%v %v cond %q", n["Node Type"], n["Index Name"], cond)
		}
		if name, _ := n["Index Name"].(string); name == "obligation_instances_goat_live_due_idx" {
			usedPartial = true
		}
	}
	return unbounded, usedPartial, planJSON
}

// vaccinationDueWorkPrecheckArgs binds vaccinationDueWorkPrecheckSQL exactly as
// hasVaccinationDueWork does: $3/$4 the IST day as instants, $6/$7 the same day as dates
// (dayStart must be an IST midnight). Kept in lockstep with hasVaccinationDueWork.
func vaccinationDueWorkPrecheckArgs(tenantID, parkID string, dayStart time.Time, includeCompleted bool) []any {
	dayEnd := dayStart.AddDate(0, 0, 1)
	return []any{tenantID, parkID, dayStart, dayEnd, includeCompleted, dayStart.Format("2006-01-02"), dayEnd.Format("2006-01-02")}
}
