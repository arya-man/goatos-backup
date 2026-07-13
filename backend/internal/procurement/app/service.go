// Package app coordinates procurement/source-entry use-cases.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	"github.com/vgoats/goatos/backend/internal/procurement/domain"
	"github.com/vgoats/goatos/backend/internal/procurement/ports"
)

const (
	defaultListLimit = 100
	maxListLimit     = 500
)

type Service struct {
	repo     ports.Repository
	canceler ports.VaccinationCanceler
	now      func() time.Time
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

func (s *Service) WithVaccinationCanceler(c ports.VaccinationCanceler) *Service {
	s.canceler = c
	return s
}

func (s *Service) ListLoads(ctx context.Context, q domain.LoadQuery) (domain.LoadListResult, error) {
	if err := validateTenant(q.TenantID); err != nil {
		return domain.LoadListResult{}, err
	}
	if q.Limit <= 0 {
		q.Limit = defaultListLimit
	}
	if q.Limit > maxListLimit {
		q.Limit = maxListLimit
	}
	return s.repo.ListLoads(ctx, q)
}

func (s *Service) ActionCenter(ctx context.Context, q domain.WorkQuery) (domain.ActionCenterResponse, error) {
	q, err := s.workDefaults(q)
	if err != nil {
		return domain.ActionCenterResponse{}, err
	}
	result, err := s.repo.ListWorkRows(ctx, q)
	if err != nil {
		return domain.ActionCenterResponse{}, err
	}
	return domain.ActionCenterResponse{
		Source:            domain.SourceAPI,
		Items:             result.Rows,
		CountsByWorkState: result.CountsByWorkState,
		NextCursor:        result.NextCursor,
	}, nil
}

func (s *Service) ProtocolAdherence(ctx context.Context, q domain.WorkQuery) (domain.ProtocolAdherenceResponse, error) {
	q, err := s.workDefaults(q)
	if err != nil {
		return domain.ProtocolAdherenceResponse{}, err
	}
	result, err := s.repo.ListWorkRows(ctx, q)
	if err != nil {
		return domain.ProtocolAdherenceResponse{}, err
	}
	summary := domain.AdherenceSummary{}
	rows := make([]domain.AdherenceRow, 0, len(result.Rows))
	for _, row := range result.Rows {
		summary.ExpectedCount += row.ExpectedCount
		summary.CompletedCount += row.CompletedCount
		if row.WorkState == "completed" {
			summary.ProcessIntactCount++
		} else {
			summary.OpenGapCount++
		}
		if row.WorkState == "blocked" {
			summary.BlockedCount++
		}
		if row.WorkState == "deferred" {
			summary.DeferredCount++
		}
		rows = append(rows, domain.AdherenceRow{
			RowID:      row.RowID,
			LoadID:     row.LoadID,
			GoatID:     row.GoatID,
			Expected:   row.Title,
			Actual:     row.Detail,
			Gap:        blockerText(row),
			Severity:   row.Severity,
			WorkState:  row.WorkState,
			NextAction: row.NextAction,
		})
	}
	if summary.ExpectedCount > 0 {
		summary.AdherencePercent = float64(summary.CompletedCount) / float64(summary.ExpectedCount) * 100
	}
	return domain.ProtocolAdherenceResponse{Source: domain.SourceAPI, Summary: summary, Rows: rows, NextCursor: result.NextCursor}, nil
}

func (s *Service) ControlTower(ctx context.Context, q domain.WorkQuery) (domain.ControlTowerResponse, error) {
	q, err := s.workDefaults(q)
	if err != nil {
		return domain.ControlTowerResponse{}, err
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	q.ExceptionOnly = true
	result, err := s.repo.ListWorkRows(ctx, q)
	if err != nil {
		return domain.ControlTowerResponse{}, err
	}
	summary := domain.ControlTowerSummary{ProcessIntact: true}
	alerts := make([]domain.ControlTowerAlert, 0, len(result.Rows))
	for _, row := range result.Rows {
		if !isControlTowerException(row) {
			continue
		}
		summary.OpenGapCount++
		switch row.Severity {
		case "critical", "broken":
			summary.CriticalCount++
		default:
			summary.WarningCount++
		}
		switch row.WorkType {
		case "dispatch_proof":
			summary.MissingProofCount++
		case "arrival_mismatch":
			summary.ArrivalMismatchCount++
		case "source_entry":
			summary.SourceEntryBlockedCount++
		}
		alerts = append(alerts, domain.ControlTowerAlert{
			RowID:      row.RowID,
			LoadID:     row.LoadID,
			GoatID:     row.GoatID,
			Severity:   row.Severity,
			WorkState:  row.WorkState,
			Title:      row.Title,
			Detail:     row.Detail,
			NextAction: row.NextAction,
			// Command lenses are top-level: evidence links resolve to the top-level Workflows screen scoped
			// by ?domain=procurement, never a nested per-module command route.
			EvidenceLink: "/workflows/" + row.RowID + "?domain=procurement",
		})
	}
	summary.ProcessIntact = summary.OpenGapCount == 0
	return domain.ControlTowerResponse{Source: domain.SourceAPI, Summary: summary, Alerts: alerts}, nil
}

func isControlTowerException(row domain.WorkRow) bool {
	switch row.WorkState {
	case "blocked", "overdue":
		return true
	case "proof_pending":
		return row.WorkType == "dispatch_proof"
	case "rejected", "deferred":
		return row.Severity == "at_risk" || row.Severity == "critical" || row.Severity == "broken"
	}
	return false
}

func (s *Service) WorkflowDrilldown(ctx context.Context, q domain.WorkQuery, rowID string) (domain.WorkflowDrilldownResponse, bool, error) {
	q, err := s.workDefaults(q)
	if err != nil {
		return domain.WorkflowDrilldownResponse{}, false, err
	}
	if err := domain.ValidateRowID(rowID); err != nil {
		return domain.WorkflowDrilldownResponse{}, false, BadRequest("invalid_row_id", "row_id must be a procurement workflow row id")
	}
	row, found, err := s.repo.GetWorkRow(ctx, q, rowID)
	if err != nil || !found {
		return domain.WorkflowDrilldownResponse{}, found, err
	}
	detail, err := s.repo.GetLoadDetail(ctx, q.TenantID, row.LoadID)
	if err != nil {
		return domain.WorkflowDrilldownResponse{}, false, err
	}
	return domain.WorkflowDrilldownResponse{Source: domain.SourceAPI, Row: row, Nodes: workflowNodes(detail)}, true, nil
}

func (s *Service) workDefaults(q domain.WorkQuery) (domain.WorkQuery, error) {
	if err := validateTenant(q.TenantID); err != nil {
		return domain.WorkQuery{}, err
	}
	if q.WorkState != nil && !oneOf(*q.WorkState, "due", "overdue", "proof_pending", "deferred", "blocked", "rejected", "completed") {
		return domain.WorkQuery{}, BadRequest("invalid_work_state", "work_state must be a procurement work state")
	}
	if q.Severity != nil && !oneOf(*q.Severity, "ok", "watch", "at_risk", "critical", "broken") {
		return domain.WorkQuery{}, BadRequest("invalid_severity", "severity must be ok, watch, at_risk, critical, or broken")
	}
	if q.Limit <= 0 {
		q.Limit = defaultListLimit
	}
	if q.Limit > maxListLimit {
		q.Limit = maxListLimit
	}
	return q, nil
}

func blockerText(row domain.WorkRow) string {
	if row.BlockerReason != nil && strings.TrimSpace(*row.BlockerReason) != "" {
		return *row.BlockerReason
	}
	if row.WorkState == "completed" {
		return "complete"
	}
	return row.WorkType
}

func workflowNodes(detail domain.LoadDetail) []domain.WorkflowNode {
	nodes := []domain.WorkflowNode{
		{Key: "load_created", Label: "Load created", State: detail.Load.Status, Timestamp: &detail.Load.CreatedAt, RefID: &detail.Load.LoadID},
		{Key: "source_health", Label: "Source health", State: "pending"},
		{Key: "pre_dispatch", Label: "Pre-dispatch decision", State: "pending"},
		{Key: "dispatch", Label: "Truck loading proof", State: "pending"},
		{Key: "arrival", Label: "Arrival gate", State: "pending"},
		{Key: "intake", Label: "Accepted herd intake", State: "pending"},
	}
	for _, health := range detail.HealthChecks {
		ref := health.HealthCheckID
		nodes[1].State = health.HealthState
		nodes[1].Timestamp = &health.CheckedAt
		nodes[1].RefID = &ref
	}
	for _, decision := range detail.Decisions {
		if decision.DecisionStage != "pre_dispatch" {
			continue
		}
		ref := decision.DecisionID
		nodes[2].State = decision.DecisionType
		nodes[2].Timestamp = &decision.DecidedAt
		nodes[2].RefID = &ref
	}
	for _, handoff := range detail.Transit {
		ref := handoff.HandoffID
		nodes[3].State = handoff.Status + ":" + handoff.DiscrepancyState
		nodes[3].Timestamp = &handoff.DispatchedAt
		nodes[3].RefID = &ref
	}
	for _, review := range detail.ArrivalReviews {
		ref := review.ReviewID
		nodes[4].State = review.Status
		nodes[4].Timestamp = &review.ReviewedAt
		nodes[4].RefID = &ref
	}
	for _, handoff := range detail.PCHandoffs {
		ref := handoff.HandoffID
		nodes[5].State = handoff.EventStatus
		nodes[5].Timestamp = &handoff.AcceptedAt
		nodes[5].RefID = &ref
	}
	return nodes
}

func (s *Service) CreateLoad(ctx context.Context, in ports.CreateLoad) (domain.Load, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.Load{}, err
	}
	if err := requireUUID("source_party_id", in.SourcePartyID); err != nil {
		return domain.Load{}, err
	}
	if err := validateOptionalUUID("source_location_id", in.SourceLocationID); err != nil {
		return domain.Load{}, err
	}
	if in.ExpectedCount < 0 {
		return domain.Load{}, BadRequest("invalid_expected_count", "expected_count must be zero or positive")
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.Load{}, err
	}
	in.Notes = strings.TrimSpace(in.Notes)
	in.Context = jsonObject(in.Context)
	return s.repo.CreateLoad(ctx, in)
}

func (s *Service) GetLoadDetail(ctx context.Context, tenantID, loadID string) (domain.LoadDetail, error) {
	if err := validateTenant(tenantID); err != nil {
		return domain.LoadDetail{}, err
	}
	if err := requireUUID("load_id", loadID); err != nil {
		return domain.LoadDetail{}, err
	}
	out, err := s.repo.GetLoadDetail(ctx, tenantID, loadID)
	if errors.Is(err, ports.ErrNotFound) {
		return domain.LoadDetail{}, NotFound("procurement load was not found")
	}
	return out, err
}

func (s *Service) AddGoatToLoad(ctx context.Context, in ports.AddGoatToLoad) (domain.LoadGoat, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.LoadGoat{}, err
	}
	if err := requireUUID("load_id", in.LoadID); err != nil {
		return domain.LoadGoat{}, err
	}
	if err := validateOptionalUUID("goat_id", in.GoatID); err != nil {
		return domain.LoadGoat{}, err
	}
	if in.GoatID == nil && blankPtr(in.AnimalIdentifier1) {
		return domain.LoadGoat{}, BadRequest("missing_animal_identifier_1", "animal identifier 1 is required")
	}
	// Animal ID 2 stays optional until double RFID tagging is live; that rollout
	// must add both service validation and a DB invariant requiring the second tag.
	if !blankPtr(in.AnimalIdentifier1) && !blankPtr(in.AnimalIdentifier2) && normalizeAnimalIdentifier(*in.AnimalIdentifier1) == normalizeAnimalIdentifier(*in.AnimalIdentifier2) {
		return domain.LoadGoat{}, BadRequest("duplicate_animal_identifiers", "animal identifier 1 and animal identifier 2 must be different")
	}
	in.Species = strings.TrimSpace(in.Species)
	if in.Species == "" {
		return domain.LoadGoat{}, BadRequest("missing_species", "species is required and must be goat or sheep")
	}
	if !oneOf(in.Species, "goat", "sheep") {
		return domain.LoadGoat{}, BadRequest("invalid_species", "species must be goat or sheep")
	}
	in.Sex = strings.TrimSpace(in.Sex)
	if in.Sex == "" {
		return domain.LoadGoat{}, BadRequest("missing_sex", "sex is required and must be female or male")
	}
	if !oneOf(in.Sex, "female", "male") {
		return domain.LoadGoat{}, BadRequest("invalid_sex", "sex must be female or male")
	}
	if err := validateOptionalUUID("holding_location_id", in.HoldingLocationID); err != nil {
		return domain.LoadGoat{}, err
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.LoadGoat{}, err
	}
	if in.SelectionState == "" {
		in.SelectionState = "candidate"
	}
	if in.Purpose == "" {
		in.Purpose = domain.PurposeUnspecified
	}
	if !validPurpose(in.Purpose) {
		return domain.LoadGoat{}, BadRequest("invalid_purpose", "purpose must be breeding, fattening, non_breeding, or unspecified")
	}
	if in.CurrentState == "" {
		in.CurrentState = domain.GoatStateSourceCandidate
		if in.WarmupStartedAt != nil {
			in.CurrentState = domain.GoatStateSourceWarmup
		}
	}
	if in.SourceEntryState == "" {
		in.SourceEntryState = "pending"
	}
	if !oneOf(in.SourceEntryState, "pending", "accepted", "blocked") {
		return domain.LoadGoat{}, BadRequest("invalid_source_entry_state", "source_entry_state must be pending, accepted, or blocked")
	}
	if in.OwnershipState == "" {
		in.OwnershipState = "pending"
	}
	if in.HealthState == "" {
		in.HealthState = "pending"
	}
	if in.WarmupDays == nil && in.WarmupStartedAt != nil && in.WarmupEndedAt != nil {
		days := int(in.WarmupEndedAt.Sub(*in.WarmupStartedAt).Hours() / 24)
		if days < 0 {
			return domain.LoadGoat{}, BadRequest("invalid_warmup_window", "warmup_ended_at must be after warmup_started_at")
		}
		in.WarmupDays = &days
	}
	in.ProofRefs = jsonArray(in.ProofRefs)
	in.Metadata = jsonObject(in.Metadata)
	goat, err := s.repo.AddGoatToLoad(ctx, in)
	if errors.Is(err, ports.ErrSexMismatch) {
		return domain.LoadGoat{}, BadRequest("sex_mismatch", "sex must match the existing goat sex")
	}
	if errors.Is(err, ports.ErrInvalidReference) {
		return domain.LoadGoat{}, BadRequest("invalid_goat_reference", "goat_id must reference an existing goat for this tenant")
	}
	if errors.Is(err, ports.ErrInvalidTransition) {
		return domain.LoadGoat{}, Conflict("animal_identifier_conflict", "animal identifier already belongs to another animal")
	}
	if errors.Is(err, ports.ErrWriteConflict) {
		return domain.LoadGoat{}, Conflict("write_conflict", "animal identifier was claimed by another write; reload before retrying")
	}
	return goat, err
}

func (s *Service) RecordHFVaccinationEvidence(ctx context.Context, in ports.HFVaccinationEvidence) (domain.HFVaccinationEvidence, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if err := requireUUID("load_id", in.LoadID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if err := requireUUID("goat_id", in.GoatID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if err := requireUUID("protocol_version_id", in.ProtocolVersionID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if err := requireUUID("rule_id", in.RuleID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if strings.TrimSpace(in.DoseCode) == "" {
		return domain.HFVaccinationEvidence{}, BadRequest("missing_dose_code", "dose_code is required")
	}
	if in.AdministeredAt.IsZero() {
		return domain.HFVaccinationEvidence{}, BadRequest("missing_administered_at", "administered_at is required")
	}
	if err := validateOptionalUUID("proof_ref_id", in.ProofRefID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	in.DoseCode = strings.TrimSpace(in.DoseCode)
	in.VaccineName = strings.TrimSpace(in.VaccineName)
	in.LotNumber = strings.TrimSpace(in.LotNumber)
	in.SourceRef = strings.TrimSpace(in.SourceRef)
	in.Metadata = jsonObject(in.Metadata)
	// App-level validation: the goat must already be on the load before evidence is recorded, so a bad
	// reference returns a precise 400 rather than relying on the DB FK / WHERE-EXISTS to fail late.
	onLoad, err := s.repo.GoatOnLoad(ctx, in.TenantID, in.LoadID, in.GoatID)
	if err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if !onLoad {
		return domain.HFVaccinationEvidence{}, BadRequest("goat_not_on_load", "goat_id must reference a goat on the procurement load")
	}
	evidence, err := s.repo.RecordHFVaccinationEvidence(ctx, in)
	if errors.Is(err, ports.ErrInvalidReference) {
		return domain.HFVaccinationEvidence{}, BadRequest("invalid_hf_vaccination_reference", "protocol_version_id, rule_id, goat_id, or proof_ref_id does not exist for this tenant")
	}
	if errors.Is(err, ports.ErrInvalidTransition) {
		return domain.HFVaccinationEvidence{}, BadRequest("invalid_hf_vaccination_evidence", "HF vaccination evidence must reference a goat on the procurement load")
	}
	return evidence, err
}

func (s *Service) ReviewHFVaccinationEvidence(ctx context.Context, in ports.ReviewHFVaccinationEvidence) (domain.HFVaccinationEvidence, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if err := requireUUID("evidence_id", in.EvidenceID); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if in.ExpectedRowVersion <= 0 {
		return domain.HFVaccinationEvidence{}, BadRequest("missing_expected_row_version", "expected_row_version is required")
	}
	if !oneOf(in.ReviewStatus,
		domain.HFVaccinationReviewTrusted,
		domain.HFVaccinationReviewRejected,
		domain.HFVaccinationReviewConflicting,
		domain.HFVaccinationReviewDuplicate,
	) {
		return domain.HFVaccinationEvidence{}, BadRequest("invalid_review_status", "review_status must be trusted, rejected, conflicting, or duplicate")
	}
	if err := validateOptionalUUID("reviewed_by", in.ReviewedBy); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.HFVaccinationEvidence{}, err
	}
	if !in.ReviewedAtSet {
		in.ReviewedAt = s.now().UTC()
	}
	in.ReviewReason = strings.TrimSpace(in.ReviewReason)
	evidence, err := s.repo.ReviewHFVaccinationEvidence(ctx, in)
	if errors.Is(err, ports.ErrNotFound) {
		return domain.HFVaccinationEvidence{}, NotFound("HF vaccination evidence was not found")
	}
	if errors.Is(err, ports.ErrStaleWrite) {
		return domain.HFVaccinationEvidence{}, Conflict("stale_hf_vaccination_evidence_review", "HF vaccination evidence changed; reload before reviewing")
	}
	if errors.Is(err, ports.ErrProofRequired) {
		return domain.HFVaccinationEvidence{}, BadRequest("missing_proof_ref", "trusted procurement holding vaccination evidence requires a completed proof_ref_id before it can suppress a dose")
	}
	if errors.Is(err, ports.ErrInvalidTrustContext) {
		return domain.HFVaccinationEvidence{}, BadRequest("invalid_trust_context", "trusted procurement holding vaccination evidence must belong to an animal held in our procurement holding park for 28-35 days, with the dose administered inside that holding stay")
	}
	if errors.Is(err, ports.ErrInvalidTransition) {
		return domain.HFVaccinationEvidence{}, Conflict("invalid_hf_vaccination_evidence_review_transition", "trusted HF vaccination evidence cannot be changed by this review endpoint")
	}
	return evidence, err
}

func (s *Service) RecordSourceHealth(ctx context.Context, in ports.SourceHealth) (domain.SourceHealthCheck, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	if err := requireUUID("goat_id", in.GoatID); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	if err := requireUUID("load_id", in.LoadID); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	if !oneOf(in.HealthState, domain.HealthPassed, domain.HealthFailed, domain.HealthDeferred) {
		return domain.SourceHealthCheck{}, BadRequest("invalid_health_state", "health_state must be passed, failed, or deferred")
	}
	if err := validateOptionalUUID("proof_ref_id", in.ProofRefID); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	if err := validateOptionalUUID("sop_task_id", in.SOPTaskID); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.SourceHealthCheck{}, err
	}
	if !in.CheckedAtSet {
		in.CheckedAt = s.now().UTC()
	}
	check, err := s.repo.RecordSourceHealth(ctx, in)
	if err != nil {
		return domain.SourceHealthCheck{}, err
	}
	// Skip the cancel hook on an idempotent replay — the repo ran no procurement mutation, and re-firing it
	// could cancel vaccination obligations opened after the original write.
	if !check.Replayed && in.HealthState != domain.HealthPassed {
		_ = s.cancelOpenVaccination(ctx, in.TenantID, in.GoatID, "procurement_source_health_"+in.HealthState)
	}
	return check, nil
}

func (s *Service) PreDispatchDecision(ctx context.Context, in ports.Decision) (domain.Decision, error) {
	in.DecisionStage = "pre_dispatch"
	decision, err := s.recordDecision(ctx, in)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidTransition) {
			return domain.Decision{}, BadRequest("invalid_pre_dispatch_transition", "pre-dispatch acceptance requires accepted source entry, passed health, and resolved ownership")
		}
		return domain.Decision{}, err
	}
	if !decision.Replayed && in.DecisionType != domain.DecisionAccepted {
		_ = s.cancelOpenVaccination(ctx, in.TenantID, in.GoatID, "procurement_pre_dispatch_"+in.DecisionType)
	}
	return decision, nil
}

func (s *Service) DispatchLoad(ctx context.Context, in ports.DispatchLoad) (domain.TransitHandoff, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.TransitHandoff{}, err
	}
	if err := requireUUID("load_id", in.LoadID); err != nil {
		return domain.TransitHandoff{}, err
	}
	if err := requireUUID("to_location_id", in.ToLocationID); err != nil {
		return domain.TransitHandoff{}, err
	}
	if err := validateOptionalUUID("from_location_id", in.FromLocationID); err != nil {
		return domain.TransitHandoff{}, err
	}
	if err := validateOptionalUUID("proof_ref_id", in.ProofRefID); err != nil {
		return domain.TransitHandoff{}, err
	}
	for _, goatID := range in.GoatIDs {
		if err := requireUUID("goat_id", goatID); err != nil {
			return domain.TransitHandoff{}, err
		}
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.TransitHandoff{}, err
	}
	if !in.DispatchedAtSet {
		in.DispatchedAt = s.now().UTC()
	}
	handoff, err := s.repo.DispatchLoad(ctx, in)
	if errors.Is(err, ports.ErrInvalidTransition) {
		return domain.TransitHandoff{}, BadRequest("invalid_dispatch_transition", "dispatch requires pre-dispatch accepted animals with accepted source entry, passed health, and resolved ownership")
	}
	return handoff, err
}

func (s *Service) RecordArrivalReview(ctx context.Context, in ports.ArrivalReview) (domain.ArrivalReview, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.ArrivalReview{}, err
	}
	if err := requireUUID("load_id", in.LoadID); err != nil {
		return domain.ArrivalReview{}, err
	}
	if err := requireUUID("park_location_id", in.ParkLocationID); err != nil {
		return domain.ArrivalReview{}, err
	}
	if err := validateOptionalUUID("media_proof_id", in.MediaProofID); err != nil {
		return domain.ArrivalReview{}, err
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.ArrivalReview{}, err
	}
	if !in.ReviewedAtSet {
		in.ReviewedAt = s.now().UTC()
	}
	if in.Status == "" {
		in.Status = arrivalStatus(in)
	}
	if !oneOf(in.Status, "pending", "mismatch", domain.DecisionAccepted, domain.DecisionRejected, domain.DecisionDeferred, domain.DecisionBlocked) {
		return domain.ArrivalReview{}, BadRequest("invalid_arrival_status", "status must be pending, mismatch, accepted, rejected, deferred, or blocked")
	}
	in.HealthFlags = jsonArray(in.HealthFlags)
	in.WeightFlags = jsonArray(in.WeightFlags)
	for i := range in.Goats {
		if err := validateOptionalUUID("goats[].goat_id", in.Goats[i].GoatID); err != nil {
			return domain.ArrivalReview{}, err
		}
		if err := validateOptionalUUID("goats[].proof_ref_id", in.Goats[i].ProofRefID); err != nil {
			return domain.ArrivalReview{}, err
		}
		if !oneOf(in.Goats[i].ArrivalState, "matched", "missing", "extra_unresolved", "health_flag", "weight_flag", "accepted", "rejected", "deferred", "blocked") {
			return domain.ArrivalReview{}, BadRequest("invalid_arrival_state", "goats[].arrival_state must be matched, missing, extra_unresolved, health_flag, weight_flag, accepted, rejected, deferred, or blocked")
		}
		if oneOf(in.Goats[i].ArrivalState, "matched", "accepted") && in.Goats[i].GoatID == nil {
			return domain.ArrivalReview{}, BadRequest("missing_arrival_goat_id", "accepted or matched arrival rows require goat_id")
		}
	}
	// Reject duplicate rows for the same animal in a single review. The batch upsert
	// conflicts on (tenant_id, review_id, item_key); a repeat would fail with
	// "cannot affect row a second time", and the batched state mapping would apply
	// the first row's state to every repeat. Dedup by the same item_key the adapter uses.
	seenItems := make(map[string]struct{}, len(in.Goats))
	for i := range in.Goats {
		key := ports.ArrivalItemKey(in.Goats[i])
		if _, dup := seenItems[key]; dup {
			return domain.ArrivalReview{}, BadRequest("duplicate_arrival_item", "goats[] contains duplicate rows for the same animal; each animal may appear at most once per arrival review")
		}
		seenItems[key] = struct{}{}
	}
	review, err := s.repo.RecordArrivalReview(ctx, in)
	if err != nil {
		if errors.Is(err, ports.ErrInvalidTransition) {
			return domain.ArrivalReview{}, BadRequest("invalid_arrival_transition", "arrival acceptance requires a loaded animal with accepted source entry, passed health, and resolved ownership")
		}
		return domain.ArrivalReview{}, err
	}
	// Skip the cancel hooks on an idempotent replay — no procurement mutation ran, and re-firing them could
	// cancel vaccination obligations opened after the original review.
	if !review.Replayed {
		for _, item := range in.Goats {
			if item.GoatID == nil {
				continue
			}
			if oneOf(item.ArrivalState, "rejected", "blocked", "extra_unresolved") || in.Status == domain.DecisionRejected {
				_ = s.cancelOpenVaccination(ctx, in.TenantID, *item.GoatID, "procurement_arrival_"+item.ArrivalState)
			}
		}
		_ = s.cancelIneligibleFromDetail(ctx, in.TenantID, in.LoadID)
	}
	return review, nil
}

func (s *Service) AcceptIntake(ctx context.Context, in ports.AcceptIntake) ([]domain.PCHandoff, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return nil, err
	}
	if err := requireUUID("load_id", in.LoadID); err != nil {
		return nil, err
	}
	if err := requireUUID("park_location_id", in.ParkLocationID); err != nil {
		return nil, err
	}
	if err := requireUUID("shed_location_id", in.ShedLocationID); err != nil {
		return nil, err
	}
	seenGoats := make(map[string]struct{}, len(in.GoatIDs))
	for _, goatID := range in.GoatIDs {
		if err := requireUUID("goat_id", goatID); err != nil {
			return nil, err
		}
		if _, dup := seenGoats[goatID]; dup {
			// The pc_handoffs upsert conflicts on (tenant_id, load_id, goat_id) and
			// goat_location_history/goats are batched by goat id; a duplicate would fail
			// with "cannot affect row a second time".
			return nil, BadRequest("duplicate_goat_id", "goat_ids contains duplicate entries; each animal may appear at most once")
		}
		seenGoats[goatID] = struct{}{}
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return nil, err
	}
	if !in.AcceptedAtSet {
		in.AcceptedAt = s.now().UTC()
	}
	if in.EntryDate.IsZero() {
		in.EntryDate = in.AcceptedAt
	}
	in.TrustedVaccinationHistory = jsonArray(in.TrustedVaccinationHistory)
	handoffs, err := s.repo.AcceptIntake(ctx, in)
	if errors.Is(err, ports.ErrInvalidTransition) {
		return nil, BadRequest("invalid_intake_transition", "accepted intake requires arrival-accepted animals with truck proof, accepted source entry, passed health, and resolved ownership")
	}
	return handoffs, err
}

func (s *Service) recordDecision(ctx context.Context, in ports.Decision) (domain.Decision, error) {
	if err := validateTenant(in.TenantID); err != nil {
		return domain.Decision{}, err
	}
	if err := requireUUID("goat_id", in.GoatID); err != nil {
		return domain.Decision{}, err
	}
	if err := requireUUID("load_id", in.LoadID); err != nil {
		return domain.Decision{}, err
	}
	if !oneOf(in.DecisionType, domain.DecisionAccepted, domain.DecisionRejected, domain.DecisionDeferred, domain.DecisionBlocked) {
		return domain.Decision{}, BadRequest("invalid_decision_type", "decision_type must be accepted, rejected, deferred, or blocked")
	}
	if err := validateOptionalUUID("proof_ref_id", in.ProofRefID); err != nil {
		return domain.Decision{}, err
	}
	if err := validateOptionalUUID("sop_task_id", in.SOPTaskID); err != nil {
		return domain.Decision{}, err
	}
	if err := validateOptionalUUID("owner_id", in.OwnerID); err != nil {
		return domain.Decision{}, err
	}
	if err := requireIdempotency(in.IdempotencyKey); err != nil {
		return domain.Decision{}, err
	}
	if !in.DecidedAtSet {
		in.DecidedAt = s.now().UTC()
	}
	in.Metadata = jsonObject(in.Metadata)
	return s.repo.RecordDecision(ctx, in)
}

func (s *Service) cancelOpenVaccination(ctx context.Context, tenantID, goatID, reason string) error {
	if s.canceler == nil {
		return nil
	}
	_, err := s.canceler.CancelOpenForGoat(ctx, tenantID, goatID, reason)
	return err
}

func (s *Service) cancelIneligibleFromDetail(ctx context.Context, tenantID, loadID string) error {
	if s.canceler == nil {
		return nil
	}
	detail, err := s.repo.GetLoadDetail(ctx, tenantID, loadID)
	if err != nil {
		return err
	}
	for _, goat := range detail.Goats {
		if procurementStateBlocksVaccination(goat) {
			_ = s.cancelOpenVaccination(ctx, tenantID, goat.GoatID, "procurement_"+goat.CurrentState)
		}
	}
	return nil
}

func procurementStateBlocksVaccination(goat domain.LoadGoat) bool {
	if goat.CurrentState == domain.GoatStateAcceptedHerdIntake {
		return false
	}
	return true
}

func validPurpose(purpose string) bool {
	return oneOf(purpose, domain.PurposeBreeding, domain.PurposeFattening, domain.PurposeNonBreeding, domain.PurposeUnspecified)
}

func arrivalStatus(in ports.ArrivalReview) string {
	switch {
	case in.ExtraCount > 0 || in.MissingCount > 0:
		return "mismatch"
	case in.RejectedCount > 0:
		return domain.DecisionRejected
	case len(in.HealthFlags) > 2 || len(in.WeightFlags) > 2:
		return domain.DecisionDeferred
	default:
		return domain.DecisionAccepted
	}
}

func validateTenant(tenantID string) error {
	return requireUUID("tenant_id", tenantID)
}

func requireUUID(field, value string) error {
	if strings.TrimSpace(value) == "" || !uuidutil.IsUUIDString(value) {
		return BadRequest("invalid_"+sanitizeField(field), field+" must be a UUID")
	}
	return nil
}

func validateOptionalUUID(field string, value *string) error {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return requireUUID(field, *value)
}

func requireIdempotency(value string) error {
	if strings.TrimSpace(value) == "" {
		return BadRequest("missing_idempotency_key", "Idempotency-Key header is required for procurement writes")
	}
	return nil
}

func sanitizeField(field string) string {
	return strings.NewReplacer("[", "", "]", "", ".", "_").Replace(field)
}

func blankPtr(value *string) bool {
	return value == nil || strings.TrimSpace(*value) == ""
}

func normalizeAnimalIdentifier(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
}

func jsonObject(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage(`{}`)
	}
	if _, ok := v.(map[string]any); !ok {
		return json.RawMessage(`{}`)
	}
	return raw
}

func jsonArray(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return json.RawMessage(`[]`)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return json.RawMessage(`[]`)
	}
	if _, ok := v.([]any); !ok {
		return json.RawMessage(`[]`)
	}
	return raw
}
