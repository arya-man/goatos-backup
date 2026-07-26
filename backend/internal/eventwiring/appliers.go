// Package eventwiring holds the SINGLE registration of the cross-module verifier-verdict appliers, so
// every process that owns a domain-event bus registers the exact same set and they cannot drift.
//
// Why this package exists: the shifting + feed-distribution + feed-packing verification handlers were
// registered on the API's in-process bus (bootstrap/api.go) but NOT on the two processes that actually
// consume the durable outbox — cmd/outbox-relay (local eventbus) and cmd/domain-event-consumer (prod
// Pub/Sub). A verifier's verdict is delivered ONLY through the outbox, so the API-bus registration
// never fired and every feed/shifting approval was silently stranded in pending_verification. Three
// hand-maintained registration lists drifted; this collapses them to one.
package eventwiring

import (
	"context"
	"log/slog"

	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
)

// ShiftingVerificationRepo is the counts slice the shifting applier drives. Exported so the shared
// wiring (and its test) can supply it; *countspg.Repository (with WithIdentityTxWriter) satisfies it.
type ShiftingVerificationRepo interface {
	ApplyVerifiedShiftingEvent(ctx context.Context, in countsdomain.ShiftingVerifiedApplyCommand) (countsdomain.ShiftingExecutionResult, bool, error)
	BounceShiftingEventForRework(ctx context.Context, in countsdomain.ShiftingReworkCommand) error
}

// FeedCompletionStore is satisfied by *feeddirectionpg.Repository (it owns both the distribution and
// packing completion tables, so it is both stores at once).
type FeedCompletionStore interface {
	feeddirectionports.DistributionCompletionStore
	feeddirectionports.PackingCompletionStore
}

// RegisterVerificationAppliers subscribes the shifting, feed-distribution, and feed-packing appliers to
// the generic verification verdict events on `bus`. Each handler filters strictly on
// source.module + source.ref_type (counts/shifting_event, feed/feed_distribution_completion,
// feed/feed_packing_completion), so cross-fire is impossible. This is the ONE place these three are
// registered; bootstrap/api.go, cmd/outbox-relay, and cmd/domain-event-consumer all call it.
func RegisterVerificationAppliers(bus eventbus.Bus, feed FeedCompletionStore, shifting ShiftingVerificationRepo, log *slog.Logger) {
	countsapp.NewShiftingVerificationHandler(shifting, nil).Register(bus)
	feeddirectionapp.NewFeedDistributionVerificationHandler(feed, log).Register(bus)
	feeddirectionapp.NewFeedPackingVerificationHandler(feed, log).Register(bus)
}
