package app

import (
	"context"
	"fmt"
	"math"
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

// WithProcessStateReader wires the PHASE 2 Calendar / Control Tower binding. It is
// optional injection (like the verification enqueuer) so the planner/execution
// port surface does not grow a read model every fake has to implement.
func (s *Service) WithProcessStateReader(reader ports.WeighingProcessStateReader) *Service {
	s.processState = reader
	return s
}

// checkParkScope enforces park-scoped access control for mutation operations.
// It resolves the campaign's park and verifies the actor is authorized to access it
// for the WeighingMonitor capability -- the reopen/close/abandon authority.
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
		return s.repo.ListCampaignsForOperator(ctx, actor.TenantID, actor.UserID, parkID, strings.TrimSpace(cursor), limit)
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
	return s.repo.PlannerCatalog(ctx, actor.TenantID, periodStartDate)
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

func (s *Service) ListScopeRoster(ctx context.Context, actor domain.Actor, campaignID, campaignShedID string, cursor string, observationsCursor string, limit int, includeRoster bool) (domain.RosterPage, error) {
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
	return s.repo.ListScopeRosterForOperator(ctx, actor.TenantID, campaignID, campaignShedID, actor.UserID, strings.TrimSpace(cursor), strings.TrimSpace(observationsCursor), limit, includeRoster)
}

// ListCampaignSheds pages ONE task's buckets for the task-detail screen.
//
// Authority mirrors the task list it drills from: an assignee (weighing.execute)
// sees only their OWN buckets on the task, while a planner/monitor sees all of
// them. That is deliberately not "monitor widens execute" — it is the same split
// the list already applies, so the detail cannot show a bucket the list did not.
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
	operatorFilter := ""
	if !canMonitor && !canPlan {
		operatorFilter = actor.UserID
	}
	if limit <= 0 {
		limit = domain.CampaignShedPageSize
	}
	if limit > domain.MaxCampaignShedPageSize {
		limit = domain.MaxCampaignShedPageSize
	}
	return s.repo.ListCampaignSheds(ctx, actor.TenantID, campaignID, operatorFilter, strings.TrimSpace(cursor), limit)
}

func (s *Service) GetLeadershipShedVideos(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, cursor string, limit int) (domain.LeadershipShedVideos, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.LeadershipShedVideos{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) {
		return domain.LeadershipShedVideos{}, ports.ErrInvalidArgument
	}
	// The role check above only proves the actor holds WeighingMonitor SOMEWHERE; it does not
	// prove they hold it in THIS campaign's park. Resolve the campaign's park (same lookup
	// checkParkScope uses) and authorize it before returning any evidence -- otherwise a
	// park-scoped monitor for park A could read park B's leadership shed videos by campaign ID.
	parkID, err := s.repo.CampaignParkID(ctx, actor.TenantID, campaignID)
	if err != nil {
		return domain.LeadershipShedVideos{}, err
	}
	if err := s.checkParkScopeForCapability(ctx, actor.TenantID, parkID, permissions.WeighingMonitor); err != nil {
		return domain.LeadershipShedVideos{}, err
	}
	if limit <= 0 {
		limit = domain.LeadershipShedVideosPageSize
	}
	if limit > domain.MaxLeadershipShedVideosPageSize {
		limit = domain.MaxLeadershipShedVideosPageSize
	}
	return s.repo.GetLeadershipShedVideos(ctx, actor.TenantID, campaignID, campaignShedID, strings.TrimSpace(cursor), limit)
}

// ListLeadershipSheds pages the leadership gallery at BUCKET grain. Same
// monitor-only authority as the single-bucket evidence read it pages.
//
// The repository has no park filter parameter (it pages across every campaign in the
// tenant), so a tenant-wide WeighingMonitor role check alone is not enough: a park-scoped
// monitor for park A would otherwise see every OTHER park's buckets too. Each returned
// bucket is therefore authorized, per-campaign, against the actor's actual capability-scoped
// parks before being handed back; buckets outside the actor's authorized parks are dropped.
// This is a service-layer stopgap -- the correct long-term fix is a park filter pushed into
// the repository query, which is out of scope for this authorization fix.
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
	page, err := s.repo.ListLeadershipSheds(ctx, actor.TenantID, strings.TrimSpace(cursor), limit, domain.LeadershipShedVideosPageSize)
	if err != nil {
		return domain.LeadershipShedPage{}, err
	}

	grants := httpmiddleware.AuthGrantsFromContext(ctx)
	if hasTenantWideCapability(grants, actor.TenantID, permissions.WeighingMonitor) {
		return page, nil
	}
	authorizedParkIDs := map[string]struct{}{}
	for _, id := range httpmiddleware.AuthorizedParkIDsForCapability(grants, permissions.WeighingMonitor) {
		authorizedParkIDs[id] = struct{}{}
	}
	parkIDCache := map[string]bool{}
	filtered := page.Items[:0]
	for _, item := range page.Items {
		allowed, ok := parkIDCache[item.CampaignID]
		if !ok {
			// Memoized by parkIDCache: this runs once per DISTINCT campaign on an
			// already-paginated page (in practice 1), not once per row.
			// scale-guard:ignore: memoized per distinct campaign on a bounded page
			parkID, err := s.repo.CampaignParkID(ctx, actor.TenantID, item.CampaignID)
			if err != nil {
				continue
			}
			_, allowed = authorizedParkIDs[parkID]
			parkIDCache[item.CampaignID] = allowed
		}
		if allowed {
			filtered = append(filtered, item)
		}
	}
	page.Items = filtered
	return page, nil
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
	if err := s.enqueueVerification(ctx, cmd.TenantID, parkID, cmd.CampaignID, cmd.CampaignShedID, cmd.RecordedBy, obs, []string{obs.ProofArtifactID}, "individual animal weight"); err != nil {
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
	if err := s.enqueueVerification(ctx, cmd.TenantID, parkID, cmd.CampaignID, cmd.CampaignShedID, cmd.RecordedBy, obs, mediaRefs, "lump-sum shed weight"); err != nil {
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

// AbandonScope ends a bucket WITHOUT the verification gate, for work that will
// never finish. Same permission and validation as CloseScope; the reason is what
// justifies skipping verification, so it stays mandatory.
func (s *Service) AbandonScope(ctx context.Context, actor domain.Actor, campaignID, campaignShedID, idempotencyKey, reason string) (domain.CloseResult, error) {
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
	return s.repo.AbandonScope(ctx, domain.CloseCommand{
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
