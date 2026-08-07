// Package domain holds the feed-direction GENERATION model: the pure computation that turns
// projected shed counts plus the authored ration grid into per-session feed quantities.
//
// SCOPE BOUNDARY -- this package owns GENERATION, and nothing else.
//
//   - backend/internal/counts owns the projected head counts this reads (live herd + the
//     movements that are approved but not yet executed). It is the count INPUT.
//   - backend/internal/feedconfig owns the AUTHORED ration grid, the tag vocabulary, the
//     breed -> ration-group map, the session split, and the per-shed multipliers, and is the
//     only writer of those tables. It is the config INPUT.
//   - backend/internal/feed owns feed COMPLETIONS (what was actually delivered).
//
// This package writes nothing. It is a pure function of those two inputs plus a target date.
//
// # THE RESOLUTION RULE
//
//	ration_group = (feed_shed_tags.applies_to = 'kid') ? 'Kid' : feed_ration_groups[breed]
//	tag          = goats.management_stage
//	grams/head   = feed_ration_rates[park, ration_group, tag, feed_item]   (valid_to IS NULL)
//	shed total   = projected_head_count x grams_per_head x COALESCE(shed_factor, 1.0)
//	per session  = shed total x feed_session_templates.split_fraction
//
// THE KID/ADULT BRANCH COMES FROM feed_shed_tags.applies_to, NOT FROM goats.age_band. Migration
// 000003's header still describes the age_band form, which was the original design. It was
// superseded by a maintainer decision (2026-07-19) after validation against live data: the stored
// age_band CONTRADICTS the management-stage tag on 6 live animals (5 carrying the adult 'Mother'
// tag and 1 carrying the kid 'ICU-Kid' tag), and the tag vocabulary is the source of truth because
// it is what the ration grid is actually indexed by. Deriving the branch from the tag resolves all
// 1,682 live animals; deriving it from age_band strands those 6 and therefore blocks their sheds.
//
// # CONFIGURED ZERO IS NOT MISSING CONFIGURATION
//
// This package inherits migration 000003's safety-critical rule, and it is the single most
// important property of the types below:
//
//	rate = 0        -> feed nothing. Authored, correct, proceed.
//	rate not found  -> we do not know what to feed this group. BLOCK and surface the gap.
//
// A shed whose ration was never authored must never render as a clean 0 kg on a feed sheet: that
// is a shed that quietly goes unfed while the operator sees a complete-looking document. The
// distinction is enforced STRUCTURALLY here rather than by convention -- see ItemQuantity, whose
// QuantityKg is a POINTER that is nil for every blocked item. A blocked quantity is not
// representable as a number, so no downstream sum, render, or JSON encode can turn it into 0.
package domain

import "time"

// Workflow names the quantity strategy that produced a row. It is carried on every row so an
// operator can see WHY a shed's numbers look the way they do -- an experiment shed's quantities
// are hand-authored absolutes and are not derived from the ration grid at all.
const (
	// WorkflowNormal is the ration-grid path: head count x grams per head x shed factor.
	WorkflowNormal = "normal"
	// WorkflowExperiment is the hand-authored path: absolute kg per shed, head count informational.
	WorkflowExperiment = "experiment"
)

// Quantity resolution status. Two values, deliberately -- see the package comment.
const (
	// QuantityResolved means a quantity was DERIVED from authored configuration. It includes an
	// authored zero, which is a real feeding instruction ("K0/K1 kids are on milk").
	QuantityResolved = "resolved"
	// QuantityBlocked means the configuration needed to derive a quantity does not exist. It is not
	// a zero and must never be summed, defaulted, or rendered as one.
	QuantityBlocked = "blocked"
)

// Blocked reason codes. A code is machine-stable so a UI can group and count gaps; the Detail
// alongside it is the human sentence an operator acts on.
const (
	// BlockReasonNoRationRate is the common case: the (park, ration_group, shed_tag, feed_item)
	// cell has no currently-open row in feed_ration_rates.
	BlockReasonNoRationRate = "no_ration_rate"
	// BlockReasonUnknownShedTag means the animals' management_stage does not normalize onto any
	// authored feed_shed_tags row, so neither the kid/adult branch nor the grid key can be
	// resolved. Blocking is the only safe answer: guessing the course would feed a kid an adult
	// ration.
	BlockReasonUnknownShedTag = "unknown_shed_tag"
	// BlockReasonUnknownRationGroup means an ADULT animal's breed has no feed_ration_groups
	// mapping. Kids never reach this: they resolve to the fixed 'Kid' group without consulting the
	// breed map.
	BlockReasonUnknownRationGroup = "unknown_ration_group"
	// BlockReasonNoSessionTemplate means the park has no active feed_session_templates rows, so
	// there is no defensible way to divide a daily quantity into the sessions that are actually
	// packed and delivered.
	BlockReasonNoSessionTemplate = "no_session_template"
	// BlockReasonInvalidShedFactor means a feed_shed_factors row EXISTS for this (shed, item) but
	// its stored multiplier is not a parseable decimal. Distinct from a MISSING factor, which
	// safely defaults to 1.0 (declining to scale is safe); a present-but-corrupt factor must block
	// rather than silently apply an unauthored 1.0 multiplier (P2-FACTOR).
	BlockReasonInvalidShedFactor = "invalid_shed_factor"
)

// MultiValueSeparator joins the distinct values of a descriptive column when a shed genuinely holds
// more than one -- two breeds, or two management stages.
//
// LISTING BEATS COLLAPSING. The alternative, a bare "Mixed", tells an operator standing at the shed
// door strictly less than the raw data does: they cannot tell Beetal + Sojat from Beetal +
// Osmanabadi, and both exist in live data today (Yashoda 3 and Yashoda 4). Naming both values costs
// a few characters of column width and keeps the cell readable as what it is. The separator is
// surrounded by spaces so a value that itself contains a hyphen ('F2-Male') cannot be misread as a
// join.
const MultiValueSeparator = " + "

// KidRationGroupLabel is the fixed ration group every kid resolves to, of every breed. The source
// workbook literally stores the string 'Kid' in its breed column, which is why one group covers
// them all and why the breed map is bypassed entirely for the kid course.
const KidRationGroupLabel = "Kid"

// AppliesToKid / AppliesToAdult mirror the feed_shed_tags.applies_to CHECK vocabulary.
const (
	AppliesToKid   = "kid"
	AppliesToAdult = "adult"
)

// ---------------------------------------------------------------------------
// Query shapes
// ---------------------------------------------------------------------------

// PreviewQuery selects and pages one park's generated feed direction for one day.
//
// ParkID is REQUIRED, not optional. The ration grid is park-scoped (CBE and CPT genuinely differ
// on two group/tag rows), the session split is park-scoped, and the dispatch clock is park-scoped
// -- a tenant-wide preview would silently mix two parks' rations into one document.
type PreviewQuery struct {
	TenantID string
	ParkID   string
	// TargetDate is the feed day being planned, in the Goat OS business calendar. The time-of-day
	// component is discarded.
	TargetDate time.Time
	// ShedID optionally narrows to a single shed.
	ShedID string
	// SessionNo optionally narrows to a single feeding session. Zero means every session.
	SessionNo int32
	// Workflow optionally narrows the served issue to one dispatch workflow (normal | experiment).
	// Empty means both -- the read path unions the park-day's issues.
	Workflow string
	// Status optionally narrows to one verification-lifecycle bucket (SessionStatus* value). Empty
	// means every status. Applied over the WHOLE scope BEFORE the shed paging, so a filtered page and
	// its summary describe the same status set and pagination stays correct.
	Status string
	// Draft, when true, LIVE-COMPUTES a what-if sheet WITHOUT reading or writing any issue. It is the
	// only path that may live-compute, and the response is stamped Draft so it is never mistaken for
	// an issued sheet. It exists so someone tuning ration rates can preview the effect before issue.
	Draft bool
	// Limit and Offset page the SHED set, never the grain set -- see the paging note on
	// PreviewPage.
	Limit  int32
	Offset int32
}

// PackingQuery selects one park's packing worklist for one day.
type PackingQuery struct {
	TenantID   string
	ParkID     string
	TargetDate time.Time
	// SessionNo optionally narrows the worklist to a single feeding session. Zero means every
	// session, mirroring PreviewQuery.SessionNo.
	SessionNo int32
	// Workflow optionally narrows the served issue to one dispatch workflow. Empty means both.
	Workflow string
	// Status optionally narrows to one verification-lifecycle bucket (SessionStatus* value). Empty
	// means every status. Applied over the whole scope before paging, mirroring PreviewQuery.Status.
	Status string
	// Draft live-computes the worklist without touching any issue. See PreviewQuery.Draft.
	Draft  bool
	Limit  int32
	Offset int32
}

// ---------------------------------------------------------------------------
// Result shapes
// ---------------------------------------------------------------------------

// ProofRef is one OPTIONAL video-proof reference attached to a feed-direction completion.
//
// It mirrors the SOP/procurement ProofReference shape (a proof_id plus descriptive fields), but the
// feed module stores it OPAQUELY: the video bytes and the authoritative artifact live in the proof
// module (minted through /app/proofs/*), and this record only keeps the pointer. Video is optional,
// so a completion may carry zero refs.
type ProofRef struct {
	ProofID     string `json:"proof_id"`
	ProofType   string `json:"proof_type,omitempty"`
	SubjectType string `json:"subject_type,omitempty"`
	SubjectID   string `json:"subject_id,omitempty"`
	UploadState string `json:"upload_state,omitempty"`
}

// ItemQuantity is one feed item's quantity for one row.
//
// THE POINTER IS THE SAFETY CONTRACT. QuantityKg is nil if and only if Status is QuantityBlocked.
// An unauthored ration therefore cannot be read as a number by anything downstream -- not by a
// sum, not by a template, not by a JSON consumer, which receives `"quantity_kg": null` and a
// reason rather than a plausible-looking 0. An AUTHORED zero is the opposite state and is fully
// numeric: Status QuantityResolved with QuantityKg = "0.000".
type ItemQuantity struct {
	FeedItem string `json:"feed_item"`
	Status   string `json:"status"`
	// QuantityKg is the rounded, packable quantity in kg. Nil iff blocked.
	QuantityKg *string `json:"quantity_kg"`
	// GramsPerHead is the authored rate this quantity came from, echoed so an operator can see the
	// input without opening the config screen. Nil for blocked rows and for the experiment
	// workflow, which authors absolute kg and has no per-head rate at all.
	GramsPerHead *string `json:"grams_per_head,omitempty"`
	// ShedFactor is the multiplier applied. Nil for blocked and experiment rows.
	ShedFactor *string `json:"shed_factor,omitempty"`
	// BlockedReason is set iff blocked.
	BlockedReason *BlockedReason `json:"blocked_reason,omitempty"`
}

// BlockedReason carries a machine-stable code plus the sentence an operator reads.
type BlockedReason struct {
	Code string `json:"code"`
	// Detail names the exact missing coordinate, e.g. which park/group/tag/item cell had no
	// currently-open rate row. A gap the operator cannot locate is a gap they cannot close.
	Detail string `json:"detail"`
}

// DirectionRow is one generated instruction: what one ration grain in one shed gets in one
// session.
//
// The grain is (park, shed, shed_tag, ration_group) x session. SEX IS DELIBERATELY ABSENT: the
// projected counts arrive at (park, shed, stage, breed, sex) grain, but sex is NOT part of the
// ration lookup key, so the generator sums over it. Carrying sex into the output would split one
// feeding instruction into two half-sized rows that an operator would have to re-add by hand.
type DirectionRow struct {
	ParkID    string `json:"park_id"`
	ParkLabel string `json:"park_label"`
	ShedID    string `json:"shed_id"`
	ShedLabel string `json:"shed_label"`
	// PartitionLabel is the row's operational partition ("1", "Part 3"), empty for a shed with no
	// partitions. Clients render shed + partition per the operational-location rule ("Godel 2 -
	// Part 3"); they must never print the 'whole' matching token, which never reaches this field.
	PartitionLabel string `json:"partition_label,omitempty"`
	// ShedTag is the authored tag label the live management_stage normalized onto -- the canonical
	// spelling, not the raw source text.
	//
	// IT IS ALWAYS THE ANIMALS' TAG, on every workflow. An experiment row has no ration GRAIN, but
	// the shed still HOLDS animals and those animals still carry a management stage, so this column
	// reports it. It used to carry the experiment ARM instead, which made one column mean two
	// different things depending on the workflow -- an operator reading 'Sheep M NEW' here had no
	// way to know the shed's 63 animals are tagged 'F2-Male', and no way to trust the column on any
	// other row either. The arm now has its own field; see ExperimentArm.
	//
	// MULTI-VALUED SHEDS ARE REPRESENTED HONESTLY. A shed holding two stages reports both, joined
	// by MultiValueSeparator ("F2-Male + F2-Female"), never one of them silently chosen. Normal rows
	// are single-valued by construction (the grain IS the tag), so this only ever has one value
	// there.
	ShedTag string `json:"shed_tag"`
	// Breed is the raw live breed label; RationGroup is what it resolved to. Both are reported
	// because they differ in ways an operator needs to see: Beetal and Sirohi are two breeds that
	// share one 'Beetal/Sirohi' group, and every kid breed collapses to 'Kid'.
	//
	// Like ShedTag, this is populated on EVERY workflow from the live animals in the shed, and a
	// multi-breed shed (Yashoda 3 is Beetal + Sojat) reports both joined by MultiValueSeparator. A
	// blank breed on a row that has a head count told the operator nothing about what is standing in
	// the shed, which is the one thing they walk in holding.
	Breed string `json:"breed"`
	// RationGroup is EMPTY on an experiment row, correctly: an absolute hand-authored kg never
	// consults the breed -> ration-group map, so there is no group to report. That is a real state,
	// not missing data, and the renderer must show it as a deliberate blank rather than a hole.
	RationGroup string `json:"ration_group"`
	// ExperimentArm is the trial group a hand-authored experiment shed belongs to ('Sheep M NEW').
	// Empty on every normal row.
	//
	// It is a SEPARATE field from ShedTag, not a reuse of it, because the two are different facts:
	// a tag is the animals' management stage and drives the ration course, while an arm is which
	// trial the shed is enrolled in and drives nothing at all -- the quantity is hand-entered. It
	// mirrors the 'Experiment arm' column Feed Config already shows over the same authored value.
	ExperimentArm string `json:"experiment_arm"`
	SessionNo     int32  `json:"session_no"`
	SessionLabel  string `json:"session_label"`
	// HeadCount is the projected head count for this grain on the target date.
	HeadCount int64 `json:"head_count"`
	// HeadCountInformational is true when HeadCount did NOT drive the quantity. It is true for the
	// experiment workflow, whose absolute_kg is already a shed total -- multiplying it by head
	// count would be a straightforward overfeed, so the flag exists to stop a reader (or a future
	// rollup) from doing exactly that.
	HeadCountInformational bool           `json:"head_count_informational"`
	Workflow               string         `json:"workflow"`
	Items                  []ItemQuantity `json:"items"`
	// SessionTotalKg sums the RESOLVED items only. Blocked items contribute nothing because they
	// have no number to contribute; Blocked below reports that the total is therefore partial.
	SessionTotalKg string `json:"session_total_kg"`
	// Blocked is true when at least one item on this row could not be resolved. It is the flag that
	// stops SessionTotalKg from being mistaken for a complete figure.
	Blocked bool `json:"blocked"`
	// OverduePending mirrors the counts projection: a movement this row's head count already
	// assumes came due days ago and still has not been executed.
	OverduePending bool `json:"overdue_pending"`
	// Completed is true when this shed-session has a recorded feed.direction.completed. It is the
	// operator-action state the generator overlays onto the derived sheet: the ration numbers are
	// still generated the same way, but the row also reports that the feeding was carried out. A
	// whole shed-session is completed at once (that is the completion grain), so every ration grain
	// of the same (shed, session) reports Completed together.
	Completed bool `json:"completed"`
	// LifecycleStatus is the verification-lifecycle bucket of this shed-session's feed-distribution
	// completion, one of the three SessionStatus* values. It is the finer state Completed collapses:
	// SessionStatusCompleted iff Completed is true. Set by the serve path from
	// feed_distribution_completions; every ration grain of the same (shed, session) reports the same
	// value. See SessionStatus* / NormalizeSessionStatus for the raw->bucket mapping.
	LifecycleStatus string `json:"lifecycle_status"`
}

// Session lifecycle status buckets (maintainer decision 2026-07-26). A shed-session's
// feed-distribution / feed-packing completion moves through these client-visible buckets. The raw
// table has a fourth state, 'rework' (verifier rejected), which is INTENTIONALLY merged into
// SessionStatusPending here: from the operator's list it is "needs my action again", the same bucket
// as never-submitted. See NormalizeSessionStatus.
const (
	// SessionStatusPending: the operator has not submitted proof yet (no completion row), OR the
	// verifier bounced it back for rework. Either way the next action is the operator's.
	SessionStatusPending = "pending"
	// SessionStatusAwaitingVerification: the operator submitted proof and a verifier has not acted
	// (raw 'pending_verification'). The next action is the verifier's.
	SessionStatusAwaitingVerification = "pending_verification"
	// SessionStatusCompleted: a verifier approved the proof (raw 'completed'). Terminal.
	SessionStatusCompleted = "completed"
)

// NormalizeSessionStatus maps a raw completion-row status onto the three client-visible buckets. An
// empty/unknown raw status (including the absence of a completion row) and raw 'rework' both map to
// SessionStatusPending; 'pending_verification' and 'completed' pass through. This is the single
// producer of the bucket vocabulary, so a status filter and the row chip can never diverge.
func NormalizeSessionStatus(raw string) string {
	switch raw {
	case SessionStatusCompleted:
		return SessionStatusCompleted
	case SessionStatusAwaitingVerification:
		return SessionStatusAwaitingVerification
	default:
		// "", "rework", and any unexpected value are all "operator must (re)act" == pending.
		return SessionStatusPending
	}
}

// IsValidSessionStatusFilter reports whether raw is a status the read APIs accept as a filter. The
// empty string ("all statuses") is valid; every other accepted value is one of the three buckets.
func IsValidSessionStatusFilter(raw string) bool {
	switch raw {
	case "", SessionStatusPending, SessionStatusAwaitingVerification, SessionStatusCompleted:
		return true
	default:
		return false
	}
}

// PreviewPage is one page of generated rows plus the WHOLE-SCOPE summary.
//
// Items is the page; Summary covers every row matching the filters. The two are deliberately
// different scopes and both say so in their own docs -- Summary.RowCount is the honest way to see
// how much of the result the page in Items represents.
//
// PAGING UNIT IS THE SHED, and that is load-bearing rather than a style choice. A shed's rows must
// never straddle a page boundary: half a shed's grains on page 1 and half on page 2 would present
// two partial session totals, each of which looks like a complete instruction for that shed. So
// the reader pages sheds first and then fetches every grain for exactly those sheds in one batched
// read.
type PreviewPage struct {
	Items   []DirectionRow `json:"items"`
	Summary PreviewSummary `json:"summary"`
	// Lifecycle reports whether the served sheet was issued/amended/locked, or is pending/not_issued
	// (nothing frozen yet), or draft (live what-if). It is what tells a client this is a FROZEN
	// artifact rather than a live computation. Always present.
	Lifecycle Lifecycle `json:"lifecycle"`
	// Draft is true only for the deliberate live-compute escape hatch (draft=true). An issued,
	// amended, locked, pending or not_issued response is never draft.
	Draft bool `json:"draft"`
	// Filters is the backend-owned park/shed filter vocabulary plus the served park id. Present on
	// every response so a client can render the farm/shed pickers without holding its own location
	// list and can show which park it is currently viewing.
	Filters    FeedFilterOptions `json:"filters"`
	TargetDate string            `json:"target_date"`
	Limit      int32             `json:"limit"`
	Offset     int32             `json:"offset"`
	HasMore    bool              `json:"has_more"`
}

// SummaryScopeFiltered is the only scope this module reports.
//
// It means: every row matching the request's tenant, park, target date, shed and session filters --
// NOT the visible page. It is stamped on every summary so a client can assert the guarantee rather
// than infer it, and so a future page-scoped block (if one is ever added) must introduce its own
// clearly-named field instead of overloading this one.
const SummaryScopeFiltered = "filtered"

// PreviewSummary rolls up the WHOLE FILTERED RESULT SET, and is invariant to limit and offset.
//
// # WHY THIS IS NOT PAGE-SCOPED, AND WHY IT IS NOT SQL
//
// It used to roll up the page, which made every figure here shrink with page size: the same park
// and day reported 129.800 kg of Concentrate over 4 sheds at limit=5 and 313.200 kg over 38 sheds
// unpaged. An operator reads the first number as the park total and packs a fraction of what the
// sheds need. That is the capped read-time rollup AGENTS.md bans outright.
//
// The obvious fix -- aggregate in SQL with a window function, the way counts' breakdown does -- is
// NOT available here, and the reason is worth stating because it looks like an oversight:
//
//   - counts aggregates count(*) over goats. The aggregated value IS a SQL expression.
//   - a feed quantity is not. It is produced by this package's pure pipeline: normalize live
//     free-text stage/breed onto authored tags, branch kid/adult off feed_shed_tags.applies_to,
//     resolve the ration group, look up a rate (whose ABSENCE means BLOCKED, not zero), multiply by
//     head count and shed factor, apply the session split, and finally round UP to the item's step
//     (see RoundSessionGramsToKg).
//   - that last step is NON-LINEAR: sum(ceil(x_i)) != ceil(sum(x_i)). The park total must equal the
//     sum of the cells actually PRINTED, because that is what the packer physically packs. A SQL
//     aggregate over raw grams would round once at the end and silently under-report.
//   - reproducing resolution, blocked-vs-zero, the split and the rounding policy in SQL would fork
//     the definition of a feeding quantity in two, including RoundingPolicy.PerItemStepGrams, whose
//     baking-soda entry is an open maintainer decision (see rounding.go). Two definitions that
//     drift silently is a worse defect than the one being fixed.
//
// So the rollup stays in Go, over the whole filtered scope, through the SAME GenerateDirection call
// that produces the visible rows -- the total is the sum of those rows by construction, not a
// second computation that could disagree with them.
//
// THE SCOPE IS BOUNDED BY PHYSICAL INFRASTRUCTURE, NOT BY HERD SIZE: sheds x sessions x feed slots.
// For the largest live park-day that is 78 sheds x 2 sessions x 5 slots, ~780 cells -- smaller than
// the 721 authored ration-rate rows LoadConfigSnapshot already reads unconditionally on every
// single request, paged or not. It does not grow when the farm buys animals. Both reads that feed
// it fail closed rather than truncate (ports.ErrScopeTooLarge), because a total covering an
// arbitrary prefix of the park is exactly the defect this type was rewritten to remove.
type PreviewSummary struct {
	// Scope is always SummaryScopeFiltered. It is an explicit field rather than documentation so a
	// client can assert the coverage guarantee instead of trusting a comment.
	Scope string `json:"scope"`
	// ShedCount is the number of distinct sheds in the whole filtered scope.
	ShedCount int32 `json:"shed_count"`
	// RowCount is the number of generated rows in the whole filtered scope -- NOT len(items), which
	// is the page. The two differ whenever the result is paged, and that is the point.
	RowCount int32 `json:"row_count"`
	// TotalKgByFeedItem sums the RESOLVED quantities across the whole filtered scope per feed item.
	// A blocked cell is absent from the sum and counted in BlockedCount instead -- never added as
	// zero.
	TotalKgByFeedItem []FeedItemTotal `json:"total_kg_by_feed_item"`
	// BlockedCount is the number of blocked ITEM cells in the whole filtered scope (not rows),
	// because that is the number of authored gaps an operator has to close.
	BlockedCount int32 `json:"blocked_count"`
	// BlockedShedCount is the number of distinct sheds carrying at least one blocked cell -- the
	// number of sheds at risk of going unfed.
	BlockedShedCount int32 `json:"blocked_shed_count"`
}

// PackingSummary rolls up the whole filtered worklist, on exactly the same terms as PreviewSummary.
//
// It is a distinct type because its RowCount counts PACKING LINES (shed x session), not ration
// grains -- a packer's unit of work is the bag, and reporting the preview's grain count here would
// overstate the job. Every other field carries the same meaning and the same whole-scope guarantee.
type PackingSummary struct {
	// Scope is always SummaryScopeFiltered.
	Scope string `json:"scope"`
	// ShedCount is the number of distinct sheds in the whole filtered scope.
	ShedCount int32 `json:"shed_count"`
	// LineCount is the number of shed x session packing lines in the whole filtered scope.
	LineCount int32 `json:"line_count"`
	// TotalKgByFeedItem sums the RESOLVED per-shed quantities across the whole filtered scope.
	TotalKgByFeedItem []FeedItemTotal `json:"total_kg_by_feed_item"`
	// BlockedCount is the number of blocked item cells across the whole filtered scope.
	BlockedCount int32 `json:"blocked_count"`
	// BlockedShedCount is the number of distinct sheds carrying at least one blocked cell.
	BlockedShedCount int32 `json:"blocked_shed_count"`
	// BlockedLineCount is the number of packing lines that cannot be packed as printed.
	BlockedLineCount int32 `json:"blocked_line_count"`
}

// FeedFilterPark is one park a caller may generate a sheet for. The park is REQUIRED on every feed
// read (one park per sheet, never mixed), so a client needs this vocabulary to offer a farm filter
// and to know which park it is currently looking at.
type FeedFilterPark struct {
	ParkID string `json:"park_id"`
	Label  string `json:"label"`
}

// FeedFilterShed is one shed within the served park. Carries ParkID so a client that caches several
// parks' sheds can still narrow the shed picker to the selected park.
type FeedFilterShed struct {
	ShedID string `json:"shed_id"`
	Label  string `json:"label"`
	ParkID string `json:"park_id"`
}

// FeedFilterOptions is the backend-owned filter vocabulary for the feed screens, so the client
// holds no park/shed list of its own (the golden frontend rule). It is bounded by physical
// infrastructure — parks and one park's shed catalog — never by herd size.
//
// ServedParkID is the park this response was actually generated for. It equals the requested
// park_id, or — when the request omitted park_id — the default park the server selected, so the
// client can show the right park as active without guessing.
type FeedFilterOptions struct {
	ServedParkID string              `json:"served_park_id"`
	Parks        []FeedFilterPark    `json:"parks"`
	Sheds        []FeedFilterShed    `json:"sheds"`
	Sessions     []FeedFilterSession `json:"sessions"`
}

// FeedFilterSession is one feeding session in the served park's active split (session 1, session 2,
// ...), the backend-owned vocabulary a client renders its session picker from. The label is the
// authored session_label; the number is what the client sends back as the `session` filter param.
// Ordered by display order then session number, matching the generator's own iteration order.
type FeedFilterSession struct {
	SessionNo int32  `json:"session_no"`
	Label     string `json:"label"`
}

// FeedItemTotal is one feed item's page total. A slice of pairs rather than a map so the order is
// the authored catalog display order and a client renders columns consistently across pages.
type FeedItemTotal struct {
	FeedItem   string `json:"feed_item"`
	QuantityKg string `json:"quantity_kg"`
	// BlockedCells counts the cells for this item that could not be resolved, so a column total is
	// never read as complete when part of it is missing.
	BlockedCells int32 `json:"blocked_cells"`
}

// PackingRow is one shed/session line of the packing worklist.
type PackingRow struct {
	ParkID    string `json:"park_id"`
	ParkLabel string `json:"park_label"`
	ShedID    string `json:"shed_id"`
	ShedLabel string `json:"shed_label"`
	// PartitionLabel is the operational partition this bag is for ("1", "Part 3"), empty for a shed
	// with no partitions. A packing line is grouped at the same OPERATIONAL LOCATION grain as the
	// direction row it is built from, so Castro 1 and Castro 2 are two separate bags. Without it a
	// packer sees two identical "Castro" lines and cannot tell which pen either bag belongs to --
	// and one shed's partitions can carry very different quantities when some are on an authored
	// experiment and the rest on the per-head grid.
	PartitionLabel string `json:"partition_label,omitempty"`
	SessionNo      int32  `json:"session_no"`
	SessionLabel   string `json:"session_label"`
	Workflow       string `json:"workflow"`
	// ExperimentArm is the trial group of a hand-authored experiment shed, empty on normal lines.
	// Carried here as well as on DirectionRow so the packer knows which trial a bag belongs to
	// without cross-referencing the direction sheet -- the same authored value, never a shed tag.
	ExperimentArm string `json:"experiment_arm"`
	// HeadCount is the shed's projected head count, summed across its ration grains.
	HeadCount int64 `json:"head_count"`
	// Items is the SHED-level expected quantity per feed item -- the grains are already summed,
	// because a packer fills one bag per item per shed, not one per ration grain.
	Items []ItemQuantity `json:"items"`
	// TotalKg sums the resolved items.
	TotalKg string `json:"total_kg"`
	// Status is the packing state (ready | blocked | empty), derived from the generation result.
	Status string `json:"status"`
	// Completed is true when this shed-session has a recorded feed.direction.completed. It is
	// orthogonal to Status: a completed line was still ready/blocked/empty underneath, and reporting
	// both lets the client show a "completed" badge without losing the packing state. A shed-session
	// is completed as a whole, so this packing line (which IS one shed-session) maps 1:1 to it.
	Completed bool `json:"completed"`
	// LifecycleStatus is the verification-lifecycle bucket of this shed-session's feed-PACKING
	// completion (one of the SessionStatus* values), the finer state Completed collapses. Orthogonal
	// to Status (the ready/blocked/empty ration state): a line can be blocked underneath and still be
	// pending_verification. Set by the serve path from feed_packing_completions.
	LifecycleStatus string `json:"lifecycle_status"`
	// BlockedReasons lists the distinct gaps behind a blocked status.
	BlockedReasons []BlockedReason `json:"blocked_reasons,omitempty"`
}

// Packing statuses. Derived, never stored -- see PackingRow.Status.
const (
	// PackingStatusReady means every item on the line resolved and the line can be packed.
	PackingStatusReady = "ready"
	// PackingStatusBlocked means at least one item has no authored ration. The line must NOT be
	// packed from the resolved remainder as though it were complete.
	PackingStatusBlocked = "blocked"
	// PackingStatusEmpty means the shed holds no projected animals on the target date. Distinct
	// from blocked: there is nothing to feed, which is not a configuration gap.
	PackingStatusEmpty = "empty"
)

// PackingPage is one page of the worklist. Paged by shed for the same reason as PreviewPage.
//
// Summary covers the WHOLE filtered worklist, not this page -- see PackingSummary. It is what an
// operator uses to know how much of each feed to draw from the store before walking the park, so a
// page-scoped figure here would send them out with a fraction of the load.
type PackingPage struct {
	Items   []PackingRow   `json:"items"`
	Summary PackingSummary `json:"summary"`
	// Lifecycle and Draft carry the same issue-state metadata as PreviewPage.
	Lifecycle Lifecycle `json:"lifecycle"`
	Draft     bool      `json:"draft"`
	// Filters is the backend-owned park/shed filter vocabulary plus the served park id, on the same
	// terms as PreviewPage.Filters.
	Filters    FeedFilterOptions `json:"filters"`
	TargetDate string            `json:"target_date"`
	Limit      int32             `json:"limit"`
	Offset     int32             `json:"offset"`
	HasMore    bool              `json:"has_more"`
}
