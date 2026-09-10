// Package ports declares what the board needs from a module: a Source that emits
// domain.Row over the module's own tables.
package ports

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/workboard/domain"
)

// SourceQuery is one bounded read against one source: one tenant, one park, one business
// date, ordered by source_id ascending, starting AFTER AfterSourceID.
type SourceQuery struct {
	TenantID     string
	ParkID       string
	BusinessDate string
	// OwnerUserID, when set, restricts to rows owned by that user (the operator lens).
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
}
