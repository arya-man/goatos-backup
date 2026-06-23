// Package publisher selects an outbox ports.Publisher implementation by environment, so the same
// relay binary runs locally (logging) and deployed (Pub/Sub) with zero code change — only the
// GOATOS_OUTBOX_PUBLISHER env value and the injected broker client differ.
package publisher

import (
	"log/slog"
	"strings"

	"github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/logging"
	"github.com/vgoats/goatos/backend/internal/outbox/adapters/publisher/pubsub"
	"github.com/vgoats/goatos/backend/internal/outbox/ports"
)

// KindPubSub selects the Pub/Sub adapter; any other value selects the logging adapter.
const KindPubSub = "pubsub"

// Select returns the publisher for the given kind. The Pub/Sub adapter is chosen only when the
// kind is "pubsub" AND a broker client is supplied (the deploy-time GCP wrapper); otherwise it
// falls back to the logging adapter — the safe local default. A "pubsub" request with no client
// falls back to logging (and the caller is expected to log that downgrade).
func Select(kind string, logger *slog.Logger, broker pubsub.MessagePublisher) ports.Publisher {
	if strings.EqualFold(strings.TrimSpace(kind), KindPubSub) && broker != nil {
		return pubsub.NewPublisher(broker)
	}
	return logging.NewPublisher(logger)
}

// WantsPubSub reports whether the configured kind requests the Pub/Sub adapter (used by callers
// to detect a downgrade when no broker client is wired yet).
func WantsPubSub(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), KindPubSub)
}
