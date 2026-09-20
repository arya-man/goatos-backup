// Package sqlbind validates the contract between PostgreSQL positional
// placeholders and pgx arguments.
package sqlbind

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// BoundQuery keeps dynamically generated SQL and its validated arguments
// together so callers cannot accidentally prune one without the other.
type BoundQuery struct {
	sql  string
	args []any
}

// SQL returns the validated SQL text.
func (q BoundQuery) SQL() string { return q.sql }

// Args returns a copy so the validated argument collection cannot be mutated.
func (q BoundQuery) Args() []any { return append([]any(nil), q.args...) }

// Bind validates sql and args and returns an immutable copy of the argument
// slice on success.
func Bind(sql string, args ...any) (BoundQuery, error) {
	if err := ValidatePositional(sql, args); err != nil {
		return BoundQuery{}, err
	}
	return BoundQuery{sql: sql, args: append([]any(nil), args...)}, nil
}

// MustBind is Bind for construction paths where an invalid query is a
// programmer error. It panics with the validation error.
func MustBind(sql string, args ...any) BoundQuery {
	q, err := Bind(sql, args...)
	if err != nil {
		panic(err)
	}
	return q
}

// ValidatePositional requires the placeholders visible to PostgreSQL to be
// exactly $1..$N and N to match args. Callers pass data arguments only; pgx
// execution options belong at the eventual Query/Exec call, outside BoundQuery.
func ValidatePositional(sql string, args []any) error {
	ordinals, err := PlaceholderOrdinals(sql)
	if err != nil {
		return fmt.Errorf("sqlbind: scan placeholders: %w", err)
	}
	seen := make(map[int]struct{}, len(ordinals))
	max := 0
	for _, n := range ordinals {
		seen[n] = struct{}{}
		if n > max {
			max = n
		}
	}
	if max != len(args) {
		if max < len(args) {
			return fmt.Errorf("sqlbind: %d data arguments but highest placeholder is $%d (unused argument count %d)", len(args), max, len(args)-max)
		}
		return fmt.Errorf("sqlbind: highest placeholder is $%d but only %d data arguments were supplied (missing argument count at least %d)", max, len(args), max-len(args))
	}
	missing := make([]int, 0)
	for n := 1; n <= len(args); n++ {
		if _, ok := seen[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("sqlbind: placeholder sequence has gaps: missing %s", formatOrdinals(missing))
	}
	return nil
}

// ValidateNamed applies pgx's strict, bidirectional named-argument contract.
// Every @name in SQL must have a key and every supplied key must be used.
func ValidateNamed(sql string, keys []string) error {
	args := make(pgx.StrictNamedArgs, len(keys))
	for _, key := range keys {
		args[key] = nil
	}
	if _, _, err := args.RewriteQuery(context.Background(), nil, sql, nil); err != nil {
		return fmt.Errorf("sqlbind: strict named arguments: %w", err)
	}
	return nil
}

// PlaceholderOrdinals lexes PostgreSQL SQL and returns positional placeholder
// ordinals, ignoring quoted strings/identifiers, dollar-quoted bodies, and
// comments. Repeated and out-of-order placeholders are retained.
func PlaceholderOrdinals(sql string) ([]int, error) {
	var out []int
	for i := 0; i < len(sql); {
		switch sql[i] {
		case '\'':
			escape := i > 0 && (sql[i-1] == 'E' || sql[i-1] == 'e') && (i < 2 || !isTagPart(sql[i-2]))
			var err error
			i, err = skipSingleQuoted(sql, i+1, escape)
			if err != nil {
				return nil, err
			}
		case '"':
			var err error
			i, err = skipDoubleQuoted(sql, i+1)
			if err != nil {
				return nil, err
			}
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				i = skipLineComment(sql, i+2)
			} else {
				i++
			}
		case '/':
			if i+1 < len(sql) && sql[i+1] == '*' {
				var err error
				i, err = skipBlockComment(sql, i+2)
				if err != nil {
					return nil, err
				}
			} else {
				i++
			}
		case '$':
			if end, ok := dollarQuoteDelimiter(sql, i); ok {
				delim := sql[i:end]
				closeAt := strings.Index(sql[end:], delim)
				if closeAt < 0 {
					return nil, fmt.Errorf("unterminated dollar quote %q", delim)
				}
				i = end + closeAt + len(delim)
				continue
			}
			j := i + 1
			for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
				j++
			}
			if j == i+1 {
				i++
				continue
			}
			n, err := strconv.Atoi(sql[i+1 : j])
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("invalid positional placeholder %q", sql[i:j])
			}
			out = append(out, n)
			i = j
		default:
			i++
		}
	}
	return out, nil
}

func skipSingleQuoted(s string, i int, escape bool) (int, error) {
	for i < len(s) {
		if escape && s[i] == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				i += 2
				continue
			}
			return i + 1, nil
		}
		i++
	}
	return i, fmt.Errorf("unterminated single-quoted string")
}
func skipDoubleQuoted(s string, i int) (int, error) {
	for i < len(s) {
		if s[i] == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				i += 2
				continue
			}
			return i + 1, nil
		}
		i++
	}
	return i, fmt.Errorf("unterminated quoted identifier")
}
func skipLineComment(s string, i int) int {
	for i < len(s) && s[i] != '\n' {
		i++
	}
	return i
}
func skipBlockComment(s string, i int) (int, error) {
	depth := 1
	for i < len(s) {
		if i+1 < len(s) && s[i:i+2] == "/*" {
			depth++
			i += 2
			continue
		}
		if i+1 < len(s) && s[i:i+2] == "*/" {
			depth--
			i += 2
			if depth == 0 {
				return i, nil
			}
			continue
		}
		i++
	}
	return i, fmt.Errorf("unterminated block comment")
}
func dollarQuoteDelimiter(s string, i int) (int, bool) {
	j := i + 1
	if j < len(s) && !isTagStart(s[j]) && s[j] != '$' {
		return 0, false
	}
	for j < len(s) && isTagPart(s[j]) {
		j++
	}
	if j < len(s) && s[j] == '$' {
		return j + 1, true
	}
	return 0, false
}
func isTagStart(b byte) bool { return b == '_' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }
func isTagPart(b byte) bool  { return isTagStart(b) || b >= '0' && b <= '9' }
func formatOrdinals(ns []int) string {
	sort.Ints(ns)
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = "$" + strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}
