package app

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

func (s *SweeperService) consolidateParkDrives(ctx context.Context, tenantID, versionID string, cfg SweepConfig, dueBefore time.Time) (domain.SweepResult, error) {
	planner := normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode)
	return s.consolidateParkDrivesWithVisitCounts(ctx, tenantID, versionID, cfg, time.Time{}, dueBefore, planner, NewSweepSession(), time.Time{}, nil)
}

func (s *SweeperService) consolidateParkDrivesWithVisitCounts(ctx context.Context, tenantID, versionID string, cfg SweepConfig, asOf, dueBefore time.Time, planner domain.DrivePlannerSettings, session *SweepSession, createdAtHWM time.Time, candidateIDs []string) (domain.SweepResult, error) {
	var res domain.SweepResult
	settings := cfg.ParkConsolidation
	if !settings.Enabled {
		return res, nil
	}
	minMergeTargets := settings.MinParkMergeTargets
	if minMergeTargets < 1 {
		minMergeTargets = domain.DefaultParkConsolidationSettings().MinParkMergeTargets
	}

	groups := make(map[string][]domain.ParkConsolidationCandidate)
	groupOrder := make([]string, 0)
	if candidateIDs != nil {
		for _, chunk := range snapshotIDChunks(candidateIDs, s.page) {
			rows, err := s.listUnbatchedShedDueForParkConsolidationBounded(ctx, tenantID, versionID, dueBefore, s.page, nil, createdAtHWM, chunk)
			if err != nil {
				return res, err
			}
			rows, err = s.applyParkDriveDateOverrides(ctx, tenantID, cfg, rows)
			if err != nil {
				return res, err
			}
			for _, row := range rows {
				key := parkConsolidationGroupKey(cfg, row)
				if _, ok := groups[key]; !ok {
					groupOrder = append(groupOrder, key)
				}
				groups[key] = append(groups[key], row)
			}
		}
	} else {
		var after *domain.ParkConsolidationCursor
		seenCursors := map[string]struct{}{}
		for {
			rows, err := s.listUnbatchedShedDueForParkConsolidationBounded(ctx, tenantID, versionID, dueBefore, s.page, after, createdAtHWM, nil)
			if err != nil {
				return res, err
			}
			if len(rows) == 0 {
				break
			}
			groupRows, err := s.applyParkDriveDateOverrides(ctx, tenantID, cfg, rows)
			if err != nil {
				return res, err
			}
			for _, row := range groupRows {
				key := parkConsolidationGroupKey(cfg, row)
				if _, ok := groups[key]; !ok {
					groupOrder = append(groupOrder, key)
				}
				groups[key] = append(groups[key], row)
			}
			if int32(len(rows)) < s.page {
				break
			}
			next := parkConsolidationCursor(rows[len(rows)-1])
			key := parkConsolidationCursorKey(next)
			if key == "" {
				return res, fmt.Errorf("obligation: park consolidation pagination did not produce an advance cursor")
			}
			if _, ok := seenCursors[key]; ok {
				return res, fmt.Errorf("obligation: park consolidation pagination did not advance after cursor %s", key)
			}
			seenCursors[key] = struct{}{}
			after = next
		}
	}

	now := biztime.BusinessDayStart(asOf)
	for _, key := range orderParkConsolidationGroups(groupOrder, groups, cfg) {
		rows := groups[key]
		if len(rows) == 0 {
			continue
		}
		parkID := rows[0].ParkID
		remaining := append([]domain.ParkConsolidationCandidate(nil), rows...)
		fullDates := make(map[string]struct{})
		for uniqueParkTargetCount(remaining) >= int(minMergeTargets) {
			next, attached, plannedDate, animalCapReached, stop, err := s.parkMergeStep(ctx, tenantID, versionID, cfg, planner, now, asOf, session, parkID, remaining, minMergeTargets, fullDates)
			if err != nil {
				return res, err
			}
			remaining = next
			if animalCapReached && plannedDate != nil {
				fullDates[plannedDate.Format("2006-01-02")] = struct{}{}
			}
			if attached > 0 {
				res.ParkBatches++
				res.ParkObligations += int(attached)
			}
			if stop {
				break
			}
		}
	}
	return res, nil
}

// parkMergeStep performs one park-consolidation merge attempt: picks the best shared drive date
// for remaining, shot-cap-selects the animals that fit (walking later feasible overflow dates
// exactly like the shed-batching path), and creates one park batch from the result. It seeds session with the
// persisted, cross-pass shot count for every candidate target before selecting (VAX-REV-01), and --
// when the repo supports it -- holds a per-visit advisory lock across the select+create sequence
// so a concurrent sweeper worker cannot commit a conflicting claim for the same visit in between.
// stop is true when the caller's merge loop should not attempt another iteration for this park
// (nothing left to merge, or a hard cap/error condition), whether or not this call itself attached
// anything.
func (s *SweeperService) parkMergeStep(ctx context.Context, tenantID, versionID string, cfg SweepConfig, planner domain.DrivePlannerSettings, now, asOf time.Time, session *SweepSession, parkID string, remaining []domain.ParkConsolidationCandidate, minMergeTargets int32, excludedDates map[string]struct{}) (newRemaining []domain.ParkConsolidationCandidate, attached int64, plannedDate *time.Time, animalCapReached bool, stop bool, err error) {
	remaining, err = s.applyParkDriveDateOverrides(ctx, tenantID, cfg, remaining)
	if err != nil {
		return remaining, 0, nil, false, true, err
	}
	// VAXCAP-005: pick the drive date by scoring EVERY feasible candidate date post-capacity
	// (persisted park/date animal slots + session claims), not by uncapped animal counts.
	plannedDate, err = s.selectBestParkDriveDateWithCapacity(ctx, tenantID, cfg, planner, now, remaining, minMergeTargets, excludedDates, session)
	if err != nil {
		return remaining, 0, nil, false, true, err
	}
	if plannedDate == nil {
		return remaining, 0, nil, false, true, nil
	}
	targetIDs := distinctParkTargetIDs(remaining)
	// R2-05(b): resolve each candidate's OWN rule to its real vaccine identity instead of the single
	// version-level wrapper (cfg.VaccineCode/planner.VaccinePriority) -- a park merge routinely mixes
	// several distinct matrix vaccines in one candidate set.
	orderedRemaining := orderParkCandidatesByVaccinePriority(remaining, cfg.getRuleVaccineIdentity)
	visitRelease, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, plannedDate, planner.MaxShotsPerAnimalPerDrive, session)
	if err != nil {
		return remaining, 0, plannedDate, false, true, err
	}
	selected, shotClaims, err := selectParkIDsWithinVisitShotCapForSession(now, orderedRemaining, obligationsFeasibleOnDateForPlanner(now, *plannedDate, remaining, planner), plannedDate, planner, cfg.getRuleVaccineIdentity, session)
	if err != nil {
		_ = visitRelease(ctx)
		return remaining, 0, plannedDate, false, true, err
	}
	capPlanner, err := s.operatorCapacityPlannerForTargets(ctx, tenantID, parkID, plannedDate, planner, session, targetIDs)
	if err != nil {
		session.releaseClaims(shotClaims)
		_ = visitRelease(ctx)
		return remaining, 0, plannedDate, false, true, err
	}
	if driveOperatorCapacityExhausted(planner, capPlanner) {
		// F1: operators were found but every one has 0 remaining capacity. Do NOT fall through to
		// lockAndRefreshDriveCapacity or limitParkSelectionByDriveAnimals, which treat
		// MaxGoatsPerDrive == 0 as "no cap configured" and would admit onto already-exhausted operators.
		session.releaseClaims(shotClaims)
		_ = visitRelease(ctx)
		return remaining, 0, plannedDate, false, true, nil
	}
	driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, parkID, plannedDate, capPlanner.MaxGoatsPerDrive, session)
	if err != nil {
		session.releaseClaims(shotClaims)
		_ = visitRelease(ctx)
		return remaining, 0, plannedDate, false, true, err
	}
	release := combineReleases(driveRelease, visitRelease)
	if capPlanner.MaxGoatsPerDrive > 0 {
		capped := limitParkSelectionByDriveAnimals(now, orderedRemaining, selected, *plannedDate, capPlanner, configuredParkAnimalCap(planner, capPlanner), session)
		if len(capped) < len(selected) {
			animalCapReached = true
			cappedClaims := splitShotCapReservations(shotClaims, selected, [][]string{capped})
			session.releaseClaims(claimsOutsideSelection(shotClaims, capped))
			if len(cappedClaims) > 0 {
				shotClaims = cappedClaims[0]
			} else {
				shotClaims = nil
			}
			selected = capped
		}
	}
	defer func() {
		if relErr := release(ctx); relErr != nil && err == nil {
			err = relErr
		}
	}()

	selectedRows := filterRows(remaining, selected)
	if int32(uniqueParkTargetCount(selectedRows)) < minMergeTargets {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, animalCapReached, !animalCapReached, nil
	}
	windowStart, windowEnd := parkDriveWindow(selectedRows, selected)
	var batchingHoldUntil *time.Time
	if parkDriveDateUsesBatchingHold(*plannedDate, selectedRows) {
		holdUntil := *plannedDate
		batchingHoldUntil = &holdUntil
	}
	// VAXCAP-003: carry each obligation's OWN cell count into the batch writer -- a park merge
	// routinely mixes 1- and 2-dose rules, so the persisted total must be the sum over the rows
	// actually attached, never a selected-set total or an average.
	cellsByObligation := make(map[string]int32, len(selectedRows))
	for _, row := range selectedRows {
		cellsByObligation[row.ObligationID] = parkCandidateDriveCells(cfg, row)
	}
	newBatch := domain.NewBatch{
		TenantID:          tenantID,
		ProtocolVersionID: versionID,
		ScopeType:         "park",
		ScopeID:           parkID,
		Session:           parkConsolidationSession(selected),
		PlannedDate:       plannedDate,
		WindowStart:       windowStart,
		WindowEnd:         windowEnd,
		Status:            "planned",
		EstimatedTargets:  int32(uniqueParkTargetCount(selectedRows)),
		PlannedQuantity:   parkDrivePlannedQuantity(cfg, selectedRows),
		QuantityUnit:      "dose",
		BatchingHoldUntil: batchingHoldUntil,
	}
	if assignErr := s.assignVaccinationOperator(ctx, &newBatch, planner.MaxGoatsPerDrive, session); assignErr != nil {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, animalCapReached, true, assignErr
	}
	driveAssignments := driveAssignmentsForParkConsolidation("pending", newBatch, selectedRows)
	driveAssignments, assignErr := s.distributeVaccinationDriveAssignments(ctx, tenantID, newBatch, planner.MaxGoatsPerDrive, driveAssignments, session)
	if assignErr != nil {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, animalCapReached, true, assignErr
	}
	newBatch.DriveAssignments = driveAssignments
	batchID, attachedIDs, createErr := s.createBatchWithAttachedIDs(ctx, newBatch, selected, cellsByObligation)
	if createErr != nil {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, animalCapReached, true, createErr
	}
	if len(attachedIDs) == 0 {
		session.releaseClaims(shotClaims)
		return remaining, 0, plannedDate, animalCapReached, true, nil
	}
	if len(attachedIDs) < len(selected) {
		session.releaseClaims(claimsOutsideSelection(shotClaims, attachedIDs))
	}
	// BUG-041 (park-consolidation path): the drive rows above were built from selectedRows PRE-attach.
	// createBatchWithAttachedIDs can attach a subset (some rows became ineligible in-tx) or MERGE into
	// an existing planned park batch that already holds obligations from a prior step -- either way the
	// pre-built set may not cover the batch's real full attached membership. Rebuild from the batch's
	// FULL attached obligation set (real operators, target excluded from its own load, no NULL-operator
	// rows, atomic replace+membership-sync), exactly like the per-rule finalize and combo-align merge
	// paths. Only the in-memory test fakes (no full-batch read) keep the pre-built create-path set.
	if _, ok := s.repo.(driveRebuildInputsReader); ok {
		if rebuildErr := s.RebuildMergedBatchDriveAssignments(ctx, tenantID, batchID, planner.MaxGoatsPerDrive, session); rebuildErr != nil {
			session.releaseClaims(shotClaims)
			return remaining, 0, plannedDate, animalCapReached, true, rebuildErr
		}
	}
	claimParkDriveAnimals(session, filterRows(remaining, attachedIDs), *plannedDate)
	return removeRows(remaining, attachedIDs), int64(len(attachedIDs)), plannedDate, animalCapReached, false, nil
}

func (s *SweeperService) applyParkDriveDateOverrides(ctx context.Context, tenantID string, cfg SweepConfig, rows []domain.ParkConsolidationCandidate) ([]domain.ParkConsolidationCandidate, error) {
	reader, ok := s.repo.(vaccinationDriveDateOverrideReader)
	if !ok || len(rows) == 0 {
		return rows, nil
	}
	type key struct {
		parkID      string
		vaccineCode string
		original    string
	}
	cache := make(map[key]*time.Time)
	out := append([]domain.ParkConsolidationCandidate(nil), rows...)
	for i := range out {
		identity := cfg.getRuleVaccineIdentity(out[i].RuleID)
		vaccineCode := strings.TrimSpace(identity.VaccineCode)
		parkID := strings.TrimSpace(out[i].ParkID)
		if vaccineCode == "" || parkID == "" || out[i].DueAt.IsZero() {
			continue
		}
		original := businessDate(out[i].DueAt)
		k := key{parkID: parkID, vaccineCode: vaccineCode, original: original.Format("2006-01-02")}
		overrideDate, seen := cache[k]
		if !seen {
			override, err := reader.ActiveVaccinationDriveDateOverride(ctx, tenantID, parkID, vaccineCode, original) // scale-guard:ignore: bounded by sweeper page and cached by park/vaccine/original date; not a request path
			if err != nil {
				return nil, err
			}
			if override != nil && !override.OverrideDate.IsZero() {
				day := businessDate(override.OverrideDate)
				overrideDate = &day
			}
			cache[k] = overrideDate
		}
		if overrideDate == nil {
			continue
		}
		overrideEnd := driveDateOverrideWindowEnd(*overrideDate)
		out[i].DueAt = *overrideDate
		out[i].WindowStart = overrideDate
		out[i].WindowEnd = &overrideEnd
		out[i].BatchingHoldCount = 0
		out[i].FirstBatchingHoldUntil = nil
	}
	return out, nil
}

func claimParkDriveAnimals(session *SweepSession, rows []domain.ParkConsolidationCandidate, plannedDate time.Time) {
	if session == nil {
		return
	}
	seenTargets := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		targetKey := parkCandidateTargetKey(row)
		if _, ok := seenTargets[targetKey]; ok {
			continue
		}
		seenTargets[targetKey] = struct{}{}
		session.claimDriveCapacity(row.ParkID, plannedDate, targetKey, 1)
	}
}

func driveAssignmentsForParkConsolidation(batchID string, batch domain.NewBatch, rows []domain.ParkConsolidationCandidate) []domain.DriveAssignment {
	if strings.TrimSpace(batchID) == "" || batch.PlannedDate == nil || strings.TrimSpace(batch.ScopeID) == "" || len(rows) == 0 {
		return nil
	}
	type assignmentBucket struct {
		parkID       string
		shedID       *string
		physicalShed string
		partition    string
		targets      map[string]struct{}
		ruleIDs      map[string]struct{}
		doseKeys     map[string]struct{}
	}
	buckets := make(map[string]*assignmentBucket)
	for _, row := range rows {
		parkID := strings.TrimSpace(row.ParkID)
		if parkID == "" {
			parkID = batch.ScopeID
		}
		var shedID *string
		if strings.TrimSpace(row.ShedID) != "" {
			s := strings.TrimSpace(row.ShedID)
			shedID = &s
		}
		physicalShed, partition := normalizeAssignmentShed(row.ShedName)
		if physicalShed == "" {
			physicalShed = "park"
		}
		key := parkID + "\x00" + stringPtrValue(shedID) + "\x00" + physicalShed + "\x00" + partition
		bucket := buckets[key]
		if bucket == nil {
			bucket = &assignmentBucket{
				parkID:       parkID,
				shedID:       shedID,
				physicalShed: physicalShed,
				partition:    partition,
				targets:      map[string]struct{}{},
				ruleIDs:      map[string]struct{}{},
				doseKeys:     map[string]struct{}{},
			}
			buckets[key] = bucket
		}
		targetKey := parkCandidateTargetKey(row)
		if targetKey != "" {
			bucket.targets[targetKey] = struct{}{}
			if ruleID := strings.TrimSpace(row.RuleID); ruleID != "" {
				bucket.ruleIDs[ruleID] = struct{}{}
				bucket.doseKeys[targetKey+"\x00"+ruleID] = struct{}{}
			}
		}
	}
	out := make([]domain.DriveAssignment, 0, len(buckets))
	for _, bucket := range buckets {
		out = append(out, domain.DriveAssignment{
			BatchID:        batchID,
			PlannedDate:    *batch.PlannedDate,
			OperatorID:     batch.ConductedBy,
			ParkID:         bucket.parkID,
			ShedID:         bucket.shedID,
			PhysicalShed:   bucket.physicalShed,
			PartitionLabel: bucket.partition,
			AnimalCount:    int32(len(bucket.targets)),
			VaccineRuleIDs: sortedStringSet(bucket.ruleIDs),
			TotalDoses:     int32(len(bucket.doseKeys)),
			CapacityStatus: "within_cap",
		})
	}
	return out
}

func parkConsolidationCursorKey(cursor *domain.ParkConsolidationCursor) string {
	if cursor == nil {
		return ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s",
		cursor.ParkID,
		cursor.TargetSpecies,
		cursor.TargetAnimalStage,
		cursor.DueAt.UTC().Format(time.RFC3339Nano),
		cursor.RuleID,
		cursor.ObligationID,
	)
}

func parkConsolidationCursor(row domain.ParkConsolidationCandidate) *domain.ParkConsolidationCursor {
	return &domain.ParkConsolidationCursor{
		ParkID:            row.ParkID,
		TargetSpecies:     row.TargetSpecies,
		TargetAnimalStage: row.TargetAnimalStage,
		DueAt:             row.DueAt,
		RuleID:            row.RuleID,
		ObligationID:      row.ObligationID,
	}
}

func parkConsolidationDriveGroupKey(cfg SweepConfig, row domain.ParkConsolidationCandidate) string {
	identity := cfg.getRuleVaccineIdentity(row.RuleID)
	// Blue Tongue participates in two approved bundles. When this published matrix
	// intentionally excludes PPR but carries Sheep Pox, put the two sheep lanes in
	// the same park group. Protocols that still carry PPR retain the established,
	// higher-priority PPR+Blue Tongue session selected by batchSession.
	if normalizedVaccineMatrixCode(identity.VaccineCode) == "blue tongue" &&
		sweepConfigHasVaccine(cfg, "sheep pox") && !sweepConfigHasVaccine(cfg, "ppr") {
		return "combo:Sheep Pox+Blue Tongue"
	}
	if parkCandidateUsesDriveDateOverrideWindow(row) {
		if session := batchSession(row.RuleID, identity.VaccineCode); strings.HasPrefix(strings.TrimSpace(session), "combo:") {
			return session
		}
		if code := strings.TrimSpace(identity.VaccineCode); code != "" {
			return "vaccine:" + normalizedVaccineMatrixCode(code)
		}
	}
	if session := batchSession(row.RuleID, identity.VaccineCode); strings.TrimSpace(session) != "" {
		return session
	}
	if code := strings.TrimSpace(identity.VaccineCode); code != "" {
		return "vaccine:" + normalizedVaccineMatrixCode(code)
	}
	if ruleID := strings.TrimSpace(row.RuleID); ruleID != "" {
		return "rule:" + ruleID
	}
	return "vaccine:unknown"
}

func sweepConfigHasVaccine(cfg SweepConfig, vaccineCode string) bool {
	want := normalizedVaccineMatrixCode(vaccineCode)
	for _, identity := range cfg.RuleVaccineIDs {
		if normalizedVaccineMatrixCode(identity.VaccineCode) == want {
			return true
		}
	}
	return false
}

func parkConsolidationGroupKey(cfg SweepConfig, row domain.ParkConsolidationCandidate) string {
	return strings.TrimSpace(row.ParkID) + "|" + parkConsolidationDriveGroupKey(cfg, row)
}

func parkCandidateUsesDriveDateOverrideWindow(row domain.ParkConsolidationCandidate) bool {
	if row.DueAt.IsZero() || row.WindowStart == nil || row.WindowEnd == nil {
		return false
	}
	start := businessDate(*row.WindowStart)
	due := businessDate(row.DueAt)
	end := businessDate(*row.WindowEnd)
	return start.Equal(due) && end.Equal(due.AddDate(0, 0, 1)) && row.BatchingHoldCount == 0 && row.FirstBatchingHoldUntil == nil
}

func parkSelectionUsesDriveDateOverrideWindow(rows []domain.ParkConsolidationCandidate) bool {
	if len(rows) == 0 {
		return false
	}
	for _, row := range rows {
		if !parkCandidateUsesDriveDateOverrideWindow(row) {
			return false
		}
	}
	return true
}

func orderParkConsolidationGroups(order []string, groups map[string][]domain.ParkConsolidationCandidate, cfg SweepConfig) []string {
	out := append([]string(nil), order...)
	sort.SliceStable(out, func(i, j int) bool {
		left := groups[out[i]]
		right := groups[out[j]]
		leftEnd := parkConsolidationGroupLatestSafeDate(left)
		rightEnd := parkConsolidationGroupLatestSafeDate(right)
		if !leftEnd.Equal(rightEnd) {
			if leftEnd.IsZero() {
				return false
			}
			if rightEnd.IsZero() {
				return true
			}
			return leftEnd.Before(rightEnd)
		}
		leftPriority := parkConsolidationGroupVaccinePriority(left, cfg)
		rightPriority := parkConsolidationGroupVaccinePriority(right, cfg)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return out[i] < out[j]
	})
	return out
}

func parkConsolidationGroupLatestSafeDate(rows []domain.ParkConsolidationCandidate) time.Time {
	var latest time.Time
	for _, row := range rows {
		rowLatest := parkCandidateLatestSafeDate(row)
		if rowLatest.IsZero() {
			continue
		}
		if latest.IsZero() || rowLatest.Before(latest) {
			latest = rowLatest
		}
	}
	return latest
}

func parkConsolidationGroupVaccinePriority(rows []domain.ParkConsolidationCandidate, cfg SweepConfig) int32 {
	var priority int32
	for _, row := range rows {
		rowPriority := cfg.getRuleVaccineIdentity(row.RuleID).VaccinePriority
		if rowPriority <= 0 {
			continue
		}
		if priority == 0 || rowPriority < priority {
			priority = rowPriority
		}
	}
	if priority <= 0 {
		return normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode).VaccinePriority
	}
	return priority
}

func (s *SweeperService) recordParkBatchingHoldIfNeeded(ctx context.Context, tenantID string, ids []string, rows []domain.ParkConsolidationCandidate, plannedDate *time.Time, occurredAt time.Time) error {
	if plannedDate == nil || len(ids) == 0 || !parkDriveDateUsesBatchingHold(*plannedDate, rows) {
		return nil
	}
	recorder, ok := s.repo.(batchingHoldRecorder)
	if !ok {
		return nil
	}
	_, err := recorder.RecordBatchingHoldForObligations(ctx, tenantID, ids, *plannedDate, occurredAt)
	return err
}

func parkDriveDateUsesBatchingHold(planned time.Time, rows []domain.ParkConsolidationCandidate) bool {
	planned = biztime.BusinessDayStart(planned)
	for _, row := range rows {
		if planned.After(biztime.BusinessDayStart(row.DueAt)) {
			return true
		}
	}
	return false
}

func parkConsolidationSession(selected []string) string {
	if len(selected) == 0 {
		return "park-consolidation"
	}
	return "park-consolidation:" + selected[0]
}

func parkDrivePlannedQuantity(cfg SweepConfig, rows []domain.ParkConsolidationCandidate) string {
	total := int64(0)
	for _, row := range rows {
		total += int64(normalizedDosesPerGoat(cfg.forRule(row.RuleID).DosesPerGoat))
	}
	return strconv.FormatInt(total, 10)
}

func filterRows(rows []domain.ParkConsolidationCandidate, selected []string) []domain.ParkConsolidationCandidate {
	if len(selected) == 0 {
		return nil
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]domain.ParkConsolidationCandidate, 0, len(selected))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; ok {
			out = append(out, row)
		}
	}
	return out
}

func claimsOutsideSelection(claims []shotCapReservation, selected []string) []shotCapReservation {
	if len(claims) == 0 {
		return nil
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]shotCapReservation, 0, len(claims))
	for _, claim := range claims {
		if _, ok := selectedSet[claim.obligationID]; !ok {
			out = append(out, claim)
		}
	}
	return out
}

func removeRows(rows []domain.ParkConsolidationCandidate, selected []string) []domain.ParkConsolidationCandidate {
	if len(selected) == 0 {
		return rows
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]domain.ParkConsolidationCandidate, 0, len(rows))
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			out = append(out, row)
		}
	}
	return out
}

func uniqueParkTargetCount(rows []domain.ParkConsolidationCandidate) int {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if row.TargetID == "" {
			continue
		}
		seen[row.TargetID] = struct{}{}
	}
	return len(seen)
}

func pickBestParkDriveDate(now time.Time, rows []domain.ParkConsolidationCandidate, minMergeTargets int32) (*time.Time, []string) {
	return pickBestParkDriveDateExcluding(now, rows, domain.DefaultDrivePlannerSettings(), minMergeTargets, nil)
}

// pickBestParkDriveDateExcluding is the capacity-BLIND date ranking, used only when no
// session/repo capacity context exists. The real merge and preflight paths use
// selectBestParkDriveDateWithCapacity (VAXCAP-005), which scores each date post-capacity.
func pickBestParkDriveDateExcluding(now time.Time, rows []domain.ParkConsolidationCandidate, planner domain.DrivePlannerSettings, minMergeTargets int32, excludedDates map[string]struct{}) (*time.Time, []string) {
	candidates := parkDriveCandidateDates(now, rows)
	nowDay := biztime.BusinessDayStart(now)
	bestTargets := 0
	bestObligations := 0
	var bestDate *time.Time
	var bestIDs []string
	for _, candidate := range candidates {
		if candidate.Before(nowDay) {
			continue
		}
		if _, excluded := excludedDates[candidate.Format("2006-01-02")]; excluded {
			continue
		}
		ids := obligationsFeasibleOnDateForPlanner(now, candidate, rows, planner)
		selectedRows := filterRows(rows, ids)
		targets := uniqueParkTargetCount(selectedRows)
		if int32(targets) < minMergeTargets {
			continue
		}
		obligations := len(ids)
		if targets > bestTargets || (targets == bestTargets && obligations > bestObligations) {
			bestTargets = targets
			bestObligations = obligations
			day := candidate
			bestDate = &day
			bestIDs = ids
			continue
		}
		if targets == bestTargets && obligations == bestObligations && obligations > 0 && bestDate != nil && candidate.Before(*bestDate) {
			day := candidate
			bestDate = &day
			bestIDs = ids
		}
	}
	return bestDate, bestIDs
}

// selectBestParkDriveDateWithCapacity scores EVERY feasible candidate date by the animal count
// admissible AFTER the persisted park/date animal-slot capacity plus this session's in-memory claims
// (VAXCAP-005). The date maximizing post-capacity admitted animals wins (then admitted
// obligations); the earliest date breaks ties only. Every per-date probe releases its shot-cap
// claims and locks before moving on, so scoring is side-effect-free on the session.
func (s *SweeperService) selectBestParkDriveDateWithCapacity(ctx context.Context, tenantID string, cfg SweepConfig, planner domain.DrivePlannerSettings, now time.Time, remaining []domain.ParkConsolidationCandidate, minMergeTargets int32, excludedDates map[string]struct{}, session *SweepSession) (*time.Time, error) {
	targetIDs := distinctParkTargetIDs(remaining)
	orderedRemaining := orderParkCandidatesByVaccinePriority(remaining, cfg.getRuleVaccineIdentity)
	parkID := firstParkID(remaining)
	candidates := parkDriveCandidateDates(now, remaining)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	nowDay := biztime.BusinessDayStart(now)
	bestAnimals := -1
	bestObligations := -1
	var bestDate *time.Time
	overrideWindow := parkSelectionUsesDriveDateOverrideWindow(remaining)
	for _, candidate := range candidates {
		if candidate.Before(nowDay) {
			continue
		}
		if _, excluded := excludedDates[candidate.Format("2006-01-02")]; excluded {
			continue
		}
		feasible := obligationsFeasibleOnDateForPlanner(now, candidate, remaining, planner)
		if len(feasible) == 0 {
			continue
		}
		day := candidate
		visitRelease, err := s.lockAndRefreshVisitShots(ctx, tenantID, targetIDs, &day, planner.MaxShotsPerAnimalPerDrive, session)
		if err != nil {
			return nil, err
		}
		capPlanner, err := s.operatorCapacityPlannerForTargets(ctx, tenantID, parkID, &day, planner, session, targetIDs)
		if err != nil {
			_ = visitRelease(ctx)
			return nil, err
		}
		if driveOperatorCapacityExhausted(planner, capPlanner) {
			// F1: operators were found but every one has 0 remaining capacity. Do NOT fall through to
			// lockAndRefreshDriveCapacity or limitParkSelectionByDriveAnimals, which treat
			// MaxGoatsPerDrive == 0 as "no cap configured" and would admit onto already-exhausted operators.
			_ = visitRelease(ctx)
			continue
		}
		driveRelease, err := s.lockAndRefreshDriveCapacity(ctx, tenantID, parkID, &day, capPlanner.MaxGoatsPerDrive, session)
		if err != nil {
			_ = visitRelease(ctx)
			return nil, err
		}
		release := combineReleases(driveRelease, visitRelease)
		selected, shotClaims, err := selectParkIDsWithinVisitShotCapForSession(now, orderedRemaining, feasible, &day, planner, cfg.getRuleVaccineIdentity, session)
		if err != nil {
			_ = release(ctx)
			return nil, err
		}
		capped := selected
		scored := selected
		if capPlanner.MaxGoatsPerDrive > 0 {
			capped = limitParkSelectionByDriveAnimals(now, orderedRemaining, selected, day, capPlanner, configuredParkAnimalCap(planner, capPlanner), session)
			// Rank by the WITHIN-CAP admissible set only: last-safe overflow admissions keep a date
			// eligible (threshold below) but must not make an over-cap date outrank a date that fits
			// the same animals inside its free capacity.
			scored = parkSelectionWithinCapNoOverflow(now, orderedRemaining, capped, day, capPlanner, session, cfg)
		}
		scoreAnimals := uniqueParkTargetCount(filterRows(remaining, scored))
		session.releaseClaims(shotClaims)
		if err := release(ctx); err != nil {
			return nil, err
		}
		if int32(scoreAnimals) < minMergeTargets {
			continue
		}
		if overrideWindow {
			if bestDate == nil || day.Before(*bestDate) {
				bestAnimals = scoreAnimals
				bestObligations = len(scored)
				chosen := day
				bestDate = &chosen
			}
			continue
		}
		// Strictly-greater comparisons + ascending date iteration = earliest date wins ties only.
		if scoreAnimals > bestAnimals || (scoreAnimals == bestAnimals && len(scored) > bestObligations) {
			bestAnimals = scoreAnimals
			bestObligations = len(scored)
			chosen := day
			bestDate = &chosen
		}
	}
	return bestDate, nil
}

// parkSelectionWithinCapNoOverflow admits selected rows immovable-first strictly WITHIN the
// remaining park/date animal-slot capacity -- no last-safe overflow. Used only for date RANKING in
// selectBestParkDriveDateWithCapacity; real admission (with legitimate overflow) stays
// limitParkSelectionByDriveAnimals.
func parkSelectionWithinCapNoOverflow(now time.Time, rows []domain.ParkConsolidationCandidate, selected []string, plannedDate time.Time, planner domain.DrivePlannerSettings, session *SweepSession, cfg SweepConfig) []string {
	maxAnimals := planner.MaxGoatsPerDrive
	if maxAnimals <= 0 || len(selected) == 0 {
		return selected
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	used := session.driveCapacityUsed(firstParkID(rows), plannedDate)
	admitted := make(map[string]struct{}, len(selected))
	admittedTargets := make(map[string]struct{}, len(selected))
	admitPass := func(movable bool) {
		for _, row := range rows {
			if _, ok := selectedSet[row.ObligationID]; !ok {
				continue
			}
			if _, ok := admitted[row.ObligationID]; ok {
				continue
			}
			if parkObligationCanMoveAfter(now, plannedDate, row, planner) != movable {
				continue
			}
			targetKey := parkCandidateTargetKey(row)
			if _, ok := admittedTargets[targetKey]; !ok {
				if used+1 > maxAnimals {
					continue
				}
				admittedTargets[targetKey] = struct{}{}
				used++
			}
			admitted[row.ObligationID] = struct{}{}
		}
	}
	admitPass(false)
	admitPass(true)
	out := make([]string, 0, len(admitted))
	for _, row := range rows {
		if _, ok := admitted[row.ObligationID]; ok {
			out = append(out, row.ObligationID)
		}
	}
	return out
}

func parkDriveCandidateDates(now time.Time, rows []domain.ParkConsolidationCandidate) []time.Time {
	seen := make(map[string]time.Time)
	add := func(t time.Time) {
		if t.IsZero() {
			return
		}
		day := biztime.BusinessDayStart(t)
		seen[day.Format("2006-01-02")] = day
	}
	add(now)
	for _, row := range rows {
		add(row.DueAt)
		if row.WindowStart != nil {
			add(*row.WindowStart)
		}
		if row.WindowEnd != nil {
			add(*row.WindowEnd)
		}
		start := obligationEarliestDate(row)
		if row.WindowEnd != nil && !row.WindowEnd.IsZero() {
			for day, end, offset := start, obligationLatestDate(row), 0; !day.After(end) && offset < 14; day, offset = day.AddDate(0, 0, 1), offset+1 {
				add(day)
			}
		}
	}
	out := make([]time.Time, 0, len(seen))
	for _, day := range seen {
		out = append(out, day)
	}
	return out
}

func obligationsFeasibleOnDate(now, day time.Time, rows []domain.ParkConsolidationCandidate) []string {
	return obligationsFeasibleOnDateForPlanner(now, day, rows, domain.DefaultDrivePlannerSettings())
}

func obligationsFeasibleOnDateForPlanner(now, day time.Time, rows []domain.ParkConsolidationCandidate, planner domain.DrivePlannerSettings) []string {
	day = biztime.BusinessDayStart(day)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if !parkObligationFeasibleOnPlannerDate(now, day, row, planner) {
			continue
		}
		ids = append(ids, row.ObligationID)
	}
	return ids
}

// limitParkSelectionByDriveAnimals enforces the park/date animal-slot cap with two-pass admission
// (VAXCAP-006): pass 1 reserves slots for IMMOVABLE rows -- obligations that cannot legally move
// to any later feasible date (last-safe-day / due+7 hold boundary); pass 2 fills the remaining
// capacity with movable rows in route order. A movable row can therefore never consume a slot a
// last-safe row needs, and neither pass admits animals beyond the operator-day cap.
func limitParkSelectionByDriveAnimals(now time.Time, rows []domain.ParkConsolidationCandidate, selected []string, plannedDate time.Time, planner domain.DrivePlannerSettings, configuredAnimalCap int32, session *SweepSession) []string {
	maxAnimals := planner.MaxGoatsPerDrive
	if maxAnimals <= 0 || len(selected) == 0 {
		return selected
	}
	if configuredAnimalCap <= 0 {
		configuredAnimalCap = maxAnimals
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	used := session.driveCapacityUsed(firstParkID(rows), plannedDate)
	admitted := make(map[string]struct{}, len(selected))
	admittedTargets := make(map[string]struct{}, len(selected))
	// Pass 1: immovable (last-safe / hold-boundary) rows reserve capacity first. They still use the
	// same physical shed/partition packing unit as movable rows; being near the end of the window is
	// not permission to shave a few animals off a normal partition.
	used = admitParkRouteChunks(now, rows, selectedSet, plannedDate, planner, maxAnimals, configuredAnimalCap, used, admitted, admittedTargets, false)
	// Pass 2: movable rows fill only the remaining capacity and never push past the cap. The unit of
	// admission is a route/shed chunk, not arbitrary obligation scan order: finish a physical shed
	// when it fits; otherwise try whole partitions. Carry partitions that do not fit to the next
	// operator-day, and split row-by-row only when a partition itself is larger than the day cap.
	used = admitParkRouteChunks(now, rows, selectedSet, plannedDate, planner, maxAnimals, configuredAnimalCap, used, admitted, admittedTargets, true)
	out := make([]string, 0, len(admitted))
	for _, row := range rows {
		if _, ok := admitted[row.ObligationID]; ok {
			out = append(out, row.ObligationID)
		}
	}
	return out
}

func configuredParkAnimalCap(planner, capPlanner domain.DrivePlannerSettings) int32 {
	if planner.MaxGoatsPerDrive > 0 {
		return planner.MaxGoatsPerDrive
	}
	return capPlanner.MaxGoatsPerDrive
}

func admitParkRouteChunks(now time.Time, rows []domain.ParkConsolidationCandidate, selectedSet map[string]struct{}, plannedDate time.Time, planner domain.DrivePlannerSettings, maxAnimals, configuredAnimalCap, used int32, admitted, admittedTargets map[string]struct{}, movable bool) int32 {
	groups := parkRouteGroupsByMovability(now, rows, selectedSet, plannedDate, planner, admitted, movable)
	for _, group := range groups {
		needed := newTargetCount(group.rows, admittedTargets)
		if needed == 0 {
			admitParkRows(group.rows, admitted, admittedTargets)
			continue
		}
		if used+int32(needed) <= maxAnimals {
			admitParkRows(group.rows, admitted, admittedTargets)
			used += int32(needed)
			continue
		}
		// The physical shed is the normal packing unit even when its animals came from
		// different rule rows (for example adult catch-up plus history-backed repeat).
		// A shed that fits the configured full operator cap carries intact to the next
		// operator-day; residual capacity must not peel off one of its partitions.
		if int32(group.targetCount) <= configuredAnimalCap {
			continue
		}
		for _, partition := range group.partitions {
			partitionNeeded := newTargetCount(partition.rows, admittedTargets)
			if partitionNeeded == 0 {
				admitParkRows(partition.rows, admitted, admittedTargets)
				continue
			}
			if used+int32(partitionNeeded) > maxAnimals {
				if int32(partition.targetCount) <= configuredAnimalCap {
					continue
				}
				for _, row := range partition.rows {
					targetKey := parkCandidateTargetKey(row)
					if _, ok := admittedTargets[targetKey]; !ok {
						if used+1 > maxAnimals {
							break
						}
						admittedTargets[targetKey] = struct{}{}
						used++
					}
					admitted[row.ObligationID] = struct{}{}
				}
				continue
			}
			admitParkRows(partition.rows, admitted, admittedTargets)
			used += int32(partitionNeeded)
		}
	}
	return used
}

type parkRouteGroup struct {
	physicalShed string
	routeRank    int
	rows         []domain.ParkConsolidationCandidate
	partitions   []parkPartitionGroup
	targetCount  int
}

type parkPartitionGroup struct {
	partition   string
	rows        []domain.ParkConsolidationCandidate
	targetCount int
}

func parkRouteGroupsByMovability(now time.Time, rows []domain.ParkConsolidationCandidate, selectedSet map[string]struct{}, plannedDate time.Time, planner domain.DrivePlannerSettings, admitted map[string]struct{}, movable bool) []parkRouteGroup {
	ordered := append([]domain.ParkConsolidationCandidate(nil), rows...)
	sort.SliceStable(ordered, func(i, j int) bool {
		leftShed, leftPartition := normalizeAssignmentShed(ordered[i].ShedName)
		rightShed, rightPartition := normalizeAssignmentShed(ordered[j].ShedName)
		leftRank := vaccinationRouteShedRank(leftShed)
		rightRank := vaccinationRouteShedRank(rightShed)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if leftShed != rightShed {
			return leftShed < rightShed
		}
		if leftPartition != rightPartition {
			return partitionLabelLess(leftPartition, rightPartition)
		}
		if ordered[i].RuleID != ordered[j].RuleID {
			return ordered[i].RuleID < ordered[j].RuleID
		}
		if !ordered[i].DueAt.Equal(ordered[j].DueAt) {
			return ordered[i].DueAt.Before(ordered[j].DueAt)
		}
		return ordered[i].ObligationID < ordered[j].ObligationID
	})

	// Movability is a physical-shed property for packing. If one rule row in a shed is already
	// last-safe/hold-bound, reserve the whole compatible shed in the immovable pass; otherwise the
	// two admission passes can recreate the exact catch-up-versus-repeat shed split forbidden by
	// the operator planner.
	shedMovable := make(map[string]bool)
	for _, row := range ordered {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		if _, ok := admitted[row.ObligationID]; ok {
			continue
		}
		if !parkObligationFeasibleOnPlannerDate(now, plannedDate, row, planner) {
			continue
		}
		physicalShed, _ := normalizeAssignmentShed(row.ShedName)
		if physicalShed == "" {
			physicalShed = "park"
		}
		key := strings.TrimSpace(row.ParkID) + "\x00" + physicalShed
		if _, exists := shedMovable[key]; !exists {
			shedMovable[key] = true
		}
		if !parkObligationCanMoveAfter(now, plannedDate, row, planner) {
			shedMovable[key] = false
		}
	}

	groupByShed := make(map[string]*parkRouteGroup)
	order := make([]string, 0)
	for _, row := range ordered {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		if _, ok := admitted[row.ObligationID]; ok {
			continue
		}
		if !parkObligationFeasibleOnPlannerDate(now, plannedDate, row, planner) {
			continue
		}
		physicalShed, partition := normalizeAssignmentShed(row.ShedName)
		if physicalShed == "" {
			physicalShed = "park"
		}
		movabilityKey := strings.TrimSpace(row.ParkID) + "\x00" + physicalShed
		if shedMovable[movabilityKey] != movable {
			continue
		}
		key := physicalShed
		group := groupByShed[key]
		if group == nil {
			group = &parkRouteGroup{physicalShed: physicalShed, routeRank: vaccinationRouteShedRank(physicalShed)}
			groupByShed[key] = group
			order = append(order, key)
		}
		group.rows = append(group.rows, row)
		partitionKey := partition
		if len(group.partitions) == 0 || group.partitions[len(group.partitions)-1].partition != partitionKey {
			group.partitions = append(group.partitions, parkPartitionGroup{partition: partitionKey})
		}
		last := &group.partitions[len(group.partitions)-1]
		last.rows = append(last.rows, row)
	}

	out := make([]parkRouteGroup, 0, len(order))
	for _, key := range order {
		group := groupByShed[key]
		group.targetCount = distinctParkTargetCount(group.rows)
		for i := range group.partitions {
			group.partitions[i].targetCount = distinctParkTargetCount(group.partitions[i].rows)
		}
		out = append(out, *group)
	}
	return out
}

func admitParkRows(rows []domain.ParkConsolidationCandidate, admitted, admittedTargets map[string]struct{}) {
	for _, row := range rows {
		targetKey := parkCandidateTargetKey(row)
		if targetKey != "" {
			admittedTargets[targetKey] = struct{}{}
		}
		admitted[row.ObligationID] = struct{}{}
	}
}

func newTargetCount(rows []domain.ParkConsolidationCandidate, admittedTargets map[string]struct{}) int {
	seen := make(map[string]struct{})
	for _, row := range rows {
		targetKey := parkCandidateTargetKey(row)
		if targetKey == "" {
			continue
		}
		if _, ok := admittedTargets[targetKey]; ok {
			continue
		}
		seen[targetKey] = struct{}{}
	}
	return len(seen)
}

func distinctParkTargetCount(rows []domain.ParkConsolidationCandidate) int {
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		targetKey := parkCandidateTargetKey(row)
		if targetKey == "" {
			continue
		}
		seen[targetKey] = struct{}{}
	}
	return len(seen)
}

func vaccinationRouteShedRank(physicalShed string) int {
	switch strings.ToLower(strings.TrimSpace(physicalShed)) {
	case "gandhi":
		return 10
	case "godel 1":
		return 20
	case "godel 2":
		return 30
	case "mandela 2":
		return 40
	case "old yashoda":
		return 50
	default:
		return 1000
	}
}

func partitionLabelLess(left, right string) bool {
	leftN, leftOK := partitionLabelNumber(left)
	rightN, rightOK := partitionLabelNumber(right)
	if leftOK && rightOK && leftN != rightN {
		return leftN < rightN
	}
	if leftOK != rightOK {
		return leftOK
	}
	return left < right
}

func partitionLabelNumber(value string) (int, bool) {
	cleaned := strings.TrimSpace(strings.ToLower(value))
	cleaned = strings.TrimPrefix(cleaned, "part ")
	n, err := strconv.Atoi(strings.TrimSpace(cleaned))
	return n, err == nil
}

func parkCandidateTargetKey(row domain.ParkConsolidationCandidate) string {
	targetID := strings.TrimSpace(row.TargetID)
	if targetID != "" {
		return targetID
	}
	return strings.TrimSpace(row.ObligationID)
}

func parkCandidateDriveCells(cfg SweepConfig, row domain.ParkConsolidationCandidate) int32 {
	return normalizedDosesPerGoat(cfg.forRule(row.RuleID).DosesPerGoat)
}

func firstParkID(rows []domain.ParkConsolidationCandidate) string {
	for _, row := range rows {
		if strings.TrimSpace(row.ParkID) != "" {
			return row.ParkID
		}
	}
	return ""
}

func parkObligationCanMoveAfter(now, plannedDate time.Time, row domain.ParkConsolidationCandidate, planner domain.DrivePlannerSettings) bool {
	next := businessDate(plannedDate).AddDate(0, 0, 1)
	latest := obligationLatestDate(row)
	for !latest.IsZero() && !next.After(latest) {
		if parkObligationFeasibleOnPlannerDate(now, next, row, planner) {
			return true
		}
		next = next.AddDate(0, 0, 1)
	}
	return false
}

func parkObligationFeasibleOnDate(now, day time.Time, row domain.ParkConsolidationCandidate) bool {
	return parkObligationFeasibleOnPlannerDate(now, day, row, domain.DefaultDrivePlannerSettings())
}

func parkObligationFeasibleOnPlannerDate(now, day time.Time, row domain.ParkConsolidationCandidate, planner domain.DrivePlannerSettings) bool {
	if row.DueAt.IsZero() &&
		(row.WindowStart == nil || row.WindowStart.IsZero()) &&
		(row.WindowEnd == nil || row.WindowEnd.IsZero()) {
		return true
	}
	day = biztime.BusinessDayStart(day)
	earliest := obligationEarliestDate(row)
	latest := obligationLatestDate(row)
	if day.Before(earliest) || day.After(latest) {
		return false
	}
	return driveCandidateFeasibleOnPlannerDate(now, day, driveCandidate{
		ObligationID:             row.ObligationID,
		TargetID:                 row.TargetID,
		TargetReproductiveStatus: row.TargetReproductiveStatus,
		DueAt:                    row.DueAt,
		WindowStart:              row.WindowStart,
		WindowEnd:                row.WindowEnd,
		BatchingHoldCount:        row.BatchingHoldCount,
		FirstBatchingHoldUntil:   row.FirstBatchingHoldUntil,
	}, planner)
}

func obligationEarliestDate(row domain.ParkConsolidationCandidate) time.Time {
	if row.WindowStart != nil && !row.WindowStart.IsZero() {
		return biztime.BusinessDayStart(*row.WindowStart)
	}
	return biztime.BusinessDayStart(row.DueAt)
}

func obligationLatestDate(row domain.ParkConsolidationCandidate) time.Time {
	if row.WindowEnd != nil && !row.WindowEnd.IsZero() {
		return biztime.BusinessDayStart(*row.WindowEnd)
	}
	if !row.DueAt.IsZero() {
		return biztime.BusinessDayStart(row.DueAt)
	}
	return obligationEarliestDate(row)
}

func parkDriveWindow(rows []domain.ParkConsolidationCandidate, selected []string) (*time.Time, *time.Time) {
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	var windowStart *time.Time
	var windowEnd *time.Time
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		start := obligationEarliestDate(row)
		end := obligationLatestDate(row)
		if windowStart == nil || start.After(*windowStart) {
			s := start
			windowStart = &s
		}
		if row.WindowEnd != nil && !row.WindowEnd.IsZero() && (windowEnd == nil || end.Before(*windowEnd)) {
			e := end
			windowEnd = &e
		}
	}
	return windowStart, windowEnd
}
