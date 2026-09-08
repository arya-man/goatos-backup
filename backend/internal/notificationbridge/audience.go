package notificationbridge

import (
	"context"
	"log/slog"

	calendarports "github.com/vgoats/goatos/backend/internal/calendar/ports"
	notificationaudienceapp "github.com/vgoats/goatos/backend/internal/notificationaudience/app"
	notificationaudienceports "github.com/vgoats/goatos/backend/internal/notificationaudience/ports"
)

// WHO HEARS A LEADERSHIP PUSH IS CONFIG, PER DESIGNATION (maintainer decision 2026-09-08).
//
// Every upward push in this package -- the daily alerts, the proof lifecycle copies to
// directors, the weighing lifecycle notices, the missed-work escalation -- used to resolve its
// audience by hand from literal position codes. The audience of each is now an ALERT KEY in
// notificationaudience/domain's catalog, resolved through one AudienceResolver: the tenant's
// stored override when an admin has customised the alert on People / HRMS -> Notifications,
// the catalog default otherwise. The default of every alert is exactly the audience this
// package resolved by hand before, so nothing changes on deploy.
//
// The person-addressed pushes (the operator whose work bounced, the packer whose bag was
// reopened, the park head whose pen visit is due) are NOT designation questions and stay on
// ResolveMemberRecipients. The verifier's own queue push stays on the module verify duty.
//
// Two wiring rules, both pinned by audience_wiring_test.go:
//
//  1. Every constructor here builds a DEFAULT resolver from its RecipientResolver, so a
//     consumer that is never given the stored override still resolves the catalog default --
//     never nobody. That is what keeps every roster-fake test in this package valid.
//  2. Every PRODUCTION construction site chains .WithAudience(NewStoredAudience(...)); a site
//     that forgets serves defaults forever and the matrix silently does nothing for it.
type AudienceResolver interface {
	// Recipients resolves one alert's effective designations to reachable, deduped devices.
	// parkID scopes the park desks; "" resolves tenant desks only.
	Recipients(ctx context.Context, tenantID, parkID, alertKey string) ([]calendarports.NotificationRecipient, error)
	// Addressed gates an alert addressed to specific people (a task's CXO, the verifier on duty):
	// the addressed devices are kept when one of the addressee designations is ticked, and every
	// other ticked designation receives a copy.
	Addressed(ctx context.Context, tenantID, parkID, alertKey string, addressee []string, addressed []calendarports.NotificationRecipient) ([]calendarports.NotificationRecipient, error)
}

// defaultAudience is the catalog-defaults-only resolver every constructor starts with.
func defaultAudience(recipients RecipientResolver) AudienceResolver {
	return notificationaudienceapp.NewResolver(recipients)
}

// NewStoredAudience builds the production resolver: catalog defaults overridden by the tenant's
// stored per-alert designations. Every production wiring site passes it to WithAudience.
func NewStoredAudience(recipients notificationaudienceports.PositionRecipientResolver, stored notificationaudienceports.AudienceRepository, logger *slog.Logger) AudienceResolver {
	return notificationaudienceapp.NewResolver(recipients).WithSubscriptions(stored).WithLogger(logger)
}
