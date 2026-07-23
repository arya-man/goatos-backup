// Package app coordinates vaccination-execution read use-cases.
package app

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

type Service struct {
	repo      ports.Repository
	ownership ports.ShedOwnershipReader
}

type plannedDriveSessionsReader interface {
	PlannedDriveSessionsForShed(ctx context.Context, tenantID, shedID string) ([]domain.PlannedSession, error)
}

// NewService builds the vaccination-execution read service. An optional ShedOwnershipReader attaches
// each shed's Manager/Backup (from the workforce roster, cross-module). When omitted/nil, every shed
// reads as a manager/backup seed gap (NoopShedOwnership) — the honest default, never a fabricated owner.
func NewService(repo ports.Repository, ownership ...ports.ShedOwnershipReader) *Service {
	var own ports.ShedOwnershipReader = ports.NoopShedOwnership{}
	if len(ownership) > 0 && ownership[0] != nil {
		own = ownership[0]
	}
	return &Service{repo: repo, ownership: own}
}

func (s *Service) VaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionRow, error) {
	page, err := s.VaccinationExecutionPage(ctx, q)
	if err != nil {
		return nil, err
	}
	return page.Rows, nil
}

// VaccinationExecutionPage returns one server-filtered keyset page plus the authoritative filtered
// total. The repository fetches limit+1 rows in the same query, so pagination never adds a count call.
func (s *Service) VaccinationExecutionPage(ctx context.Context, q domain.ExecutionQuery) (domain.ExecutionResponse, error) {
	page, err := s.repo.ListVaccinationExecutionPage(ctx, q)
	if err != nil {
		return domain.ExecutionResponse{}, err
	}
	rows := make([]domain.ExecutionRow, 0, len(page.Rows))
	for _, p := range page.Rows {
		row := rowFromProjection(p, q)
		rows = append(rows, row)
	}
	var next *string
	if page.NextCursor != nil {
		encoded, err := domain.EncodeExecutionCursor(*page.NextCursor)
		if err != nil {
			return domain.ExecutionResponse{}, err
		}
		next = &encoded
	}
	return domain.ExecutionResponse{Source: domain.SourceAPI, Rows: rows, TotalCount: page.TotalCount, NextCursor: next, Freshness: page.Freshness}, nil
}

func (s *Service) ShedDrilldown(ctx context.Context, q domain.ExecutionQuery) (domain.ShedDrilldown, bool, error) {
	rows, err := s.VaccinationExecution(ctx, q)
	if err != nil {
		return domain.ShedDrilldown{}, false, err
	}
	if len(rows) == 0 {
		return domain.ShedDrilldown{}, false, nil
	}
	head := rows[0]
	stagesSeen := map[string]bool{}
	drives := make([]domain.DriveSummary, 0, len(rows))
	summary := domain.ShedDrilldownSummary{}
	for _, row := range rows {
		stagesSeen[row.AnimalStage] = true
		drives = append(drives, domain.DriveSummary{DriveID: row.DriveID, DriveName: row.DriveName, WorkState: row.WorkState, Severity: row.Severity})
		summary.Total++
		switch row.WorkState {
		case domain.WorkStateDue:
			summary.Due++
		case domain.WorkStateOverdue:
			summary.Overdue++
		case domain.WorkStateProofPending:
			summary.ProofPending++
		case domain.WorkStateVerificationPending:
			summary.VerificationPending++
		case domain.WorkStateRejected:
			summary.Rejected++
		case domain.WorkStateDeferred:
			summary.Deferred++
		case domain.WorkStateMissed:
			summary.Missed++
		case domain.WorkStateBlocked:
			summary.Blocked++
		case domain.WorkStateCompleted:
			summary.Completed++
		}
	}
	stages := make([]string, 0, len(stagesSeen))
	for stage := range stagesSeen {
		stages = append(stages, stage)
	}
	sort.Strings(stages)
	return domain.ShedDrilldown{
		ParkID:       head.ParkID,
		ParkName:     head.ParkName,
		ShedID:       head.ShedID,
		ShedName:     head.ShedName,
		AnimalStages: stages,
		Drives:       drives,
		Rows:         rows,
		Summary:      summary,
	}, true, nil
}

// VaccinationOperations rolls the flat cohort × protocol rows into the matrix + per-cohort detail shape:
// a deduped protocol list, and one cohort per (park · shed · stage) carrying its cells, headcount, age
// band, latest accepted last_dose, earliest open next_due, and worst computed status.
func (s *Service) VaccinationOperations(ctx context.Context, q domain.OperationsQuery) (domain.OperationsResponse, error) {
	rows, err := s.repo.VaccinationOperations(ctx, q)
	if err != nil {
		return domain.OperationsResponse{}, err
	}
	return operationsResponseFromRows(rows, q.Limit)
}

func (s *Service) VaccinationSchedule(ctx context.Context, q domain.ScheduleQuery) (domain.OperationsResponse, error) {
	rows, err := s.repo.VaccinationSchedule(ctx, q)
	if err != nil {
		return domain.OperationsResponse{}, err
	}
	return operationsResponseFromRows(rows, q.Limit)
}

func (s *Service) DriveAssignments(ctx context.Context, q domain.DriveAssignmentQuery) (domain.DriveAssignmentResponse, error) {
	rows, err := s.repo.DriveAssignments(ctx, q)
	if err != nil {
		return domain.DriveAssignmentResponse{}, err
	}
	return domain.DriveAssignmentResponse{Source: domain.SourceAPI, Rows: rows}, nil
}

// operationsResponseFromRows rolls the flat cohort × protocol rows into the matrix + per-cohort
// detail shape shared by /vaccination/operations and /vaccination/schedule.
func operationsResponseFromRows(rows []domain.OperationsRow, limit int) (domain.OperationsResponse, error) {
	protocolSeen := map[string]bool{}
	protocols := []domain.OperationsProtocol{}
	cohortIndex := map[string]int{}
	cohorts := []domain.OperationsCohort{}
	for _, r := range rows {
		if !protocolSeen[r.ProtocolID] {
			protocolSeen[r.ProtocolID] = true
			protocols = append(protocols, domain.OperationsProtocol{ProtocolID: r.ProtocolID, Name: r.ProtocolName})
		}
		key := r.ParkID + "|" + r.ShedID + "|" + r.Stage
		idx, ok := cohortIndex[key]
		if !ok {
			idx = len(cohorts)
			cohortIndex[key] = idx
			cohorts = append(cohorts, domain.OperationsCohort{
				ParkID: r.ParkID, ParkName: r.ParkName, ShedID: r.ShedID, ShedName: r.ShedName,
				Stage: r.Stage, AgeBand: r.AgeBand, WorkState: domain.WorkStateCompleted, Cells: []domain.OperationsCell{},
			})
		}
		c := &cohorts[idx]
		cellState := cellWorkState(r)
		cellCounts := countsFromRow(r)
		c.Cells = append(c.Cells, domain.OperationsCell{
			ProtocolID:   r.ProtocolID,
			WorkState:    cellState,
			LastDose:     r.LastDose,
			NextDue:      r.NextDue,
			VaccineNames: append([]string(nil), r.VaccineNames...),
			Counts:       cellCounts,
		})
		// Cohort rollup: headcount = max across protocols (same goats), worst status, latest last_dose, earliest next_due.
		// Counts roll up by summing across protocol cells (obligations differ per vaccine, so a sum is the
		// cohort's total work across all its vaccines).
		c.Counts = addCounts(c.Counts, cellCounts)
		if r.Animals > c.Animals {
			c.Animals = r.Animals
		}
		if c.AgeBand == nil && r.AgeBand != nil {
			c.AgeBand = r.AgeBand
		}
		if r.LastDose != nil && (c.LastDose == nil || r.LastDose.After(*c.LastDose)) {
			c.LastDose = r.LastDose
		}
		if r.NextDue != nil && (c.NextDue == nil || r.NextDue.Before(*c.NextDue)) {
			c.NextDue = r.NextDue
		}
		if operationsRank(cellState) < operationsRank(c.WorkState) {
			c.WorkState = cellState
		}
	}
	var nextCursor *string
	if limit > 0 && len(cohorts) > limit {
		last := cohorts[limit-1]
		encoded, err := domain.EncodeOperationsCursor(domain.OperationsCursor{
			ParkID:   last.ParkID,
			ParkName: last.ParkName,
			ShedID:   last.ShedID,
			ShedName: last.ShedName,
			Stage:    last.Stage,
		})
		if err != nil {
			return domain.OperationsResponse{}, err
		}
		nextCursor = &encoded
		cohorts = cohorts[:limit]
		visibleProtocols := make(map[string]bool)
		for _, cohort := range cohorts {
			for _, cell := range cohort.Cells {
				visibleProtocols[cell.ProtocolID] = true
			}
		}
		filtered := protocols[:0]
		for _, protocol := range protocols {
			if visibleProtocols[protocol.ProtocolID] {
				filtered = append(filtered, protocol)
			}
		}
		protocols = filtered
	}
	sort.SliceStable(protocols, func(i, j int) bool { return protocols[i].Name < protocols[j].Name })
	var freshness *domain.ProjectionFreshness
	if len(rows) > 0 {
		freshness = rows[0].Freshness
	}
	return domain.OperationsResponse{Source: domain.SourceAPI, Protocols: protocols, Cohorts: cohorts, NextCursor: nextCursor, Freshness: freshness}, nil
}

// countsFromRow projects the SQL obligation/completion tallies onto the API counts shape.
func countsFromRow(r domain.OperationsRow) domain.OperationsCounts {
	return domain.OperationsCounts{
		Overdue:      r.OverdueCount,
		Due:          r.DueCount,
		InProgress:   r.InProgressCount,
		Scheduled:    r.ScheduledCount,
		Missed:       r.MissedCount,
		Deferred:     r.DeferredCount,
		Accepted:     r.AcceptedCount,
		ProofPending: r.ProofPendingCount,
		Rejected:     r.RejectedCount,
		Total:        r.TotalCount,
	}
}

// addCounts sums two count buckets for the cohort rollup across protocol cells.
func addCounts(a, b domain.OperationsCounts) domain.OperationsCounts {
	return domain.OperationsCounts{
		Overdue:      a.Overdue + b.Overdue,
		Due:          a.Due + b.Due,
		InProgress:   a.InProgress + b.InProgress,
		Scheduled:    a.Scheduled + b.Scheduled,
		Missed:       a.Missed + b.Missed,
		Deferred:     a.Deferred + b.Deferred,
		Accepted:     a.Accepted + b.Accepted,
		ProofPending: a.ProofPending + b.ProofPending,
		Rejected:     a.Rejected + b.Rejected,
		Total:        a.Total + b.Total,
	}
}

// cellWorkState derives one cohort × protocol cell's status from its obligation/completion counts.
func cellWorkState(r domain.OperationsRow) domain.WorkState {
	switch {
	case r.TotalCount > 0 && r.AcceptedCount == r.TotalCount:
		return domain.WorkStateCompleted
	case r.RejectedCount > 0:
		return domain.WorkStateRejected
	case r.ProofPendingCount > 0:
		return domain.WorkStateProofPending
	case r.MissedCount > 0:
		return domain.WorkStateMissed
	case r.OverdueCount > 0:
		return domain.WorkStateOverdue
	case r.DueCount > 0:
		return domain.WorkStateDue
	case r.InProgressCount > 0:
		return domain.WorkStateInProgress
	case r.DeferredCount > 0:
		return domain.WorkStateDeferred
	default:
		return domain.WorkStateScheduled
	}
}

// operationsRank orders work states most-urgent first (lower = worse) for the cohort worst-of rollup.
func operationsRank(w domain.WorkState) int {
	switch w {
	case domain.WorkStateOverdue:
		return 0
	case domain.WorkStateMissed:
		return 1
	case domain.WorkStateBlocked:
		return 2
	case domain.WorkStateRejected:
		return 3
	case domain.WorkStateProofPending:
		return 4
	case domain.WorkStateVerificationPending:
		return 5
	case domain.WorkStateDue:
		return 6
	case domain.WorkStateInProgress:
		return 7
	case domain.WorkStateScheduled:
		return 8
	case domain.WorkStateDeferred:
		return 9
	default: // completed
		return 10
	}
}

func rowFromProjection(p domain.ExecutionProjection, q domain.ExecutionQuery) domain.ExecutionRow {
	sopStatus := sopStatus(p.TaskState)
	proofStatus := proofStatus(p)
	verificationStatus := verificationStatus(p)
	workState := p.WorkState
	if workState == "" {
		workState = workStateFromProjection(p, q)
	}
	targetCount, openCount, doneCount := executionDisplayCounts(p)
	physicalShed := strings.TrimSpace(p.PhysicalShed)
	partition := strings.TrimSpace(p.Partition)
	if physicalShed == "" || partition == "" {
		physicalShed, partition = domain.NormalizeDriveShed(p.ShedName)
	}
	if partition == "" {
		partition = "whole"
	}
	return domain.ExecutionRow{
		ParkID:             p.ParkID,
		ParkName:           p.ParkName,
		ShedID:             p.ShedID,
		ShedName:           p.ShedName,
		PhysicalShed:       physicalShed,
		Partition:          partition,
		AnimalStage:        p.AnimalStage,
		TargetCount:        targetCount,
		OpenCount:          openCount,
		DoneCount:          doneCount,
		DriveID:            p.BatchID,
		DriveName:          driveName(p),
		DueDate:            dueDate(p),
		WorkState:          workState,
		Severity:           severity(workState),
		Owner:              owner(p),
		BlockerReason:      blockerReason(p, workState),
		SOPStatus:          sopStatus,
		ProofStatus:        proofStatus,
		VerificationStatus: verificationStatus,
		NextAction:         nextAction(p, workState),
		ObligationID:       p.ObligationID,
		BatchID:            p.BatchID,
		SOPTaskID:          p.SOPTaskID,
		SOPVersionID:       p.SOPVersionID,
		SOPTaskRowVersion:  p.SOPTaskRowVersion,
		CompletionID:       p.CompletionID,
	}
}

// executionDisplayCounts is the backend-owned count contract for mobile shed cards. One execution
// row is an aggregated obligation group, not one goat, so clients must never infer counts from the
// number of rows. Recorded/accepted/rejected completion evidence all means field execution occurred;
// deferred/missed/cancelled targets are not presented as open work.
func executionDisplayCounts(p domain.ExecutionProjection) (target, open, done int) {
	target = p.ObligationCount
	done = p.CompletedCount
	completionEvidence := p.CompletionRecorded + p.CompletionAccepted + p.CompletionRejected
	if completionEvidence > done {
		done = completionEvidence
	}
	if done > target {
		done = target
	}
	open = target - done - p.DeferredCount - p.MissedCount - p.CanceledCount
	if open < 0 {
		open = 0
	}
	return target, open, done
}

func workStateFromProjection(p domain.ExecutionProjection, q domain.ExecutionQuery) domain.WorkState {
	if p.ObligationCount > 0 && p.CompletedCount == p.ObligationCount && p.CompletionRejected == 0 && p.CompletionRecorded == 0 {
		return domain.WorkStateCompleted
	}
	if p.CompletionRejected > 0 {
		return domain.WorkStateRejected
	}
	if !p.UsableForVaccination {
		return domain.WorkStateBlocked
	}
	if p.DeferredCount > 0 || p.HealthDeferredCount > 0 || p.IsQuarantine || p.IsICU {
		return domain.WorkStateDeferred
	}
	if p.MissedCount > 0 {
		return domain.WorkStateMissed
	}
	if p.OperatorName == nil && p.CompletedCount < p.ObligationCount {
		return domain.WorkStateBlocked
	}
	if p.CompletionRecorded > 0 || taskStateIs(p, "submitted", "needs_review") {
		return domain.WorkStateVerificationPending
	}
	if p.InProgressCount > 0 || batchStatusIs(p, "in_progress") || taskStateIs(p, "in_progress") {
		return domain.WorkStateInProgress
	}
	if taskStateIs(p, "rework_requested", "rejected") {
		return domain.WorkStateProofPending
	}
	if p.DueAt != nil && !q.AsOf.IsZero() && p.DueAt.Before(q.AsOf) {
		return domain.WorkStateOverdue
	}
	if p.DueCount > 0 {
		return domain.WorkStateDue
	}
	return domain.WorkStateScheduled
}

func taskStateIs(p domain.ExecutionProjection, states ...string) bool {
	if p.TaskState == nil {
		return false
	}
	for _, state := range states {
		if *p.TaskState == state {
			return true
		}
	}
	return false
}

func batchStatusIs(p domain.ExecutionProjection, states ...string) bool {
	if p.BatchStatus == nil {
		return false
	}
	for _, state := range states {
		if *p.BatchStatus == state {
			return true
		}
	}
	return false
}

func severity(workState domain.WorkState) domain.Severity {
	switch workState {
	case domain.WorkStateCompleted:
		return domain.SeverityOK
	case domain.WorkStateScheduled, domain.WorkStateDue, domain.WorkStateInProgress, domain.WorkStateDeferred, domain.WorkStateVerificationPending:
		return domain.SeverityWatch
	case domain.WorkStateProofPending, domain.WorkStateOverdue, domain.WorkStateMissed:
		return domain.SeverityAtRisk
	default:
		return domain.SeverityBroken
	}
}

func sopStatus(state *string) domain.SOPStatus {
	if state == nil {
		return domain.SOPStatusNotStarted
	}
	switch *state {
	case "in_progress":
		return domain.SOPStatusInProgress
	case "submitted", "needs_review":
		return domain.SOPStatusSubmitted
	case "accepted":
		return domain.SOPStatusAccepted
	case "rework_requested", "rejected":
		return domain.SOPStatusRework
	default:
		return domain.SOPStatusNotStarted
	}
}

func proofStatus(p domain.ExecutionProjection) domain.ProofStatus {
	switch {
	case p.CompletionRejected > 0:
		return domain.ProofStatusRejected
	case p.CompletionAccepted > 0 && p.CompletionRecorded == 0:
		return domain.ProofStatusAccepted
	case p.CompletionRecorded > 0 || taskStateIs(p, "submitted", "needs_review"):
		return domain.ProofStatusUploaded
	default:
		return domain.ProofStatusMissing
	}
}

func verificationStatus(p domain.ExecutionProjection) domain.VerificationStatus {
	switch {
	case p.CompletionRejected > 0:
		return domain.VerificationStatusRejected
	case p.CompletionRecorded > 0 || taskStateIs(p, "submitted", "needs_review"):
		return domain.VerificationStatusPending
	case p.CompletionAccepted > 0 && p.CompletedCount == p.ObligationCount:
		return domain.VerificationStatusVerified
	default:
		return domain.VerificationStatusNotReady
	}
}

func owner(p domain.ExecutionProjection) *domain.Owner {
	if p.OperatorName == nil && p.ParkHeadName == nil && p.VerifierName == nil {
		return nil
	}
	return &domain.Owner{OperatorName: p.OperatorName, ParkHeadName: p.ParkHeadName, VerifierName: p.VerifierName}
}

func driveName(p domain.ExecutionProjection) *string {
	name := domain.VaccinationDoseDisplayLabel(p.ProtocolName, p.DoseCode)
	if name == "" {
		return nil
	}
	return &name
}

func dueDate(p domain.ExecutionProjection) *string {
	if p.DueAt == nil {
		return nil
	}
	date := biztime.BusinessDate(*p.DueAt)
	return &date
}

func blockerReason(p domain.ExecutionProjection, workState domain.WorkState) *string {
	var reason string
	switch {
	case !p.UsableForVaccination:
		reason = "Shed is not marked usable for vaccination"
	case p.IsICU:
		reason = "Shed is ICU; PC defer/approval required"
	case p.IsQuarantine:
		reason = "Shed is quarantine; PC defer/approval required"
	case p.HealthDeferredCount > 0:
		reason = "Some goats are sick, under treatment, recovering, quarantined, or in ICU"
	case p.MissedCount > 0:
		reason = "Missed dose escalation required"
	case workState == domain.WorkStateBlocked && p.OperatorName == nil && p.CompletedCount < p.ObligationCount:
		reason = "Operator assignment required before execution"
	}
	if reason == "" {
		return nil
	}
	return &reason
}

func nextAction(p domain.ExecutionProjection, workState domain.WorkState) string {
	switch workState {
	case domain.WorkStateCompleted:
		return "No action - drive verified"
	case domain.WorkStateRejected:
		return "Review rejection and request rework"
	case domain.WorkStateMissed:
		return "Escalate missed dose to PC"
	case domain.WorkStateBlocked:
		return "Resolve blocker before execution"
	case domain.WorkStateDeferred:
		return "Confirm defer reason with PC"
	case domain.WorkStateVerificationPending:
		return "Verifier to accept or reject proof"
	case domain.WorkStateProofPending:
		return "Upload required SOP proof"
	case domain.WorkStateInProgress:
		return "Complete drive and submit proof"
	case domain.WorkStateOverdue:
		return "Start SOP - overdue"
	case domain.WorkStateDue:
		return "Start scheduled vaccination SOP"
	default:
		return "Monitor scheduled drive"
	}
}

// ScanRoster returns per-animal vaccination obligations for a shed with RFID tags and vaccine labels.
// Used by the mobile scan screen to match keyboard-wedge tag captures.
func (s *Service) ScanRoster(ctx context.Context, q domain.ScanRosterQuery) (domain.ScanRosterResult, error) {
	return s.repo.ScanRoster(ctx, q)
}

func (s *Service) TaskOptionValues(ctx context.Context, tenantID, taskID string) (domain.TaskOptionValuesResponse, error) {
	return s.repo.TaskOptionValues(ctx, tenantID, taskID)
}

const (
	defaultGapsLimit = 200
	maxGapsLimit     = 500
)

var gapReasonLabels = map[domain.GapReasonCode]string{
	domain.GapReasonNoDateOfBirth:   "No date of birth",
	domain.GapReasonNoBreedOnRecord: "No breed on record",
}

func gapReasonLabel(code domain.GapReasonCode) string {
	if label, ok := gapReasonLabels[code]; ok {
		return label
	}
	return string(code)
}

// VaccinationGaps returns the animals excluded from the vaccination coverage denominator due to
// missing identity data (no date of birth, no breed on record) for the mobile "Data gaps" overlay.
// Data gaps are strictly PER ANIMAL: every row is one goat (display id + physical tags + reason it's
// excluded). The rows are the bounded, goat_id-keyset paginated drill-down — never a full-herd scan,
// however many animals a park has gapped, and never a by-reason aggregate masquerading as entries.
func (s *Service) VaccinationGaps(ctx context.Context, q domain.GapsQuery) (domain.GapsResponse, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultGapsLimit
	}
	if limit > maxGapsLimit {
		limit = maxGapsLimit
	}
	q.Limit = limit

	projections, err := s.repo.VaccinationGaps(ctx, q)
	if err != nil {
		return domain.GapsResponse{}, err
	}
	rows := make([]domain.GapRow, 0, len(projections))
	for _, p := range projections {
		rows = append(rows, domain.GapRow{
			GoatID:            p.GoatID,
			DisplayID:         p.DisplayID,
			AnimalIdentifier1: p.AnimalIdentifier1,
			AnimalIdentifier2: p.AnimalIdentifier2,
			ParkID:            p.ParkID,
			ParkName:          p.ParkName,
			ShedID:            p.ShedID,
			ShedName:          p.ShedName,
			ReasonCode:        p.ReasonCode,
			ReasonLabel:       gapReasonLabel(p.ReasonCode),
		})
	}
	var nextCursor *string
	if len(rows) == limit {
		last := rows[len(rows)-1].GoatID
		nextCursor = &last
	}
	return domain.GapsResponse{
		Source:     domain.SourceAPI,
		ParkID:     q.ParkID,
		Rows:       rows,
		NextCursor: nextCursor,
	}, nil
}

// CoverageRollup returns per-vaccine (protocol) given-dose counts + coverage % for a scope, for the
// mobile "Doses given" overlay. It reuses the same indexed cohort×protocol rows VaccinationOperations
// already reads (ports.Repository.VaccinationOperations) and re-aggregates them by protocol only, so no
// new hot-table query is introduced — this is the scoped rollup docs/decisions/high-scale-dashboard-
// projections.md requires instead of a raw COUNT(*) over the full herd.
func (s *Service) CoverageRollup(ctx context.Context, q domain.OperationsQuery) (domain.CoverageResponse, error) {
	rows, err := s.repo.VaccinationOperations(ctx, q)
	if err != nil {
		return domain.CoverageResponse{}, err
	}
	order := make([]string, 0, len(rows))
	byProtocol := map[string]*domain.CoverageProtocol{}
	for _, r := range rows {
		agg, ok := byProtocol[r.ProtocolID]
		if !ok {
			agg = &domain.CoverageProtocol{ProtocolID: r.ProtocolID, Name: r.ProtocolName}
			byProtocol[r.ProtocolID] = agg
			order = append(order, r.ProtocolID)
		}
		agg.GivenCount += r.AcceptedCount
		agg.TotalCount += r.TotalCount
	}
	protocols := make([]domain.CoverageProtocol, 0, len(order))
	for _, id := range order {
		agg := byProtocol[id]
		if agg.TotalCount > 0 {
			agg.CoveragePercent = int(float64(agg.GivenCount) / float64(agg.TotalCount) * 100)
		}
		protocols = append(protocols, *agg)
	}
	sort.SliceStable(protocols, func(i, j int) bool { return protocols[i].Name < protocols[j].Name })
	return domain.CoverageResponse{Source: domain.SourceAPI, ParkID: q.ParkID, Protocols: protocols}, nil
}

// ---- Shed-wise vaccination (shed rollup + shed detail + animal roster) ----

const defaultShedSummaryLimit = 50

// ShedSummary returns the shed-wise rollup: one animal-level row per shed with the resolved Manager/
// Backup attached from the workforce roster and a derived shed Status, plus offset-pagination metadata.
func (s *Service) ShedSummary(ctx context.Context, q domain.ShedSummaryQuery) (domain.ShedSummaryResponse, error) {
	projections, err := s.repo.ShedSummary(ctx, q)
	if err != nil {
		return domain.ShedSummaryResponse{}, err
	}
	at := q.AsOf
	if at.IsZero() {
		at = time.Now().In(biztime.DefaultLocation())
	}
	total := 0
	rows := make([]domain.ShedSummaryRow, 0, len(projections))
	ownershipScopes := make([]domain.ShedOwnershipScope, 0, len(projections))
	for _, p := range projections {
		ownershipScopes = append(ownershipScopes, domain.ShedOwnershipScope{ShedID: p.ShedID, ParkID: p.ParkID})
	}
	ownersByShed, err := s.ownership.ShedOwnerships(ctx, q.TenantID, ownershipScopes, at)
	if err != nil {
		return domain.ShedSummaryResponse{}, err
	}
	for _, p := range projections {
		total = p.TotalCount // window COUNT(*) OVER() — identical on every row of the filtered set
		owners := ownersByShed[p.ShedID]
		rows = append(rows, domain.ShedSummaryRow{
			ParkID:             p.ParkID,
			ParkName:           p.ParkName,
			ShedID:             p.ShedID,
			ShedName:           p.ShedName,
			Animals:            p.Animals,
			Due:                p.DueAnimals,
			Done:               p.Animals - p.DueAnimals,
			Sessions:           p.Sessions,
			LastDone:           businessDatePtr(p.LastDone),
			NextDue:            businessDatePtr(p.NextDue),
			Manager:            owners.Manager,
			Backup:             owners.Backup,
			DriveOperatorNames: append([]string(nil), p.DriveOperatorNames...),
			Capacity:           p.Capacity,
			Status:             p.Status,
		})
	}
	limit := q.Limit
	if limit <= 0 {
		limit = defaultShedSummaryLimit
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	return domain.ShedSummaryResponse{
		Source: domain.SourceAPI,
		Rows:   rows,
		Page:   domain.PageInfo{Total: total, Limit: limit, Offset: offset},
		Freshness: func() *domain.ProjectionFreshness {
			if len(projections) > 0 {
				return projections[0].Freshness
			}
			return nil
		}(),
	}, nil
}

// ShedDetail returns one shed's header (the same animal-level counts + Manager/Backup + Sessions +
// Capacity + merged Status as the list row, so detail and list agree), the per-day Planned sessions
// (re-planned deterministically from the shed's due animal count via PlanSessions — mirrors the SQL
// sessions count), and the per-vaccine obligation breakdown. found=false when the shed has no alive
// animals / is not an active shed. The per-shed animal roster is a separate keyset endpoint
// (ShedAnimals).
func (s *Service) ShedDetail(ctx context.Context, shedID string, q domain.OperationsQuery) (domain.ShedDetailResponse, bool, error) {
	projections, err := s.repo.ShedSummary(ctx, domain.ShedSummaryQuery{
		TenantID:       q.TenantID,
		ShedID:         &shedID,
		AsOf:           q.AsOf,
		DueBefore:      q.DueBefore,
		HistoricalAsOf: q.HistoricalAsOf,
		Limit:          1,
	})
	if err != nil {
		return domain.ShedDetailResponse{}, false, err
	}
	if len(projections) == 0 {
		return domain.ShedDetailResponse{}, false, nil
	}
	p := projections[0]

	at := q.AsOf
	if at.IsZero() {
		at = time.Now().In(biztime.DefaultLocation())
	}
	manager, backup, err := s.ownership.ShedOwnership(ctx, q.TenantID, p.ShedID, p.ParkID, at)
	if err != nil {
		return domain.ShedDetailResponse{}, false, err
	}

	planned, err := s.plannedSessionsForShed(ctx, q.TenantID, shedID, p.DueAnimals, p.NextDue, at)
	if err != nil {
		return domain.ShedDetailResponse{}, false, err
	}

	opsQ := q
	opsQ.ParkID = nil
	opsQ.ShedID = &shedID
	ops, err := s.repo.VaccinationOperations(ctx, opsQ)
	if err != nil {
		return domain.ShedDetailResponse{}, false, err
	}
	return domain.ShedDetailResponse{
		Source:          domain.SourceAPI,
		ParkID:          p.ParkID,
		ParkName:        p.ParkName,
		ShedID:          p.ShedID,
		ShedName:        p.ShedName,
		Animals:         p.Animals,
		Due:             p.DueAnimals,
		Done:            p.Animals - p.DueAnimals,
		Sessions:        p.Sessions,
		Manager:         manager,
		Backup:          backup,
		Capacity:        p.Capacity,
		Status:          p.Status,
		PlannedSessions: planned,
		Vaccines:        aggregateShedVaccines(ops),
	}, true, nil
}

func (s *Service) plannedSessionsForShed(ctx context.Context, tenantID, shedID string, dueAnimals int, nextDue *time.Time, at time.Time) ([]domain.PlannedSession, error) {
	if reader, ok := s.repo.(plannedDriveSessionsReader); ok {
		planned, err := reader.PlannedDriveSessionsForShed(ctx, tenantID, shedID)
		if err != nil {
			return nil, err
		}
		if len(planned) > 0 {
			return planned, nil
		}
	}
	cfg, err := s.repo.CapacityConfig(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	start := at
	if nextDue != nil {
		start = *nextDue
	}
	_, _, planned, err := PlanSessions(dueAnimals, cfg, start)
	return planned, err
}

// ShedAnimals returns the shed's keyset-paginated alive-animal roster (Display ID + two tag identities +
// status). NextCursor is the last goat_id when a full page is returned, nil when the shed is exhausted.
func (s *Service) ShedAnimals(ctx context.Context, q domain.ShedAnimalQuery) (domain.ShedAnimalPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	q.Limit = limit
	if q.AsOf.IsZero() {
		q.AsOf = time.Now().In(biztime.DefaultLocation())
	}
	rows, err := s.repo.ShedAnimals(ctx, q)
	if err != nil {
		return domain.ShedAnimalPage{}, err
	}
	var next *string
	if len(rows) == limit {
		last := rows[len(rows)-1].GoatID
		next = &last
	}
	return domain.ShedAnimalPage{Rows: rows, NextCursor: next}, nil
}

// CapacityConfig returns the tenant's daily operator animal cap config for the admin Config screen (falls
// back to the code default when no row is authored).
func (s *Service) CapacityConfig(ctx context.Context, tenantID string) (domain.CapacityConfig, error) {
	return s.repo.CapacityConfig(ctx, tenantID)
}

// aggregateShedVaccines rolls the shed's cohort×protocol rows (across stages) up to one row per vaccine:
// summed obligation counts, worst-of work state, latest accepted last dose, earliest open next due. This
// is the ONLY surface exposing per-vaccine obligation counts (the shed row stays animal-level).
func aggregateShedVaccines(rows []domain.OperationsRow) []domain.ShedVaccineRow {
	order := make([]string, 0)
	byProto := map[string]*domain.ShedVaccineRow{}
	lastByProto := map[string]*time.Time{}
	nextByProto := map[string]*time.Time{}
	for _, r := range rows {
		v, ok := byProto[r.ProtocolID]
		if !ok {
			name := r.ProtocolName
			if len(r.VaccineNames) > 0 {
				name = strings.Join(r.VaccineNames, ", ")
			}
			v = &domain.ShedVaccineRow{ProtocolID: r.ProtocolID, Name: name, WorkState: domain.WorkStateCompleted}
			byProto[r.ProtocolID] = v
			order = append(order, r.ProtocolID)
		}
		v.Counts = addCounts(v.Counts, countsFromRow(r))
		cs := cellWorkState(r)
		if operationsRank(cs) < operationsRank(v.WorkState) {
			v.WorkState = cs
		}
		if r.LastDose != nil && (lastByProto[r.ProtocolID] == nil || r.LastDose.After(*lastByProto[r.ProtocolID])) {
			lastByProto[r.ProtocolID] = r.LastDose
		}
		if r.NextDue != nil && (nextByProto[r.ProtocolID] == nil || r.NextDue.Before(*nextByProto[r.ProtocolID])) {
			nextByProto[r.ProtocolID] = r.NextDue
		}
	}
	out := make([]domain.ShedVaccineRow, 0, len(order))
	for _, id := range order {
		v := byProto[id]
		v.LastDose = businessDatePtr(lastByProto[id])
		v.NextDue = businessDatePtr(nextByProto[id])
		out = append(out, *v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func businessDatePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	d := biztime.BusinessDate(*t)
	return &d
}

// ---- Vaccination operator shift + assignment config ----
// The admin config screen authors these rows; the drive/obligation scheduler consumes them.

// ErrOperatorAssignmentConfigNotFound is returned when no config row is authored yet for the park (never
// silently defaulted -- the caller renders "not configured", not a fabricated default operator).
var ErrOperatorAssignmentConfigNotFound = errors.New("vaccination execution: operator assignment config: not found")

// ErrOperatorAssignmentConfigConflict re-exports ports.ErrOperatorAssignmentConfigConflict so HTTP callers
// only need to import the app package. See ports.ErrOperatorAssignmentConfigConflict for the contract.
var ErrOperatorAssignmentConfigConflict = ports.ErrOperatorAssignmentConfigConflict

// OperatorAssignmentConfigView is the combined read-model for the admin config screen: the N/default
// config plus every operator's authored shift.
type OperatorAssignmentConfigView struct {
	Config domain.OperatorAssignmentConfig `json:"config"`
	Shifts []domain.OperatorShift          `json:"shifts"`
}

// GetOperatorAssignmentConfig returns the park's assignment config + shifts. Returns
// ErrOperatorAssignmentConfigNotFound when the park has no config row yet.
func (s *Service) GetOperatorAssignmentConfig(ctx context.Context, tenantID, parkID string) (OperatorAssignmentConfigView, error) {
	shifts, err := s.repo.OperatorShifts(ctx, tenantID, parkID)
	if err != nil {
		return OperatorAssignmentConfigView{}, err
	}
	cfg, found, err := s.repo.OperatorAssignmentConfig(ctx, tenantID, parkID)
	if err != nil {
		return OperatorAssignmentConfigView{}, err
	}
	if !found {
		return OperatorAssignmentConfigView{}, ErrOperatorAssignmentConfigNotFound
	}
	return OperatorAssignmentConfigView{Config: cfg, Shifts: shifts}, nil
}

// ListOperatorShifts returns every operator's authored shift row for a park (used standalone by the
// weekly-preview read path even before a default is configured).
func (s *Service) ListOperatorShifts(ctx context.Context, tenantID, parkID string) ([]domain.OperatorShift, error) {
	return s.repo.OperatorShifts(ctx, tenantID, parkID)
}

// UpdateOperatorAssignmentConfig validates then idempotently writes the park's N + default-operator
// config (validate-or-reject: never silently defaulted). Returns the same 400-shaped (code, message)
// pair as CapacityConfig.Validate on invalid input.
func (s *Service) UpdateOperatorAssignmentConfig(ctx context.Context, tenantID string, cfg domain.OperatorAssignmentConfig) (domain.OperatorAssignmentConfig, string, string, error) {
	shifts, err := s.repo.OperatorShifts(ctx, tenantID, cfg.ParkID)
	if err != nil {
		return domain.OperatorAssignmentConfig{}, "", "", err
	}
	knownDefault := false
	for _, sh := range shifts {
		if sh.OperatorID == cfg.DefaultOperatorID {
			knownDefault = true
			break
		}
	}
	if code, message, ok := cfg.Validate(knownDefault); !ok {
		return domain.OperatorAssignmentConfig{}, code, message, nil
	}
	updated, err := s.repo.UpsertOperatorAssignmentConfig(ctx, tenantID, cfg)
	return updated, "", "", err
}
