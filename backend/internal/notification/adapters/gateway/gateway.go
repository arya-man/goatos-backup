// Package gateway contains replaceable notification channel adapters.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
)

type Config struct {
	WebhookURL      string
	SlackWebhookURL string
	DryRun          bool
	HTTPTimeout     time.Duration
}

type Gateway struct {
	client *http.Client
	config Config
	log    *slog.Logger
}

func New(config Config, log *slog.Logger) *Gateway {
	if config.HTTPTimeout <= 0 {
		config.HTTPTimeout = 5 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &Gateway{
		client: &http.Client{Timeout: config.HTTPTimeout},
		config: config,
		log:    log,
	}
}

func (g *Gateway) Name() string {
	return "notification-gateway"
}

func (g *Gateway) Send(ctx context.Context, request domain.Request) error {
	channel := strings.TrimSpace(request.Channel)
	if channel == "" {
		channel = "local-stub"
	}
	if g.config.DryRun {
		g.log.InfoContext(ctx, "notification_dry_run",
			slog.String("notification_request_id", request.NotificationRequestID),
			slog.String("calendar_event_id", request.CalendarEventID),
			slog.String("channel", channel),
			slog.String("type", request.NotificationType),
		)
		return nil
	}
	switch channel {
	case "local-stub":
		g.log.InfoContext(ctx, "notification_local_stub_delivered",
			slog.String("notification_request_id", request.NotificationRequestID),
			slog.String("calendar_event_id", request.CalendarEventID),
			slog.String("type", request.NotificationType),
		)
		return nil
	case "slack":
		if strings.TrimSpace(g.config.SlackWebhookURL) == "" {
			return fmt.Errorf("%w: slack", ports.ErrChannelNotConfigured)
		}
		return g.postJSON(ctx, g.config.SlackWebhookURL, map[string]any{
			"text": fmt.Sprintf("%s\n%s", request.Title, request.Body),
			"metadata": map[string]any{
				"notification_request_id": request.NotificationRequestID,
				"calendar_event_id":       request.CalendarEventID,
				"notification_type":       request.NotificationType,
			},
		})
	case "webhook":
		if strings.TrimSpace(g.config.WebhookURL) == "" {
			return fmt.Errorf("%w: webhook", ports.ErrChannelNotConfigured)
		}
		return g.postJSON(ctx, g.config.WebhookURL, requestPayload(request))
	case "email", "push_fcm":
		return fmt.Errorf("%w: %s", ports.ErrChannelNotConfigured, channel)
	default:
		return fmt.Errorf("unsupported notification channel: %s", channel)
	}
}

func (g *Gateway) postJSON(ctx context.Context, url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal notification payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build notification webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "goatos-notification-dispatcher/1.0")
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("post notification webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("notification webhook status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

func requestPayload(request domain.Request) map[string]any {
	return map[string]any{
		"notification_request_id": request.NotificationRequestID,
		"tenant_id":               request.TenantID,
		"calendar_event_id":       request.CalendarEventID,
		"target_type":             request.TargetType,
		"target_id":               request.TargetID,
		"notification_type":       request.NotificationType,
		"channel":                 request.Channel,
		"recipient_ref":           request.RecipientRef,
		"title":                   request.Title,
		"body":                    request.Body,
		"trace_id":                request.TraceID,
		"context":                 json.RawMessage(request.Context),
	}
}
