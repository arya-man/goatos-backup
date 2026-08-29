package domain

// Feed DISTRIBUTION verification gate coordinates (maintainer decision, 2026-07-26). A
// feed-direction shed-session now passes through the generic Verification module before it counts as
// done: the operator completes with a MANDATORY feed-distribution video + a MANDATORY
// water-distribution video, which enqueues one verification item, and the session is 'completed'
// only when a verifier approves.
//
// These are the (vertical, module, category, ref_type) the generic verification module stores as a
// back-pointer. The consumer (FeedDistributionVerificationHandler) filters verdict events on
// module + ref_type so a vaccination/shifting/etc. verdict is ignored. Kept in the feed domain
// package (a neutral core package) so producer, consumer, and bridge share ONE definition.
//
// See docs/decisions/feed-distribution-verification.md. This is entirely separate from the untouched
// feed PACKING path (feed_direction_session_completions + feed.direction.completed), which stays
// instant, optional-video, and unverified.
const (
	VerificationVerticalFeed = "feed"
	VerificationModuleFeed   = "feed"
	VerificationCategoryFeed = "feed_distribution"
	VerificationRefTypeFeed  = "feed_distribution_completion"

	// DistributionStatusPendingVerification is operator-completed-but-not-yet-verified: all proofs are
	// stored, a verification item is queued, and NOTHING is completed yet.
	DistributionStatusPendingVerification = "pending_verification"
	// DistributionStatusCompleted is verifier-approved: the session is done NOW and
	// feed.distribution.completed is emitted.
	DistributionStatusCompleted = "completed"
	// DistributionStatusRework is verifier-rejected: the operator re-records and re-submits, which
	// returns the row to pending_verification.
	DistributionStatusRework = "rework"
)

const (
	VerificationCategoryTransport  = "feed_transport"
	VerificationRefTypeTransport   = "feed_transport_attempt"
	TransportStatusDue             = "due"
	TransportStatusVerificationDue = "verification_due"
	TransportStatusRework          = "rework"
	TransportStatusCompleted       = "completed"
)

// Feed PACKING verification gate coordinates (maintainer decision, 2026-07-26, SUPERSEDING the
// "packing stays instant, no verifier" rule). A feed PACKING shed-session now passes through the
// generic Verification module too: the operator completes with ONE MANDATORY packing video, which
// enqueues one verification item, and the session is 'completed' only when a verifier approves.
//
// Same (vertical, module) as distribution -- both are the "feed" module -- but a DISTINCT category and
// ref_type so a distribution verdict and a packing verdict never cross-fire. The consumer
// (FeedPackingVerificationHandler) filters verdict events on module + ref_type. This writes to a
// SEPARATE table (feed_packing_completions); it is NOT the old instant feed_direction_session_completions
// path, which is left inert.
// Feed WASTAGE verification gate coordinates (maintainer decision, 2026-08-18). Feed Wastage is a
// daily EXPERIMENT-pen task: each pen on a hand-authored feed experiment owes ONE wastage video per
// feed day. The operator films the leftover feed and submits; the verifier watches the clip, RECORDS
// the leftover weight she can read in it (the measurement rides the producer's own route, declared
// to clients via the category's MeasurementCorrection spec), and approves -- or rejects for a
// re-shoot when the value is not readable.
//
// Same (vertical, module) as distribution/packing/transport -- all are the "feed" module -- but a
// DISTINCT category and ref_type so the four feed gates never cross-fire. The consumer
// (FeedWastageVerificationHandler) filters verdict events on module + ref_type.
const (
	VerificationCategoryWastage = "feed_wastage"
	VerificationRefTypeWastage  = "feed_wastage_completion"

	// WastageStatusPendingVerification is operator-completed-but-not-yet-verified: the video is
	// stored, a verification item is queued, and NOTHING is completed yet.
	WastageStatusPendingVerification = "pending_verification"
	// WastageStatusCompleted is verifier-approved: the pen's wastage for that feed day is done NOW
	// and feed.wastage.completed is emitted.
	WastageStatusCompleted = "completed"
	// WastageStatusRework is verifier-rejected: the operator re-records and re-submits, which
	// returns the row to pending_verification.
	WastageStatusRework = "rework"
)

const (
	VerificationCategoryPacking = "feed_packing"
	VerificationRefTypePacking  = "feed_packing_completion"

	// PackingStatusPendingVerification is operator-completed-but-not-yet-verified: the video is stored, a
	// verification item is queued, and NOTHING is completed yet.
	PackingStatusPendingVerification = "pending_verification"
	// PackingStatusCompleted is verifier-approved: the packing session is done NOW and
	// feed.packing.completed is emitted.
	PackingStatusCompleted = "completed"
	// PackingStatusRework is verifier-rejected: the operator re-records and re-submits, which returns the
	// row to pending_verification.
	PackingStatusRework = "rework"
)
