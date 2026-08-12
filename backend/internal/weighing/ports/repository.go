package ports

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

var (
	ErrForbidden           = errors.New("weighing: forbidden")
	ErrInvalidArgument     = errors.New("weighing: invalid argument")
	ErrNotFound            = errors.New("weighing: not found")
	ErrIdempotencyConflict = errors.New("weighing: idempotency conflict")
	ErrImmutable           = errors.New("weighing: immutable")
	ErrScopeIncomplete     = errors.New("weighing: scope incomplete")

	// ErrParkSelectionRequired is returned when the actor legitimately covers SEVERAL parks
	// without a tenant grant and the surface can only answer for one. It is deliberately its
	// own class rather than ErrInvalidArgument: the request was not malformed, the client
	// simply has to name a park, and it needs a code it can act on. The vaccination side
	// already answers this exact situation with park_selection_required plus the park list;
	// weighing returning a bare "request is invalid" left the Android task list permanently
	// blank with no way to discover the remedy.
	ErrParkSelectionRequired = errors.New("weighing: park selection required")

	// ErrCaptureIncomplete is the SERVER-side pair rule: an animal in an
	// individual bucket is only submittable once BOTH its weight and its video
	// exist. It is deliberately distinct from ErrScopeIncomplete (which means the
	// submitted list OMITS a complete capture) and from ErrNotFound (which used
	// to swallow this case and told the operator a shed they are standing in does
	// not exist). Carried to the client as 409 weighing_capture_incomplete with
	// one field error per animal, so the app can name the row to go back and fix.
	ErrCaptureIncomplete = errors.New("weighing: capture incomplete")

	// ErrProofNotReady is the lump-sum equivalent: the shed total was sent with a
	// video that is not a usable, finished upload for this shed. Previously
	// collapsed into ErrInvalidArgument, which renders as "request is invalid" --
	// true of a malformed request, useless to an operator whose video is simply
	// still uploading.
	ErrProofNotReady = errors.New("weighing: proof not ready")

	// ErrReworkNotRecaptured is submit refusing a bucket that still holds an animal a
	// verifier SENT BACK. Re-capturing (a new weight or a new video) is what returns the
	// row to 'pending' and clears submitted_at; until then a re-submit of the unchanged
	// capture must not complete the bucket. It used to: the bucket went 'completed' while
	// the rejected animal stayed in 'rework' with no new verification item, so the
	// verifier's rejection was silently dropped and the work read as done.
	ErrReworkNotRecaptured = errors.New("weighing: rejected animal must be re-recorded before submit")
)

// ReworkNotRecapturedTags is ErrReworkNotRecaptured carrying the scanned identifiers that are
// actually awaiting re-record, so the operator is told WHICH animals to redo instead of "that
// animal". It stays errors.Is-comparable to ErrReworkNotRecaptured, so every existing handler
// branch keeps matching.
type ReworkNotRecapturedTags struct {
	Tags []string
}

func (e *ReworkNotRecapturedTags) Error() string {
	return ErrReworkNotRecaptured.Error() + ": " + strings.Join(e.Tags, ", ")
}

func (e *ReworkNotRecapturedTags) Is(target error) bool { return target == ErrReworkNotRecaptured }

// ReworkNotRecapturedFor wraps tags, falling back to the bare sentinel when none were resolved.
func ReworkNotRecapturedFor(tags []string) error {
	if len(tags) == 0 {
		return ErrReworkNotRecaptured
	}
	return &ReworkNotRecapturedTags{Tags: tags}
}

var (

	// ErrRejectedProofReuse is returned when an operator attempts to re-submit a shed
	// observation using a proof that was already attached to a withdrawn or rework
	// (rejection) observation for the same shed. The operator must record a new video.
	// It is distinct from ErrProofNotReady (video still uploading) and ErrInvalidArgument
	// (wrong type/shed/count) because this is not a problem the operator can wait out
	// or fix by changing the request -- a new video is required.
	ErrRejectedProofReuse = errors.New("weighing: rejected video cannot be re-used; record a new video")

	// ErrDuplicateScan is returned when a scanned_identifier was already
	// captured AND SUBMITTED in an earlier round for the same campaign_shed_id
	// and business day. It is deliberately distinct from ErrIdempotencyConflict:
	// an idempotency conflict means the SAME idempotency key was reused with a
	// DIFFERENT payload (a client replay bug), whereas a duplicate scan is a
	// brand-new request (new idempotency key, no fingerprint to compare) that
	// collides with a different row already committed as submitted. Mapped to
	// 409 Conflict — the resource (this tag, in this bucket, today) already
	// exists as a submitted capture, so the request cannot proceed as issued.
	ErrDuplicateScan = errors.New("weighing: duplicate scan")

	// ErrVerificationPending is the leadership close gate: a bucket may only be
	// closed on the NORMAL path once every submitted video has a verdict. It is
	// deliberately distinct from ErrImmutable ("alreadyterminal") because the
	// bucket is perfectly writable — it is the CLOSER who is early, and the
	// operator and verifier are both still free to work.
	ErrVerificationPending = errors.New("weighing: verification pending")

	// ErrShedAlreadyScheduled is the duplicate-work block: one open weighing row
	// per (park, weigh date, shed). It is distinct from ErrImmutable (the target
	// is in a state that refuses the write) because nothing here is in a wrong
	// state — the requested buckets are simply already somebody's work on that
	// date, and the planner must be told WHICH ones so it can render the reason.
	// Carried to the client as 409 weighing_shed_already_scheduled.
	ErrShedAlreadyScheduled = errors.New("weighing: shed already scheduled")

	// ErrOperatorOutsidePark blocks assigning a weighing bucket to someone whose scope does not
	// reach that park.
	//
	// Operators are park-scoped by invariant: an operator belongs to exactly ONE park. The planner
	// happily accepted a CPT-scoped operator on a CBE shed, and nothing downstream re-checked it --
	// the work list filters on operator_user_id alone and the write only requires the caller to be
	// the assignee -- so that person would have seen and been able to weigh another park's shed.
	// Leadership scoped to the whole tenant (the growth director) legitimately spans parks and is
	// unaffected.
	ErrOperatorOutsidePark = errors.New("weighing: operator is not scoped to this park")

	// ErrStaleEvidence is returned when a verdict names a proof that is no longer
	// the proof attached to the observation -- the reviewer decided on evidence that
	// has since been superseded (a rework re-shoot swapped the video out from under
	// an in-flight review). It lives HERE, not in the postgres adapter that raises
	// it, because the event consumer in weighing/app has to recognise it: a verdict
	// against superseded evidence can never become applicable no matter how many
	// times the bus redelivers it, so the consumer must fail it permanently rather
	// than retry it forever as an unclassified store error. Distinct from
	// ErrIdempotencyConflict (same key, different request) and ErrImmutable (target
	// already terminal): the verdict is well-formed and the target is writable --
	// it is the EVIDENCE that moved on.
	ErrStaleEvidence = errors.New("weighing: stale verification evidence")

	// ErrWriteConflict is a Postgres SERIALIZABLE (SSI) conflict, SQLSTATE
	// 40001, on a write that touches no duplicate at all -- it means "this
	// transaction lost a race against another that overlapped it in time",
	// nothing more. It is deliberately NOT ErrDuplicateScan: RecordAnimalObservation
	// runs at SERIALIZABLE because concurrent captures of the SAME bucket take a
	// shared FOR NO KEY UPDATE row lock (weighing_campaign_sheds), so TWO
	// DIFFERENT animals captured at overlapping instants in the SAME bucket are
	// ordinary SSI-conflict candidates even though neither is a duplicate of
	// anything. Collapsing 40001 into ErrDuplicateScan would tell an operator
	// they double-scanned an animal they scanned exactly once, and DISCARD a
	// real capture with a real video instead of retrying it -- worse than the
	// race the SERIALIZABLE fix exists to close. This error is retried a bounded
	// number of times INSIDE the repository (see RecordAnimalObservation); a
	// caller should only ever see it if every retry also lost, which packages as
	// a transient 409/503-class failure the client is expected to resubmit, not
	// as "you already did this."
	ErrWriteConflict = errors.New("weighing: write conflict, retry")

	// ErrFinishedShedBlocksReschedule refuses to MOVE a task that already holds
	// finished work. A weighed bucket records the day it was actually weighed, and
	// its proof video hangs off that day, so moving the task cannot drag it along
	// without falsifying when the work happened -- and leaving it behind silently
	// is worse: the task reads "Tuesday" while the finished shed still belongs to
	// Monday, on neither day's list. The planner is told instead, and chooses:
	// drop the finished shed from this task, or leave the task where it is and
	// plan the new date as its own task.
	ErrFinishedShedBlocksReschedule = errors.New("weighing: task has finished sheds and cannot be moved")
)

// ShedScheduleConflict names the buckets that blocked a create/update/publish so
// the client can say "Cannot publish - Shed 4, Shed 7 already scheduled" without
// a second round trip. It unwraps to ErrShedAlreadyScheduled, so callers that
// only care about the class keep using errors.Is.
type ShedScheduleConflict struct {
	// WeighDate is the Asia/Kolkata business DATE the conflict is on.
	WeighDate string `json:"weigh_date"`
	// Sheds are the human-readable bucket names, ordered, deduplicated.
	Sheds []string `json:"sheds"`
}

func (c *ShedScheduleConflict) Error() string {
	return "weighing: sheds already scheduled on " + c.WeighDate + ": " + strings.Join(c.Sheds, ", ")
}

func (c *ShedScheduleConflict) Unwrap() error { return ErrShedAlreadyScheduled }

// FinishedShedConflict names the already-weighed buckets that blocked a task move,
// in the same shape as ShedScheduleConflict so the client renders it through the
// path it already has for "these sheds are the problem".
type FinishedShedConflict struct {
	// WeighDate is the Asia/Kolkata business DATE those buckets were weighed on --
	// the date they keep, and the reason the task cannot leave it behind quietly.
	WeighDate string `json:"weigh_date"`
	// Sheds are the human-readable bucket names, ordered, deduplicated.
	Sheds []string `json:"sheds"`
}

func (c *FinishedShedConflict) Error() string {
	return "weighing: sheds already weighed on " + c.WeighDate + ": " + strings.Join(c.Sheds, ", ")
}

func (c *FinishedShedConflict) Unwrap() error { return ErrFinishedShedBlocksReschedule }

// CampaignAccess is the caller's authority over ONE task, expressed as data the single-task
// query can evaluate against the row it is about to return. The arms are alternatives, in the
// same either/or shape the service's role gate already has:
//
//	Unrestricted        -- tenant-wide plan-or-monitor authority, or an internal caller with no
//	                       grants at all (CLI, seeder, integration test). Admits any park.
//	AuthorizedParkIDs   -- the parks the actor holds plan-or-monitor in. Admits a task in one of
//	                       them, unfiltered.
//	AssigneeUserID      -- the actor's own user id, set when they hold weighing.execute. Admits a
//	                       task they hold a live bucket on, narrowed to that bucket.
//
// The assignee arm is an OR and not an AND deliberately. A Growth Director holds
// WeighingMonitor AND WeighingExecute at once; one who monitors park A while being ASSIGNED
// work in park B is authorized for B through the assignment alone, and requiring both arms
// would 404 them on their own task (the defect fixed in 2c78f87f1).
//
// A zero value admits nothing, which is the correct answer for an actor who passed a park-blind
// role gate but holds no park here and is assigned nothing.
type CampaignAccess struct {
	Unrestricted      bool
	AuthorizedParkIDs []string
	AssigneeUserID    string
}

// AdmitsPark reports whether the actor's PARK authority -- as opposed to their assignment --
// covers parkID.
//
// Callers evaluate it against the park_id the single-task query ALREADY RETURNED, never against
// a separately fetched one, so it cannot disagree with the row it describes. It decides bucket
// VISIBILITY only: a caller admitted by park authority reads the task's buckets unfiltered,
// while one admitted solely because they are assigned on it reads their own bucket, which is
// the same split the task list applies.
func (a CampaignAccess) AdmitsPark(parkID string) bool {
	if a.Unrestricted {
		return true
	}
	for _, id := range a.AuthorizedParkIDs {
		if id == parkID {
			return true
		}
	}
	return false
}

// CaptureIncomplete names the animals whose (weight, video) PAIR is not
// complete in the bucket the operator just tried to submit.
//
// One animal = one pair. The submit transaction already refuses to complete a
// bucket where any scanned animal lacks a weight or a completed video (see
// SubmitIndividualScope's NOT EXISTS gates), but that refusal used to fall
// through the classifier as a bare ErrNotFound -- the operator was told
// "weighing resource was not found" about a shed that plainly exists, which
// names neither the problem nor the animal. The client gate is not enforcement
// (a stale build, a replayed request, or a modified client all bypass it), so
// the SERVER's rejection is the one that has to be readable.
//
// The identifiers here are exactly the ones the client submitted -- nothing is
// derived from a roster, the herd register, or an expected count. Weighing has
// no denominator.
type CaptureIncomplete struct {
	// MissingWeight are scanned identifiers with no recorded weight in this bucket.
	MissingWeight []string `json:"missing_weight"`
	// MissingVideo are scanned identifiers with a weight but no completed video.
	MissingVideo []string `json:"missing_video"`
}

func (c *CaptureIncomplete) Error() string {
	parts := make([]string, 0, 2)
	if len(c.MissingWeight) > 0 {
		parts = append(parts, "missing weight: "+strings.Join(c.MissingWeight, ", "))
	}
	if len(c.MissingVideo) > 0 {
		parts = append(parts, "missing video: "+strings.Join(c.MissingVideo, ", "))
	}
	return "weighing: capture incomplete (" + strings.Join(parts, "; ") + ")"
}

func (c *CaptureIncomplete) Unwrap() error { return ErrCaptureIncomplete }

type Repository interface {
	CreateCampaign(ctx context.Context, cmd domain.CreateCampaign) (domain.Campaign, error)
	UpdateCampaign(ctx context.Context, campaignID string, cmd domain.UpdateCampaign) (domain.Campaign, error)
	PublishCampaign(ctx context.Context, tenantID, campaignID, actorID, idempotencyKey string) (domain.Campaign, error)
	// ListCampaigns / ListCampaignsForOperator page the task list. parkID is an
	// optional row filter; the page's whole-filter Counts are computed over the
	// scope and are deliberately NOT narrowed by it (see domain.CampaignCounts).
	ListCampaigns(ctx context.Context, tenantID, parkID string, cursor string, limit int) (domain.CampaignPage, error)
	ListCampaignsForOperator(ctx context.Context, tenantID, operatorUserID, parkID string, cursor string, limit int) (domain.CampaignPage, error)
	// CampaignByID is the SINGLE-task read behind a notification deep link. The task
	// list is keyset-paged with no id filter, so a cold tap on a task that is not on
	// the first page or two could not be resolved at all: the client walked a few
	// pages and then reported "not found" for work that exists.
	//
	// access carries the caller's authority INTO the query instead of being checked
	// around it. The previous shape resolved the campaign's park with a separate
	// CampaignParkID call, authorized that park, and then read the campaign in a second
	// statement -- two reads of a mutable column with no transaction between them, so a
	// task that moved park in the gap was authorized as park A and returned as park B.
	// Authorization and retrieval are now one statement over one snapshot of the row,
	// which is the only shape in which the park that was checked and the park that was
	// returned cannot differ.
	//
	// ErrNotFound when the task does not exist OR no arm of access admits it, so the two
	// are indistinguishable to a caller probing ids.
	CampaignByID(ctx context.Context, tenantID, campaignID string, access CampaignAccess) (domain.Campaign, error)
	// WeighingParks is the park VOCABULARY behind the oversight surfaces' park chips:
	// identity only, no date scope and no counts (that is PlannerCatalog, which is
	// gated on the CEO-only WeighingPlan).
	//
	// parkIDs is the caller's capability-scoped park set and is part of the QUERY, the
	// same contract ListLeadershipSheds declares: a nil/empty slice means unrestricted
	// (tenant-wide authority or an internal caller), NOT "authorized for nothing" --
	// this port cannot tell those apart and the service is the layer that knows.
	WeighingParks(ctx context.Context, tenantID string, parkIDs []string) ([]domain.WeighingPark, error)
	// ListParks returns all active parks for a tenant, unrestricted by authorization scope.
	ListParks(ctx context.Context, tenantID string) ([]domain.WeighingPark, error)
	// PlannerCatalog is the PARK-grain planner read for ONE weigh date: EVERY park
	// the planner may use, each with a park-grain shed COUNT (not shed rows), plus
	// the operator picker. Bounded by domain.MaxPlannerParks; there is no park
	// cursor, because a park step that pages cannot offer the parks it has not
	// reached yet.
	PlannerCatalog(ctx context.Context, tenantID string, periodStartDate string) (domain.PlannerCatalog, error)
	// PlannerParkBuckets is ONE keyset page of the sheds of ONE park on ONE weigh
	// date, carrying that date's availability. excludeCampaignID is the task being
	// edited, whose own buckets must not read back as "taken".
	PlannerParkBuckets(ctx context.Context, tenantID, parkID, periodStartDate, excludeCampaignID, search, cursor string, limit int) (domain.PlannerParkBuckets, error)
	// ListCampaignSheds is the task-DETAIL bucket page. The task list embeds a
	// campaign's whole bucket set; the detail screen reads ~20 at a time instead.
	//
	// access carries the caller's authority INTO the page instead of being checked
	// around it, for the same reason CampaignByID does. The previous shape resolved the
	// campaign's park with a separate CampaignParkID call, authorized it, and then read
	// the buckets in a second statement -- two reads of a MUTABLE column (UpdateCampaign
	// moves a task between parks) with nothing spanning them, so a task that moved in the
	// gap was authorized as its old park and paged as its new one, operator display names
	// included. The access arms are now evaluated per returned row, in the same statement
	// that returns it.
	//
	// It also replaces the old operatorUserID parameter: the assignee arm both ADMITS a
	// bucket and NARROWS the page to the caller's own buckets, which is exactly what that
	// parameter did, so keeping both would be two spellings of one rule.
	//
	// ErrNotFound when no arm of access can admit the campaign at all, matching what the
	// preceding park check used to answer, so existence is still not leaked.
	ListCampaignSheds(ctx context.Context, tenantID, campaignID, cursor string, limit int, access CampaignAccess) (domain.CampaignShedPage, error)
	ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, observationsCursor string, limit int) (domain.RosterPage, error)
	ListScopeRosterForOperator(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, observationsCursor string, limit int) (domain.RosterPage, error)
	// cursor/limit page the shed's INDIVIDUAL observations on (accepted_at,
	// observation_id). The lump-sum row is a single latest read and is not paged.
	//
	// access is the caller's PARK authority, evaluated against the campaign row the head
	// query already joins rather than against a park fetched by a preceding statement.
	// The old shape read park_id via CampaignParkID, authorized it, and then read the
	// evidence: a task moved between parks in that gap was authorized as its old park and
	// its proof footage served from its new one. Only the park arms are ever set here --
	// this surface's role gate is WeighingMonitor alone, so there is no assignee arm to
	// admit; an assignee reads their own evidence through the roster, not through
	// leadership review.
	//
	// ErrNotFound when the bucket does not exist OR the access arms do not admit its
	// campaign's park, so the two stay indistinguishable to a caller probing ids.
	GetLeadershipShedVideos(ctx context.Context, tenantID, campaignID, campaignShedID, cursor string, limit int, access CampaignAccess) (domain.LeadershipShedVideos, error)
	// ListLeadershipSheds is the gallery read: ONE keyset page of buckets across
	// tasks, each with its own first page of evidence. It replaces the client
	// pattern of expanding a task page into buckets and calling the single-shed
	// read once per bucket.
	//
	// parkIDs is the caller's capability-scoped park set and is part of the QUERY, not a
	// post-filter: dropping unauthorized rows after the page was cut returned short (or
	// empty) pages to a park-scoped monitor whenever another park's buckets happened to
	// occupy the page, while authorized buckets sat unreachable further down the keyset.
	// A nil/empty slice means unrestricted (tenant-wide authority or an internal caller);
	// it is NOT "authorized for nothing", because this port has no way to tell the two
	// apart and the service is the layer that knows.
	ListLeadershipSheds(ctx context.Context, tenantID string, parkIDs []string, cursor string, limit, perShedLimit int) (domain.LeadershipShedPage, error)
	RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error)
	// AnimalProofWasRejected reports whether a proof is already attached to a rework
	// observation for a different tag in this bucket, so a re-capture for one
	// animal cannot reuse the very video the verifier sent back on another animal.
	AnimalProofWasRejected(ctx context.Context, tenantID, campaignShedID, proofArtifactID, scannedIdentifier string) (bool, error)
	RecordShedObservation(ctx context.Context, cmd domain.RecordShedObservation) (domain.Observation, error)
	SubmitIndividualScope(ctx context.Context, tenantID, campaignID, campaignShedID, actorID, idempotencyKey string, scannedIdentifiers []string) error
	// ReopenScope returns the shed-observation ids whose lump-sum submissions the
	// reopen superseded (withdrawn_at stamped, never deleted -- a rejected proof
	// attempt is immutable history). The app layer hands them to the verification
	// module's own withdraw port so the items raised for them stop being decidable;
	// weighing never writes verification's tables itself. A replay of the same key
	// re-reports the same ids, so a retry heals a crash between commit and withdraw.
	ReopenScope(ctx context.Context, tenantID, campaignID, campaignShedID, actorID, idempotencyKey, reason string) ([]string, error)
	// CloseScope / CloseCampaign are the EXPLICIT terminal actions. Both are
	// allowed while work is still not accepted, and neither may mark unaccepted
	// work accepted. Both follow the ReopenScope transaction shape (fingerprint ->
	// exact-replay readback -> state change -> audit -> idempotency record ->
	// outbox enqueue, all in ONE transaction).
	CloseScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error)
	CloseCampaign(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error)
	// CampaignParkID resolves the park a campaign runs in. It exists because the park is the
	// ROUTING key of a weighing verification item (the notification consumer resolves the park's
	// verify-duty holders from it), while an observation row itself only knows its shed. One
	// indexed primary-key lookup per observation write, never a scan.
	CampaignParkID(ctx context.Context, tenantID, campaignID string) (string, error)
	// CampaignShedLocation resolves the shed a campaign-shed bucket stands for: its canonical
	// location_id, and the operational display the VERIFIER reads ("Godel 1 - Part 3", partition
	// included). It exists because a weighing verification item named no shed at all -- lump-sum
	// items carried the literal "Whole shed" and a NULL shed_id, and individual items carried only
	// the scanned tag -- so the verifier could not tell which shed or partition a clip came from.
	// One indexed primary-key lookup per observation write, never a scan.
	CampaignShedLocation(ctx context.Context, tenantID, campaignShedID string) (locationID, display string, err error)
	// ListAlerts is the module-scoped weighing lifecycle feed: the weighing
	// notifications that were ALREADY routed to this caller, newest first.
	//
	// memberOrUserID is the caller's authenticated user id; the adapter resolves it
	// to the canonical workforce_member_id the same way ResolveMemberRecipients
	// does, because that is the identity the notification consumers stamped on each
	// row. parkIDs is the caller's authorized park list and is IGNORED when
	// tenantWide is true.
	//
	// ISOLATION (maintainer ruling 2026-08-03): this read touches
	// notification_requests and workforce_members ONLY. It never joins goats,
	// goat_identifiers, herd_animals, obligation/protocol/vaccination tables, or
	// any expected-roster source -- weighing is free-flow, so there is no
	// denominator to report against.
	ListAlerts(ctx context.Context, tenantID, memberOrUserID string, tenantWide bool, parkIDs []string, cursor string, limit int) (domain.AlertPage, error)

	// GetWeightHistory returns weight observations per RFID across weigh days,
	// scoped to authorizedParkIDs. If parkID is provided, results are narrowed to that park only.
	// If campaignShedID is provided, results are further filtered to that shed.
	//
	// The response includes:
	// - Complete park and shed vocabulary for the authorized scope (so the client can
	//   render filter chips without a separate request)
	// - Weight time series grouped by scanned_identifier, ordered by weigh date (oldest first)
	// - Truncation flag if any cap is hit (max unique tags, max weigh days, or max points total)
	//
	// This is a bounded chart read, not an export: result caps are enforced and reported.
	GetWeightHistory(ctx context.Context, tenantID string, authorizedParkIDs []string, parkID, campaignShedID string) (domain.WeightHistory, error)

	// GetLeadershipGrowthADG returns the herd-level ADG (Average Daily Gain) read model
	// aggregated across parkIDs (one park, or every park the caller is authorized to monitor)
	// for one half-open period [periodStart, periodEnd), built from individual weighs only. See
	// domain.GrowthADG for the full business-rule contract (goat_identifiers resolution,
	// rejected-vs-pending handling, the same-timestamp-only pair exclusion -- there is NO
	// weighing cadence rule -- and why lump-sum totals never feed per-animal ADG). parkIDs must
	// be non-empty and every id must already be authorization-checked by the caller: this method
	// does no scoping of its own.
	GetLeadershipGrowthADG(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.GrowthADG, error)

	// GetShedWeights returns one row per SHED (not per campaign bucket) carrying that
	// shed's most recent weigh inside the half-open period [periodStart, periodEnd),
	// plus the whole-filter KPI rollup behind it. Both capture modes are covered:
	// per-animal sheds aggregate their deduplicated scans, lump-sum sheds report their
	// live shed observation, and the two are SELECTED between by weighing_category
	// rather than summed. See domain.ShedWeights for the grain and threshold-basis
	// contract. parkIDs must be non-empty and already authorization-checked by the
	// caller: this method does no scoping of its own.
	GetShedWeights(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.ShedWeights, error)

	// GetWeightDemographics returns average weight by breed, sex and management stage.
	// This is the ONE weighing read permitted to resolve a scanned tag to its animal
	// (maintainer decision 2026-08-07); see domain.WeightDemographics for the scope.
	GetWeightDemographics(ctx context.Context, tenantID string, parkIDs []string, periodStart, periodEnd time.Time) (domain.WeightDemographics, error)

	// ExportCampaignCSV exports weighing observations for a campaign as CSV.
	// It streams CSV-formatted rows to the provided writer, including both individual
	// and lump-sum observations, with verification status and proof references.
	// The CSV includes a header row and is properly escaped for fields containing quotes/commas.
	ExportCampaignCSV(ctx context.Context, tenantID, campaignID string, writer io.Writer) error
}

// VerificationVerdictStore is the narrow write side the weighing verdict consumer
// drives. It is deliberately NOT part of Repository: the consumer runs on the
// durable event bus, not behind the HTTP service, so it must not be able to reach
// the planner/execution writes.
type VerificationVerdictStore interface {
	ApplyVerificationVerdict(ctx context.Context, verdict domain.VerificationVerdict) (domain.VerificationVerdictResult, error)
}

// WeighingKernelStore is the PHASE 2 time-driven kernel write/read side. It is
// deliberately NOT part of Repository: the kernel worker runs on a cadence, not
// behind the HTTP service, so it must not be able to reach planner/execution
// writes. Weighing has no separate worker binary — this store is driven by the
// consolidated kernel worker (backend/cmd/kernel-worker) operational cadence.
type WeighingKernelStore interface {
	// SweepWorkItems is one BOUNDED, resumable, forward-progressing tick:
	// terminal reconciliation, roll-forward, delayed/escalation, day-start. Every
	// pass is keyset-chunked with FOR UPDATE SKIP LOCKED and every cadence event is
	// enqueued in the same transaction as the state change.
	SweepWorkItems(ctx context.Context, params domain.KernelSweepParams) (domain.KernelSweepResult, error)
}

// WeighingReworkDigestStore batches the rework push PER SHED.
//
// A rework verdict arrives per observation, asynchronously, one event at a time, and
// nothing in the system marks "the verifier finished reviewing this shed". Rather than
// invent that moment as a queue, it is derived: an un-delivered bounce is exactly
// `verification_status='rework' AND rework_notified_at IS NULL`. This sweep groups those
// rows per bucket, emits ONE weighing.observation.rework_digest naming the animals, and
// stamps the rows it named IN THE SAME TRANSACTION -- so a crash between the two is
// impossible and a bounce can never be silently dropped.
//
// Same isolation rule as WeighingKernelStore: driven by the consolidated kernel worker
// on a cadence, never reachable from the HTTP service.
type WeighingReworkDigestStore interface {
	SweepReworkDigests(ctx context.Context, params domain.ReworkDigestSweepParams) (domain.ReworkDigestSweepResult, error)
}

// WeighingProcessStateReader is the Calendar + Control Tower binding: dot-grain
// day markers plus a whole-filter, disjoint-bucket summary at
// `weighing_work_item` grain.
type WeighingProcessStateReader interface {
	WeighingProcessState(ctx context.Context, tenantID, campaignID, fromBusinessDate, toBusinessDate string) (domain.ProcessState, error)
}

// CategoryFlipConflict is an edit refusing to change a bucket's weighing category while that
// bucket already holds captures. The two categories write to different tables and every count
// sums both, so a flip double counts the same animals instead of migrating them.
type CategoryFlipConflict struct {
	Sheds []string
}

func (e *CategoryFlipConflict) Error() string {
	return "weighing: cannot change weighing category for buckets that already hold captures: " +
		strings.Join(e.Sheds, ", ")
}
