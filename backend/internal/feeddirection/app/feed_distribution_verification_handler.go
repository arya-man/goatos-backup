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

// Feed distribution verification consumer (maintainer decision, 2026-07-26). A feed-direction session
// is completed only when an independent verifier approves the operator's three proofs. The
// verification module emits a generic verdict event on every approve/reject; this handler is the
// feed-side consumer of those events for feed_distribution items:
//
//	verification.verdict.approved (our feed_distribution item) -> ApplyVerifiedDistribution   (session completed NOW)
//	verification.verdict.rework   (our feed_distribution item) -> BounceDistributionForRework (back to rework)
//
// It filters STRICTLY on source.module + source.ref_type so a vaccination/shifting/etc. verdict is
// ignored. This is the same in-process bus code path a future durable Pub/Sub consumer would use.
const (
	eventVerificationVerdictApproved = "verification.verdict.approved"
	eventVerificationVerdictRework   = "verification.verdict.rework"
)

// feedDistributionVerdictPayload is the subset of the verification verdict payload this consumer reads.
type feedDistributionVerdictPayload struct {
	VerifiedBy string `json:"verified_by"`
	Reason     string `json:"reason"`
	Source     struct {
		Module  string `json:"module"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	} `json:"source"`
}

// FeedDistributionVerificationHandler applies a verifier's verdict to the distribution it verified.
type FeedDistributionVerificationHandler struct {
	store ports.DistributionCompletionStore
	log   *slog.Logger
}

// NewFeedDistributionVerificationHandler constructs the consumer over the distribution store.
func NewFeedDistributionVerificationHandler(store ports.DistributionCompletionStore, log *slog.Logger) *FeedDistributionVerificationHandler {
	return &FeedDistributionVerificationHandler{store: store, log: log}
}

var _ eventbus.Handler = (*FeedDistributionVerificationHandler)(nil)

// Register subscribes the handler to both verdict event types.
func (h *FeedDistributionVerificationHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(eventVerificationVerdictApproved, h)
	bus.Subscribe(eventVerificationVerdictRework, h)
}

// HandleEvent routes an approve/reject verdict for a feed_distribution item to the matching write.
func (h *FeedDistributionVerificationHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	if e.Type != eventVerificationVerdictApproved && e.Type != eventVerificationVerdictRework {
		return nil
	}
	var p feedDistributionVerdictPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
	}
	// Only OUR module's feed-distribution verdicts. Everything else (vaccination, shifting, diagnosis) is
	// a different producer's item and must pass through untouched.
	if p.Source.Module != domain.VerificationModuleFeed || p.Source.RefType != domain.VerificationRefTypeFeed {
		return nil
	}
	completionID := strings.TrimSpace(p.Source.RefID)
	if completionID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}

	switch e.Type {
	case eventVerificationVerdictApproved:
		_, err := h.store.ApplyVerifiedDistribution(ctx, ports.ApplyDistributionParams{
			TenantID:     e.TenantID,
			CompletionID: completionID,
			VerifiedBy:   strings.TrimSpace(p.VerifiedBy),
			// Thread the verification event id as the completion's trace id so the feed.distribution.completed
			// outbox envelope carries a non-empty trace_id.
			TraceID: e.ID,
		})
		return err
	case eventVerificationVerdictRework:
		_, err := h.store.BounceDistributionForRework(ctx, ports.BounceDistributionParams{
			TenantID:     e.TenantID,
			CompletionID: completionID,
			Reason:       strings.TrimSpace(p.Reason),
			TraceID:      e.ID,
		})
		return err
	}
	return nil
}
