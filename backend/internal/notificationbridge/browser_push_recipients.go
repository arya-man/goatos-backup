package notificationbridge

import (
	"context"
	"log/slog"
	"time"

	"github.com/vgoats/goatos/backend/internal/browserpush"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
)

// THE DASHBOARD IS A DEVICE (2026-09-18).
//
// The CEO and the CxOs do their day in Chrome on admin-web. Every push in this package resolved
// its recipients through the workforce roster's device registry, which is Android-only by its own
// CHECK constraint (`platform = 'android'`), so a leadership task raised at 11:40 reached phones
// and left the one surface those people actually have open all day silent.
//
// WHY A DECORATOR AND NOT A CHANGE TO THE ROSTER QUERY. `pushReachableDeviceSQL` is deliberately
// the ONE definition of "a device we can reach" (its own comment says so, and the third condition
// it carries went missing once already when that definition was copied). Teaching it to UNION a
// second table would make every one of the ~20 recipient queries that splice it read a table
// belonging to another module, and would make the browser registry's reachability rules a
// property of workforce's SQL rather than of the module that owns them.
//
// Instead this wraps the resolver at the seam every consumer already talks to. Consequences worth
// stating plainly:
//
//   - IT IS GENERIC OVER notification_requests, not over leadership tasks. It adds recipients;
//     the caller then writes the same QueueRoleNotifications rows it always did, on the same
//     'push_fcm' channel, with the same copy. A notification type added by another module --
//     including the mention type being added alongside this change -- reaches a subscribed
//     browser without that module knowing browsers exist, and this file does not import it.
//   - IT IS ADDITIVE AND FAILS OPEN. A browser lookup that errors is LOGGED and the phone
//     recipients are returned unchanged. A person's phone notification must never be lost because
//     the browser registry was unavailable; a browser copy is the newer, lesser half.
//   - DeviceID CARRIES THE BROWSER REGISTRATION ID. The downstream idempotency key is per
//     (tenant, event, type, completion, device), so a person's phone and each of their browsers
//     each get their own row instead of racing for one. A registration id is a uuid from a
//     different table than device_id, and that column is an unconstrained text key downstream --
//     collision is impossible and no FK is implied.
//
// WHAT IT DOES NOT COVER, and this is the honest limit: the POSITION and MODULE-DUTY paths
// used to cover only ResolveMemberRecipients. That left leadership and park-desk alerts visible in
// the dashboard drawer but silent in Chrome, because those paths are addressed by position or
// module duty. The browser registry now mirrors the same audience reads, so all three resolver
// shapes can add browser addresses without each notifier learning about browsers.
type browserRecipientSource interface {
	ResolveBrowserRecipients(ctx context.Context, tenantID, memberOrUserID string) ([]browserpush.Recipient, error)
	ResolveModuleDutyBrowserRecipients(ctx context.Context, tenantID, scopeType, scopeID, moduleCode, dutyType string, at time.Time) ([]browserpush.Recipient, error)
	ResolvePositionBrowserRecipients(ctx context.Context, tenantID, scopeType, scopeID, positionCode string, at time.Time) ([]browserpush.Recipient, error)
}

// NoBrowsers is the browser source for a process that has no database to read registrations
// from (the pool-less dispatch tests): the decorator still stands at the seam, it simply finds
// no browsers. Production composition never uses it -- a consumer built on the raw roster is a
// test failure (TestEveryNotifierInAnAsyncProcessReachesBrowsers) precisely so the seam cannot
// be skipped by accident.
type NoBrowsers struct{}

func (NoBrowsers) ResolveBrowserRecipients(context.Context, string, string) ([]browserpush.Recipient, error) {
	return nil, nil
}

func (NoBrowsers) ResolveModuleDutyBrowserRecipients(context.Context, string, string, string, string, string, time.Time) ([]browserpush.Recipient, error) {
	return nil, nil
}

func (NoBrowsers) ResolvePositionBrowserRecipients(context.Context, string, string, string, string, time.Time) ([]browserpush.Recipient, error) {
	return nil, nil
}

// BrowserAwareRecipients decorates a RecipientResolver so a person's reachable browsers are
// returned beside their reachable phones.
type BrowserAwareRecipients struct {
	RecipientResolver
	browsers browserRecipientSource
	logger   *slog.Logger
}

var _ RecipientResolver = (*BrowserAwareRecipients)(nil)

// WithBrowserRecipients wraps a resolver. A nil browser source returns the resolver untouched, so
// a process that has not wired the browser registry behaves exactly as it did before.
func WithBrowserRecipients(recipients RecipientResolver, browsers browserRecipientSource, logger *slog.Logger) RecipientResolver {
	if recipients == nil || browsers == nil {
		return recipients
	}
	return &BrowserAwareRecipients{RecipientResolver: recipients, browsers: browsers, logger: logger}
}

// ResolveMemberRecipients returns the person's phones plus their subscribed browsers.
func (r *BrowserAwareRecipients) ResolveMemberRecipients(ctx context.Context, tenantID, memberOrUserID string) ([]workforcedomain.NotificationRecipient, error) {
	devices, err := r.RecipientResolver.ResolveMemberRecipients(ctx, tenantID, memberOrUserID)
	if err != nil {
		// The phone lookup failing is the caller's error to handle; a browser copy cannot
		// substitute for a recipient list nobody could read.
		return nil, err
	}
	browsers, browserErr := r.browsers.ResolveBrowserRecipients(ctx, tenantID, memberOrUserID)
	if browserErr != nil {
		if r.logger != nil {
			r.logger.WarnContext(ctx, "browser_push_recipients_unavailable",
				slog.String("tenant_id", tenantID),
				slog.String("error", browserErr.Error()),
			)
		}
		return devices, nil
	}
	return mergeBrowserRecipients(devices, browsers), nil
}

// ResolveModuleDutyRecipients returns module-duty phones plus subscribed browsers for the same
// duty audience.
func (r *BrowserAwareRecipients) ResolveModuleDutyRecipients(ctx context.Context, tenantID, scopeType, scopeID, moduleCode, dutyType string) ([]workforcedomain.NotificationRecipient, error) {
	devices, err := r.RecipientResolver.ResolveModuleDutyRecipients(ctx, tenantID, scopeType, scopeID, moduleCode, dutyType)
	if err != nil {
		return nil, err
	}
	browsers, browserErr := r.browsers.ResolveModuleDutyBrowserRecipients(ctx, tenantID, scopeType, scopeID, moduleCode, dutyType, time.Now())
	if browserErr != nil {
		r.logBrowserError(ctx, tenantID, browserErr)
		return devices, nil
	}
	return mergeBrowserRecipients(devices, browsers), nil
}

// ResolvePositionRecipients returns position phones plus subscribed browsers for the same position
// or role-grant audience.
func (r *BrowserAwareRecipients) ResolvePositionRecipients(ctx context.Context, tenantID, scopeType, scopeID, positionCode string) ([]workforcedomain.NotificationRecipient, error) {
	devices, err := r.RecipientResolver.ResolvePositionRecipients(ctx, tenantID, scopeType, scopeID, positionCode)
	if err != nil {
		return nil, err
	}
	browsers, browserErr := r.browsers.ResolvePositionBrowserRecipients(ctx, tenantID, scopeType, scopeID, positionCode, time.Now())
	if browserErr != nil {
		r.logBrowserError(ctx, tenantID, browserErr)
		return devices, nil
	}
	return mergeBrowserRecipients(devices, browsers), nil
}

func (r *BrowserAwareRecipients) logBrowserError(ctx context.Context, tenantID string, err error) {
	if r.logger == nil {
		return
	}
	r.logger.WarnContext(ctx, "browser_push_recipients_unavailable",
		slog.String("tenant_id", tenantID),
		slog.String("error", err.Error()),
	)
}

func mergeBrowserRecipients(devices []workforcedomain.NotificationRecipient, browsers []browserpush.Recipient) []workforcedomain.NotificationRecipient {
	if len(browsers) == 0 {
		return devices
	}
	combined := make([]workforcedomain.NotificationRecipient, 0, len(devices)+len(browsers))
	combined = append(combined, devices...)
	// A token already present on a phone row is skipped. It should not happen -- an Android FCM
	// token and a web one are minted for different apps -- but the cost of the check is one map
	// and the cost of being wrong is the same person notified twice for one event.
	seen := make(map[string]struct{}, len(devices))
	for _, device := range devices {
		seen[device.FCMToken] = struct{}{}
	}
	for _, browser := range browsers {
		if browser.Token == "" {
			continue
		}
		if _, duplicate := seen[browser.Token]; duplicate {
			continue
		}
		seen[browser.Token] = struct{}{}
		combined = append(combined, workforcedomain.NotificationRecipient{
			WorkforceMemberID: browser.WorkforceMemberID,
			DeviceID:          browser.BrowserRegistrationID,
			FCMToken:          browser.Token,
		})
	}
	return combined
}
