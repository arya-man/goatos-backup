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
	"sync"
	"time"

	"github.com/vgoats/goatos/backend/internal/notification/domain"
	"github.com/vgoats/goatos/backend/internal/notification/ports"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type Config struct {
	WebhookURL         string
	SlackWebhookURL    string
	EmailWebhookURL    string
	EmailAuthToken     string
	EmailDefaultTo     string
	IncidentWebhookURL string
	IncidentAuthToken  string
	FCMProjectID       string
	FCMEndpoint        string
	FCMBearerToken     string
	FCMDefaultTopic    string
	DryRun             bool
	HTTPTimeout        time.Duration
}

type Gateway struct {
	client         *http.Client
	config         Config
	log            *slog.Logger
	fcmMu          sync.Mutex
	fcmTokenSource oauth2.TokenSource
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
	case "incident", "opsgenie", "pagerduty":
		return g.sendIncident(ctx, channel, request)
	case "email":
		return g.sendEmail(ctx, request)
	case "push_fcm":
		return g.sendFCM(ctx, request)
	default:
		return fmt.Errorf("unsupported notification channel: %s", channel)
	}
}

func (g *Gateway) sendIncident(ctx context.Context, channel string, request domain.Request) error {
	if strings.TrimSpace(g.config.IncidentWebhookURL) == "" {
		return fmt.Errorf("%w: incident", ports.ErrChannelNotConfigured)
	}
	headers := map[string]string{}
	if token := strings.TrimSpace(g.config.IncidentAuthToken); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	payload := map[string]any{
		"routing_key": channel,
		"dedupe_key":  request.NotificationRequestID,
		"severity":    incidentSeverity(request),
		"title":       request.Title,
		"body":        request.Body,
		"source":      "goatos-notification-dispatcher",
		"metadata":    requestPayload(request),
	}
	return g.postJSONWithHeaders(ctx, g.config.IncidentWebhookURL, payload, headers)
}

func incidentSeverity(request domain.Request) string {
	body := strings.ToLower(request.Title + " " + request.Body + " " + string(request.Context))
	switch {
	case strings.Contains(body, "critical"), strings.Contains(body, "level\":4"), strings.Contains(body, "level_4"):
		return "critical"
	case strings.Contains(body, "level\":3"), strings.Contains(body, "level_3"):
		return "high"
	case strings.Contains(body, "warning"), strings.Contains(body, "level\":2"), strings.Contains(body, "level_2"):
		return "medium"
	default:
		return "low"
	}
}

func (g *Gateway) postJSON(ctx context.Context, url string, payload any) error {
	return g.postJSONWithHeaders(ctx, url, payload, nil)
}

func (g *Gateway) postJSONWithHeaders(ctx context.Context, url string, payload any, headers map[string]string) error {
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
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}
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

func (g *Gateway) sendEmail(ctx context.Context, request domain.Request) error {
	if strings.TrimSpace(g.config.EmailWebhookURL) == "" {
		return fmt.Errorf("%w: email", ports.ErrChannelNotConfigured)
	}
	to := strings.TrimSpace(g.config.EmailDefaultTo)
	if ref := strings.TrimSpace(request.RecipientRef); strings.Contains(ref, "@") {
		to = ref
	}
	if to == "" {
		return fmt.Errorf("%w: email recipient", ports.ErrChannelNotConfigured)
	}
	headers := map[string]string{}
	if token := strings.TrimSpace(g.config.EmailAuthToken); token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	return g.postJSONWithHeaders(ctx, g.config.EmailWebhookURL, map[string]any{
		"to":       []string{to},
		"subject":  request.Title,
		"text":     request.Body,
		"metadata": requestPayload(request),
	}, headers)
}

func (g *Gateway) sendFCM(ctx context.Context, request domain.Request) error {
	projectID := strings.TrimSpace(g.config.FCMProjectID)
	endpoint := strings.TrimSpace(g.config.FCMEndpoint)
	if endpoint == "" {
		if projectID == "" {
			return fmt.Errorf("%w: push_fcm project", ports.ErrChannelNotConfigured)
		}
		endpoint = fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", projectID)
	}
	message := map[string]any{
		"notification": map[string]string{
			"title": request.Title,
			"body":  request.Body,
		},
		"data": fcmData(request),
	}
	if err := setFCMTarget(message, request.RecipientRef, g.config.FCMDefaultTopic); err != nil {
		return err
	}
	token, err := g.fcmBearer(ctx)
	if err != nil {
		return err
	}
	return g.postJSONWithHeaders(ctx, endpoint, map[string]any{"message": message}, map[string]string{
		"Authorization": "Bearer " + token,
	})
}

func setFCMTarget(message map[string]any, recipientRef, defaultTopic string) error {
	ref := strings.TrimSpace(recipientRef)
	switch {
	case strings.HasPrefix(ref, "topic:"):
		topic := strings.TrimSpace(strings.TrimPrefix(ref, "topic:"))
		if topic != "" {
			message["topic"] = topic
			return nil
		}
	case strings.HasPrefix(ref, "condition:"):
		condition := strings.TrimSpace(strings.TrimPrefix(ref, "condition:"))
		if condition != "" {
			message["condition"] = condition
			return nil
		}
	case ref != "":
		message["token"] = ref
		return nil
	}
	topic := strings.TrimSpace(defaultTopic)
	if topic == "" {
		return fmt.Errorf("%w: push_fcm recipient", ports.ErrChannelNotConfigured)
	}
	message["topic"] = topic
	return nil
}

func (g *Gateway) fcmBearer(ctx context.Context) (string, error) {
	if token := strings.TrimSpace(g.config.FCMBearerToken); token != "" {
		return token, nil
	}
	source, err := g.fcmAuthSource(ctx)
	if err != nil {
		return "", err
	}
	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("fetch FCM access token: %w", err)
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", fmt.Errorf("%w: push_fcm auth token", ports.ErrChannelNotConfigured)
	}
	return token.AccessToken, nil
}

func (g *Gateway) fcmAuthSource(ctx context.Context) (oauth2.TokenSource, error) {
	g.fcmMu.Lock()
	defer g.fcmMu.Unlock()
	if g.fcmTokenSource != nil {
		return g.fcmTokenSource, nil
	}
	source, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("%w: push_fcm auth", ports.ErrChannelNotConfigured)
	}
	g.fcmTokenSource = source
	return source, nil
}

func fcmData(request domain.Request) map[string]string {
	return map[string]string{
		"notification_request_id": request.NotificationRequestID,
		"tenant_id":               request.TenantID,
		"calendar_event_id":       request.CalendarEventID,
		"notification_type":       request.NotificationType,
		"trace_id":                request.TraceID,
	}
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
