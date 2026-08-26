package domain

import (
	"crypto/md5"
	"encoding/hex"
	"strconv"
	"strings"
)

// RANDOMIZED VERIFICATION SAMPLING (maintainer decision 2026-08-26).
//
// The CEO sets, per category, what PERCENTAGE of that category's proof videos the verifier has to
// watch. The rest are settled by the policy without her. Her day is complete when she has cleared
// HER SHARE: at 40%, reviewing those 40% IS 100% of her work.
//
// Verification stays ignorant of what any category MEANS here, exactly as it is everywhere else in
// this module -- the percentage is keyed by the registry's own category token, so a new producer
// gets a Randomization row by registering, with no code change here.

// DefaultSamplePercent is what an unset category resolves to: verify everything. Sampling is
// opt-in per category, so a tenant that never opens the Randomization panel behaves exactly as it
// did before this feature existed.
const DefaultSamplePercent = 100

// SamplingBucketCount is the number of buckets one item's stable draw is spread over. 100 so the
// bucket reads directly as a percentile: bucket < percent is "in sample", and nothing has to be
// scaled or rounded.
const SamplingBucketCount = 100

// AutoResolutionNotSampled is verification_items.auto_resolution for an item the policy settled
// because it was not drawn. The item is APPROVED (sampling decides what gets watched; a video
// nobody watched can never be evidence the work was wrong) and carries no verified_by, so it
// cannot appear in any per-verifier aggregate as work she did.
const AutoResolutionNotSampled = "not_sampled"

// SamplingBucket is the item's stable 0..99 draw.
//
// It MIRRORS the SQL generated column in migration 000214 byte for byte:
//
//	mod(('x' || substr(md5(item_id::text), 1, 6))::bit(24)::int, 100)
//
// i.e. the first 24 bits of md5(item_id) read big-endian, modulo 100. The database is the
// authority -- the column is GENERATED, so every row has one whether or not any Go code ran -- and
// this function exists so tests and any future in-process decision agree with it rather than
// inventing a second draw. TestSamplingBucketMatchesTheGeneratedColumn pins the two together.
//
// The id is lower-cased first because md5 is byte-sensitive and Postgres renders uuid::text in
// lower case; an upper-case id from a client would otherwise draw a different bucket than its own
// row.
func SamplingBucket(itemID string) int {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(itemID)))) //nolint:gosec // not a security hash: a stable, SQL-reproducible draw.
	first24 := hex.EncodeToString(sum[:])[:6]
	value, err := strconv.ParseInt(first24, 16, 64)
	if err != nil {
		// Unreachable: 6 hex characters always parse. Falling back to bucket 0 keeps the item IN
		// sample for any non-zero percentage, which is the safe direction -- a draw that cannot be
		// computed sends the video to a human rather than waiving it.
		return 0
	}
	return int(value % SamplingBucketCount)
}

// InSample reports whether an item with this bucket is drawn at this percentage.
//
// STRICTLY LESS THAN, which is what makes the setting monotonic: every bucket in sample at 40 is
// still in sample at 60, so RAISING the percentage mid-day only ever ADDS videos to her queue and
// can never retract one she is already holding or has already reviewed. 0 draws nothing, 100 draws
// everything.
func InSample(bucket, percent int) bool {
	return bucket < percent
}

// SamplingWaivable reports whether an unsampled item in this category may be settled by the policy
// without a verifier.
//
// FALSE where the verifier is the DATA SOURCE rather than a check: a category whose approve is
// REQUIRED to carry a measurement (feed packing's per-item packed quantities, feed wastage's
// leftover weight) has an operator who submitted a video AND NO NUMBER, so waiving the review
// would complete that pen-day with no quantity recorded at all -- and the producer's own applier
// refuses an approve carrying none, which would strand the item mid-apply instead.
//
// Those categories therefore stay at 100% and their Randomization row is shown locked with the
// reason below. This is derived from the registry entry the producer already wrote, never a
// hardcoded category list: a future producer that declares a required measurement is locked
// automatically, and one that stops declaring it becomes samplable with no change here.
func (d CategoryDefinition) SamplingWaivable() bool {
	return d.MeasurementCorrection == nil || !d.MeasurementCorrection.RequiredForApprove
}

// SamplingLockedReason is the backend-owned farm-language sentence shown on a locked Randomization
// row. It says WHY rather than "not available": the CEO is owed the reason a number he can set
// everywhere else is refused here.
const SamplingLockedReason = "The verifier records the measured quantity for this work, so every video has to be watched."

// SamplingCategory is one row of the Randomization panel: what the CEO set, and how the day is
// going against it.
//
// Every label on it is registry copy resolved by the service. The renderer composes no vocabulary
// of its own -- category tokens like feed_packing are config vocabulary and are banned from
// visible UI by the copy firewall.
type SamplingCategory struct {
	Category    string
	ModuleKey   string
	ModuleLabel string
	PageLabel   string
	PageOrder   int
	// SamplePercent is the percentage in force for the requested business date: the newest policy
	// row on or before that date, or DefaultSamplePercent when the CEO has never set one.
	SamplePercent int
	// Waivable is false for a category whose approve must carry a measurement; see
	// CategoryDefinition.SamplingWaivable. LockedReason is non-empty exactly when this is false.
	Waivable     bool
	LockedReason string
	// SetByName / SetAt describe the standing policy row, empty when none has ever been set.
	SetByName string
	SetAt     string
	// EffectiveFrom is the business date (YYYY-MM-DD) the standing row was written for, empty when
	// no row exists.
	EffectiveFrom string
	// Stats are whole-day aggregates for the requested business date, never a page-local recount.
	Stats SamplingDayStats
}

// SamplingDayStats is one category's day, at the item grain.
//
// The four counts are NOT disjoint and must not be added together: Selected is a subset of
// Captured, and Reviewed is a subset of Selected. They are stated separately because the CEO's
// question ("is she keeping up with the share I set?") and the auditor's question ("how much of
// the day did a human actually watch?") are different questions.
type SamplingDayStats struct {
	// Captured is every item of this category captured on the business date, whatever its status.
	Captured int
	// Selected is the subset drawn for review at the day's percentage -- HER share.
	Selected int
	// Reviewed is the drawn items a verifier has decided (approved or rejected). Withdrawn items
	// are excluded from both Selected and Reviewed: a withdrawn item's source record was
	// superseded, so it is not work anybody owes.
	Reviewed int
	// AutoAccepted is the items the policy settled because they were not drawn. It rises as the
	// closeout stage settles a finished business day, so on TODAY it is normally 0 and the
	// difference between Captured and Selected is what is still waiting to be waived.
	AutoAccepted int
}

// ProgressPercent is how much of HER SHARE is done -- the number the maintainer named: at 40%
// sampling, reviewing all 40% reads 100%.
//
// A day with nothing drawn is COMPLETE (100), not zero: at 0% she owes nothing, and rendering 0%
// would read as a verifier who has fallen behind on work that does not exist.
func (s SamplingDayStats) ProgressPercent() int {
	if s.Selected <= 0 {
		return 100
	}
	if s.Reviewed >= s.Selected {
		return 100
	}
	return int(float64(s.Reviewed) / float64(s.Selected) * 100)
}

// SamplingOverview is the whole Randomization panel for one business date.
type SamplingOverview struct {
	BusinessDate string
	Categories   []SamplingCategory
}

// SetSamplingPolicy is the CEO's write: one category's percentage, effective from the business
// date the service resolves (today), leaving every earlier day at the percentage it ran at.
type SetSamplingPolicy struct {
	TenantID string
	Category string
	Percent  int
	ActorID  string
	// EffectiveBusinessDate is resolved by the service from the business clock, never by the
	// client: a caller that could name its own date could rewrite a day the verifier has already
	// worked, retroactively changing what she owed.
	EffectiveBusinessDate string
	IdempotencyKey        string
}
