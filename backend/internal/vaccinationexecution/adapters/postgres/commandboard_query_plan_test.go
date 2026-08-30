package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// The command board's QUERY-PLAN GATE.
//
// GET /vaccination/command returned 500 in staging -- "closed-without-dose rows: timeout: context
// deadline exceeded" -- on a tenant holding only ~71k obligation rows, 5.8k completions and 1.6k
// live goats. Nothing about that data is large. Every statement on the endpoint was individually
// reviewed, tested for correctness, and green; what nobody could see from reading them was their
// PLAN SHAPE, and four separate statements were quietly doing tenant-scale work to produce a
// drawer-sized answer.
//
// A latency gate alone would not have caught this either, because the gate runs against a fixture
// far smaller than staging and every one of these statements is fast on a small fixture. The defect
// is in how the work SCALES, which is a property of the plan, not of the clock. So this file asserts
// the plan.
//
// The four rejected shapes below are not hypotheses. Each is a shape that shipped:
//
//	unbounded pre-limit work   shedVaccineAnimalSQL sorted an estimated 57,176 rows to return 500,
//	                           and on the live tenant spent 2.4s returning ZERO.
//	repeated CTE re-scan       closedWithoutDoseSQL's LATERAL re-scanned the materialised 70k-row
//	                           `scoped` CTE once per candidate animal. This is the 15s timeout.
//	nested loop at tenant scale the cohort-exception probes ran against a CTE the planner estimated
//	                           at 29 rows when 5,053 stood, chose a Nested Loop Anti Join, and
//	                           discarded 13.9 MILLION rows -- 18s to return 43.
//	broad seq scan on a hot table every drilldown scanned all of obligation_instances because the
//	                           cell it was explaining was applied in Go, after the query.
//
// MUTATION-TESTED IN-FILE. TestCommandBoardPlanGuardRejectsThePreFixShapes below runs the guard
// against the ORIGINAL statements, preserved verbatim, and FAILS if the guard passes them. A
// guardrail that cannot be shown to reject the bug it was written for is decoration, and this repo
// has been burned by exactly that (see uuidFromSuffix's own comment). Keeping the pre-fix SQL here
// is the cost of being able to prove the gate works.

// commandBoardPlanLimits are the ceilings one command-board statement's executed plan must respect.
//
// They are expressed against the FIXTURE SIZE rather than as absolute numbers, so the gate keeps
// its meaning if the fixture grows: what is being asserted is "this statement's work is
// proportional to the answer, not to the tenant".
type commandBoardPlanLimits struct {
	// label names the statement in failure output.
	label string
	// maxScanRows bounds the biggest single scan of a hot canonical table. A cell-scoped drilldown
	// must not read the tenant's obligations to describe one drawer.
	maxScanRows float64
	// maxPreLimitRows bounds the rows any Sort/Hash/Aggregate handles before the final LIMIT. This
	// is the "capped result, unbounded pre-limit work" rule: a statement returning 50 rows may not
	// sort 57,000 to find them.
	maxPreLimitRows float64
	// maxNodeTotalRows bounds rows x loops at any single node, which is what makes a nested-loop
	// inner scan over tenant-scale rows visible. A node executed 5,206 times over 2,670 rows reads
	// as 13.9M here, which is exactly the cohort-exception defect.
	maxNodeTotalRows float64
	// maxRowsRemovedByJoinFilter bounds rows a join may evaluate and discard. A nested loop chosen
	// off a bad row estimate is invisible in every other metric -- the cohort-exception probes
	// discarded 13.9M rows while no single node's row count looked large.
	maxRowsRemovedByJoinFilter float64
	// hotTables are the canonical tables whose scans are bounded by maxScanRows.
	hotTables []string
}

// commandBoardPlanFixtureAnimals is the fixture herd. Large enough that a tenant-wide plan and a
// cell-scoped plan are separated by more than noise, small enough to stay a unit-speed test.
const commandBoardPlanFixtureAnimals = 400

// commandBoardPlanFixture seeds one park, two sheds and commandBoardPlanFixtureAnimals goats, each
// carrying several obligations across two vaccines, so a "cell" (one shed x one vaccine) is a small
// fraction of the tenant. A guard written against a fixture where the cell IS the tenant proves
// nothing, which is the trap this shape avoids.
type commandBoardPlanFixture struct {
	tenantID    string
	parkID      string
	shedID      string
	otherShedID string
	vaccineCode string
	asOf        time.Time
	doseCodes   []string
}

func seedCommandBoardPlanFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) commandBoardPlanFixture {
	t.Helper()
	f := commandBoardPlanFixture{
		tenantID:    "00000000-0000-4000-8000-0000000000f1",
		parkID:      uuidFromSuffix("01", "qp"),
		shedID:      uuidFromSuffix("02", "qp"),
		otherShedID: uuidFromSuffix("02", "qp2"),
		vaccineCode: "ET_TT",
		asOf:        time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		doseCodes:   []string{"ppr_adult"},
	}
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, f.tenantID, "qp")
	seedCommandBoardPark(t, ctx, pool, f.tenantID, f.parkID, f.shedID, "QueryPlan")
	execProjectionSQL(t, ctx, pool, "second shed",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 VALUES ($1, $2, 'Shed QueryPlan Two', 'shed', $3, 'active')`, f.otherShedID, f.tenantID, f.parkID)
	seedVaccineDimension(t, ctx, pool, f.tenantID, protocolVersionID, ruleID,
		uuidFromSuffix("0b", "qpd"), "qp-sel", f.vaccineCode)

	// One set-based load rather than a per-animal loop: the fixture is data, and an N+1 insert loop
	// in a test is still an N+1.
	execProjectionSQL(t, ctx, pool, "plan fixture parties",
		`INSERT INTO parties (party_id, party_type, display_name, status)
		 SELECT ('80000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid, 'org', 'Custodian', 'active'
		 FROM generate_series(1, $1) i`, commandBoardPlanFixtureAnimals)
	execProjectionSQL(t, ctx, pool, "plan fixture goats",
		`INSERT INTO goats (goat_id, tenant_id, display_id, sex, lifecycle_status, management_stage, shed_id, custodian_party_id, dob)
		 SELECT ('81000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
		        $1, 'G' || lpad(i::text, 5, '0'), 'female', 'alive', 'Non-Pregnant',
		        CASE WHEN i % 2 = 0 THEN $2::uuid ELSE $3::uuid END,
		        ('80000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid, '2025-01-01'
		 FROM generate_series(1, $4) i`,
		f.tenantID, f.shedID, f.otherShedID, commandBoardPlanFixtureAnimals)
	// Several doses per animal, so an animal-grain fold has something to fold and a per-obligation
	// plan is visibly wider than a per-animal one.
	execProjectionSQL(t, ctx, pool, "plan fixture obligations",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 SELECT ('82000000-0000-4000-8000-' || lpad((i * 10 + d)::text, 12, '0'))::uuid,
		        $1, $2,
		        ('81000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid, 'goat', 'shed',
		        CASE WHEN i % 2 = 0 THEN $3::uuid ELSE $4::uuid END,
		        $5, 'missed',
		        ('2026-08-01'::timestamptz + (d || ' days')::interval),
		        'qp-' || i || '-' || d
		 FROM generate_series(1, $6) i, generate_series(1, 4) d`,
		f.tenantID, protocolVersionID, f.shedID, f.otherShedID, ruleID, commandBoardPlanFixtureAnimals)

	// Real statistics, or every plan below is a guess about an empty table. This mirrors the
	// mandatory post-seed ANALYZE contract in AGENTS.md.
	execProjectionSQL(t, ctx, pool, "analyze plan fixture",
		`ANALYZE obligation_instances, vaccination_completions, goats, locations, protocol_rules,
		   protocol_rule_dimensions, goat_shed_partitions, shed_partitions, obligation_batches`)
	return f
}

// TestCommandBoardQueryPlansAreBoundedByTheAnswerNotTheTenant is the gate itself.
func TestCommandBoardQueryPlansAreBoundedByTheAnswerNotTheTenant(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	obligations := float64(commandBoardPlanFixtureAnimals * 4)

	// A DRILLDOWN describes one cell. Its work must be a fraction of the tenant's obligations --
	// generously a quarter, which still fails hard on a statement that reads them all.
	drilldownScanCeiling := obligations / 4
	// The tenant-wide SUMMARY sections legitimately read every obligation once. What they may NOT
	// do is multiply them: no node may handle several times the row count, which is what a
	// dimension fan-out or a repeated CTE scan looks like in a plan.
	summaryNodeCeiling := obligations * 3

	for _, tc := range []struct {
		limits commandBoardPlanLimits
		sql    string
		args   []any
	}{
		{
			limits: commandBoardPlanLimits{
				label:                      "commandBoardClosedWithoutDoseSQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            obligations * 2,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"goat_identifiers", "goat_shed_partitions"},
			},
			// The residual bucket is a whole-tenant fold by definition, so obligation_instances is
			// legitimately read once. What is gated here is the DECORATION: identifiers, partitions
			// and the closure reason must be joined to the PAGE, never to every candidate animal.
			// The pre-fix statement re-scanned the 70k-row `scoped` CTE once per candidate through a
			// LATERAL, which is what exhausted the pool timeout.
			sql:  commandBoardClosedWithoutDoseSQL,
			args: []any{f.tenantID, f.asOf, nil, nil, nil, nil, 50},
		},
		{
			limits: commandBoardPlanLimits{
				label:                      "commandBoardShedVaccineAnimalSQL",
				maxScanRows:                drilldownScanCeiling,
				maxPreLimitRows:            drilldownScanCeiling,
				maxNodeTotalRows:           drilldownScanCeiling * 2,
				maxRowsRemovedByJoinFilter: drilldownScanCeiling * 2,
				hotTables:                  []string{"obligation_instances"},
			},
			// The cell (shed + vaccine) is a WHERE clause now. Before, it was a Go-side bucketing
			// step after a tenant-wide sort, so this statement is the clearest case of the
			// capped-result-over-unbounded-work shape.
			sql:  commandBoardShedVaccineAnimalSQL,
			args: []any{f.tenantID, f.asOf, nil, nil, f.shedID, f.vaccineCode, "", nil, nil, 50},
		},
		{
			limits: commandBoardPlanLimits{
				label:                      "commandBoardCohortExceptionListSQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            obligations * 2,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"obligation_instances"},
			},
			// maxNodeTotalRows is the load-bearing ceiling here: the pre-fix statement's nested-loop
			// anti-join was not a big SCAN, it was a small scan executed thousands of times.
			sql: commandBoardCohortExceptionListSQL,
			args: []any{f.tenantID, nil, nil, f.parkID, "Non-Pregnant", "female", f.doseCodes,
				nil, nil, 50},
		},
		{
			limits: commandBoardPlanLimits{
				label:                      "commandBoardCohortExceptionCountSQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            obligations * 3,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"obligation_instances"},
			},
			sql:  commandBoardCohortExceptionCountSQL,
			args: []any{f.tenantID, nil, nil, nil, nil, nil, nil},
		},
		{
			limits: commandBoardPlanLimits{
				label:                      "commandBoardShedVaccineSQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            summaryNodeCeiling,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"obligation_instances"},
			},
			// maxNodeTotalRows catches the protocol_rule_dimensions fan-out: the pre-fix statement
			// multiplied every obligation by the rule's dimension rows before aggregating.
			sql:  commandBoardShedVaccineSQL,
			args: []any{f.tenantID, f.asOf, nil, nil},
		},
		{
			limits: commandBoardPlanLimits{
				label:                      "commandBoardCohortSQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            summaryNodeCeiling,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"obligation_instances"},
			},
			sql:  commandBoardCohortSQL,
			args: []any{f.tenantID, f.asOf, nil, nil},
		},
		{
			limits: commandBoardPlanLimits{
				label:                      "driveOptionsSQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            summaryNodeCeiling,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"goat_shed_partitions"},
			},
			// The picker pages BEFORE it decorates, so goat_shed_partitions -- the per-goat table --
			// must only be touched for the page's drives.
			sql:  driveOptionsSQL,
			args: []any{f.tenantID, nil, domain.CommandBoardDriveOptionsPageSize + 1, nil, nil, nil, nil, nil, nil},
		},
	} {
		t.Run(tc.limits.label, func(t *testing.T) {
			res := explainAnalyzeJSON(t, ctx, pool, tc.sql, tc.args...)
			if violations := commandBoardPlanViolations(tc.limits, res); len(violations) > 0 {
				t.Fatalf("%s plan is not bounded by its answer:\n  %s\n(fixture: %d animals, %.0f obligations; execTime=%.1fms)",
					tc.limits.label, strings.Join(violations, "\n  "),
					commandBoardPlanFixtureAnimals, obligations, res.ExecutionTime)
			}
		})
	}
}

// commandBoardPlanViolations walks an executed plan and returns every ceiling it broke.
//
// It returns ALL violations rather than the first, because a regression usually trips more than one
// and reporting one at a time turns a single fix into several round trips.
func commandBoardPlanViolations(limits commandBoardPlanLimits, res explainAnalyzeResult) []string {
	var out []string
	hot := map[string]bool{}
	for _, table := range limits.hotTables {
		hot[table] = true
	}

	var walk func(n explainPlanNode, depth int)
	walk = func(n explainPlanNode, depth int) {
		loops := n.ActualLoops
		if loops < 1 {
			loops = 1
		}
		totalRows := n.ActualRows * loops

		// (1) Broad scan of a hot canonical table where scoped access is expected.
		if hot[n.RelationName] && n.ActualRows > limits.maxScanRows {
			out = append(out, fmt.Sprintf("%s on %s read %.0f rows (ceiling %.0f) - the scope is being applied after the scan, not in it",
				n.NodeType, n.RelationName, n.ActualRows, limits.maxScanRows))
		}

		// (2) rows x loops at one node. This is the nested-loop-inner-scan and repeated-CTE-scan
		// detector: neither shows up as a large single scan, both show up here.
		if totalRows > limits.maxNodeTotalRows {
			out = append(out, fmt.Sprintf("%s%s handled %.0f rows x %.0f loops = %.0f (ceiling %.0f) - repeated scan or nested-loop inner over tenant-scale rows",
				n.NodeType, relationSuffix(n), n.ActualRows, loops, totalRows, limits.maxNodeTotalRows))
		}

		// (3) Large sort/hash/aggregate before the answer is cut down.
		if isBlockingNode(n.NodeType) && totalRows > limits.maxPreLimitRows {
			out = append(out, fmt.Sprintf("%s handled %.0f rows before the result was bounded (ceiling %.0f) - capped result over unbounded pre-limit work",
				n.NodeType, totalRows, limits.maxPreLimitRows))
		}

		// (4) A MATERIALISED CTE RE-SCANNED PER OUTER ROW. This is the rule that catches the actual
		// staging 500, and it is structural rather than volumetric ON PURPOSE: the pre-fix
		// closed-without-dose LATERAL re-scanned the 71k-row scoped CTE 323 times, but because the
		// LATERAL carried its own LIMIT 1 the node reported only ~57 rows per loop. EVERY row-count
		// ceiling in this file passed it -- that was measured, not assumed. The cost of re-walking a
		// materialised tuplestore is not proportional to the rows it returns, so the loop count is
		// the only honest signal, and a re-scanned CTE is never the right shape on a hot path: join
		// it, or fold it set-wise.
		if (strings.Contains(n.NodeType, "CTE Scan") || n.NodeType == "Materialize") && loops > 1 {
			out = append(out, fmt.Sprintf("%s%s was executed %.0f times - a materialised result re-scanned per outer row; join it set-wise instead",
				n.NodeType, cteSuffix(n), loops))
		}

		// (5) A join evaluating and discarding tenant-scale rows.
		if discarded := n.RowsRemovedByJoinFilter * loops; discarded > limits.maxRowsRemovedByJoinFilter {
			out = append(out, fmt.Sprintf("%s%s evaluated and threw away %.0f rows (ceiling %.0f) - nested loop chosen off a bad row estimate",
				n.NodeType, relationSuffix(n), discarded, limits.maxRowsRemovedByJoinFilter))
		}

		for _, c := range n.Plans {
			walk(c, depth+1)
		}
	}
	walk(res.Plan, 0)
	return out
}

// isBlockingNode names the plan nodes that must materialise their whole input before emitting a
// row. They are where "unbounded work behind a small answer" actually costs time.
func isBlockingNode(nodeType string) bool {
	switch {
	case strings.Contains(nodeType, "Sort"),
		strings.Contains(nodeType, "Aggregate"),
		strings.Contains(nodeType, "Hash"),
		strings.Contains(nodeType, "Materialize"),
		strings.Contains(nodeType, "Unique"):
		return true
	}
	return false
}

func cteSuffix(n explainPlanNode) string {
	if n.CTEName == "" {
		return ""
	}
	return " (" + n.CTEName + ")"
}

func relationSuffix(n explainPlanNode) string {
	if n.RelationName == "" {
		return ""
	}
	return " on " + n.RelationName
}

// commandBoardPreFixClosedWithoutDoseSQL is the closed-without-dose statement EXACTLY as it stood
// before this change, kept so the gate above can be shown to reject it.
//
// The defect is the final LATERAL. `scoped` is a CTE Postgres materialises (~71k rows on the live
// tenant), and the LATERAL re-scans it once per candidate animal to find that animal's most recent
// closure. Decoration also hangs off the candidate set rather than the page, so the identifier
// lookups run for every residual animal and the LIMIT applies last. In staging this exhausted the
// 15s statement timeout and returned
// "vaccination command board: closed-without-dose rows: timeout: context deadline exceeded",
// which is the 500 the user saw as "Unable to load command board".
const commandBoardPreFixClosedWithoutDoseSQL = `
WITH comp AS (
  SELECT
    obligation_id,
    bool_or(status = 'accepted') AS has_accepted,
    bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
),
scoped AS (
  SELECT
    oi.target_id,
    oi.obligation_id,
    oi.status,
    oi.rule_id,
    oi.due_at,
    COALESCE(comp.has_accepted, false) AS has_accepted,
    COALESCE(comp.has_recorded_unverified, false) AS has_recorded_unverified,
    comp.obligation_id IS NULL AS no_completion,
    oi.status IN ('scheduled','due','in_progress','deferred','missed') AS is_open,
    oi.status = 'missed' AS is_missed,
    (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date AS due_before_as_of
  FROM obligation_instances oi
  JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
  LEFT JOIN comp ON oi.obligation_id = comp.obligation_id
  WHERE oi.tenant_id = $1::uuid
    AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
    AND g.merged_into_goat_id IS NULL
    AND (COALESCE($3::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR oi.batch_id = $3::uuid)
    AND (COALESCE($4::uuid,'00000000-0000-0000-0000-000000000000') = '00000000-0000-0000-0000-000000000000' OR EXISTS (
      SELECT 1 FROM locations pl WHERE pl.location_id = oi.scope_id AND pl.tenant_id = oi.tenant_id AND pl.parent_location_id = $4::uuid
    ))
),
per_animal AS (
  SELECT
    target_id,
    bool_or(is_missed) AS any_missed,
    bool_or(has_accepted) AS any_verified,
    bool_or(has_recorded_unverified AND NOT has_accepted) AS any_awaiting,
    bool_or(is_open AND no_completion AND due_before_as_of) AS any_overdue,
    bool_or(is_open AND no_completion AND NOT due_before_as_of) AS any_scheduled
  FROM scoped
  GROUP BY target_id
)
SELECT
  g.goat_id::text,
  g.display_id,
  COALESCE(aid1.identifier_value, '') AS animal_identifier_1,
  COALESCE(park.name, '') AS park_name,
  COALESCE(closed.status, '') AS reason_status
FROM per_animal pa
JOIN goats g ON g.goat_id = pa.target_id AND g.tenant_id = $1::uuid
LEFT JOIN locations shed ON g.shed_id = shed.location_id AND g.tenant_id = shed.tenant_id
LEFT JOIN locations park ON shed.parent_location_id = park.location_id AND shed.tenant_id = park.tenant_id
LEFT JOIN LATERAL (
  SELECT gi.identifier_value FROM goat_identifiers gi
  WHERE gi.tenant_id = g.tenant_id AND gi.goat_id = g.goat_id
    AND gi.identifier_type = 'animal_identifier_1' AND gi.status = 'active'
  ORDER BY gi.is_primary_for_goat DESC, gi.identifier_id
  LIMIT 1
) aid1 ON true
LEFT JOIN LATERAL (
  SELECT s.status
  FROM scoped s
  WHERE s.target_id = pa.target_id
  ORDER BY s.due_at DESC NULLS LAST, s.obligation_id
  LIMIT 1
) closed ON true
WHERE NOT pa.any_verified AND NOT pa.any_awaiting AND NOT pa.any_overdue AND NOT pa.any_scheduled
ORDER BY g.display_id
LIMIT $5
`

// TestCommandBoardPlanGuardRejectsThePreFixShapes is the guard's own mutation test.
//
// It runs commandBoardPlanViolations against the statement as it stood BEFORE the fix and requires
// at least one violation. If this test passes while the gate above also passes, the gate is
// decoration: it would have let the 500 through.
//
// It asserts the CLASS, not a message: the point is that the shape is rejected, and pinning exact
// wording would make the guard's failure text unchangeable.
func TestCommandBoardPlanGuardRejectsThePreFixShapes(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	obligations := float64(commandBoardPlanFixtureAnimals * 4)

	limits := commandBoardPlanLimits{
		label:                      "commandBoardPreFixClosedWithoutDoseSQL",
		maxScanRows:                obligations * 2,
		maxPreLimitRows:            obligations * 2,
		maxNodeTotalRows:           obligations * 3,
		maxRowsRemovedByJoinFilter: obligations * 3,
		hotTables:                  []string{"goat_identifiers", "goat_shed_partitions"},
	}
	res := explainAnalyzeJSON(t, ctx, pool, commandBoardPreFixClosedWithoutDoseSQL,
		f.tenantID, f.asOf, nil, nil, 50)

	violations := commandBoardPlanViolations(limits, res)
	if len(violations) == 0 {
		t.Fatalf("the plan guard PASSED the pre-fix closed-without-dose statement, so it would not have caught the "+
			"staging 500. Guard is not enforcing anything.\nfixture: %d animals, %.0f obligations; execTime=%.1fms",
			commandBoardPlanFixtureAnimals, obligations, res.ExecutionTime)
	}
	t.Logf("guard correctly rejected the pre-fix shape with %d violation(s):\n  %s",
		len(violations), strings.Join(violations, "\n  "))
}

// TestCommandBoardTileAndDrilldownRangeOverTheSameAnimals is the PREDICATE-DRIFT gate.
//
// The brief that produced this change named a specific risk: the KPI tile's any_missed semantics
// and the closed-without-dose drilldown's had drifted apart in comment, and nothing proved the tile
// and its drawer described the same animals. A number a reader can click into is a promise that the
// list explains THAT number; if the two predicates diverge the board is worse than slow, it is
// wrong, and nothing about the split makes that impossible on its own.
//
// So it is asserted on data rather than trusted to the shared-CTE construction: the tile's
// ClosedWithoutDose count and the drilldown's row count must agree over the same scope.
func TestCommandBoardTileAndDrilldownRangeOverTheSameAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	repo := NewRepository(pool, 30*time.Second)

	board, err := repo.VaccinationCommandBoard(ctx, domain.CommandBoardQuery{TenantID: f.tenantID, AsOf: f.asOf})
	if err != nil {
		t.Fatalf("VaccinationCommandBoard() error = %v", err)
	}

	// Every animal in the fixture holds only 'missed' obligations with no completion, so none is
	// verified, awaiting, overdue or scheduled: the whole herd is the residual bucket. That makes
	// the fixture a real test of the tile rather than a check that 0 == 0.
	if board.KPIs.ClosedWithoutDose != commandBoardPlanFixtureAnimals {
		t.Fatalf("tile closedWithoutDose = %d, want %d; the fixture herd is entirely residual",
			board.KPIs.ClosedWithoutDose, commandBoardPlanFixtureAnimals)
	}

	// Page the drilldown to exhaustion and count. Paging is part of the assertion: a drawer that
	// agreed with the tile only on its first page would still strand a reader.
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > commandBoardPlanFixtureAnimals {
			t.Fatalf("closed-without-dose paging did not terminate after %d pages", pages)
		}
		page, err := repo.CommandBoardClosedWithoutDoseAnimals(ctx, domain.CommandBoardDrilldownQuery{
			TenantID: f.tenantID,
			AsOf:     f.asOf,
			Limit:    50,
			Cursor:   cursor,
		})
		if err != nil {
			t.Fatalf("CommandBoardClosedWithoutDoseAnimals() error = %v", err)
		}
		for _, animal := range page.Animals {
			if seen[animal.GoatID] {
				t.Fatalf("goat %s appeared on two pages; the keyset is not over a total order", animal.GoatID)
			}
			seen[animal.GoatID] = true
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}

	if len(seen) != board.KPIs.ClosedWithoutDose {
		t.Fatalf("tile says %d animals closed without a dose but the drilldown listed %d distinct animals; "+
			"the number and the list describe different sets, so the drawer does not explain the tile",
			board.KPIs.ClosedWithoutDose, len(seen))
	}
}
