package sqlbind

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"testing"
)

func TestValidatePositionalMatrix(t *testing.T) {
	tests := []struct {
		name, sql string
		args      []any
		want      string
	}{
		{"valid repeated out of order", `select $2,$1,$2`, []any{1, 2}, ""},
		{"dollar identifier", `select 1 as value$1`, nil, ""},
		{"dollar identifier extra arg", `select 1 as value$1`, []any{1}, "unused argument"},
		{"dollar quote shaped identifier", `select 1 as value$tag$`, nil, ""},
		{"unicode identifier", `select 1 as 名$1`, nil, ""},
		{"identifier and parameter", `select $1 as value$2`, []any{1}, ""},
		{"valid empty", `select now()`, nil, ""},
		{"missing arg", `select $1,$2`, []any{1}, "missing argument"},
		{"extra arg", `select $1`, []any{1, 2}, "unused argument"},
		{"gap", `select $1,$3`, []any{1, 2, 3}, "gaps"},
		{"starts at two", `select $2`, []any{1, 2}, "gaps"},
		{"pruned highest", `select $1`, []any{1, 2}, "unused argument"},
		{"huge ordinal bounded", `select $1000000000`, []any{1}, "missing argument count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePositional(tt.sql, tt.args)
			if tt.want == "" && err != nil {
				t.Fatal(err)
			}
			if tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("error=%v want substring %q", err, tt.want)
			}
		})
	}
}

func TestPlaceholderLexerIgnoresNonCode(t *testing.T) {
	sql := `select $1, '$9', E'escaped \' $8', "$7", $$ body $6 $$, $tag$ body $5 $tag$
-- $4
/* $3 /* $2 */ still */ , $2`
	got, err := PlaceholderOrdinals(sql)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{1, 2}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
}

func TestPlaceholderLexerRejectsMalformedSQLRegions(t *testing.T) {
	for _, sql := range []string{`select /* nope`, `select $tag$ nope`, `select 'nope`, `select "nope`} {
		if _, err := PlaceholderOrdinals(sql); err == nil {
			t.Fatalf("expected error for %q", sql)
		}
	}
}

func TestValidateNamedBidirectional(t *testing.T) {
	for _, tc := range []struct {
		name, sql string
		keys      []string
		wantErr   bool
	}{
		{"valid repeated", `select @id, @id`, []string{"id"}, false},
		{"missing", `select @id, @tenant`, []string{"id"}, true},
		{"extra", `select @id`, []string{"id", "unused"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateNamed(tc.sql, tc.keys)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestBindCopiesArgsAndMustBindPanics(t *testing.T) {
	args := []any{1}
	q, err := Bind(`select $1`, args...)
	if err != nil {
		t.Fatal(err)
	}
	args[0] = 2
	if q.Args()[0] != 1 {
		t.Fatal("args not copied")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	MustBind(`select $2`, 1)
}

// Controls must be rejected at every position: the bound slice may be reused
// with either Query or Exec, or after an execution option at the call site.
func TestBindRejectsPGXControls(t *testing.T) {
	for _, arg := range []any{
		pgx.QueryExecModeExec,
		pgx.QueryResultFormats{0},
		pgx.QueryResultFormatsByOID{23: 0},
		pgx.NamedArgs{"id": 1},
		pgx.StrictNamedArgs{"id": 1},
		testQueryRewriter{},
		(*testQueryRewriter)(nil),
	} {
		for _, args := range [][]any{{arg, 1}, {1, arg}} {
			if _, err := Bind("select $1, $2", args...); err == nil || !strings.Contains(err.Error(), "not a data argument") {
				t.Errorf("Bind(%T): %v", arg, err)
			}
		}
	}
}

type testQueryRewriter struct{}

func (testQueryRewriter) RewriteQuery(context.Context, *pgx.Conn, string, []any) (string, []any, error) {
	return "select 1", nil, nil
}

func TestPlaceholderLexerPostgresTokenBoundaries(t *testing.T) {
	for _, sql := range []string{
		"select 1 -- comment\r, $1",
		"select $名$ $2 $名$, $1",
		"select $名1$ $2 $名1$, $1",
	} {
		if err := ValidatePositional(sql, []any{1}); err != nil {
			t.Errorf("%q: %v", sql, err)
		}
	}
}

func TestPlaceholderLexerContinuedEscapeStrings(t *testing.T) {
	for _, gap := range []string{"\n", "\r", " \t\f\n ", " -- comment\n", "\n-- comment\n", "\r-- comment\r"} {
		sql := "select E'a'" + gap + "'\\' $2', $1::int"
		if err := ValidatePositional(sql, []any{9}); err != nil {
			t.Errorf("gap %q: %v", gap, err)
		}
		if err := ValidatePositional(sql, nil); err == nil {
			t.Errorf("gap %q: missing actual parameter was accepted", gap)
		}
	}
	// Continuation can span multiple literals and must keep the original mode.
	if err := ValidatePositional("select E'a'\n'b'\n'\\' $2', $1::int", []any{9}); err != nil {
		t.Fatal(err)
	}
	// Ordinary strings retain standard-conforming semantics across a newline.
	if err := ValidatePositional("select 'a'\n'\\', $1::int", []any{9}); err != nil {
		t.Fatal(err)
	}
}

func TestStringContinuationRequiresPostgresWhitespace(t *testing.T) {
	for _, suffix := range []string{" 'next'", "\t'next'", "\f'next'", "\v\n'next'", " /* comment */\n'next'", "\n/* comment */'next'", " -- no newline", "\nE'next'"} {
		if _, ok := continuedStringStart(suffix, 0); ok {
			t.Errorf("invalid continuation %q was accepted", suffix)
		}
	}
}
