// Package ports declares what the board needs from a module: a Source that emits
// domain.Row over the module's own tables.
package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// SourceQuery is one bounded read against one source: one tenant, one park, one business
// date, ordered by source_id ascending, starting AFTER AfterSourceID.
type SourceQuery struct {
	TenantID     string
	ParkID       string
	BusinessDate string
	// OwnerUserID, when set, restricts to rows that person OWNS plus the park's UNCLAIMED pool
	// rows (nobody named yet: an untaken health session, an unassigned milk or transport task,
	// a PC care task with no assignee, an approval the person raised). The operator lens is
	// this with the caller's own id: a board that hid the pool told an operator "nothing to
	// do" while work was owed (live E2E 2026-09-11). A row owned by SOMEONE ELSE never matches.
	OwnerUserID string
	// WorkStates, when set, restricts to those states. Sources apply it in SQL.
	WorkStates []domain.WorkState
	// AfterSourceID is the keyset boundary; empty means from the start.
	AfterSourceID string
	Limit         int
}

// Source is a module's contribution to the board. Exactly ONE source type per Source;
// a module with two kinds of work registers two Sources.
//
// Every implementation lives inside its own module's package and reads only that
// module's tables plus the org tables every module may read (locations,
// workforce_members, shed_partitions). It never reads another module's schema, and the
// board never reads any table at all.
type Source interface {
	Module() domain.Module
	SourceType() string
	// ListRows returns up to Limit rows ordered by source_id ascending, each already
	// Finalize()d. Rows with source_id <= AfterSourceID are excluded.
	ListRows(ctx context.Context, q SourceQuery) ([]domain.Row, error)
	// CountByState returns the whole-filter count per work state for the same bounds,
	// ignoring AfterSourceID and Limit. One aggregate query, on the same index.
	CountByState(ctx context.Context, q SourceQuery) (map[domain.WorkState]int, error)
	SubtaskSource
}

// SubtaskQuery is one bounded read of ONE row's subtasks: the row is named by its source id
// on the same tenant, park and business date the board was read for, so a source resolves
// the row on the same index its ListRows uses and the subtasks on the row's own child table.
type SubtaskQuery struct {
	TenantID     string
	ParkID       string
	BusinessDate string
	SourceID     string
	// OwnerUserID carries the same operator/assignee lens used to resolve the parent row.
	// Aggregate sources apply it so a drawer cannot reveal children hidden from the card.
	OwnerUserID string
	// AfterKey is the keyset boundary (a domain.SubtaskKey); empty means from the start.
	AfterKey string
	Limit    int
}

// SubtaskSource is the per-row drill a Source serves for the issue view: the row's units of
// work, worst first, keyset-paged on the subtask key, with the whole count. A source whose
// row has no finer grain returns the row itself as one subtask with its steps -- never an
// empty page for a live row. A row the query does not resolve (wrong park or day, canceled)
// returns an empty page with Total 0.
type SubtaskSource interface {
	ListSubtasks(ctx context.Context, q SubtaskQuery) (domain.SubtaskPage, error)
}

// CheckUUIDSourceID is the guard a source whose ids are uuids runs before binding a keyset
// boundary: a cursor id that is not a uuid is domain.ErrInvalidCursor (400), never a
// database cast failure (500). Sources whose ids are composite strings (vaccination) compare
// in Go and do not need it.
func CheckUUIDSourceID(after string) error {
	if after == "" {
		return nil
	}
	if !uuidutil.IsUUIDString(after) {
		return domain.ErrInvalidCursor
	}
	return nil
}
