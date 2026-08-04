package domain

import "time"

// WEIGHING ALERTS — the module-scoped lifecycle feed.
//
// WHAT AN ALERT IS (maintainer definition, 2026-08-03): "alerts are specific to
// weighing -- to show if a task is created (downstream alert), or if a task is
// submitted or reopened (upstream or downstream alerts)." So this is the
// work-state transitions of the weighing module, routed to whoever owns the next
// action. It is NOT a generic notification dump, and it is NOT the vaccination
// process-integrity feed at /alerts (which needs ObligationRead+VaccinationRead
// and whose label reads "Vaccination alerts" in all four locales).
//
// WHERE THE ROWS COME FROM. Nothing new is produced. Every weighing lifecycle
// transition ALREADY writes a durable, idempotent notification_requests row via
// the two weighing notification consumers
// (backend/internal/notificationbridge/weighing_lifecycle_notify_consumer.go and
// weighing_submission_notify_consumer.go) plus the weighing profile of the shared
// verification consumer. Those consumers already resolved WHO owns the next
// action -- ResolveMemberRecipients for the assignment-row operator,
// ResolvePositionRecipients for the growth_director / ceo_internal seats -- and
// wrote one row per recipient device.
//
// This feed is therefore the READ of routing that already happened: "the alerts
// that were sent to me". That is why it needs no second audience model and
// cannot leak another operator's buckets -- an operator was never a recipient of
// them in the first place. See docs/decisions/2026-08-03-weighing-alerts-feed.md.
//
// HONEST LIMIT: a person with no push-registered device has no rows, because
// QueueRoleNotifications writes per device. The phone registers a device at
// sign-in, so field operators and phone-using leadership always have one; a
// browser-only seat does not.

const (
	// AlertPageSize / MaxAlertPageSize bound one page of the feed. The list is a
	// phone surface, so the default stays inside the ~20-rows-per-screen mobile
	// list rule (docs/decisions/mobile-list-fetch-pagination.md).
	AlertPageSize    = 20
	MaxAlertPageSize = 50

	// AlertRetentionDays bounds the feed to a rolling window so the read can
	// never degenerate into a full-history scan as notification_requests grows.
	// An alert is a "what changed, act now" signal; a month-old assignment is
	// history, and history lives on the weighing task surfaces.
	AlertRetentionDays = 30

	// AlertDirection values are the maintainer's own vocabulary. They are derived
	// from the RECIPIENT ROLE the notification consumer stamped on the row, never
	// from the event type, so a row always says who it travelled to.
	AlertDirectionDownstream = "downstream" // it landed on the person who must do the work
	AlertDirectionUpstream   = "upstream"   // it landed on the person who oversees the work

	AlertSeverityHigh   = "high"
	AlertSeverityNormal = "normal"
)

// Alert is one weighing work-state transition that was routed to the caller.
// Every human-visible string is authored HERE or by the notification consumer
// that produced the row; renderers never compose copy.
type Alert struct {
	AlertID string `json:"alert_id"`
	// Kind is the producer's context type, e.g. "weighing_campaign_published",
	// "weighing_shed_submitted", "weighing_shed_reopened". Clients may branch on
	// it for iconography; they must not rebuild the sentence from it.
	Kind      string `json:"kind"`
	Direction string `json:"direction"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Severity  string `json:"severity"`
	// Target is the in-app destination the producer chose for this transition
	// (e.g. "/weighing"). Tapping the row goes here.
	Target     string    `json:"target"`
	ShedLabel  string    `json:"shed_label,omitempty"`
	CampaignID string    `json:"campaign_id,omitempty"`
	ParkID     string    `json:"park_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// AlertPage is the feed response. Title and EmptyMessage are BACKEND-OWNED copy:
// the phone must not hardcode either, so the surface can be renamed or
// re-scoped without shipping an app build.
type AlertPage struct {
	Items        []Alert `json:"items"`
	NextCursor   string  `json:"next_cursor,omitempty"`
	Title        string  `json:"title"`
	EmptyMessage string  `json:"empty_message"`
}

// AlertFeedTitle / AlertFeedEmptyMessage are the only copy this surface shows
// when it has nothing to render. Farm/product language, never module internals.
const (
	AlertFeedTitle        = "Weighing alerts"
	AlertFeedEmptyMessage = "No weighing updates yet. New work, submissions, and reopened sheds show up here."
)
