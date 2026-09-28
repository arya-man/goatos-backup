package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// sqlEndSeatsOfMember ends every seat a person still holds -- the vaccination operator seat, pen
// manager/backup seats, anything on workforce_positions -- as they leave. valid_to never moves
// earlier than the seat's own start (the window check), and a seat already due to end sooner keeps
// its earlier end.
const sqlEndSeatsOfMember = `
UPDATE workforce_positions
SET status = 'ended',
    valid_to = GREATEST(LEAST(COALESCE(valid_to, now()), now()), valid_from + interval '1 millisecond'),
    updated_at = now(),
    row_version = row_version + 1
WHERE tenant_id = $1::uuid
  AND workforce_member_id = $2::uuid
  AND status = 'active'
RETURNING position_id::text, position_code, scope_type, scope_id::text, row_version`

// endSeatsOfDeactivatedMember is what makes "inactive on People / HRMS" reach every module that
// reads the roster. Deactivation already revoked the person's logins, but their seats stayed active,
// and the vaccination operator roster, pen ownership and the drive operator pool all read seats --
// so a person who had left the park kept showing as a vaccination operator. The seats end in the
// SAME transaction as the status change, and every park where one of them was a vaccination
// operator seat gets vaccination.roster.changed so its future drives are re-planned without them.
// Reactivating a person does NOT restore seats: they are re-seated on the roster deliberately.
func endSeatsOfDeactivatedMember(ctx context.Context, tx pgx.Tx, tenantID, actorID, memberID string) error {
	rows, err := tx.Query(ctx, sqlEndSeatsOfMember, tenantID, memberID)
	if err != nil {
		return fmt.Errorf("workforce: end seats of deactivated member: %w", err)
	}
	type endedSeat struct {
		positionID, code, scopeType, scopeID string
		rowVersion                           int
	}
	var ended []endedSeat
	for rows.Next() {
		var seat endedSeat
		if err := rows.Scan(&seat.positionID, &seat.code, &seat.scopeType, &seat.scopeID, &seat.rowVersion); err != nil {
			rows.Close()
			return err
		}
		ended = append(ended, seat)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	parks := map[string]bool{}
	for _, seat := range ended {
		if !strings.HasPrefix(seat.code, "vaccination_operator_") || seat.scopeType != "center" || seat.scopeID == "" || parks[seat.scopeID] {
			continue
		}
		parks[seat.scopeID] = true
		key := fmt.Sprintf("workforce.member-deactivated.vaccination.roster.changed:%s:%s:%d", memberID, seat.positionID, seat.rowVersion)
		// scale-guard:ignore: one event per park the person held a vaccination seat in -- bounded by the farm's parks, never by herd size.
		if err := enqueueRosterChangedForPark(ctx, tx, tenantID, actorID, seat.scopeID, key); err != nil {
			return err
		}
	}
	return nil
}

// sqlEnqueueRosterChanged writes one park-scoped vaccination.roster.changed envelope. The literal
// event type in ON CONFLICT is what lets Postgres use outbox_messages_roster_changed_idempotency_idx.
const sqlEnqueueRosterChanged = `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES ($1::uuid, $2::uuid, 'vaccination.roster.changed', '1.0.0', 'park', $3::uuid,
  'vaccination.events', $4::jsonb, $5::jsonb, $6, $6, 'pending', now())
ON CONFLICT (tenant_id, idempotency_key) WHERE event_type = 'vaccination.roster.changed' DO NOTHING`

// enqueueRosterChangedForPark uses the envelope the roster seat-edit path and vaccinationexecution's
// UpsertOperatorAssignmentConfig already emit (park aggregate, payload {"park_id": ...}), so the
// obligation OperatorConfigReplanHandler consumes it identically. The idempotency key decides the
// event id: a retry of the same deactivation can never enqueue a second event.
func enqueueRosterChangedForPark(ctx context.Context, tx pgx.Tx, tenantID, actorID, parkID, idempotencyKey string) error {
	const eventType = "vaccination.roster.changed"
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000Z")
	eventID := platformoutbox.DeterministicUUID(idempotencyKey)
	envelope, err := json.Marshal(map[string]any{
		"event_id":        eventID,
		"event_type":      eventType,
		"schema_version":  "1.0.0",
		"schema_ref":      "domain-event-envelope.v1",
		"aggregate_type":  "park",
		"aggregate_id":    parkID,
		"occurred_at":     now,
		"recorded_at":     now,
		"producer":        map[string]any{"service": "goatos-api", "module": "workforce", "version": nil},
		"idempotency_key": idempotencyKey,
		"actor":           map[string]any{"actor_type": "human", "actor_id": actorID, "actor_ref": nil},
		"subject_type":    "location",
		"subject_id":      parkID,
		"visibility_scope": map[string]any{
			"tenant_id": tenantID,
			"park_id":   parkID,
		},
		"evidence_refs": []map[string]string{{"evidence_type": "location", "evidence_id": parkID}},
		"payload":       map[string]any{"park_id": parkID},
		"trace_id":      idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("workforce: marshal %s envelope: %w", eventType, err)
	}
	headers, err := json.Marshal(map[string]any{
		"producer":        "workforce.SetOperatorStatus",
		"schema_version":  "1.0.0",
		"park_id":         parkID,
		"idempotency_key": idempotencyKey,
	})
	if err != nil {
		return fmt.Errorf("workforce: marshal %s headers: %w", eventType, err)
	}
	if _, err := tx.Exec(ctx, sqlEnqueueRosterChanged, tenantID, eventID, parkID, envelope, headers, idempotencyKey); err != nil {
		return fmt.Errorf("workforce: enqueue %s to outbox: %w", eventType, err)
	}
	return nil
}
