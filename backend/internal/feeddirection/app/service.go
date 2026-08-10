// Package app orchestrates feed-direction generation.
//
// The orchestration is deliberately thin and always the same three bounded reads, in this order:
//
//  1. read the FULL FILTERED SHED SCOPE (unpaged, bounded by the park's active shed catalog);
//  2. batch-read the authored config snapshot for the park (one call, set-based queries);
//  3. batch-read the projected grains for EXACTLY those sheds (one call, shed-set filter).
//
// Then ONE pure in-memory generation over the whole scope, from which the requested page is sliced.
// There is no per-shed read, no per-grain read, and no loop containing a ctx-taking call to an
// injected dependency -- the N+1 fan-out shape the scale rules ban, which a raw-driver check cannot
// see because the query sits an adapter layer down. The read count is CONSTANT: three, whatever the
// page size and whatever the park.
//
// The scope is read whole rather than paged because the summary must cover every row matching the
// filters, and a feed quantity cannot be aggregated in SQL -- see domain.PreviewSummary for the
// full argument. Both scope reads fail closed rather than truncate.
package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	"github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const (
	// DefaultShedPageLimit is the shed page size. Sized to a screen of sheds, not to a park.
	DefaultShedPageLimit = int32(25)
	// MaxShedPageLimit bounds one page.
	MaxShedPageLimit = int32(100)
	// MaxShedPageOffset bounds the OFFSET walk over the park's shed catalog.
	//
	// Bounded LIMIT/OFFSET rather than keyset is defensible HERE and would not be on an event
	// stream: the paged set is the park's shed catalog, a small, stable configuration list (63
	// sheds in the largest live park), not a growing feed. The offset cannot grow with the herd,
	// and a caller past this bound is not reading a screen.
	MaxShedPageOffset = int32(5000)
)

// Clock lets tests pin the business instant the lifecycle stamps and the read path compares
// against. Production passes nil and gets the real clock.
type Clock func() time.Time

// Service generates feed direction and OWNS the issued-sheet lifecycle.
//
// The generation dependencies (config, counts) are always present. The issue/schedule dependencies
// are optional at construction: a pure-generation unit test wires only config+counts and drives the
// live compute through Draft, while production wires the whole set so Preview/PackingWorklist serve
// FROZEN issued rows and the worker can issue/amend/lock.
type Service struct {
	config   ports.ConfigRepository
	counts   ports.ShedCountsReader
	rounding domain.RoundingPolicy
	planners domain.PlannerSet
	issues   ports.IssueStore
	schedule ports.ScheduleReader
	// now resolves "today" for the past-business-date regeneration guard (see
	// ports.ErrPastDateRegenerationBlocked) and stamps the lifecycle issue/amend/lock instants.
	// Injectable so a test can pin a fixed clock rather than racing the real wall clock -- a
	// pinned-date fixture must never depend on when the test suite happens to run.
	now         Clock
	generatedBy string
	// completions is the OPTIONAL feed-completion store (the PACKING write path). Without it,
	// the packing overlay overlays no completion state and CompleteSession is unavailable -- a pure
	// generation unit test wires only config+counts. Production wires it. UNTOUCHED by the
	// distribution verification gate.
	completions ports.CompletionStore
	// distributions is the OPTIONAL feed DISTRIBUTION verification-gated store (a SEPARATE table from
	// completions). Without it the direction overlay overlays no verified state and CompleteDistribution
	// is unavailable. See docs/decisions/feed-distribution-verification.md.
	distributions ports.DistributionCompletionStore
	// distributionEnqueuer enqueues the verifier queue item for a fresh pending distribution completion.
	// Without it CompleteDistribution fails closed rather than stranding a pending_verification row with
	// nothing for a verifier to act on.
	distributionEnqueuer FeedDistributionVerificationEnqueuer
	// packing is the OPTIONAL feed PACKING verification-gated store (a SEPARATE table from both completions
	// and distributions). Without it the packing overlay overlays no verified state and CompletePacking is
	// unavailable. Maintainer decision 2026-07-26 gated packing too. See
	// docs/decisions/feed-distribution-verification.md.
	packing ports.PackingCompletionStore
	// packingEnqueuer enqueues the verifier queue item for a fresh pending packing completion. Without it
	// CompletePacking fails closed rather than stranding a pending_verification row with nothing for a
	// verifier to act on.
	packingEnqueuer FeedPackingVerificationEnqueuer
	// transports owns the daily, non-session Feed Transport shed task and append-only proof attempts.
	transports        ports.TransportStore
	transportEnqueuer FeedTransportVerificationEnqueuer
	// proofs is the OPTIONAL validator for attached video proofs. Nil skips validation.
	proofs ports.ProofValidator
	// alerts is the OPTIONAL reader for the feed module's own lifecycle alerts feed
	// (backend/internal/feeddirection/domain/alerts.go). Without it, ListAlerts fails closed with
	// ErrAlertsUnavailable. See alerts.go.
	alerts ports.AlertsRepository
}

func NewService(config ports.ConfigRepository, counts ports.ShedCountsReader) *Service {
	return &Service{
		config:      config,
		counts:      counts,
		rounding:    domain.StandardRoundingPolicy(),
		planners:    domain.NewPlannerSet(),
		now:         time.Now,
		generatedBy: "feed-direction-service",
	}
}

// WithRoundingPolicy overrides the rounding policy. Present so a test can pin a policy explicitly
// and so the baking-soda seam can be wired without touching the pipeline; production uses
// StandardRoundingPolicy.
func (s *Service) WithRoundingPolicy(policy domain.RoundingPolicy) *Service {
	s.rounding = policy
	return s
}

// WithIssueStore wires the frozen-sheet persistence. Without it, only Draft reads and pure
// generation work; the issued/pending serve path and the lifecycle ops require it.
func (s *Service) WithIssueStore(issues ports.IssueStore) *Service {
	s.issues = issues
	return s
}

// WithScheduleReader wires the feed_schedule_config dispatch clock read.
func (s *Service) WithScheduleReader(schedule ports.ScheduleReader) *Service {
	s.schedule = schedule
	return s
}

// WithClock pins the business clock. Test-only seam: the issue/amend/lock instants, the past-date
// regeneration guard, and the pending-vs-issued read decision all depend on "now".
func (s *Service) WithClock(now Clock) *Service {
	if now != nil {
		s.now = now
	}
	return s
}

// WithNowFunc overrides the clock the past-date regeneration guard uses. Test-only seam; production
// always uses time.Now. Retained as an alias of WithClock because existing tests call it by this
// name; there is a single notion of "now" in the service.
func (s *Service) WithNowFunc(now func() time.Time) *Service {
	return s.WithClock(now)
}

// WithGeneratedBy stamps the generated_by provenance on issued sheets.
func (s *Service) WithGeneratedBy(by string) *Service {
	if strings.TrimSpace(by) != "" {
		s.generatedBy = strings.TrimSpace(by)
	}
	return s
}

// WithCompletionStore wires the feed-completion table. Without it the serve path overlays no
// completion state and CompleteSession returns ports.ErrCompletionUnavailable.
func (s *Service) WithCompletionStore(store ports.CompletionStore) *Service {
	s.completions = store
	return s
}

// WithProofValidator wires optional video-proof validation. Nil skips validation.
func (s *Service) WithProofValidator(proofs ports.ProofValidator) *Service {
	s.proofs = proofs
	return s
}

// WithDistributionStore wires the feed DISTRIBUTION verification-gated table (a SEPARATE record from
// the packing completions store). Without it the direction overlay overlays no verified state and
// CompleteDistribution returns ports.ErrDistributionStoreUnavailable.
func (s *Service) WithDistributionStore(store ports.DistributionCompletionStore) *Service {
	s.distributions = store
	return s
}

// WithDistributionVerificationEnqueuer wires the verifier-queue enqueue seam. Without it,
// CompleteDistribution fails closed rather than flipping a session to pending_verification with no
// verifier queue item.
func (s *Service) WithDistributionVerificationEnqueuer(enqueuer FeedDistributionVerificationEnqueuer) *Service {
	s.distributionEnqueuer = enqueuer
	return s
}

// WithPackingStore wires the feed PACKING verification-gated table (a SEPARATE record from both the
// completions store and the distributions store). Without it the packing overlay overlays no verified
// state and CompletePacking returns ports.ErrPackingStoreUnavailable.
func (s *Service) WithPackingStore(store ports.PackingCompletionStore) *Service {
	s.packing = store
	return s
}

// WithPackingVerificationEnqueuer wires the verifier-queue enqueue seam for packing. Without it,
// CompletePacking fails closed rather than flipping a session to pending_verification with no verifier
// queue item.
func (s *Service) WithPackingVerificationEnqueuer(enqueuer FeedPackingVerificationEnqueuer) *Service {
	s.packingEnqueuer = enqueuer
	return s
}

func (s *Service) WithTransportStore(store ports.TransportStore) *Service {
	s.transports = store
	return s
}

func (s *Service) WithTransportVerificationEnqueuer(enqueuer FeedTransportVerificationEnqueuer) *Service {
	s.transportEnqueuer = enqueuer
	return s
}

// CompleteSession records that one shed-session's feed direction was carried out. It validates the
// request, resolves/normalizes the park and date, validates any attached video proof, then delegates
// the canonical write + audit + outbox to the completion store. Idempotent via the client key.
func (s *Service) CompleteSession(ctx context.Context, in CompleteSessionInput) (ports.CompleteSessionResult, error) {
	if s.completions == nil {
		return ports.CompleteSessionResult{}, ports.ErrCompletionUnavailable
	}
	// Write path: the route already clamped the park to the caller's grant. See CompleteDistribution.
	resolvedPark, err := s.resolveParkID(ctx, in.TenantID, in.ParkID, nil)
	if err != nil {
		return ports.CompleteSessionResult{}, err
	}
	in.ParkID = resolvedPark
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ShedID = strings.TrimSpace(in.ShedID)
	in.Workflow = strings.TrimSpace(in.Workflow)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)

	if in.TenantID == "" || in.ParkID == "" {
		return ports.CompleteSessionResult{}, ports.ErrParkRequired
	}
	if in.ShedID == "" {
		return ports.CompleteSessionResult{}, ports.ErrShedRequired
	}
	if in.TargetDate.IsZero() {
		return ports.CompleteSessionResult{}, ports.ErrInvalidTargetDate
	}
	in.TargetDate = biztime.BusinessDayStart(in.TargetDate)
	if in.SessionNo < 1 {
		return ports.CompleteSessionResult{}, ports.ErrInvalidSession
	}
	// A completion records ONE concrete workflow. Empty ("both") is a read filter, never a completion.
	switch in.Workflow {
	case domain.WorkflowNormal, domain.WorkflowExperiment:
	case "":
		return ports.CompleteSessionResult{}, ports.ErrWorkflowRequired
	default:
		return ports.CompleteSessionResult{}, ports.ErrInvalidWorkflow
	}
	if in.IdempotencyKey == "" {
		return ports.CompleteSessionResult{}, ports.ErrIdempotencyRequired
	}

	// Video is OPTIONAL. When present and a validator is wired, each ref must resolve to a real,
	// completed, tenant-owned upload before the completion is written.
	if s.proofs != nil && len(in.ProofRefs) > 0 {
		ids := make([]string, 0, len(in.ProofRefs))
		for _, ref := range in.ProofRefs {
			id := strings.TrimSpace(ref.ProofID)
			if id == "" {
				return ports.CompleteSessionResult{}, ports.ErrInvalidProof
			}
			ids = append(ids, id)
		}
		if err := s.proofs.ValidateFeedProofs(ctx, in.TenantID, ids); err != nil {
			return ports.CompleteSessionResult{}, err
		}
	}

	return s.completions.CompleteSession(ctx, ports.CompleteSessionParams{
		TenantID:       in.TenantID,
		ParkID:         in.ParkID,
		ShedID:         in.ShedID,
		SessionNo:      in.SessionNo,
		TargetDate:     in.TargetDate,
		Workflow:       in.Workflow,
		ProofRefs:      in.ProofRefs,
		CompletedBy:    strings.TrimSpace(in.CompletedBy),
		IdempotencyKey: in.IdempotencyKey,
		ActorID:        in.ActorID,
		ActorType:      in.ActorType,
		TraceID:        in.TraceID,
	})
}

// CompleteSessionInput is the app-level completion request the HTTP handler builds from the body plus
// the authenticated actor context.
type CompleteSessionInput struct {
	TenantID       string
	ParkID         string
	ShedID         string
	SessionNo      int32
	TargetDate     time.Time
	Workflow       string
	ProofRefs      []domain.ProofRef
	CompletedBy    string
	IdempotencyKey string
	ActorID        string
	ActorType      string
	TraceID        string
}

// directionStatusMap reads ONE park-day's feed_distribution_completions in a single bounded indexed
// read and returns each shed-session's NORMALIZED lifecycle bucket (SessionStatus*) keyed by
// completedKey. A nil/empty map (store unwired, or no rows) is valid: the caller treats a missing key
// as SessionStatusPending. This is the finer-grained successor to the old ListVerifiedDistributions
// overlay -- it now surfaces pending_verification and rework, not just completed. ONE read per serve
// path, so the read-count invariant is preserved.
func (s *Service) directionStatusMap(ctx context.Context, tenantID, parkID string, asOf time.Time) (map[string]string, error) {
	if s.distributions == nil {
		return nil, nil
	}
	list, err := s.distributions.ListDistributionSessionStatuses(ctx, tenantID, parkID, asOf)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, d := range list {
		out[completedKey(d.ShedID, d.PartitionLabel, d.SessionNo, d.Workflow)] = domain.NormalizeSessionStatus(d.Status)
	}
	return out, nil
}

// packingCompletionState is a pen-day's completion state as the serve path needs it: the normalized
// lifecycle bucket plus, for a rework row, the sentence explaining why it came back.
//
// The reason travels WITH the status rather than in a second map because the two are read together
// on every row and a second lookup keyed the same way is a second chance to key it wrong.
type packingCompletionState struct {
	status string
	// rawStatus is the stored row state before NormalizeSessionStatus folds it. It is kept because
	// 'rework' has NO client bucket of its own -- it normalizes to SessionStatusPending, "needs my
	// action again" -- so the normalized value cannot tell a reopened pen from one nobody has packed.
	rawStatus    string
	reworkReason string
}

// packingStatusMap is the packing twin of directionStatusMap, reading feed_packing_completions
// (maintainer decision 2026-07-26 gated packing too).
func (s *Service) packingStatusMap(ctx context.Context, tenantID, parkID string, asOf time.Time) (map[string]packingCompletionState, error) {
	if s.packing == nil {
		return nil, nil
	}
	list, err := s.packing.ListPackingCompletionStatuses(ctx, tenantID, parkID, asOf)
	if err != nil {
		return nil, err
	}
	out := make(map[string]packingCompletionState, len(list))
	for _, d := range list {
		out[packingCompletedKey(d.ShedID, d.PartitionLabel, d.Workflow)] = packingCompletionState{
			status:       domain.NormalizeSessionStatus(d.Status),
			rawStatus:    strings.TrimSpace(d.Status),
			reworkReason: d.ReworkReason,
		}
	}
	return out, nil
}

// stampAndFilterDirectionRows sets each row's LifecycleStatus (default SessionStatusPending) and
// Completed from statusMap, then returns the rows narrowed to statusFilter. statusFilter "" keeps
// every row (stamp only). MUST run over the WHOLE scope BEFORE the shed paging, so the page and its
// summary describe the same status set and pagination stays correct. A shed-session's grains all
// share one status, so filtering keeps or drops a session as a unit.
func stampAndFilterDirectionRows(rows []domain.DirectionRow, statusMap map[string]string, statusFilter string) []domain.DirectionRow {
	out := rows
	if statusFilter != "" {
		out = make([]domain.DirectionRow, 0, len(rows))
	}
	for i := range rows {
		bucket := statusMap[completedKey(rows[i].ShedID, rows[i].PartitionLabel, rows[i].SessionNo, rows[i].Workflow)]
		if bucket == "" {
			bucket = domain.SessionStatusPending
		}
		rows[i].LifecycleStatus = bucket
		rows[i].Completed = bucket == domain.SessionStatusCompleted
		if statusFilter != "" {
			if bucket == statusFilter {
				out = append(out, rows[i])
			}
		}
	}
	return out
}

// stampAndFilterDirectionRowsForPacking stamps and filters the UNDERLYING direction rows for the
// PACKING serve path, keyed at the pen-DAY grain.
//
// It exists because the packing status map is keyed without the session while the direction one is
// keyed with it. Reusing stampAndFilterDirectionRows here compiled fine and missed on every row --
// silently, because a miss defaults to `pending` rather than erroring, so the worklist would have
// looked plausible while a `completed` filter returned nothing and a submitted pen offered itself
// for filming again.
//
// A pen's rows all share one pen-day status, so filtering keeps or drops a pen as a unit -- both of
// its sessions travel together, which is what makes the built PackingRow whole.
func stampAndFilterDirectionRowsForPacking(rows []domain.DirectionRow, statusMap map[string]packingCompletionState, statusFilter string) []domain.DirectionRow {
	out := rows
	if statusFilter != "" {
		out = make([]domain.DirectionRow, 0, len(rows))
	}
	for i := range rows {
		bucket := statusMap[packingCompletedKey(rows[i].ShedID, rows[i].PartitionLabel, rows[i].Workflow)].status
		if bucket == "" {
			bucket = domain.SessionStatusPending
		}
		rows[i].LifecycleStatus = bucket
		rows[i].Completed = bucket == domain.SessionStatusCompleted
		if statusFilter != "" {
			if bucket == statusFilter {
				out = append(out, rows[i])
			}
		}
	}
	return out
}

// stampAndFilterPackingRows is the packing twin of stampAndFilterDirectionRows, keyed at the PEN-DAY
// grain because that is what a packing completion now covers.
func stampAndFilterPackingRows(rows []domain.PackingRow, statusMap map[string]packingCompletionState, statusFilter string) []domain.PackingRow {
	out := rows
	if statusFilter != "" {
		out = make([]domain.PackingRow, 0, len(rows))
	}
	for i := range rows {
		state := statusMap[packingCompletedKey(rows[i].ShedID, rows[i].PartitionLabel, rows[i].Workflow)]
		bucket := state.status
		if bucket == "" {
			bucket = domain.SessionStatusPending
		}
		rows[i].LifecycleStatus = bucket
		rows[i].Completed = bucket == domain.SessionStatusCompleted
		// Carried ONLY while the stored row is actually 'rework'. Re-submitting clears the stored
		// reason, so a stale sentence cannot survive to tell a packer to redo work they already redid.
		if state.rawStatus == domain.PackingStatusRework {
			rows[i].ReworkReason = state.reworkReason
		} else {
			rows[i].ReworkReason = ""
		}
		if statusFilter != "" {
			if bucket == statusFilter {
				out = append(out, rows[i])
			}
		}
	}
	return out
}

// completedKey is the identity of ONE completion: a shed's PEN, in one session, in one workflow.
//
// The pen is not decoration here. It was missing until 2026-08-08, so all three Castro pens shared
// a single key: submitting the Castro - 1 morning video flipped Castro - 2 and Castro - 3 to "in
// review" as well, and one clip stood as proof for pens nobody filmed. Normalized via
// PartitionMatchKey so 'Part 3'/'part 3' are one pen and an undivided shed is a stable 'whole'.
func completedKey(shedID, partitionLabel string, sessionNo int32, workflow string) string {
	return shedID + "|" + domain.PartitionMatchKey(partitionLabel) + "|" + strconv.Itoa(int(sessionNo)) + "|" + workflow
}

// packingCompletedKey is the identity of ONE PACKING completion: a shed's PEN, on one day, in one
// workflow. Distinct from completedKey because packing dropped the session from its grain on
// 2026-08-10 while DISTRIBUTION did not -- distribution is still gated per shed-session and keys on
// completedKey above.
//
// A separate function rather than passing 0 for sessionNo: a shared key that silently means
// "session 0" invites the next author to reuse it for distribution, where it would collapse the
// morning and evening completions into one and mark the evening fed because the morning was.
//
// The PEN is carried for the same reason it is on completedKey, and the reason has not gone away.
// Until 2026-08-08 all three Castro pens shared one key, so the Castro - 1 video flipped Castro - 2
// and Castro - 3 to "in review" and one clip stood as proof for pens nobody filmed. Normalized via
// PartitionMatchKey so 'Part 3'/'part 3' are one pen and an undivided shed is a stable 'whole'.
func packingCompletedKey(shedID, partitionLabel, workflow string) string {
	return shedID + "|" + domain.PartitionMatchKey(partitionLabel) + "|" + workflow
}

// Preview serves one page of feed direction rows for a feed day.
//
// THIS IS NOW A SERVE PATH, NOT A LIVE CALCULATOR. For a real target date it reads the FROZEN issued
// sheet: an issued/amended/locked sheet returns its STORED rows plus lifecycle metadata; a future
// date with no issue returns an explicit pending state naming when it will be issued; a past/current
// date with no issue returns an honest never-issued state. None of those live-computes. The ONLY
// path that live-computes is Draft=true, the deliberate config-authoring what-if escape hatch, and
// that path alone carries the past-date regeneration guard.
func (s *Service) Preview(ctx context.Context, q domain.PreviewQuery) (domain.PreviewPage, error) {
	// Resolve the park BEFORE normalize's park-required check: an omitted park_id defaults to the
	// caller's first authorized park so the client's first load has a sheet to show, rather than a
	// 400 it must recover from. This never mixes parks — exactly one park is selected.
	resolvedPark, err := s.resolveParkID(ctx, q.TenantID, q.ParkID, q.AuthorizedParkIDs)
	if err != nil {
		return domain.PreviewPage{}, err
	}
	q.ParkID = resolvedPark
	normalized, err := s.normalizePreviewQuery(q)
	if err != nil {
		return domain.PreviewPage{}, err
	}
	var page domain.PreviewPage
	if normalized.Draft {
		page, err = s.previewDraft(ctx, normalized)
	} else {
		page, err = s.servePreview(ctx, normalized)
	}
	if err != nil {
		return domain.PreviewPage{}, err
	}
	// LifecycleStatus + the status filter are applied INSIDE the serve/generate paths, over the whole
	// scope before paging (servePreview / servePreviewGenerated), and stamp-only for draft below -- so
	// a status-filtered page and its summary stay consistent and pagination stays correct.
	filters, err := s.buildFilters(ctx, normalized.TenantID, normalized.ParkID, normalized.TargetDate, normalized.AuthorizedParkIDs)
	if err != nil {
		return domain.PreviewPage{}, err
	}
	page.Filters = filters
	return page, nil
}

// previewDraft LIVE-COMPUTES a what-if sheet without touching any issue. It is stamped Draft so a
// client can never mistake it for a frozen, issued document.
func (s *Service) previewDraft(ctx context.Context, normalized domain.PreviewQuery) (domain.PreviewPage, error) {
	result, err := s.generate(ctx, generateRequest{
		tenantID:   normalized.TenantID,
		parkID:     normalized.ParkID,
		targetDate: normalized.TargetDate,
		shedID:     normalized.ShedID,
		sessionNo:  normalized.SessionNo,
		limit:      normalized.Limit,
		offset:     normalized.Offset,
	})
	if err != nil {
		return domain.PreviewPage{}, err
	}
	// Draft pages inside generate, so the status filter (which must precede paging) cannot apply here;
	// stamp LifecycleStatus/Completed onto the page rows only. Draft is the config-authoring what-if,
	// not an operator worklist, so an ignored status filter is acceptable.
	statusMap, err := s.directionStatusMap(ctx, normalized.TenantID, normalized.ParkID, normalized.TargetDate)
	if err != nil {
		return domain.PreviewPage{}, err
	}
	// One row per operational location, on the same terms as the served paths (see
	// domain.CollapseDirectionRowsByLocation). Draft pages inside generate, so page and scope are
	// folded separately; both folds are keyed on (shed, partition, session), and a shed's rows never
	// straddle a page, so the page is exactly the collapsed rows of its own sheds.
	pageRows := domain.CollapseDirectionRowsByLocation(stampAndFilterDirectionRows(result.pageRows, statusMap, ""))
	return domain.PreviewPage{
		Items:      pageRows,
		Summary:    domain.SummarizeScope(domain.CollapseDirectionRowsByLocation(result.scopeRows), result.config.PlannedFeedItems()),
		Lifecycle:  domain.Lifecycle{State: domain.LifecycleStateDraft, Workflows: []domain.WorkflowLifecycle{}},
		Draft:      true,
		TargetDate: biztime.BusinessDate(normalized.TargetDate),
		Limit:      result.limit,
		Offset:     result.offset,
		HasMore:    result.hasMore,
	}, nil
}

// PackingWorklist generates one page of the per-shed packing view.
//
// It is built from the SAME generated rows as the preview -- not from a second, independently
// rounded computation -- so the bag a packer fills always matches the sheet the direction printed.
// Read-only: no proof capture, no video, no packing status is recorded anywhere. The status field
// is derived from the generation result.
func (s *Service) PackingWorklist(ctx context.Context, q domain.PackingQuery) (domain.PackingPage, error) {
	resolvedPark, err := s.resolveParkID(ctx, q.TenantID, q.ParkID, q.AuthorizedParkIDs)
	if err != nil {
		return domain.PackingPage{}, err
	}
	q.ParkID = resolvedPark
	normalized, err := s.normalizePackingQuery(q)
	if err != nil {
		return domain.PackingPage{}, err
	}
	var page domain.PackingPage
	if normalized.Draft {
		page, err = s.packingDraft(ctx, normalized)
	} else {
		page, err = s.servePacking(ctx, normalized)
	}
	if err != nil {
		return domain.PackingPage{}, err
	}
	// LifecycleStatus + the status filter are applied INSIDE servePacking / servePackingGenerated over
	// the whole scope before paging (stamp-only for draft), same contract as the preview path.
	filters, err := s.buildFilters(ctx, normalized.TenantID, normalized.ParkID, normalized.TargetDate, normalized.AuthorizedParkIDs)
	if err != nil {
		return domain.PackingPage{}, err
	}
	page.Filters = filters
	return page, nil
}

// resolveParkID selects the park a feed read serves. A non-empty park_id is returned as-is (trimmed)
// and validated downstream; an empty park_id defaults to the first park the CALLER is authorized for
// so the client's first load has a sheet to render instead of a park-required error. It never
// selects more than one park, so the one-park-per-sheet invariant holds. A tenant with no parks --
// or a caller authorized for none of them -- still yields ErrParkRequired, the same closed-fail as
// before.
//
// The authorized filter matters even though the route resolver normally hands down a concrete park:
// defaulting to the tenant's FIRST park would serve a park-scoped operator someone else's farm
// whenever their own park is not first in the catalog.
func (s *Service) resolveParkID(ctx context.Context, tenantID, parkID string, authorizedParkIDs []string) (string, error) {
	if trimmed := strings.TrimSpace(parkID); trimmed != "" {
		return trimmed, nil
	}
	if strings.TrimSpace(tenantID) == "" {
		return "", ports.ErrParkRequired
	}
	parks, err := s.config.ListParks(ctx, tenantID)
	if err != nil {
		return "", err
	}
	parks = parksInScope(parks, authorizedParkIDs)
	if len(parks) == 0 {
		return "", ports.ErrParkRequired
	}
	return parks[0].ParkID, nil
}

// parksInScope narrows a park catalog to the caller's own authorized set, PRESERVING catalog order
// so the default-park pick and the dropdown stay deterministic. An empty authorized set means
// unrestricted (tenant-wide principal or internal context) and returns the catalog untouched -- the
// same nil-means-everything contract httpmiddleware.ParkScopeDecision.ParkIDs uses.
func parksInScope(parks []ports.Park, authorizedParkIDs []string) []ports.Park {
	if len(authorizedParkIDs) == 0 {
		return parks
	}
	allowed := make(map[string]struct{}, len(authorizedParkIDs))
	for _, id := range authorizedParkIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			allowed[trimmed] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return parks
	}
	out := make([]ports.Park, 0, len(parks))
	for _, park := range parks {
		if _, ok := allowed[park.ParkID]; ok {
			out = append(out, park)
		}
	}
	return out
}

// buildFilters assembles the backend-owned farm/shed filter vocabulary for the served park. Two
// bounded reads: the tenant park catalog (order-of two parks) and the served park's active shed
// catalog (bounded by physical infrastructure, the same read the generation already trusts). The
// client renders its farm/shed pickers from this and holds no location list of its own.
//
// The park catalog is narrowed to authorizedParkIDs -- the caller's own scope -- so the dropdown
// offers only parks this principal may actually open. Empty means unrestricted (tenant-wide
// principal or internal context). Sheds need no equivalent narrowing: they are already read for the
// SERVED park alone, and that park was clamped to the caller's scope at the route boundary.
func (s *Service) buildFilters(ctx context.Context, tenantID, servedParkID string, asOf time.Time, authorizedParkIDs []string) (domain.FeedFilterOptions, error) {
	parks, err := s.config.ListParks(ctx, tenantID)
	if err != nil {
		return domain.FeedFilterOptions{}, err
	}
	scope, err := s.config.ListShedScope(ctx, ports.ShedScopeQuery{TenantID: tenantID, ParkID: servedParkID})
	if err != nil {
		return domain.FeedFilterOptions{}, err
	}
	// The session vocabulary is the served park's authored split, effective on the feed day. It is a
	// dedicated bounded read (~10 rows), NOT the full config snapshot: the read-count invariant is
	// that the snapshot is loaded exactly once per request, by generation, never here. The filter
	// offers exactly the sessions the sheet can contain -- the client holds no session list of its
	// own (the golden frontend rule). Consistent with the parks/sheds reads above, this runs even on
	// the beyond-horizon/never-issued path so the picker still renders.
	sessions, err := s.config.ListSessionTemplates(ctx, tenantID, servedParkID, asOf)
	if err != nil {
		return domain.FeedFilterOptions{}, err
	}
	parks = parksInScope(parks, authorizedParkIDs)
	parkOptions := make([]domain.FeedFilterPark, 0, len(parks))
	for _, p := range parks {
		parkOptions = append(parkOptions, domain.FeedFilterPark{ParkID: p.ParkID, Label: p.Label})
	}
	shedOptions := make([]domain.FeedFilterShed, 0, len(scope.Items))
	for _, sh := range scope.Items {
		shedOptions = append(shedOptions, domain.FeedFilterShed{ShedID: sh.ShedID, Label: sh.Label, ParkID: servedParkID})
	}
	sessionOptions := make([]domain.FeedFilterSession, 0, len(sessions))
	for _, sess := range sessions {
		sessionOptions = append(sessionOptions, domain.FeedFilterSession{SessionNo: sess.SessionNo, Label: sess.Label})
	}
	return domain.FeedFilterOptions{
		ServedParkID: servedParkID,
		Parks:        parkOptions,
		Sheds:        shedOptions,
		Sessions:     sessionOptions,
	}, nil
}

// packingDraft LIVE-COMPUTES the worklist without touching any issue. See previewDraft.
func (s *Service) packingDraft(ctx context.Context, normalized domain.PackingQuery) (domain.PackingPage, error) {
	result, err := s.generate(ctx, generateRequest{
		tenantID:   normalized.TenantID,
		parkID:     normalized.ParkID,
		targetDate: normalized.TargetDate,
		// sessionNo 0 = every session; the draft card carries the same whole-day breakdown the served
		// one does, so the config author sees exactly what the packer would.
		limit:  normalized.Limit,
		offset: normalized.Offset,
	})
	if err != nil {
		return domain.PackingPage{}, err
	}
	items := result.config.PlannedFeedItems()
	// Draft pages inside generate, so status is stamp-only here (no filter), mirroring previewDraft.
	statusMap, err := s.packingStatusMap(ctx, normalized.TenantID, normalized.ParkID, normalized.TargetDate)
	if err != nil {
		return domain.PackingPage{}, err
	}
	return domain.PackingPage{
		Items:      stampAndFilterPackingRows(domain.BuildPackingRows(result.pageRows, items), statusMap, ""),
		Summary:    domain.SummarizePacking(stampAndFilterPackingRows(domain.BuildPackingRows(result.scopeRows, items), statusMap, ""), items),
		Lifecycle:  domain.Lifecycle{State: domain.LifecycleStateDraft, Workflows: []domain.WorkflowLifecycle{}},
		Draft:      true,
		TargetDate: biztime.BusinessDate(normalized.TargetDate),
		Limit:      result.limit,
		Offset:     result.offset,
		HasMore:    result.hasMore,
	}, nil
}

type generateRequest struct {
	tenantID   string
	parkID     string
	targetDate time.Time
	shedID     string
	sessionNo  int32
	limit      int32
	offset     int32
}

// generateResult carries one generation run: the whole filtered scope, and the page sliced out of
// it.
//
// scopeRows and pageRows come from the SAME GenerateDirection call, which is the property that
// makes the summary trustworthy: the totals are the sum of rows the operator can page through and
// verify, not a parallel computation that could drift from them.
type generateResult struct {
	scopeRows []domain.DirectionRow
	pageRows  []domain.DirectionRow
	config    domain.ConfigSnapshot
	limit     int32
	offset    int32
	hasMore   bool
}

// generate runs the three bounded reads and the pure generation shared by both surfaces.
//
// # WHY THE SCOPE IS GENERATED, NOT JUST THE PAGE
//
// The summary must cover every row matching the filters (see domain.PreviewSummary), and a feed
// quantity is not reconstructable in SQL -- the rounding step is per-cell and non-linear, so the
// only honest park total is the sum of the cells this generator actually produced. So the scope is
// generated once and the page is sliced from it, rather than the page being generated and the
// summary being guessed from it.
//
// THE READ COUNT IS STILL CONSTANT: one shed-scope read, one config snapshot, one batched grain
// read, whatever the page size. Nothing here scales with sheds or grains, and no loop below issues
// I/O -- they only collect ids and slice already-materialized rows.
//
// PAGING BY SHED IS PRESERVED EXACTLY. The page is sliced out of the ordered SHED SCOPE and every
// row of each selected shed travels with it, so a shed's grains still cannot straddle a page
// boundary -- which would present two partial session totals as if each were complete. Slicing
// sheds rather than rows is what keeps that invariant; slicing rows would break it.
func (s *Service) generate(ctx context.Context, req generateRequest) (generateResult, error) {
	// READ 1 -- the FULL filtered shed scope, unpaged. Bounded by the park's active shed catalog
	// (physical infrastructure), not by herd size, and it fails closed rather than truncating.
	scope, err := s.config.ListShedScope(ctx, ports.ShedScopeQuery{
		TenantID: req.tenantID,
		ParkID:   req.parkID,
		ShedID:   req.shedID,
	})
	if err != nil {
		return generateResult{}, err
	}

	// READ 2 -- the authored config for the park, once. Not per shed, not per grain.
	config, err := s.config.LoadConfigSnapshot(ctx, req.tenantID, req.parkID, req.targetDate)
	if err != nil {
		return generateResult{}, err
	}

	pageSheds, hasMore := sliceShedPage(scope.Items, req.limit, req.offset)
	if len(scope.Items) == 0 {
		return generateResult{
			scopeRows: []domain.DirectionRow{},
			pageRows:  []domain.DirectionRow{},
			config:    config,
			limit:     req.limit,
			offset:    req.offset,
		}, nil
	}

	scopeShedIDs := make([]string, 0, len(scope.Items))
	for _, shed := range scope.Items {
		scopeShedIDs = append(scopeShedIDs, shed.ShedID)
	}

	// READ 3 -- every projected grain for the WHOLE scope, in ONE batched call. The loop above only
	// collects ids; it issues no I/O.
	grains, err := s.counts.ProjectedGrainsForSheds(ctx, ports.ProjectedGrainsRequest{
		TenantID:   req.tenantID,
		ParkID:     req.parkID,
		TargetDate: req.targetDate,
		ShedIDs:    scopeShedIDs,
	})
	if err != nil {
		return generateResult{}, err
	}

	// ONE ShedInput per OPERATIONAL LOCATION (shed + partition), not per shed. A planner is chosen
	// per input, so this is what lets one shed run its authored experiment on some partitions while
	// the rest stay on the per-head ration grid -- CBE's Godel 2 has eight partitions and only
	// Parts 3/4/5 are experiments. A shed with no partitions yields exactly one input with an empty
	// label and behaves exactly as it did before.
	//
	// Order is preserved from the shed scope, and every input keeps its ShedID, so paging by shed
	// still keeps all of a shed's partitions together on one page.
	sheds := make([]domain.ShedInput, 0, len(scope.Items))
	for _, shed := range scope.Items {
		for _, part := range domain.SplitGrainsByPartition(grains[shed.ShedID]) {
			sheds = append(sheds, domain.ShedInput{
				ShedID:         shed.ShedID,
				ShedLabel:      shed.Label,
				PartitionLabel: part.PartitionLabel,
				Grains:         part.Grains,
			})
		}
	}

	scopeRows := domain.GenerateDirection(domain.GenerateInput{
		Config:    config,
		Sheds:     sheds,
		SessionNo: req.sessionNo,
		Rounding:  s.rounding,
		Planners:  s.planners,
	})

	return generateResult{
		scopeRows: scopeRows,
		pageRows:  rowsForSheds(scopeRows, pageSheds),
		config:    config,
		limit:     req.limit,
		offset:    req.offset,
		hasMore:   hasMore,
	}, nil
}

// sliceShedPage takes the requested page out of the ordered shed scope.
//
// HasMore is "are there sheds after this page", computed from the scope that is already in hand --
// not a second COUNT query. An offset past the end yields an empty page rather than an error: that
// is a caller walking off the end of a shrinking park, not a bad request.
func sliceShedPage(scope []ports.Shed, limit, offset int32) ([]ports.Shed, bool) {
	start := int(offset)
	if start > len(scope) {
		start = len(scope)
	}
	end := start + int(limit)
	if end > len(scope) {
		end = len(scope)
	}
	return scope[start:end], end < len(scope)
}

// rowsForSheds narrows the generated scope to the sheds on the page, preserving generation order.
//
// A pure slice-and-filter over rows already in memory: no I/O, and no regeneration that could round
// differently from the rows the summary counted.
func rowsForSheds(rows []domain.DirectionRow, sheds []ports.Shed) []domain.DirectionRow {
	if len(sheds) == 0 {
		return []domain.DirectionRow{}
	}
	wanted := make(map[string]struct{}, len(sheds))
	for _, shed := range sheds {
		wanted[shed.ShedID] = struct{}{}
	}
	out := make([]domain.DirectionRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := wanted[row.ShedID]; ok {
			out = append(out, row)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Query normalization
// ---------------------------------------------------------------------------

func (s *Service) normalizePreviewQuery(q domain.PreviewQuery) (domain.PreviewQuery, error) {
	q.TenantID = strings.TrimSpace(q.TenantID)
	q.ParkID = strings.TrimSpace(q.ParkID)
	q.ShedID = strings.TrimSpace(q.ShedID)
	if q.TenantID == "" || q.ParkID == "" {
		return domain.PreviewQuery{}, ports.ErrParkRequired
	}
	if q.TargetDate.IsZero() {
		return domain.PreviewQuery{}, ports.ErrInvalidTargetDate
	}
	// Normalized to a business-day start in Asia/Kolkata AT THE BOUNDARY, so a caller that passed a
	// late-evening UTC instant cannot push the whole generation onto the wrong feed day. Per
	// AGENTS.md, UTC never defines a Goat OS business day.
	q.TargetDate = biztime.BusinessDayStart(q.TargetDate)
	// The past-date regeneration guard applies ONLY to the live-compute (Draft) path. The serve path
	// never recomputes -- it reads the frozen issued sheet or returns an honest never-issued state --
	// so serving a historical feed day is safe and must NOT be rejected. See
	// ports.ErrPastDateRegenerationBlocked and Preview's doc comment.
	if q.Draft && s.isPastBusinessDate(q.TargetDate) {
		return domain.PreviewQuery{}, ports.ErrPastDateRegenerationBlocked
	}

	if q.SessionNo < 0 {
		return domain.PreviewQuery{}, ports.ErrInvalidPaging
	}
	workflow, err := normalizeWorkflowFilter(q.Workflow)
	if err != nil {
		return domain.PreviewQuery{}, err
	}
	q.Workflow = workflow
	limit, offset, err := normalizePaging(q.Limit, q.Offset)
	if err != nil {
		return domain.PreviewQuery{}, err
	}
	q.Limit, q.Offset = limit, offset
	return q, nil
}

// normalizeWorkflowFilter accepts an empty filter (both workflows) or one of the two authored
// workflows. An unrecognised value is REJECTED rather than ignored: silently widening a "normal
// only" request to both workflows would merge the experiment sheet into it.
func normalizeWorkflowFilter(workflow string) (string, error) {
	workflow = strings.TrimSpace(workflow)
	switch workflow {
	case "", domain.WorkflowNormal, domain.WorkflowExperiment:
		return workflow, nil
	default:
		return "", ports.ErrInvalidWorkflow
	}
}

func (s *Service) normalizePackingQuery(q domain.PackingQuery) (domain.PackingQuery, error) {
	q.TenantID = strings.TrimSpace(q.TenantID)
	q.ParkID = strings.TrimSpace(q.ParkID)
	if q.TenantID == "" || q.ParkID == "" {
		return domain.PackingQuery{}, ports.ErrParkRequired
	}
	if q.TargetDate.IsZero() {
		return domain.PackingQuery{}, ports.ErrInvalidTargetDate
	}
	q.TargetDate = biztime.BusinessDayStart(q.TargetDate)
	// Draft-only guard, same reasoning as normalizePreviewQuery: the serve path reads frozen rows and
	// must be allowed to serve a past feed day.
	if q.Draft && s.isPastBusinessDate(q.TargetDate) {
		return domain.PackingQuery{}, ports.ErrPastDateRegenerationBlocked
	}

	workflow, err := normalizeWorkflowFilter(q.Workflow)
	if err != nil {
		return domain.PackingQuery{}, err
	}
	q.Workflow = workflow
	limit, offset, err := normalizePaging(q.Limit, q.Offset)
	if err != nil {
		return domain.PackingQuery{}, err
	}
	q.Limit, q.Offset = limit, offset
	return q, nil
}

// isPastBusinessDate reports whether target is strictly before today's Asia/Kolkata business day.
//
// TODO(feed-followup): persist an immutable generation/count/config snapshot per park/date, then
// allow past-date regeneration against that snapshot instead of this live-state guard.
//
// See ports.ErrPastDateRegenerationBlocked for why: this generator has no persisted direction
// record, so every call recomputes from CURRENT herd/shed-scope state. Allowing a target date in
// the past would silently rewrite that day's direction with today's facts instead of the facts
// that were true on the day being planned.
func (s *Service) isPastBusinessDate(target time.Time) bool {
	today := biztime.BusinessDayStart(s.now())
	return target.Before(today)
}

// normalizePaging applies the declared default to an ABSENT value and REJECTS a present
// out-of-range one. A bad limit is never silently clamped: per AGENTS.md, a present-but-invalid
// value fails rather than being rewritten to something the caller never asked for.
func normalizePaging(limit, offset int32) (int32, int32, error) {
	if limit == 0 {
		limit = DefaultShedPageLimit
	}
	if limit < 0 || limit > MaxShedPageLimit {
		return 0, 0, fmt.Errorf("%w: limit must be between 1 and %d", ports.ErrInvalidPaging, MaxShedPageLimit)
	}
	if offset < 0 || offset > MaxShedPageOffset {
		return 0, 0, fmt.Errorf("%w: offset must be between 0 and %d", ports.ErrInvalidPaging, MaxShedPageOffset)
	}
	return limit, offset, nil
}
