package ports

import (
	"context"
	"errors"
	"strings"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

var (
	ErrForbidden           = errors.New("weighing: forbidden")
	ErrInvalidArgument     = errors.New("weighing: invalid argument")
	ErrNotFound            = errors.New("weighing: not found")
	ErrIdempotencyConflict = errors.New("weighing: idempotency conflict")
	ErrImmutable           = errors.New("weighing: immutable")
	ErrScopeIncomplete     = errors.New("weighing: scope incomplete")

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
	// PlannerCatalog is the PARK-grain planner read for ONE weigh date: EVERY park
	// the planner may use, each with a park-grain shed COUNT (not shed rows), plus
	// the operator picker. Bounded by domain.MaxPlannerParks; there is no park
	// cursor, because a park step that pages cannot offer the parks it has not
	// reached yet.
	PlannerCatalog(ctx context.Context, tenantID string, periodStartDate string) (domain.PlannerCatalog, error)
	// PlannerParkBuckets is ONE keyset page of the sheds of ONE park on ONE weigh
	// date, carrying that date's availability. excludeCampaignID is the task being
	// edited, whose own buckets must not read back as "taken".
	PlannerParkBuckets(ctx context.Context, tenantID, parkID, periodStartDate, excludeCampaignID, cursor string, limit int) (domain.PlannerParkBuckets, error)
	// ListCampaignSheds is the task-DETAIL bucket page. The task list embeds a
	// campaign's whole bucket set; the detail screen reads ~20 at a time instead.
	ListCampaignSheds(ctx context.Context, tenantID, campaignID, operatorUserID, cursor string, limit int) (domain.CampaignShedPage, error)
	ListScopeRoster(ctx context.Context, tenantID, campaignID, campaignShedID string, cursor string, observationsCursor string, limit int, includeRoster bool) (domain.RosterPage, error)
	ListScopeRosterForOperator(ctx context.Context, tenantID, campaignID, campaignShedID, operatorUserID string, cursor string, observationsCursor string, limit int, includeRoster bool) (domain.RosterPage, error)
	// cursor/limit page the shed's INDIVIDUAL observations on (accepted_at,
	// observation_id). The lump-sum row is a single latest read and is not paged.
	GetLeadershipShedVideos(ctx context.Context, tenantID, campaignID, campaignShedID, cursor string, limit int) (domain.LeadershipShedVideos, error)
	// ListLeadershipSheds is the gallery read: ONE keyset page of buckets across
	// tasks, each with its own first page of evidence. It replaces the client
	// pattern of expanding a task page into buckets and calling the single-shed
	// read once per bucket.
	ListLeadershipSheds(ctx context.Context, tenantID, cursor string, limit, perShedLimit int) (domain.LeadershipShedPage, error)
	RecordAnimalObservation(ctx context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error)
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
	// AbandonScope ends a bucket whose work will never finish. Separate from
	// CloseScope on purpose: it skips the verification gate, demands a reason, and
	// records itself distinguishably so it can never read as a verified close.
	AbandonScope(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error)
	CloseCampaign(ctx context.Context, cmd domain.CloseCommand) (domain.CloseResult, error)
	// CampaignParkID resolves the park a campaign runs in. It exists because the park is the
	// ROUTING key of a weighing verification item (the notification consumer resolves the park's
	// verify-duty holders from it), while an observation row itself only knows its shed. One
	// indexed primary-key lookup per observation write, never a scan.
	CampaignParkID(ctx context.Context, tenantID, campaignID string) (string, error)
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

// WeighingProcessStateReader is the Calendar + Control Tower binding: dot-grain
// day markers plus a whole-filter, disjoint-bucket summary at
// `weighing_work_item` grain.
type WeighingProcessStateReader interface {
	WeighingProcessState(ctx context.Context, tenantID, campaignID, fromBusinessDate, toBusinessDate string) (domain.ProcessState, error)
}
