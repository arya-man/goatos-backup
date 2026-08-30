package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeGo writes src to a temp file and returns (repoRoot, absPath).
func writeGo(t *testing.T, src string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, p
}

func rules(fs []finding) map[string]int {
	m := map[string]int{}
	for _, f := range fs {
		m[f.rule]++
	}
	return m
}

func TestDetectsAntiPatterns(t *testing.T) {
	src := `package p

import "context"

type T struct{ pool interface{ Query(context.Context, string, ...any) (any, error); Exec(context.Context, string, ...any) (any, error) } }

func (r *T) nplus(ctx context.Context, ids []string) {
	for _, id := range ids {
		_, _ = r.pool.Exec(ctx, "UPDATE t SET x=1 WHERE id=$1", id)
	}
}

const offsetSQL = "SELECT id FROM t WHERE tenant_id=$1 ORDER BY id LIMIT $2 OFFSET $3"

const delProj = "DELETE FROM t_projection_rows WHERE tenant_id = $1"

const likeSQL = "SELECT id FROM t WHERE lower(name) LIKE '%' || $1 || '%'"

const castSQL = "UPDATE outbox_messages SET status='pending' WHERE outbox_id::text = ANY($1::text[])"

const godCTE = "WITH a AS (SELECT 1), b AS (SELECT 1), c AS (SELECT 1), d AS (SELECT 1), e AS (SELECT 1), f AS (SELECT 1), g AS (SELECT 1), h AS (SELECT 1), i AS (SELECT 1), j AS (SELECT 1) SELECT * FROM a"

const aggregateRawFetchLimit = 5000

func aggregateCalendarList() {}

func (r *T) badRollup() {
	q := struct{ Limit int }{}
	resp := struct{ NextCursor *string }{}
	q.Limit = aggregateRawFetchLimit
	aggregateCalendarList()
	resp.NextCursor = nil
}
`
	repo, path := writeGo(t, src)
	got := rules(scanFile(repo, path))
	for _, want := range []string{"n-plus-one", "offset-pagination", "full-mv-refresh", "non-sargable-like", "non-sargable-cast", "god-cte", "read-rollup-truth"} {
		if got[want] == 0 {
			t.Errorf("expected rule %q to fire, got %+v", want, got)
		}
	}
}

func TestColumnCastPredicateGuardRequiresTypedBindArray(t *testing.T) {
	clean := `package p

const cleanSQL = "UPDATE outbox_messages SET status='pending' WHERE outbox_id = ANY($1::uuid[])"
`
	repo, path := writeGo(t, clean)
	if got := rules(scanFile(repo, path)); got["non-sargable-cast"] != 0 {
		t.Fatalf("typed UUID bind array must stay clean, got %+v", got)
	}

	bad := `package p

const badSQL = "UPDATE outbox_messages SET status='pending' WHERE outbox_id::text = ANY($1::text[])"
`
	repo, path = writeGo(t, bad)
	if got := rules(scanFile(repo, path)); got["non-sargable-cast"] != 1 {
		t.Fatalf("column-side text cast must be blocked, got %+v", got)
	}
}

func TestFanoutN1DetectionAndPrecision(t *testing.T) {
	// The ShedSummary class: a ctx-taking call to an injected dependency inside a
	// range loop. The driver .Query is an adapter layer down, so n-plus-one can't
	// see it; n-plus-one-fanout must.
	bad := `package p

import (
	"context"
	"time"
)

type owner struct{}
type dep interface {
	ShedOwnership(ctx context.Context, tenantID, shedID, parkID string, at time.Time) (*owner, *owner, error)
}
type S struct {
	ownership dep
	log       interface{ InfoContext(context.Context, string) }
}

func (s *S) Summary(ctx context.Context, sheds []string) {
	for _, id := range sheds {
		s.log.InfoContext(ctx, "enriching")            // noise receiver: must NOT flag
		_, _, _ = s.ownership.ShedOwnership(ctx, "t", id, "p", time.Now())
	}
}
`
	repo, path := writeGo(t, bad)
	got := rules(scanFile(repo, path))
	if got["n-plus-one-fanout"] != 1 {
		t.Errorf("expected exactly 1 n-plus-one-fanout (the ownership call, not the logger), got %+v", got)
	}
	if got["n-plus-one"] != 0 {
		t.Errorf("cross-boundary call is not a raw driver call; n-plus-one must stay 0, got %+v", got)
	}

	// Precision: batched-outside-loop + pure work must stay clean.
	clean := `package p

import "context"

type S struct{ repo interface{ GetByIDs(context.Context, []string) error } }

func (s *S) f(ctx context.Context, ids []string) {
	_ = s.repo.GetByIDs(ctx, ids)
	total := 0
	for range ids {
		total++
	}
	_ = total
}
`
	repo, path = writeGo(t, clean)
	if got := rules(scanFile(repo, path)); got["n-plus-one-fanout"] != 0 {
		t.Errorf("batched-outside-loop + pure work must be clean, got %+v", got)
	}
}

func TestFanoutInlineIgnoreSuppresses(t *testing.T) {
	src := `package p

import "context"

type S struct{ roster interface{ Manager(context.Context, string) error } }

func (s *S) f(ctx context.Context, ids []string) {
	for _, id := range ids {
		_ = s.roster.Manager(ctx, id) // scale-guard:ignore: bounded to <=4 parks
	}
}
`
	repo, path := writeGo(t, src)
	if got := rules(scanFile(repo, path)); got["n-plus-one-fanout"] != 0 {
		t.Errorf("inline ignore must suppress n-plus-one-fanout, got %+v", got)
	}
}

func TestVaccinationOperatorAvailabilityFanoutIsBlocked(t *testing.T) {
	src := `package p

import (
	"context"
	"time"
)

type repo interface {
	AvailableVaccinationOperatorsForDrive(context.Context, string, string, time.Time, int32) ([]string, error)
}

type S struct{ repo repo }

func (s *S) scoreDates(ctx context.Context, tenantID, parkID string, dates []time.Time) error {
	for _, day := range dates {
		if _, err := s.repo.AvailableVaccinationOperatorsForDrive(ctx, tenantID, parkID, day, 200); err != nil {
			return err
		}
	}
	return nil
}
`
	repo, path := writeGo(t, src)
	got := rules(scanFile(repo, path))
	if got["n-plus-one-fanout"] != 1 {
		t.Fatalf("vaccination operator availability in a date loop must be blocked, got %+v", got)
	}
}

func TestNoFalsePositiveOnOffsetErrorString(t *testing.T) {
	// An "offset must be..." validation message is not a SQL OFFSET clause.
	src := `package p

const msg = "offset must be a non-negative integer for this list endpoint"

func f() string { return msg }
`
	repo, path := writeGo(t, src)
	if got := rules(scanFile(repo, path)); got["offset-pagination"] != 0 {
		t.Errorf("offset error string must not trip offset-pagination, got %+v", got)
	}
}

func TestInlineIgnoreSuppresses(t *testing.T) {
	src := `package p

import "context"

type T struct{ pool interface{ Exec(context.Context, string, ...any) (any, error) } }

func (r *T) f(ctx context.Context, ids []string) {
	for _, id := range ids {
		_, _ = r.pool.Exec(ctx, "UPDATE t SET x=1 WHERE id=$1", id) // scale-guard:ignore: bounded chunk
	}
}
`
	repo, path := writeGo(t, src)
	if got := rules(scanFile(repo, path)); got["n-plus-one"] != 0 {
		t.Errorf("inline ignore must suppress n-plus-one, got %+v", got)
	}
}

func TestBoundedQueryIsClean(t *testing.T) {
	// Keyset pagination + single set-based statement: no findings.
	src := `package p

import "context"

type T struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }

const keyset = "SELECT id FROM t WHERE tenant_id=$1 AND (sort_key,id) > ($2,$3) ORDER BY sort_key,id LIMIT $4"

func (r *T) f(ctx context.Context) { _, _ = r.pool.Query(ctx, keyset, nil) }
`
	repo, path := writeGo(t, src)
	if got := scanFile(repo, path); len(got) != 0 {
		t.Errorf("clean keyset query should have no findings, got %+v", got)
	}
}

func TestVersionScopedPruneNotFlagged(t *testing.T) {
	// Whole-tenant and range wipes ARE full-mv-refresh; a version-scoped prune is NOT.
	src := "package p\n" +
		"const wipe = `DELETE FROM t_projection_rows WHERE tenant_id = $1`\n" +
		"const rangeWipe = `DELETE FROM t_projection_rows WHERE tenant_id = $1 AND projection_version > 0`\n" +
		"const prune = `DELETE FROM t_projection_rows WHERE tenant_id = $1 AND projection_version <> $2`\n"
	repo, path := writeGo(t, src)
	got := scanFile(repo, path)
	n := rules(got)["full-mv-refresh"]
	if n != 2 {
		t.Fatalf("expected exactly 2 full-mv-refresh findings (wipe/range wipe, not prune), got %d: %+v", n, got)
	}
	for _, f := range got {
		if f.rule == "full-mv-refresh" && f.line == 4 {
			t.Errorf("full-mv-refresh must not flag the version-scoped prune, got line %d", f.line)
		}
	}
}

func TestLoopNoCursorDetectionAndGuard(t *testing.T) {
	// Infinite for{} paging a List* method with no cursor guard -> flagged.
	bad := `package p

import "context"

type R struct{}
func (R) ListThings(ctx context.Context, page int) ([]int, error) { return nil, nil }

func run(ctx context.Context, r R, page int) {
	for {
		rows, _ := r.ListThings(ctx, page)
		if len(rows) == 0 { break }
	}
}
`
	repo, path := writeGo(t, bad)
	if got := rules(scanFile(repo, path)); got["loop-no-cursor"] == 0 {
		t.Errorf("uncursored paging loop must trip loop-no-cursor, got %+v", got)
	}

	// Same loop but with a cursor/progress guard identifier -> clean.
	good := `package p

import "context"

type R struct{}
func (R) ListThings(ctx context.Context, after int) ([]int, error) { return nil, nil }

func run(ctx context.Context, r R) {
	after := 0
	for {
		rows, _ := r.ListThings(ctx, after)
		if len(rows) == 0 { break }
		after = rows[len(rows)-1]
	}
}
`
	repo, path = writeGo(t, good)
	if got := rules(scanFile(repo, path)); got["loop-no-cursor"] != 0 {
		t.Errorf("cursor-guarded loop must not trip loop-no-cursor, got %+v", got)
	}
}

func TestLoopProgressBreakGuardRecognized(t *testing.T) {
	// A zero-progress break (`if progressed == 0 { break }`) is a valid guard;
	// the `len(rows) == 0` empty-page exit alone is NOT and must stay flagged.
	guarded := `package p

import "context"

type R struct{}
func (R) ListThings(ctx context.Context, page int) ([]int, error) { return nil, nil }

func run(ctx context.Context, r R, page int) {
	for {
		rows, _ := r.ListThings(ctx, page)
		if len(rows) == 0 { break }
		progressed := 0
		for range rows { progressed++ }
		if progressed == 0 { break }
	}
}
`
	repo, path := writeGo(t, guarded)
	if got := rules(scanFile(repo, path)); got["loop-no-cursor"] != 0 {
		t.Errorf("progress-break guarded loop must not trip loop-no-cursor, got %+v", got)
	}

	// Same loop with ONLY the len()==0 empty exit -> still a hang risk -> flagged.
	emptyOnly := `package p

import "context"

type R struct{}
func (R) ListThings(ctx context.Context, page int) ([]int, error) { return nil, nil }

func run(ctx context.Context, r R, page int) {
	for {
		rows, _ := r.ListThings(ctx, page)
		if len(rows) == 0 { break }
	}
}
`
	repo, path = writeGo(t, emptyOnly)
	if got := rules(scanFile(repo, path)); got["loop-no-cursor"] == 0 {
		t.Errorf("len()==0 empty-exit is not a progress guard; loop must be flagged, got %+v", got)
	}
}

func TestObligationSweeperWorkerN1Detection(t *testing.T) {
	// Obligation-sweeper worker pattern: per-batch, for each row, execute
	// a query/write. This is N+1 and must be detected.
	workerBad := `package p

import "context"

type Batch struct{ ID string }
type Pool struct{}
func (p *Pool) Exec(ctx context.Context, q string, args ...any) error { return nil }

func processBatches(ctx context.Context, pool *Pool) {
	batches := []Batch{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	for _, batch := range batches {
		// Per-batch/per-row Exec is N+1
		if err := pool.Exec(ctx, "UPDATE batches SET status='done' WHERE id=$1", batch.ID); err != nil {
			continue
		}
	}
}

`
	repo, path := writeGo(t, workerBad)
	got := rules(scanFile(repo, path))
	if got["n-plus-one"] == 0 {
		t.Errorf("worker loop with per-row Exec must detect n-plus-one, got %+v", got)
	}
}

func TestExplicitOneTimeCommandClassification(t *testing.T) {
	repo := t.TempDir()
	for rel := range explicitOneTimeCommands {
		if !isExplicitOneTimeCommand(repo, filepath.Join(repo, filepath.FromSlash(rel))) {
			t.Fatalf("expected explicit one-time command %s to be excluded", rel)
		}
	}
	for _, rel := range []string{
		"backend/cmd/obligation-sweeper/main.go",
		"backend/cmd/outbox-relay/main.go",
		"backend/internal/sop/adapters/postgres/repository.go",
	} {
		if isExplicitOneTimeCommand(repo, filepath.Join(repo, filepath.FromSlash(rel))) {
			t.Fatalf("production path %s must never be excluded", rel)
		}
	}
}

func TestBaselineRequiresOwnedUnexpiredMetadata(t *testing.T) {
	now := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	write := func(content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "baseline.txt")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	valid := "n-plus-one backend/internal/example.go 2 # owner=kernel issue=C35-020 expires=2026-09-30 reason=bounded legacy loop\n"
	allowed, problems := loadBaseline(write(valid), now)
	if len(problems) != 0 || allowed["n-plus-one\tbackend/internal/example.go"] != 2 {
		t.Fatalf("valid baseline = %#v problems=%v", allowed, problems)
	}
	for name, content := range map[string]string{
		"anonymous": "n-plus-one backend/internal/example.go 1 # legacy\n",
		"expired":   "n-plus-one backend/internal/example.go 1 # owner=kernel issue=C35-020 expires=2026-07-01 reason=old\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, problems := loadBaseline(write(content), now)
			if len(problems) == 0 {
				t.Fatal("invalid baseline passed")
			}
		})
	}
}

// writeGoAt writes src at a chosen repo-relative path so path-scoped rules can be exercised.
func writeGoAt(t *testing.T, relPath, src string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, p
}

// TestHotPathInlineSQLRejectsAFunctionLocalStatement is the adversarial fixture for
// hot-path-inline-sql.
//
// The positive case is the shape GET /vaccination/command actually shipped: a multi-line statement
// declared as a local inside the repository method that runs it. Nothing could address it -- not a
// plan test, not this scanner -- so its plan regressed into a 15s pool timeout with every test
// green and the board showing "Unable to load command board".
func TestHotPathInlineSQLRejectsAFunctionLocalStatement(t *testing.T) {
	src := "package postgres\n\nimport \"context\"\n\n" +
		"type R struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }\n\n" +
		"func (r *R) Board(ctx context.Context) {\n" +
		"\tboardSQL := `\nSELECT g.goat_id, o.status\nFROM obligation_instances o\nJOIN goats g ON g.goat_id = o.target_id\nWHERE o.tenant_id = $1\n`\n" +
		"\t_, _ = r.pool.Query(ctx, boardSQL, \"t\")\n}\n"

	repo, path := writeGoAt(t, "backend/internal/vaccinationexecution/adapters/postgres/repository.go", src)
	got := rules(scanFile(repo, path))
	if got["hot-path-inline-sql"] != 1 {
		t.Fatalf("hot-path-inline-sql = %d, want 1; the guard does not reject a function-local hot-path statement, "+
			"which is the exact shape that hid the command board's plan regression", got["hot-path-inline-sql"])
	}
}

// TestHotPathInlineSQLAcceptsANamedPackageLevelStatement is the negative half.
//
// The SAME statement as a package-level const must pass: it has a name, so a query-plan test can
// EXPLAIN it and a reviewer can diff it. A guard that flagged this too would be telling authors to
// stop writing SQL rather than to name it.
func TestHotPathInlineSQLAcceptsANamedPackageLevelStatement(t *testing.T) {
	src := "package postgres\n\nimport \"context\"\n\n" +
		"type R struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }\n\n" +
		"const boardSQL = `\nSELECT g.goat_id, o.status\nFROM obligation_instances o\nJOIN goats g ON g.goat_id = o.target_id\nWHERE o.tenant_id = $1\n`\n\n" +
		"func (r *R) Board(ctx context.Context) {\n\t_, _ = r.pool.Query(ctx, boardSQL, \"t\")\n}\n"

	repo, path := writeGoAt(t, "backend/internal/vaccinationexecution/adapters/postgres/repository.go", src)
	if got := rules(scanFile(repo, path)); got["hot-path-inline-sql"] != 0 {
		t.Fatalf("hot-path-inline-sql = %d, want 0; a NAMED package-level statement is the fix this rule asks for "+
			"and must not itself be flagged", got["hot-path-inline-sql"])
	}
}

// TestHotPathInlineSQLIsScopedToPostgresAdapters keeps the rule where serving SQL lives. The same
// literal in a one-off command or a test helper is not a hot path, and flagging it would train
// authors to reach for scale-guard:ignore -- which is how a guard stops meaning anything.
func TestHotPathInlineSQLIsScopedToPostgresAdapters(t *testing.T) {
	src := "package tool\n\nimport \"context\"\n\n" +
		"type R struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }\n\n" +
		"func (r *R) Board(ctx context.Context) {\n" +
		"\tboardSQL := `\nSELECT g.goat_id, o.status\nFROM obligation_instances o\nJOIN goats g ON g.goat_id = o.target_id\nWHERE o.tenant_id = $1\n`\n" +
		"\t_, _ = r.pool.Query(ctx, boardSQL, \"t\")\n}\n"

	repo, path := writeGoAt(t, "backend/internal/tools/importer/importer.go", src)
	if got := rules(scanFile(repo, path)); got["hot-path-inline-sql"] != 0 {
		t.Fatalf("hot-path-inline-sql = %d, want 0 outside a postgres adapter", got["hot-path-inline-sql"])
	}
}

// TestHotPathInlineSQLIgnoresShortLiterals pins the multi-line threshold. A short single-table
// lookup is not the shape that hides a plan regression.
func TestHotPathInlineSQLIgnoresShortLiterals(t *testing.T) {
	src := "package postgres\n\nimport \"context\"\n\n" +
		"type R struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }\n\n" +
		"func (r *R) One(ctx context.Context) {\n" +
		"\t_, _ = r.pool.Query(ctx, `SELECT name FROM locations WHERE location_id = $1`, \"t\")\n}\n"

	repo, path := writeGoAt(t, "backend/internal/x/adapters/postgres/repository.go", src)
	if got := rules(scanFile(repo, path)); got["hot-path-inline-sql"] != 0 {
		t.Fatalf("hot-path-inline-sql = %d, want 0 for a short single-line lookup", got["hot-path-inline-sql"])
	}
}

// TestHotPathInlineSQLRejectsConcatenatedStatements closes a one-keystroke evasion.
//
// Splitting a statement across a `+` is gofmt-stable and leaves each half under the newline
// threshold, so measuring literals individually let the whole shape through. The rule is about SQL
// a plan test can NAME; assembling it from two anonymous halves is no more nameable than one.
func TestHotPathInlineSQLRejectsConcatenatedStatements(t *testing.T) {
	src := "package postgres\n\nimport \"context\"\n\n" +
		"type R struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }\n\n" +
		"func (r *R) Board(ctx context.Context) {\n" +
		"\tq := \"SELECT g.goat_id\\nFROM obligation_instances o\\n\" + \"JOIN goats g ON g.goat_id = o.target_id\\nWHERE o.tenant_id = $1\\n\"\n" +
		"\t_, _ = r.pool.Query(ctx, q, \"t\")\n}\n"

	repo, path := writeGoAt(t, "backend/internal/x/adapters/postgres/repository.go", src)
	if got := rules(scanFile(repo, path))["hot-path-inline-sql"]; got != 1 {
		t.Fatalf("hot-path-inline-sql = %d, want 1; a statement split across a `+` is still unnameable hot-path SQL", got)
	}
}

// TestHotPathInlineSQLRejectsBlockCommentHeader pins the multi-line /* */ case. The single-line
// `--` header was already covered; a block comment spanning two lines slipped the start match
// because the regex was not in dot-matches-newline mode.
func TestHotPathInlineSQLRejectsBlockCommentHeader(t *testing.T) {
	src := "package postgres\n\nimport \"context\"\n\n" +
		"type R struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }\n\n" +
		"func (r *R) Board(ctx context.Context) {\n" +
		"\tq := `/* board read\n   owner: preventive care */\nSELECT g.goat_id\nFROM obligation_instances o\nJOIN goats g ON g.goat_id = o.target_id\nWHERE o.tenant_id = $1\n`\n" +
		"\t_, _ = r.pool.Query(ctx, q, \"t\")\n}\n"

	repo, path := writeGoAt(t, "backend/internal/x/adapters/postgres/repository.go", src)
	if got := rules(scanFile(repo, path))["hot-path-inline-sql"]; got != 1 {
		t.Fatalf("hot-path-inline-sql = %d, want 1; a multi-line block-comment header must not hide the statement under it", got)
	}
}

// TestHotPathInlineSQLIgnoresBuiltQueries keeps the rule off dynamic assembly. A `+` chain carrying
// a variable is a query being BUILT, not a statement this rule can read or a plan test can pin, and
// flagging it would push authors toward scale-guard:ignore.
func TestHotPathInlineSQLIgnoresBuiltQueries(t *testing.T) {
	src := "package postgres\n\nimport \"context\"\n\n" +
		"type R struct{ pool interface{ Query(context.Context, string, ...any) (any, error) } }\n\n" +
		"const filterClause = \"AND o.status = $2\"\n\n" +
		"func (r *R) Board(ctx context.Context, extra string) {\n" +
		"\tq := \"SELECT g.goat_id\\nFROM obligation_instances o\\nJOIN goats g ON g.goat_id = o.target_id\\n\" + extra\n" +
		"\t_, _ = r.pool.Query(ctx, q, \"t\")\n}\n"

	repo, path := writeGoAt(t, "backend/internal/x/adapters/postgres/repository.go", src)
	if got := rules(scanFile(repo, path))["hot-path-inline-sql"]; got != 0 {
		t.Fatalf("hot-path-inline-sql = %d, want 0 for a query assembled from a variable", got)
	}
}
