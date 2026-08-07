package domain

import "time"

// Vaccination's OWN module-scoped alerts feed, mirroring the weighing feed in
// backend/internal/weighing/domain/alerts.go.
//
// WHY THIS EXISTS: the vaccination lifecycle has been PRODUCING notifications all
// along -- "vaccination.record.closed", "vaccination.proof.rework",
// "vaccination.proof.approved", "vaccination.proof.pending.verifier",
// "vaccination.proof.pending.leadership" -- and nothing on the phone ever read
// them. The Alerts tab was hosted at /vaccination/alerts and bound to the generic
// control-tower ViewModel, whose own doc comment says it "carries no module
// dimension at all". So a verifier's rejection wrote a real notification row that
// the operator's Alerts tab could not see, and the tab rendered whatever the
// control-tower gap summary happened to hold (usually nothing). Observed on
// 2026-08-08 with 41 vaccination.* notification_requests present and an empty
// Alerts tab on all three phones.
//
// Weighing got this feed on 2026-08-03; vaccination was never given the same
// treatment. The two feeds are deliberately SEPARATE per-module reads over the
// same shared notification_requests plumbing -- they share no table beyond that,
// and vaccination must never be served from the weighing reader (or vice versa),
// because the module discriminator is the whole audience contract.
const (
	// AlertRetentionDays bounds the feed to a rolling window so the read can
	// never walk full notification history. Same window as weighing.
	AlertRetentionDays = 30

	// AlertPageSize / MaxAlertPageSize bound one page of the feed. A phone
	// viewport holds ~7-10 rows, so ~20 per keyset page with infinite scroll is
	// the mobile fetch rule; never widen these to avoid paging.
	AlertPageSize    = 20
	MaxAlertPageSize = 50
)

// Direction reads off the RECIPIENT ROLE the producing consumer stamped, never
// off the event type: one transition travels both ways at once (a rework goes
// DOWN to the operator and UP to the director), and each recipient's own row
// must say which way it went for THEM.
const (
	AlertDirectionDownstream = "downstream" // it landed on the person who must do the work
	AlertDirectionUpstream   = "upstream"   // it landed on the person who oversees it
)

const (
	AlertSeverityHigh   = "high"
	AlertSeverityNormal = "normal"
)

// Alert is one row of the vaccination alerts feed.
type Alert struct {
	AlertID string `json:"alert_id"`
	// Kind is the producer's context type. Clients may branch on it for
	// iconography; they must not rebuild the sentence from it -- Title and Body
	// are backend-owned copy per the copy-firewall rule.
	Kind      string `json:"kind"`
	Direction string `json:"direction"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Severity  string `json:"severity"`
	// Target is the in-app destination the producer chose for this transition.
	// Tapping the row goes here.
	Target    string    `json:"target"`
	ShedLabel string    `json:"shed_label,omitempty"`
	DriveID   string    `json:"drive_id,omitempty"`
	ParkID    string    `json:"park_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// AlertPage is the feed response. Title and EmptyMessage are BACKEND-OWNED copy:
// the phone must not hardcode either.
type AlertPage struct {
	Items        []Alert `json:"items"`
	NextCursor   string  `json:"next_cursor,omitempty"`
	Title        string  `json:"title"`
	EmptyMessage string  `json:"empty_message"`
}

// AlertFeedTitle / AlertFeedEmptyMessage are the only copy this surface shows
// when it has no rows. Farm language only -- no internal vocabulary.
// The title is just "Alerts", NOT "Vaccination alerts". The person reading this
// is already standing inside the vaccination module, and naming the module again
// is the exact string AGENTS.md records as a violation -- it was hardcoded in
// AlertsViewModel.kt and removed. Weighing's feed says "Weighing alerts"; that is
// not precedent to copy here, because the recorded decision names this module.
const (
	AlertFeedTitle        = "Alerts"
	AlertFeedEmptyMessage = "No vaccination updates yet. New drives, submitted proofs, and rework requests show up here."
)
