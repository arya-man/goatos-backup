package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// Feed wastage verification consumer (maintainer decision, 2026-08-18). A pen-day's wastage task is
// completed only when an independent verifier approves the operator's video:
//
//	verification.verdict.approved (our feed_wastage item) -> ApplyVerifiedWastage   (pen-day completed NOW)
//	verification.verdict.rework   (our feed_wastage item) -> BounceWastageForRework (back to rework)
//
// It filters STRICTLY on source.module + source.ref_type so a vaccination/shifting/feed-distribution/
// feed-packing/feed-transport verdict is ignored. All four feed gates share source.module="feed" and
// are kept apart only by ref_type — feed_wastage_completion is this handler's whole world.

// feedWastageVerdictPayload is the subset of the verification verdict payload this consumer reads.
type feedWastageVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// FeedWastageVerificationHandler applies a verifier's verdict to the wastage completion it verified.
type FeedWastageVerificationHandler struct {
	store ports.WastageCompletionStore
	log   *slog.Logger
}

// NewFeedWastageVerificationHandler constructs the consumer over the wastage store.
func NewFeedWastageVerificationHandler(store ports.WastageCompletionStore, log *slog.Logger) *FeedWastageVerificationHandler {
	return &FeedWastageVerificationHandler{store: store, log: log}
}

var _ eventbus.Handler = (*FeedWastageVerificationHandler)(nil)

// Register subscribes the handler to both verdict event types.
func (h *FeedWastageVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVerificationVerdictApproved, h)
	bus.Subscribe(eventVerificationVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a feed_wastage item to the matching write.
func (h *FeedWastageVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventVerificationVerdictApproved && e.Type != eventVerificationVerdictRework {
		return nil
	}
	var p feedWastageVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	// Only OUR module's feed-wastage verdicts. Everything else is a different producer's item and
	// must pass through untouched.
	if p.Source.Module != domain.VerificationModuleFeed || p.Source.RefType != domain.VerificationRefTypeWastage {
		return nil
	}
	completionID := strings.TrimSpace(p.Source.RefID)
	if completionID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}

	switch e.Type {
	case eventVerificationVerdictApproved:
		_, err := h.store.ApplyVerifiedWastage(ctx, ports.ApplyWastageParams{
			TenantID:     e.TenantID,
			CompletionID: completionID,
			VerifiedBy:   strings.TrimSpace(p.VerifiedBy),
			// Thread the verification event id as the completion's trace id so the
			// feed.wastage.completed outbox envelope carries a non-empty trace_id.
			TraceID: e.ID,
		})
		return err
	case eventVerificationVerdictRework:
		_, err := h.store.BounceWastageForRework(ctx, ports.BounceWastageParams{
			TenantID:     e.TenantID,
			CompletionID: completionID,
			Reason:       strings.TrimSpace(p.Reason),
			TraceID:      e.ID,
		})
		return err
	}
	return nil
}
