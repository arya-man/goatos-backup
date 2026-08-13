package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

// reclassifyResultType labels the idempotency row's stored result. The reclassification's result is
// a PEN, not a goat, so it deliberately does not reuse goatLifecycleResultType -- result_id carries
// the shed id and a replay must not be mistaken for a single-animal lifecycle mutation.
const reclassifyResultType = "shed_stage_reclassification"

func insertIdempotencyStartedParams(cmd ports.ReclassifyShedStageCommand) identitydb.InsertIdempotencyStartedParams {
	tenantUUID, _ := uuidParam(cmd.TenantID)
	return identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	}
}

func completeReclassifyIdempotency(ctx context.Context, qtx *identitydb.Queries, cmd ports.ReclassifyShedStageCommand, result *ports.ReclassifyShedStageResult) error {
	shedUUID, err := uuidParam(result.ShedID)
	if err != nil {
		return err
	}
	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(reclassifyResultType),
		ResultID:       shedUUID,
		IdempotencyKey: cmd.StoredIdempotencyKey,
	}); err != nil {
		return fmt.Errorf("identity: reclassify shed stage: complete idempotency: %w", err)
	}
	return nil
}

// replayReclassifyShedStage answers an EXACT replay of a completed reclassification without taking
// row locks, writing anything, or re-emitting events.
//
// The reclassified count is re-derived from the goat_identity_events this key actually wrote, not
// recomputed from the pen's present state: by the time a replay arrives, animals may have been
// shifted in or out, so "how many carry the tag now" is a different question from "how many did
// this command change". A same-key different-payload replay is rejected on the request hash.
func (r *Repository) replayReclassifyShedStage(ctx context.Context, tx pgx.Tx, cmd ports.ReclassifyShedStageCommand) (*ports.ReclassifyShedStageResult, error) {
	qtx := r.queries.WithTx(tx)
	idempotency, err := qtx.GetIdempotencyKey(ctx, cmd.StoredIdempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, fmt.Errorf("identity: reclassify shed stage: read idempotency: %w", err)
	}
	if idempotency.RequestHash != cmd.RequestHash {
		return nil, ports.ErrIdempotencyConflict
	}
	if idempotency.Status != "completed" || idempotency.ResultType != reclassifyResultType {
		return nil, ports.ErrIdempotencyPending
	}

	resolution, err := r.resolveReclassifyStage(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	location, err := r.resolveReclassifyLocation(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}

	var reclassified int
	if err := tx.QueryRow(ctx, `
SELECT count(*) FROM goat_identity_events
WHERE tenant_id = $1::uuid AND source_record_id = $2::text AND event_type = $3::text`,
		cmd.TenantID, cmd.StoredIdempotencyKey, goatStageChangedEventType).Scan(&reclassified); err != nil {
		return nil, fmt.Errorf("identity: reclassify shed stage: replay count: %w", err)
	}

	return &ports.ReclassifyShedStageResult{
		ShedID:                     cmd.ShedID,
		ShedName:                   location.ShedName,
		PartitionLabel:             location.PartitionLabel,
		OperationalLocationDisplay: location.Display(),
		ManagementStage:            resolution.stage,
		AgeBand:                    resolution.ageBand,
		TotalLive:                  reclassified,
		Reclassified:               reclassified,
		Unchanged:                  0,
	}, nil
}

// recordReclassifyAudit writes the accountability row. This is doing the work that Park Head
// approval does on the movement path, so it records the WHOLE decision -- which pen, which cohort,
// how many animals were touched and how many were already correct -- not just that a call happened.
func (r *Repository) recordReclassifyAudit(ctx context.Context, tx pgx.Tx, cmd ports.ReclassifyShedStageCommand, result *ports.ReclassifyShedStageResult) error {
	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return err
	}
	shedUUID, err := uuidParam(result.ShedID)
	if err != nil {
		return err
	}
	afterState, err := json.Marshal(map[string]any{
		"shed_id":                      result.ShedID,
		"shed_name":                    result.ShedName,
		"partition_label":              result.PartitionLabel,
		"operational_location_display": result.OperationalLocationDisplay,
		"management_stage":             result.ManagementStage,
		"age_band":                     result.AgeBand,
		"total_live":                   result.TotalLive,
		"reclassified":                 result.Reclassified,
		"unchanged":                    result.Unchanged,
	})
	if err != nil {
		return fmt.Errorf("identity: reclassify shed stage: encode audit state: %w", err)
	}
	metadata, err := json.Marshal(map[string]any{
		"reason":                  cmd.Reason,
		"management_stage_source": reclassifyStageSource,
		"idempotency_key":         cmd.StoredIdempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("identity: reclassify shed stage: encode audit metadata: %w", err)
	}

	if err := r.queries.WithTx(tx).InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     tenantUUID,
		ActorID:      actorUUID,
		Action:       reclassifyStageSource,
		ResourceType: "shed",
		ResourceID:   shedUUID,
		ScopeType:    textParam("shed"),
		ScopeID:      shedUUID,
		AfterState:   afterState,
		Metadata:     metadata,
		TraceID:      textParam(cmd.TraceID),
	}); err != nil {
		return fmt.Errorf("identity: reclassify shed stage: audit: %w", err)
	}
	return nil
}
