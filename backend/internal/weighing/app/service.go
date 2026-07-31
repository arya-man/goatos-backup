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

func (s *Service) ListCampaigns(ctx context.Context, actor domain.Actor, cursor string, limit int) (domain.CampaignPage, error) {
	canMonitor := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	canExecute := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingExecute}, false)
	if !canMonitor && !canExecute {
		return domain.CampaignPage{}, ports.ErrForbidden
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if canExecute && !canMonitor {
		return s.repo.ListCampaignsForOperator(ctx, actor.TenantID, actor.UserID, strings.TrimSpace(cursor), limit)
	}
	return s.repo.ListCampaigns(ctx, actor.TenantID, strings.TrimSpace(cursor), limit)
}

func (s *Service) PlannerCatalog(ctx context.Context, actor domain.Actor, periodStartDate string) (domain.PlannerCatalog, error) {
	canPlan := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingPlan}, false)
	canMonitor := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	if !canPlan && !canMonitor {
		return domain.PlannerCatalog{}, ports.ErrForbidden
	}
	if strings.TrimSpace(periodStartDate) == "" {
		return domain.PlannerCatalog{}, ports.ErrInvalidArgument
	}
	return s.repo.PlannerCatalog(ctx, actor.TenantID, strings.TrimSpace(periodStartDate))
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
	canMonitor := permissions.RolesAuthorize(actor.Roles, []string{permissions.WeighingMonitor}, false)
	if canMonitor {
		return s.repo.ListScopeRoster(ctx, actor.TenantID, campaignID, campaignShedID, strings.TrimSpace(cursor), strings.TrimSpace(observationsCursor), limit, includeRoster)
	}
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
	if cmd.PeriodStartDate == "" || cmd.PeriodEndDate == "" || cmd.StartBusinessDate == "" || len(cmd.Sheds) == 0 {
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
