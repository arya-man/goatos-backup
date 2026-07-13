package domain

import "time"

// ProjectionKindShed is the only vaccination-projection dirty-scope kind implemented today.
// vaccination_projection_dirty_scopes.projection_kind leaves room to add execution/operations/
// process-integrity dirty-scope kinds later without a schema change.
const ProjectionKindShed = "shed"

// DirtyScopeStatus is the lifecycle of one queued vaccination_projection_dirty_scopes row.
type DirtyScopeStatus string

const (
	DirtyScopeStatusPending    DirtyScopeStatus = "pending"
	DirtyScopeStatusLeased     DirtyScopeStatus = "leased"
	DirtyScopeStatusDone       DirtyScopeStatus = "done"
	DirtyScopeStatusFailed     DirtyScopeStatus = "failed"
	DirtyScopeStatusDeadLetter DirtyScopeStatus = "dead_letter"
)

// DirtyScope is one claimed row from vaccination_projection_dirty_scopes: a durable, coalesced
// request to rebuild exactly one shed's projection row (P0-B bounded incremental projector).
type DirtyScope struct {
	DirtyScopeID   int64
	TenantID       string
	ProjectionKind string
	ShedID         string
	Reason         string
	Status         DirtyScopeStatus
	AttemptCount   int
	MaxAttempts    int
	EnqueuedAt     time.Time
}

// RebuildShedShardRequest asks the incremental projector to recompute ONLY one shed's row and
// UPSERT it into the tenant's CURRENT serving projection_version. Unlike
// ShedProjectionRecomputeRequest (RecomputeShedProjection, full-tenant rebuild), this never scans
// or touches any other shed's row.
type RebuildShedShardRequest struct {
	TenantID  string
	ShedID    string
	AsOf      time.Time
	DueBefore time.Time
}

// RebuildShedShardResult reports what a single-shed incremental rebuild committed. NextTransitionAt
// is the shed's earliest upcoming scheduled->due->overdue boundary (nil when the shed has no open
// obligations), so the worker's time-driven pass can enqueue it again exactly when it will next
// change state, without polling every shed.
type RebuildShedShardResult struct {
	TenantID          string
	ShedID            string
	ProjectionVersion int64
	ProjectedAt       time.Time
	AsOf              time.Time
	RowPresent        bool
	NextTransitionAt  *time.Time
	Deferred          bool // true when there is no serving projection_version yet; the shard write was skipped
	// VersionConflict is true when the tenant's serving_projection_version changed (or disappeared)
	// between RebuildShedShard's initial read and its final pre-write re-check -- e.g. a concurrent
	// full RecomputeShedProjection committed a new version (and will prune the old one) while this
	// rebuild was in flight. No row was written and shard_state was NOT stamped fresh; the caller
	// must re-enqueue the shed so the next attempt targets whatever is now actually serving, rather
	// than silently writing into (and falsely marking fresh) a version that is about to be orphaned.
	VersionConflict bool
}
