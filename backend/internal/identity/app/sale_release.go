package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// A FAILED SALE GIVES ITS ANIMALS BACK (maintainer decision 2026-09-25, docs/decisions/sales-sop.md
// -> "A failed sale"). The sales ledger emits sales.deal.status_changed inside the transaction that
// marks a deal failed; this consumer releases every animal still tagged to that deal back into the
// herd in the pen it was sold from (ports.SaleAllocationReleaser). The event is durable (outbox),
// the release is idempotent, and Deal Failed is final -- so there is no window in which a deal is
// failed and its animals are sold without a delivery that will release them.
const (
	EventSalesDealStatusChanged = "sales.deal.status_changed"
	// dealStatusFailed is the sales ledger's failed-deal word (sales/domain.StatusDealFailed); named
	// here rather than imported, because the payload is the contract between the two modules.
	dealStatusFailed = "Deal Failed"
)

type salesDealStatusChangedPayload struct {
	SalesDealID string `json:"sales_deal_id"`
	Status      string `json:"status"`
	ActorID     string `json:"actor_id"`
}

// SaleFailedReleaseHandler releases a failed deal's tagged animals.
type SaleFailedReleaseHandler struct {
	releaser ports.SaleAllocationReleaser
}

// NewSaleFailedReleaseHandler constructs the consumer.
func NewSaleFailedReleaseHandler(releaser ports.SaleAllocationReleaser) *SaleFailedReleaseHandler {
	return &SaleFailedReleaseHandler{releaser: releaser}
}

var _ eventbus.Handler = (*SaleFailedReleaseHandler)(nil)

// Register subscribes the status-change event.
func (h *SaleFailedReleaseHandler) Register(bus eventbus.Bus) {
	bus.Subscribe(EventSalesDealStatusChanged, h)
}

// HandleEvent releases on Deal Failed and ignores every other status.
func (h *SaleFailedReleaseHandler) HandleEvent(ctx context.Context, e eventbus.Event) error {
	var p salesDealStatusChangedPayload
	if len(e.Payload) > 0 {
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return eventbus.PermanentError(err)
		}
	}
	if !strings.EqualFold(strings.TrimSpace(p.Status), dealStatusFailed) {
		return nil
	}
	dealID := strings.TrimSpace(p.SalesDealID)
	if dealID == "" {
		dealID = strings.TrimSpace(e.Key)
	}
	if dealID == "" || strings.TrimSpace(e.TenantID) == "" {
		return nil
	}
	at := e.OccurredAt
	if at.IsZero() {
		at = time.Now()
	}
	_, err := h.releaser.ReleaseSaleAllocations(ctx, ports.ReleaseSaleAllocationsCommand{
		TenantID:    e.TenantID,
		SalesDealID: dealID,
		ActorID:     strings.TrimSpace(p.ActorID),
		TraceID:     "sales-deal:" + dealID,
		OccurredAt:  at.UTC(),
	})
	return err
}
