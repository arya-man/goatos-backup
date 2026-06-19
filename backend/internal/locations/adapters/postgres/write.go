package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/vgoats/goatos/backend/internal/locations/domain"
	"github.com/vgoats/goatos/backend/internal/locations/ports"
)

const (
	locationResultType         = "location"
	locationDeleteResultType   = "location_delete"
	locationAliasResultType    = "location_alias"
	locationAliasDeleteType    = "location_alias_delete"
	locationCapacityResultType = "location_capacity_record"
	locationCapacityDeleteType = "location_capacity_delete"
	locationReviewResultType   = "location_review_item"
)

func (r *Repository) CreateLocation(ctx context.Context, cmd ports.CreateLocationCommand) (*ports.LocationMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		location, err := r.getLocationTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationMutationResult{Location: location, Replayed: true, FirstResultID: &replayID}, nil
	}

	locationID, err := insertLocation(ctx, tx, cmd)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	location, err := r.getLocationTx(ctx, tx, cmd.TenantID, locationID)
	if err != nil {
		return nil, err
	}
	after, _ := json.Marshal(location)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.location.created",
		ResourceType: "location", ResourceID: locationID, AfterState: after, TraceID: cmd.TraceID,
		Metadata: metadata(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, locationID, "location_created", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationResultType, locationID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationMutationResult{Location: location}, nil
}

func (r *Repository) UpdateLocation(ctx context.Context, cmd ports.UpdateLocationCommand) (*ports.LocationMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		location, err := r.getLocationTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationMutationResult{Location: location, Replayed: true, FirstResultID: &replayID}, nil
	}

	before, err := r.getLocationTx(ctx, tx, cmd.TenantID, cmd.LocationID)
	if err != nil {
		return nil, err
	}
	if _, err := updateLocationRow(ctx, tx, cmd); err != nil {
		return nil, mapWriteErr(err)
	}
	if cmd.Operational != nil {
		if err := upsertOperational(ctx, tx, cmd.TenantID, cmd.LocationID, *cmd.Operational); err != nil {
			return nil, mapWriteErr(err)
		}
	}
	location, err := r.getLocationTx(ctx, tx, cmd.TenantID, cmd.LocationID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(location)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.location.updated",
		ResourceType: "location", ResourceID: cmd.LocationID, BeforeState: beforeJSON, AfterState: afterJSON, TraceID: cmd.TraceID,
		Metadata: metadata(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_updated", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationResultType, cmd.LocationID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationMutationResult{Location: location}, nil
}

func (r *Repository) RetireLocation(ctx context.Context, cmd ports.RetireLocationCommand) (*ports.LocationMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		location, err := r.getLocationTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationMutationResult{Location: location, Replayed: true, FirstResultID: &replayID}, nil
	}

	before, err := r.getLocationTx(ctx, tx, cmd.TenantID, cmd.LocationID)
	if err != nil {
		return nil, err
	}
	usage, err := usageTx(ctx, tx, cmd.TenantID, cmd.LocationID)
	if err != nil {
		return nil, err
	}
	if usage.HasBlockingUsage {
		return nil, ports.ErrBlockingUsage
	}
	if tag, err := tx.Exec(ctx, `
UPDATE locations
SET status = 'inactive',
    retired_at = now(),
    retired_by = $3::uuid,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND location_id = $2::uuid
  AND row_version = $4`, cmd.TenantID, cmd.LocationID, cmd.ActorID, cmd.RowVersion); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	location, err := r.getLocationTx(ctx, tx, cmd.TenantID, cmd.LocationID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(location)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.location.retired",
		ResourceType: "location", ResourceID: cmd.LocationID, BeforeState: beforeJSON, AfterState: afterJSON, TraceID: cmd.TraceID,
		Metadata: metadataWithReason(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope, cmd.Reason),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_retired", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationResultType, cmd.LocationID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationMutationResult{Location: location}, nil
}

func (r *Repository) DeleteLocation(ctx context.Context, cmd ports.DeleteLocationCommand) (*ports.LocationDeleteMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationDeleteResultType)
	if err != nil {
		return nil, err
	}
	locationIDPtr := &cmd.LocationID
	if replayed {
		return &ports.LocationDeleteMutationResult{
			ResourceType: "location", ResourceID: replayID, LocationID: locationIDPtr, Replayed: true, FirstResultID: &replayID,
		}, nil
	}

	before, err := r.getLocationTx(ctx, tx, cmd.TenantID, cmd.LocationID)
	if err != nil {
		return nil, err
	}
	if before.RowVersion != cmd.RowVersion {
		return nil, ports.ErrWriteConflict
	}
	if before.Status != "staging" && before.Status != "review" {
		return nil, ports.ErrBlockingUsage
	}
	blocked, err := hardDeleteLocationBlockedTx(ctx, tx, cmd.TenantID, cmd.LocationID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, ports.ErrBlockingUsage
	}

	beforeJSON, _ := json.Marshal(before)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.location.deleted",
		ResourceType: "location", ResourceID: cmd.LocationID, BeforeState: beforeJSON, TraceID: cmd.TraceID,
		Metadata: metadataWithReason(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope, cmd.Reason),
	}); err != nil {
		return nil, err
	}
	if err := detachLocationInvalidationsForDelete(ctx, tx, cmd.TenantID, cmd.LocationID); err != nil {
		return nil, err
	}
	if tag, err := tx.Exec(ctx, `
DELETE FROM locations
WHERE tenant_id = $1::uuid
  AND location_id = $2::uuid
  AND row_version = $3
  AND status IN ('staging', 'review')`, cmd.TenantID, cmd.LocationID, cmd.RowVersion); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	if err := insertLocationDeleteInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationDeleteResultType, cmd.LocationID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationDeleteMutationResult{ResourceType: "location", ResourceID: cmd.LocationID, LocationID: locationIDPtr}, nil
}

func (r *Repository) CreateLocationAlias(ctx context.Context, cmd ports.CreateLocationAliasCommand) (*ports.LocationAliasMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationAliasResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		alias, err := getAliasTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationAliasMutationResult{Alias: alias, Replayed: true, FirstResultID: &replayID}, nil
	}
	if _, err := r.getLocationTx(ctx, tx, cmd.TenantID, cmd.LocationID); err != nil {
		return nil, err
	}
	aliasID, err := insertAlias(ctx, tx, cmd.TenantID, cmd.LocationID, cmd.AliasCode, cmd.SourceContext, cmd.Notes)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	alias, err := getAliasTx(ctx, tx, cmd.TenantID, aliasID)
	if err != nil {
		return nil, err
	}
	after, _ := json.Marshal(alias)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.alias.created",
		ResourceType: "location_alias", ResourceID: aliasID, ScopeType: "location", ScopeID: cmd.LocationID,
		AfterState: after, TraceID: cmd.TraceID,
		Metadata: metadata(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_alias_created", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationAliasResultType, aliasID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationAliasMutationResult{Alias: alias}, nil
}

func (r *Repository) UpdateLocationAlias(ctx context.Context, cmd ports.UpdateLocationAliasCommand) (*ports.LocationAliasMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationAliasResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		alias, err := getAliasTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationAliasMutationResult{Alias: alias, Replayed: true, FirstResultID: &replayID}, nil
	}
	before, err := getAliasTx(ctx, tx, cmd.TenantID, cmd.AliasID)
	if err != nil {
		return nil, err
	}
	if before.CanonicalLocationID != cmd.LocationID {
		return nil, ports.ErrNotFound
	}
	if tag, err := tx.Exec(ctx, `
UPDATE location_aliases
SET alias_code = $4,
    source_context = $5,
    notes = $6,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND canonical_location_id = $2::uuid
  AND alias_id = $3::uuid
  AND row_version = $7`, cmd.TenantID, cmd.LocationID, cmd.AliasID, cmd.AliasCode, cmd.SourceContext, nullableString(cmd.Notes), cmd.RowVersion); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	alias, err := getAliasTx(ctx, tx, cmd.TenantID, cmd.AliasID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(alias)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.alias.updated",
		ResourceType: "location_alias", ResourceID: cmd.AliasID, ScopeType: "location", ScopeID: cmd.LocationID,
		BeforeState: beforeJSON, AfterState: afterJSON, TraceID: cmd.TraceID,
		Metadata: metadata(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_alias_updated", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationAliasResultType, cmd.AliasID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationAliasMutationResult{Alias: alias}, nil
}

func (r *Repository) RetireLocationAlias(ctx context.Context, cmd ports.RetireLocationAliasCommand) (*ports.LocationAliasMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationAliasResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		alias, err := getAliasTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationAliasMutationResult{Alias: alias, Replayed: true, FirstResultID: &replayID}, nil
	}
	before, err := getAliasTx(ctx, tx, cmd.TenantID, cmd.AliasID)
	if err != nil {
		return nil, err
	}
	if before.CanonicalLocationID != cmd.LocationID {
		return nil, ports.ErrNotFound
	}
	if tag, err := tx.Exec(ctx, `
UPDATE location_aliases
SET status = 'retired',
    retired_at = now(),
    updated_at = now(),
    row_version = row_version + 1,
    notes = COALESCE(notes || E'\n', '') || $5
WHERE tenant_id = $1::uuid
  AND canonical_location_id = $2::uuid
  AND alias_id = $3::uuid
  AND row_version = $4
  AND status = 'active'`, cmd.TenantID, cmd.LocationID, cmd.AliasID, cmd.RowVersion, "Retired: "+cmd.Reason); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	alias, err := getAliasTx(ctx, tx, cmd.TenantID, cmd.AliasID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(alias)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.alias.retired",
		ResourceType: "location_alias", ResourceID: cmd.AliasID, ScopeType: "location", ScopeID: cmd.LocationID,
		BeforeState: beforeJSON, AfterState: afterJSON, TraceID: cmd.TraceID,
		Metadata: metadataWithReason(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope, cmd.Reason),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_alias_retired", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationAliasResultType, cmd.AliasID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationAliasMutationResult{Alias: alias}, nil
}

func (r *Repository) DeleteLocationAlias(ctx context.Context, cmd ports.DeleteLocationAliasCommand) (*ports.LocationDeleteMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationAliasDeleteType)
	if err != nil {
		return nil, err
	}
	locationIDPtr := &cmd.LocationID
	if replayed {
		return &ports.LocationDeleteMutationResult{
			ResourceType: "location_alias", ResourceID: replayID, LocationID: locationIDPtr, Replayed: true, FirstResultID: &replayID,
		}, nil
	}
	before, err := getAliasTx(ctx, tx, cmd.TenantID, cmd.AliasID)
	if err != nil {
		return nil, err
	}
	if before.CanonicalLocationID != cmd.LocationID {
		return nil, ports.ErrNotFound
	}
	if before.RowVersion != cmd.RowVersion {
		return nil, ports.ErrWriteConflict
	}
	if before.Status == "active" {
		return nil, ports.ErrBlockingUsage
	}
	beforeJSON, _ := json.Marshal(before)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.alias.deleted",
		ResourceType: "location_alias", ResourceID: cmd.AliasID, ScopeType: "location", ScopeID: cmd.LocationID,
		BeforeState: beforeJSON, TraceID: cmd.TraceID,
		Metadata: metadataWithReason(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope, cmd.Reason),
	}); err != nil {
		return nil, err
	}
	if tag, err := tx.Exec(ctx, `
DELETE FROM location_aliases
WHERE tenant_id = $1::uuid
  AND canonical_location_id = $2::uuid
  AND alias_id = $3::uuid
  AND row_version = $4
  AND status <> 'active'`, cmd.TenantID, cmd.LocationID, cmd.AliasID, cmd.RowVersion); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_alias_deleted", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationAliasDeleteType, cmd.AliasID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationDeleteMutationResult{ResourceType: "location_alias", ResourceID: cmd.AliasID, LocationID: locationIDPtr}, nil
}

func (r *Repository) CreateLocationCapacity(ctx context.Context, cmd ports.CreateLocationCapacityCommand) (*ports.LocationCapacityMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationCapacityResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		capacity, err := getCapacityTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationCapacityMutationResult{Capacity: capacity, Replayed: true, FirstResultID: &replayID}, nil
	}
	if _, err := r.getLocationTx(ctx, tx, cmd.TenantID, cmd.LocationID); err != nil {
		return nil, err
	}
	capacityID, err := insertCapacity(ctx, tx, cmd)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	capacity, err := getCapacityTx(ctx, tx, cmd.TenantID, capacityID)
	if err != nil {
		return nil, err
	}
	after, _ := json.Marshal(capacity)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.capacity.created",
		ResourceType: "location_capacity_record", ResourceID: capacityID, ScopeType: "location", ScopeID: cmd.LocationID,
		AfterState: after, TraceID: cmd.TraceID,
		Metadata: metadata(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_capacity_created", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationCapacityResultType, capacityID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationCapacityMutationResult{Capacity: capacity}, nil
}

func (r *Repository) UpdateLocationCapacity(ctx context.Context, cmd ports.UpdateLocationCapacityCommand) (*ports.LocationCapacityMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationCapacityResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		capacity, err := getCapacityTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationCapacityMutationResult{Capacity: capacity, Replayed: true, FirstResultID: &replayID}, nil
	}
	before, err := getCapacityTx(ctx, tx, cmd.TenantID, cmd.CapacityRecordID)
	if err != nil {
		return nil, err
	}
	if before.LocationID != cmd.LocationID {
		return nil, ports.ErrNotFound
	}
	if tag, err := tx.Exec(ctx, `
UPDATE location_capacity_records
SET capacity_kind = $4,
    capacity_value = $5,
    effective_from = $6::date,
    effective_to = $7::date,
    source = $8,
    source_ref = $9,
    notes = $10,
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND location_id = $2::uuid
  AND capacity_record_id = $3::uuid
  AND row_version = $11`, cmd.TenantID, cmd.LocationID, cmd.CapacityRecordID, cmd.CapacityKind, cmd.CapacityValue,
		cmd.EffectiveFrom, nullableString(cmd.EffectiveTo), cmd.Source, nullableString(cmd.SourceRef), nullableString(cmd.Notes), cmd.RowVersion); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	capacity, err := getCapacityTx(ctx, tx, cmd.TenantID, cmd.CapacityRecordID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(capacity)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.capacity.updated",
		ResourceType: "location_capacity_record", ResourceID: cmd.CapacityRecordID, ScopeType: "location", ScopeID: cmd.LocationID,
		BeforeState: beforeJSON, AfterState: afterJSON, TraceID: cmd.TraceID,
		Metadata: metadata(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope),
	}); err != nil {
		return nil, err
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_capacity_updated", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationCapacityResultType, cmd.CapacityRecordID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationCapacityMutationResult{Capacity: capacity}, nil
}

func (r *Repository) DeleteLocationCapacity(ctx context.Context, cmd ports.DeleteLocationCapacityCommand) (*ports.LocationDeleteMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationCapacityDeleteType)
	if err != nil {
		return nil, err
	}
	locationIDPtr := &cmd.LocationID
	if replayed {
		return &ports.LocationDeleteMutationResult{
			ResourceType: "location_capacity_record", ResourceID: replayID, LocationID: locationIDPtr, Replayed: true, FirstResultID: &replayID,
		}, nil
	}
	before, err := getCapacityTx(ctx, tx, cmd.TenantID, cmd.CapacityRecordID)
	if err != nil {
		return nil, err
	}
	if before.LocationID != cmd.LocationID {
		return nil, ports.ErrNotFound
	}
	if before.RowVersion != cmd.RowVersion {
		return nil, ports.ErrWriteConflict
	}
	beforeJSON, _ := json.Marshal(before)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.capacity.deleted",
		ResourceType: "location_capacity_record", ResourceID: cmd.CapacityRecordID, ScopeType: "location", ScopeID: cmd.LocationID,
		BeforeState: beforeJSON, TraceID: cmd.TraceID,
		Metadata: metadataWithReason(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope, cmd.Reason),
	}); err != nil {
		return nil, err
	}
	if tag, err := tx.Exec(ctx, `
DELETE FROM location_capacity_records
WHERE tenant_id = $1::uuid
  AND location_id = $2::uuid
  AND capacity_record_id = $3::uuid
  AND row_version = $4`, cmd.TenantID, cmd.LocationID, cmd.CapacityRecordID, cmd.RowVersion); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	if err := insertInvalidations(ctx, tx, cmd.TenantID, cmd.LocationID, "location_capacity_deleted", cmd.TraceID); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationCapacityDeleteType, cmd.CapacityRecordID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationDeleteMutationResult{ResourceType: "location_capacity_record", ResourceID: cmd.CapacityRecordID, LocationID: locationIDPtr}, nil
}

func (r *Repository) CreateLocationReviewItem(ctx context.Context, cmd ports.CreateLocationReviewItemCommand) (*ports.LocationReviewMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationReviewResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		item, err := getReviewItemTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationReviewMutationResult{ReviewItem: item, Replayed: true, FirstResultID: &replayID}, nil
	}
	reviewID, err := insertReviewItem(ctx, tx, cmd)
	if err != nil {
		return nil, mapWriteErr(err)
	}
	item, err := getReviewItemTx(ctx, tx, cmd.TenantID, reviewID)
	if err != nil {
		return nil, err
	}
	after, _ := json.Marshal(item)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.review_item.created",
		ResourceType: "location_review_item", ResourceID: reviewID, AfterState: after, TraceID: cmd.TraceID,
		Metadata: metadata(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope),
	}); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationReviewResultType, reviewID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationReviewMutationResult{ReviewItem: item}, nil
}

func (r *Repository) OpenLocationReviewItem(ctx context.Context, cmd ports.OpenLocationReviewItemCommand) (*ports.OpenLocationReviewItemResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	if item, ok, err := getOpenReviewItemBySourceTx(ctx, tx, cmd); err != nil {
		return nil, err
	} else if ok {
		return &ports.OpenLocationReviewItemResult{ReviewItem: item, Reused: true}, nil
	}

	reviewID, inserted, err := insertReviewItemIfNotExists(ctx, tx, ports.CreateLocationReviewItemCommand{
		TenantID:              cmd.TenantID,
		ActorID:               cmd.ActorID,
		TraceID:               cmd.TraceID,
		ReviewType:            cmd.ReviewType,
		SourceContext:         nonEmptyStringPtr(cmd.SourceContext),
		SourceLabel:           nonEmptyStringPtr(cmd.SourceLabel),
		NormalizedSourceLabel: nonEmptyStringPtr(cmd.NormalizedSourceLabel),
		CanonicalLocationID:   cmd.CanonicalLocationID,
		CandidateLocationIDs:  cmd.CandidateLocationIDs,
		EvidenceJSON:          cmd.EvidenceJSON,
		EvidenceHash:          cmd.EvidenceHash,
	})
	if err != nil {
		return nil, mapWriteErr(err)
	}
	if !inserted {
		item, ok, err := getOpenReviewItemBySourceTx(ctx, tx, cmd)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ports.ErrWriteConflict
		}
		return &ports.OpenLocationReviewItemResult{ReviewItem: item, Reused: true}, nil
	}

	item, err := getReviewItemTx(ctx, tx, cmd.TenantID, reviewID)
	if err != nil {
		return nil, err
	}
	after, _ := json.Marshal(item)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.review_item.created",
		ResourceType: "location_review_item", ResourceID: reviewID, AfterState: after, TraceID: cmd.TraceID,
		Metadata: map[string]any{
			"source_context":          cmd.SourceContext,
			"source_label":            cmd.SourceLabel,
			"normalized_source_label": cmd.NormalizedSourceLabel,
			"evidence_hash":           cmd.EvidenceHash,
			"created_by_resolver":     true,
		},
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.OpenLocationReviewItemResult{ReviewItem: item}, nil
}

func (r *Repository) ResolveLocationReviewItem(ctx context.Context, cmd ports.ResolveLocationReviewItemCommand) (*ports.LocationReviewMutationResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer rollbackUnlessCommitted(ctx, tx, &committed)

	replayID, replayed, err := beginIdempotency(ctx, tx, cmd.StoredIdempotencyKey, cmd.TenantID, cmd.IdempotencyScope, cmd.RequestHash, locationReviewResultType)
	if err != nil {
		return nil, err
	}
	if replayed {
		item, err := getReviewItemTx(ctx, tx, cmd.TenantID, replayID)
		if err != nil {
			return nil, err
		}
		return &ports.LocationReviewMutationResult{ReviewItem: item, Replayed: true, FirstResultID: &replayID}, nil
	}
	before, err := getReviewItemTx(ctx, tx, cmd.TenantID, cmd.ReviewID)
	if err != nil {
		return nil, err
	}
	if tag, err := tx.Exec(ctx, `
UPDATE location_review_items
SET status = $3,
    canonical_location_id = COALESCE($4::uuid, canonical_location_id),
    resolved_by = $5::uuid,
    resolution_notes = $6,
    resolved_at = now(),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND review_id = $2::uuid
  AND status = 'open'
  AND row_version = $7`, cmd.TenantID, cmd.ReviewID, cmd.Status, nullableString(cmd.CanonicalLocationID), cmd.ActorID, cmd.ResolutionNotes, cmd.RowVersion); err != nil {
		return nil, mapWriteErr(err)
	} else if tag.RowsAffected() != 1 {
		return nil, ports.ErrWriteConflict
	}
	if cmd.Status == "resolved" && cmd.CanonicalLocationID != nil && before.SourceContext != nil && before.SourceLabel != nil {
		_, err := insertAlias(ctx, tx, cmd.TenantID, *cmd.CanonicalLocationID, *before.SourceLabel, *before.SourceContext, stringPtr("Resolved from location review item "+cmd.ReviewID+"."))
		if err != nil {
			return nil, mapWriteErr(err)
		}
		if err := insertInvalidations(ctx, tx, cmd.TenantID, *cmd.CanonicalLocationID, "location_review_resolved", cmd.TraceID); err != nil {
			return nil, err
		}
	}
	item, err := getReviewItemTx(ctx, tx, cmd.TenantID, cmd.ReviewID)
	if err != nil {
		return nil, err
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(item)
	if err := insertLocationAudit(ctx, tx, auditEntry{
		TenantID: cmd.TenantID, ActorID: cmd.ActorID, Action: "locations.review_item.resolved",
		ResourceType: "location_review_item", ResourceID: cmd.ReviewID, BeforeState: beforeJSON, AfterState: afterJSON, TraceID: cmd.TraceID,
		Metadata: metadataWithReason(cmd.ClientIdempotencyKey, cmd.StoredIdempotencyKey, cmd.IdempotencyScope, cmd.ResolutionNotes),
	}); err != nil {
		return nil, err
	}
	if err := completeIdempotency(ctx, tx, cmd.StoredIdempotencyKey, locationReviewResultType, cmd.ReviewID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &ports.LocationReviewMutationResult{ReviewItem: item}, nil
}

func insertLocation(ctx context.Context, tx pgx.Tx, cmd ports.CreateLocationCommand) (string, error) {
	var locationID string
	err := tx.QueryRow(ctx, `
INSERT INTO locations (
  tenant_id, location_type, location_code, name, parent_location_id, country,
  state_region, district, pincode, lat, lng, timezone, status, display_order,
  operational_notes
) VALUES (
  $1::uuid, $2, $3, $4, $5::uuid, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
RETURNING location_id::text`,
		cmd.TenantID, cmd.LocationType, nullableString(cmd.LocationCode), cmd.Name, nullableString(cmd.ParentLocationID),
		cmd.Country, nullableString(cmd.StateRegion), nullableString(cmd.District), nullableString(cmd.Pincode),
		nullableFloat(cmd.Lat), nullableFloat(cmd.Lng), cmd.Timezone, cmd.Status, cmd.Operational.DisplayOrder,
		nullableString(cmd.Operational.Notes),
	).Scan(&locationID)
	if err != nil {
		return "", err
	}
	if err := upsertOperational(ctx, tx, cmd.TenantID, locationID, cmd.Operational); err != nil {
		return "", err
	}
	return locationID, nil
}

func updateLocationRow(ctx context.Context, tx pgx.Tx, cmd ports.UpdateLocationCommand) (string, error) {
	var locationID string
	err := tx.QueryRow(ctx, `
UPDATE locations
SET location_type = COALESCE($3, location_type),
    location_code = COALESCE($4, location_code),
    name = COALESCE($5, name),
    parent_location_id = CASE WHEN $6 THEN NULL ELSE COALESCE($7::uuid, parent_location_id) END,
    status = COALESCE($8, status),
    country = COALESCE($9, country),
    state_region = COALESCE($10, state_region),
    district = COALESCE($11, district),
    pincode = COALESCE($12, pincode),
    lat = COALESCE($13, lat),
    lng = COALESCE($14, lng),
    timezone = COALESCE($15, timezone),
    display_order = COALESCE($16, display_order),
    operational_notes = COALESCE($17, operational_notes),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND location_id = $2::uuid
  AND row_version = $18
RETURNING location_id::text`,
		cmd.TenantID, cmd.LocationID, nullableString(cmd.LocationType), nullableString(cmd.LocationCode), nullableString(cmd.Name),
		cmd.ClearParent, nullableString(cmd.ParentLocationID), nullableString(cmd.Status), nullableString(cmd.Country),
		nullableString(cmd.StateRegion), nullableString(cmd.District), nullableString(cmd.Pincode), nullableFloat(cmd.Lat),
		nullableFloat(cmd.Lng), nullableString(cmd.Timezone), nullableOperationalDisplayOrder(cmd.Operational),
		nullableOperationalNotes(cmd.Operational), cmd.RowVersion,
	).Scan(&locationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ports.ErrWriteConflict
	}
	return locationID, err
}

func upsertOperational(ctx context.Context, tx pgx.Tx, tenantID, locationID string, op domain.OperationalAttributes) error {
	_, err := tx.Exec(ctx, `
INSERT INTO location_operational_attributes (
  tenant_id, location_id, usable_for_counts, usable_for_feed, usable_for_vaccination,
  usable_for_sop, is_holding, is_quarantine, is_icu, display_order, notes, updated_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, now()
)
ON CONFLICT (location_id) DO UPDATE
SET usable_for_counts = EXCLUDED.usable_for_counts,
    usable_for_feed = EXCLUDED.usable_for_feed,
    usable_for_vaccination = EXCLUDED.usable_for_vaccination,
    usable_for_sop = EXCLUDED.usable_for_sop,
    is_holding = EXCLUDED.is_holding,
    is_quarantine = EXCLUDED.is_quarantine,
    is_icu = EXCLUDED.is_icu,
    display_order = EXCLUDED.display_order,
    notes = EXCLUDED.notes,
    updated_at = now()`,
		tenantID, locationID, op.UsableForCounts, op.UsableForFeed, op.UsableForVaccination, op.UsableForSOP,
		op.IsHolding, op.IsQuarantine, op.IsICU, op.DisplayOrder, nullableString(op.Notes))
	return err
}

func insertAlias(ctx context.Context, tx pgx.Tx, tenantID, locationID, aliasCode, sourceContext string, notes *string) (string, error) {
	var aliasID string
	err := tx.QueryRow(ctx, `
INSERT INTO location_aliases (
  tenant_id, canonical_location_id, alias_code, source_context, notes, status, updated_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, 'active', now()
)
RETURNING alias_id::text`, tenantID, locationID, aliasCode, sourceContext, nullableString(notes)).Scan(&aliasID)
	return aliasID, err
}

func insertCapacity(ctx context.Context, tx pgx.Tx, cmd ports.CreateLocationCapacityCommand) (string, error) {
	var capacityID string
	err := tx.QueryRow(ctx, `
INSERT INTO location_capacity_records (
  tenant_id, location_id, capacity_kind, capacity_value, effective_from,
  effective_to, source, source_ref, notes, created_by
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5::date, $6::date, $7, $8, $9, $10::uuid
)
RETURNING capacity_record_id::text`,
		cmd.TenantID, cmd.LocationID, cmd.CapacityKind, cmd.CapacityValue, cmd.EffectiveFrom,
		nullableString(cmd.EffectiveTo), cmd.Source, nullableString(cmd.SourceRef), nullableString(cmd.Notes), cmd.ActorID,
	).Scan(&capacityID)
	return capacityID, err
}

func insertReviewItem(ctx context.Context, tx pgx.Tx, cmd ports.CreateLocationReviewItemCommand) (string, error) {
	candidateIDs := cmd.CandidateLocationIDs
	if candidateIDs == nil {
		candidateIDs = []string{}
	}
	candidateBytes, err := json.Marshal(candidateIDs)
	if err != nil {
		return "", err
	}
	var reviewID string
	err = tx.QueryRow(ctx, `
INSERT INTO location_review_items (
  tenant_id, review_type, source_context, source_label, normalized_source_label,
  canonical_location_id, candidate_location_ids, evidence_json, evidence_hash, created_by
) VALUES (
  $1::uuid, $2, $3, $4, $5, $6::uuid, $7::jsonb, $8::jsonb, $9, $10::uuid
)
RETURNING review_id::text`,
		cmd.TenantID, cmd.ReviewType, nullableString(cmd.SourceContext), nullableString(cmd.SourceLabel),
		nullableString(cmd.NormalizedSourceLabel), nullableString(cmd.CanonicalLocationID), candidateBytes,
		cmd.EvidenceJSON, cmd.EvidenceHash, cmd.ActorID,
	).Scan(&reviewID)
	return reviewID, err
}

func insertReviewItemIfNotExists(ctx context.Context, tx pgx.Tx, cmd ports.CreateLocationReviewItemCommand) (string, bool, error) {
	candidateIDs := cmd.CandidateLocationIDs
	if candidateIDs == nil {
		candidateIDs = []string{}
	}
	candidateBytes, err := json.Marshal(candidateIDs)
	if err != nil {
		return "", false, err
	}
	var reviewID string
	err = tx.QueryRow(ctx, `
INSERT INTO location_review_items (
  tenant_id, review_type, source_context, source_label, normalized_source_label,
  canonical_location_id, candidate_location_ids, evidence_json, evidence_hash, created_by
) VALUES (
  $1::uuid, $2, $3, $4, $5, $6::uuid, $7::jsonb, $8::jsonb, $9, $10::uuid
)
ON CONFLICT DO NOTHING
RETURNING review_id::text`,
		cmd.TenantID, cmd.ReviewType, nullableString(cmd.SourceContext), nullableString(cmd.SourceLabel),
		nullableString(cmd.NormalizedSourceLabel), nullableString(cmd.CanonicalLocationID), candidateBytes,
		cmd.EvidenceJSON, cmd.EvidenceHash, nullableString(nonEmptyStringPtr(cmd.ActorID)),
	).Scan(&reviewID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return reviewID, true, err
}

func getOpenReviewItemBySourceTx(ctx context.Context, tx pgx.Tx, cmd ports.OpenLocationReviewItemCommand) (domain.LocationReviewItem, bool, error) {
	row := tx.QueryRow(ctx, `
SELECT review_id::text, review_type, status, source_context, source_label, normalized_source_label,
       canonical_location_id::text, candidate_location_ids, evidence_hash, resolution_notes,
       row_version, created_at, updated_at, resolved_at
FROM location_review_items
WHERE tenant_id = $1::uuid
  AND review_type = $2
  AND COALESCE(source_context, '') = $3
  AND COALESCE(normalized_source_label, '') = $4
  AND evidence_hash = $5
  AND status = 'open'
ORDER BY created_at ASC, review_id ASC
LIMIT 1`, cmd.TenantID, cmd.ReviewType, cmd.SourceContext, cmd.NormalizedSourceLabel, cmd.EvidenceHash)
	item, err := scanReviewItemRow(row)
	if errors.Is(err, ports.ErrNotFound) {
		return domain.LocationReviewItem{}, false, nil
	}
	if err != nil {
		return domain.LocationReviewItem{}, false, err
	}
	return item, true, nil
}

func (r *Repository) getLocationTx(ctx context.Context, tx pgx.Tx, tenantID, locationID string) (domain.LocationSummary, error) {
	rows, err := tx.Query(ctx, locationSelectSQL(`
WHERE l.tenant_id = $1::uuid
  AND l.location_id = $2::uuid
LIMIT 1`), tenantID, locationID)
	if err != nil {
		return domain.LocationSummary{}, err
	}
	items, err := scanLocations(rows)
	if err != nil {
		return domain.LocationSummary{}, err
	}
	if len(items) == 0 {
		return domain.LocationSummary{}, ports.ErrNotFound
	}
	return items[0], nil
}

func getAliasTx(ctx context.Context, tx pgx.Tx, tenantID, aliasID string) (domain.LocationAlias, error) {
	row := tx.QueryRow(ctx, `
SELECT alias_id::text, alias_code, canonical_location_id::text, source_context, status, notes, row_version, created_at, updated_at
FROM location_aliases
WHERE tenant_id = $1::uuid
  AND alias_id = $2::uuid`, tenantID, aliasID)
	return scanAliasRow(row)
}

func getCapacityTx(ctx context.Context, tx pgx.Tx, tenantID, capacityID string) (domain.LocationCapacityRecord, error) {
	row := tx.QueryRow(ctx, `
SELECT capacity_record_id::text, location_id::text, capacity_kind, capacity_value, effective_from::text,
       effective_to::text, source, source_ref, notes, row_version, created_at, updated_at
FROM location_capacity_records
WHERE tenant_id = $1::uuid
  AND capacity_record_id = $2::uuid`, tenantID, capacityID)
	return scanCapacityRow(row)
}

func getReviewItemTx(ctx context.Context, tx pgx.Tx, tenantID, reviewID string) (domain.LocationReviewItem, error) {
	row := tx.QueryRow(ctx, `
SELECT review_id::text, review_type, status, source_context, source_label, normalized_source_label,
       canonical_location_id::text, candidate_location_ids, evidence_hash, resolution_notes,
       row_version, created_at, updated_at, resolved_at
FROM location_review_items
WHERE tenant_id = $1::uuid
  AND review_id = $2::uuid`, tenantID, reviewID)
	return scanReviewItemRow(row)
}

func usageTx(ctx context.Context, tx pgx.Tx, tenantID, locationID string) (domain.LocationUsageResponse, error) {
	var usage domain.LocationUsageResponse
	usage.LocationID = locationID
	err := tx.QueryRow(ctx, `
SELECT
  (SELECT count(*) FROM goats g WHERE g.tenant_id = $1::uuid AND (g.current_location_id = $2::uuid OR g.farm_id = $2::uuid OR g.park_id = $2::uuid OR g.shed_id = $2::uuid OR g.cohort_id = $2::uuid) AND g.identity_state <> 'merged'),
  (SELECT count(*) FROM goat_location_history glh WHERE glh.tenant_id = $1::uuid AND (glh.from_location_id = $2::uuid OR glh.to_location_id = $2::uuid)),
  (SELECT count(*) FROM locations child WHERE child.tenant_id = $1::uuid AND child.parent_location_id = $2::uuid AND child.status = 'active'),
  (SELECT count(*) FROM location_aliases la WHERE la.tenant_id = $1::uuid AND la.canonical_location_id = $2::uuid AND la.status = 'active'),
  (SELECT count(*) FROM user_scope_grants usg WHERE usg.tenant_id = $1::uuid AND usg.scope_id = $2::uuid AND (usg.valid_to IS NULL OR usg.valid_to > now())),
  0::bigint,
  (SELECT count(*) FROM counts_current_snapshot_rows csr WHERE csr.tenant_id = $1::uuid AND (csr.farm_id = $2::uuid OR csr.park_id = $2::uuid OR csr.shed_id = $2::uuid OR csr.resolved_location_id = $2::uuid))
    + (SELECT count(*) FROM mortality_events me WHERE me.tenant_id = $1::uuid AND (me.canonical_farm_location_id = $2::uuid OR me.canonical_park_location_id = $2::uuid OR me.canonical_shed_location_id = $2::uuid OR me.canonical_housing_location_id = $2::uuid)),
  (SELECT count(*) FROM counts_projection_rows cpr WHERE cpr.tenant_id = $1::uuid AND cpr.dimension_key = $2::text)
    + (SELECT count(*) FROM mortality_projection_rows mpr WHERE mpr.tenant_id = $1::uuid AND mpr.dimension_key = $2::text)`,
		tenantID, locationID).Scan(
		&usage.GoatsCurrentlyAssigned, &usage.GoatLocationHistoryRows, &usage.ChildLocations, &usage.ActiveAliases,
		&usage.ActiveRBACGrants, &usage.ActiveSOPDependencies, &usage.ImportOrSourceRows, &usage.DashboardProjectionRows,
	)
	if err != nil {
		return domain.LocationUsageResponse{}, err
	}
	usage.HasBlockingUsage = usage.GoatsCurrentlyAssigned > 0 || usage.ChildLocations > 0 || usage.ActiveAliases > 0 || usage.ActiveRBACGrants > 0 || usage.ActiveSOPDependencies > 0
	return usage, nil
}

func hardDeleteLocationBlockedTx(ctx context.Context, tx pgx.Tx, tenantID, locationID string) (bool, error) {
	var blocked bool
	err := tx.QueryRow(ctx, `
SELECT
  EXISTS (SELECT 1 FROM goats g WHERE g.tenant_id = $1::uuid AND (g.current_location_id = $2::uuid OR g.farm_id = $2::uuid OR g.park_id = $2::uuid OR g.shed_id = $2::uuid OR g.cohort_id = $2::uuid) AND g.identity_state <> 'merged')
  OR EXISTS (SELECT 1 FROM goat_location_history glh WHERE glh.tenant_id = $1::uuid AND (glh.from_location_id = $2::uuid OR glh.to_location_id = $2::uuid))
  OR EXISTS (SELECT 1 FROM locations child WHERE child.tenant_id = $1::uuid AND child.parent_location_id = $2::uuid)
  OR EXISTS (SELECT 1 FROM location_aliases la WHERE la.tenant_id = $1::uuid AND la.canonical_location_id = $2::uuid)
  OR EXISTS (SELECT 1 FROM location_capacity_records lcr WHERE lcr.tenant_id = $1::uuid AND lcr.location_id = $2::uuid)
  OR EXISTS (SELECT 1 FROM location_review_items lri WHERE lri.tenant_id = $1::uuid AND lri.canonical_location_id = $2::uuid AND lri.status = 'open')
  OR EXISTS (SELECT 1 FROM user_scope_grants usg WHERE usg.tenant_id = $1::uuid AND usg.scope_id = $2::uuid AND (usg.valid_to IS NULL OR usg.valid_to > now()))
  OR EXISTS (SELECT 1 FROM counts_current_snapshot_rows csr WHERE csr.tenant_id = $1::uuid AND (csr.farm_id = $2::uuid OR csr.park_id = $2::uuid OR csr.shed_id = $2::uuid OR csr.resolved_location_id = $2::uuid))
  OR EXISTS (SELECT 1 FROM mortality_events me WHERE me.tenant_id = $1::uuid AND (me.canonical_farm_location_id = $2::uuid OR me.canonical_park_location_id = $2::uuid OR me.canonical_shed_location_id = $2::uuid OR me.canonical_housing_location_id = $2::uuid))
  OR EXISTS (SELECT 1 FROM counts_projection_rows cpr WHERE cpr.tenant_id = $1::uuid AND cpr.dimension_key = $2::text)
  OR EXISTS (SELECT 1 FROM mortality_projection_rows mpr WHERE mpr.tenant_id = $1::uuid AND mpr.dimension_key = $2::text)`, tenantID, locationID).Scan(&blocked)
	if err != nil {
		return false, err
	}
	return blocked, nil
}

func scanAliasRow(row pgx.Row) (domain.LocationAlias, error) {
	var item domain.LocationAlias
	var notes pgtype.Text
	var createdAt, updatedAt time.Time
	if err := row.Scan(&item.AliasID, &item.AliasCode, &item.CanonicalLocationID, &item.SourceContext, &item.Status, &notes, &item.RowVersion, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.LocationAlias{}, ports.ErrNotFound
		}
		return domain.LocationAlias{}, err
	}
	item.Notes = textPtr(notes)
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return item, nil
}

func scanCapacityRow(row pgx.Row) (domain.LocationCapacityRecord, error) {
	var item domain.LocationCapacityRecord
	var effectiveTo, sourceRef, notes pgtype.Text
	var createdAt, updatedAt time.Time
	if err := row.Scan(&item.CapacityRecordID, &item.LocationID, &item.CapacityKind, &item.CapacityValue, &item.EffectiveFrom, &effectiveTo, &item.Source, &sourceRef, &notes, &item.RowVersion, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.LocationCapacityRecord{}, ports.ErrNotFound
		}
		return domain.LocationCapacityRecord{}, err
	}
	item.EffectiveTo = textPtr(effectiveTo)
	item.SourceRef = textPtr(sourceRef)
	item.Notes = textPtr(notes)
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	return item, nil
}

func scanReviewItemRow(row pgx.Row) (domain.LocationReviewItem, error) {
	var item domain.LocationReviewItem
	var sourceContext, sourceLabel, normalizedLabel, canonicalLocationID, resolutionNotes pgtype.Text
	var candidateBytes []byte
	var createdAt, updatedAt time.Time
	var resolvedAt pgtype.Timestamptz
	if err := row.Scan(&item.ReviewID, &item.ReviewType, &item.Status, &sourceContext, &sourceLabel, &normalizedLabel, &canonicalLocationID, &candidateBytes, &item.EvidenceHash, &resolutionNotes, &item.RowVersion, &createdAt, &updatedAt, &resolvedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.LocationReviewItem{}, ports.ErrNotFound
		}
		return domain.LocationReviewItem{}, err
	}
	item.SourceContext = textPtr(sourceContext)
	item.SourceLabel = textPtr(sourceLabel)
	item.NormalizedSourceLabel = textPtr(normalizedLabel)
	item.CanonicalLocationID = textPtr(canonicalLocationID)
	item.CandidateLocationIDs = decodeStringArray(candidateBytes)
	item.ResolutionNotes = textPtr(resolutionNotes)
	item.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	item.UpdatedAt = updatedAt.UTC().Format(time.RFC3339)
	item.ResolvedAt = timePtr(resolvedAt)
	return item, nil
}

type auditEntry struct {
	TenantID     string
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	ScopeType    string
	ScopeID      string
	BeforeState  []byte
	AfterState   []byte
	Metadata     map[string]any
	TraceID      string
}

func insertLocationAudit(ctx context.Context, tx pgx.Tx, entry auditEntry) error {
	metadataBytes, err := json.Marshal(entry.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id, actor_id, actor_type, action, resource_type, resource_id,
  scope_type, scope_id, before_state, after_state, metadata, trace_id
) VALUES (
  $1::uuid, $2::uuid, CASE WHEN $2::uuid IS NULL THEN 'system' ELSE 'human' END,
  $3, $4, $5::uuid, $6, $7::uuid, $8::jsonb, $9::jsonb, $10::jsonb, $11
)`, entry.TenantID, nullableString(nonEmptyStringPtr(entry.ActorID)), entry.Action, entry.ResourceType, nullableString(&entry.ResourceID),
		nullableString(nonEmptyStringPtr(entry.ScopeType)), nullableString(nonEmptyStringPtr(entry.ScopeID)),
		nullableBytes(entry.BeforeState), nullableBytes(entry.AfterState), metadataBytes, nullableString(nonEmptyStringPtr(entry.TraceID)))
	return err
}

func insertInvalidations(ctx context.Context, tx pgx.Tx, tenantID, locationID, reason, traceID string) error {
	for _, module := range []string{"counts", "mortality", "infra", "feed", "vaccination"} {
		if _, err := tx.Exec(ctx, `
INSERT INTO location_projection_invalidations (
  tenant_id, source_module, projection_module, reason, affected_location_id, source_ref
) VALUES ($1::uuid, 'locations', $2, $3, $4::uuid, $5)`, tenantID, module, reason, locationID, nullableString(nonEmptyStringPtr(traceID))); err != nil {
			return err
		}
	}
	return nil
}

func insertLocationDeleteInvalidations(ctx context.Context, tx pgx.Tx, tenantID, locationID, traceID string) error {
	sourceRef := strings.TrimSpace(locationID)
	if strings.TrimSpace(traceID) != "" {
		sourceRef = traceID + ":" + sourceRef
	}
	for _, module := range []string{"counts", "mortality", "infra", "feed", "vaccination"} {
		if _, err := tx.Exec(ctx, `
INSERT INTO location_projection_invalidations (
  tenant_id, source_module, projection_module, reason, affected_location_id, source_ref
) VALUES ($1::uuid, 'locations', $2, 'location_deleted', NULL, $3)`, tenantID, module, sourceRef); err != nil {
			return err
		}
	}
	return nil
}

func detachLocationInvalidationsForDelete(ctx context.Context, tx pgx.Tx, tenantID, locationID string) error {
	_, err := tx.Exec(ctx, `
UPDATE location_projection_invalidations
SET affected_location_id = NULL,
    source_ref = COALESCE(source_ref, $2::text),
    status = CASE WHEN status = 'pending' THEN 'superseded' ELSE status END
WHERE tenant_id = $1::uuid
  AND affected_location_id = $2::uuid`, tenantID, locationID)
	return err
}

func beginIdempotency(ctx context.Context, tx pgx.Tx, key, tenantID, scope, requestHash, resultType string) (string, bool, error) {
	var inserted string
	err := tx.QueryRow(ctx, `
INSERT INTO idempotency_keys (idempotency_key, tenant_id, scope, request_hash, status, expires_at)
VALUES ($1, $2::uuid, $3, $4, 'started', now() + interval '24 hours')
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING idempotency_key`, key, tenantID, scope, requestHash).Scan(&inserted)
	if err == nil {
		return "", false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	var existingHash, status string
	var existingResultType pgtype.Text
	var existingResultID pgtype.Text
	if err := tx.QueryRow(ctx, `
SELECT request_hash, status, result_type, result_id::text
FROM idempotency_keys
WHERE idempotency_key = $1`, key).Scan(&existingHash, &status, &existingResultType, &existingResultID); err != nil {
		return "", false, err
	}
	if existingHash != requestHash {
		return "", false, ports.ErrIdempotencyConflict
	}
	if status != "completed" || !existingResultType.Valid || existingResultType.String != resultType || !existingResultID.Valid || strings.TrimSpace(existingResultID.String) == "" {
		return "", false, ports.ErrIdempotencyPending
	}
	return existingResultID.String, true, nil
}

func completeIdempotency(ctx context.Context, tx pgx.Tx, key, resultType, resultID string) error {
	_, err := tx.Exec(ctx, `
UPDATE idempotency_keys
SET status = 'completed',
    result_type = $1,
    result_id = $2::uuid,
    completed_at = now()
WHERE idempotency_key = $3`, resultType, resultID, key)
	return err
}

func rollbackUnlessCommitted(ctx context.Context, tx pgx.Tx, committed *bool) {
	if !*committed {
		_ = tx.Rollback(ctx)
	}
}

func mapWriteErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ports.ErrNotFound) || errors.Is(err, ports.ErrWriteConflict) || errors.Is(err, ports.ErrBlockingUsage) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23503", "23514", "23P01", "P0001":
			return ports.ErrWriteConflict
		}
	}
	return err
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func nullableOperationalDisplayOrder(op *domain.OperationalAttributes) any {
	if op == nil {
		return nil
	}
	return op.DisplayOrder
}

func nullableOperationalNotes(op *domain.OperationalAttributes) any {
	if op == nil {
		return nil
	}
	return nullableString(op.Notes)
}

func nonEmptyStringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func metadata(clientKey, storedKey, scope string) map[string]any {
	return map[string]any{
		"client_idempotency_key": clientKey,
		"idempotency_key":        storedKey,
		"idempotency_scope":      scope,
	}
}

func metadataWithReason(clientKey, storedKey, scope, reason string) map[string]any {
	out := metadata(clientKey, storedKey, scope)
	out["reason"] = reason
	return out
}
