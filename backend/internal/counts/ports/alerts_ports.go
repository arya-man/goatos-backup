package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
)

// ErrInvalidArgument is returned when the alerts feed's paging cursor cannot be decoded.
var ErrInvalidArgument = errors.New("counts: invalid argument")

// AlertsRepository is the read boundary for the counts module's own lifecycle alerts feed
// (backend/internal/counts/domain/alerts.go). It is a NARROW, OPTIONAL interface -- resolved by
// type assertion against whatever concrete ports.Repository was passed to NewHerdRegisterService,
// the same pattern MilkPreparationCompletionStore/MilkFeedingStore already use -- so adding it
// cannot break every fake implementing the big ports.Repository interface.
//
// ListAlerts returns one keyset page of the CALLER'S OWN counts alerts, newest first. tenantWide
// and parkIDs express the caller's already-resolved audience scope; the query itself filters on
// context->>'member_id' = the caller, so this is never a broadening of what rows are reachable --
// only a decision about whether the per-park predicate is applied at all.
type AlertsRepository interface {
	ListAlerts(ctx context.Context, tenantID, memberOrUserID string, tenantWide bool, parkIDs []string, cursor string, limit int) (domain.AlertPage, error)
}
