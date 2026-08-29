package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const goatLocationHistoryReasonShiftingApproved = "counts_shifting_approved"

// RelocateGoatsToShedInTx moves an explicitly named set of animals into one destination shed inside
// a transaction the CALLER owns and commits.
//
// This is the "approving a shifting event MOVES THE ANIMALS" half of the Counts approval workflow.
// Flipping shifting_events.authorization_state alone would leave the canonical goat location stale,
// so the location write and the authorization flip must commit together -- which is why this takes
// the caller's tx rather than opening its own.
//
// Shape rules this path is built around:
//
//   - SET-BASED, never a loop. Two statements cover the whole group regardless of size: one that
//     locks the targets and writes their identity events, and one that moves them and writes the
//     history + outbox rows. Moving 200 animals is 2 round trips, not 200 (the n-plus-one-fanout
//     anti-pattern).
//
//   - THE CANONICAL IDENTITY EVENT IS WRITTEN FIRST, IN ITS OWN STATEMENT. Every goat-aggregate
//     outbox row must be backed by the goat_identity_events row it derives from: outbox_messages
//     carries a BEFORE INSERT trigger (outbox_messages_validate_event_tenant_trg) whose fallback
//     branch rejects any event_id with no matching goat_identity_events row for the tenant
//     (SQLSTATE 23503, "outbox event % does not exist for tenant %"). The identity event is also
//     the goat's own timeline record -- a move that never lands in goat_identity_events is
//     invisible on the animal's history even when the outbox insert is accepted.
//
//     The insert MUST be a separate, earlier statement rather than a sibling CTE. Postgres runs
//     every data-modifying CTE in one statement under the SAME snapshot and in an unspecified
//     order, so a goat_identity_events row written by a neighbouring CTE is not a dependable input
//     to the outbox trigger's lookup. Sequencing the statements is what makes the trigger see the
//     row, and it mirrors finishGoatLifecycleMutation, which likewise inserts the identity event
//     and then threads that same event id into InsertOutboxMessage.
//
//   - PER-ANIMAL events are still emitted. goat.location.changed is what re-scopes a goat's open
//     shed-scoped vaccination obligations to the new shed (obligation SM-2,
//     internal/obligation/app/shift.go) and what the vaccination generator listens to. Collapsing
//     the batch into one event would silently strand every moved animal's obligations at the old
//     shed. The events are produced by INSERT ... SELECT, so per-animal fidelity costs one
//     statement, not N.
//
//   - FAIL CLOSED on a shortfall. Only alive, unmerged animals are movable; the caller compares
//     MovedGoatIDs against the requested set and aborts the whole approval if they differ, so an
//     approval never half-applies.
func (r *Repository) RelocateGoatsToShedInTx(ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand) (ports.RelocateGoatsResult, error) {
	if len(cmd.GoatIDs) == 0 {
		return ports.RelocateGoatsResult{}, fmt.Errorf("identity: relocate goats: no goat ids supplied")
	}
	if len(cmd.GoatIDs) > ports.MaxRelocateGoatsPerCommand {
		return ports.RelocateGoatsResult{}, fmt.Errorf(
			"identity: relocate goats: %d animals exceeds the per-command cap of %d",
			len(cmd.GoatIDs), ports.MaxRelocateGoatsPerCommand)
	}
	// P0-1: Cross-park move prevention (defense in depth). Goats never move between parks.
	// P1 follow-up #1: Stale location overwrite prevention. If an expected source park is supplied,
	// fail closed when it disagrees with the destination.
	if cmd.FromParkID != nil && *cmd.FromParkID != cmd.ToParkID {
		return ports.RelocateGoatsResult{}, fmt.Errorf(
			"identity: relocate goats: cross-park movement forbidden (from: %s, to: %s)",
			*cmd.FromParkID, cmd.ToParkID)
	}
	// P0-3 (review judge): the supplied FromParkID is OPTIONAL and can be nil (a multi-animal or
	// not-derivable submit stores a NULL source park), so the check above alone lets a cross-park
	// move through on the nil path. Enforce the invariant against GROUND TRUTH instead: every
	// movable animal's CURRENT park must equal the destination park. This reads the same rows the
	// relocation is about to lock+move, so a mismatch cannot slip past regardless of what the
	// (optional) source hint said.
	if err := r.assertGoatsInPark(ctx, tx, cmd); err != nil {
		return ports.RelocateGoatsResult{}, err
	}
	if err := r.ensureShedUnderPark(ctx, cmd.TenantID, cmd.ToShedID, cmd.ToParkID); err != nil {
		return ports.RelocateGoatsResult{}, err
	}

	// The raise-time request owns the optional stage transition. Sheds may contain mixed management
	// stages, so neither residents nor shed_profiles are stage authority for a move.
	stageResolution, err := r.resolveDestinationTag(ctx, tx, cmd)
	if err != nil {
		return ports.RelocateGoatsResult{}, err
	}
	// The raise request snapshots the explicit target. An empty value means preserve each animal's
	// current stage; the destination shed and its residents never infer or override it.
	effectiveStage := stageResolution.stage
	// KID/ADULT RIDES ALONG WITH THE COHORT TAG (maintainer decision 2026-08-05). age_band is a
	// property of the stage, read from the tenant's stage vocabulary, so an animal that adopts an
	// adult cohort at the destination stops being a kid in the SAME write that moves it. Empty when
	// the stage is unclassified (clinical tags carry NULL age_band) -- then age_band is preserved
	// exactly like the stage itself.
	effectiveAgeBand := stageResolution.ageBand

	reason := cmd.Reason
	if reason == "" {
		reason = goatLocationHistoryReasonShiftingApproved
	}
	occurredAt := cmd.OccurredAt.UTC()

	assignedGoatIDs, assignedEventIDs, err := r.insertRelocationIdentityEvents(ctx, tx, cmd, reason, occurredAt)
	if err != nil {
		return ports.RelocateGoatsResult{}, err
	}
	// No movable animal matched. The caller fails closed on the shortfall; returning here keeps
	// this from writing history/outbox rows for an empty set.
	if len(assignedGoatIDs) == 0 {
		return ports.RelocateGoatsResult{}, nil
	}
	// CR-01: enforce the APPROVED per-goat source placement against ground truth, now that the target
	// rows are locked FOR UPDATE by insertRelocationIdentityEvents (the lock is held for the rest of
	// this transaction). FromShedID/FromParkID are the source captured on the shifting event at
	// approval time; passing them without enforcing them let this completion silently overwrite a
	// NEWER legitimate relocation. Concretely: an A->B movement is approved (FromShedID=A), the animal
	// is then legitimately relocated A->C, and completing the stale A->B movement would move the goat
	// C->B, clobbering the newer placement. Failing closed here leaves the movement authorized so a
	// human can reconcile it, rather than applying a stale move on top of current state.
	if err := r.assertGoatsAtExpectedSource(ctx, tx, cmd); err != nil {
		return ports.RelocateGoatsResult{}, err
	}

	// RECLASSIFICATION events. A goat that adopts a NEW cohort tag at the destination shed is
	// reclassified, so it must emit goat.stage_changed alongside goat.location.changed — feed and the
	// vaccination generator both key on management_stage, and without this a K1→K2 shift would leave
	// the animal fed and scheduled as K1 in a K2 shed. Only animals whose tag actually CHANGES get a
	// stage event (idempotent no-op for a within-cohort move). The identity events are inserted here,
	// in their own statement BEFORE applyRelocation enqueues the matching outbox rows, for exactly the
	// reason insertRelocationIdentityEvents documents: outbox_messages_validate_event_tenant_trg
	// requires the goat_identity_events row to already exist.
	var stageGoatIDs, stageEventIDs []string
	if effectiveStage != "" {
		stageGoatIDs, stageEventIDs, err = r.insertStageChangeIdentityEvents(ctx, tx, cmd, effectiveStage, reason, occurredAt, assignedGoatIDs)
		if err != nil {
			return ports.RelocateGoatsResult{}, err
		}
	}

	moved, err := r.applyRelocation(ctx, tx, cmd, reason, occurredAt, effectiveStage, effectiveAgeBand, assignedGoatIDs, assignedEventIDs, stageGoatIDs, stageEventIDs)
	if err != nil {
		return ports.RelocateGoatsResult{}, err
	}
	sort.Strings(moved)

	// OperationalLocation = park + physical shed + optional partition (backend/internal/platform/
	// oploc). goats.shed_id/park_id above always carries the PARENT physical shed; the partition half
	// lives here, in goat_shed_partitions, and must move atomically with the shed/park write -- in the
	// SAME transaction -- or a reader combining the two tables would observe an animal whose shed says
	// "moved" but whose partition still names its old location.
	if err := r.upsertGoatShedPartitionsInTx(ctx, tx, cmd, moved); err != nil {
		return ports.RelocateGoatsResult{}, err
	}
	// Re-derive vaccination_drive_assignment_members for whichever moved goats still have open,
	// BATCHED obligations. A cross-shed move is separately re-scoped by the async
	// goat.location.changed consumer (SM-2), which unbatches those obligations outright, so this call
	// is typically a no-op for that case; a SAME-shed partition-only move has no shed change to
	// trigger that async path at all, so this is the only place that keeps drive-assignment
	// membership (and its counters) correct for it. See
	// obligationpg.SyncDriveAssignmentMembershipForGoatsInTx.
	if err := obligationpg.SyncDriveAssignmentMembershipForGoatsInTx(ctx, tx, cmd.TenantID, moved); err != nil {
		return ports.RelocateGoatsResult{}, fmt.Errorf("identity: relocate goats: sync drive assignment membership: %w", err)
	}
	return ports.RelocateGoatsResult{MovedGoatIDs: moved}, nil
}

// upsertGoatShedPartitionsInTx writes the destination operational location's partition half for
// every moved goat: goat_shed_partitions.shed_id/partition_label/source_shed_name. PRIMARY KEY
// (tenant_id, goat_id) makes this a straightforward upsert -- every goat carries at most one
// partition row, mirroring its current shed_id at all times, whether or not that shed is actually
// partitioned (a non-partitioned destination writes the 'whole' sentinel, never a synthesized
// business label -- see oploc.WholeSentinel/Display).
//
// cmd.DestinationPartitionLabel is nil for a genuinely non-partitioned destination (or for an
// existing caller that has not been updated to supply it, which preserves today's behaviour: those
// callers already only ever moved goats into non-partitioned sheds). cmd.DestinationShedName, when
// supplied, avoids a redundant shed-name lookup the caller may already have; when blank this reads
// the name from `locations` so source_shed_name is a real display string and not the shed uuid.
func (r *Repository) upsertGoatShedPartitionsInTx(ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand, goatIDs []string) error {
	if len(goatIDs) == 0 {
		return nil
	}
	partitionLabel := oploc.WholeSentinel
	if cmd.DestinationPartitionLabel != nil {
		if trimmed := strings.TrimSpace(*cmd.DestinationPartitionLabel); trimmed != "" {
			partitionLabel = trimmed
		}
	}
	shedName := strings.TrimSpace(cmd.DestinationShedName)
	if shedName == "" {
		if err := tx.QueryRow(ctx, `SELECT name FROM locations WHERE tenant_id = $1::uuid AND location_id = $2::uuid`,
			cmd.TenantID, cmd.ToShedID).Scan(&shedName); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("identity: relocate goats: destination shed name: %w", err)
		}
	}
	sourceShedName := oploc.OperationalLocation{
		ParkID: cmd.ToParkID, ShedID: cmd.ToShedID, ShedName: shedName, PartitionLabel: partitionLabel,
	}.Display()
	if strings.TrimSpace(sourceShedName) == "" {
		// goat_shed_partitions_source_nonblank requires a non-blank value; a shed name lookup miss
		// (should not happen -- ensureShedUnderPark already verified the shed exists) still leaves the
		// write satisfying the constraint rather than failing the whole relocation on a cosmetic field.
		sourceShedName = cmd.ToShedID
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name, updated_at)
SELECT $1::uuid, g.goat_id, $2::uuid, $3, $4, now()
FROM unnest($5::uuid[]) AS g(goat_id)
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET
    shed_id = EXCLUDED.shed_id,
    partition_label = EXCLUDED.partition_label,
    source_shed_name = EXCLUDED.source_shed_name,
    updated_at = now()`,
		cmd.TenantID, cmd.ToShedID, partitionLabel, sourceShedName, goatIDs); err != nil {
		return fmt.Errorf("identity: relocate goats: upsert goat_shed_partitions: %w", err)
	}
	return nil
}

// destinationStageResolution is the canonical active stage selected when the shifting was raised.
// It deliberately carries no shed-profile provenance because destination sheds may be mixed-stage.
type destinationStageResolution struct {
	stage string
	// ageBand is the kid/adult classification the tenant's stage vocabulary attaches to that stage
	// ('kid', 'adult', or "" when the stage is deliberately unclassified). It is read here, from the
	// same animal_stage_lookup row that canonicalizes the stage code, so the two answers can never
	// disagree about the same tag.
	ageBand string
}

// resolveDestinationTag validates the explicit raise-time target. An empty target means preserve
// each goat's current management_stage (legacy and keep-current requests).
func (r *Repository) resolveDestinationTag(ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand) (destinationStageResolution, error) {
	stage := strings.TrimSpace(cmd.DestinationTag)
	if stage == "" {
		return destinationStageResolution{}, nil
	}
	var canonical, ageBand string
	err := tx.QueryRow(ctx, `SELECT stage_code, COALESCE(age_band, '') FROM animal_stage_lookup
WHERE tenant_id=$1::uuid AND status='active' AND lower(stage_code)=lower($2)
ORDER BY stage_code LIMIT 1`, cmd.TenantID, stage).Scan(&canonical, &ageBand)
	if errors.Is(err, pgx.ErrNoRows) {
		return destinationStageResolution{}, ports.ErrDestinationTagConflict
	}
	if err != nil {
		return destinationStageResolution{}, fmt.Errorf("identity: validate requested management stage: %w", err)
	}
	resolved := destinationStageResolution{stage: canonical, ageBand: ageBand}

	// Clinical fail-closed. Movement never invents clinical truth: a profile naming a clinical state
	// (sick/under_treatment/recovering/quarantine/icu -- protocol/domain.MandatoryClinicalDeferStates,
	// reused not re-hardcoded) is rejected. The animal's clinical state is set by its owning clinical
	// flow; the move then follows an already-diagnosed animal. See ports.ErrClinicalDestinationTag.
	//
	// THE ONE EXCEPTION (maintainer decision 2026-08-20): a HEALTH-type shifting sets
	// AllowClinicalDestinationTag -- moving an animal into the ICU pen IS the health team setting
	// her clinical state, and the typed raise resolver already gated which raises may carry the
	// flag. Every other caller keeps the refusal.
	if isClinicalDestinationStage(canonical) && !cmd.AllowClinicalDestinationTag {
		return destinationStageResolution{}, ports.ErrClinicalDestinationTag
	}
	return resolved, nil
}

// isClinicalDestinationStage reports whether a resolved destination tag names a clinical state -- the
// canonical protocol/domain.MandatoryClinicalDeferStates (sick, under_treatment, recovering,
// quarantine, icu), reused rather than re-hardcoded per the clinical-defer safety rule.
//
// The normalization and the comparison BOTH moved to protocol/domain (2026-08-15) so this guard and
// the counts raise-time resolver share one implementation. They used to answer the same question in
// two places: this one collapsed inner whitespace, the shifting resolver did not consult the set at
// all, so a pen tagged with a clinical state resolved cleanly at raise and then failed HERE -- at the
// second gate, after the operator had shot the completion video and the park head had approved.
func isClinicalDestinationStage(stage string) bool {
	return protocoldomain.IsClinicalManagementStage(stage)
}

// insertRelocationIdentityEvents takes the row locks and writes the canonical per-animal
// goat.location.changed identity events, returning the (goat_id, event_id) pairs it minted.
//
// targets is taken FOR UPDATE and ordered by goat_id so concurrent relocations of overlapping
// groups acquire row locks in a deterministic order and cannot deadlock against each other. Those
// locks are held for the rest of the caller's transaction, which is what lets applyRelocation
// re-derive the same rows without the movable set drifting underneath it. targets also snapshots
// the OLD location, which the event payload records and which the UPDATE later overwrites.
func (r *Repository) insertRelocationIdentityEvents(
	ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand, reason string, occurredAt time.Time,
) ([]string, []string, error) {
	const identityEventsSQL = `
WITH targets AS (
    SELECT goat_id, current_location_id AS from_location_id, park_id AS from_park_id, shed_id AS from_shed_id
    FROM goats
    WHERE tenant_id = $1::uuid
      AND goat_id = ANY($2::uuid[])
      AND merged_into_goat_id IS NULL
      AND exited_at IS NULL
    ORDER BY goat_id
    FOR UPDATE
)
INSERT INTO goat_identity_events (
    identity_event_id, tenant_id, goat_id, event_type, event_version,
    occurred_at, recorded_at, actor_id, payload, idempotency_key, source_record_id
)
SELECT
    gen_random_uuid(),
    $1::uuid,
    t.goat_id,
    $3,
    1,
    $4::timestamptz,
    $4::timestamptz,
    $5::uuid,
    jsonb_build_object(
        'goat_id', t.goat_id::text,
        'from_location_id', t.from_location_id::text,
        'from_park_id', t.from_park_id::text,
        'from_shed_id', t.from_shed_id::text,
        'to_park_id', $7::text,
        'to_shed_id', $8::text,
        'reason', $9::text,
        'scope_type', 'shed',
        'scope_id', $8::text
    ),
    $6::text || ':' || t.goat_id::text,
    $6
FROM targets t
RETURNING goat_id::text, identity_event_id::text`

	rows, err := tx.Query(ctx, identityEventsSQL,
		cmd.TenantID,                // $1
		cmd.GoatIDs,                 // $2
		goatMovedEventType,          // $3
		occurredAt,                  // $4
		cmd.ActorID,                 // $5
		cmd.OutboxIdempotencyPrefix, // $6
		cmd.ToParkID,                // $7
		cmd.ToShedID,                // $8
		reason,                      // $9
	)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: relocate goats: record identity events: %w", err)
	}
	defer rows.Close()

	goatIDs := make([]string, 0, len(cmd.GoatIDs))
	eventIDs := make([]string, 0, len(cmd.GoatIDs))
	for rows.Next() {
		var goatID, eventID string
		if err := rows.Scan(&goatID, &eventID); err != nil {
			return nil, nil, fmt.Errorf("identity: relocate goats: scan identity event: %w", err)
		}
		goatIDs = append(goatIDs, goatID)
		eventIDs = append(eventIDs, eventID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("identity: relocate goats: record identity events: %w", err)
	}
	return goatIDs, eventIDs, nil
}

// insertStageChangeIdentityEvents writes the canonical per-animal goat.stage_changed identity event
// for exactly the animals whose management_stage CHANGES to the destination cohort tag, returning the
// (goat_id, event_id) pairs it minted. Animals already carrying the destination tag (a within-cohort
// move) are not reclassified and get no stage event.
//
// The animals are already locked FOR UPDATE by insertRelocationIdentityEvents, so the read of the OLD
// management_stage here is stable, and this runs BEFORE applyRelocation enqueues the matching outbox
// rows so outbox_messages_validate_event_tenant_trg finds the identity event it requires. The
// idempotency key namespaces the stage event under the SAME per-completion prefix
// ("<prefix>:stage:<goat_id>") so it can never collide with the location event's key.
func (r *Repository) insertStageChangeIdentityEvents(
	ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand, effectiveStage, reason string, occurredAt time.Time,
	assignedGoatIDs []string,
) ([]string, []string, error) {
	const stageEventsSQL = `
WITH targets AS (
    SELECT goat_id, COALESCE(management_stage, '') AS from_stage,
           current_location_id AS from_location_id, park_id AS from_park_id, shed_id AS from_shed_id
    FROM goats
    WHERE tenant_id = $1::uuid
      AND goat_id = ANY($2::uuid[])
      AND merged_into_goat_id IS NULL
      AND exited_at IS NULL
      AND COALESCE(management_stage, '') IS DISTINCT FROM $3::text
)
INSERT INTO goat_identity_events (
    identity_event_id, tenant_id, goat_id, event_type, event_version,
    occurred_at, recorded_at, actor_id, payload, idempotency_key, source_record_id
)
SELECT
    gen_random_uuid(),
    $1::uuid,
    t.goat_id,
    $4,
    1,
    $5::timestamptz,
    $5::timestamptz,
    $6::uuid,
    jsonb_build_object(
        'goat_id', t.goat_id::text,
        'previous_management_stage', t.from_stage::text,
        'management_stage', $3::text,
        'reason', $7::text,
        'current_park_id', $8::text,
        'current_shed_id', $9::text,
        'management_stage_source', 'shifting_raise_request',
        'scope_type', 'goat',
        'scope_id', t.goat_id::text
    ),
    $10::text || ':stage:' || t.goat_id::text,
    $10
FROM targets t
RETURNING goat_id::text, identity_event_id::text`

	rows, err := tx.Query(ctx, stageEventsSQL,
		cmd.TenantID,                // $1
		assignedGoatIDs,             // $2 (only the locked, movable set)
		effectiveStage,              // $3
		goatStageChangedEventType,   // $4
		occurredAt,                  // $5
		cmd.ActorID,                 // $6
		reason,                      // $7
		cmd.ToParkID,                // $8
		cmd.ToShedID,                // $9
		cmd.OutboxIdempotencyPrefix, // $10
	)
	if err != nil {
		return nil, nil, fmt.Errorf("identity: relocate goats: record stage-change events: %w", err)
	}
	defer rows.Close()

	goatIDs := make([]string, 0, len(assignedGoatIDs))
	eventIDs := make([]string, 0, len(assignedGoatIDs))
	for rows.Next() {
		var goatID, eventID string
		if err := rows.Scan(&goatID, &eventID); err != nil {
			return nil, nil, fmt.Errorf("identity: relocate goats: scan stage-change event: %w", err)
		}
		goatIDs = append(goatIDs, goatID)
		eventIDs = append(eventIDs, eventID)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("identity: relocate goats: record stage-change events: %w", err)
	}
	return goatIDs, eventIDs, nil
}

// applyRelocation moves the already-locked animals, records their location history, and enqueues
// the outbox row for each identity event minted by insertRelocationIdentityEvents.
//
// assigned carries the (goat_id, event_id) pairs forward, so the outbox row for an animal reuses
// the id of the identity event that already exists in this transaction -- the threading that
// finishGoatLifecycleMutation does with a single eventUUID, done set-wise for the group.
//
// Every data-modifying CTE reads from `identified`, so the UPDATE, the history rows, and the events
// cover exactly the same animals by construction -- they cannot drift apart. The rows are already
// locked FOR UPDATE by the first statement, so re-deriving them here is stable.
// It also, in the SAME statement, writes the destination cohort tag onto every moved animal
// (management_stage = $16), the kid/adult band that tag carries (age_band = $19), and enqueues the
// goat.stage_changed outbox row for the reclassified subset (the (goat_id, stage_event_id) pairs
// from insertStageChangeIdentityEvents). Shed, tag and band move together atomically: a goat can
// never land in the destination shed still carrying its old cohort, nor sit in an adult cohort
// still counted as a kid.
func (r *Repository) applyRelocation(
	ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand, reason string, occurredAt time.Time,
	effectiveStage, effectiveAgeBand string, assignedGoatIDs, assignedEventIDs, stageGoatIDs, stageEventIDs []string,
) ([]string, error) {
	// NOT compute-on-read. This is a single set-based WRITE for one bounded shifting completion
	// (<= MaxRelocateGoatsPerCommand animals): the CTEs are data-modifying (bulk UPDATE of
	// already-locked rows + INSERT of location history and the location/stage outbox rows), each
	// executed exactly once, so the whole group moves in ONE round trip rather than N. It
	// reconstructs no derived state from event tables; adding the stage-reclassification sibling CTEs
	// to the pre-existing 6-CTE relocation only preserves that one-statement shape.
	// scale-guard:ignore: bounded set-based transactional write, not a compute-on-read god-CTE.
	const relocateSQL = `
WITH assigned AS (
    SELECT goat_id, event_id
    FROM unnest($12::uuid[], $13::uuid[]) AS a(goat_id, event_id)
),
stage_assigned AS (
    SELECT goat_id, event_id
    FROM unnest($17::uuid[], $18::uuid[]) AS a(goat_id, event_id)
),
targets AS (
    SELECT g.goat_id, g.current_location_id AS from_location_id, g.park_id AS from_park_id,
           g.shed_id AS from_shed_id, gsp.partition_label AS from_partition_label,
           g.farm_id, COALESCE(g.management_stage, '') AS from_stage
    FROM goats g
    LEFT JOIN goat_shed_partitions gsp
      ON gsp.tenant_id = g.tenant_id
     AND gsp.goat_id = g.goat_id
    WHERE g.tenant_id = $1::uuid
      AND g.goat_id = ANY($12::uuid[])
      AND g.merged_into_goat_id IS NULL
      AND g.exited_at IS NULL
),
identified AS (
    SELECT t.*, a.event_id
    FROM targets t
    JOIN assigned a ON a.goat_id = t.goat_id
),
identified_stage AS (
    SELECT t.goat_id, t.from_park_id, t.from_shed_id, t.from_stage, t.farm_id, sa.event_id
    FROM targets t
    JOIN stage_assigned sa ON sa.goat_id = t.goat_id
),
moved AS (
    UPDATE goats g
    SET current_location_id = $2::uuid,
        park_id             = $3::uuid,
        shed_id             = $2::uuid,
        management_stage    = CASE WHEN $16::text = '' THEN g.management_stage ELSE $16::text END,
        -- Kid/adult follows the cohort tag it is a property of. Guarded on the SAME "is there a
        -- destination tag" condition ($16) as management_stage, and additionally on the band being
        -- classified ($20), so a keep-current move and an unclassified/clinical tag both leave the
        -- animal's existing band untouched rather than blanking it.
        age_band            = CASE WHEN $16::text = '' OR $20::text = '' THEN g.age_band ELSE $20::text END,
        -- Stamping a clinical KID pen tag (ICU-Kid, Quarantine kids -- writable since 000167)
        -- OVERWRITES the milk band the animal was on, and a kid in ICU still drinks milk. Save the
        -- band here, in the same statement that destroys it, so it can never be lost: there is no
        -- window where management_stage says ICU-Kid and nothing remembers the animal was a K2.
        -- Milk Preparation reads milk_cohort as its fallback band (000166).
        --
        -- The reverse leg clears it: a move back onto a real milk band means the band is live in
        -- management_stage again, and a stale milk_cohort would outlive its own truth. Every other
        -- destination -- keep-current, or a weaned/adult cohort -- leaves the column untouched,
        -- because moving a K3 kid to F2-Male takes it OFF milk rather than hiding its band.
        milk_cohort         = CASE
            WHEN $16::text = '' THEN g.milk_cohort
            WHEN $16::text IN ('K1', 'K2', 'K3') THEN NULL
            WHEN upper(regexp_replace(btrim($16::text), '[^A-Za-z0-9]+', '', 'g'))
                 IN ('ICUKID', 'QUARANTINEKIDS', 'QUARANTINEKID', 'QUARANTINEMILKKID')
                 AND g.management_stage IN ('K1', 'K2', 'K3')
                THEN g.management_stage
            ELSE g.milk_cohort
        END,
        -- K3 is a SEVEN DAY weaning window (000168), and this is where its clock starts: an animal
        -- shifted INTO K3 draws milk for 7 days from today, then stops.
        --
        -- Guarded on the animal not ALREADY being K3, so a within-K3 move (a partition change, a
        -- move to another K3 pen) does not restart a week the animal is halfway through. Leaving K3
        -- clears it, so a later return starts a fresh week instead of inheriting a spent one.
        --
        -- The date is the business day in Asia/Kolkata, not the UTC calendar day: a feed day is a
        -- farm day, and an 02:00 IST move belongs to that day rather than the one before.
        k3_milk_started_on  = CASE
            WHEN $16::text = '' THEN g.k3_milk_started_on
            WHEN $16::text = 'K3' AND g.management_stage IS DISTINCT FROM 'K3'
                THEN ($4::timestamptz AT TIME ZONE 'Asia/Kolkata')::date
            WHEN $16::text = 'K3' THEN g.k3_milk_started_on
            ELSE NULL
        END,
        updated_at          = $4::timestamptz,
        row_version         = row_version + 1
    FROM identified i
    WHERE g.tenant_id = $1::uuid AND g.goat_id = i.goat_id
    RETURNING g.goat_id
),
history AS (
    INSERT INTO goat_location_history (
        tenant_id, goat_id, from_location_id, from_partition_label, to_location_id, to_partition_label,
        reason, occurred_at, actor_id, source_record_id
    )
    SELECT $1::uuid, i.goat_id, i.from_location_id, nullif(i.from_partition_label, ''),
           $2::uuid, nullif($21::text, ''), $5, $4::timestamptz, $6::uuid, $7
    FROM identified i
),
events AS (
    INSERT INTO outbox_messages (
        tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
        topic, payload, headers, idempotency_key, trace_id, status
    )
    SELECT
        $1::uuid,
        i.event_id,
        $14,
        $8,
        'goat',
        i.goat_id,
        $9,
        jsonb_build_object(
            'event_id', i.event_id::text,
            'event_type', $14::text,
            'schema_version', $8::text,
            'schema_ref', $10::text,
            'aggregate_type', 'goat',
            'aggregate_id', i.goat_id::text,
            'occurred_at', to_char($4::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'recorded_at', to_char($4::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'producer', jsonb_build_object('service', 'goatos-api', 'module', 'identity', 'version', NULL),
            'idempotency_key', $11::text || ':' || i.goat_id::text,
            'actor', jsonb_build_object('actor_type', 'human', 'actor_id', $6::text),
            'subject_type', 'goat',
            'subject_id', i.goat_id::text,
            'visibility_scope', jsonb_strip_nulls(jsonb_build_object(
                'tenant_id', $1::text,
                'farm_id', i.farm_id::text,
                'park_id', $3::text,
                'shed_id', $2::text
            )),
            'evidence_refs', '[]'::jsonb,
            'trace_id', $15::text,
            -- The payload the obligation re-scope handler (ShiftPayload) reads: it must name the
            -- destination shed as the goat's new scope, or open obligations stay at the old shed.
            'payload', jsonb_build_object(
                'goat_id', i.goat_id::text,
                'from_location_id', i.from_location_id::text,
                'from_park_id', i.from_park_id::text,
                'from_shed_id', i.from_shed_id::text,
                'from_partition_label', i.from_partition_label,
                'to_park_id', $3::text,
                'to_shed_id', $2::text,
                'to_partition_label', nullif($21::text, ''),
                'reason', $5::text,
                'scope_type', 'shed',
                'scope_id', $2::text
            )
        ),
        jsonb_build_object('actor_id', $6::text, 'trace_id', $15::text, 'source', 'counts.shifting_approval'),
        $11::text || ':' || i.goat_id::text,
        nullif($15::text, ''),
        'pending'
    FROM identified i
),
events_stage AS (
    INSERT INTO outbox_messages (
        tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
        topic, payload, headers, idempotency_key, trace_id, status
    )
    SELECT
        $1::uuid,
        s.event_id,
        $19,
        $8,
        'goat',
        s.goat_id,
        $9,
        jsonb_build_object(
            'event_id', s.event_id::text,
            'event_type', $19::text,
            'schema_version', $8::text,
            'schema_ref', $10::text,
            'aggregate_type', 'goat',
            'aggregate_id', s.goat_id::text,
            'occurred_at', to_char($4::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'recorded_at', to_char($4::timestamptz AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'),
            'producer', jsonb_build_object('service', 'goatos-api', 'module', 'identity', 'version', NULL),
            'idempotency_key', $11::text || ':stage:' || s.goat_id::text,
            'actor', jsonb_build_object('actor_type', 'human', 'actor_id', $6::text),
            'subject_type', 'goat',
            'subject_id', s.goat_id::text,
            'visibility_scope', jsonb_strip_nulls(jsonb_build_object(
                'tenant_id', $1::text,
                'farm_id', s.farm_id::text,
                'park_id', $3::text,
                'shed_id', $2::text
            )),
            'evidence_refs', '[]'::jsonb,
            'trace_id', $15::text,
            -- Reclassification payload. The vaccination generator re-reads the goat by id, but the
            -- previous/new cohort make the event self-describing for feed and audit consumers.
            'payload', jsonb_build_object(
                'goat_id', s.goat_id::text,
                'previous_management_stage', s.from_stage::text,
                'management_stage', $16::text,
                'reason', $5::text,
                'current_park_id', $3::text,
                'current_shed_id', $2::text,
                'management_stage_source', 'shifting_raise_request',
                'scope_type', 'goat',
                'scope_id', s.goat_id::text
            )
        ),
        jsonb_build_object('actor_id', $6::text, 'trace_id', $15::text, 'source', 'counts.shifting_completion'),
        $11::text || ':stage:' || s.goat_id::text,
        nullif($15::text, ''),
        'pending'
    FROM identified_stage s
)
SELECT goat_id::text FROM moved`

	rows, err := tx.Query(ctx, relocateSQL,
		cmd.TenantID,                // $1
		cmd.ToShedID,                // $2
		cmd.ToParkID,                // $3
		occurredAt,                  // $4
		reason,                      // $5
		cmd.ActorID,                 // $6
		cmd.OutboxIdempotencyPrefix, // $7 (goat_location_history.source_record_id)
		eventSchemaVersion,          // $8
		goatLifecycleTopic,          // $9
		eventSchemaRef,              // $10
		cmd.OutboxIdempotencyPrefix, // $11
		assignedGoatIDs,             // $12
		assignedEventIDs,            // $13
		goatMovedEventType,          // $14
		cmd.TraceID,                 // $15
		effectiveStage,              // $16 (destination cohort tag written onto every moved animal)
		stageGoatIDs,                // $17 (reclassified subset only)
		stageEventIDs,               // $18
		goatStageChangedEventType,   // $19
		effectiveAgeBand,            // $20 (kid/adult band carried by that cohort tag; "" = leave as-is)
		stringValue(cmd.DestinationPartitionLabel), // $21
	)
	if err != nil {
		return nil, fmt.Errorf("identity: relocate goats: %w", err)
	}
	defer rows.Close()

	moved := make([]string, 0, len(assignedGoatIDs))
	for rows.Next() {
		var goatID string
		if err := rows.Scan(&goatID); err != nil {
			return nil, fmt.Errorf("identity: relocate goats: scan moved goat: %w", err)
		}
		moved = append(moved, goatID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: relocate goats: %w", err)
	}
	return moved, nil
}
