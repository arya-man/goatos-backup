// Package app coordinates vaccination-execution read use-cases.
package app

import (
	"context"
	"sort"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/ports"
)

type Service struct {
	repo ports.Repository
}

func NewService(repo ports.Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) VaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionRow, error) {
	projections, err := s.repo.ListVaccinationExecution(ctx, q)
	if err != nil {
		return nil, err
	}
	rows := make([]domain.ExecutionRow, 0, len(projections))
	for _, p := range projections {
		row := rowFromProjection(p, q)
		rows = append(rows, row)
	}
	return rows, nil
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
		case domain.WorkStateBlocked:
			summary.Blocked++
		case domain.WorkStateOwnerMissing:
			summary.OwnerMissing++
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
		c.Cells = append(c.Cells, domain.OperationsCell{ProtocolID: r.ProtocolID, WorkState: cellState, LastDose: r.LastDose, NextDue: r.NextDue, Counts: cellCounts})
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
	sort.SliceStable(protocols, func(i, j int) bool { return protocols[i].Name < protocols[j].Name })
	return domain.OperationsResponse{Source: domain.SourceAPI, Protocols: protocols, Cohorts: cohorts}, nil
}

// countsFromRow projects the SQL obligation/completion tallies onto the API counts shape.
func countsFromRow(r domain.OperationsRow) domain.OperationsCounts {
	return domain.OperationsCounts{
		Overdue:      r.OverdueCount,
		Due:          r.DueCount,
		InProgress:   r.InProgressCount,
		Scheduled:    r.ScheduledCount,
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
		return domain.WorkStateVerificationPending
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
	case domain.WorkStateRejected:
		return 1
	case domain.WorkStateVerificationPending:
		return 2
	case domain.WorkStateDue:
		return 3
	case domain.WorkStateInProgress:
		return 4
	case domain.WorkStateScheduled:
		return 5
	case domain.WorkStateDeferred:
		return 6
	default: // completed
		return 7
	}
}

func rowFromProjection(p domain.ExecutionProjection, q domain.ExecutionQuery) domain.ExecutionRow {
	sopStatus := sopStatus(p.TaskState)
	proofStatus := proofStatus(p)
	verificationStatus := verificationStatus(p)
	workState := workState(p, q)
	return domain.ExecutionRow{
		ParkID:             p.ParkID,
		ParkName:           p.ParkName,
		ShedID:             p.ShedID,
		ShedName:           p.ShedName,
		AnimalStage:        p.AnimalStage,
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
		CompletionID:       p.CompletionID,
	}
}

func workState(p domain.ExecutionProjection, q domain.ExecutionQuery) domain.WorkState {
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
		return domain.WorkStateBlocked
	}
	if p.OperatorName == nil && p.CompletedCount < p.ObligationCount {
		return domain.WorkStateOwnerMissing
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
	case domain.WorkStateProofPending, domain.WorkStateOverdue:
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
	name := p.ProtocolName
	if p.DoseCode != "" {
		if name != "" {
			name += " - "
		}
		name += p.DoseCode
	}
	if name == "" {
		return nil
	}
	return &name
}

func dueDate(p domain.ExecutionProjection) *string {
	if p.DueAt == nil {
		return nil
	}
	date := p.DueAt.UTC().Format("2006-01-02")
	return &date
}

func blockerReason(p domain.ExecutionProjection, workState domain.WorkState) *string {
	var reason string
	switch {
	case !p.UsableForVaccination:
		reason = "Shed is not marked usable for vaccination"
	case p.IsICU:
		reason = "Shed is ICU; PHC defer/approval required"
	case p.IsQuarantine:
		reason = "Shed is quarantine; PHC defer/approval required"
	case p.HealthDeferredCount > 0:
		reason = "Some goats are sick, under treatment, quarantined, or in ICU"
	case p.MissedCount > 0:
		reason = "Missed dose escalation required"
	case workState == domain.WorkStateOwnerMissing:
		reason = "Owner chain awaiting assignment"
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
	case domain.WorkStateBlocked:
		if p.MissedCount > 0 {
			return "Escalate missed dose to PHC"
		}
		return "Resolve blocker before execution"
	case domain.WorkStateDeferred:
		return "Confirm defer reason with PHC"
	case domain.WorkStateOwnerMissing:
		return "Assign operator / owner chain"
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
