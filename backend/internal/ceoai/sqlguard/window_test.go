package sqlguard

import (
	"errors"
	"testing"
	"time"
)

type fakeCard struct{ name, dateCol string }

func (c fakeCard) CardName() string       { return c.name }
func (c fakeCard) CardDateColumn() string { return c.dateCol }

func augustWindow() Window {
	return Window{
		From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		To:   time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
	}
}

const tenant = "11111111-1111-1111-1111-111111111111"

func TestValidateWindowRequiredDateLiterals(t *testing.T) {
	card := fakeCard{name: "mortality_base", dateCol: "event_date"}
	w := augustWindow()

	cases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{
			name: "both bounds present, half-open",
			sql: "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value, park_label AS scope FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' AND event_date >= '2026-08-01' AND event_date < '2026-09-01' GROUP BY park_label LIMIT 100",
		},
		{
			name: "alias-qualified column accepted",
			sql: "SELECT 'Deaths' AS label, CAST(sum(m.deaths) AS text) AS value FROM ceo_ai.mortality_base m " +
				"WHERE m.tenant_id = '" + tenant + "' AND m.event_date >= '2026-08-01' AND m.event_date < '2026-09-01' LIMIT 100",
		},
		{
			name: "case-insensitive column and keywords",
			sql: "select 'x' as label, cast(count(*) as text) as value from ceo_ai.mortality_base " +
				"where tenant_id = '" + tenant + "' and EVENT_DATE >= '2026-08-01' and Event_Date < '2026-09-01' limit 10",
		},
		{
			name: "no window predicate at all",
			sql: "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' LIMIT 100",
			wantErr: true,
		},
		{
			name: "lower bound only",
			sql: "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' AND event_date >= '2026-08-01' LIMIT 100",
			wantErr: true,
		},
		{
			name: "inclusive upper bound (<=) is the wrong shape",
			sql: "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' AND event_date >= '2026-08-01' AND event_date <= '2026-08-31' LIMIT 100",
			wantErr: true,
		},
		{
			name: "wrong literal (off by one month)",
			sql: "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' AND event_date >= '2026-07-01' AND event_date < '2026-08-01' LIMIT 100",
			wantErr: true,
		},
		{
			name: "BETWEEN is not accepted",
			sql: "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' AND event_date BETWEEN '2026-08-01' AND '2026-09-01' LIMIT 100",
			wantErr: true,
		},
		{
			name: "window bound on a different column",
			sql: "SELECT 'Deaths' AS label, CAST(sum(deaths) AS text) AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' AND created_at >= '2026-08-01' AND created_at < '2026-09-01' LIMIT 100",
			wantErr: true,
		},
		{
			name: "literals hidden inside a string do not count",
			sql: "SELECT 'event_date >= ''2026-08-01'' AND event_date < ''2026-09-01''' AS label, '1' AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' LIMIT 1",
			wantErr: true,
		},
		{
			name: "predicate before WHERE (projection) does not count",
			sql: "SELECT event_date >= '2026-08-01' AND event_date < '2026-09-01' AS label FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' LIMIT 1",
			wantErr: true,
		},
		{
			name: "longer identifier containing the column name is not a match",
			sql: "SELECT 'x' AS label, '1' AS value FROM ceo_ai.mortality_base " +
				"WHERE tenant_id = '" + tenant + "' AND prev_event_date >= '2026-08-01' AND prev_event_date < '2026-09-01' LIMIT 1",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWindow(tc.sql, card, w)
			if tc.wantErr && err == nil {
				t.Fatalf("expected a window rejection for %q", tc.sql)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
			if tc.wantErr {
				if _, ok := AsValidationError(err); !ok {
					t.Fatalf("window rejection must be a *ValidationError, got %T (%v)", err, err)
				}
			}
		})
	}
}

func TestValidateWindowCurrentStateViewRefusesPeriod(t *testing.T) {
	card := fakeCard{name: "animal_current_scope", dateCol: ""}
	sql := "SELECT 'Active animals' AS label, CAST(count(*) AS text) AS value FROM ceo_ai.animal_current_scope " +
		"WHERE tenant_id = '" + tenant + "' AND lifecycle_status = 'alive' LIMIT 100"

	err := ValidateWindow(sql, card, augustWindow())
	if !errors.Is(err, ErrWindowOnCurrentStateView) {
		t.Fatalf("expected ErrWindowOnCurrentStateView, got %v", err)
	}
	if _, isValidation := AsValidationError(err); isValidation {
		t.Fatal("ErrWindowOnCurrentStateView must not be a *ValidationError: the draft is fine, the period is not")
	}

	// No window requested: a current-state view is always fine.
	if err := ValidateWindow(sql, card, Window{}); err != nil {
		t.Fatalf("no-window read on a current-state view must pass, got %v", err)
	}
}

func TestValidateWindowEdges(t *testing.T) {
	w := augustWindow()
	sql := "SELECT 'x' AS label, '1' AS value FROM ceo_ai.mortality_base WHERE tenant_id = '" + tenant + "' AND event_date >= '2026-08-01' AND event_date < '2026-09-01' LIMIT 1"

	t.Run("nil card with a window is rejected", func(t *testing.T) {
		if err := ValidateWindow(sql, nil, w); err == nil {
			t.Fatal("expected rejection for unknown card")
		}
	})
	t.Run("nil card without a window passes", func(t *testing.T) {
		if err := ValidateWindow(sql, nil, Window{}); err != nil {
			t.Fatalf("unexpected: %v", err)
		}
	})
	t.Run("inverted window is rejected", func(t *testing.T) {
		inv := Window{From: w.To, To: w.From}
		if err := ValidateWindow(sql, fakeCard{"mortality_base", "event_date"}, inv); err == nil {
			t.Fatal("expected rejection for inverted window")
		}
	})
	t.Run("literals derived from the window", func(t *testing.T) {
		if w.FromLiteral() != "2026-08-01" || w.ToExclusiveLiteral() != "2026-09-01" {
			t.Fatalf("literals: %s / %s", w.FromLiteral(), w.ToExclusiveLiteral())
		}
	})
	t.Run("WindowFromDates round-trips", func(t *testing.T) {
		got, ok := WindowFromDates("2026-08-01", "2026-08-31", time.UTC)
		if !ok || got.FromLiteral() != "2026-08-01" || got.ToExclusiveLiteral() != "2026-09-01" {
			t.Fatalf("WindowFromDates: ok=%v got=%v", ok, got)
		}
		if _, ok := WindowFromDates("Aug 2026", "2026-08-31", time.UTC); ok {
			t.Fatal("malformed from must not parse")
		}
	})
}

func TestBannedKeywordsAndTokensExports(t *testing.T) {
	kws := BannedKeywords()
	if len(kws) != len(bannedKeywords) {
		t.Fatalf("BannedKeywords len %d != %d", len(kws), len(bannedKeywords))
	}
	for _, k := range []string{"SET", "CLOSE", "LOAD", "START", "WITH"} {
		found := false
		for _, got := range kws {
			if got == k {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %s in BannedKeywords", k)
		}
	}
	toks := Tokens("SELECT a FROM ceo_ai.v WHERE x = 'quoted SET' LIMIT 1")
	for _, tk := range toks {
		if tk.Text == "quoted" || tk.Text == "SET" {
			t.Fatalf("string literal content leaked into token stream: %+v", toks)
		}
	}
	if Tokens(`SELECT "bad" FROM ceo_ai.v`) != nil {
		t.Fatal("a statement stripStringLiterals rejects must yield nil tokens")
	}
}
