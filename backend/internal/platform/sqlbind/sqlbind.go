// Package sqlbind validates the contract between PostgreSQL positional
// placeholders and pgx arguments.
package sqlbind

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// BoundQuery keeps dynamically generated SQL and its validated arguments
// together so callers cannot accidentally prune one without the other.
type BoundQuery struct {
	SQL  string
	Args []any
}

// Bind validates sql and args and returns an immutable copy of the argument
// slice on success.
func Bind(sql string, args ...any) (BoundQuery, error) {
	if err := ValidatePositional(sql, args); err != nil {
		return BoundQuery{}, err
	}
	return BoundQuery{SQL: sql, Args: append([]any(nil), args...)}, nil
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
// exactly $1..$N and N to match the number of pgx data arguments. Leading pgx
// execution options are excluded from N just as pgx excludes them before bind.
func ValidatePositional(sql string, args []any) error {
	dataArgs, named := dataArguments(args)
	if named {
		return nil // StrictNamedArgs validates its own bidirectional contract.
	}
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
	missing := make([]int, 0)
	for n := 1; n <= max; n++ {
		if _, ok := seen[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("sqlbind: placeholder sequence has gaps: missing %s", formatOrdinals(missing))
	}
	if max != len(dataArgs) {
		if max < len(dataArgs) {
			return fmt.Errorf("sqlbind: %d data arguments but highest placeholder is $%d (unused argument positions %s)", len(dataArgs), max, formatRange(max+1, len(dataArgs)))
		}
		return fmt.Errorf("sqlbind: highest placeholder is $%d but only %d data arguments were supplied (missing argument positions %s)", max, len(dataArgs), formatRange(len(dataArgs)+1, max))
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
			i = skipSingleQuoted(sql, i+1, escape)
		case '"':
			i = skipDoubleQuoted(sql, i+1)
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

func dataArguments(args []any) ([]any, bool) {
	for len(args) > 0 {
		switch args[0].(type) {
		case pgx.QueryExecMode, pgx.QueryResultFormats, pgx.QueryResultFormatsByOID:
			args = args[1:]
		case pgx.StrictNamedArgs:
			return nil, true
		default:
			return args, false
		}
	}
	return args, false
}

func skipSingleQuoted(s string, i int, escape bool) int {
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
			return i + 1
		}
		i++
	}
	return i
}
func skipDoubleQuoted(s string, i int) int {
	for i < len(s) {
		if s[i] == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
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
func formatRange(from, to int) string {
	if from > to {
		return "none"
	}
	ns := make([]int, 0, to-from+1)
	for n := from; n <= to; n++ {
		ns = append(ns, n)
	}
	return formatOrdinals(ns)
}
