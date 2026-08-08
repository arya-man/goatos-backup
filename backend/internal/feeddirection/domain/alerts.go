package domain

import "time"

// Feed's OWN module-scoped alerts feed, mirroring the weighing feed
// (backend/internal/weighing/domain/alerts.go) and the vaccination feed
// (backend/internal/vaccinationexecution/domain/alerts.go).
//
// WHY THIS EXISTS: backend/internal/notificationbridge/verification_notify_consumer.go has been
// queuing feed.proof.pending.verifier / feed.proof.pending.leadership / feed.proof.approved /
// feed.proof.rework / feed.record.closed notification_requests rows for the verifier, the
// feed_director, the park_head, and the CEO ever since the feed-distribution and feed-packing
// verification gates shipped (2026-07-26) -- and nothing on the phone or admin-web ever read them
// back. There was no GET /app/feed/alerts route, so every one of those rows sat in
// notification_requests unread. Weighing and vaccination each got this feed already; feed did not.
//
// This feed is deliberately a SEPARATE per-module read over the same shared notification_requests
// plumbing table -- it shares no table beyond that with weighing/vaccination/counts, and must never
// be served from another module's reader (or vice versa), because the module discriminator
// (context->>'message_key' LIKE 'feed.%') is the whole audience contract.
const (
	// AlertRetentionDays bounds the feed to a rolling window so the read can never walk full
	// notification history. Same window as weighing/vaccination.
	AlertRetentionDays = 30

	// AlertPageSize / MaxAlertPageSize bound one page of the feed. Same mobile fetch rule as the
	// other two module feeds: ~20 rows with infinite scroll.
	AlertPageSize    = 20
	MaxAlertPageSize = 50
)

// Direction reads off the RECIPIENT ROLE the producing consumer stamped, never off the event type:
// the same transition can travel both ways at once (a rework goes DOWN to the operator and UP to
// the director), and each recipient's own row must say which way it went for THEM.
const (
	AlertDirectionDownstream = "downstream" // it landed on the person who must do the work
	AlertDirectionUpstream   = "upstream"   // it landed on the person who oversees it
)

const (
	AlertSeverityHigh   = "high"
	AlertSeverityNormal = "normal"
)

// Alert is one row of the feed alerts feed.
type Alert struct {
	AlertID string `json:"alert_id"`
	// Kind is the producer's context type. Clients may branch on it for iconography; they must not
	// rebuild the sentence from it -- Title and Body are backend-owned copy per the copy-firewall
	// rule.
	Kind      string `json:"kind"`
	Direction string `json:"direction"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Severity  string `json:"severity"`
	// Target is the in-app destination the producer chose for this transition. Tapping the row
	// goes here.
	Target     string    `json:"target"`
	ShedLabel  string    `json:"shed_label,omitempty"`
	ParkID     string    `json:"park_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// AlertPage is the feed response. Title and EmptyMessage are BACKEND-OWNED copy: the phone must
// not hardcode either.
type AlertPage struct {
	Items        []Alert `json:"items"`
	NextCursor   string  `json:"next_cursor,omitempty"`
	Title        string  `json:"title"`
	EmptyMessage string  `json:"empty_message"`
}

// AlertFeedTitle / AlertFeedEmptyMessage are the only copy this surface shows when it has no
// rows. Farm language only -- no internal vocabulary.
const (
	AlertFeedTitle        = "Feed alerts"
	AlertFeedEmptyMessage = "No feed updates yet. New drives, submitted proofs, and rework requests show up here."
)
