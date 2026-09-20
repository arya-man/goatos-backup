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
		{"strict named", `p.Query(ctx,q,pgx.StrictNamedArgs{"id":a})`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := "package p\nfunc f(){" + tt.body + "}"
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
func f(){ q := sqlbind.MustBind(buildSQL(), args...); p.Query(ctx,q.SQL,q.Args...) }`
	if got := findings(t, src); len(got) != 0 {
		t.Fatalf("%+v", got)
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
