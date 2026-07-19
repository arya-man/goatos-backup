package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
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
	if err := r.ensureShedUnderPark(ctx, cmd.TenantID, cmd.ToShedID, cmd.ToParkID); err != nil {
		return ports.RelocateGoatsResult{}, err
	}

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

	moved, err := r.applyRelocation(ctx, tx, cmd, reason, occurredAt, assignedGoatIDs, assignedEventIDs)
	if err != nil {
		return ports.RelocateGoatsResult{}, err
	}
	sort.Strings(moved)
	return ports.RelocateGoatsResult{MovedGoatIDs: moved}, nil
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
func (r *Repository) applyRelocation(
	ctx context.Context, tx pgx.Tx, cmd ports.RelocateGoatsCommand, reason string, occurredAt time.Time,
	assignedGoatIDs, assignedEventIDs []string,
) ([]string, error) {
	const relocateSQL = `
WITH assigned AS (
    SELECT goat_id, event_id
    FROM unnest($12::uuid[], $13::uuid[]) AS a(goat_id, event_id)
),
targets AS (
    SELECT g.goat_id, g.current_location_id AS from_location_id, g.park_id AS from_park_id,
           g.shed_id AS from_shed_id, g.farm_id
    FROM goats g
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
moved AS (
    UPDATE goats g
    SET current_location_id = $2::uuid,
        park_id             = $3::uuid,
        shed_id             = $2::uuid,
        updated_at          = $4::timestamptz,
        row_version         = row_version + 1
    FROM identified i
    WHERE g.tenant_id = $1::uuid AND g.goat_id = i.goat_id
    RETURNING g.goat_id
),
history AS (
    INSERT INTO goat_location_history (
        tenant_id, goat_id, from_location_id, to_location_id, reason, occurred_at, actor_id, source_record_id
    )
    SELECT $1::uuid, i.goat_id, i.from_location_id, $2::uuid, $5, $4::timestamptz, $6::uuid, $7
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
                'to_park_id', $3::text,
                'to_shed_id', $2::text,
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
