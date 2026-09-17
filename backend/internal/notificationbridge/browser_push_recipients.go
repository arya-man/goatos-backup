package notificationbridge

import (
	"context"
	"log/slog"

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
// (ResolvePositionRecipients, ResolveModuleDutyRecipients) resolve a desk, not a person, and
// their SQL returns devices without ever naming the member set it drew them from. There is no
// member list to re-ask the browser registry with, so those paths are passed through UNCHANGED
// and a position-addressed alert still reaches phones only. The leadership-task pushes -- the
// ones the CEOs actually care about, and the ones this change exists for -- are addressed by
// person and go through ResolveMemberRecipients, which IS covered.
type browserRecipientSource interface {
	ResolveBrowserRecipients(ctx context.Context, tenantID, memberOrUserID string) ([]browserpush.Recipient, error)
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
	if len(browsers) == 0 {
		return devices, nil
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
	return combined, nil
}
