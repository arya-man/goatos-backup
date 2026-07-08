package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	identitydb "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres/sqlc"
	"github.com/vgoats/goatos/backend/internal/identity/domain"
	"github.com/vgoats/goatos/backend/internal/identity/ports"
)

const (
	goatLifecycleResultType               = "goat"
	goatLifecyclePolicyVersion            = "goat-lifecycle-v1"
	goatMovedEventType                    = "goat.location.changed"
	goatExitedEventType                   = "goat.exited"
	goatStageChangedEventType             = "goat.stage_changed"
	goatHealthChangedEventType            = "goat.health.changed"
	goatReproductiveChangedEventType      = "goat.reproductive.changed"
	goatLifecycleAggregate                = "goat"
	goatLifecycleSubject                  = "goat"
	goatLifecycleTopic                    = "identity.events"
	goatMovedDecisionType                 = "move_goat"
	goatMovedDecisionResult               = "goat_moved"
	goatExitedDecisionType                = "exit_goat"
	goatExitedDecisionResult              = "goat_exited"
	goatStageChangedDecisionType          = "stage_goat"
	goatStageChangedDecisionResult        = "goat_stage_changed"
	goatHealthChangedDecisionType         = "health_goat"
	goatHealthChangedDecisionResult       = "goat_health_changed"
	goatReproductiveChangedDecisionType   = "reproductive_goat"
	goatReproductiveChangedDecisionResult = "goat_reproductive_changed"
	goatLocationHistoryReasonMove         = "admin_goat_move"
)

type goatMutationState struct {
	LifecycleStatus    string
	MergedIntoGoatID   *string
	ManagementStage    string
	HealthStatus       string
	ReproductiveStatus string
	RowVersion         int
	CurrentLocation    *string
	FarmID             *string
	ParkID             *string
	ShedID             *string
}

func (r *Repository) MoveGoat(ctx context.Context, cmd ports.MoveGoatCommand) (*ports.AdminGoatMutationResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(cmd.GoatID)
	if err != nil {
		return nil, err
	}
	if _, err := uuidParam(cmd.ToParkID); err != nil {
		return nil, err
	}
	if _, err := uuidParam(cmd.ToShedID); err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)
	if _, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return r.replayGoatLifecycleMutation(ctx, tx, qtx, tenantUUID, goatUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	} else if err != nil {
		return nil, err
	}

	if err := r.ensureShedUnderPark(ctx, cmd.TenantID, cmd.ToShedID, cmd.ToParkID); err != nil {
		return nil, err
	}
	state, err := lockGoatForLifecycleMutation(ctx, tx, cmd.TenantID, cmd.GoatID)
	if err != nil {
		return nil, err
	}
	if state.RowVersion != cmd.RowVersion || state.MergedIntoGoatID != nil || exitedLifecycleStatus(state.LifecycleStatus) {
		return nil, ports.ErrWriteConflict
	}
	if state.ShedID != nil && *state.ShedID == cmd.ToShedID && state.ParkID != nil && *state.ParkID == cmd.ToParkID {
		return nil, ports.ErrWriteConflict
	}

	if _, err := tx.Exec(ctx, `
UPDATE goats
SET current_location_id = $4::uuid,
    park_id = $3::uuid,
    shed_id = $4::uuid,
    updated_at = $5::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		cmd.TenantID, cmd.GoatID, cmd.ToParkID, cmd.ToShedID, cmd.OccurredAt); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO goat_location_history (
  tenant_id, goat_id, from_location_id, to_location_id, reason, occurred_at, actor_id, source_record_id
) VALUES (
  $1::uuid, $2::uuid, nullif($3::text, '')::uuid, $4::uuid, $5, $6::timestamptz, $7::uuid, $8
)`,
		cmd.TenantID, cmd.GoatID, stringValue(state.CurrentLocation), cmd.ToShedID, goatLocationHistoryReasonMove, cmd.OccurredAt, cmd.ActorID, cmd.StoredIdempotencyKey); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"goat_id":          cmd.GoatID,
		"from_park_id":     stringValue(state.ParkID),
		"from_shed_id":     stringValue(state.ShedID),
		"to_park_id":       cmd.ToParkID,
		"to_shed_id":       cmd.ToShedID,
		"scope_type":       "shed",
		"scope_id":         cmd.ToShedID,
		"reason":           cmd.Reason,
		"row_version_from": cmd.RowVersion,
	}
	return r.finishGoatLifecycleMutation(ctx, tx, qtx, &committed, goatLifecycleFinish{
		TenantUUID:     tenantUUID,
		ActorUUID:      actorUUID,
		GoatUUID:       goatUUID,
		AggregateUUID:  goatUUID,
		Command:        goatLifecycleCommandFromMove(cmd),
		DecisionType:   goatMovedDecisionType,
		DecisionResult: goatMovedDecisionResult,
		EventType:      goatMovedEventType,
		OccurredAt:     cmd.OccurredAt,
		Payload:        payload,
		Scope: domain.LocationScope{
			FarmID: state.FarmID,
			ParkID: &cmd.ToParkID,
			ShedID: &cmd.ToShedID,
		},
		AggregateType: goatLifecycleAggregate,
		SubjectType:   goatLifecycleSubject,
		SubjectID:     cmd.GoatID,
	})
}

func (r *Repository) ExitGoat(ctx context.Context, cmd ports.ExitGoatCommand) (*ports.AdminGoatMutationResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(cmd.GoatID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)
	if _, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return r.replayGoatLifecycleMutation(ctx, tx, qtx, tenantUUID, goatUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	} else if err != nil {
		return nil, err
	}

	state, err := lockGoatForLifecycleMutation(ctx, tx, cmd.TenantID, cmd.GoatID)
	if err != nil {
		return nil, err
	}
	if state.RowVersion != cmd.RowVersion || state.MergedIntoGoatID != nil || exitedLifecycleStatus(state.LifecycleStatus) {
		return nil, ports.ErrWriteConflict
	}
	if criticalDeathExit(cmd.LifecycleStatus, cmd.ExitReason) && !cmd.GuardrailApproved {
		return nil, ports.ErrCriticalDeathGuardrailRequired
	}
	if _, err := tx.Exec(ctx, `
UPDATE goats
SET lifecycle_status = $3,
    exit_reason = $4,
    exited_at = $5::timestamptz,
    updated_at = $5::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		cmd.TenantID, cmd.GoatID, cmd.LifecycleStatus, cmd.ExitReason, cmd.OccurredAt); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"goat_id":             cmd.GoatID,
		"previous_lifecycle":  state.LifecycleStatus,
		"lifecycle_status":    cmd.LifecycleStatus,
		"exit_reason":         cmd.ExitReason,
		"reason":              cmd.Reason,
		"row_version_from":    cmd.RowVersion,
		"current_location_id": stringValue(state.CurrentLocation),
		"current_park_id":     stringValue(state.ParkID),
		"current_shed_id":     stringValue(state.ShedID),
		"scope_type":          "goat",
		"scope_id":            cmd.GoatID,
	}
	return r.finishGoatLifecycleMutation(ctx, tx, qtx, &committed, goatLifecycleFinish{
		TenantUUID:     tenantUUID,
		ActorUUID:      actorUUID,
		GoatUUID:       goatUUID,
		AggregateUUID:  goatUUID,
		Command:        goatLifecycleCommandFromExit(cmd),
		DecisionType:   goatExitedDecisionType,
		DecisionResult: goatExitedDecisionResult,
		EventType:      goatExitedEventType,
		OccurredAt:     cmd.OccurredAt,
		Payload:        payload,
		Scope: domain.LocationScope{
			FarmID: state.FarmID,
			ParkID: state.ParkID,
			ShedID: state.ShedID,
		},
		AggregateType: goatLifecycleAggregate,
		SubjectType:   goatLifecycleSubject,
		SubjectID:     cmd.GoatID,
	})
}

func (r *Repository) StageGoat(ctx context.Context, cmd ports.StageGoatCommand) (*ports.AdminGoatMutationResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(cmd.GoatID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)
	if _, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return r.replayGoatLifecycleMutation(ctx, tx, qtx, tenantUUID, goatUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	} else if err != nil {
		return nil, err
	}

	state, err := lockGoatForLifecycleMutation(ctx, tx, cmd.TenantID, cmd.GoatID)
	if err != nil {
		return nil, err
	}
	stageOK, err := activeManagementStageExists(ctx, tx, cmd.TenantID, cmd.ManagementStage)
	if err != nil {
		return nil, err
	}
	if !stageOK {
		return nil, ports.ErrInvalidReference
	}
	if state.RowVersion != cmd.RowVersion || state.MergedIntoGoatID != nil || exitedLifecycleStatus(state.LifecycleStatus) {
		return nil, ports.ErrWriteConflict
	}
	if state.ManagementStage == cmd.ManagementStage {
		return nil, ports.ErrWriteConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE goats
SET management_stage = $3,
    updated_at = $4::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		cmd.TenantID, cmd.GoatID, cmd.ManagementStage, cmd.OccurredAt); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"goat_id":                   cmd.GoatID,
		"previous_management_stage": state.ManagementStage,
		"management_stage":          cmd.ManagementStage,
		"reason":                    cmd.Reason,
		"row_version_from":          cmd.RowVersion,
		"current_location_id":       stringValue(state.CurrentLocation),
		"current_park_id":           stringValue(state.ParkID),
		"current_shed_id":           stringValue(state.ShedID),
		"scope_type":                "goat",
		"scope_id":                  cmd.GoatID,
	}
	return r.finishGoatLifecycleMutation(ctx, tx, qtx, &committed, goatLifecycleFinish{
		TenantUUID:     tenantUUID,
		ActorUUID:      actorUUID,
		GoatUUID:       goatUUID,
		AggregateUUID:  goatUUID,
		Command:        goatLifecycleCommandFromStage(cmd),
		DecisionType:   goatStageChangedDecisionType,
		DecisionResult: goatStageChangedDecisionResult,
		EventType:      goatStageChangedEventType,
		OccurredAt:     cmd.OccurredAt,
		Payload:        payload,
		Scope: domain.LocationScope{
			FarmID: state.FarmID,
			ParkID: state.ParkID,
			ShedID: state.ShedID,
		},
		AggregateType: goatLifecycleAggregate,
		SubjectType:   goatLifecycleSubject,
		SubjectID:     cmd.GoatID,
	})
}

func (r *Repository) HealthGoat(ctx context.Context, cmd ports.HealthGoatCommand) (*ports.AdminGoatMutationResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(cmd.GoatID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)
	if _, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return r.replayGoatLifecycleMutation(ctx, tx, qtx, tenantUUID, goatUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	} else if err != nil {
		return nil, err
	}

	state, err := lockGoatForLifecycleMutation(ctx, tx, cmd.TenantID, cmd.GoatID)
	if err != nil {
		return nil, err
	}
	if state.RowVersion != cmd.RowVersion || state.MergedIntoGoatID != nil || exitedLifecycleStatus(state.LifecycleStatus) {
		return nil, ports.ErrWriteConflict
	}
	if state.HealthStatus == cmd.HealthStatus {
		return nil, ports.ErrWriteConflict
	}
	if criticalHealthStatus(state.HealthStatus) || criticalHealthStatus(cmd.HealthStatus) {
		return nil, ports.ErrGuardrailRequired
	}
	if _, err := tx.Exec(ctx, `
UPDATE goats
SET health_status = $3,
    updated_at = $4::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		cmd.TenantID, cmd.GoatID, cmd.HealthStatus, cmd.OccurredAt); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"goat_id":                cmd.GoatID,
		"previous_health_status": state.HealthStatus,
		"health_status":          cmd.HealthStatus,
		"reason":                 cmd.Reason,
		"row_version_from":       cmd.RowVersion,
		"current_location_id":    stringValue(state.CurrentLocation),
		"current_park_id":        stringValue(state.ParkID),
		"current_shed_id":        stringValue(state.ShedID),
		"scope_type":             "goat",
		"scope_id":               cmd.GoatID,
	}
	return r.finishGoatLifecycleMutation(ctx, tx, qtx, &committed, goatLifecycleFinish{
		TenantUUID:     tenantUUID,
		ActorUUID:      actorUUID,
		GoatUUID:       goatUUID,
		AggregateUUID:  goatUUID,
		Command:        goatLifecycleCommandFromHealth(cmd),
		DecisionType:   goatHealthChangedDecisionType,
		DecisionResult: goatHealthChangedDecisionResult,
		EventType:      goatHealthChangedEventType,
		OccurredAt:     cmd.OccurredAt,
		Payload:        payload,
		Scope: domain.LocationScope{
			FarmID: state.FarmID,
			ParkID: state.ParkID,
			ShedID: state.ShedID,
		},
		AggregateType: goatLifecycleAggregate,
		SubjectType:   goatLifecycleSubject,
		SubjectID:     cmd.GoatID,
	})
}

func (r *Repository) ReproductiveGoat(ctx context.Context, cmd ports.ReproductiveGoatCommand) (*ports.AdminGoatMutationResult, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	tenantUUID, err := uuidParam(cmd.TenantID)
	if err != nil {
		return nil, err
	}
	actorUUID, err := uuidParam(cmd.ActorID)
	if err != nil {
		return nil, err
	}
	goatUUID, err := uuidParam(cmd.GoatID)
	if err != nil {
		return nil, err
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	qtx := r.queries.WithTx(tx)
	if _, err = qtx.InsertIdempotencyStarted(ctx, identitydb.InsertIdempotencyStartedParams{
		IdempotencyKey: cmd.StoredIdempotencyKey,
		TenantID:       tenantUUID,
		Scope:          cmd.IdempotencyScope,
		RequestHash:    cmd.RequestHash,
	}); errors.Is(err, pgx.ErrNoRows) {
		return r.replayGoatLifecycleMutation(ctx, tx, qtx, tenantUUID, goatUUID, cmd.StoredIdempotencyKey, cmd.RequestHash)
	} else if err != nil {
		return nil, err
	}

	state, err := lockGoatForLifecycleMutation(ctx, tx, cmd.TenantID, cmd.GoatID)
	if err != nil {
		return nil, err
	}
	// Vocabulary is source-backed, not a hardcoded map: the target must be an active
	// status_definitions(axis='reproductive') code (mirrors StageGoat's animal_stage_lookup check).
	reproductiveOK, err := activeReproductiveStatusExists(ctx, tx, cmd.ReproductiveStatus)
	if err != nil {
		return nil, err
	}
	if !reproductiveOK {
		return nil, ports.ErrInvalidReference
	}
	if state.RowVersion != cmd.RowVersion || state.MergedIntoGoatID != nil || exitedLifecycleStatus(state.LifecycleStatus) {
		return nil, ports.ErrWriteConflict
	}
	if state.ReproductiveStatus == cmd.ReproductiveStatus {
		return nil, ports.ErrWriteConflict
	}
	if _, err := tx.Exec(ctx, `
UPDATE goats
SET reproductive_status = $3,
    breeding_date = COALESCE($4, breeding_date),
    last_delivery_date = COALESCE($5, last_delivery_date),
    updated_at = $6::timestamptz,
    row_version = row_version + 1
WHERE tenant_id = $1::uuid AND goat_id = $2::uuid`,
		cmd.TenantID, cmd.GoatID, cmd.ReproductiveStatus, nullableDate(cmd.BreedingDate), nullableDate(cmd.LastDeliveryDate), cmd.OccurredAt); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"goat_id":                      cmd.GoatID,
		"previous_reproductive_status": state.ReproductiveStatus,
		"reproductive_status":          cmd.ReproductiveStatus,
		"reason":                       cmd.Reason,
		"row_version_from":             cmd.RowVersion,
		"current_location_id":          stringValue(state.CurrentLocation),
		"current_park_id":              stringValue(state.ParkID),
		"current_shed_id":              stringValue(state.ShedID),
		"scope_type":                   "goat",
		"scope_id":                     cmd.GoatID,
	}
	if cmd.BreedingDate != nil {
		payload["breeding_date"] = cmd.BreedingDate.UTC().Format("2006-01-02")
	}
	if cmd.LastDeliveryDate != nil {
		payload["last_delivery_date"] = cmd.LastDeliveryDate.UTC().Format("2006-01-02")
	}
	return r.finishGoatLifecycleMutation(ctx, tx, qtx, &committed, goatLifecycleFinish{
		TenantUUID:     tenantUUID,
		ActorUUID:      actorUUID,
		GoatUUID:       goatUUID,
		AggregateUUID:  goatUUID,
		Command:        goatLifecycleCommandFromReproductive(cmd),
		DecisionType:   goatReproductiveChangedDecisionType,
		DecisionResult: goatReproductiveChangedDecisionResult,
		EventType:      goatReproductiveChangedEventType,
		OccurredAt:     cmd.OccurredAt,
		Payload:        payload,
		Scope: domain.LocationScope{
			FarmID: state.FarmID,
			ParkID: state.ParkID,
			ShedID: state.ShedID,
		},
		AggregateType: goatLifecycleAggregate,
		SubjectType:   goatLifecycleSubject,
		SubjectID:     cmd.GoatID,
	})
}

type goatLifecycleCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	TraceID              string
	GoatID               string
	Reason               string
	EvidenceRefs         []domain.EvidenceRef
}

type goatLifecycleFinish struct {
	TenantUUID     pgtype.UUID
	ActorUUID      pgtype.UUID
	GoatUUID       pgtype.UUID
	AggregateUUID  pgtype.UUID
	Command        goatLifecycleCommand
	DecisionType   string
	DecisionResult string
	EventType      string
	OccurredAt     time.Time
	Payload        map[string]any
	Scope          domain.LocationScope
	AggregateType  string
	SubjectType    string
	SubjectID      string
}

func goatLifecycleCommandFromMove(cmd ports.MoveGoatCommand) goatLifecycleCommand {
	return goatLifecycleCommand{
		TenantID:             cmd.TenantID,
		ActorID:              cmd.ActorID,
		ClientIdempotencyKey: cmd.ClientIdempotencyKey,
		StoredIdempotencyKey: cmd.StoredIdempotencyKey,
		IdempotencyScope:     cmd.IdempotencyScope,
		TraceID:              cmd.TraceID,
		GoatID:               cmd.GoatID,
		Reason:               cmd.Reason,
		EvidenceRefs:         cmd.EvidenceRefs,
	}
}

func goatLifecycleCommandFromExit(cmd ports.ExitGoatCommand) goatLifecycleCommand {
	return goatLifecycleCommand{
		TenantID:             cmd.TenantID,
		ActorID:              cmd.ActorID,
		ClientIdempotencyKey: cmd.ClientIdempotencyKey,
		StoredIdempotencyKey: cmd.StoredIdempotencyKey,
		IdempotencyScope:     cmd.IdempotencyScope,
		TraceID:              cmd.TraceID,
		GoatID:               cmd.GoatID,
		Reason:               cmd.Reason,
		EvidenceRefs:         cmd.EvidenceRefs,
	}
}

func goatLifecycleCommandFromStage(cmd ports.StageGoatCommand) goatLifecycleCommand {
	return goatLifecycleCommand{
		TenantID:             cmd.TenantID,
		ActorID:              cmd.ActorID,
		ClientIdempotencyKey: cmd.ClientIdempotencyKey,
		StoredIdempotencyKey: cmd.StoredIdempotencyKey,
		IdempotencyScope:     cmd.IdempotencyScope,
		TraceID:              cmd.TraceID,
		GoatID:               cmd.GoatID,
		Reason:               cmd.Reason,
		EvidenceRefs:         cmd.EvidenceRefs,
	}
}

func goatLifecycleCommandFromHealth(cmd ports.HealthGoatCommand) goatLifecycleCommand {
	return goatLifecycleCommand{
		TenantID:             cmd.TenantID,
		ActorID:              cmd.ActorID,
		ClientIdempotencyKey: cmd.ClientIdempotencyKey,
		StoredIdempotencyKey: cmd.StoredIdempotencyKey,
		IdempotencyScope:     cmd.IdempotencyScope,
		TraceID:              cmd.TraceID,
		GoatID:               cmd.GoatID,
		Reason:               cmd.Reason,
		EvidenceRefs:         cmd.EvidenceRefs,
	}
}

func goatLifecycleCommandFromReproductive(cmd ports.ReproductiveGoatCommand) goatLifecycleCommand {
	return goatLifecycleCommand{
		TenantID:             cmd.TenantID,
		ActorID:              cmd.ActorID,
		ClientIdempotencyKey: cmd.ClientIdempotencyKey,
		StoredIdempotencyKey: cmd.StoredIdempotencyKey,
		IdempotencyScope:     cmd.IdempotencyScope,
		TraceID:              cmd.TraceID,
		GoatID:               cmd.GoatID,
		Reason:               cmd.Reason,
		EvidenceRefs:         cmd.EvidenceRefs,
	}
}

// activeReproductiveStatusExists validates a reproductive target against the source-backed
// vocabulary (active status_definitions on the reproductive axis) instead of a hardcoded list.
// status_definitions is a global reference table (no tenant scoping), matching the adminui read.
func activeReproductiveStatusExists(ctx context.Context, tx pgx.Tx, statusCode string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM status_definitions
  WHERE axis = 'reproductive'
    AND active = true
    AND status_code = $1
)`, statusCode).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// nullableDate maps an optional date to a pgtype.Date. A nil pointer becomes SQL NULL so the
// COALESCE in the reproductive UPDATE keeps the stored value untouched.
func nullableDate(value *time.Time) pgtype.Date {
	if value == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *value, Valid: true}
}

func activeManagementStageExists(ctx context.Context, tx pgx.Tx, tenantID, stageCode string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM animal_stage_lookup
  WHERE tenant_id = $1::uuid
    AND stage_code = $2
    AND status = 'active'
)`, tenantID, stageCode).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (r *Repository) finishGoatLifecycleMutation(ctx context.Context, tx pgx.Tx, qtx *identitydb.Queries, committed *bool, finish goatLifecycleFinish) (*ports.AdminGoatMutationResult, error) {
	decisionID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	decisionUUID, err := uuidParam(decisionID)
	if err != nil {
		return nil, err
	}
	decisionRecord, err := goatLifecycleDecisionRecordPayload(finish, decisionID)
	if err != nil {
		return nil, err
	}
	decisionEvidence, err := json.Marshal(map[string]any{
		"evidence_refs":   finish.Command.EvidenceRefs,
		"reason":          finish.Command.Reason,
		"decision_record": json.RawMessage(decisionRecord),
	})
	if err != nil {
		return nil, err
	}
	decisionRow, err := qtx.InsertIdentityDecision(ctx, identitydb.InsertIdentityDecisionParams{
		DecisionID:     decisionUUID,
		TenantID:       finish.TenantUUID,
		DecisionType:   finish.DecisionType,
		DecisionResult: finish.DecisionResult,
		DecisionState:  "approved",
		DecidedBy:      finish.ActorUUID,
		PolicyVersion:  goatLifecyclePolicyVersion,
		ReviewerID:     finish.ActorUUID,
		Evidence:       decisionEvidence,
		CreatedAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		ApprovedAt:     pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		DecidedAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	decision := decisionSummaryFromInsertRow(decisionRow)
	if _, err := tx.Exec(ctx, `
INSERT INTO identity_decision_goats (decision_id, tenant_id, goat_id, role)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'affected')`,
		decisionID, finish.Command.TenantID, finish.Command.GoatID); err != nil {
		return nil, err
	}

	eventID, err := qtx.NewUUID(ctx)
	if err != nil {
		return nil, err
	}
	eventUUID, err := uuidParam(eventID)
	if err != nil {
		return nil, err
	}
	finish.Payload["decision_id"] = decision.DecisionID
	eventPayload, err := json.Marshal(finish.Payload)
	if err != nil {
		return nil, err
	}
	eventRow, err := qtx.InsertGoatIdentityEvent(ctx, identitydb.InsertGoatIdentityEventParams{
		IdentityEventID: eventUUID,
		TenantID:        finish.TenantUUID,
		GoatID:          finish.GoatUUID,
		EventType:       finish.EventType,
		OccurredAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		RecordedAt:      pgtype.Timestamptz{Time: finish.OccurredAt, Valid: true},
		ActorID:         finish.ActorUUID,
		Payload:         eventPayload,
		DecisionID:      decisionUUID,
		IdempotencyKey:  finish.Command.StoredIdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertIdentityDecisionEvent(ctx, identitydb.InsertIdentityDecisionEventParams{
		DecisionID:      decisionUUID,
		TenantID:        finish.TenantUUID,
		EventID:         eventUUID,
		EventRecordedAt: eventRow.RecordedAt,
	}); err != nil {
		return nil, err
	}
	response, err := adminGoatMutationResult(ctx, qtx, finish.TenantUUID, finish.Command.TenantID, finish.GoatUUID, decision, []domain.EventSummary{{EventID: eventRow.EventID, EventType: finish.EventType}}, false, nil)
	if err != nil {
		return nil, err
	}
	afterState, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(map[string]any{
		"actor_id":               finish.Command.ActorID,
		"idempotency_key":        finish.Command.StoredIdempotencyKey,
		"client_idempotency_key": finish.Command.ClientIdempotencyKey,
		"idempotency_scope":      finish.Command.IdempotencyScope,
		"trace_id":               finish.Command.TraceID,
		"decision_id":            decision.DecisionID,
		"event_type":             finish.EventType,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertAuditLog(ctx, identitydb.InsertAuditLogParams{
		TenantID:     finish.TenantUUID,
		ActorID:      finish.ActorUUID,
		Action:       finish.EventType,
		ResourceType: goatLifecycleSubject,
		ResourceID:   finish.GoatUUID,
		ScopeType:    nullableText(nonEmptyStringPtr(scopeType(finish.Scope))),
		ScopeID:      nullableUUID(scopeID(finish.Scope)),
		AfterState:   afterState,
		Metadata:     metadata,
		TraceID:      nullableText(nonEmptyStringPtr(finish.Command.TraceID)),
	}); err != nil {
		return nil, err
	}
	if r.afterAuditHook != nil {
		if err := r.afterAuditHook(ctx); err != nil {
			return nil, err
		}
	}

	envelope, err := goatLifecycleDomainEventEnvelope(finish, eventRow.EventID)
	if err != nil {
		return nil, err
	}
	headers, err := json.Marshal(map[string]any{
		"actor_id":               finish.Command.ActorID,
		"client_idempotency_key": finish.Command.ClientIdempotencyKey,
		"trace_id":               finish.Command.TraceID,
		"decision_id":            decision.DecisionID,
	})
	if err != nil {
		return nil, err
	}
	if err := qtx.InsertOutboxMessage(ctx, identitydb.InsertOutboxMessageParams{
		TenantID:       finish.TenantUUID,
		EventID:        eventUUID,
		EventType:      finish.EventType,
		SchemaVersion:  eventSchemaVersion,
		AggregateType:  finish.AggregateType,
		AggregateID:    finish.AggregateUUID,
		Topic:          goatLifecycleTopic,
		Payload:        envelope,
		Headers:        headers,
		IdempotencyKey: finish.Command.StoredIdempotencyKey,
		TraceID:        nullableText(nonEmptyStringPtr(finish.Command.TraceID)),
	}); err != nil {
		return nil, err
	}
	if err := qtx.CompleteIdempotencyKey(ctx, identitydb.CompleteIdempotencyKeyParams{
		ResultType:     textParam(goatLifecycleResultType),
		ResultID:       finish.GoatUUID,
		IdempotencyKey: finish.Command.StoredIdempotencyKey,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	*committed = true
	return response, nil
}

func lockGoatForLifecycleMutation(ctx context.Context, tx pgx.Tx, tenantID, goatID string) (goatMutationState, error) {
	var state goatMutationState
	var mergedInto, currentLocation, farmID, parkID, shedID pgtype.UUID
	err := tx.QueryRow(ctx, `
	SELECT lifecycle_status, merged_into_goat_id, COALESCE(management_stage, ''), COALESCE(health_status, ''), COALESCE(reproductive_status, ''), row_version,
	       current_location_id, farm_id, park_id, shed_id
	FROM goats
	WHERE tenant_id = $1::uuid AND goat_id = $2::uuid
	FOR UPDATE`, tenantID, goatID).Scan(
		&state.LifecycleStatus,
		&mergedInto,
		&state.ManagementStage,
		&state.HealthStatus,
		&state.ReproductiveStatus,
		&state.RowVersion,
		&currentLocation,
		&farmID,
		&parkID,
		&shedID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, ports.ErrNotFound
	}
	if err != nil {
		return state, err
	}
	state.MergedIntoGoatID = uuidStringPtr(mergedInto)
	state.CurrentLocation = uuidStringPtr(currentLocation)
	state.FarmID = uuidStringPtr(farmID)
	state.ParkID = uuidStringPtr(parkID)
	state.ShedID = uuidStringPtr(shedID)
	return state, nil
}

func exitedLifecycleStatus(status string) bool {
	switch status {
	case "dead", "sold", "culled", "transferred", "lost", "merged", "inactive":
		return true
	default:
		return false
	}
}

func criticalHealthStatus(status string) bool {
	switch status {
	case "quarantine", "icu":
		return true
	default:
		return false
	}
}

func criticalDeathExit(lifecycleStatus, exitReason string) bool {
	return lifecycleStatus == "dead" || exitReason == "died"
}

func uuidStringPtr(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}
	out := uuidText(value)
	return &out
}

func goatLifecycleDecisionRecordPayload(finish goatLifecycleFinish, decisionID string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"decision_id":     decisionID,
		"decision_type":   finish.DecisionType,
		"decision_result": finish.DecisionResult,
		"decision_state":  "approved",
		"decided_by_type": "human",
		"decided_by":      finish.Command.ActorID,
		"reviewer_id":     finish.Command.ActorID,
		"policy_version":  goatLifecyclePolicyVersion,
		"reason":          finish.Command.Reason,
		"evidence": map[string]any{
			"evidence_refs": finish.Command.EvidenceRefs,
		},
		"affected_goats": []map[string]any{{
			"goat_id": finish.Command.GoatID,
			"role":    "affected",
		}},
		"idempotency_key": finish.Command.StoredIdempotencyKey,
		"trace_id":        finish.Command.TraceID,
		"created_at":      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"approved_at":     finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		"decided_at":      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
	})
}

func goatLifecycleDomainEventEnvelope(finish goatLifecycleFinish, eventID string) ([]byte, error) {
	envelope := eventEnvelope{
		EventID:         eventID,
		EventType:       finish.EventType,
		SchemaVersion:   eventSchemaVersion,
		SchemaRef:       eventSchemaRef,
		AggregateType:   finish.AggregateType,
		AggregateID:     finish.Command.GoatID,
		OccurredAt:      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		RecordedAt:      finish.OccurredAt.UTC().Format("2006-01-02T15:04:05.000000Z"),
		Producer:        eventProducer{Service: "goatos-api", Module: "identity"},
		IdempotencyKey:  finish.Command.StoredIdempotencyKey,
		Actor:           eventActor{ActorType: "human", ActorID: &finish.Command.ActorID},
		SubjectType:     finish.SubjectType,
		SubjectID:       finish.SubjectID,
		VisibilityScope: finish.Scope,
		EvidenceRefs:    finish.Command.EvidenceRefs,
		Payload:         finish.Payload,
		TraceID:         finish.Command.TraceID,
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	var withTenant map[string]any
	if err := json.Unmarshal(payload, &withTenant); err != nil {
		return nil, err
	}
	visibilityScope, ok := withTenant["visibility_scope"].(map[string]any)
	if !ok {
		visibilityScope = map[string]any{}
	}
	visibilityScope["tenant_id"] = finish.Command.TenantID
	withTenant["visibility_scope"] = visibilityScope
	return json.Marshal(withTenant)
}

func (r *Repository) replayGoatLifecycleMutation(ctx context.Context, tx pgx.Tx, qtx *identitydb.Queries, tenantUUID, goatUUID pgtype.UUID, key string, requestHash string) (*ports.AdminGoatMutationResult, error) {
	idempotency, err := qtx.GetIdempotencyKey(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, err
	}
	if idempotency.RequestHash != requestHash {
		return nil, ports.ErrIdempotencyConflict
	}
	if idempotency.Status != "completed" || idempotency.ResultType != goatLifecycleResultType || idempotency.ResultID != uuidText(goatUUID) {
		return nil, ports.ErrIdempotencyPending
	}
	var decision domain.DecisionRecordSummary
	var event domain.EventSummary
	err = tx.QueryRow(ctx, `
SELECT d.decision_id::text, d.decision_type, d.decision_result, d.decision_state, d.policy_version, d.created_at,
       gie.identity_event_id::text, gie.event_type
FROM goat_identity_events gie
JOIN identity_decisions d ON d.tenant_id = gie.tenant_id AND d.decision_id = gie.decision_id
WHERE gie.tenant_id = $1::uuid
  AND gie.goat_id = $2::uuid
  AND gie.idempotency_key = $3
ORDER BY gie.recorded_at DESC
LIMIT 1`, uuidText(tenantUUID), uuidText(goatUUID), key).Scan(
		&decision.DecisionID,
		&decision.DecisionType,
		&decision.DecisionResult,
		&decision.DecisionState,
		&decision.PolicyVersion,
		&decision.CreatedAt,
		&event.EventID,
		&event.EventType,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ports.ErrIdempotencyPending
	}
	if err != nil {
		return nil, err
	}
	firstResultID := idempotency.ResultID
	return adminGoatMutationResult(ctx, qtx, tenantUUID, uuidText(tenantUUID), goatUUID, decision, []domain.EventSummary{event}, true, &firstResultID)
}
