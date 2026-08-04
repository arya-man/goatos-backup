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
	"github.com/vgoats/goatos/backend/internal/platform/localization"
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

// Localization decision (revised 2026-08-02 -- CORRECTS an earlier version of this comment that
// claimed locale is unavailable server-side; it is not). Per-request locale IS already plumbed
// synchronously: Android sends it on every authenticated call (core-network's Accept-Language
// header), backend/internal/platform/httpmiddleware/request_context.go parses it via
// localization.FromHeaders into request context, and backend/internal/ceoai already consumes it.
// The real gap is narrower: pushes are ASYNCHRONOUS (the vaccination reminder sweeper, obligation-
// missed sweeper, etc. fire with no HTTP request in flight for that recipient), so nothing carries
// a persisted "last known locale" into that path today. That requires a schema change this package
// does not own (see the DDL request below) and has NOT landed yet.
//
// Decision, re-examined on the merits now that locale IS obtainable (not repeating the old
// "infeasible" reasoning): this gateway still uses KEY + CLIENT-TRANSLATION as primary for the full
// message content --
//   - It keeps ONE translation source of truth (the Android app's own values-hi/kn/te resources,
//     already used for 100% of the rest of the UI) instead of maintaining a second, parallel
//     translation catalog in Go for all ~40 message_key sentences -- that duplication is a real,
//     ongoing maintenance cost and drift risk (a hi/kn/te string edited in one place and not the
//     other), not a one-time cost.
//   - It preserves per-message specificity (named vaccine, shed, count, IST date) built from
//     whatever structured params a producer supplies, without teaching Go a second templating
//     layer to reproduce Android's rendering.
//   - It is what the maintainer explicitly asked for: "if it's just key/value where the value is
//     already language-translated in the client, use that."
//
// BUT server-rendering has a real, now-relevant advantage this file did not previously account
// for: a populated `notification` block displays even when the app is fully killed, with NO
// dependence on a high-priority wake succeeding. Once a per-device locale is persisted (see DDL
// request below), this gateway should stop sending an English-only fallback for that guaranteed-
// display path. Forward-compatible, degrades-gracefully design shipped now:
//   - If the request context carries `recipient_locale` (populated by dispatch/queue once the
//     migration below lands and a locale is on file for the device -- NOT populated by anything
//     today, so this branch is currently always a no-op) AND `message_key` is present: this
//     gateway sends BOTH a `data` payload (message_key + params, for the client to render its full,
//     specific string) AND a `notification` block containing a locale-correct but GENERIC,
//     category-level string (from the small embedded table below -- NOT a duplicate of Android's
//     ~40 message templates, just ~5 category strings x 4 locales). Android's onMessageReceived
//     (see GoatOsMessagingService), when it runs, overrides the display with the FULL specific
//     client-rendered string; the generic notification block is what actually reaches the user only
//     in the guaranteed-killed case where onMessageReceived cannot run.
//   - If `recipient_locale` is absent (true today, always): behavior is EXACTLY the data-only path
//     already shipped -- no `notification` block, client renders in foreground/background, and
//     killed-but-not-force-stopped relies on a high-priority data wake (see three-state answer
//     below). This is the honest, currently-shipping state until the migration + a locale-populating
//     dispatch-layer change (outside this package) land.
//
// DDL REQUESTED (this package does not own migrations -- backend/migrations/** is out of scope
// here; hand this to the migration-owning agent):
//
//	ALTER TABLE workforce_member_devices
//	    ADD COLUMN locale_tag TEXT NOT NULL DEFAULT 'en';
//	Table: workforce_member_devices, not workforce_members -- locale here is a per-INSTALL
//	preference (Android's AppLocaleState/SessionStore.language is stored per app-install, not
//	synced to a member-wide profile), and this is the exact table that already stores one row per
//	registered device/FCM token, updated on the SAME registerToken/push-token-sync call site --
//	so locale_tag naturally rides along with the token it belongs to instead of needing a new
//	sync path. NOT NULL DEFAULT 'en' matches localization.DefaultTag so no existing row needs a
//	backfill migration and no caller needs nil-handling. No index: this column is only ever
//	SELECTed alongside the device row when queuing a push, never filtered/joined on.
//
// Concrete three-state outcome for the message_key-carrying, `recipient_locale`-ABSENT case (i.e.
// what actually ships today, before the migration above lands):
//
//	(a) App FOREGROUND: onMessageReceived always fires (independent of block shape). Android
//	    renders message_key+params from its own locale resources -- correct language, full
//	    specificity.
//	(b) App BACKGROUNDED (process alive, not force-stopped): data-only means FCM still delivers to
//	    onMessageReceived, so Android again renders the localized, specific string itself.
//	(c) App KILLED (swiped from recents, not force-stopped): high Android priority lets Play
//	    Services attempt to wake the process to deliver the data message; onMessageReceived fires
//	    and renders locally IF that wake succeeds. The accepted risk: aggressive OEM battery
//	    managers can delay/drop that wake, in which case NOTHING displays for this send (no
//	    `notification` block exists to fall back on) until the app is next opened. A user-FORCE-
//	    STOPPED app blocks delivery of ANY FCM message type regardless of shape -- an OS-level
//	    restriction no design here removes. Once `recipient_locale` is populated (post-migration),
//	    this same state (c) instead shows the locale-correct GENERIC notification (server-rendered),
//	    closing exactly this gap -- see the hybrid branch above.
//
// Hybrid safety net (unchanged from before): a request with NO message_key at all (a caller that
// has not migrated, or a genuinely English-only internal/ops notification) still gets the legacy
// behavior -- a populated `notification.title`/`body` from request.Title/Body, unconditionally.
//
// FCM priority is forced to "high" for every push_fcm send (previously only when the caller's
// context explicitly set priority=high) precisely because data-only delivery depends on it to reach
// onMessageReceived promptly in the backgrounded/killed cases above.
func (g *Gateway) sendFCMWithResult(ctx context.Context, request domain.Request) (ports.DeliveryResult, error) {
	projectID := strings.TrimSpace(g.config.FCMProjectID)
	endpoint := strings.TrimSpace(g.config.FCMEndpoint)
	if endpoint == "" {
		if projectID == "" {
			return ports.DeliveryResult{}, fmt.Errorf("%w: push_fcm project", ports.ErrChannelNotConfigured)
		}
		endpoint = fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", projectID)
	}
	// Parse context first: whether a message_key is present decides whether this send is data-only
	// (client-localized) or carries the legacy server-rendered notification block. See the
	// localization-decision comment above this function.
	var contextMap map[string]string
	if len(request.Context) > 0 {
		if err := json.Unmarshal(request.Context, &contextMap); err != nil {
			contextMap = nil
		}
	}
	messageKey := strings.TrimSpace(contextMap["message_key"])
	recipientLocale := strings.TrimSpace(contextMap["recipient_locale"])

	message := map[string]any{
		"data": fcmData(request),
	}
	switch {
	case messageKey == "":
		// Hybrid safety net: no message_key means this caller has not migrated to the
		// key+client-translation contract (or is a genuinely English-only internal notification).
		// Keep the old server-rendered notification block so it is not silently dropped.
		message["notification"] = map[string]string{
			"title": request.Title,
			"body":  request.Body,
		}
	case recipientLocale != "":
		// Forward-compatible path (see the localization-decision comment above): once dispatch
		// populates a persisted `recipient_locale`, send a locale-correct GENERIC notification
		// block alongside the client-render data payload, so the guaranteed-killed case shows the
		// recipient's own language instead of nothing/English. This is not populated by any
		// producer today (the migration this depends on has not landed), so this branch is
		// currently unreachable in production -- it activates automatically the moment a caller
		// starts supplying `recipient_locale`.
		title, body := localizedFallbackNotification(localization.Normalize(recipientLocale), contextMap["category"])
		message["notification"] = map[string]string{"title": title, "body": body}
	}
	// Every push_fcm send now requests Android high priority: data-only delivery for a
	// message_key push depends on it to reach onMessageReceived promptly while backgrounded or
	// recently killed (see the localization-decision comment above).
	androidConfig := map[string]any{
		"priority": "high",
	}
	if contextMap != nil {
		data := message["data"].(map[string]string)
		for key, value := range contextMap {
			data[key] = value
		}
		// FCM collapse/category controls come from the central notification request context.
		// Repeated per-goat events for the same park/category replace one tray slot instead of
		// bombarding the device, while the durable request/delivery ledger still records each.
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
	}
	message["android"] = androidConfig
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

// isInvalidFCMRecipientResponse reports whether err signals that the FCM RECIPIENT itself is
// permanently dead -- never whether the request payload was malformed. This distinction is load
// bearing: FCM returns HTTP 400 INVALID_ARGUMENT both for a dead/malformed registration token AND
// for a well-formed token but a bad *message* (wrong field, bad data block, oversized payload). A
// bug in OUR payload construction produces the exact same "notification webhook status 400
// ... invalid_argument" text for every recipient in the fleet. Treating bare INVALID_ARGUMENT as a
// dead-token signal previously caused SuppressInvalidRecipient to mass-revoke every addressed
// device on a single bad push (see P0 device-lockout incident): a payload bug looked identical to
// every recipient being unregistered.
//
// Only two classes of signal may suppress a recipient:
//  1. UNREGISTERED / NOT_REGISTERED (FCM's own errorCode for "this token is gone" -- HTTP 404 or
//     400, unambiguous, provider-defined) -- see
//     https://firebase.google.com/docs/reference/fcm/rest/v1/ErrorCode
//  2. A message body that explicitly names the TOKEN as invalid ("registration token is not a
//     valid fcm registration token", or the errorCode detail spelled
//     "invalid-registration-token"/"registration-token-not-registered"), i.e. FCM is talking about
//     the token, not the envelope.
//
// A bare "invalid_argument" / "invalid argument" with no token-specific wording is AMBIGUOUS by
// design -- it is exactly as likely to be our payload bug as a dead token -- and per the fix
// contract for this bug class, ambiguous signals must NOT suppress. Callers must instead log it as
// a send/payload error and leave the device row untouched.
func isInvalidFCMRecipientResponse(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	isFCMError := strings.Contains(text, "notification webhook status 404") ||
		strings.Contains(text, "notification webhook status 400")
	if !isFCMError {
		return false
	}
	// Unambiguous: FCM's own "this token no longer exists" signal.
	if strings.Contains(text, "notregistered") ||
		strings.Contains(text, "unregistered") ||
		strings.Contains(text, "registration-token-not-registered") {
		return true
	}
	// Unambiguous: the error text names the TOKEN specifically, not just the request envelope.
	if strings.Contains(text, "invalid registration token") ||
		strings.Contains(text, "invalid-registration-token") ||
		strings.Contains(text, "not a valid fcm registration token") ||
		strings.Contains(text, "registration token is not valid") {
		return true
	}
	// Everything else -- including bare "invalid_argument" / "invalid argument" -- is ambiguous
	// (could be a payload/envelope bug affecting every recipient) and must NOT suppress.
	return false
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

// localizedFallbackNotificationTable holds ONLY a small, category-level generic string per
// locale -- deliberately NOT a duplicate of the Android client's ~40 message_key templates (that
// full catalog stays client-side, see the localization-decision comment above sendFCMWithResult).
// It exists solely so the guaranteed-display `notification` block (used when a killed app cannot
// run onMessageReceived) shows SOME text in the recipient's own language rather than English,
// once a persisted `recipient_locale` is available. Category keys mirror the Android
// `PushMessageCatalog` fallback categories (vaccination, weighing, feed, counts); an unrecognized
// or absent category falls back to "generic".
var localizedFallbackNotificationTable = map[string]map[string][2]string{
	"en": {
		"vaccination": {"Vaccination", "There is a vaccination update for you. Open the app for details."},
		"weighing":    {"Weighing", "There is a weighing update for you. Open the app for details."},
		"feed":        {"Feed", "There is a feed update for you. Open the app for details."},
		"counts":      {"Counts", "There is a counts update for you. Open the app for details."},
		"generic":     {"Mesha", "You have a new update. Open the app for details."},
	},
	"hi": {
		"vaccination": {"टीकाकरण", "आपके लिए टीकाकरण अपडेट है। विवरण के लिए ऐप खोलें।"},
		"weighing":    {"वज़न", "आपके लिए वज़न अपडेट है। विवरण के लिए ऐप खोलें।"},
		"feed":        {"चारा", "आपके लिए चारा अपडेट है। विवरण के लिए ऐप खोलें।"},
		"counts":      {"गिनती", "आपके लिए गिनती अपडेट है। विवरण के लिए ऐप खोलें।"},
		"generic":     {"Mesha", "आपके लिए एक नया अपडेट है। विवरण के लिए ऐप खोलें।"},
	},
	"kn": {
		"vaccination": {"ಲಸಿಕೆ", "ನಿಮಗಾಗಿ ಲಸಿಕೆ ಅಪ್‌ಡೇಟ್ ಇದೆ. ವಿವರಗಳಿಗಾಗಿ ಆ್ಯಪ್ ತೆರೆಯಿರಿ."},
		"weighing":    {"ತೂಕ", "ನಿಮಗಾಗಿ ತೂಕ ಅಪ್‌ಡೇಟ್ ಇದೆ. ವಿವರಗಳಿಗಾಗಿ ಆ್ಯಪ್ ತೆರೆಯಿರಿ."},
		"feed":        {"ಆಹಾರ", "ನಿಮಗಾಗಿ ಆಹಾರ ಅಪ್‌ಡೇಟ್ ಇದೆ. ವಿವರಗಳಿಗಾಗಿ ಆ್ಯಪ್ ತೆರೆಯಿರಿ."},
		"counts":      {"ಗಣತಿ", "ನಿಮಗಾಗಿ ಗಣತಿ ಅಪ್‌ಡೇಟ್ ಇದೆ. ವಿವರಗಳಿಗಾಗಿ ಆ್ಯಪ್ ತೆರೆಯಿರಿ."},
		"generic":     {"Mesha", "ನಿಮಗಾಗಿ ಹೊಸ ಅಪ್‌ಡೇಟ್ ಇದೆ. ವಿವರಗಳಿಗಾಗಿ ಆ್ಯಪ್ ತೆರೆಯಿರಿ."},
	},
	"te": {
		"vaccination": {"టీకా", "మీ కోసం టీకా అప్‌డేట్ ఉంది. వివరాల కోసం యాప్ తెరవండి."},
		"weighing":    {"బరువు", "మీ కోసం బరువు అప్‌డేట్ ఉంది. వివరాల కోసం యాప్ తెరవండి."},
		"feed":        {"దాణా", "మీ కోసం దాణా అప్‌డేట్ ఉంది. వివరాల కోసం యాప్ తెరవండి."},
		"counts":      {"లెక్కలు", "మీ కోసం లెక్కల అప్‌డేట్ ఉంది. వివరాల కోసం యాప్ తెరవండి."},
		"generic":     {"Mesha", "మీ కోసం కొత్త అప్‌డేట్ ఉంది. వివరాల కోసం యాప్ తెరవండి."},
	},
}

// localizedFallbackNotification returns a (title, body) pair for the guaranteed-display
// `notification` block in the recipient's normalized locale. category is matched by prefix
// against "vaccination"/"weighing"/"feed"/"counts"; anything else (including empty) uses the
// generic strings. Never returns raw tokens or a blank string -- always falls back to the "en"
// row, which always exists.
func localizedFallbackNotification(localeTag, category string) (string, string) {
	byCategory, ok := localizedFallbackNotificationTable[localeTag]
	if !ok {
		byCategory = localizedFallbackNotificationTable[localization.DefaultTag]
	}
	key := "generic"
	switch {
	case strings.HasPrefix(category, "vaccination"):
		key = "vaccination"
	case strings.HasPrefix(category, "weighing"):
		key = "weighing"
	case strings.HasPrefix(category, "feed"):
		key = "feed"
	case strings.HasPrefix(category, "counts"):
		key = "counts"
	}
	pair, ok := byCategory[key]
	if !ok {
		pair = localizedFallbackNotificationTable[localization.DefaultTag]["generic"]
	}
	return pair[0], pair[1]
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
