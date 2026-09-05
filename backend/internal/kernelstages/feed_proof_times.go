package kernelstages

import (
	"context"
	"fmt"
	"log/slog"

	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	notificationgateway "github.com/vgoats/goatos/backend/internal/notification/adapters/gateway"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
)

// FeedProofTimesStage posts the daily feed-proof-times table to Slack, one message per park.
//
// It rides the SHARED operational cadence rather than owning a schedule, which the task-kernel lock
// requires: no module may keep a private scheduler. The 17:30 IST cutoff is a GATE inside the
// notifier (it declines to write earlier) and "once per day" comes from the notifier's business-date
// idempotency key, so both properties hold without state of its own and survive a worker restart, a
// redeploy at 18:00, or the HA pair running both instances.
type FeedProofTimesStage struct {
	notifier *notificationbridge.FeedProofTimesNotifier
	tenantID string
}

// FeedProofTimesSlackChannelEnv names the Slack channel the report is posted to. Unset disables the
// stage: an environment with no channel wired posts nothing rather than queueing undeliverable rows.
const FeedProofTimesSlackChannelEnv = "GOATOS_FEED_PROOF_TIMES_SLACK_CHANNEL"

func NewFeedProofTimesStage(deps Deps, tenantID string, logger *slog.Logger) *FeedProofTimesStage {
	feedRepo := feeddirectionpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout))
	return &FeedProofTimesStage{
		notifier: notificationbridge.NewFeedProofTimesNotifier(
			feedRepo, calendarService, getenv(FeedProofTimesSlackChannelEnv), logger),
		tenantID: tenantID,
	}
}

func (s *FeedProofTimesStage) Name() string { return "feed-proof-times-slack" }

func (s *FeedProofTimesStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return fmt.Errorf("feed proof times post: tenant id is required")
	}
	return s.notifier.NotifyFeedProofTimes(ctx, s.tenantID)
}

// SlackChannelWebhookURLsEnv is GOATOS_SLACK_CHANNEL_WEBHOOKS: a JSON object mapping Slack channel
// id to the incoming-webhook URL bound to that channel, e.g.
//
//	{"C0BV1GXCX8B":"https://hooks.slack.com/services/..."}
//
// A Slack incoming webhook cannot be retargeted by its payload, so a second channel needs a second
// URL; this is where they are held. Malformed JSON is logged and treated as unconfigured -- a
// dispatcher that refused to start over one bad env var would take every OTHER channel down with it.
const SlackChannelWebhookURLsEnv = "GOATOS_SLACK_CHANNEL_WEBHOOKS"

func slackChannelWebhookURLs(logger *slog.Logger) map[string]string {
	urls, err := notificationgateway.ChannelWebhookURLsFromJSON(getenv(SlackChannelWebhookURLsEnv))
	if err != nil && logger != nil {
		logger.Warn("slack_channel_webhooks_unparsable", "env", SlackChannelWebhookURLsEnv, "error", err.Error())
	}
	return urls
}
