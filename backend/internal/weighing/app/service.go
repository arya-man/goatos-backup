package app

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

type Service struct {
	repo                   ports.Repository
	enqueuer               VerificationEnqueuer
	verificationWithdrawer VerificationWithdrawer
	processState           ports.WeighingProcessStateReader
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

type VerificationEnqueuer interface {
	EnqueueWeighingVerification(ctx context.Context, in VerificationEnqueueRequest) error
}

type VerificationEnqueueRequest struct {
	TenantID       string
	Category       string
	ObservationID  string
	CampaignID     string
	CampaignShedID string
	MediaRefs      []string
	OperatorID     string
	ShedID         string
	// ParkID is the campaign's park. It is MANDATORY routing data, not decoration:
	// the verification notification consumer resolves the park's verify-duty holders
	// from it, and an item enqueued without a park notifies nobody.
	ParkID         string
	SubjectLabel   string
	CapturedAt     time.Time
	IdempotencyKey string
}

func (s *Service) WithVerificationEnqueuer(enqueuer VerificationEnqueuer) *Service {
	s.enqueuer = enqueuer
	return s
}

// VerificationWithdrawer retires verification items whose weighing source record
// has been superseded. Separate from VerificationEnqueuer so existing fakes that
// only raise items keep satisfying that interface unchanged.
type VerificationWithdrawer interface {
	WithdrawWeighingVerification(ctx context.Context, tenantID, refType string, observationIDs []string) error
}

func (s *Service) WithVerificationWithdrawer(withdrawer VerificationWithdrawer) *Service {
	s.verificationWithdrawer = withdrawer
	return s
}

// VerificationApplyAcker reports back to the verification module that weighing has
// APPLIED a verdict to its own observation.
//
// It is a separate seam from the two above for the same reason they are separate
// from each other: an existing fake that only raises or retires items keeps
// satisfying its own interface unchanged. It is consumed by the verdict handler
// (weighing/app/verification_verdict_handler.go), not by Service -- the ack
// belongs to the applier, which is the only thing that knows an application
// actually happened.
type VerificationApplyAcker interface {
	AckWeighingVerificationApplied(ctx context.Context, tenantID, refType string, observationIDs []string) error
}

// WithProcessStateReader wires the PHASE 2 Calendar / Control Tower binding. It is
// optional injection (like the verification enqueuer) so the planner/execution
// port surface does not grow a read model every fake has to implement.
func (s *Service) WithProcessStateReader(reader ports.WeighingProcessStateReader) *Service {
	s.processState = reader
	return s
}

// checkParkScope enforces park-scoped access control for mutation operations.
// It resolves the campaign's park and verifies the actor is authorized to access it
// for the WeighingMonitor capability -- the reopen/close authority.
//
// Authorization semantics:
//   - Tenant-wide grant with weighing-relevant role = access all parks
//   - Park-scoped grant = must match campaign park AND that grant's role must carry
//     the capability (see checkParkScopeForCapability)
//   - Campaign not found or unauthorized park = ErrNotFound (not leaking existence)
func (s *Service) checkParkScope(ctx context.Context, tenantID, campaignID string) error {
	// Get the campaign's park
	parkID, err := s.repo.CampaignParkID(ctx, tenantID, campaignID)
	if err != nil {
		return err // ErrNotFound if campaign doesn't exist
	}
	return s.checkParkScopeForCapability(ctx, tenantID, parkID, permissions.WeighingMonitor)
}

// checkCampaignParkScopeForAny is checkParkScope for an EITHER/OR surface: it resolves the
// campaign's park and then admits the actor if ANY of `capabilities` is held there.
//
// checkParkScope cannot serve those surfaces because it hardcodes WeighingMonitor, which is the
// reopen/close authority. On a plan-OR-monitor read that hardcoding is a latent lockout:
// an actor holding weighing.plan without weighing.monitor is admitted by the role gate and then
// refused by the park check -- as ErrNotFound, which is the hardest possible failure to diagnose
// because it is indistinguishable from a task that does not exist.
//
// IT HAS NO PRODUCTION CALLER LEFT. Both surfaces that used it -- GetCampaign and
// ListCampaignSheds -- now push their authority INTO the read instead, because resolving the
// park here and reading the data in a second statement is two reads of a mutable value. It is
// kept because the regression tests replay this exact pre-fix algorithm to prove their fixture
// discriminates: a test that cannot demonstrate the old shape LOSING the race pins nothing.
// Anything reaching for it for a new check-then-read surface wants ports.CampaignAccess instead.
func (s *Service) checkCampaignParkScopeForAny(ctx context.Context, tenantID, campaignID string, capabilities ...string) error {
	parkID, err := s.repo.CampaignParkID(ctx, tenantID, campaignID)
	if err != nil {
		return err // ErrNotFound if the campaign does not exist
	}
	return s.checkParkScopeForAnyCapability(ctx, tenantID, parkID, capabilities...)
}

// checkParkScopeForCapability verifies the actor holds `capability` in `parkID`, either via a
// tenant-wide grant whose role carries the capability, or via a park-scoped grant whose OWN
// role carries the capability and whose scope matches parkID.
//
// This is capability-aware by construction: it never separates "which parks am I scoped to"
// from "which capability does that specific grant's role carry" -- the bug this function
// replaces (checkParkScope + a bare AuthorizedParkIDs call) let an actor combine an unrelated
// park grant with a capability-carrying grant scoped to a DIFFERENT park to gain that
// capability in the first park.
func (s *Service) checkParkScopeForCapability(ctx context.Context, tenantID, parkID, capability string) error {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	// No grants at all = internal/service context (CLI, seeder, integration test running off
	// context.Background()), never an HTTP request -- the auth middleware always attaches
	// grants. Same escape hatch ResolveAuthorizedParkScope and the vaccination-execution park
	// filter already use, made explicit here because the park checks on the planner and
	// campaign-write paths now run on code paths those internal callers reach.
	if len(grants) == 0 {
		return nil
	}

	if hasTenantWideCapability(grants, tenantID, capability) {
		return nil
	}

	authorizedParkIDs := httpmiddleware.AuthorizedParkIDsForCapability(grants, capability)
	for _, id := range authorizedParkIDs {
		if id == parkID {
			return nil
		}
	}

	// Park is outside authorized scope for this capability - return not found to hide existence
	return ports.ErrNotFound
}

// WeighingProcessState serves the shared command surfaces: Calendar day markers
// and the Control Tower gap summary, both at the declared `weighing_work_item`
// grain.
//
// The date window is an INCLUSIVE business-date range and must be aligned to
// Asia/Kolkata business-day boundaries by the caller. There is deliberately NO
// pagination on this read: the summary is a whole-filter aggregate computed in the
// database, so it is page-size independent by construction.
func (s *Service) WeighingProcessState(ctx context.Context, actor domain.Actor, campaignID, fromBusinessDate, toBusinessDate string) (domain.ProcessState, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.ProcessState{}, ports.ErrForbidden
	}
	if s.processState == nil {
		return domain.ProcessState{}, ports.ErrNotFound
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID != "" && !uuidutil.IsUUIDString(campaignID) {
		return domain.ProcessState{}, ports.ErrInvalidArgument
	}
	// Same park-blind role check as ListCampaignSheds had: WeighingMonitor answers "somewhere",
	// not "here", so a park-scoped monitor could read any park's campaign aggregates by naming
	// its id. An EMPTY campaign id is the whole-window summary and carries no park to check --
	// it is bounded by tenant only, which is the surface's existing contract.
	if campaignID != "" {
		if err := s.checkParkScope(ctx, actor.TenantID, campaignID); err != nil {
			return domain.ProcessState{}, err
		}
	}
	from := strings.TrimSpace(fromBusinessDate)
	to := strings.TrimSpace(toBusinessDate)
	if !isBusinessDate(from) || !isBusinessDate(to) || to < from {
		return domain.ProcessState{}, ports.ErrInvalidArgument
	}
	return s.processState.WeighingProcessState(ctx, actor.TenantID, campaignID, from, to)
}

// isBusinessDate accepts only a business DATE (YYYY-MM-DD). An instant or a
// `now ± N hours` style value is rejected outright, because weighing work has
// business-day grain and nothing finer.
func isBusinessDate(value string) bool {
	if len(value) != 10 {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func (s *Service) CreateCampaign(ctx context.Context, actor domain.Actor, cmd domain.CreateCampaign) (domain.Campaign, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false) {
		return domain.Campaign{}, ports.ErrForbidden
	}
	cmd.TenantID = actor.TenantID
	cmd.CreatedBy = actor.UserID
	if err := validateCreate(cmd); err != nil {
		return domain.Campaign{}, err
	}
	// The WeighingPlan role check is park-blind. The campaign names its own park, so a planner
	// scoped to one park could otherwise CREATE weighing work in another park's sheds.
	if err := s.checkParkScopeForCapability(ctx, actor.TenantID, cmd.ParkID, permissions.WeighingPlan); err != nil {
		return domain.Campaign{}, err
	}
	if cmd.PlannedCapPerDay <= 0 {
		cmd.PlannedCapPerDay = 100
	}
	return s.repo.CreateCampaign(ctx, cmd)
}

func (s *Service) UpdateCampaign(ctx context.Context, actor domain.Actor, campaignID string, cmd domain.UpdateCampaign) (domain.Campaign, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false) {
		return domain.Campaign{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) {
		return domain.Campaign{}, ports.ErrInvalidArgument
	}
	cmd.TenantID = actor.TenantID
	cmd.CreatedBy = actor.UserID
	if err := validateCreate(cmd); err != nil {
		return domain.Campaign{}, err
	}
	// BOTH parks are checked, and deliberately so. checkParkScope resolves the campaign's
	// CURRENT park (so a planner cannot edit somebody else's task), while the cmd.ParkID check
	// covers the payload's TARGET park (so an authorized edit cannot be used to push the task
	// into a park the planner has no authority over). Checking either one alone leaves the
	// other direction open.
	if err := s.checkParkScopeForCapability(ctx, actor.TenantID, cmd.ParkID, permissions.WeighingPlan); err != nil {
		return domain.Campaign{}, err
	}
	parkID, err := s.repo.CampaignParkID(ctx, actor.TenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	if err := s.checkParkScopeForCapability(ctx, actor.TenantID, parkID, permissions.WeighingPlan); err != nil {
		return domain.Campaign{}, err
	}
	if cmd.PlannedCapPerDay <= 0 {
		cmd.PlannedCapPerDay = 100
	}
	return s.repo.UpdateCampaign(ctx, campaignID, cmd)
}

func (s *Service) PublishCampaign(ctx context.Context, actor domain.Actor, campaignID, idempotencyKey string) (domain.Campaign, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false) {
		return domain.Campaign{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || strings.TrimSpace(idempotencyKey) == "" {
		return domain.Campaign{}, ports.ErrInvalidArgument
	}
	// Publishing is what turns a draft into real operator work, so it needs the same park
	// authority as creating it -- the role check above is park-blind.
	parkID, err := s.repo.CampaignParkID(ctx, actor.TenantID, campaignID)
	if err != nil {
		return domain.Campaign{}, err
	}
	if err := s.checkParkScopeForCapability(ctx, actor.TenantID, parkID, permissions.WeighingPlan); err != nil {
		return domain.Campaign{}, err
	}
	return s.repo.PublishCampaign(ctx, actor.TenantID, campaignID, actor.UserID, idempotencyKey)
}

// ListCampaigns serves three DISTINCT surfaces, and the caller names which one it wants.
//
// The scope is explicit because inferring it from the actor's roles is what broke this list.
// The old rule was `canExecute && !canMonitor` as a stand-in for "is a worker", which is true
// only for RoleOperator: a growth director holds BOTH execute and monitor, fell into the
// unfiltered branch, and got every shed in every park with a live Scan action -- including sheds
// whose submit would be refused because the write requires the caller to be the assignee.
//
// Each scope carries its own capability, so no role name appears here:
//
//	ScopeMine      -- my own assigned sheds, the executable work list (WeighingExecute).
//	ScopeAll       -- the planner's flat all-tasks list (WeighingPlan or WeighingMonitor).
//	ScopeOperators -- somebody else's work, READ-ONLY oversight (WeighingOverseeOperators).
//
// ScopeAll and ScopeOperators read the same unfiltered page; they differ in who may ask and in
// what the client renders (the oversight surface has no scan CTA). Neither widens the write:
// recording a weight still requires the caller to be the shed's assignee.
//
// parkID is an OPTIONAL row filter on top of the chosen scope. It narrows the rows only: the
// page's Active/Completed counts are whole-scope aggregates, so switching park chips never
// makes the tab numbers jump.
func (s *Service) ListCampaigns(ctx context.Context, actor domain.Actor, scope domain.CampaignListScope, parkID, cursor string, limit int) (domain.CampaignPage, error) {
	// NOTE: RolesAuthorize requires ALL of the permissions it is given, so an either/or surface
	// is expressed as separate calls rather than a two-element slice.
	var allowed bool
	switch scope {
	case domain.CampaignListScopeMine:
		allowed = permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false)
	case domain.CampaignListScopeAll:
		allowed = permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false) ||
			permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	case domain.CampaignListScopeOperators:
		allowed = permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingOverseeOperators}, false)
	default:
		return domain.CampaignPage{}, ports.ErrInvalidArgument
	}
	if !allowed {
		return domain.CampaignPage{}, ports.ErrForbidden
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	parkID = strings.TrimSpace(parkID)
	if parkID != "" && !uuidutil.IsUUIDString(parkID) {
		return domain.CampaignPage{}, ports.ErrInvalidArgument
	}
	if scope == domain.CampaignListScopeMine {
		// ScopeMine is already narrowed to the actor's OWN assignments, so it needs no park
		// authority: an operator can only ever be assigned work in a park they work in.
		return s.repo.ListCampaignsForOperator(ctx, actor.TenantID, actor.UserID, parkID, strings.TrimSpace(cursor), limit)
	}
	// ScopeAll and ScopeOperators page across EVERY campaign in the tenant -- the repository
	// has no notion of the actor's scope, only the optional parkID row filter. So the park
	// filter is the only thing standing between a park-scoped planner/overseer and another
	// park's task list, and it arrives from the client. Clamp it to the actor's authority:
	// a named park must be one they hold plan-or-monitor in, and an unnamed one resolves to
	// their own park rather than defaulting to "all parks".
	// The park set MUST be resolved against the SAME capability that admitted this scope, not
	// against plan-or-monitor for every scope. Using the wider set here reintroduced the exact
	// bug this branch exists to kill, one level up: an actor holding WeighingOverseeOperators in
	// park A and WeighingPlan in park B passed the ScopeOperators role gate on A's grant and
	// then got B in the park set from the unrelated planning grant -- reading somebody else's
	// operator work in a park they have no oversight authority in. Each grant's role stays bound
	// to its own scope only if the capability asked for is the one the scope requires.
	scopeCapabilities := planOrMonitorParkCapabilities
	if scope == domain.CampaignListScopeOperators {
		scopeCapabilities = []string{permissions.WeighingOverseeOperators}
	}
	authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, scopeCapabilities...)
	if !tenantWide {
		if parkID != "" {
			if _, ok := authorizedParks[parkID]; !ok {
				return domain.CampaignPage{}, ports.ErrForbidden
			}
		} else {
			switch len(authorizedParks) {
			case 0:
				return domain.CampaignPage{}, ports.ErrForbidden
			case 1:
				for id := range authorizedParks {
					parkID = id
				}
			default:
				// Multi-park without a tenant grant. One query filters one park, so answering
				// for an arbitrary one would show half the work and no error. Make the client
				// name the park -- but with a code it can act on, not a bare 400.
				return domain.CampaignPage{}, ports.ErrParkSelectionRequired
			}
		}
	}
	return s.repo.ListCampaigns(ctx, actor.TenantID, parkID, strings.TrimSpace(cursor), limit)
}

// PlannerCatalog returns the PARK-grain planner vocabulary for ONE weigh date: every park
// the planner may pick plus the operator picker. It carries no shed rows -- the sheds of
// the chosen park are read a page at a time by PlannerParkBuckets.
func (s *Service) PlannerCatalog(ctx context.Context, actor domain.Actor, periodStartDate string) (domain.PlannerCatalog, error) {
	if !s.canPlanOrMonitor(actor) {
		return domain.PlannerCatalog{}, ports.ErrForbidden
	}
	// The existing-task decoration is DATE-scoped, so the date has to be a real business
	// date and not merely non-empty.
	periodStartDate = strings.TrimSpace(periodStartDate)
	if !isBusinessDate(periodStartDate) {
		return domain.PlannerCatalog{}, ports.ErrInvalidArgument
	}
	catalog, err := s.repo.PlannerCatalog(ctx, actor.TenantID, periodStartDate)
	if err != nil {
		return domain.PlannerCatalog{}, err
	}
	// The repository has no park filter (it returns the whole tenant's parks), and the role
	// check above only says the actor may plan SOMEWHERE. Without this a park-scoped planner
	// got every park in the tenant as a pickable option -- and each option carries that park's
	// kid/shed counts and its existing campaign, so it leaked the other park's numbers before
	// anything was even selected. Drop parks outside the actor's capability-scoped set.
	//
	// Service-layer filter, same stopgap shape as ListLeadershipSheds: the catalog is
	// PARK-grain and unpaginated (one row per park, single digits), so filtering it here is
	// not a page-shrinking hazard. The long-term fix is a park filter in the query.
	authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, planOrMonitorParkCapabilities...)
	if tenantWide {
		return catalog, nil
	}
	// New slices, NOT an in-place catalog.Parks[:0] filter: that overwrites the repository's own
	// backing array, which is harmless for a per-call Postgres read and silently corrupting for
	// any future memoizing decorator.
	parks := make([]domain.PlannerPark, 0, len(catalog.Parks))
	for _, park := range catalog.Parks {
		if _, ok := authorizedParks[park.ParkID]; ok {
			parks = append(parks, park)
		}
	}
	catalog.Parks = parks

	// The OPERATOR list needs the same filter, and not filtering it made the park fix a
	// half-fix: the operator query is tenant-wide with no park predicate and every row carries
	// that person's park_ids, so a park-scoped planner still received every assignable member of
	// the whole tenant -- names, display codes and park membership. It also drove admin-web to
	// pre-select operators[0], who could belong to a park the planner cannot see, producing an
	// operator_outside_park 409 on save with no way to understand why.
	//
	// An operator is offered when ANY of their parks is one the actor may plan in -- or when
	// their ParkIDs is EMPTY, which on this type means "every park" (a cross-park director), NOT
	// "no parks". Dropping the empty case would hide exactly the people who can cover both parks,
	// and they are the ones a planner reaches for when their own park is short-handed.
	operators := make([]domain.PlannerOperator, 0, len(catalog.Operators))
	for _, operator := range catalog.Operators {
		if len(operator.ParkIDs) == 0 {
			operators = append(operators, operator)
			continue
		}
		for _, parkID := range operator.ParkIDs {
			if _, ok := authorizedParks[parkID]; ok {
				operators = append(operators, operator)
				break
			}
		}
	}
	catalog.Operators = operators
	return catalog, nil
}

// PlannerParkBuckets returns ONE keyset page of the chosen park's sheds with their
// availability on the requested weigh date.
//
// excludeCampaignID is the task currently being edited. Without it an edit would see its
// OWN buckets as already taken and refuse to re-save them, so the planner needs to say
// "everything except this task".
func (s *Service) PlannerParkBuckets(ctx context.Context, actor domain.Actor, parkID, periodStartDate, excludeCampaignID, cursor string, limit int) (domain.PlannerParkBuckets, error) {
	if !s.canPlanOrMonitor(actor) {
		return domain.PlannerParkBuckets{}, ports.ErrForbidden
	}
	parkID = strings.TrimSpace(parkID)
	if !uuidutil.IsUUIDString(parkID) {
		return domain.PlannerParkBuckets{}, ports.ErrInvalidArgument
	}
	// canPlanOrMonitor above is role-only: it says the actor plans SOMEWHERE, not that they
	// plan HERE. This route takes the park straight off the request, so without a park-scope
	// check a planner scoped to one park could page any other park's sheds and their
	// availability by naming its id.
	if err := s.checkParkScopeForAnyCapability(ctx, actor.TenantID, parkID, planOrMonitorParkCapabilities...); err != nil {
		return domain.PlannerParkBuckets{}, err
	}
	// Availability is DATE-scoped, so the date has to be a real business date.
	periodStartDate = strings.TrimSpace(periodStartDate)
	if !isBusinessDate(periodStartDate) {
		return domain.PlannerParkBuckets{}, ports.ErrInvalidArgument
	}
	excludeCampaignID = strings.TrimSpace(excludeCampaignID)
	if excludeCampaignID != "" && !uuidutil.IsUUIDString(excludeCampaignID) {
		return domain.PlannerParkBuckets{}, ports.ErrInvalidArgument
	}
	if limit <= 0 {
		limit = domain.PlannerBucketPageSize
	}
	if limit > domain.MaxPlannerBucketPageSize {
		limit = domain.MaxPlannerBucketPageSize
	}
	return s.repo.PlannerParkBuckets(ctx, actor.TenantID, parkID, periodStartDate, excludeCampaignID, strings.TrimSpace(cursor), limit)
}

// canPlanOrMonitor is the planner's read gate: the planner writes belong to WeighingPlan,
// but read-only oversight (WeighingMonitor) may look at the same vocabulary.
func (s *Service) canPlanOrMonitor(actor domain.Actor) bool {
	if permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false) {
		return true
	}
	return permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
}

// planOrMonitorParkCapabilities is the alternative set behind every planner/oversight surface:
// holding EITHER in a park is enough to plan or watch that park's weighing.
var planOrMonitorParkCapabilities = []string{permissions.WeighingPlan, permissions.WeighingMonitor}

// checkParkScopeForAnyCapability is checkParkScopeForCapability over a set of alternatives:
// the actor passes when ANY of `capabilities` is held in `parkID` (tenant-wide with that
// capability, or a park-scoped grant whose OWN role carries it).
//
// A role check alone is NOT park scope. RolesAuthorize answers "does this actor hold the role
// SOMEWHERE", which is exactly the question a park-scoped planner passes for every park in the
// tenant -- so any surface that names a park must run this too.
func (s *Service) checkParkScopeForAnyCapability(ctx context.Context, tenantID, parkID string, capabilities ...string) error {
	var err error
	for _, capability := range capabilities {
		if err = s.checkParkScopeForCapability(ctx, tenantID, parkID, capability); err == nil {
			return nil
		}
	}
	return err
}

// authorizedParkSet returns the parks in which the actor holds any of `capabilities`, and
// whether the actor is tenant-wide for one of them (in which case the set is meaningless and
// no filtering applies).
func authorizedParkSet(ctx context.Context, tenantID string, capabilities ...string) (parks map[string]struct{}, tenantWide bool) {
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	// No grants at all = internal/service context (CLI, integration test), unrestricted.
	if len(grants) == 0 {
		return nil, true
	}
	parks = map[string]struct{}{}
	for _, capability := range capabilities {
		if hasTenantWideCapability(grants, tenantID, capability) {
			return nil, true
		}
		for _, parkID := range httpmiddleware.AuthorizedParkIDsForCapability(grants, capability) {
			parks[parkID] = struct{}{}
		}
	}
	return parks, false
}

func (s *Service) ListScopeRoster(ctx context.Context, actor domain.Actor, campaignID, campaignShedID string, observationsCursor string, limit int) (domain.RosterPage, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return domain.RosterPage{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) {
		return domain.RosterPage{}, ports.ErrInvalidArgument
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	// The scan roster is ALWAYS scoped to the caller's own assigned bucket, monitor or not. This is
	// the data behind the scan screen, and the submit that follows requires
	// cs.operator_user_id = actor -- so serving another assignee's roster produced a fully
	// populated scan screen whose write would be refused.
	//
	// Holding WeighingMonitor does not open someone else's scan roster: overseeing other people's
	// work is a READ-ONLY surface of its own (WeighingOverseeOperators), and reviewing their
	// captured evidence is GetLeadershipShedVideos. Neither route goes through here.
	return s.repo.ListScopeRosterForOperator(ctx, actor.TenantID, campaignID, campaignShedID, actor.UserID, strings.TrimSpace(observationsCursor), limit)
}

// ListCampaignSheds pages ONE task's buckets for the task-detail screen.
//
// Authority mirrors the task list it drills from: an assignee (weighing.execute)
// sees only their OWN buckets on the task, while a planner/monitor sees all of
// them. That is deliberately not "monitor widens execute" — it is the same split
// the list already applies, so the detail cannot show a bucket the list did not.
//
// The authority travels INTO the read rather than being checked before it, so the park that
// admits a bucket and the park on the bucket's task are one value, not two reads of a moving
// one. See the assembly below.
func (s *Service) ListCampaignSheds(ctx context.Context, actor domain.Actor, campaignID, cursor string, limit int) (domain.CampaignShedPage, error) {
	canMonitor := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	canPlan := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false)
	canExecute := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false)
	if !canMonitor && !canPlan && !canExecute {
		return domain.CampaignShedPage{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) {
		return domain.CampaignShedPage{}, ports.ErrInvalidArgument
	}
	// The actor's authority is ASSEMBLED here and EVALUATED in the query, the same shape
	// GetCampaign uses for the task header this page belongs to.
	//
	// It used to call checkCampaignParkScopeForAny, which resolves the campaign's park via
	// repo.CampaignParkID, and then page the buckets in a second, independent statement.
	// park_id is mutable -- UpdateCampaign moves a task between parks -- and nothing spanned
	// the two reads, so a task that moved in the gap was authorized as its OLD park and paged
	// as its NEW one: buckets and their assigned operator display names, which is another
	// park's roster. Re-checking after the read would only add a third read of the same moving
	// value; the durable answer is that the rows returned are the rows the predicate admitted.
	//
	// Nothing about the resulting authority is new. The arms come from the SAME helpers every
	// other park check in this file uses, so there is still exactly one park-scope
	// implementation here.
	access := ports.CampaignAccess{}
	if canMonitor || canPlan {
		// The capability set must match the one the ROLE GATE above admits: plan-or-monitor.
		// checkParkScope's hardcoded WeighingMonitor would be a latent lockout -- an actor
		// holding WeighingPlan WITHOUT WeighingMonitor is admitted by the role gate, resolves
		// the task header via GetCampaign, and would then 404 on its own contents. Unreachable
		// today (only the CEO role holds plan, and it holds monitor too), but closing it before
		// a permission split makes it real is cheaper than after.
		authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, planOrMonitorParkCapabilities...)
		access.Unrestricted = tenantWide
		for parkID := range authorizedParks {
			access.AuthorizedParkIDs = append(access.AuthorizedParkIDs, parkID)
		}
	}
	if canExecute {
		// THE ASSIGNEE ARM IS AN ALTERNATIVE, NOT A NARROWING. It needs no park check -- an
		// operator is park-bound in the database (weighing_operator_park_bound_guard), which is
		// the same reason ScopeMine needs none -- and it must not be gated on park authority: a
		// Growth Director holds WeighingMonitor AND WeighingExecute at once, and one who
		// monitors park A while being ASSIGNED work in park B would otherwise be 404'd on their
		// own buckets (the defect 2c78f87f1 fixed, which GetCampaign already pins). The arm
		// both admits the task and narrows the page to the actor's own buckets, which is
		// exactly what the old execute-only operator filter did.
		access.AssigneeUserID = actor.UserID
	}
	if limit <= 0 {
		limit = domain.CampaignShedPageSize
	}
	if limit > domain.MaxCampaignShedPageSize {
		limit = domain.MaxCampaignShedPageSize
	}
	// An actor admitted by neither arm gets ErrNotFound from the repository, so the cross-park
	// refusal is unchanged and existence is still not leaked.
	return s.repo.ListCampaignSheds(ctx, actor.TenantID, campaignID, strings.TrimSpace(cursor), limit, access)
}

// GetCampaign resolves ONE weighing task by id. It exists for the notification deep link: the
// task list is keyset-paged with no id filter, so a cold tap on a task further down the keyset
// could not be resolved at all -- the client walked a few pages and then honestly reported "not
// found" for work that exists.
//
// Authority is the SAME split ListCampaignSheds applies to the bucket page this task header
// drills into, so the header and its buckets can never disagree about who may see the task:
// an assignee (weighing.execute) resolves it only through their own assignment, while a
// planner/monitor resolves it unfiltered -- after a park check.
//
// That park authority is the whole point. The role gates below are park-BLIND: RolesAuthorize
// answers "do I monitor SOMEWHERE", which a park-scoped monitor passes for every task in the
// tenant. This route takes the task id straight off the request, and leadership pushes now
// carry campaign ids to devices as deep links, so without binding the actor's authority to the
// task's OWN park, naming another park's campaign id would return that park's task, its buckets
// and its operator names. The authority travels INTO the read rather than being checked before
// it, so the park that admits the row and the park on the row are one value, not two reads.
func (s *Service) GetCampaign(ctx context.Context, actor domain.Actor, campaignID string) (domain.Campaign, error) {
	canMonitor := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	canPlan := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false)
	canExecute := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false)
	if !canMonitor && !canPlan && !canExecute {
		return domain.Campaign{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) {
		return domain.Campaign{}, ports.ErrInvalidArgument
	}
	// The actor's authority is ASSEMBLED here and EVALUATED in the query, rather than checked
	// here against a separately fetched park.
	//
	// The previous shape called checkCampaignParkScopeForAny -- which resolves the campaign's
	// park via repo.CampaignParkID -- and then read the campaign in a second, independent
	// statement. park_id is mutable (UpdateCampaign moves a task between parks), and nothing
	// spanned the two reads, so a task that moved in the gap was authorized as its OLD park and
	// returned as its NEW one: a monitor scoped only to park A could be served park B's task
	// detail, operator names included. Re-checking the park AFTER the read would not fix it
	// either; it would just add a third read of the same moving value. The only durable answer
	// is for the row that is returned to be the row the predicate admitted.
	//
	// Nothing about the resulting authority is new -- the arms are the same either/or set the
	// role gate above admits, resolved through the SAME capability helpers every other park check
	// in this file uses, so there is still exactly one park-scope implementation.
	access := ports.CampaignAccess{}
	if canMonitor || canPlan {
		// plan-OR-monitor, matching the role gate. checkParkScope's hardcoded WeighingMonitor
		// would be wrong here: an actor who plans this park without monitoring it is admitted by
		// the role gate and must not then be 404'd.
		authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, planOrMonitorParkCapabilities...)
		access.Unrestricted = tenantWide
		for parkID := range authorizedParks {
			access.AuthorizedParkIDs = append(access.AuthorizedParkIDs, parkID)
		}
	}
	if canExecute {
		// THE ASSIGNEE ARM IS AN ALTERNATIVE, NOT A NARROWING, and it is required for
		// correctness. A Growth Director holds WeighingMonitor AND WeighingExecute at once --
		// that dual role is deliberate, they genuinely execute weighing as well as overseeing it.
		// One who monitors park A while being ASSIGNED work in park B is authorized for B through
		// the assignment alone, and gating on park authority would 404 them on THEIR OWN TASK.
		// It needs no park check for the same reason ScopeMine does not: nobody is assigned work
		// in a park they do not work in, and the repository re-proves the assignee holds a grant
		// covering the task's park anyway.
		access.AssigneeUserID = actor.UserID
	}
	// An actor admitted by neither arm gets ErrNotFound from the repository -- existence is not
	// leaked, and the cross-park refusal is unchanged.
	return s.repo.CampaignByID(ctx, actor.TenantID, campaignID, access)
}

// CampaignCapabilities answers which task-level writes this caller may attempt on THIS task.
//
// It is park-aware, and that is the whole point. The capabilities used to be a bare
// RolesAuthorize answer with no park in it, while CloseCampaign/ReopenCampaign run a park-scope
// check and refuse an unauthorized park with ErrNotFound. A monitor scoped to park A therefore
// got can_end = true on a park-B task and rendered a live Close button whose tap
// answered "not found" -- the very failure the capability map exists to prevent, displaced from
// permission grain to park grain.
//
// Each capability is checked against the capability the corresponding WRITE enforces, in the
// campaign's OWN park: publish is WeighingPlan (see PublishCampaign), end and reopen are
// WeighingMonitor (see checkParkScope, which both close/reopen paths use).
func (s *Service) CampaignCapabilities(ctx context.Context, actor domain.Actor, campaign domain.Campaign) domain.CampaignCapabilities {
	// A zero campaign is what the handler holds when the read failed. There is no park to check
	// and no task to act on, so every answer is no.
	if strings.TrimSpace(campaign.CampaignID) == "" {
		return domain.CampaignCapabilities{}
	}
	can := func(capability string) bool {
		if !permissions.RolesAuthorize(actor.Roles, []string{capability}, false) {
			return false
		}
		return s.checkParkScopeForCapability(ctx, actor.TenantID, campaign.ParkID, capability) == nil
	}
	monitorsThisPark := can(permissions.WeighingMonitor)
	return domain.CampaignCapabilities{
		CanPublish: can(permissions.WeighingPlan),
		CanEnd:     monitorsThisPark,
		CanReopen:  monitorsThisPark,
	}
}

// ListParks returns the parks whose weighing this actor may look at, as filter-chip vocabulary.
//
// It exists because the only park list on this surface was the planner catalog, gated on
// WeighingPlan -- which is CEO-only. A Growth Director holds WeighingMonitor and
// WeighingOverseeOperators and never WeighingPlan, so their oversight park chips 403'd and the
// client degraded to the parks it could see on the rows it happened to have loaded. A filter
// vocabulary derived from the filtered data drops a park as soon as that park's tasks page out.
//
// It is deliberately the actor's CAPABILITY-SCOPED park set and not the tenant's parks: a
// park-scoped monitor may not learn the name or the existence of a park they do not oversee,
// and a chip they cannot use would 403 the list read behind it anyway.
func (s *Service) ListParks(ctx context.Context, actor domain.Actor) ([]domain.WeighingPark, error) {
	if !s.canPlanOrMonitor(actor) &&
		!permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingOverseeOperators}, false) {
		return nil, ports.ErrForbidden
	}
	// The park set spans every capability that admits a surface WITH park chips, because the
	// chips filter all three lists (the planner's flat list, oversight, and the leadership
	// gallery). Narrower would hide a park whose rows the actor can already read on one of them.
	capabilities := append(append([]string{}, planOrMonitorParkCapabilities...), permissions.WeighingOverseeOperators)
	authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, capabilities...)
	if tenantWide {
		// nil, NOT an empty slice: the repository reads an empty set as "match nothing", so a
		// tenant-wide actor would get zero chips (see WeighingParks).
		return s.repo.WeighingParks(ctx, actor.TenantID, nil)
	}
	if len(authorizedParks) == 0 {
		// Passed the flat role gate on some grant, but holds no park here. An unrestricted read
		// would be exactly the escalation this path exists to prevent, so the answer is no chips.
		return []domain.WeighingPark{}, nil
	}
	parkIDs := make([]string, 0, len(authorizedParks))
	for parkID := range authorizedParks {
		parkIDs = append(parkIDs, parkID)
	}
	return s.repo.WeighingParks(ctx, actor.TenantID, parkIDs)
}

func (s *Service) GetLeadershipShedVideos(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, cursor string, limit int) (domain.LeadershipShedVideos, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.LeadershipShedVideos{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) {
		return domain.LeadershipShedVideos{}, ports.ErrInvalidArgument
	}
	// The role check above only proves the actor holds WeighingMonitor SOMEWHERE; it does not
	// prove they hold it in THIS campaign's park, so a park-scoped monitor for park A could
	// otherwise read park B's evidence footage by naming its campaign id.
	//
	// The authority is ASSEMBLED here and EVALUATED in the read. It used to resolve the
	// campaign's park with repo.CampaignParkID, authorize that park, and then fetch the
	// evidence in a second, independent statement. park_id is mutable -- UpdateCampaign moves a
	// task between parks -- and nothing spanned the two reads, so a task that moved in the gap
	// was authorized as its OLD park and had its proof video served from its NEW one. Adding a
	// re-check after the read would just be a third read of the same moving value.
	//
	// WeighingMonitor ALONE, matching the role gate above exactly. Widening to plan-or-monitor
	// would admit a planner the gate already refused; narrowing further would 404 a monitor the
	// gate admitted. There is no assignee arm because there is no assignee branch on this
	// surface -- an operator reviews their own captures through the roster.
	authorizedParks, tenantWide := authorizedParkSet(ctx, actor.TenantID, permissions.WeighingMonitor)
	access := ports.CampaignAccess{Unrestricted: tenantWide}
	for parkID := range authorizedParks {
		access.AuthorizedParkIDs = append(access.AuthorizedParkIDs, parkID)
	}
	if limit <= 0 {
		limit = domain.LeadershipShedVideosPageSize
	}
	if limit > domain.MaxLeadershipShedVideosPageSize {
		limit = domain.MaxLeadershipShedVideosPageSize
	}
	// An actor with no monitored park here is admitted by no arm and gets ErrNotFound from the
	// repository -- the same answer checkParkScopeForCapability gave, so existence is still not
	// leaked and the cross-park refusal is unchanged.
	return s.repo.GetLeadershipShedVideos(ctx, actor.TenantID, campaignID, campaignShedID, strings.TrimSpace(cursor), limit, access)
}

// ListLeadershipSheds pages the leadership gallery at BUCKET grain. Same
// monitor-only authority as the single-bucket evidence read it pages.
//
// A tenant-wide WeighingMonitor role check alone is not enough here: the role says the actor
// monitors SOMEWHERE, so a monitor scoped to park A would otherwise page every OTHER park's
// buckets. The actor's capability-scoped park set therefore goes INTO the repository query.
//
// It used to be applied to the page the repository had already cut, and that stopgap was itself
// a defect: the keyset walks every park in the tenant, so a page could be filled entirely with
// parks the actor may not see and come back EMPTY -- indistinguishable from "no evidence" --
// while their own buckets sat further down the same order with no cursor able to reach them.
// Paginating over already-authorized rows is the only shape in which a page boundary and an
// authorization boundary cannot collide.
func (s *Service) ListLeadershipSheds(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.LeadershipShedPage, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.LeadershipShedPage{}, ports.ErrForbidden
	}
	if limit <= 0 {
		limit = domain.LeadershipShedPageSize
	}
	if limit > domain.MaxLeadershipShedPageSize {
		limit = domain.MaxLeadershipShedPageSize
	}
	// An empty slice is the repository's "unrestricted" arm, which is why the tenant-wide and
	// no-grants cases (see authorizedParkSet) must reach it as nil rather than as an empty set:
	// a tenant-wide monitor is authorized everywhere, not nowhere.
	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	var parkIDs []string
	if !hasTenantWideCapability(grants, actor.TenantID, permissions.WeighingMonitor) &&
		len(grants) > 0 {
		parkIDs = httpmiddleware.AuthorizedParkIDsForCapability(grants, permissions.WeighingMonitor)
		if len(parkIDs) == 0 {
			// Park-scoped grants that carry no monitor capability anywhere: the actor passed the
			// flat role gate but owns no park here. An unrestricted read would be the escalation
			// this whole path exists to prevent, so the answer is an empty page.
			return domain.LeadershipShedPage{Items: []domain.LeadershipShedVideos{}}, nil
		}
	}
	return s.repo.ListLeadershipSheds(ctx, actor.TenantID, parkIDs, strings.TrimSpace(cursor), limit, domain.LeadershipShedVideosPageSize)
}

// hasTenantWideCapability reports whether any grant is scoped to the whole tenant AND carries
// a role that has the given capability. A tenant-wide grant for an unrelated role returns false.
func hasTenantWideCapability(grants []permissions.ActiveGrant, tenantID, capability string) bool {
	for _, grant := range grants {
		if grant.ScopeType == "tenant" && grant.ScopeID == tenantID && permissions.RoleHasPermission(grant.Role, capability) {
			return true
		}
	}
	return false
}

func (s *Service) RecordAnimalObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return domain.Observation{}, ports.ErrForbidden
	}
	cmd.TenantID = actor.TenantID
	cmd.RecordedBy = actor.UserID
	if !uuidutil.IsUUIDString(cmd.CampaignID) || !uuidutil.IsUUIDString(cmd.CampaignShedID) || !uuidutil.IsUUIDString(cmd.ProofArtifactID) || !isPositiveFinite(cmd.WeightKg) || strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	// FREE-FLOW: the scanned RFID is the required identity. There is no
	// animal_id field on this command at all (maintainer decision 2026-08-03,
	// following the 2026-07-31 decision that first made it a no-op) — a
	// request carrying only a goat UUID and no scanned_identifier is an
	// invalid weighing capture, since there is nothing to record as the scan.
	if strings.TrimSpace(cmd.ScannedIdentifier) == "" {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	if strings.TrimSpace(cmd.ActualLocationID) != "" && !uuidutil.IsUUIDString(cmd.ActualLocationID) {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	// Resolve the notification routing park BEFORE persisting. The park is the routing key of
	// the verification item, and an observation row only knows its shed -- so it is read from
	// the campaign. Doing this after the write cannot honour its own contract: the weight is
	// already committed, so returning an error hands the operator a failure over saved data and
	// leaves an observation nobody is asked to verify. Resolve first; a campaign we cannot place
	// in a park is rejected before anything is written.
	parkID, err := s.repo.CampaignParkID(ctx, cmd.TenantID, cmd.CampaignID)
	if err != nil {
		return domain.Observation{}, err
	}
	obs, err := s.repo.RecordAnimalObservation(ctx, cmd)
	if err != nil {
		return domain.Observation{}, err
	}
	if err := s.reviseVerificationRound(ctx, cmd.TenantID, obs); err != nil {
		return domain.Observation{}, err
	}
	if err := s.enqueueVerification(ctx, cmd.TenantID, parkID, cmd.CampaignID, cmd.CampaignShedID, cmd.RecordedBy, obs, []string{obs.ProofArtifactID}, individualSubjectLabel(obs)); err != nil {
		return domain.Observation{}, err
	}
	return obs, nil
}

func (s *Service) RecordShedObservation(ctx context.Context, actor domain.Actor, cmd domain.RecordShedObservation) (domain.Observation, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return domain.Observation{}, ports.ErrForbidden
	}
	cmd.TenantID = actor.TenantID
	cmd.RecordedBy = actor.UserID
	if !isPositiveFinite(cmd.WeightKg) || cmd.AnimalCount <= 0 {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	cmd.AverageWeightKg = cmd.WeightKg / float64(cmd.AnimalCount)
	cmd.ProofArtifactIDs = normalizeProofArtifactIDs(cmd.ProofArtifactID, cmd.ProofArtifactIDs)
	if len(cmd.ProofArtifactIDs) > 0 {
		cmd.ProofArtifactID = cmd.ProofArtifactIDs[0]
	}
	if !uuidutil.IsUUIDString(cmd.CampaignID) || !uuidutil.IsUUIDString(cmd.CampaignShedID) || !isPositiveFinite(cmd.AverageWeightKg) || strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	if len(cmd.ProofArtifactIDs) < 1 || len(cmd.ProofArtifactIDs) > domain.MaxShedProofArtifacts {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	for _, proofID := range cmd.ProofArtifactIDs {
		if !uuidutil.IsUUIDString(proofID) {
			return domain.Observation{}, ports.ErrInvalidArgument
		}
	}
	// Resolve the notification routing park BEFORE persisting. The park is the routing key of
	// the verification item, and an observation row only knows its shed -- so it is read from
	// the campaign. Doing this after the write cannot honour its own contract: the weight is
	// already committed, so returning an error hands the operator a failure over saved data and
	// leaves an observation nobody is asked to verify. Resolve first; a campaign we cannot place
	// in a park is rejected before anything is written.
	parkID, err := s.repo.CampaignParkID(ctx, cmd.TenantID, cmd.CampaignID)
	if err != nil {
		return domain.Observation{}, err
	}
	obs, err := s.repo.RecordShedObservation(ctx, cmd)
	if err != nil {
		return domain.Observation{}, err
	}
	mediaRefs := obs.ProofArtifactIDs
	if len(mediaRefs) == 0 && obs.ProofArtifactID != "" {
		mediaRefs = []string{obs.ProofArtifactID}
	}
	if err := s.enqueueVerification(ctx, cmd.TenantID, parkID, cmd.CampaignID, cmd.CampaignShedID, cmd.RecordedBy, obs, mediaRefs, lumpSumSubjectLabel(obs)); err != nil {
		return domain.Observation{}, err
	}
	return obs, nil
}

// reviseVerificationRound is the B06 root-cause fix.
//
// enqueueVerification fires on EVERY capture, including an edit of a
// not-yet-submitted (or verifier-reworked) observation
// (recordUnknownAnimalObservationTx's `updated` branch). The verification
// idempotency key used to be `weighing:<category>:<observation_id>` --
// content-blind, keyed only on the observation's identity. verification's
// CreateItem is `ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`, so an
// edit's re-enqueue silently no-opped and left the SAME verification_items row
// bound to the OLD weight/proof, including a stale 'verified' decision if a
// verifier had already approved it before the operator touched the draft
// again.
//
// obs.Superseded (set by the repository's updated-vs-inserted CTE branch)
// marks an edit. On that same write the observation's OWN
// verification_status already resets to 'pending' (see
// recordUnknownAnimalObservationTx's `updated` CTE) -- but weighing does not
// own verification_items and must not write it directly, so that reset never
// reached the separate module's row. This closes the gap the same way
// ReopenScope closes it for superseded lump-sum submissions: withdraw the
// stale item through verification's own port BEFORE a new one is raised for
// the new evidence round. enqueueVerification below then keys the new item on
// the capture's AcceptedAt so the withdrawn item's idempotency key is never
// reused (a reused key would just collide with the withdrawn row, DO NOTHING,
// and stay withdrawn instead of raising fresh 'pending' work).
func (s *Service) reviseVerificationRound(ctx context.Context, tenantID string, obs domain.Observation) error {
	if !obs.Superseded || s.verificationWithdrawer == nil {
		return nil
	}
	return s.verificationWithdrawer.WithdrawWeighingVerification(ctx, tenantID, domain.VerificationRefTypeAnimal, []string{obs.ObservationID})
}

// individualSubjectLabel / lumpSumSubjectLabel compose the sentence the VERIFIER reads.
//
// These used to be the hardcoded literals "individual animal weight" and "lump-sum shed
// weight", so every item in a shed's queue rendered identically and the weight under review
// was never shown to the person reviewing it. The verifier's whole job is deciding whether
// the video matches the weight; a mistyped 120 kg looks exactly like a correct 12 kg when
// all she is handed is a video. Both facts are already on the observation this enqueue site
// holds, so the label is composed here -- backend owns it, the clients only render it.
//
// Free-flow rule: the scanned tag IS the identity. Nothing here resolves it to a goat, and
// lump-sum carries no per-animal identity at all, so it names only what the operator
// actually entered: the total on the scale and how many animals it covered.
func individualSubjectLabel(obs domain.Observation) string {
	parts := make([]string, 0, 2)
	if tag := strings.TrimSpace(obs.ScannedIdentifier); tag != "" {
		parts = append(parts, "Tag "+tag)
	}
	parts = append(parts, formatWeightKg(obs.WeightKg))
	return strings.Join(parts, " · ")
}

func lumpSumSubjectLabel(obs domain.Observation) string {
	label := "Whole shed · " + formatWeightKg(obs.WeightKg)
	if obs.AnimalCount > 0 {
		label += " · " + strconv.Itoa(obs.AnimalCount) + " goats"
	}
	return label
}

// formatWeightKg always carries the unit -- a bare number on a verification screen is the
// exact ambiguity this change exists to remove.
func formatWeightKg(weightKg float64) string {
	return strconv.FormatFloat(weightKg, 'f', 1, 64) + " kg"
}

func (s *Service) enqueueVerification(ctx context.Context, tenantID, parkID, campaignID, campaignShedID, operatorID string, obs domain.Observation, mediaRefs []string, label string) error {
	if s.enqueuer == nil {
		return nil
	}
	category := domain.VerificationRefTypeAnimal
	if obs.AnimalCount > 0 || len(mediaRefs) > 1 {
		category = domain.VerificationRefTypeShed
	}
	return s.enqueuer.EnqueueWeighingVerification(ctx, VerificationEnqueueRequest{
		TenantID:       tenantID,
		Category:       category,
		ObservationID:  obs.ObservationID,
		CampaignID:     campaignID,
		CampaignShedID: campaignShedID,
		MediaRefs:      mediaRefs,
		OperatorID:     operatorID,
		ShedID:         obs.ExpectedLocationID,
		ParkID:         parkID,
		SubjectLabel:   label,
		CapturedAt:     obs.AcceptedAt,
		// Versioned on AcceptedAt (the evidence round), not just the observation
		// identity -- see reviseVerificationRound above. AcceptedAt only advances on
		// a genuine new/edited capture (an exact client retry replays the SAME
		// cached observation row via the repo's own idempotency lookup and never
		// reaches the write that stamps a fresh AcceptedAt), so a real retry still
		// re-derives the SAME key here and stays a safe no-op; an edit derives a
		// NEW key so it cannot collide with -- and silently no-op against -- the
		// item just withdrawn for the prior round.
		IdempotencyKey: fmt.Sprintf("weighing:%s:%s:%d", category, obs.ObservationID, obs.AcceptedAt.UTC().UnixNano()),
	})
}

func isPositiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *Service) SubmitIndividualScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey string, scannedIdentifiers []string) error {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false) {
		return ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) || strings.TrimSpace(idempotencyKey) == "" || len(scannedIdentifiers) == 0 {
		return ports.ErrInvalidArgument
	}
	normalized := make([]string, 0, len(scannedIdentifiers))
	seen := make(map[string]struct{}, len(scannedIdentifiers))
	for _, identifier := range scannedIdentifiers {
		identifier = strings.ToLower(strings.TrimSpace(identifier))
		if identifier == "" {
			return ports.ErrInvalidArgument
		}
		if _, exists := seen[identifier]; !exists {
			seen[identifier] = struct{}{}
			normalized = append(normalized, identifier)
		}
	}
	return s.repo.SubmitIndividualScope(ctx, actor.TenantID, campaignID, campaignShedID, actor.UserID, strings.TrimSpace(idempotencyKey), normalized)
}

func (s *Service) ReopenScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) error {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) || strings.TrimSpace(idempotencyKey) == "" {
		return ports.ErrInvalidArgument
	}
	// Enforce park-scoped authorization. Actor must have a grant that covers the campaign's park.
	// Returns ErrNotFound (not ErrForbidden) to hide existence of cross-park campaigns.
	if err := s.checkParkScope(ctx, actor.TenantID, campaignID); err != nil {
		return err
	}
	superseded, err := s.repo.ReopenScope(ctx, actor.TenantID, campaignID, campaignShedID, actor.UserID, strings.TrimSpace(idempotencyKey), strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	// The reopen withdrew the bucket's lump-sum submission. The verification items
	// raised for those submissions must stop being decidable in the same breath:
	// left pending, a verifier approves work the bucket no longer counts, the
	// verdict lands on a superseded row, and the UI reports that non-decision as
	// success. Weighing does not write verification_items -- it asks verification
	// to retire them through verification's own port.
	//
	// This runs AFTER the reopen commits, exactly like the enqueue side of this
	// module. A failure here must not un-reopen the bucket, so it surfaces as the
	// call's error and the operator-visible remedy is a retry with the same
	// idempotency key, which re-reports the same ids and re-runs the withdrawal.
	if len(superseded) > 0 && s.verificationWithdrawer != nil {
		if err := s.verificationWithdrawer.WithdrawWeighingVerification(ctx, actor.TenantID, domain.VerificationRefTypeShed, superseded); err != nil {
			return err
		}
	}
	return nil
}

// CloseScope ends one weighing bucket (campaign shed). Same permission and
// validation shape as ReopenScope: monitor-only, UUID-validated ids, mandatory
// idempotency key. A reason is REQUIRED here (unlike reopen) because a close is
// allowed to strand work that was never accepted, and the reason is the only
// record of why.
func (s *Service) CloseScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) (domain.CloseResult, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.CloseResult{}, ports.ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) || strings.TrimSpace(idempotencyKey) == "" || reason == "" {
		return domain.CloseResult{}, ports.ErrInvalidArgument
	}
	// Enforce park-scoped authorization. Actor must have a grant that covers the campaign's park.
	// Returns ErrNotFound (not ErrForbidden) to hide existence of cross-park campaigns.
	if err := s.checkParkScope(ctx, actor.TenantID, campaignID); err != nil {
		return domain.CloseResult{}, err
	}
	return s.repo.CloseScope(ctx, domain.CloseCommand{
		TenantID:       actor.TenantID,
		CampaignID:     campaignID,
		CampaignShedID: campaignShedID,
		Reason:         reason,
		ClosedBy:       actor.UserID,
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
}

// CloseCampaign ends a whole weighing campaign and every bucket still open under
// it. Buckets whose work was never accepted stay not accepted.
func (s *Service) CloseCampaign(ctx context.Context, actor domain.Actor, campaignID, idempotencyKey, reason string) (domain.CloseResult, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.CloseResult{}, ports.ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if !uuidutil.IsUUIDString(campaignID) || strings.TrimSpace(idempotencyKey) == "" || reason == "" {
		return domain.CloseResult{}, ports.ErrInvalidArgument
	}
	// Enforce park-scoped authorization. Actor must have a grant that covers the campaign's park.
	// Returns ErrNotFound (not ErrForbidden) to hide existence of cross-park campaigns.
	if err := s.checkParkScope(ctx, actor.TenantID, campaignID); err != nil {
		return domain.CloseResult{}, err
	}
	// A client may send a reason CODE rather than author the sentence that is kept
	// forever; the recorded copy is ours, not the phone's.
	reason = domain.ResolveCloseReason(reason)
	return s.repo.CloseCampaign(ctx, domain.CloseCommand{
		TenantID:       actor.TenantID,
		CampaignID:     campaignID,
		Reason:         reason,
		ClosedBy:       actor.UserID,
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
	})
}

func normalizeProofArtifactIDs(primary string, ids []string) []string {
	normalized := make([]string, 0, len(ids)+1)
	seen := make(map[string]struct{}, len(ids)+1)
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	add(primary)
	for _, id := range ids {
		add(id)
	}
	return normalized
}

func validateCreate(cmd domain.CreateCampaign) error {
	if !uuidutil.IsUUIDString(cmd.TenantID) || !uuidutil.IsUUIDString(cmd.ParkID) || !uuidutil.IsUUIDString(cmd.OperatorUserID) || strings.TrimSpace(cmd.IdempotencyKey) == "" {
		return ports.ErrInvalidArgument
	}
	if len(cmd.Sheds) == 0 {
		return ports.ErrInvalidArgument
	}
	// TASK IDENTITY IS ONE PARK ON ONE WEIGH DATE.
	//
	// All three date columns describe that single day. The duplicate guard, the
	// unique index and the planner's "already taken" lookup key on
	// start_business_date, while the campaign keyset orders on
	// period_start_date; a task accepted with those two disagreeing sorts under
	// one date while occupying the slot of another -- exactly the double booking
	// the index exists to stop. They must be a real business DATE and they must
	// be the same date. Unvalidated text also reached `$9::date` and surfaced as
	// a 500 rather than a 400.
	if !isBusinessDate(cmd.PeriodStartDate) || !isBusinessDate(cmd.PeriodEndDate) || !isBusinessDate(cmd.StartBusinessDate) {
		return ports.ErrInvalidArgument
	}
	if cmd.PeriodStartDate != cmd.PeriodEndDate || cmd.PeriodStartDate != cmd.StartBusinessDate {
		return ports.ErrInvalidArgument
	}
	for _, shed := range cmd.Sheds {
		if !uuidutil.IsUUIDString(shed.LocationID) || strings.TrimSpace(shed.DisplayName) == "" {
			return ports.ErrInvalidArgument
		}
		if strings.TrimSpace(shed.OperatorUserID) != "" && !uuidutil.IsUUIDString(shed.OperatorUserID) {
			return ports.ErrInvalidArgument
		}
		switch shed.WeighingCategory {
		case domain.CategoryIndividualAnimal, domain.CategoryPerShedPartition:
		default:
			return ports.ErrInvalidArgument
		}
	}
	return nil
}

// ListAlerts serves the weighing module's own lifecycle feed: the weighing
// work-state transitions that were already routed to THIS caller, newest first.
//
// AUTHORIZATION is weighing-only and deliberately OR, never AND. Everyone with a
// weighing job has a stake in the feed but nobody holds all three capabilities:
// an operator holds execute, the CEO holds plan+monitor but not execute, and the
// Growth Director holds BOTH execute and monitor -- which is intentional, not a
// grant bug, so the gate must not treat "also an executor" as disqualifying for
// the upstream view. This route never touches ObligationRead or VaccinationRead;
// requiring those is exactly what made the previous weighing alerts tab 403 for
// weighing operators and got it deleted.
//
// AUDIENCE needs no second model here. The notification consumers already
// resolved who owns the next action when they wrote each row, so "the alerts sent
// to me" IS the correct per-person scope: an operator cannot see another
// operator's bucket because they were never a recipient of it.
//
// The park filter below is defence in depth on top of that. A caller without a
// tenant-wide weighing capability is narrowed to the parks their grants actually
// reach, so a park-scoped seat cannot read another park's rows even if a future
// producer routes too broadly.
func (s *Service) ListAlerts(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.AlertPage, error) {
	if !permissions.RolesAuthorizeAny(actor.Roles, []string{
		permissions.WeighingExecute,
		permissions.WeighingMonitor,
		permissions.WeighingPlan,
	}) {
		return domain.AlertPage{}, ports.ErrForbidden
	}
	if limit <= 0 {
		limit = domain.AlertPageSize
	}
	if limit > domain.MaxAlertPageSize {
		limit = domain.MaxAlertPageSize
	}

	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	tenantWide := false
	parkIDs := []string{}
	for _, capability := range []string{
		permissions.WeighingExecute,
		permissions.WeighingMonitor,
		permissions.WeighingPlan,
	} {
		if hasTenantWideCapability(grants, actor.TenantID, capability) {
			tenantWide = true
			break
		}
		parkIDs = append(parkIDs, httpmiddleware.AuthorizedParkIDsForCapability(grants, capability)...)
	}
	return s.repo.ListAlerts(ctx, actor.TenantID, actor.UserID, tenantWide, dedupeStrings(parkIDs), strings.TrimSpace(cursor), limit)
}

// dedupeStrings keeps the park-scope argument small and stable; the same park can
// arrive once per weighing capability the caller holds.
func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := make(map[string]struct{}, len(values))
	out := values[:0]
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
