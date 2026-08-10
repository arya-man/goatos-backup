package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/audit"
	platformoutbox "github.com/vgoats/goatos/backend/internal/platform/outbox"
)

// feed.direction.completed producer. This file is the module's ONLY write path: it records one
// shed-session completion and, in the SAME transaction, writes the audit row and enqueues the
// feed.direction.completed domain event to the transactional outbox. It is registered in
// context/architecture/domain-event-registry.json; the literal event type below plus the
// outbox_messages INSERT are what the domain-event-architecture guard matches against.
const (
	feedDirectionCompletedEventType     = "feed.direction.completed"
	feedDirectionCompletedSchemaVersion = "1.0.0"
	feedDirectionCompletedSchemaRef     = "domain-event-envelope.v1"
	feedDirectionCompletedTopic         = "feed.events"
	feedDirectionCompletedAggregateType = "feed_direction_session_completion"
	feedCompletionIdemScope             = "feed.direction.complete"
	feedCompletionResourceType          = "feed_direction_session_completion"
)

var _ ports.CompletionStore = (*Repository)(nil)

// CompleteSession records that ONE shed-session was carried out. Canonical write + audit + outbox +
// idempotency reservation are one transaction; idempotent on both the request key and the
// shed-session natural key.
func (r *Repository) CompleteSession(ctx context.Context, p ports.CompleteSessionParams) (ports.CompleteSessionResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	proofRefs := normalizeProofRefs(p.ProofRefs)
	proofJSON, err := json.Marshal(proofRefs)
	if err != nil {
		return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: marshal proof refs: %w", err)
	}
	targetDate := p.TargetDate.Format("2006-01-02")

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: begin completion tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	// Shed must be an active shed of the addressed park. Fail closed rather than record a completion
	// against a shed that does not belong to the park being fed.
	if err := requireShedInPark(ctx, tx, p.TenantID, p.ParkID, p.ShedID); err != nil {
		return ports.CompleteSessionResult{}, err
	}

	fingerprint := requestFingerprint(
		p.ParkID,
		p.ShedID,
		fmt.Sprintf("%d", p.SessionNo),
		targetDate,
		p.Workflow,
		proofFingerprint(proofRefs),
	)
	reservation, err := reserveIdempotency(ctx, tx, p.TenantID, feedCompletionIdemScope, p.IdempotencyKey, fingerprint)
	if err != nil {
		return ports.CompleteSessionResult{}, err
	}
	if !reservation.proceed {
		// Exact replay of the same request: return the original result, run NO side effects.
		if err := tx.Commit(ctx); err != nil {
			return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: commit idempotent replay: %w", err)
		}
		committed = true
		return ports.CompleteSessionResult{CompletionID: reservation.resultID, Applied: false, Status: "completed"}, nil
	}

	var completionID string
	inserted := true
	err = tx.QueryRow(ctx, `
INSERT INTO feed_direction_session_completions (
  tenant_id, park_id, shed_id, session_no, target_date, workflow, status, proof_refs, completed_by, idempotency_key
) VALUES (
  $1::uuid, $2::uuid, $3::uuid, $4, $5::date, $6, 'completed', $7::jsonb, nullif($8::text, '')::uuid, $9
)
ON CONFLICT (tenant_id, park_id, shed_id, session_no, target_date, workflow) DO NOTHING
RETURNING completion_id::text`,
		p.TenantID, p.ParkID, p.ShedID, p.SessionNo, targetDate,
		p.Workflow, proofJSON, p.CompletedBy, p.IdempotencyKey).Scan(&completionID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Natural-key conflict: the shed-session was already completed by a PRIOR request (different
		// key). This new key produced no completion, so no new event or audit row -- return the
		// existing completion id and mark not-applied.
		inserted = false
		if err := tx.QueryRow(ctx, `
SELECT completion_id::text
FROM feed_direction_session_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND shed_id = $3::uuid
  AND session_no = $4 AND target_date = $5::date AND workflow = $6`,
			p.TenantID, p.ParkID, p.ShedID, p.SessionNo, targetDate, p.Workflow).Scan(&completionID); err != nil {
			return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: read existing completion: %w", err)
		}
	} else if err != nil {
		return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: insert completion: %w", err)
	}

	if inserted {
		if err := writeCompletionAudit(ctx, tx, p, completionID); err != nil {
			return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: write completion audit: %w", err)
		}
		if err := insertFeedDirectionCompletedOutbox(ctx, tx, p, proofRefs, completionID, targetDate); err != nil {
			return ports.CompleteSessionResult{}, err
		}
	}

	if err := completeIdempotency(ctx, tx, p.TenantID, feedCompletionIdemScope, p.IdempotencyKey, feedCompletionResourceType, completionID); err != nil {
		return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: complete idempotency: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ports.CompleteSessionResult{}, fmt.Errorf("feeddirection: commit completion: %w", err)
	}
	committed = true
	return ports.CompleteSessionResult{CompletionID: completionID, Applied: inserted, Status: "completed"}, nil
}

// ListCompletedSessions returns every completed (shed, session, workflow) for one park-day in one
// bounded indexed read -- the serving-read overlay. A DEDICATED read, NOT the config snapshot, so the
// module's read-count invariant is preserved.
func (r *Repository) ListCompletedSessions(ctx context.Context, tenantID, parkID string, targetDate time.Time) ([]ports.CompletedSession, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// scale-guard:ignore: bounded read of ONE park-day's completed shed-sessions, covered by feed_direction_session_completions_serving_idx (tenant_id, park_id, target_date, workflow). Bounded by the park's shed catalog x sessions (physical infrastructure), never by herd size; binds are cast, indexed columns stay bare.
	rows, err := r.pool.Query(ctx, `
SELECT shed_id::text, session_no, workflow
FROM feed_direction_session_completions
WHERE tenant_id = $1::uuid AND park_id = $2::uuid AND target_date = $3::date AND status = 'completed'`,
		tenantID, parkID, targetDate.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("feeddirection: list completed sessions: %w", err)
	}
	defer rows.Close()
	out := make([]ports.CompletedSession, 0)
	for rows.Next() {
		var c ports.CompletedSession
		if err := rows.Scan(&c.ShedID, &c.SessionNo, &c.Workflow); err != nil {
			return nil, fmt.Errorf("feeddirection: scan completed session: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// requireShedInPark asserts the shed is an active shed under the addressed park in the caller's
// tenant, fail-closed with ports.ErrShedNotInPark.
func requireShedInPark(ctx context.Context, tx pgx.Tx, tenantID, parkID, shedID string) error {
	var ok bool
	err := tx.QueryRow(ctx, `
SELECT true
FROM locations
WHERE tenant_id = $1::uuid AND location_id = $2::uuid
  AND parent_location_id = $3::uuid AND location_type = 'shed' AND status = 'active'`,
		tenantID, shedID, parkID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.ErrShedNotInPark
	}
	if err != nil {
		return fmt.Errorf("feeddirection: resolve shed: %w", err)
	}
	return nil
}

// requireShedPartitionInPark asserts the physical shed belongs to the park and that the requested
// partition identity matches the catalog. Partitioned sheds fail closed on blank labels; undivided
// sheds fail closed on fabricated labels.
func requireShedPartitionInPark(ctx context.Context, tx pgx.Tx, tenantID, parkID, shedID, partitionLabel string) error {
	if err := requireShedInPark(ctx, tx, tenantID, parkID, shedID); err != nil {
		return err
	}
	normalizedRequested := domain.PartitionMatchKey(partitionLabel)
	rows, err := tx.Query(ctx, `
SELECT COALESCE(NULLIF(BTRIM(partition_label), ''), 'whole')
FROM shed_partitions
WHERE tenant_id = $1::uuid
  AND shed_id = $2::uuid
  AND status = 'active'
  AND COALESCE(NULLIF(BTRIM(partition_label), ''), 'whole') <> 'whole'`,
		tenantID, shedID)
	if err != nil {
		return fmt.Errorf("feeddirection: resolve shed partitions: %w", err)
	}
	defer rows.Close()

	hasPartitions := false
	matches := false
	for rows.Next() {
		hasPartitions = true
		var catalogLabel string
		if err := rows.Scan(&catalogLabel); err != nil {
			return fmt.Errorf("feeddirection: scan shed partition: %w", err)
		}
		if domain.PartitionMatchKey(catalogLabel) == normalizedRequested {
			matches = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("feeddirection: iterate shed partitions: %w", err)
	}
	if hasPartitions {
		if normalizedRequested == "whole" || !matches {
			return ports.ErrInvalidPartition
		}
		return nil
	}
	if normalizedRequested != "whole" {
		return ports.ErrInvalidPartition
	}
	return nil
}

func writeCompletionAudit(ctx context.Context, tx pgx.Tx, p ports.CompleteSessionParams, completionID string) error {
	actorType := strings.TrimSpace(p.ActorType)
	if actorType == "" {
		actorType = "operator"
	}
	return audit.NewTxRecorder(tx).Record(ctx, audit.Event{
		TenantID:     p.TenantID,
		ActorID:      p.ActorID,
		ActorType:    actorType,
		Action:       feedDirectionCompletedEventType,
		ResourceType: feedCompletionResourceType,
		ResourceID:   completionID,
		ScopeType:    "shed",
		ScopeID:      p.ShedID,
		AfterState: map[string]any{
			"park_id":     p.ParkID,
			"shed_id":     p.ShedID,
			"session_no":  p.SessionNo,
			"target_date": p.TargetDate.Format("2006-01-02"),
			"workflow":    p.Workflow,
			"proof_count": len(p.ProofRefs),
		},
		Metadata: map[string]any{"source": "feed-direction-completion"},
		TraceID:  p.TraceID,
	})
}

func insertFeedDirectionCompletedOutbox(ctx context.Context, tx pgx.Tx, p ports.CompleteSessionParams, proofRefs []domain.ProofRef, completionID, targetDate string) error {
	idempotencyKey := feedDirectionCompletedEventType + ":" + completionID
	eventID := platformoutbox.DeterministicUUID(feedDirectionCompletedEventType + ":" + p.TenantID + ":" + completionID)

	proofIDs := make([]string, 0, len(proofRefs))
	for _, ref := range proofRefs {
		if ref.ProofID != "" {
			proofIDs = append(proofIDs, ref.ProofID)
		}
	}
	payload := map[string]any{
		"completion_id": completionID,
		"park_id":       p.ParkID,
		"shed_id":       p.ShedID,
		"session_no":    p.SessionNo,
		"target_date":   targetDate,
		"workflow":      p.Workflow,
		"proof_ids":     proofIDs,
	}
	// This path is INERT (the pre-gate instant completion, no longer wired or routed), but it is
	// corrected alongside its two live siblings: leaving one broken copy behind is how the next
	// author learns the wrong shape from the codebase.
	envelope := feedEventEnvelope{
		EventID:        eventID,
		EventType:      feedDirectionCompletedEventType,
		SchemaVersion:  feedDirectionCompletedSchemaVersion,
		SchemaRef:      feedDirectionCompletedSchemaRef,
		AggregateType:  feedDirectionCompletedAggregateType,
		AggregateID:    completionID,
		IdempotencyKey: idempotencyKey,
		TenantID:       p.TenantID,
		ParkID:         p.ParkID,
		ShedID:         p.ShedID,
		ActorID:        p.ActorID,
		OccurredAt:     businessInstant(targetDate),
		Payload:        payload,
		TraceID:        p.TraceID,
	}.build()
	envelopeJSON, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("feeddirection: marshal outbox envelope: %w", err)
	}
	headersJSON, err := json.Marshal(map[string]any{"content_type": "application/json"})
	if err != nil {
		return fmt.Errorf("feeddirection: marshal outbox headers: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO outbox_messages (
  tenant_id, event_id, event_type, schema_version, aggregate_type, aggregate_id,
  topic, payload, headers, idempotency_key, trace_id, status, next_attempt_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $6::uuid,
  $7, $8::jsonb, $9::jsonb, $10, $11, 'pending', now()
)
ON CONFLICT DO NOTHING`,
		p.TenantID, eventID, feedDirectionCompletedEventType, feedDirectionCompletedSchemaVersion,
		feedDirectionCompletedAggregateType, completionID, feedDirectionCompletedTopic,
		envelopeJSON, headersJSON, idempotencyKey, p.TraceID)
	if err != nil {
		return fmt.Errorf("feeddirection: insert outbox: %w", err)
	}
	return nil
}

func normalizeProofRefs(refs []domain.ProofRef) []domain.ProofRef {
	if refs == nil {
		return []domain.ProofRef{}
	}
	return refs
}

// proofFingerprint is a stable, order-independent digest of the attached proof ids for the request
// fingerprint, so a same-key replay carrying a different proof set is flagged as a conflict.
func proofFingerprint(refs []domain.ProofRef) string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, ref.ProofID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}
