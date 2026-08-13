package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// reclassifyStageSource labels the provenance of a stage change on the event payload, so a consumer
// (and a human reading audit) can tell a cohort reclassification apart from the stage a shifting
// adopted from its destination shed. The shifting path writes "shifting_raise_request".
const reclassifyStageSource = "counts_shed_reclassification"

// scopeSQL selects the live animals of ONE exact physical shed.
//
// Two things here are load-bearing and must not be "simplified":
//
// "Live" uses the write-path definition (not merged, not exited) AND lifecycle_status, so a
// reclassification can never touch an animal the herd register no longer counts.
const reclassifyScopeSQL = `
    FROM goats g
    WHERE g.tenant_id = $1::uuid
      AND g.shed_id = $2::uuid
      AND g.lifecycle_status = 'alive'
      AND g.merged_into_goat_id IS NULL
      AND g.exited_at IS NULL`

func strptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// PreviewReclassifyShedStage answers "what would this button do" without writing anything.
//
// It runs in a read-only transaction and takes NO row locks: the commit re-derives the same scope
// under lock, so a preview that goes stale is corrected at commit time rather than pinning the pen
// while a human reads a dialog.
func (r *Repository) PreviewReclassifyShedStage(ctx context.Context, cmd ports.ReclassifyShedStageCommand) (*ports.ReclassifyShedStagePreview, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: preview reclassify shed stage: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	resolution, err := r.resolveReclassifyStage(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	location, err := r.resolveReclassifyLocation(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(location.ShedID) != "" {
		cmd.ShedID = location.ShedID
	}

	// Whole-scope aggregate over the pen, not a page of rows: one grouped read answers total,
	// changing, unchanged, and the composition breakdown together.
	//
	// projection-review: membership=goats rows in one (tenant, shed, normalized partition) pen;
	// group_key=(management_stage, age_band); join_cardinality=goat_shed_partitions is PK
	// (tenant_id, goat_id) so the LEFT JOIN is strictly 1:0..1 and cannot fan a goat into two
	// rows; pagination=none -- this is a whole-scope aggregate with no LIMIT, and the commit
	// re-derives the SAME scope predicate (reclassifyScopeSQL, shared verbatim) so preview and
	// write can never range over different key sets; scope=explicit tenant_id + shed_id +
	// normalized partition key, never shed NAME (names repeat across parks).
	//
	// Producer unique columns: goats(tenant_id, goat_id). Consumer match/group columns:
	// (tenant_id, shed_id, normalized partition) filtered, then (management_stage, age_band)
	// grouped. Row multiplicity of the joined side: goat_shed_partitions <= 1 per goat (PK).
	// Ratio check: Changing + Unchanged is partitioned from the SAME bucket set that sums to
	// TotalLive, so numerator and denominator range over one identical key set by construction.
	//
	// scale-guard:ignore: bounded single-pen aggregate over an indexed (tenant_id, shed_id) scope.
	rows, err := tx.Query(ctx, `
SELECT COALESCE(g.management_stage, '') AS stage,
       COALESCE(g.age_band, '')         AS age_band,
       count(*)                          AS animals
`+reclassifyScopeSQL+`
    GROUP BY 1, 2
    ORDER BY animals DESC, stage ASC`,
		cmd.TenantID, cmd.ShedID)
	if err != nil {
		return nil, fmt.Errorf("identity: preview reclassify shed stage: %w", err)
	}
	defer rows.Close()

	preview := &ports.ReclassifyShedStagePreview{
		ShedID:                     cmd.ShedID,
		ShedName:                   location.ShedName,
		PartitionLabel:             "",
		OperationalLocationDisplay: location.Display(),
		ManagementStage:            resolution.stage,
		AgeBand:                    resolution.ageBand,
	}
	for rows.Next() {
		var bucket ports.ReclassifyStageBucket
		if err := rows.Scan(&bucket.ManagementStage, &bucket.AgeBand, &bucket.Count); err != nil {
			return nil, fmt.Errorf("identity: preview reclassify shed stage: scan: %w", err)
		}
		preview.TotalLive += bucket.Count
		// Compared against the CANONICAL resolved tag and its band. age_band is part of the tag's
		// durable meaning, so a stale same-stage/wrong-band row still changes.
		if bucket.ManagementStage == resolution.stage && (resolution.ageBand == "" || bucket.AgeBand == resolution.ageBand) {
			preview.Unchanged += bucket.Count
		} else {
			preview.Changing += bucket.Count
		}
		preview.CurrentStages = append(preview.CurrentStages, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: preview reclassify shed stage: %w", err)
	}
	if preview.TotalLive == 0 {
		return nil, ports.ErrReclassifyEmptyScope
	}
	if preview.TotalLive > ports.MaxReclassifyGoatsPerCommand {
		return nil, ports.ErrReclassifyScopeTooLarge
	}
	return preview, nil
}

// ReclassifyShedStage applies the cohort tag to every live animal of one pen, in ONE transaction.
//
// Order inside the transaction is fixed by a database constraint, not by preference:
// outbox_messages_validate_event_tenant_trg requires the goat_identity_events row to exist before
// the outbox row that references its event id. So: reserve idempotency, lock and mint identity
// events for the animals that actually change, then one set-based UPDATE + outbox insert, then
// audit, then release the key.
func (r *Repository) ReclassifyShedStage(ctx context.Context, cmd ports.ReclassifyShedStageCommand) (*ports.ReclassifyShedStageResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: reclassify shed stage: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := r.queries.WithTx(tx)

	if _, err := qtx.InsertIdempotencyStarted(ctx, insertIdempotencyStartedParams(cmd)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Exact replay: the key is already held. Return the stored result rather than taking
			// locks and re-emitting events for animals that were reclassified by the first call.
			return r.replayReclassifyShedStage(ctx, tx, cmd)
		}
		return nil, fmt.Errorf("identity: reclassify shed stage: reserve idempotency: %w", err)
	}

	resolution, err := r.resolveReclassifyStage(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	location, err := r.resolveReclassifyLocation(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(location.ShedID) != "" {
		cmd.ShedID = location.ShedID
	}
	totalLive, err := r.countReclassifyScope(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if totalLive == 0 {
		return nil, ports.ErrReclassifyEmptyScope
	}
	if totalLive > ports.MaxReclassifyGoatsPerCommand {
		return nil, ports.ErrReclassifyScopeTooLarge
	}

	goatIDs, eventIDs, err := r.insertReclassifyIdentityEvents(ctx, tx, cmd, resolution)
	if err != nil {
		return nil, err
	}

	if len(goatIDs) > 0 {
		if err := r.applyReclassify(ctx, tx, cmd, resolution, location, goatIDs, eventIDs); err != nil {
			return nil, err
		}
	}

	result := &ports.ReclassifyShedStageResult{
		ShedID:                     cmd.ShedID,
		ShedName:                   location.ShedName,
		PartitionLabel:             "",
		OperationalLocationDisplay: location.Display(),
		ManagementStage:            resolution.stage,
		AgeBand:                    resolution.ageBand,
		TotalLive:                  totalLive,
		Reclassified:               len(goatIDs),
		Unchanged:                  totalLive - len(goatIDs),
	}

	if err := r.recordReclassifyAudit(ctx, tx, cmd, result); err != nil {
		return nil, err
	}
	if err := completeReclassifyIdempotency(ctx, qtx, cmd, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: reclassify shed stage: commit: %w", err)
	}
	return result, nil
}

// resolveReclassifyStage canonicalizes the requested tag against the tenant's active vocabulary and
// returns it together with the kid/adult band that tag carries.
//
// It reuses resolveDestinationTag's exact rules -- same lookup, same clinical fail-closed -- by
// borrowing its command shape, rather than re-deriving them here. Re-hardcoding the clinical set is
// specifically what the clinical-defer safety rule forbids.
func (r *Repository) resolveReclassifyStage(ctx context.Context, tx pgx.Tx, cmd ports.ReclassifyShedStageCommand) (destinationStageResolution, error) {
	resolution, err := r.resolveDestinationTag(ctx, tx, ports.RelocateGoatsCommand{
		TenantID:       cmd.TenantID,
		DestinationTag: cmd.ManagementStage,
	})
	if err != nil {
		return destinationStageResolution{}, err
	}
	if resolution.stage == "" {
		// resolveDestinationTag treats an empty tag as "keep current", which is meaningful for a
		// movement and meaningless here: this command exists only to set a tag.
		return destinationStageResolution{}, ports.ErrInvalidReference
	}
	return resolution, nil
}

// resolveReclassifyLocation resolves the exact shed's display identity and fails closed when the
// shed id is not an active shed.
func (r *Repository) resolveReclassifyLocation(ctx context.Context, tx pgx.Tx, cmd ports.ReclassifyShedStageCommand) (oploc.OperationalLocation, error) {
	var location oploc.OperationalLocation
	if label := strings.TrimSpace(strptrValue(cmd.PartitionLabel)); label != "" {
		err := tx.QueryRow(ctx, `
SELECT exact.location_id::text, COALESCE(exact.name, ''), COALESCE(park.location_id::text, ''), COALESCE(park.name, '')
FROM shed_partitions sp
JOIN locations exact
  ON exact.tenant_id = sp.tenant_id
 AND exact.location_id = sp.operational_location_id
 AND exact.location_type = 'shed'
 AND exact.status = 'active'
LEFT JOIN locations park
  ON park.tenant_id = exact.tenant_id
 AND park.location_id = exact.parent_location_id
WHERE sp.tenant_id = $1::uuid
  AND sp.shed_id = $2::uuid
  AND sp.status = 'active'
  AND regexp_replace(lower(btrim(sp.partition_label)), '^part[[:space:]]+', '') =
      regexp_replace(lower(btrim($3::text)), '^part[[:space:]]+', '')
LIMIT 1
FOR SHARE OF sp, exact`,
			cmd.TenantID, cmd.ShedID, label).Scan(&location.ShedID, &location.ShedName, &location.ParkID, &location.ParkName)
		if errors.Is(err, pgx.ErrNoRows) {
			return oploc.OperationalLocation{}, ports.ErrInvalidReference
		}
		if err != nil {
			return oploc.OperationalLocation{}, fmt.Errorf("identity: reclassify shed stage: resolve partition shed: %w", err)
		}
		return location, nil
	}
	err := tx.QueryRow(ctx, `
SELECT shed.location_id::text, COALESCE(shed.name, ''), COALESCE(park.location_id::text, ''), COALESCE(park.name, '')
FROM locations shed
LEFT JOIN locations park ON park.tenant_id = shed.tenant_id AND park.location_id = shed.parent_location_id
WHERE shed.tenant_id = $1::uuid AND shed.location_id = $2::uuid
  AND shed.location_type = 'shed' AND shed.status = 'active'`,
		cmd.TenantID, cmd.ShedID).Scan(&location.ShedID, &location.ShedName, &location.ParkID, &location.ParkName)
	if errors.Is(err, pgx.ErrNoRows) {
		return oploc.OperationalLocation{}, ports.ErrInvalidReference
	}
	if err != nil {
		return oploc.OperationalLocation{}, fmt.Errorf("identity: reclassify shed stage: resolve shed: %w", err)
	}
	location.ShedID = cmd.ShedID

	return location, nil
}

func (r *Repository) countReclassifyScope(ctx context.Context, tx pgx.Tx, cmd ports.ReclassifyShedStageCommand) (int, error) {
	var total int
	err := tx.QueryRow(ctx, `SELECT count(*)`+reclassifyScopeSQL, cmd.TenantID, cmd.ShedID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("identity: reclassify shed stage: count scope: %w", err)
	}
	return total, nil
}

// insertReclassifyIdentityEvents locks the pen's animals and mints one goat.stage_changed identity
// event per animal WHOSE STAGE ACTUALLY CHANGES.
//
// The `IS DISTINCT FROM` predicate is the whole point: an animal already carrying the target tag and
// the tag's kid/adult band is left completely alone -- no event, no row_version bump -- so pressing
// the button twice does not republish stage-change events that would make vaccination re-evaluate
// animals nothing happened to. A same-stage stale age_band still changes: age_band is durable
// scheduling truth, not decoration. Rows are taken FOR UPDATE ordered by goat_id so two concurrent
// reclassifications of overlapping pens acquire locks in a deterministic order and cannot deadlock.
func (r *Repository) insertReclassifyIdentityEvents(
	ctx context.Context, tx pgx.Tx, cmd ports.ReclassifyShedStageCommand,
	resolution destinationStageResolution,
) ([]string, []string, error) {
	rows, err := tx.Query(ctx, `
WITH targets AS (
    SELECT g.goat_id, COALESCE(g.management_stage, '') AS from_stage, g.park_id, g.shed_id,
           $3::text AS legacy_partition_label
`+reclassifyScopeSQL+`
      AND (
        COALESCE(g.management_stage, '') IS DISTINCT FROM $4::text
        OR ($11::text <> '' AND COALESCE(g.age_band, '') IS DISTINCT FROM $11::text)
      )
    ORDER BY g.goat_id
    FOR UPDATE OF g
)
INSERT INTO goat_identity_events (
    identity_event_id, tenant_id, goat_id, event_type, event_version,
    occurred_at, recorded_at, actor_id, payload, idempotency_key, source_record_id
)
SELECT
    gen_random_uuid(), $1::uuid, t.goat_id, $5, 1, $6::timestamptz, $6::timestamptz, $7::uuid,
    jsonb_build_object(
        'goat_id', t.goat_id::text,
        'previous_management_stage', t.from_stage::text,
        'management_stage', $4::text,
        'reason', $8::text,
        'current_park_id', t.park_id::text,
        'current_shed_id', t.shed_id::text,
        'management_stage_source', $9::text,
        'scope_type', 'goat',
        'scope_id', t.goat_id::text
    ),
    $10::text || ':stage:' || t.goat_id::text,
    $10
FROM targets t
RETURNING goat_id::text, identity_event_id::text`,
		cmd.TenantID,              // $1
		cmd.ShedID,                // $2
		"",                        // $3 legacy partition placeholder
		resolution.stage,          // $4
		goatStageChangedEventType, // $5
		cmd.OccurredAt,            // $6
		cmd.ActorID,               // $7
		cmd.Reason,                // $8
		reclassifyStageSource,     // $9
		cmd.StoredIdempotencyKey,  // $10
		resolution.ageBand,        // $11
	)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: reclassify shed stage: record stage-change events: %w", err)
	}
	defer rows.Close()

	goatIDs := make([]string, 0, ports.MaxReclassifyGoatsPerCommand)
	eventIDs := make([]string, 0, ports.MaxReclassifyGoatsPerCommand)
	for rows.Next() {
		var goatID, eventID string
		if err := rows.Scan(&goatID, &eventID); err != nil {
			return nil, nil, fmt.Errorf("identity: reclassify shed stage: scan stage-change event: %w", err)
		}
		goatIDs = append(goatIDs, goatID)
		eventIDs = append(eventIDs, eventID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("identity: reclassify shed stage: record stage-change events: %w", err)
	}
	return goatIDs, eventIDs, nil
}

// applyReclassify writes the cohort tag and its kid/adult band onto the already-locked animals and
// enqueues the outbox row for each identity event minted above -- one statement, not a loop.
func (r *Repository) applyReclassify(
	ctx context.Context, tx pgx.Tx, cmd ports.ReclassifyShedStageCommand,
	resolution destinationStageResolution, location oploc.OperationalLocation,
	goatIDs, eventIDs []string,
) error {
	// NOT compute-on-read. One bounded set-based WRITE for one pen (<= MaxReclassifyGoatsPerCommand
	// animals): data-modifying CTEs executed once each, so the whole pen is written in ONE round
	// trip rather than N. It reconstructs no derived state from event tables.
	// scale-guard:ignore: bounded set-based transactional write, not a compute-on-read god-CTE.
	_, err := tx.Exec(ctx, `
WITH assigned AS (
    SELECT goat_id, event_id FROM unnest($2::uuid[], $3::uuid[]) AS a(goat_id, event_id)
),
targets AS (
    SELECT g.goat_id, g.farm_id, g.park_id, g.shed_id, COALESCE(g.management_stage, '') AS from_stage, a.event_id
    FROM goats g
    JOIN assigned a ON a.goat_id = g.goat_id
    WHERE g.tenant_id = $1::uuid
),
reclassified AS (
    UPDATE goats g
    SET management_stage = $4::text,
        -- Kid/adult follows the cohort tag it is a property of, from the SAME animal_stage_lookup
        -- row. Guarded on the band being classified ($5) so a deliberately unclassified tag leaves
        -- the animal's existing band untouched rather than blanking it.
        age_band    = CASE WHEN $5::text = '' THEN g.age_band ELSE $5::text END,
        updated_at  = $6::timestamptz,
        row_version = row_version + 1
    FROM targets t
    WHERE g.tenant_id = $1::uuid AND g.goat_id = t.goat_id
),
events AS (
    INSERT INTO outbox_messages (
        tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
        topic, payload, headers, idempotency_key, trace_id, status
    )
    SELECT
        $1::uuid, t.event_id, $7, $8, 'goat', t.goat_id, $9,
        jsonb_build_object(
            'event_id', t.event_id::text,
            'event_type', $7::text,
            'schema_version', $8::text,
            'schema_ref', $10::text,
            'aggregate_type', 'goat',
            'aggregate_id', t.goat_id::text,
            'occurred_at', to_char($6::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'recorded_at', to_char($6::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'producer', jsonb_build_object('service', 'goatos-api', 'module', 'identity', 'version', NULL),
            'idempotency_key', $11::text || ':stage:' || t.goat_id::text,
            'actor', jsonb_build_object('actor_type', 'human', 'actor_id', $12::text),
            'subject_type', 'goat',
            'subject_id', t.goat_id::text,
            'visibility_scope', jsonb_strip_nulls(jsonb_build_object(
                'tenant_id', $1::text,
                'farm_id', t.farm_id::text,
                'park_id', t.park_id::text,
                'shed_id', t.shed_id::text
            )),
            'evidence_refs', '[]'::jsonb,
            'trace_id', $13::text,
            'payload', jsonb_build_object(
                'goat_id', t.goat_id::text,
                'previous_management_stage', t.from_stage::text,
                'management_stage', $4::text,
                'age_band', $5::text,
                'reason', $14::text,
                'current_park_id', t.park_id::text,
                'current_shed_id', t.shed_id::text,
                'partition_label', nullif($15::text, ''),
                'operational_location_display', $16::text,
                'management_stage_source', $17::text,
                'scope_type', 'goat',
                'scope_id', t.goat_id::text
            )
        ),
        jsonb_build_object('actor_id', $12::text, 'trace_id', $13::text, 'source', $17::text),
        $11::text || ':stage:' || t.goat_id::text,
        nullif($13::text, ''),
        'pending'
    FROM targets t
)
SELECT 1`,
		cmd.TenantID,              // $1
		goatIDs,                   // $2
		eventIDs,                  // $3
		resolution.stage,          // $4
		resolution.ageBand,        // $5
		cmd.OccurredAt,            // $6
		goatStageChangedEventType, // $7
		eventSchemaVersion,        // $8
		goatLifecycleTopic,        // $9
		eventSchemaRef,            // $10
		cmd.StoredIdempotencyKey,  // $11
		cmd.ActorID,               // $12
		cmd.TraceID,               // $13
		cmd.Reason,                // $14
		location.PartitionLabel,   // $15
		location.Display(),        // $16
		reclassifyStageSource,     // $17
	)
	if err != nil {
		return fmt.Errorf("identity: reclassify shed stage: apply: %w", err)
	}
	return nil
}
