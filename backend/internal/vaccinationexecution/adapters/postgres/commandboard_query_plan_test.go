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

// commandBoardPlanFixtureSheds spreads the herd so ONE CELL IS A SMALL SHARE OF THE TENANT.
//
// With two sheds a cell held half the herd and a cell-scoped read was within a factor of two of a
// tenant-wide one -- no ceiling can separate those, so the gate would have been asserting nothing
// about scoping. Eight sheds put ~50 animals (200 obligations) in a cell against 1,600 in the
// tenant, which is the separation the drilldown ceilings are written against.
const commandBoardPlanFixtureSheds = 8

// commandBoardPlanFixtureResidual is how many animals hold ONLY closed-with-no-dose obligations.
//
// The fixture cannot be uniformly 'missed': missed is its own KPI bucket and is excluded from
// closed_without_dose, so a herd of missed animals gives a residual bucket of ZERO -- and the
// closed-without-dose statement then has no candidate rows, its LATERAL never executes, and the
// mutation test proving the guard catches that LATERAL passes vacuously. Every fourth animal is
// therefore 'canceled' with no completion, which is the real residual shape.
const commandBoardPlanFixtureResidual = commandBoardPlanFixtureAnimals / 4

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
		shedID:      "85000000-0000-4000-8000-000000000000",
		otherShedID: "85000000-0000-4000-8000-000000000001",
		vaccineCode: "ET_TT",
		asOf:        time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		doseCodes:   []string{"ppr_adult"},
	}
	protocolVersionID, ruleID := seedCommandBoardProtocol(t, ctx, pool, f.tenantID, "qp")
	seedCommandBoardPark(t, ctx, pool, f.tenantID, f.parkID, f.shedID, "QueryPlan")
	execProjectionSQL(t, ctx, pool, "plan fixture sheds",
		`INSERT INTO locations (location_id, tenant_id, name, location_type, parent_location_id, status)
		 SELECT ('85000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
		        $1, 'Shed QueryPlan ' || i, 'shed', $2, 'active'
		 FROM generate_series(1, $3) i`,
		f.tenantID, f.parkID, commandBoardPlanFixtureSheds-1)
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
		        $1, 'G-' || lpad(i::text, 6, '0'), 'female', 'alive', 'Non-Pregnant',
		        ('85000000-0000-4000-8000-' || lpad((i % $2)::text, 12, '0'))::uuid,
		        ('80000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid, '2025-01-01'
		 FROM generate_series(1, $3) i`,
		f.tenantID, commandBoardPlanFixtureSheds, commandBoardPlanFixtureAnimals)
	// Several doses per animal, so an animal-grain fold has something to fold and a per-obligation
	// plan is visibly wider than a per-animal one.
	execProjectionSQL(t, ctx, pool, "plan fixture obligations",
		`INSERT INTO obligation_instances (obligation_id, tenant_id, protocol_version_id, target_id, target_type, scope_type, scope_id, rule_id, status, due_at, idempotency_key)
		 SELECT ('82000000-0000-4000-8000-' || lpad((i * 10 + d)::text, 12, '0'))::uuid,
		        $1, $2,
		        ('81000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid, 'goat', 'shed',
		        ('85000000-0000-4000-8000-' || lpad((i % $3)::text, 12, '0'))::uuid,
		        $4,
		        -- Every fourth animal is CLOSED WITH NO DOSE (the residual bucket the tile counts
		        -- and the drawer lists); the rest are 'missed', which is the behind shape the
		        -- shed-vaccine cell reports. A single-status herd makes one of the two empty.
		        CASE WHEN i % 4 = 1 THEN 'canceled' ELSE 'missed' END,
		        ('2026-08-01'::timestamptz + (d || ' days')::interval),
		        'qp-' || i || '-' || d
		 FROM generate_series(1, $5) i, generate_series(1, 4) d`,
		f.tenantID, protocolVersionID, commandBoardPlanFixtureSheds, ruleID, commandBoardPlanFixtureAnimals)

	// THE DECORATING TABLES ARE SEEDED, and this is not fixture padding.
	//
	// hotTables for the closed-without-dose and drive-option statements are goat_identifiers and
	// goat_shed_partitions -- the per-goat tables whose access must stay bounded to the page. Left
	// empty, those scans return zero rows and detector (1) can NEVER fire for either statement: the
	// ceiling would be compared against nothing and the gate would pass any regression touching
	// them. A guard that cannot fail on its own subject is decoration.
	execProjectionSQL(t, ctx, pool, "plan fixture identifiers",
		`INSERT INTO goat_identifiers (identifier_id, tenant_id, goat_id, identifier_type, identifier_value,
		   normalized_value, scope_key, is_primary_for_goat, status, valid_from, normalizer_version)
		 SELECT ('83000000-0000-4000-8000-' || lpad((i * 10 + k)::text, 12, '0'))::uuid,
		        $1,
		        ('81000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
		        CASE k WHEN 1 THEN 'animal_identifier_1' ELSE 'animal_identifier_2' END,
		        'TAG' || i || '-' || k, 'tag' || i || '-' || k, 'tenant', k = 1, 'active', now(), 'v1'
		 FROM generate_series(1, $2) i, generate_series(1, 2) k`,
		f.tenantID, commandBoardPlanFixtureAnimals)
	execProjectionSQL(t, ctx, pool, "plan fixture partitions",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name, updated_at)
		 SELECT $1,
		        ('81000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
		        ('85000000-0000-4000-8000-' || lpad((i % $2)::text, 12, '0'))::uuid,
		        'Part ' || ((i % 3) + 1), 'Shed QueryPlan', now()
		 FROM generate_series(1, $3) i`,
		f.tenantID, commandBoardPlanFixtureSheds, commandBoardPlanFixtureAnimals)
	// Completions, so the comp CTE is an aggregate over real rows rather than an empty-relation
	// plan the planner treats as free. Every fourth animal's first dose is accepted, which also
	// makes the residual bucket smaller than the herd -- a drilldown whose filter selects everything
	// is not a test of a filter.
	execProjectionSQL(t, ctx, pool, "plan fixture completions",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status,
		   adverse_reaction, cold_chain_verified, administered_at, idempotency_key)
		 SELECT ('84000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
		        $1,
		        ('82000000-0000-4000-8000-' || lpad((i * 10 + 1)::text, 12, '0'))::uuid,
		        ('81000000-0000-4000-8000-' || lpad(i::text, 12, '0'))::uuid,
		        'accepted', false, true, '2026-08-02'::timestamptz, 'qp-completion-' || i
		 FROM generate_series(1, $2) i WHERE i % 4 = 0`,
		f.tenantID, commandBoardPlanFixtureAnimals)

	// Real statistics, or every plan below is a guess about an empty table. This mirrors the
	// mandatory post-seed ANALYZE contract in AGENTS.md.
	execProjectionSQL(t, ctx, pool, "analyze plan fixture",
		`ANALYZE obligation_instances, vaccination_completions, goats, locations, protocol_rules,
		   protocol_rule_dimensions, goat_shed_partitions, shed_partitions, obligation_batches,
		   goat_identifiers`)
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
				label:                      "commandBoardShedDoseSQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            obligations * 2,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"goat_shed_partitions"},
			},
			// The shed x dose grid is a tenant-wide fold, so obligation_instances is legitimately
			// read once. Added because review found this statement had NEITHER a plan gate nor a
			// working latency gate, despite this change giving it its own endpoint -- exactly the
			// "escaped the guardrails" shape that caused the original incident.
			sql:  commandBoardShedDoseSQL,
			args: []any{f.tenantID, f.asOf, nil, nil},
		},
		{
			limits: commandBoardPlanLimits{
				label:                      "commandBoardKPISQL",
				maxScanRows:                obligations * 2,
				maxPreLimitRows:            obligations * 2,
				maxNodeTotalRows:           summaryNodeCeiling,
				maxRowsRemovedByJoinFilter: summaryNodeCeiling,
				hotTables:                  []string{"goat_shed_partitions"},
			},
			// Every tile on the board comes from this one statement, so it is the single most
			// load-bearing summary read on the endpoint.
			sql:  commandBoardKPISQL,
			args: []any{f.tenantID, f.asOf, nil, nil},
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
			args: []any{f.tenantID, nil, domain.CommandBoardDriveOptionsPageSize + 1, nil, nil, nil, nil, nil, nil, nil},
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
		//
		// Rows the scan PROPAGATES, with the rows it discarded reported alongside for diagnosis.
		//
		// Gating on rows-scanned instead was tried and is wrong AT THIS FIXTURE SIZE: 1,600 rows is
		// below the point where Postgres will choose an index over a sequential scan, so every
		// statement -- correct ones included -- reads the whole table and a scanned-rows ceiling
		// fails everything. What this fixture CAN prove is that the scope actually reduces the row
		// set the statement carries forward, which is exactly the difference between a cell in the
		// WHERE clause and a cell applied in Go afterwards. The limitation is recorded in
		// docs/runbooks/vaccination-command-board-latency.md rather than papered over: this gate
		// asserts work is proportional to the answer, NOT that a particular index was chosen.
		propagated := n.ActualRows * loops
		if hot[n.RelationName] && propagated > limits.maxScanRows {
			out = append(out, fmt.Sprintf("%s on %s carried %.0f rows forward (%.0f discarded at the scan, x%.0f loops; ceiling %.0f) - the scope is being applied after the scan, not in it",
				n.NodeType, n.RelationName, propagated, n.RowsRemovedByFilter, loops, limits.maxScanRows))
		}

		// (2) rows x loops at one node: the nested-loop-inner-scan detector, which neither a single
		// scan's row count nor a blocking-node ceiling can see.
		//
		// Applied to HOT TABLES and to relation-less nodes (joins, aggregates, CTE scans) only. A
		// nested loop whose inner side is a small DIMENSION table is the planner doing the right
		// thing: locations holds a few hundred rows here and stays small at a million animals, so
		// re-scanning it 50 times costs nothing and flagging it fails correct statements. The rule
		// is about tenant-scale rows, and hotTables is where this file says which those are.
		relational := n.RelationName != ""
		if (!relational || hot[n.RelationName]) && totalRows > limits.maxNodeTotalRows {
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
		// CTE Scan ONLY, not Materialize. A Materialize node is the ordinary buffer Postgres puts on
		// a nested loop's inner side and it is re-scanned by design -- flagging it made the gate
		// fail on correct statements at fixture scale, which is how a guard gets switched off. The
		// pathology is re-walking a MATERIALISED CTE, whose cost is unrelated to the rows it
		// returns; an abusive Materialize is caught by the rows x loops ceiling instead.
		if strings.Contains(n.NodeType, "CTE Scan") && loops > 1 {
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

// commandBoardPreFixShedVaccineAnimalSQL is the shed-vaccine drilldown as it stood before this
// change, kept so the gate can be shown to reject its shape too.
//
// The defect is what is ABSENT: there is no cell predicate. It selected every behind animal in the
// tenant, ordered them by due date, took the first 500, and let Go bucket them into cells. On the
// live tenant that sorted an estimated 57,176 rows and spent 2.4s returning ZERO. It was also wrong
// as a drilldown independently of speed: a global 500-row cap starves whichever cells sort late, so
// a red cell could show an empty drawer while its animals sat under another shed's rows.
const commandBoardPreFixShedVaccineAnimalSQL = `
WITH comp AS (
  SELECT obligation_id,
         bool_or(status = 'accepted') AS has_accepted,
         bool_or(status = 'recorded' AND verified_at IS NULL) AS has_recorded_unverified
  FROM vaccination_completions
  WHERE tenant_id = $1::uuid
  GROUP BY obligation_id
)
SELECT DISTINCT ON (oi.scope_id, d.vaccine_code, g.goat_id)
  oi.scope_id::text AS shed_id,
  d.vaccine_code,
  g.goat_id::text,
  g.display_id,
  oi.status,
  oi.due_at
FROM obligation_instances oi
JOIN protocol_rule_dimensions d ON d.rule_id = oi.rule_id AND d.tenant_id = oi.tenant_id
JOIN goats g ON g.goat_id = oi.target_id AND g.tenant_id = oi.tenant_id
LEFT JOIN comp ON comp.obligation_id = oi.obligation_id
WHERE oi.tenant_id = $1::uuid
  AND oi.scope_type = 'shed'
  AND g.lifecycle_status IN ('alive', 'sick', 'under_treatment', 'quarantine', 'icu')
  AND g.merged_into_goat_id IS NULL
  AND d.vaccine_code <> ''
  AND NOT COALESCE(comp.has_accepted, false)
  AND (
    COALESCE(comp.has_recorded_unverified, false)
    OR oi.status = 'missed'
    OR (oi.status IN ('scheduled','due','in_progress','deferred')
        AND (oi.due_at AT TIME ZONE 'Asia/Kolkata')::date < ($2::timestamptz AT TIME ZONE 'Asia/Kolkata')::date)
  )
ORDER BY oi.scope_id, d.vaccine_code, g.goat_id, oi.due_at ASC NULLS LAST
LIMIT $3
`

// TestCommandBoardPlanGuardRejectsThePreFixShapes is the guard's own mutation test.
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
	// THE SPECIFIC DETECTOR, not merely "something fired".
	//
	// len(violations) > 0 would let this test go green for the wrong reason: with the decorating
	// tables now seeded, detector (1) plausibly fires on this statement too, so a future relaxation
	// of detector (4) -- the only one that can see a re-scanned CTE -- would leave the guard's own
	// proof passing while the guard had stopped catching the actual staging 500.
	//
	// Detector (4) is the one that matters here and the reason is measured, not assumed: the LATERAL
	// re-walks the materialised scoped CTE once per residual animal, but its inner LIMIT 1 means the
	// node reports about one row per loop, so every ROW-COUNT ceiling in this file passes it.
	if !commandBoardHasViolation(violations, "CTE Scan", "executed") {
		t.Fatalf("the plan guard did not flag the pre-fix closed-without-dose statement's re-scanned CTE, which is the "+
			"shape that produced the staging 500. Row-count ceilings cannot see it (the LATERAL's inner LIMIT 1 keeps "+
			"per-loop rows tiny), so detector (4) is the only thing standing between us and that incident.\n"+
			"violations=%v\nfixture: %d animals, %.0f obligations; execTime=%.1fms",
			violations, commandBoardPlanFixtureAnimals, obligations, res.ExecutionTime)
	}
	t.Logf("guard rejected the pre-fix closed-without-dose shape with %d violation(s):\n  %s",
		len(violations), strings.Join(violations, "\n  "))
}

// TestCommandBoardPlanGuardRejectsTheTenantWideDrilldown is the second mutation case: a drilldown
// with NO cell predicate.
//
// It is a separate test from the one above because it must be rejected by a DIFFERENT detector.
// The closed-without-dose defect is a re-scanned CTE; this one is an honest single scan that simply
// reads the whole tenant to produce a capped list, and only the drilldown-scoped scan ceiling can
// see it. Proving one shape and assuming the other would leave the gate half-blind.
func TestCommandBoardPlanGuardRejectsTheTenantWideDrilldown(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	f := seedCommandBoardPlanFixture(t, ctx, pool)
	obligations := float64(commandBoardPlanFixtureAnimals * 4)

	// The SAME ceilings the real shed-vaccine drilldown is held to. That is the point: the fixed
	// statement passes them and the pre-fix one must not.
	limits := commandBoardPlanLimits{
		label:                      "commandBoardPreFixShedVaccineAnimalSQL",
		maxScanRows:                obligations / 4,
		maxPreLimitRows:            obligations / 4,
		maxNodeTotalRows:           obligations / 2,
		maxRowsRemovedByJoinFilter: obligations,
		hotTables:                  []string{"obligation_instances"},
	}
	res := explainAnalyzeJSON(t, ctx, pool, commandBoardPreFixShedVaccineAnimalSQL, f.tenantID, f.asOf, 500)

	violations := commandBoardPlanViolations(limits, res)
	if !commandBoardHasViolation(violations, "obligation_instances", "ceiling") {
		t.Fatalf("the plan guard PASSED a drilldown with no cell predicate -- the shape that sorted an estimated "+
			"57,176 rows to return 500 and took 2.4s to return zero on the live tenant.\nviolations=%v\n"+
			"fixture: %d animals, %.0f obligations; execTime=%.1fms",
			violations, commandBoardPlanFixtureAnimals, obligations, res.ExecutionTime)
	}
	t.Logf("guard rejected the tenant-wide drilldown shape with %d violation(s):\n  %s",
		len(violations), strings.Join(violations, "\n  "))
}

// commandBoardHasViolation reports whether any violation names all of the given fragments. It
// matches on the SHAPE being reported rather than on exact wording, so failure messages stay
// editable while the assertion stays about the right detector.
func commandBoardHasViolation(violations []string, fragments ...string) bool {
	for _, v := range violations {
		all := true
		for _, fragment := range fragments {
			if !strings.Contains(v, fragment) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
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

	// Every fourth animal holds only 'canceled' obligations with no completion: not verified, not
	// awaiting, not overdue, not scheduled and not missed. That is the residual bucket, and it being
	// a PROPER SUBSET of the herd is what makes this a test of the predicate rather than a check
	// that everything equals everything.
	if board.KPIs.ClosedWithoutDose != commandBoardPlanFixtureResidual {
		t.Fatalf("tile closedWithoutDose = %d, want %d; the fixture puts every fourth animal in the residual bucket",
			board.KPIs.ClosedWithoutDose, commandBoardPlanFixtureResidual)
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

// TestCohortExceptionTileAndDrawerRangeOverTheSameAnimals extends the predicate-drift gate to the
// SECOND tile/drawer pair on this board.
//
// The closed-without-dose pair was covered; the cohort-exception pair was not, and that gap is
// exactly how a divergence survived a full review round. The count and the drawer had drifted at
// two different grains in turn -- first dose SEQUENCE, then dose CODE -- because several dose codes
// collapse onto one displayed vaccine label (et_tt_kid_4w and et_tt_kid_7w are both "ET+TT"), so a
// per-code count double-counted an animal the drawer lists once.
//
// The fixture is built to make that specific divergence visible: one animal is an exception under
// TWO dose codes of the SAME displayed label. A per-code count reports 2; the correct answer, and
// what the drawer pages, is 1.
func TestCohortExceptionTileAndDrawerRangeOverTheSameAnimals(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	tenantID := "00000000-0000-4000-8000-0000000000e1"
	parkID := uuidFromSuffix("01", "xd")
	shedID := uuidFromSuffix("02", "xd")
	asOf := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

	protocolID := uuidFromSuffix("06", "xdp")
	protocolVersionID := uuidFromSuffix("06", "xdv")
	execProjectionSQL(t, ctx, pool, "tenant",
		`INSERT INTO tenants (tenant_id, name, status) VALUES ($1, 'Test Org', 'active')
		 ON CONFLICT (tenant_id) DO NOTHING`, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol definition",
		`INSERT INTO protocol_definitions (protocol_id, tenant_id, code, name, category, status)
		 VALUES ($1, $2, 'vaccination_xd', 'Vaccination XD', 'vaccination', 'active')`, protocolID, tenantID)
	execProjectionSQL(t, ctx, pool, "protocol version",
		`INSERT INTO protocol_versions (protocol_version_id, tenant_id, protocol_id, scope_type, version, status, effective_from, rule_dsl)
		 VALUES ($1, $2, $3, 'tenant', 1, 'draft', '2026-01-01', '{}')`, protocolVersionID, tenantID, protocolID)
	seedCommandBoardPark(t, ctx, pool, tenantID, parkID, shedID, "Drift")

	// TWO kid dose codes that render as the SAME label, plus the later dose whose acceptance makes
	// both of them exceptions.
	rules := map[string]struct {
		id       string
		sequence int
	}{
		"et_tt_kid_4w": {uuidFromSuffix("07", "xd4"), 1},
		"et_tt_kid_7w": {uuidFromSuffix("07", "xd7"), 2},
		// The later accepted dose must be in the SAME COURSE FAMILY. Family strips the position
		// suffix, so et_tt_kid_4w and et_tt_kid_7w are both "et_tt_kid" -- but et_tt_REVAC strips to
		// "et_tt", a different family, and would make neither earlier dose an exception.
		"et_tt_kid_12w": {uuidFromSuffix("07", "xdr"), 3},
	}
	for code, rule := range rules {
		execProjectionSQL(t, ctx, pool, "rule "+code,
			`INSERT INTO protocol_rules (rule_id, tenant_id, protocol_version_id, dose_code, sequence, trigger_type)
			 VALUES ($1, $2, $3, $4, $5, 'birth_age')`,
			rule.id, tenantID, protocolVersionID, code, rule.sequence)
	}

	// Published only AFTER the rules are written: published config is immutable, so seeding rules
	// against an already-published version is rejected by the schema.
	execProjectionSQL(t, ctx, pool, "publish protocol version",
		`UPDATE protocol_versions SET status = 'published', published_at = now() WHERE protocol_version_id = $1`,
		protocolVersionID)

	goatID := uuidFromSuffix("03", "xda")
	seedBareGoat(t, ctx, pool, tenantID, shedID, goatID, uuidFromSuffix("0a", "xda"))
	// Both kid doses exist and neither is accepted.
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, rules["et_tt_kid_4w"].id, shedID, goatID,
		uuidFromSuffix("08", "xd4"), "missed", asOf.Add(-30*24*time.Hour), "xd-4w")
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, rules["et_tt_kid_7w"].id, shedID, goatID,
		uuidFromSuffix("08", "xd7"), "missed", asOf.Add(-20*24*time.Hour), "xd-7w")
	// The LATER dose of the same course IS accepted, which is what makes both earlier doses
	// exceptions.
	revacObligation := uuidFromSuffix("08", "xdr")
	seedObligation(t, ctx, pool, tenantID, protocolVersionID, rules["et_tt_kid_12w"].id, shedID, goatID,
		revacObligation, "completed", asOf.Add(-10*24*time.Hour), "xd-revac")
	execProjectionSQL(t, ctx, pool, "accepted revac completion",
		`INSERT INTO vaccination_completions (completion_id, tenant_id, obligation_id, goat_id, status,
		   adverse_reaction, cold_chain_verified, administered_at, idempotency_key)
		 VALUES ($1, $2, $3, $4, 'accepted', false, true, $5::timestamptz, 'xd-completion')`,
		uuidFromSuffix("09", "xdc"), tenantID, revacObligation, goatID, asOf.Add(-10*24*time.Hour))

	execProjectionSQL(t, ctx, pool, "analyze drift fixture",
		`ANALYZE obligation_instances, vaccination_completions, goats, locations, protocol_rules`)

	repo := NewRepository(pool, 30*time.Second)
	matrix, err := repo.CommandBoardCohortMatrix(ctx, domain.CommandBoardDrilldownQuery{TenantID: tenantID, AsOf: asOf})
	if err != nil {
		t.Fatalf("CommandBoardCohortMatrix() error = %v", err)
	}

	var cell *domain.CommandBoardCohortCell
	for i := range matrix.Cells {
		if matrix.Cells[i].MissingPriorDoseCount > 0 {
			cell = &matrix.Cells[i]
			break
		}
	}
	if cell == nil {
		t.Fatalf("no cohort cell reports an exception; the fixture cannot test the drift it was built for: %+v", matrix.Cells)
	}
	if len(cell.DoseCodes) < 2 {
		t.Fatalf("cell %q folds %d dose codes, want at least 2 — the divergence only appears where codes collapse onto one label",
			cell.VaccineLabel, len(cell.DoseCodes))
	}

	page, err := repo.CommandBoardCohortExceptions(ctx, domain.CommandBoardCohortCellQuery{
		CommandBoardDrilldownQuery: domain.CommandBoardDrilldownQuery{TenantID: tenantID, AsOf: asOf, Limit: 200},
		CohortParkID:               cell.Cohort.ParkID,
		ManagementStage:            cell.Cohort.ManagementStage,
		Sex:                        cell.Cohort.Sex,
		DoseCodes:                  cell.DoseCodes,
	})
	if err != nil {
		t.Fatalf("CommandBoardCohortExceptions() error = %v", err)
	}

	if cell.MissingPriorDoseCount != len(page.Animals) {
		t.Fatalf("cell %q says %d exceptions but the drawer lists %d animals; the tile counts per dose code "+
			"while the drawer counts animals, so a cell folding two codes double-counts an animal the drawer names once",
			cell.VaccineLabel, cell.MissingPriorDoseCount, len(page.Animals))
	}
	if cell.MissingPriorDoseCount != 1 {
		t.Fatalf("cell %q says %d exceptions, want 1: ONE animal is an exception, under two dose codes that render as one label",
			cell.VaccineLabel, cell.MissingPriorDoseCount)
	}
}
