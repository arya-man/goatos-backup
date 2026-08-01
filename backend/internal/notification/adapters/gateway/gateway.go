// Package gateway contains replaceable notification channel adapters.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	_, err := g.SendWithResult(ctx, request)
	return err
}

// SendWithResult preserves the generic channel gateway while surfacing acknowledgements from
// providers that return stable message ids. Today FCM supplies one; local/webhook channels return
// an empty successful result.
func (g *Gateway) SendWithResult(ctx context.Context, request domain.Request) (ports.DeliveryResult, error) {
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
		return ports.DeliveryResult{}, nil
	}
	switch channel {
	case "local-stub":
		g.log.InfoContext(ctx, "notification_local_stub_delivered",
			slog.String("notification_request_id", request.NotificationRequestID),
			slog.String("calendar_event_id", request.CalendarEventID),
			slog.String("type", request.NotificationType),
		)
		return ports.DeliveryResult{}, nil
	case "slack":
		return ports.DeliveryResult{}, g.sendSlack(ctx, request)
	case "webhook":
		if strings.TrimSpace(g.config.WebhookURL) == "" {
			return ports.DeliveryResult{}, fmt.Errorf("%w: webhook", ports.ErrChannelNotConfigured)
		}
		return ports.DeliveryResult{}, g.postJSON(ctx, g.config.WebhookURL, requestPayload(request))
	case "incident", "opsgenie", "pagerduty":
		return ports.DeliveryResult{}, g.sendIncident(ctx, channel, request)
	case "email":
		return ports.DeliveryResult{}, g.sendEmail(ctx, request)
	case "push_fcm":
		return g.sendFCMWithResult(ctx, request)
	default:
		return ports.DeliveryResult{}, fmt.Errorf("unsupported notification channel: %s", channel)
	}
}

func (g *Gateway) sendIncident(ctx context.Context, channel string, request domain.Request) error {
	if strings.TrimSpace(g.config.IncidentWebhookURL) == "" {
		if strings.TrimSpace(g.config.SlackWebhookURL) != "" {
			g.log.WarnContext(ctx, "notification_incident_fallback_slack",
				slog.String("notification_request_id", request.NotificationRequestID),
				slog.String("calendar_event_id", request.CalendarEventID),
				slog.String("incident_channel", channel),
			)
			return g.sendSlack(ctx, request)
		}
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

func (g *Gateway) sendSlack(ctx context.Context, request domain.Request) error {
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
	_, err := g.postJSONWithHeadersResponse(ctx, url, payload, headers)
	return err
}

func (g *Gateway) postJSONWithHeadersResponse(ctx context.Context, url string, payload any, headers map[string]string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal notification payload: %w", err)
	}
	// Bound every provider call. A slow or down provider must surface as a timeout this tick and be
	// retried, never hold the dispatcher's lease open indefinitely.
	timeout := g.config.HTTPTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build notification webhook request: %w", err)
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
		// Timeouts stay plain (retryable) errors -- never a not-configured sentinel -- so the
		// dispatcher schedules another attempt instead of exhausting the request.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("notification provider did not respond within %s: %w", timeout, err)
		}
		return nil, fmt.Errorf("post notification webhook: %w", err)
	}
	defer resp.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return nil, fmt.Errorf("read notification webhook response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet := responseBody
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return nil, fmt.Errorf("notification webhook status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return responseBody, nil
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

func (g *Gateway) sendFCMWithResult(ctx context.Context, request domain.Request) (ports.DeliveryResult, error) {
	projectID := strings.TrimSpace(g.config.FCMProjectID)
	endpoint := strings.TrimSpace(g.config.FCMEndpoint)
	if endpoint == "" {
		if projectID == "" {
			return ports.DeliveryResult{}, fmt.Errorf("%w: push_fcm project", ports.ErrChannelNotConfigured)
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
	// Parse context and merge fields into data, and set android priority if present.
	if len(request.Context) > 0 {
		var contextMap map[string]string
		if err := json.Unmarshal(request.Context, &contextMap); err == nil {
			data := message["data"].(map[string]string)
			for key, value := range contextMap {
				data[key] = value
			}
			// FCM collapse/category controls come from the central notification request context.
			// Repeated per-goat events for the same park/category replace one tray slot instead of
			// bombarding the device, while the durable request/delivery ledger still records each.
			androidConfig := map[string]any{}
			if priority, ok := contextMap["priority"]; ok && priority == "high" {
				androidConfig["priority"] = "high"
			}
			if collapseKey := strings.TrimSpace(contextMap["collapse_key"]); collapseKey != "" {
				androidConfig["collapse_key"] = collapseKey
			}
			channelID := strings.TrimSpace(contextMap["android_channel_id"])
			if channelID == "" && strings.Contains(strings.ToLower(contextMap["category"]), "vaccination") {
				channelID = "goatos-push-vaccination"
			}
			notificationConfig := map[string]any{}
			if channelID != "" {
				notificationConfig["channel_id"] = channelID
			}
			if tag := strings.TrimSpace(contextMap["group_key"]); tag != "" {
				notificationConfig["tag"] = tag
			}
			if len(notificationConfig) > 0 {
				androidConfig["notification"] = notificationConfig
			}
			if len(androidConfig) > 0 {
				message["android"] = androidConfig
			}
		}
	}
	if err := setFCMTarget(message, request.RecipientRef); err != nil {
		g.log.ErrorContext(ctx, "notification_push_recipient_unusable",
			slog.String("notification_request_id", request.NotificationRequestID),
			slog.String("calendar_event_id", request.CalendarEventID),
			slog.String("type", request.NotificationType),
			slog.String("error", err.Error()),
		)
		return ports.DeliveryResult{}, err
	}
	token, err := g.fcmBearer(ctx)
	if err != nil {
		return ports.DeliveryResult{}, err
	}
	responseBody, err := g.postJSONWithHeadersResponse(ctx, endpoint, map[string]any{"message": message}, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if err != nil {
		if isInvalidFCMRecipientResponse(err) {
			return ports.DeliveryResult{}, fmt.Errorf("%w: %v", ports.ErrInvalidRecipient, err)
		}
		return ports.DeliveryResult{}, err
	}
	var acknowledgement struct {
		Name string `json:"name"`
	}
	if len(responseBody) > 0 {
		if err := json.Unmarshal(responseBody, &acknowledgement); err != nil {
			return ports.DeliveryResult{}, fmt.Errorf("decode FCM acknowledgement: %w", err)
		}
	}
	return ports.DeliveryResult{ProviderMessageID: strings.TrimSpace(acknowledgement.Name)}, nil
}

func isInvalidFCMRecipientResponse(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return (strings.Contains(text, "notification webhook status 404") ||
		strings.Contains(text, "notification webhook status 400")) &&
		(strings.Contains(text, "notregistered") ||
			strings.Contains(text, "unregistered") ||
			strings.Contains(text, "registration-token-not-registered") ||
			// A malformed / unparseable target is permanent too: no amount of retrying repairs it.
			strings.Contains(text, "invalid_argument") ||
			strings.Contains(text, "invalid argument") ||
			strings.Contains(text, "invalid registration token") ||
			strings.Contains(text, "invalid-registration-token") ||
			strings.Contains(text, "invalid-argument"))
}

// minDeviceTokenLength is a conservative floor for a real device registration token (live tokens run
// well past 100 characters). Its job is to reject identifiers that are plainly not tokens -- above all
// role names such as "park_head", which callers bind into recipient_ref.
const minDeviceTokenLength = 64

// looksLikeDeviceToken reports whether ref has the shape of a device registration token.
func looksLikeDeviceToken(ref string) bool {
	if len(ref) < minDeviceTokenLength {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == ':', r == '.', r == '~', r == '%':
		default:
			return false
		}
	}
	return true
}

// setFCMTarget resolves exactly one push target from the recipient the caller resolved. There is no
// default-topic fallback: a reminder with no resolved recipient must fail loudly rather than broadcast
// one shed's reminder to every device in the tenant. A caller that genuinely wants a topic must say so
// explicitly with a "topic:" / "condition:" recipient.
func setFCMTarget(message map[string]any, recipientRef string) error {
	ref := strings.TrimSpace(recipientRef)
	switch {
	case ref == "":
		return fmt.Errorf("%w: no push recipient resolved", ports.ErrRecipientUnusable)
	case strings.HasPrefix(ref, "topic:"):
		topic := strings.TrimSpace(strings.TrimPrefix(ref, "topic:"))
		if topic == "" {
			return fmt.Errorf("%w: empty push topic", ports.ErrRecipientUnusable)
		}
		message["topic"] = topic
		return nil
	case strings.HasPrefix(ref, "condition:"):
		condition := strings.TrimSpace(strings.TrimPrefix(ref, "condition:"))
		if condition == "" {
			return fmt.Errorf("%w: empty push condition", ports.ErrRecipientUnusable)
		}
		message["condition"] = condition
		return nil
	case looksLikeDeviceToken(ref):
		message["token"] = ref
		return nil
	default:
		// A role name or any other non-token identifier: permanent, and loud. Sending it would earn a
		// 400 that no retry can fix.
		return fmt.Errorf("%w: push recipient is not a device registration token", ports.ErrRecipientUnusable)
	}
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
		// Also network-derived: retry rather than exhaust.
		return "", fmt.Errorf("push credentials returned an empty access token")
	}
	return token.AccessToken, nil
}

func (g *Gateway) fcmAuthSource(ctx context.Context) (oauth2.TokenSource, error) {
	g.fcmMu.Lock()
	defer g.fcmMu.Unlock()
	if g.fcmTokenSource != nil {
		return g.fcmTokenSource, nil
	}
	// Credential discovery talks to the metadata server / ADC over the network, so a failure here is
	// transient by nature. It must stay a plain (retryable) error: wrapping it in
	// ErrChannelNotConfigured makes the dispatcher skip retry scheduling, and one metadata hiccup
	// would permanently exhaust every claimed request in that tick.
	source, err := google.DefaultTokenSource(ctx, "https://www.googleapis.com/auth/firebase.messaging")
	if err != nil {
		return nil, fmt.Errorf("resolve push credentials: %w", err)
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
