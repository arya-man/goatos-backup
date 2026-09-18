// Package browserpush owns the browser (Chrome) web push address book for admin-web.
//
// WHAT THIS PACKAGE IS NOT: it is not a second notification system. It stores one more KIND of
// push address and hands those addresses to the existing recipient-resolution seam as ordinary
// calendar/workforce NotificationRecipient values. Everything downstream -- notification_requests,
// the dispatcher, the retry/backoff schedule, the FCM HTTP v1 gateway, the dead-address
// suppression -- is the code that already delivers to phones, unchanged. That is deliberate: a
// notification type added by any other module reaches a subscribed browser without that module
// knowing browsers exist.
//
// WHY AN FCM WEB REGISTRATION TOKEN AND NOT A RAW WEB PUSH SUBSCRIPTION: see the design note on
// migration 000353. In one line: admin-web already ships the Firebase JS SDK for auth, so
// firebase/messaging getToken() yields a single opaque token of exactly the shape the backend
// already addresses, and FCM does the VAPID signing and payload encryption that a raw
// {endpoint, p256dh, auth} subscription would have obliged this package to implement itself.
package browserpush

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status values mirror the CHECK constraint on
// workforce_member_browser_push_registrations.status.
const (
	// StatusActive is a browser we believe we can reach.
	StatusActive = "active"
	// StatusStale is a browser the PROVIDER told us is gone: the subscription expired, the
	// profile was cleared, or the permission was revoked. Written by the send path, never by a
	// person.
	StatusStale = "stale"
	// StatusUnsubscribed is a browser the PERSON switched off from the dashboard. Distinct from
	// stale on purpose: stale is a fact about the address, unsubscribed is a choice about the
	// human, and a support question ("why did my alerts stop?") has a different answer for each.
	StatusUnsubscribed = "unsubscribed"

	// ProviderWebFCM is the only provider today. Stored rather than assumed so a future second
	// transport is an added value, not a reinterpretation of every existing row.
	ProviderWebFCM = "web_fcm"
)

// Field length ceilings. These are input hygiene, not policy: an unbounded text column reachable
// from an authenticated browser is an unbounded write, and user_agent in particular is entirely
// client-controlled.
const (
	maxBrowserInstallIDLen = 200
	maxTokenLen            = 4096
	maxUserAgentLen        = 512
	maxBrowserLabelLen     = 120
)

// ErrRegistrationNotFound means no registration exists for that (tenant, browser install).
var ErrRegistrationNotFound = errors.New("browser push registration not found")

// ErrBrowserInstallConflict means that browser profile already carries ANOTHER member's live
// registration and the caller offered no proof of being at that browser.
//
// IT IS NOT AN AUTHORIZATION FAILURE AND MUST NOT BE REPORTED AS ONE. browser_install_id is
// per-browser-profile and survives sign-out, so the ordinary cause is two colleagues sharing one
// office desktop while the Chrome token has since rotated -- nobody did anything wrong. It is a
// RECOVERABLE conflict: the client mints itself a fresh install id and registers again, which is
// why it is a distinct sentinel rather than folded into ErrRegistrationNotFound. Reporting it as
// a permission problem would leave the second person with no registration and no action to take,
// which is the silent-unreachability half of the defect this sentinel exists to close.
var ErrBrowserInstallConflict = errors.New("browser push registration belongs to another member")

// Registration is one browser profile's push address.
type Registration struct {
	BrowserRegistrationID string     `json:"browser_registration_id"`
	WorkforceMemberID     string     `json:"workforce_member_id"`
	Provider              string     `json:"provider"`
	BrowserInstallID      string     `json:"browser_install_id"`
	BrowserLabel          string     `json:"browser_label"`
	Status                string     `json:"status"`
	CreatedAt             time.Time  `json:"created_at"`
	LastSeenAt            time.Time  `json:"last_seen_at"`
	StaleAt               *time.Time `json:"stale_at,omitempty"`
	StaleReason           string     `json:"stale_reason,omitempty"`
	RowVersion            int        `json:"row_version"`
}

// RegisterRequest is the browser's own claim about itself. NOTE WHAT IS ABSENT: no tenant id, no
// member id, no user id. Those are taken from the authenticated session by the handler and are
// never accepted from the body -- a browser must not be able to register a push address against
// somebody else's name.
type RegisterRequest struct {
	BrowserInstallID string `json:"browser_install_id"`
	Token            string `json:"token"`
	BrowserLabel     string `json:"browser_label,omitempty"`
	UserAgent        string `json:"user_agent,omitempty"`
}

// UnregisterRequest names the browser to switch off. Same rule: the person is the session, not
// the body.
type UnregisterRequest struct {
	BrowserInstallID string `json:"browser_install_id"`
}

// RegisterCommand is the service-level command: session-derived identity plus the browser's claim.
type RegisterCommand struct {
	TenantID string
	ActorID  string
	Body     RegisterRequest
}

// UnregisterCommand is the service-level unregister command.
type UnregisterCommand struct {
	TenantID string
	ActorID  string
	Body     UnregisterRequest
}

// RegisterResult is what the browser gets back. It echoes the stored registration so the client
// can tell "you are newly subscribed" from "your existing subscription was refreshed" without a
// second read.
type RegisterResult struct {
	Registration Registration `json:"registration"`
	Created      bool         `json:"created"`
}

// UnregisterResult reports whether anything was actually switched off. A double-click, a second
// tab, or a browser that was already pruned all land on Removed=false and are NOT errors:
// unregister is idempotent by design, because the client calls it on a permission revocation it
// may already have reported.
type UnregisterResult struct {
	Removed bool `json:"removed"`
}

// Recipient is one reachable browser, in the minimal shape the delivery layer needs. It is
// deliberately NOT the workforce NotificationRecipient type: this package must not depend on the
// workforce module, so notificationbridge does the one-line mapping at the seam.
type Recipient struct {
	// WorkforceMemberID is the canonical member, so the resulting notification_requests row is
	// attributable to a person exactly as the phone rows are.
	WorkforceMemberID string
	// BrowserRegistrationID takes the place the phone path gives device_id. It is what makes the
	// downstream idempotency key per-browser, so two of a person's browsers each get their own
	// row instead of racing for one.
	BrowserRegistrationID string
	// Token is the FCM web registration token, addressed by the existing push_fcm channel.
	Token string
}

// Repository is the persistence seam. Implemented by adapters/postgres; faked in tests.
type Repository interface {
	// Upsert stores or refreshes one browser profile's address, keyed on
	// (tenant_id, browser_install_id). A refresh must revive a row that was previously stale or
	// unsubscribed rather than insert a second one -- a browser that comes back is the same
	// browser. It must refuse ErrBrowserInstallConflict rather than rewrite a conflicting row
	// that belongs to a DIFFERENT active member with a different token: the row would stay
	// attributed to that member while delivering to the caller's screen.
	Upsert(ctx context.Context, tenantID, memberOrUserID string, in RegisterRequest, now time.Time) (Registration, bool, error)
	// MarkUnsubscribed switches one browser off at the person's own request. Reports false when
	// there was nothing active to switch off.
	MarkUnsubscribed(ctx context.Context, tenantID, memberOrUserID, browserInstallID string, now time.Time) (bool, error)
	// ListForMember returns this person's registrations, whatever their status, so the dashboard
	// can say which browsers are subscribed and which went stale.
	ListForMember(ctx context.Context, tenantID, memberOrUserID string) ([]Registration, error)
	// ResolveMemberRecipients returns the ACTIVE browsers of one person. Status is the whole
	// predicate: 'stale' and 'unsubscribed' rows are addresses we already know are dead or
	// declined, and addressing them would burn the retry schedule and count a drop as a delivery.
	ResolveMemberRecipients(ctx context.Context, tenantID, memberOrUserID string) ([]Recipient, error)
	// MarkTokenStale is the PRUNE. It is addressed by token because that is all a delivery
	// failure knows. Reports how many registrations it retired.
	MarkTokenStale(ctx context.Context, tenantID, token, reason string, now time.Time) (int, error)
}

// Service is the application service: validation, session-identity enforcement, and nothing else.
// It deliberately holds no delivery logic.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService wires the service over its persistence seam.
func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithClock replaces the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// Register stores or refreshes the calling browser's push address.
//
// THE EXISTING-SUBSCRIPTION-NEEDS-REFRESHING CASE IS THE COMMON CASE, NOT THE EDGE CASE. Chrome
// rotates an FCM web token on its own schedule and the client re-reads it on every dashboard
// load, so the overwhelming majority of calls here are a same-token or new-token refresh of a row
// that already exists. That is why this is an upsert keyed on the browser, not a create.
func (s *Service) Register(ctx context.Context, cmd RegisterCommand) (RegisterResult, error) {
	tenantID := strings.TrimSpace(cmd.TenantID)
	actorID := strings.TrimSpace(cmd.ActorID)
	if tenantID == "" || actorID == "" {
		return RegisterResult{}, fmt.Errorf("browser push register: unauthenticated caller")
	}
	body, err := sanitizeRegister(cmd.Body)
	if err != nil {
		return RegisterResult{}, err
	}
	registration, created, err := s.repo.Upsert(ctx, tenantID, actorID, body, s.now().UTC())
	if err != nil {
		return RegisterResult{}, err
	}
	return RegisterResult{Registration: registration, Created: created}, nil
}

// Unregister switches the calling browser off. Idempotent.
func (s *Service) Unregister(ctx context.Context, cmd UnregisterCommand) (UnregisterResult, error) {
	tenantID := strings.TrimSpace(cmd.TenantID)
	actorID := strings.TrimSpace(cmd.ActorID)
	if tenantID == "" || actorID == "" {
		return UnregisterResult{}, fmt.Errorf("browser push unregister: unauthenticated caller")
	}
	installID := strings.TrimSpace(cmd.Body.BrowserInstallID)
	if installID == "" {
		return UnregisterResult{}, fmt.Errorf("browser push unregister: browser_install_id is required")
	}
	if len(installID) > maxBrowserInstallIDLen {
		return UnregisterResult{}, fmt.Errorf("browser push unregister: browser_install_id is too long")
	}
	removed, err := s.repo.MarkUnsubscribed(ctx, tenantID, actorID, installID, s.now().UTC())
	if err != nil {
		return UnregisterResult{}, err
	}
	return UnregisterResult{Removed: removed}, nil
}

// List returns the caller's own browser registrations.
func (s *Service) List(ctx context.Context, tenantID, actorID string) ([]Registration, error) {
	tenantID = strings.TrimSpace(tenantID)
	actorID = strings.TrimSpace(actorID)
	if tenantID == "" || actorID == "" {
		return nil, fmt.Errorf("browser push list: unauthenticated caller")
	}
	return s.repo.ListForMember(ctx, tenantID, actorID)
}

// ResolveBrowserRecipients returns the caller-named person's reachable browsers.
func (s *Service) ResolveBrowserRecipients(ctx context.Context, tenantID, memberOrUserID string) ([]Recipient, error) {
	tenantID = strings.TrimSpace(tenantID)
	memberOrUserID = strings.TrimSpace(memberOrUserID)
	if tenantID == "" || memberOrUserID == "" {
		return nil, nil
	}
	return s.repo.ResolveMemberRecipients(ctx, tenantID, memberOrUserID)
}

// PruneToken retires every registration holding a provider-confirmed dead token.
//
// CALLED FROM THE SEND PATH'S PERMANENT-FAILURE BRANCH, NOT ON A TIMER. A browser push address
// gives no expiry warning: Chrome invalidates it on profile clear, on a long idle stretch, and
// whenever the person revokes the site's notification permission, and tells the server nothing.
// The first and only signal is FCM rejecting the send. If that signal is not turned into a
// status change here, this table grows dead rows forever and every future notification to that
// person burns the full retry schedule against an address that can never resolve.
//
// It is NOT a send failure of any other kind. A timeout, a 5xx, a quota rejection or an
// ambiguous INVALID_ARGUMENT must never reach this function -- the caller
// (isInvalidFCMRecipientResponse in the notification gateway) has already narrowed the input to
// "the provider says this address is gone", and widening it would retire live browsers on a
// transient outage.
func (s *Service) PruneToken(ctx context.Context, tenantID, token, reason string) (int, error) {
	tenantID = strings.TrimSpace(tenantID)
	token = strings.TrimSpace(token)
	if tenantID == "" || token == "" {
		return 0, nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "invalid_push_recipient"
	}
	return s.repo.MarkTokenStale(ctx, tenantID, token, reason, s.now().UTC())
}

func sanitizeRegister(in RegisterRequest) (RegisterRequest, error) {
	out := RegisterRequest{
		BrowserInstallID: strings.TrimSpace(in.BrowserInstallID),
		Token:            strings.TrimSpace(in.Token),
		BrowserLabel:     strings.TrimSpace(in.BrowserLabel),
		UserAgent:        strings.TrimSpace(in.UserAgent),
	}
	if out.BrowserInstallID == "" {
		return RegisterRequest{}, fmt.Errorf("browser push register: browser_install_id is required")
	}
	if len(out.BrowserInstallID) > maxBrowserInstallIDLen {
		return RegisterRequest{}, fmt.Errorf("browser push register: browser_install_id is too long")
	}
	if out.Token == "" {
		return RegisterRequest{}, fmt.Errorf("browser push register: token is required")
	}
	if len(out.Token) > maxTokenLen {
		return RegisterRequest{}, fmt.Errorf("browser push register: token is too long")
	}
	// A push token is a bearer credential: whoever holds it can send to that browser. Newlines
	// and control characters in one are never legitimate and are exactly what a log-injection or
	// header-smuggling attempt looks like, so they are refused rather than stripped.
	if strings.ContainsFunc(out.Token, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return RegisterRequest{}, fmt.Errorf("browser push register: token contains control characters")
	}
	if len(out.UserAgent) > maxUserAgentLen {
		out.UserAgent = out.UserAgent[:maxUserAgentLen]
	}
	if len(out.BrowserLabel) > maxBrowserLabelLen {
		out.BrowserLabel = out.BrowserLabel[:maxBrowserLabelLen]
	}
	return out, nil
}
