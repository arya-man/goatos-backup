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

	// Batch-resolve every distinct item's category in ONE query (scale-guard: n-plus-one-fanout) --
	// a single flush batch can legitimately span more than one item (e.g. a verifier who watched
	// two videos before the periodic flush fired), so looping GetItem per event/per item would be
	// exactly the cross-boundary fan-out docs/decisions/scale-anti-patterns.md bans.
	distinctItemIDs := make([]string, 0, len(inputs))
	seenItemID := map[string]bool{}
	for _, in := range inputs {
		itemID := strings.TrimSpace(in.ItemID)
		if !uuidutil.IsUUIDString(itemID) {
			return 0, BadRequest("invalid_item", "item_id must be a UUID")
		}
		if !seenItemID[itemID] {
			seenItemID[itemID] = true
			distinctItemIDs = append(distinctItemIDs, itemID)
		}
	}
	itemCategoryCache, err := s.repo.GetItemCategories(ctx, tenantID, distinctItemIDs)
	if err != nil {
		return 0, err
	}

	events := make([]domain.ReviewEvent, 0, len(inputs))
	for _, in := range inputs {
		itemID := strings.TrimSpace(in.ItemID)
		clientEventID := strings.TrimSpace(in.ClientEventID)
		if !uuidutil.IsUUIDString(clientEventID) {
			return 0, BadRequest("invalid_client_event_id", "client_event_id must be a client-minted UUID")
		}
		eventType := domain.ReviewEventType(strings.TrimSpace(in.EventType))
		if !domain.ReviewEventTypes[eventType] {
			return 0, BadRequest("invalid_event_type", "event_type is not in the registered set")
		}
		sessionID := strings.TrimSpace(in.SessionID)
		if sessionID == "" {
			return 0, BadRequest("invalid_session", "session_id is required")
		}
		occurredAt, err := time.Parse(time.RFC3339, strings.TrimSpace(in.OccurredAt))
		if err != nil {
			return 0, BadRequest("invalid_occurred_at", "occurred_at must be RFC3339")
		}
		// Reject future timestamps with a small clock-skew allowance rather than a hard >now check,
		// since the client clock is untrusted but ordinary NTP drift should not 400 a legitimate event.
		if occurredAt.After(now.Add(2 * time.Minute)) {
			return 0, BadRequest("occurred_at_in_future", "occurred_at cannot be in the future")
		}

		category, ok := itemCategoryCache[itemID]
		if !ok {
			return 0, NotFound("item_not_found", "verification item not found")
		}
		if authorizedCategories != nil && !allowed[category] {
			return 0, Forbidden("module_scope_forbidden", "item does not belong to an authorized category")
		}

		var proofID *string
		if p := strings.TrimSpace(in.ProofID); p != "" {
			if !uuidutil.IsUUIDString(p) {
				return 0, BadRequest("invalid_proof", "proof_id must be a UUID")
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
