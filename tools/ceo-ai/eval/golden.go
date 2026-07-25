package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// validTiers is the closed set of read-path tiers the routing hierarchy uses.
var validTiers = map[string]bool{"cube": true, "api": true, "toolbox": true, "sql": true}

// idPattern keeps ids stable and file-name-safe so the report and any future
// feedback join stay deterministic.
var idPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const (
	oracleScalar = "scalar_int"
	oracleRows   = "rows"
	// minQuestions / minClasses are the floor the discovery-judge gap set
	// requires (>=40 questions across every GenAI class + adversarial refusals).
	minQuestions = 40
	minClasses   = 20
)

// loadGolden reads every *.json file in dir (each a JSON array of questions),
// returns them sorted by id for deterministic ordering, and validates the set.
func loadGolden(dir string) ([]GoldenQuestion, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read golden dir %s: %w", dir, err)
	}
	var qs []GoldenQuestion
	files := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		files++
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var batch []GoldenQuestion
		if err := json.Unmarshal(raw, &batch); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		qs = append(qs, batch...)
	}
	if files == 0 {
		return nil, fmt.Errorf("no golden *.json files found in %s", dir)
	}
	sort.Slice(qs, func(i, j int) bool { return qs[i].ID < qs[j].ID })
	if err := validateGolden(qs); err != nil {
		return nil, err
	}
	return qs, nil
}

// validateGolden fails closed on any malformed question so a typo can never
// silently weaken the gate. This is what the always-on CI self-test asserts.
func validateGolden(qs []GoldenQuestion) error {
	if len(qs) == 0 {
		return fmt.Errorf("golden set is empty")
	}
	seen := map[string]bool{}
	classes := map[string]bool{}
	var errs []string
	for _, q := range qs {
		where := q.ID
		if where == "" {
			where = "(missing id)"
		}
		if q.ID == "" || !idPattern.MatchString(q.ID) {
			errs = append(errs, fmt.Sprintf("%s: id must be kebab-case [a-z0-9-]", where))
		}
		if seen[q.ID] {
			errs = append(errs, fmt.Sprintf("%s: duplicate id", where))
		}
		seen[q.ID] = true
		if strings.TrimSpace(q.Question) == "" {
			errs = append(errs, fmt.Sprintf("%s: empty question", where))
		}
		if strings.TrimSpace(q.Class) == "" {
			errs = append(errs, fmt.Sprintf("%s: empty class", where))
		}
		classes[q.Class] = true
		for _, t := range q.Expect.TiersAnyOf {
			if !validTiers[t] {
				errs = append(errs, fmt.Sprintf("%s: unknown tier %q (want cube|api|toolbox|sql)", where, t))
			}
		}
		errs = append(errs, validateQuestionCoherence(q)...)
	}
	if len(classes) < minClasses {
		errs = append(errs, fmt.Sprintf("golden set covers %d classes, want >= %d", len(classes), minClasses))
	}
	if len(qs) < minQuestions {
		errs = append(errs, fmt.Sprintf("golden set has %d questions, want >= %d", len(qs), minQuestions))
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("golden set invalid:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return nil
}

// validateQuestionCoherence enforces that the declared expectations and the
// oracle are internally consistent, so scoring can never be a no-op.
func validateQuestionCoherence(q GoldenQuestion) []string {
	var errs []string
	needsOracle := q.Expect.Grounded || q.Expect.SpeciesSplit
	if needsOracle && q.Oracle == nil {
		errs = append(errs, fmt.Sprintf("%s: grounded/species_split requires an oracle", q.ID))
	}
	if q.Expect.InjectionForbidLeak && q.Oracle == nil {
		errs = append(errs, fmt.Sprintf("%s: injection_forbid_leak requires an oracle (the forbidden leak value)", q.ID))
	}
	// Validate any oracle that is present, whatever it is used for.
	if q.Oracle != nil {
		errs = append(errs, validateOracle(q)...)
	}
	if q.Expect.SpeciesSplit && q.Oracle != nil && q.Oracle.Kind != oracleRows {
		errs = append(errs, fmt.Sprintf("%s: species_split needs oracle.kind=rows", q.ID))
	}
	if q.Expect.Refusal && needsOracle {
		errs = append(errs, fmt.Sprintf("%s: a refusal question must not also require a grounded number", q.ID))
	}
	// Every question must assert at least one scored property, otherwise it is
	// dead weight that inflates the count without testing anything.
	if !q.Expect.Refusal && !q.Expect.Grounded && !q.Expect.SpeciesSplit &&
		!q.Expect.AggregateFirst && !q.Expect.InjectionSafe && len(q.Expect.TiersAnyOf) == 0 {
		errs = append(errs, fmt.Sprintf("%s: asserts no scored property", q.ID))
	}
	return errs
}

// forbiddenSQL are tokens that must never appear in an oracle statement. The
// oracle is read-only ground truth; it is not a place to smuggle mutations.
var forbiddenSQL = []string{
	";", "--", "/*", "*/",
	" insert ", " update ", " delete ", "truncate", " drop ", " alter ", " create ",
	" grant ", " revoke ", " merge ", " copy ", " vacuum ", " into ",
	" begin ", " commit ", " rollback ", "pg_sleep",
}

func validateOracle(q GoldenQuestion) []string {
	var errs []string
	o := q.Oracle
	if o.Kind != oracleScalar && o.Kind != oracleRows {
		errs = append(errs, fmt.Sprintf("%s: oracle.kind must be scalar_int|rows", q.ID))
	}
	sql := strings.TrimSpace(o.SQL)
	// Pad so leading/trailing keyword tokens are caught by the space-wrapped set.
	low := " " + strings.ToLower(sql) + " "
	if !strings.HasPrefix(strings.ToLower(sql), "select") && !strings.HasPrefix(strings.ToLower(sql), "with") {
		errs = append(errs, fmt.Sprintf("%s: oracle SQL must be a single SELECT/WITH", q.ID))
	}
	for _, bad := range forbiddenSQL {
		if strings.Contains(low, bad) {
			errs = append(errs, fmt.Sprintf("%s: oracle SQL contains forbidden token %q", q.ID, strings.TrimSpace(bad)))
		}
	}
	if !strings.Contains(sql, ":'tenant_id'") {
		errs = append(errs, fmt.Sprintf("%s: oracle SQL must bind the tenant via :'tenant_id'", q.ID))
	}
	return errs
}
