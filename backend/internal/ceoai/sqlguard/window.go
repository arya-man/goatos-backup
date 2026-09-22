package sqlguard

// window.go is the D1.2 "concrete guard, not a promise" for server-resolved
// time windows. When the orchestrator has resolved a period from the question
// ("last month", "Aug vs Sep", ...) and the referenced ceo_ai.* view carries a
// date column, the model-drafted SQL MUST bind that exact period as two
// literal predicates on the view's date column:
//
//	<date_column> >= '<from>'  AND  <date_column> < '<to_exclusive>'
//
// Anything else — a missing predicate, a different column, a different literal
// (an off-by-one month, a BETWEEN the model invented, a window on a timestamp
// column instead of the business-day column) — is rejected, so a period the
// leader asked for can never be silently answered with an all-time or wrongly
// bounded aggregate.
//
// When the view has NO date column (a current-state view such as
// animal_current_scope or shed_capacity_current) and a window was requested,
// the guard returns ErrWindowOnCurrentStateView. The orchestrator then either
// routes to a dated view or answers "as of now" explicitly (the composer prints
// `Window: as of <now>`), never a fabricated period.
//
// Matching is whole-token on the same literal-stripped token stream Validate
// uses (see Tokens), plus a raw-literal read for the two date values — a date
// literal inside a string ('2026-08-01') is exactly what the guard normally
// strips, so the literal comparison has to happen on the raw statement.

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Window is the resolved business-day period a question asked for. From is
// inclusive; To is the last INCLUSIVE business day of the period. Both are
// day-start instants in the business calendar; only their calendar date is
// used here. A zero From means "no window was requested".
type Window struct {
	From time.Time
	To   time.Time
}

// IsZero reports whether no window was requested.
func (w Window) IsZero() bool { return w.From.IsZero() }

// FromLiteral is the inclusive lower bound as the SQL literal the draft must
// carry (YYYY-MM-DD).
func (w Window) FromLiteral() string { return w.From.Format("2006-01-02") }

// ToExclusiveLiteral is the EXCLUSIVE upper bound (To + 1 day) as the SQL
// literal the draft must carry (YYYY-MM-DD). Half-open ranges are what the
// planner is told to emit, so a month never loses its last day.
func (w Window) ToExclusiveLiteral() string { return w.To.AddDate(0, 0, 1).Format("2006-01-02") }

// SchemaCardLike is the slice of a reporting schema card ValidateWindow needs.
// It is an interface (not the reporting struct) so sqlguard never imports
// reporting and the dependency stays one-way.
type SchemaCardLike interface {
	// CardName is the bare view name (without the ceo_ai. prefix).
	CardName() string
	// CardDateColumn is the view's business-day column, or "" for a
	// current-state view that has no period semantics.
	CardDateColumn() string
}

// ErrWindowOnCurrentStateView is returned when a period was requested but the
// referenced view has no date column. It is a sentinel, not a *ValidationError:
// the draft itself is well-formed; it is the QUESTION that cannot be honoured
// as a period on this view, and the orchestrator handles it by answering
// "as of now" explicitly rather than rejecting the read.
var ErrWindowOnCurrentStateView = errors.New("sqlguard: period requested on a current-state view with no date column")

// ValidateWindow enforces the window contract described in the file header.
// A zero window (no period requested) is always accepted. A nil card means
// the referenced view is unknown to the card registry; with a window
// requested that is a rejection (the guard cannot know which column bounds
// the period), without one it is accepted and left to Validate.
func ValidateWindow(sql string, card SchemaCardLike, w Window) error {
	if w.IsZero() {
		return nil
	}
	if card == nil {
		return rejit("window requested but the referenced view has no schema card")
	}
	dateCol := strings.TrimSpace(card.CardDateColumn())
	if dateCol == "" {
		return ErrWindowOnCurrentStateView
	}
	if w.To.Before(w.From) {
		return rejit("window is inverted (to precedes from)")
	}

	tokens := Tokens(sql)
	if tokens == nil {
		return rejit("window check: statement could not be tokenized")
	}
	whereIdx := -1
	for i, t := range tokens {
		if !t.IsSym && !t.IsNum && strings.EqualFold(t.Text, "WHERE") {
			whereIdx = i
			break
		}
	}
	if whereIdx < 0 {
		return rejit("window requested but the statement has no WHERE clause to bind %s", dateCol)
	}

	wantFrom := w.FromLiteral()
	wantTo := w.ToExclusiveLiteral()
	// The period may be bound on the view's business-day column or on another
	// date column the card declares (a question about loads PURCHASED in a
	// period binds purchase_date, not the entry day). Either way the exact
	// half-open window must be bound on ONE column; a draft binding no date
	// column is still rejected.
	candidates := []string{dateCol}
	if alt, ok := card.(AlternateDateColumnsCard); ok {
		candidates = append(candidates, alt.AlternateDateColumns()...)
	}
	for _, col := range candidates {
		if boundsWindow(sql, col, wantFrom, wantTo) {
			return nil
		}
	}
	return rejit("window requested (%s .. %s) but the statement must bind it as %s >= '%s' AND %s < '%s'",
		wantFrom, w.To.Format("2006-01-02"), dateCol, wantFrom, dateCol, wantTo)
}

// AlternateDateColumnsCard is the optional card capability listing the other
// date columns a period may be bound on (besides CardDateColumn).
type AlternateDateColumnsCard interface {
	AlternateDateColumns() []string
}

// boundsWindow reports whether the statement binds exactly [from, toExclusive)
// on col.
func boundsWindow(sql, col, wantFrom, wantTo string) bool {
	if strings.TrimSpace(col) == "" {
		return false
	}
	var haveFrom, haveTo bool
	for _, p := range datePredicates(sql, col) {
		switch {
		case p.op == ">=" && p.literal == wantFrom:
			haveFrom = true
		case p.op == "<" && p.literal == wantTo:
			haveTo = true
		}
	}
	return haveFrom && haveTo
}

// datePredicate is one `<col> <op> '<literal>'` comparison found in the raw
// statement after the WHERE keyword.
type datePredicate struct {
	op      string
	literal string
}

// datePredicates scans the RAW statement (literals intact) for comparisons on
// dateCol. The column may be alias-qualified (`v.event_date`); the identifier
// itself must match whole-token, case-insensitively. Only the operators the
// contract accepts are collected; a BETWEEN or a `<=` is deliberately not
// recognised so the model learns exactly one shape.
func datePredicates(sql, dateCol string) []datePredicate {
	runes := []rune(sql)
	n := len(runes)
	var out []datePredicate
	afterWhere := false
	i := 0
	for i < n {
		c := runes[i]
		if c == '\'' {
			// Skip a literal wholesale (with '' escapes).
			if _, end, ok := readLiteral(runes, i); ok {
				i = end
				continue
			}
			return out // unterminated: Validate will have rejected already
		}
		if isIdentStart(c) {
			j := i + 1
			for j < n && isIdentPart(runes[j]) {
				j++
			}
			word := string(runes[i:j])
			if !afterWhere {
				if strings.EqualFold(word, "WHERE") {
					afterWhere = true
				}
				i = j
				continue
			}
			if strings.EqualFold(word, dateCol) {
				// A qualified reference (`alias.event_date`) is fine; a LONGER
				// identifier that merely contains the column name is not a match
				// because we already consumed the whole identifier above.
				k := skipSpace(runes, j)
				op := ""
				switch {
				case k+1 < n && runes[k] == '>' && runes[k+1] == '=':
					op = ">="
					k += 2
				case k < n && runes[k] == '<' && (k+1 >= n || runes[k+1] != '='):
					op = "<"
					k++
				}
				if op != "" {
					k = skipSpace(runes, k)
					if k < n && runes[k] == '\'' {
						if val, _, ok := readLiteral(runes, k); ok {
							out = append(out, datePredicate{op: op, literal: strings.TrimSpace(val)})
						}
					}
				}
			}
			i = j
			continue
		}
		i++
	}
	return out
}

// WindowFromDates builds a Window from two YYYY-MM-DD strings (inclusive
// from/to) in loc. ok=false when either is malformed or missing.
func WindowFromDates(from, to string, loc *time.Location) (Window, bool) {
	if loc == nil {
		loc = time.UTC
	}
	f, err1 := time.ParseInLocation("2006-01-02", strings.TrimSpace(from), loc)
	t, err2 := time.ParseInLocation("2006-01-02", strings.TrimSpace(to), loc)
	if err1 != nil || err2 != nil {
		return Window{}, false
	}
	return Window{From: f, To: t}, true
}

// String renders the window for log/trace text.
func (w Window) String() string {
	if w.IsZero() {
		return "(no window)"
	}
	return fmt.Sprintf("%s..%s", w.FromLiteral(), w.To.Format("2006-01-02"))
}
