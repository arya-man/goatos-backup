package app

import (
	"context"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

// MaxReviewEventBatch caps one POST /verification/review-events request. The browser flushes small
// batches periodically and on unload -- there is no legitimate reason for one flush to carry more
// than a few dozen interaction events, and an unbounded batch is an unbounded single INSERT.
const MaxReviewEventBatch = 200

// ReviewEventBatchInput is one raw event from the wire, before item-ownership authorization.
type ReviewEventBatchInput struct {
	ItemID        string
	ProofID       string
	SessionID     string
	EventType     string
	OccurredAt    string // RFC3339
	Payload       domain.ReviewEventPayload
	ClientEventID string
}

// RecordReviewEvents validates and persists one batch of verifier video-review telemetry events.
// Every event in the batch is authorized against the SAME rule the queue read uses: the item must
// belong to one of the caller's authorized categories (authorizedCategories == nil means
// unrestricted, matching resolveVerifierCategories's CEO/CxO case in the HTTP layer).
func (s *Service) RecordReviewEvents(
	ctx context.Context,
	reviewRepo ports.ReviewEventRepository,
	tenantID, actorID string,
	authorizedCategories []string,
	inputs []ReviewEventBatchInput,
) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	actorID = strings.TrimSpace(actorID)
	if !uuidutil.IsUUIDString(tenantID) {
		return 0, BadRequest("invalid_tenant", "tenant_id must be a UUID")
	}
	if !uuidutil.IsUUIDString(actorID) {
		return 0, BadRequest("invalid_actor", "actor_id must be a UUID")
	}
	if len(inputs) == 0 {
		return 0, BadRequest("empty_batch", "at least one event is required")
	}
	if len(inputs) > MaxReviewEventBatch {
		return 0, BadRequest("batch_too_large", "batch exceeds the maximum of 200 events")
	}

	allowed := map[string]bool{}
	if authorizedCategories != nil {
		for _, c := range authorizedCategories {
			allowed[c] = true
		}
	}

	now := s.now()

	// Classify each input FIRST: queue-scoped (queue_opened, no item_id -- migration 000119) vs
	// item-scoped (every other type, item_id required). This must happen before any item_id
	// UUID-format check, or a queue-scoped event with an empty item_id would wrongly fail the
	// item-scoped "must be a UUID" rule instead of its own queue-scoped rule.
	distinctItemIDs := make([]string, 0, len(inputs))
	seenItemID := map[string]bool{}
	for _, in := range inputs {
		eventType := domain.ReviewEventType(strings.TrimSpace(in.EventType))
		if eventType == domain.ReviewEventQueueOpened {
			continue
		}
		itemID := strings.TrimSpace(in.ItemID)
		// A non-UUID item_id on an item-scoped event is reported per-event below (with the exact
		// field error), not here -- this pre-pass only needs to know which VALID ids to batch-fetch.
		if uuidutil.IsUUIDString(itemID) && !seenItemID[itemID] {
			seenItemID[itemID] = true
			distinctItemIDs = append(distinctItemIDs, itemID)
		}
	}
	// Batch-resolve every distinct item's category in ONE query (scale-guard: n-plus-one-fanout) --
	// a single flush batch can legitimately span more than one item (e.g. a verifier who watched
	// two videos before the periodic flush fired), so looping GetItem per event/per item would be
	// exactly the cross-boundary fan-out docs/decisions/scale-anti-patterns.md bans.
	itemCategoryCache, err := s.repo.GetItemCategories(ctx, tenantID, distinctItemIDs)
	if err != nil {
		return 0, err
	}
	// Each item's OWN proof ids, batched alongside. A client-supplied proof_id must belong to the
	// item the event is attributed to: the derived watch facts partition durations and intervals BY
	// proof_id, so a sibling item's proof (or any other existing proof uuid) would land a foreign
	// duration in this item's denominator and skew WatchFraction plus the CEO integrity aggregate.
	// The DB foreign key only proves the proof row exists, never that it belongs here.
	itemProofRefs, err := s.repo.GetItemProofRefs(ctx, tenantID, distinctItemIDs)
	if err != nil {
		return 0, err
	}

	events := make([]domain.ReviewEvent, 0, len(inputs))
	for _, in := range inputs {
		clientEventID := strings.TrimSpace(in.ClientEventID)
		if !uuidutil.IsUUIDString(clientEventID) {
			return 0, BadRequestField("invalid_client_event_id", "client_event_id must be a client-minted UUID",
				"client_event_id", "invalid_client_event_id", "must be a client-minted UUID")
		}
		eventType := domain.ReviewEventType(strings.TrimSpace(in.EventType))
		if !domain.ReviewEventTypes[eventType] {
			return 0, BadRequestField("invalid_event_type", "event_type is not in the registered set",
				"event_type", "invalid_event_type", "must be one of the registered review event types")
		}
		sessionID := strings.TrimSpace(in.SessionID)
		if sessionID == "" {
			return 0, BadRequestField("invalid_session", "session_id is required",
				"session_id", "required", "session_id is required")
		}
		occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(in.OccurredAt))
		if err != nil {
			return 0, BadRequestField("invalid_occurred_at", "occurred_at must be RFC3339",
				"occurred_at", "invalid_occurred_at", "must be an RFC3339 timestamp")
		}
		// Reject future timestamps with a small clock-skew allowance rather than a hard >now check,
		// since the client clock is untrusted but ordinary NTP drift should not 400 a legitimate event.
		if occurredAt.After(now.Add(2 * time.Minute)) {
			return 0, BadRequestField("occurred_at_in_future", "occurred_at cannot be in the future",
				"occurred_at", "occurred_at_in_future", "cannot be in the future")
		}

		rawItemID := strings.TrimSpace(in.ItemID)
		var itemID *string

		if eventType == domain.ReviewEventQueueOpened {
			// Queue-scoped: item_id must be ABSENT, never a placeholder string like "queue" (the
			// exact bug reported 2026-08-06 -- the old NOT NULL contract forced the client to send
			// something, and that something failed UUID parsing and took the whole batch down).
			if rawItemID != "" {
				return 0, UnprocessableField("invalid_item_id", "queue_opened events must not carry an item_id",
					"item_id", "queue_scoped_item_id_forbidden", "queue_opened has no item yet; omit item_id or send null")
			}
			category := ""
			if in.Payload.Category != nil {
				category = strings.TrimSpace(*in.Payload.Category)
			}
			if category == "" {
				return 0, BadRequestField("invalid_payload", "queue_opened requires payload.category for funnel attribution",
					"payload.category", "required", "category is required on a queue_opened event")
			}
			if authorizedCategories != nil && !allowed[category] {
				return 0, Forbidden("module_scope_forbidden", "category does not belong to an authorized module")
			}
		} else {
			// Item-scoped: item_id is required and must resolve to a real item in an authorized
			// category. A malformed value comes back naming the field precisely -- never the
			// generic decodeJSON invalid_json the coordinator flagged.
			if rawItemID == "" {
				return 0, BadRequestField("invalid_item_id", "item_id is required for this event type",
					"item_id", "required", "item_id is required for an item-scoped event")
			}
			if !uuidutil.IsUUIDString(rawItemID) {
				return 0, BadRequestField("invalid_item_id", "item_id must be a UUID",
					"item_id", "invalid_item_id", "must be a UUID")
			}
			category, ok := itemCategoryCache[rawItemID]
			if !ok {
				return 0, NotFound("item_not_found", "verification item not found")
			}
			if authorizedCategories != nil && !allowed[category] {
				return 0, Forbidden("module_scope_forbidden", "item does not belong to an authorized category")
			}
			itemID = &rawItemID
		}

		var proofID *string
		if p := strings.TrimSpace(in.ProofID); p != "" {
			if !uuidutil.IsUUIDString(p) {
				return 0, BadRequestField("invalid_proof", "proof_id must be a UUID",
					"proof_id", "invalid_proof_id", "must be a UUID")
			}
			// Shape and FK existence are not OWNERSHIP. Without this check an authorized item could
			// carry another item's proof and corrupt its own watch fraction.
			if itemID == nil {
				return 0, BadRequestField("invalid_proof", "a queue-scoped event has no item and cannot carry a proof_id",
					"proof_id", "queue_scoped_proof_forbidden", "omit proof_id on a queue_opened event")
			}
			owned := false
			for _, ref := range itemProofRefs[*itemID] {
				if strings.EqualFold(strings.TrimSpace(ref), p) {
					owned = true
					break
				}
			}
			if !owned {
				return 0, BadRequestField("invalid_proof", "proof_id does not belong to this verification item",
					"proof_id", "proof_not_on_item", "must be one of the item's own proofs")
			}
			proofID = &p
		}

		events = append(events, domain.ReviewEvent{
			TenantID:      tenantID,
			ItemID:        itemID,
			ProofID:       proofID,
			ActorID:       actorID,
			SessionID:     sessionID,
			EventType:     eventType,
			OccurredAt:    occurredAt,
			Payload:       in.Payload,
			ClientEventID: clientEventID,
		})
	}

	return reviewRepo.InsertReviewEvents(ctx, domain.ReviewEventBatch{
		TenantID: tenantID,
		ActorID:  actorID,
		Events:   events,
	})
}

// ItemReviewFacts returns the derived per-actor watch/timing facts for one item, after the same
// item-ownership authorization RecordReviewEvents applies.
func (s *Service) ItemReviewFacts(
	ctx context.Context,
	reviewRepo ports.ReviewEventRepository,
	tenantID, actorID string,
	authorizedCategories []string,
	itemID string,
) ([]domain.ItemReviewFacts, error) {
	item, err := s.repo.GetItem(ctx, tenantID, itemID)
	if err != nil {
		return nil, err
	}
	if authorizedCategories != nil {
		allowed := false
		for _, c := range authorizedCategories {
			if c == item.Category {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, Forbidden("module_scope_forbidden", "item does not belong to an authorized category")
		}
	}
	return reviewRepo.ItemReviewFacts(ctx, tenantID, itemID)
}
