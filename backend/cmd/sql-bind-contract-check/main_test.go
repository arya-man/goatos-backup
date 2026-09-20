package main

import (
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
		{"valid", `p.Query(ctx,"select $1,$2",a,b)`, ""},
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
