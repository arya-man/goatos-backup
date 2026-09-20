package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func findings(t *testing.T, src string) []finding {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "fixture.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return scanFile("fixture.go", fs, f)
}

func TestStaticCallMatrix(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"execution mode alias", `mode := pgx.QueryExecModeExec; p.QueryRow(ctx,"select $1",mode)`, "unverified-dynamic-args"},
		{"execution mode conversion", `p.QueryRow(ctx,"select $1",pgx.QueryExecMode(1))`, "unverified-dynamic-args"},
		{"execution mode declared type", `var mode pgx.QueryExecMode; p.QueryRow(ctx,"select $1",mode)`, "unverified-dynamic-args"},
		{"format alias", `formats := pgx.QueryResultFormats{0}; p.Query(ctx,"select $1",formats)`, "unverified-dynamic-args"},
		{"named rewriter", `rewrite := pgx.NamedArgs{"id":1}; p.Query(ctx,"select $1",rewrite)`, "unverified-dynamic-args"},
		{"rewriter declared type", `var rewrite pgx.QueryRewriter; p.Query(ctx,"select $1",rewrite)`, "unverified-dynamic-args"},
		{"valid", `p.Query(ctx,"select $1,$2",a,b)`, ""},
		{"ordinary struct field", `opts := struct { ID int }{ID: 1}; p.Exec(ctx,"select $1",opts.ID)`, ""},
		{"ordinary slice element", `ids := []int{1}; p.Exec(ctx,"select $1",ids[0])`, ""},
		{"ordinary map element", `ids := map[string]int{"id":1}; p.Exec(ctx,"select $1",ids["id"])`, ""},
		{"batch indexed execution mode is data", `modes := []pgx.QueryExecMode{pgx.QueryExecModeExec}; b.Queue("select $1",modes[0])`, ""},
		{"constant concat", `const q = "select " + "$1"; p.Exec(ctx, q, a)`, ""},
		{"missing arg", `p.QueryRow(ctx,"select $1,$2",a)`, "positional-bind-mismatch"},
		{"extra arg", `p.Exec(ctx,"select $1",a,b)`, "positional-bind-mismatch"},
		{"gap", `p.Query(ctx,"select $1,$3",a,b,c)`, "positional-bind-mismatch"},
		{"dynamic sql", `p.Query(ctx,q,a)`, "unverified-dynamic-bind"},
		{"dynamic sql shadows same-named const", `const q = "select $1"; { q := build(); p.Query(ctx,q,a) }`, "unverified-dynamic-bind"},
		{"dynamic args", `p.Query(ctx,"select $1",args...)`, "unverified-dynamic-args"},
		{"batch", `b.Queue("select $1,$2",a)`, "positional-bind-mismatch"},
		{"option", `p.Query(ctx,"select $1",pgx.QueryExecModeExec,pgx.QueryResultFormats{0},a)`, ""},
		{"trailing option is data", `p.Query(ctx,"select $1",a,pgx.QueryExecModeExec)`, "positional-bind-mismatch"},
		{"batch named rewriter literal", `b.Queue("select $1",pgx.NamedArgs{"id":1})`, "unverified-dynamic-args"},
		{"batch named rewriter alias", `rewrite := pgx.NamedArgs{"id":1}; b.Queue("select $1",rewrite)`, "unverified-dynamic-args"},
		{"batch declared rewriter", `var rewrite pgx.QueryRewriter; b.Queue("select $1",rewrite)`, "unverified-dynamic-args"},
		{"batch execution option value", `b.Queue("select $1",pgx.QueryExecModeExec)`, ""},
		{"batch execution option alias value", `mode := pgx.QueryExecModeExec; b.Queue("select $1",mode)`, ""},
		{"batch option is data", `b.Queue("select $1",pgx.QueryExecModeExec,a)`, "positional-bind-mismatch"},
		{"strict named", `p.Query(ctx,"select @id",pgx.StrictNamedArgs{"id":a})`, ""},
		{"strict named missing key", `p.Query(ctx,"select @id, @tenant",pgx.StrictNamedArgs{"id":a})`, "invalid-strict-named-args"},
		{"strict named extra key", `p.Query(ctx,"select @id",pgx.StrictNamedArgs{"id":a,"unused":b})`, "invalid-strict-named-args"},
		{"strict named wrong key", `p.Query(ctx,"select @id",pgx.StrictNamedArgs{"other":a})`, "invalid-strict-named-args"},
		{"strict named with option", `p.Query(ctx,"select @id",pgx.QueryExecModeExec,pgx.StrictNamedArgs{"id":a})`, ""},
		{"strict named extra arg", `p.Query(ctx,"select @id",a,pgx.StrictNamedArgs{"id":a})`, "positional-bind-mismatch"},
		{"strict named trailing arg", `p.Query(ctx,"select @id",pgx.StrictNamedArgs{"id":a},a)`, "invalid-strict-named-args"},
		{"strict named mixed positional", `p.Query(ctx,"select $1, @id",pgx.StrictNamedArgs{"id":a})`, "invalid-strict-named-args"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package p\nimport (\"github.com/jackc/pgx/v5\"; \"github.com/vgoats/goatos/backend/internal/platform/sqlbind\")\nfunc f(){" + tt.body + "}"
			got := findings(t, src)
			if tt.want == "" && len(got) != 0 {
				t.Fatalf("%+v", got)
			}
			if tt.want != "" && (len(got) != 1 || got[0].Kind != tt.want) {
				t.Fatalf("got %+v want %s", got, tt.want)
			}
		})
	}
}

func TestBoundQueryApproved(t *testing.T) {
	src := `package p
import "github.com/vgoats/goatos/backend/internal/platform/sqlbind"
func f(){ q := sqlbind.MustBind(buildSQL(), args...); p.Query(ctx,q.SQL(),q.Args()...) }`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestBoundQueryHostileShapes(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"late initialization untrusted", `var q sqlbind.BoundQuery; p.Query(ctx,q.SQL(),q.Args()...); q = sqlbind.MustBind(buildSQL(), args...)`, "unverified-dynamic-bind"},
		{"tuple reassignment untrusted", `q := sqlbind.MustBind(buildSQL(), args...); _, q = other(); p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"pointer escape untrusted", `q := sqlbind.MustBind(buildSQL(), args...); mutate(&q); p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"use inside error guard untrusted", `q, err := sqlbind.Bind(buildSQL(), args...); if err != nil { p.Query(ctx,q.SQL(),q.Args()...); return }`, "unverified-dynamic-bind"},
		{"parenthesized pointer escape", `q := sqlbind.MustBind(buildSQL(), args...); mutate(&(q)); p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"range overwrites bound query", `q := sqlbind.MustBind(buildSQL(), args...); for _, q = range queries { p.Query(ctx,q.SQL(),q.Args()...) }`, "unverified-dynamic-bind"},
		{"goto bypasses guard", `q, err := sqlbind.Bind(buildSQL(), args...); if err != nil { goto proceed; return }; proceed: p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"parenthesized assignment", `q := sqlbind.MustBind(buildSQL(), args...); (q) = other; p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"ordinary Bind untrusted", `q, _ := sqlbind.Bind(buildSQL(), args...); p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"checked Bind trusted", `q, err := sqlbind.Bind(buildSQL(), args...); if err != nil { return }; p.Query(ctx,q.SQL(),q.Args()...)`, ""},
		{"checked Bind reversed trusted", `q, err := sqlbind.Bind(buildSQL(), args...); if nil != err { return }; p.Query(ctx,q.SQL(),q.Args()...)`, ""},
		{"Bind delayed check untrusted", `q, err := sqlbind.Bind(buildSQL(), args...); log(err); if err != nil { return }; p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"Bind overwritten error untrusted", `q, err := sqlbind.Bind(buildSQL(), args...); err = other(); if err != nil { return }; p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"Bind nil branch untrusted", `q, err := sqlbind.Bind(buildSQL(), args...); if err == nil { return }; p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"Bind nonterminating guard untrusted", `q, err := sqlbind.Bind(buildSQL(), args...); if err != nil { log(err) }; p.Query(ctx,q.SQL(),q.Args()...)`, "unverified-dynamic-bind"},
		{"extra data before args", `q := sqlbind.MustBind(buildSQL(), args...); p.Query(ctx,q.SQL(),extra,q.Args()...)`, "unverified-dynamic-bind"},
		{"valid query option", `q := sqlbind.MustBind(buildSQL(), args...); p.Query(ctx,q.SQL(),pgx.QueryExecModeExec,q.Args()...)`, ""},
		{"queue rejects option", `q := sqlbind.MustBind(buildSQL(), args...); b.Queue(q.SQL(),pgx.QueryExecModeExec,q.Args()...)`, "unverified-dynamic-bind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := `package p
import("github.com/jackc/pgx/v5";"github.com/vgoats/goatos/backend/internal/platform/sqlbind")
func f(){` + tt.body + `}`
			got := findings(t, src)
			if tt.want == "" && len(got) != 0 {
				t.Fatalf("%+v", got)
			}
			if tt.want != "" && (len(got) != 1 || got[0].Kind != tt.want) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestBoundQueryObjectsAreScopeSafe(t *testing.T) {
	src := `package p
import "github.com/vgoats/goatos/backend/internal/platform/sqlbind"
func good(){ q := sqlbind.MustBind(buildSQL(), args...); p.Query(ctx,q.SQL(),q.Args()...) }
func bad(){ q := other(); p.Query(ctx,q.SQL(),q.Args()...) }`
	got := findings(t, src)
	if len(got) != 1 || got[0].Kind != "unverified-dynamic-bind" {
		t.Fatalf("%+v", got)
	}
}

func TestStrictNamedIdentifierAndReassignment(t *testing.T) {
	tests := []struct{ name, body, want string }{
		{"deleted key", `a := pgx.StrictNamedArgs{"id":v}; delete(a,"id"); p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
		{"added key", `a := pgx.StrictNamedArgs{"id":v}; a["unused"] = v; p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
		{"cleared map", `a := pgx.StrictNamedArgs{"id":v}; clear(a); p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
		{"aliased map", `a := pgx.StrictNamedArgs{"id":v}; b := a; delete(b,"id"); p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
		{"helper escape", `a := pgx.StrictNamedArgs{"id":v}; mutate(a); p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
		{"pointer escape", `a := pgx.StrictNamedArgs{"id":v}; mutate(&a); p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
		{"repeated safe reads", `a := pgx.StrictNamedArgs{"id":v}; p.Query(ctx,"select @id",a); p.Exec(ctx,"select @id",a)`, ""},
		{"identifier valid", `a := pgx.StrictNamedArgs{"id":v}; p.Query(ctx,"select @id",a)`, ""},
		{"identifier missing", `a := pgx.StrictNamedArgs{"id":v}; p.Query(ctx,"select @id,@tenant",a)`, "invalid-strict-named-args"},
		{"reassigned", `a := pgx.StrictNamedArgs{"id":v}; a = other; p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
		{"dynamic declared", `var a pgx.StrictNamedArgs; p.Query(ctx,"select @id",a)`, "unverified-dynamic-args"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := `package p
import "github.com/jackc/pgx/v5"
func f(){` + tt.body + `}`
			got := findings(t, src)
			if tt.want == "" && len(got) != 0 {
				t.Fatalf("%+v", got)
			}
			if tt.want != "" && (len(got) != 1 || got[0].Kind != tt.want) {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestStrictNamedObjectsAreScopeSafe(t *testing.T) {
	src := `package p
import "github.com/jackc/pgx/v5"
func good(){ a := pgx.StrictNamedArgs{"id":v}; p.Query(ctx,"select @id",a) }
func bad(){ a := pgx.StrictNamedArgs{"other":v}; p.Query(ctx,"select @id",a) }`
	got := findings(t, src)
	if len(got) != 1 || got[0].Kind != "invalid-strict-named-args" {
		t.Fatalf("%+v", got)
	}
}

func TestImportedPackageExecIgnored(t *testing.T) {
	src := `package p
import "text/template"
func f(){ template.Exec(ctx,"select $2",a) }`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestImportNamesCannotBeShadowedToBypassGuard(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"shadow pgx option", `package p
import "github.com/jackc/pgx/v5"
func f(){ pgx := fake(); p.Query(ctx,"select $1",pgx.QueryExecModeExec,a) }`, "positional-bind-mismatch"},
		{"shadow pgx strict named", `package p
import "github.com/jackc/pgx/v5"
func f(){ pgx := fake(); p.Query(ctx,"select @id",pgx.StrictNamedArgs{"id":a}) }`, "positional-bind-mismatch"},
		{"shadow sqlbind", `package p
import "github.com/vgoats/goatos/backend/internal/platform/sqlbind"
func f(){ sqlbind := fake(); q := sqlbind.MustBind(build(),args...); p.Query(ctx,q.SQL(),q.Args()...) }`, "unverified-dynamic-bind"},
		{"shadow imported non pgx package", `package p
import "text/template"
func f(){ template := fake(); template.Exec(ctx,"select $2",a) }`, "positional-bind-mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, tt.src)
			if len(got) != 1 || got[0].Kind != tt.want {
				t.Fatalf("got %+v want %s", got, tt.want)
			}
		})
	}
}

func TestReassignedBoundQueryIsNotApproved(t *testing.T) {
	src := `package p
import "github.com/vgoats/goatos/backend/internal/platform/sqlbind"
func f(){ q := sqlbind.MustBind(buildSQL(), args...); q = other(); p.Query(ctx,q.SQL(),q.Args()...) }`
	got := findings(t, src)
	if len(got) != 1 || got[0].Kind != "unverified-dynamic-bind" {
		t.Fatalf("got %+v", got)
	}
}

func TestQuotedFakePlaceholderDoesNotAffectCount(t *testing.T) {
	src := "package p\nfunc f(){p.Query(ctx,`select $1, '$9', $$ $8 $$ /* $7 */ -- $6\\n`,a)}"
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestScanTreeExcludesTestsAndGenerated(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "ok.go"), []byte("package p\nfunc f(){p.Query(ctx,`select $1`,a,b)}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "ignored_test.go"), []byte("package p\nfunc f(){p.Query(ctx,`select $1`,a,b)}"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := scanTree(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.HasSuffix(got[0].File, "ok.go") {
		t.Fatalf("%+v", got)
	}
}

// A file-local scanner cannot resolve package constants declared in other files.
// In particular, it must never substitute a constant from an unrelated function.
func TestCrossFileConstantFailsClosed(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"constants.go": `package p
const query = "select $1,$2"`,
		"queries.go": `package p
import "github.com/jackc/pgx/v5"
func unrelated() { const query = "select $1"; _ = query }
func run() { p.Exec(ctx, query, 1) }`,
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(src), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := scanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != "unverified-dynamic-bind" {
		t.Fatalf("cross-file constant must remain unverified, got %+v", got)
	}
}

func TestConstantResolutionPreservesScope(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"package constant", `const q = "select $1,$2"
func unrelated() { const q = "select $1"; _ = q }
func run() { p.Exec(ctx,q,1) }`, "positional-bind-mismatch"},
		{"same name distinct objects", `const q = "select $1"
func run() { const outer = q; { const q = outer; p.Exec(ctx,q,1) } }`, ""},
		{"implicit initializer fails closed", `func unrelated() { const q = "select $1"; _ = q }
func run() { const (base = "select $1,$2"; q); p.Exec(ctx,q,1) }`, "unverified-dynamic-bind"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(t, "package p\nimport \"github.com/jackc/pgx/v5\"\n"+tt.src)
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("%+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Kind != tt.want {
				t.Fatalf("got %+v want %s", got, tt.want)
			}
		})
	}
}

func TestControlProvenance(t *testing.T) {
	for _, body := range []string{
		`p.Exec(ctx, "select $1::int", rewrite{})`,
		`opts := struct { Mode pgx.QueryExecMode }{Mode: pgx.QueryExecModeExec}; p.Exec(ctx, "select $1::int", opts.Mode)`,
		`modes := []pgx.QueryExecMode{pgx.QueryExecModeExec}; p.Exec(ctx, "select $1::int", modes[0])`,
		`opts := struct { Mode any }{}; opts.Mode = pgx.QueryExecModeExec; p.Exec(ctx, "select $1::int", opts.Mode)`,
		`modes := []any{1}; modes[0] = pgx.QueryExecModeExec; p.Exec(ctx, "select $1::int", modes[0])`,
		`modes := map[string]any{"mode": pgx.QueryExecModeExec}; p.Exec(ctx, "select $1::int", modes["mode"])`,
		`opts := struct { R rewrite }{}; b.Queue("select $1::int", opts.R)`,
		`rs := []rewrite{{}}; b.Queue("select $1::int", rs[0])`,
		`p.Query(ctx, "select $1::int", &rewrite{})`,
		`b.Queue("select $1::int", rewrite{})`,
		`r := rewrite{}; p.QueryRow(ctx, "select $1::int", r)`,
		`var mode any = 1; mode = pgx.QueryExecModeExec; p.Exec(ctx, "select $1::int", mode)`,
		`mode := any(1); mode = pgx.QueryResultFormats{0}; p.Query(ctx, "select $1::int", mode)`,
		`var mode any = 1; alias := pgx.QueryExecModeExec; mode = alias; p.Exec(ctx, "select $1::int", mode)`,
		`var r any = 1; r = rewrite{}; b.Queue("select $1::int", r)`,
		`for _, mode := range []pgx.QueryExecMode{pgx.QueryExecModeExec} { p.Exec(ctx, "select $1::int", mode) }`,
		`var mode any; for _, mode = range []pgx.QueryExecMode{pgx.QueryExecModeExec} { p.Exec(ctx, "select $1::int", mode) }`,
		`for _, r := range []rewrite{{}} { b.Queue("select $1::int", r) }`,
	} {
		t.Run(body, func(t *testing.T) {
			src := `package p
import ("context"; "github.com/jackc/pgx/v5")
type rewrite struct{}
func (rewrite) RewriteQuery(_ context.Context, _ *pgx.Conn, sql string, _ []any) (string, []any, error) { return sql, nil, nil }
func run() {` + body + `}`
			got := findings(t, src)
			if len(got) != 1 || got[0].Kind != "unverified-dynamic-args" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestRangeDataProvenanceAccepted(t *testing.T) {
	src := `package p
func run() {
 for _, id := range []int{1} { p.Exec(ctx, "select $1::int", id) }
}`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestControlProvenanceAcrossFiles(t *testing.T) {
	root := t.TempDir()
	for name, source := range map[string]string{
		"types.go": `package p
import ("context"; driver "github.com/jackc/pgx/v5")
type rewrite struct{}
func (*rewrite) RewriteQuery(_ context.Context, _ *driver.Conn, sql string, _ []any) (string, []any, error) { return sql, nil, nil }
func newRewrite() *rewrite { return &rewrite{} }
var mode any = driver.QueryExecModeExec`,
		"queries.go": `package p
func run() {
 p.Exec(ctx, "select $1", &rewrite{})
 p.QueryRow(ctx, "select $1", newRewrite())
 p.Exec(ctx, "select $1", mode)
}
func unrelated() { type rewrite int; p.Exec(ctx, "select $1", rewrite(1)) }`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := scanTree(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %+v", got)
	}
	for _, f := range got {
		if f.Kind != "unverified-dynamic-args" {
			t.Fatalf("got %+v", got)
		}
	}
}

func TestLongDataAliasChain(t *testing.T) {
	var source strings.Builder
	source.WriteString("package p\nfunc run(){a0 := 1;\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&source, "a%d := a%d;\n", i, i-1)
	}
	source.WriteString(`p.Exec(ctx,"select $1",a40)}`)
	if got := findings(t, source.String()); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
