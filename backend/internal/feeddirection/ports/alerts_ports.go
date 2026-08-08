package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
)

// ErrInvalidArgument is returned when the alerts feed's paging cursor cannot be decoded.
var ErrInvalidArgument = errors.New("feeddirection: invalid argument")

// AlertsRepository is the read boundary for the feed module's own lifecycle alerts feed
// (backend/internal/feeddirection/domain/alerts.go). It is deliberately a NARROW, OPTIONAL
// interface -- mirroring CompletionStore/DistributionCompletionStore/PackingCompletionStore above
// -- rather than a method added to a single monolithic repository interface, so that adding it
// cannot break every existing fake that only ever needed the generation-time ports.
//
// ListAlerts returns one keyset page of the CALLER'S OWN feed alerts, newest first. tenantWide and
// parkIDs express the caller's already-resolved audience scope; the query itself filters on
// context->>'member_id' = the caller, so this is never a broadening of what rows are reachable --
// only a decision about whether the per-park predicate is applied at all.
type AlertsRepository interface {
	ListAlerts(ctx context.Context, tenantID, memberOrUserID string, tenantWide bool, parkIDs []string, cursor string, limit int) (domain.AlertPage, error)
}
