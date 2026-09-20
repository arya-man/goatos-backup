package sqlbind

import (
	"strings"
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
