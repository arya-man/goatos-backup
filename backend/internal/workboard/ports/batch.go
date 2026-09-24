package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// ResultRows is the slice of a driver result set a source's reader needs. pgx.Rows satisfies it.
type ResultRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// Statement is one bounded read a source hands to the board's StatementBatch: its own SQL
// over its own tables, and the reader that decodes the result set exactly as the source's
// direct read does.
type Statement struct {
	Query sqlbind.BoundQuery
	Read  func(ResultRows) error
}

// BatchSource is an optional Source capability: the source's count and first-page reads as
// statements, so the board sends every batchable source's read in ONE round trip instead of
// one per source (P10). The SQL and decoding are the ones CountByState and ListRows run.
type BatchSource interface {
	CountStatement(q SourceQuery, out *map[domain.WorkState]int) (Statement, error)
	ListStatement(q SourceQuery, out *[]domain.Row) (Statement, error)
}

// StatementBatch runs statements in one round trip, in order. Any error fails the whole
// batch (a pipelined batch aborts on its first error); the caller falls back to the
// per-source reads so one bad source still degrades alone.
type StatementBatch interface {
	RunBatch(ctx context.Context, stmts []Statement) error
}

// ReadCounts decodes a (board_state text, count int) result set, the shape every count
// statement returns.
func ReadCounts(rows ResultRows) (map[domain.WorkState]int, error) {
	out := map[domain.WorkState]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, err
		}
		out[domain.WorkState(state)] = n
	}
	return out, rows.Err()
}

// ReadRows decodes a list result set row by row with the source's own scanner.
func ReadRows(rows ResultRows, limit int, scan func(ResultRows) (domain.Row, error)) ([]domain.Row, error) {
	out := make([]domain.Row, 0, limit)
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
