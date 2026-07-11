package main

import (
	"os"
	"path/filepath"
	"testing"
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

const godCTE = "WITH a AS (SELECT 1), b AS (SELECT 1), c AS (SELECT 1), d AS (SELECT 1), e AS (SELECT 1), f AS (SELECT 1), g AS (SELECT 1), h AS (SELECT 1), i AS (SELECT 1), j AS (SELECT 1) SELECT * FROM a"
`
	repo, path := writeGo(t, src)
	got := rules(scanFile(repo, path))
	for _, want := range []string{"n-plus-one", "offset-pagination", "full-mv-refresh", "non-sargable-like", "god-cte"} {
		if got[want] == 0 {
			t.Errorf("expected rule %q to fire, got %+v", want, got)
		}
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
