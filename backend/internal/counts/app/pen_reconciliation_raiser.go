package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// PEN RECONCILIATION RAISER (maintainer decision 2026-09-02). Weighing is the farm's only
// systematic "which animals are physically in this pen" read, so the submitted individual
// bucket is the trigger: this consumer turns the durable weighing.shed_submission.completed
// event into Reconcile cards for every scanned animal whose registered pen disagrees with the
// pen it was weighed in.
//
// Weighing isolation is untouched: weighing emits its ordinary event and knows nothing about
// this consumer, no scan is gated, and a tag that resolves to no live animal raises nothing.
// The heavy lifting (tag resolution, alias-canonical pen comparison, the one-open-card-per-
// animal insert) is one set-based idempotent SQL statement in the counts repository, so a
// duplicate delivery on a second bus inserts nothing.

// EventWeighingShedSubmissionCompletedForReconciliation is the weighing submit event this
// consumer subscribes to. The string is owned by weighing's producer; it is restated here the
// same way notificationbridge restates it, because subscribing through a weighing import
// would couple counts to weighing's package.
const EventWeighingShedSubmissionCompletedForReconciliation = "weighing.shed_submission.completed"

// penReconciliationWeighingPayload is the subset of the weighing submit payload this consumer
// reads. Weighing owns the full payload.
type penReconciliationWeighingPayload struct {
	TenantID       string `json:"tenant_id"`
	CampaignID     string `json:"campaign_id"`
	CampaignShedID string `json:"campaign_shed_id"`
	CompletedAt    string `json:"completed_at"`
}

// penReconciliationRaiseRepo is the slice of ports.PenReconciliationRepository this consumer
// drives.
type penReconciliationRaiseRepo interface {
	RaisePenReconciliationCards(ctx context.Context, in domain.PenReconciliationRaiseCommand) (int, error)
}

// PenReconciliationRaiser consumes weighing bucket submissions and raises mismatch cards.
type PenReconciliationRaiser struct {
	repo   penReconciliationRaiseRepo
	logger *slog.Logger
	now    func() time.Time
}

// NewPenReconciliationRaiser constructs the consumer over the counts repository.
func NewPenReconciliationRaiser(repo penReconciliationRaiseRepo, logger *slog.Logger, now func() time.Time) *PenReconciliationRaiser {
	if now == nil {
		now = time.Now
	}
	return &PenReconciliationRaiser{repo: repo, logger: logger, now: now}
}

var _ eventbus.Handler = (*PenReconciliationRaiser)(nil)

// Register subscribes the raiser to the weighing submit event.
func (c *PenReconciliationRaiser) Register(bus eventbus.Bus) {
	bus.Subscribe(EventWeighingShedSubmissionCompletedForReconciliation, c)
}

// HandleEvent raises Reconcile cards for one submitted bucket. Lump-sum buckets carry no
// scans; the repository's individual_animal predicate makes them a no-op rather than a guess.
func (c *PenReconciliationRaiser) HandleEvent(ctx context.Context, event eventbus.Event) error {
	if c == nil || c.repo == nil {
		return nil
	}
	if event.Type != EventWeighingShedSubmissionCompletedForReconciliation {
		return nil
	}
	var payload penReconciliationWeighingPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return eventbus.PermanentError(fmt.Errorf("pen reconciliation raise: decode payload: %w", err))
	}
	tenantID := strings.TrimSpace(payload.TenantID)
	if tenantID == "" {
		tenantID = strings.TrimSpace(event.TenantID)
	}
	campaignShedID := strings.TrimSpace(payload.CampaignShedID)
	if tenantID == "" || campaignShedID == "" {
		// A payload naming no bucket cannot be retried into meaning anything.
		return nil
	}
	raisedAt := c.now().UTC()
	if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.CompletedAt)); err == nil {
		raisedAt = ts.UTC()
	}
	raised, err := c.repo.RaisePenReconciliationCards(ctx, domain.PenReconciliationRaiseCommand{
		TenantID:       tenantID,
		CampaignID:     strings.TrimSpace(payload.CampaignID),
		CampaignShedID: campaignShedID,
		RaisedAt:       raisedAt,
	})
	if err != nil {
		return fmt.Errorf("pen reconciliation raise for bucket %s: %w", campaignShedID, err)
	}
	if raised > 0 && c.logger != nil {
		c.logger.InfoContext(ctx, "pen reconciliation cards raised",
			slog.String("tenant_id", tenantID),
			slog.String("campaign_shed_id", campaignShedID),
			slog.Int("cards", raised))
	}
	return nil
}
