package app

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

type Service struct {
	repo         ports.Repository
	enqueuer     VerificationEnqueuer
	processState ports.WeighingProcessStateReader
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
	SubjectLabel   string
	CapturedAt     time.Time
	IdempotencyKey string
}

func (s *Service) WithVerificationEnqueuer(enqueuer VerificationEnqueuer) *Service {
	s.enqueuer = enqueuer
	return s
}

// WithProcessStateReader wires the PHASE 2 Calendar / Control Tower binding. It is
// optional injection (like the verification enqueuer) so the planner/execution
// port surface does not grow a read model every fake has to implement.
func (s *Service) WithProcessStateReader(reader ports.WeighingProcessStateReader) *Service {
	s.processState = reader
	return s
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

// PlannerCatalog returns the bounded planner vocabulary for ONE weigh date, including
// per-shed availability on that date.
//
// excludeCampaignID is the task currently being edited. Without it an edit would see its
// OWN buckets as already taken and refuse to re-save them, so the planner needs to say
// "everything except this task".
func (s *Service) PlannerCatalog(ctx context.Context, actor domain.Actor, periodStartDate, excludeCampaignID string) (domain.PlannerCatalog, error) {
	canPlan := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false)
	canMonitor := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	if !canPlan && !canMonitor {
		return domain.PlannerCatalog{}, ports.ErrForbidden
	}
	// The catalog's availability is DATE-scoped, so the date has to be a real business
	// date and not merely non-empty.
	periodStartDate = strings.TrimSpace(periodStartDate)
	if !isBusinessDate(periodStartDate) {
		return domain.PlannerCatalog{}, ports.ErrInvalidArgument
	}
	excludeCampaignID = strings.TrimSpace(excludeCampaignID)
	if excludeCampaignID != "" && !uuidutil.IsUUIDString(excludeCampaignID) {
		return domain.PlannerCatalog{}, ports.ErrInvalidArgument
	}
	return s.repo.PlannerCatalog(ctx, actor.TenantID, periodStartDate, excludeCampaignID)
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

func (s *Service) GetLeadershipShedVideos(ctx context.Context, actor domain.Actor, campaignID, campaignShedID string) (domain.LeadershipShedVideos, error) {
	if !permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false) {
		return domain.LeadershipShedVideos{}, ports.ErrForbidden
	}
	if !uuidutil.IsUUIDString(campaignID) || !uuidutil.IsUUIDString(campaignShedID) {
		return domain.LeadershipShedVideos{}, ports.ErrInvalidArgument
	}
	return s.repo.GetLeadershipShedVideos(ctx, actor.TenantID, campaignID, campaignShedID)
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
	// FREE-FLOW: the scanned RFID is the required identity. animal_id is NOT
	// accepted as a substitute and is ignored by the write path entirely
	// (maintainer decision 2026-07-31), so a request carrying only a goat UUID is
	// an invalid weighing capture — there is nothing to record as the scan.
	cmd.AnimalID = ""
	if strings.TrimSpace(cmd.ScannedIdentifier) == "" {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	if strings.TrimSpace(cmd.ActualLocationID) != "" && !uuidutil.IsUUIDString(cmd.ActualLocationID) {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	obs, err := s.repo.RecordAnimalObservation(ctx, cmd)
	if err != nil {
		return domain.Observation{}, err
	}
	if err := s.enqueueVerification(ctx, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.RecordedBy, obs, []string{obs.ProofArtifactID}, "individual animal weight"); err != nil {
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
	if len(cmd.ProofArtifactIDs) < 1 || len(cmd.ProofArtifactIDs) > 5 {
		return domain.Observation{}, ports.ErrInvalidArgument
	}
	for _, proofID := range cmd.ProofArtifactIDs {
		if !uuidutil.IsUUIDString(proofID) {
			return domain.Observation{}, ports.ErrInvalidArgument
		}
	}
	obs, err := s.repo.RecordShedObservation(ctx, cmd)
	if err != nil {
		return domain.Observation{}, err
	}
	mediaRefs := obs.ProofArtifactIDs
	if len(mediaRefs) == 0 && obs.ProofArtifactID != "" {
		mediaRefs = []string{obs.ProofArtifactID}
	}
	if err := s.enqueueVerification(ctx, cmd.TenantID, cmd.CampaignID, cmd.CampaignShedID, cmd.RecordedBy, obs, mediaRefs, "lump-sum shed weight"); err != nil {
		return domain.Observation{}, err
	}
	return obs, nil
}

func (s *Service) enqueueVerification(ctx context.Context, tenantID, campaignID, campaignShedID, operatorID string, obs domain.Observation, mediaRefs []string, label string) error {
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
		SubjectLabel:   label,
		CapturedAt:     obs.AcceptedAt,
		IdempotencyKey: fmt.Sprintf("weighing:%s:%s", category, obs.ObservationID),
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
	return s.repo.ReopenScope(ctx, actor.TenantID, campaignID, campaignShedID, actor.UserID, strings.TrimSpace(idempotencyKey), strings.TrimSpace(reason))
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
