package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// commandBoardPlanCoverageExempt lists command-board SQL constants that are deliberately NOT in the
// plan table, each with the reason it cannot regress the way the incident did.
//
// A skip list is the part of a coverage guard most likely to rot into a hiding place, so every
// entry states a MEASURED fact rather than an opinion, and adding one is a visible diff.
var commandBoardPlanCoverageExempt = map[string]string{
	"commandBoardCohortExceptionCTE": "not an executable statement -- it is the shared CTE PREFIX that " +
		"commandBoardCohortExceptionCountSQL and commandBoardCohortExceptionListSQL are each built from " +
		"by concatenation, and BOTH of those are in the plan table, so every plan this fragment can " +
		"produce is already EXPLAINed. It cannot be EXPLAINed alone: it has no final SELECT.",
	"commandBoardVaccineCodeSQL": "a bare DISTINCT over protocol_rules (204 rows tenant-wide); measured 0.14ms on the " +
		"staging-scale clone. It touches no hot table and has no join to fan out.",
	"commandBoardVerifyQueueSQL": "reads the verification queue only; measured 0.25ms on the staging-scale clone. Its " +
		"row count is bounded by outstanding verifications, not by obligation_instances.",
	"commandBoardCohortHeadSQL": "a head count over goats (1.6k rows tenant-wide); measured 1.6ms. It does not touch " +
		"obligation_instances at all.",
	"commandBoardWeeklySQL": "aggregates vaccination_completions (5.8k rows), not obligation_instances; measured 39ms. " +
		"Its grain is ISO week x dose x status, which cannot fan out per animal.",
	"commandBoardShedVideoSQL": "cell-scoped proof-video lookup, bounded by the shed ids and days handed to it by a " +
		"page that is itself already gated.",
}

// A const is in scope if its NAME ends in SQL or CTE, or if its VALUE looks like SQL. Matching the
// name alone left a hole review found: commandBoardCohortExceptionCTE is a real statement fragment
// in these files and was invisible, and a future commandBoardFooStmt or ...Query would be too.
var commandBoardSQLConstRe = regexp.MustCompile(`(?:SQL|CTE)$`)

// commandBoardSQLValueRe matches a string that is a SQL statement rather than a fragment.
var commandBoardSQLValueRe = regexp.MustCompile(`(?is)\b(SELECT|WITH)\b.*\bFROM\b`)

// TestCommandBoardPlanGateCoversEverySQLConst fails when a command-board statement exists with no
// plan-table entry and no explicit exemption.
//
// The REQUIRED_TESTS array in tools/dev/commandboard-query-plan-guard.sh protects test NAMES, not
// coverage: deleting a row from the plan table leaves every named test passing and the guard
// satisfied. The rule that would actually have prevented the original incident is "every statement
// on this endpoint is plan-proven", and nothing enforced it until this test. Found by review, which
// noted the shed x dose statement had gained its own endpoint while having neither a plan gate nor
// a reachable latency gate.
//
// Deliberately DB-free: it parses source, so it runs in plain `go test` on a machine with no
// Postgres, and cannot be skipped into vacuous green.
func TestCommandBoardPlanGateCoversEverySQLConst(t *testing.T) {
	declared := map[string]struct{}{}
	for _, file := range []string{"commandboard_sql.go", "commandboard_drilldown_sql.go"} {
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, decl := range parsed.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range value.Names {
					sqlShaped := false
					if i < len(value.Values) {
						if lit, ok := value.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							if text, err := strconv.Unquote(lit.Value); err == nil {
								sqlShaped = strings.Count(text, "\n") >= 3 && commandBoardSQLValueRe.MatchString(text)
							}
						}
					}
					if commandBoardSQLConstRe.MatchString(name.Name) || sqlShaped {
						declared[name.Name] = struct{}{}
					}
				}
			}
		}
	}
	if len(declared) == 0 {
		t.Fatal("found no *SQL constants; this test's parser has stopped seeing the SQL files, which " +
			"would make it pass vacuously forever")
	}

	gated, err := os.ReadFile("commandboard_query_plan_test.go")
	if err != nil {
		t.Fatalf("read plan test: %v", err)
	}
	table := string(gated)

	var missing []string
	for name := range declared {
		if _, exempt := commandBoardPlanCoverageExempt[name]; exempt {
			continue
		}
		// The label is what the plan gate reports on failure, so requiring the label string keeps
		// the failure message and the coverage claim in sync.
		if !regexp.MustCompile(`label:\s*"` + regexp.QuoteMeta(name) + `"`).MatchString(table) {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("command-board SQL statements with no plan-table entry and no exemption: %v\n"+
			"Add a commandBoardPlanLimits row for each, or add it to commandBoardPlanCoverageExempt "+
			"with a MEASURED reason it cannot regress. A statement nobody EXPLAINs is how "+
			"/vaccination/command reached a 15s timeout in production.", missing)
	}

	// The exemption list must not outlive the statements it excuses.
	for name := range commandBoardPlanCoverageExempt {
		if _, ok := declared[name]; !ok {
			t.Fatalf("commandBoardPlanCoverageExempt names %q, which no longer exists; a stale "+
				"exemption silently excuses whatever is added under that name next", name)
		}
	}
}
