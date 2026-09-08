// Package app resolves an alert's configured designations into reachable devices, and serves
// the admin matrix that edits those designations.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/domain"
	"github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
)

// Scope type strings the workforce resolver understands. A park is a 'center'-scoped position
// (see workforce_positions.scope_type); tenant desks are 'tenant'-scoped grants or positions.
const (
	scopeTypeTenant = "tenant"
	scopeTypeCenter = "center"
)

// roleLabelCEO is the observability label the queue has always recorded for the CEO desk
// (calendarports.NotificationRecipient.RoleLabel is diagnostics, never routing). Kept so a
// notification row written through this resolver reads exactly as one written before it.
const roleLabelCEO = "ceo"

// Resolver answers "who hears this alert" for one tenant: the stored override when the tenant
// has customised the alert, the catalog default otherwise, each designation resolved to
// devices through the workforce resolver at the right scope and deduped by device.
//
// A nil AudienceRepository is legal and means "catalog defaults only". That is what lets every
// existing notifier test keep its roster fake and its expectations: the default audience of
// each alert is byte-for-byte the audience the notifier resolved by hand before.
type Resolver struct {
	positions     ports.PositionRecipientResolver
	subscriptions ports.AudienceRepository
	logger        *slog.Logger
}

// NewResolver builds a resolver over the workforce position resolver. Attach the stored
// override with WithSubscriptions; without it the resolver serves catalog defaults.
func NewResolver(positions ports.PositionRecipientResolver) *Resolver {
	return &Resolver{positions: positions}
}

// WithSubscriptions attaches the per-tenant override. Chainable at construction time.
func (r *Resolver) WithSubscriptions(repo ports.AudienceRepository) *Resolver {
	r.subscriptions = repo
	return r
}

// WithLogger attaches a logger for the no-recipient warnings.
func (r *Resolver) WithLogger(logger *slog.Logger) *Resolver {
	r.logger = logger
	return r
}

// Designations returns the effective designation list for one alert: the override when one
// is stored, the catalog default otherwise. An unknown alert key is ports.ErrUnknownAlert --
// a wiring defect the caller must surface, never an empty audience.
func (r *Resolver) Designations(ctx context.Context, tenantID, alertKey string) ([]string, error) {
	alert, ok := domain.AlertByKey(alertKey)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ports.ErrUnknownAlert, alertKey)
	}
	if r.subscriptions != nil {
		stored, customised, err := r.subscriptions.LoadAudience(ctx, tenantID, alert.Key)
		if err != nil {
			return nil, fmt.Errorf("notification audience %s: load override: %w", alert.Key, err)
		}
		if customised {
			return append([]string(nil), stored.DesignationCodes...), nil
		}
	}
	return append([]string(nil), alert.DefaultDesignations...), nil
}

// Recipients resolves the alert's effective designations to reachable devices.
//
// parkID scopes the park desks (park_head, verifier, operator, procurement_manager). An alert
// raised without a park -- the daily low-stock run, a tenant-wide checkpoint -- passes "" and
// any park desk on its audience resolves to nobody for that designation rather than guessing a
// park. Tenant desks always resolve at tenant scope.
//
// One workforce read per designation, bounded by the designation catalog (a dozen job
// titles), never by herd or roster size; a caller that queues many notifications from one
// audience must resolve once and reuse the slice, exactly as it did before.
func (r *Resolver) Recipients(ctx context.Context, tenantID, parkID, alertKey string) ([]calendarports.NotificationRecipient, error) {
	if r == nil || r.positions == nil {
		return nil, fmt.Errorf("notification audience %s: resolver not configured", alertKey)
	}
	tenantID = strings.TrimSpace(tenantID)
	parkID = strings.TrimSpace(parkID)
	designations, err := r.Designations(ctx, tenantID, alertKey)
	if err != nil {
		return nil, err
	}
	return r.resolveDesignations(ctx, tenantID, parkID, alertKey, designations, nil)
}

// Addressed resolves an alert that is addressed to specific people rather than to a desk: the CXO
// a leadership task names, the verifier on duty for a park's videos. The addressed devices are
// KEPT when any of the addressee's own designations is ticked for the alert, and every OTHER ticked
// designation is resolved as an ordinary audience and added as a copy. Unticking the addressee's
// title is therefore the yes/no switch the matrix promises, and it silences nobody else.
func (r *Resolver) Addressed(ctx context.Context, tenantID, parkID, alertKey string, addressee []string, addressed []calendarports.NotificationRecipient) ([]calendarports.NotificationRecipient, error) {
	if r == nil || r.positions == nil {
		return nil, fmt.Errorf("notification audience %s: resolver not configured", alertKey)
	}
	designations, err := r.Designations(ctx, strings.TrimSpace(tenantID), alertKey)
	if err != nil {
		return nil, err
	}
	isAddressee := map[string]struct{}{}
	for _, code := range addressee {
		isAddressee[strings.TrimSpace(code)] = struct{}{}
	}
	keep := false
	others := make([]string, 0, len(designations))
	for _, code := range designations {
		if _, ok := isAddressee[strings.TrimSpace(code)]; ok {
			keep = true
			continue
		}
		others = append(others, code)
	}
	var seed []calendarports.NotificationRecipient
	if keep {
		seed = addressed
	}
	return r.resolveDesignations(ctx, strings.TrimSpace(tenantID), strings.TrimSpace(parkID), alertKey, others, seed)
}

// resolveDesignations turns designation codes into deduped devices, seeded with any already
// resolved recipients (an addressed person) so a device is never pushed twice.
func (r *Resolver) resolveDesignations(ctx context.Context, tenantID, parkID, alertKey string, designations []string, seed []calendarports.NotificationRecipient) ([]calendarports.NotificationRecipient, error) {
	out := make([]calendarports.NotificationRecipient, 0, 4+len(seed))
	seen := map[string]struct{}{}
	for _, recipient := range seed {
		key := strings.TrimSpace(recipient.DeviceID)
		if key == "" {
			key = strings.TrimSpace(recipient.FCMToken)
		}
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, recipient)
	}
	// scale-guard:ignore: bounded by the designation catalog (a dozen job titles), not by data.
	for _, code := range designations {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		scopeType, scopeID := scopeTypeTenant, tenantID
		if domain.DesignationScope(code) == domain.ScopePark {
			if parkID == "" {
				continue
			}
			scopeType, scopeID = scopeTypeCenter, parkID
		}
		devices, err := r.positions.ResolvePositionRecipients(ctx, tenantID, scopeType, scopeID, code)
		if err != nil {
			return nil, fmt.Errorf("notification audience %s: resolve %s: %w", alertKey, code, err)
		}
		label := code
		if code == domain.DesignationCEO {
			label = roleLabelCEO
		}
		for _, device := range devices {
			key := strings.TrimSpace(device.DeviceID)
			if key == "" {
				key = strings.TrimSpace(device.FCMToken)
			}
			if key == "" {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, calendarports.NotificationRecipient{
				MemberID:  device.WorkforceMemberID,
				DeviceID:  device.DeviceID,
				FCMToken:  device.FCMToken,
				RoleLabel: label,
			})
		}
	}
	if len(out) == 0 && r.logger != nil {
		// Loud, and no fallback: an alert nobody receives must not look sent. Whether that is a
		// deliberate empty audience or an empty desk, the log names the alert so it can be found.
		r.logger.WarnContext(ctx, "notification_audience_no_recipients",
			"tenant_id", tenantID, "park_id", parkID, "alert_key", alertKey, "designations", designations)
	}
	return out, nil
}
