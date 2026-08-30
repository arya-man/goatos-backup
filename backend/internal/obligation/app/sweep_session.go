package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// SweepSession shares per-visit shot-cap accounting across every SweepVersion call made during
// one sweeper run, so MaxShotsPerAnimalPerDrive spans vaccines/protocol versions instead of
// resetting for each version. Without a shared session, an animal due 3 different vaccines
// (3 protocol versions) on the same planned business date could be scheduled 3 shots that day,
// because each version's own SweepVersion call started counting from zero.
//
// A single SweepVersion call still allocates its own private session internally, so isolated
// single-version sweeps keep their existing behavior. The production caller
// (cmd/obligation-sweeper) shares one SweepSession across every published version it sweeps in
// one run, plus the subsequent AlignComboDrives pass, via SweepVersionWithSession.
type SweepSession struct {
	visitShotCounts              map[string]int32
	visitClaims                  map[string]shotCapClaim
	plannedVaccineClaims         map[string][]plannedVaccineClaim
	vaccinationOperatorLoads     map[string]int32
	driveCellCounts              map[string]int32
	driveLoaded                  map[string]struct{}
	driveTargetClaims            map[string]map[string]struct{}
	vaccinationOperatorAvailable map[string][]domain.DriveOperatorCapacity
	// loaded marks every visitShotCountKey whose starting count has already been resolved once
	// in this session -- either seeded from the persisted, cross-pass/cross-worker committed
	// shot count (see seedResolved/SweeperService.seedAndLockVisitShots, VAX-REV-01) or, for a
	// repo that doesn't support that seed (e.g. a pure in-memory test fake), explicitly marked
	// resolved-at-zero so every group only pays the seed cost once per key per session, not once
	// per group that happens to share a (date, target) visit.
	loaded map[string]struct{}

	// driveRebuilds records every BUG-041 merged-batch drive-assignment rebuild attempt this session
	// made, with its outcome, so a skipped rebuild (e.g. no executable operator) is observable to the
	// caller and to tests instead of vanishing as silent success.
	driveRebuilds []DriveRebuildResult
}

// DriveRebuildResult is one merged-target drive-assignment rebuild outcome (BUG-041).
type DriveRebuildResult struct {
	BatchID string
	// Reason is "" when the target was rebuilt; otherwise a stable skip code:
	// "not_rebuildable" (finalized/committed/vanished), "no_attached" (no obligations),
	// or "no_operator" (no executable operator -- rows deliberately NOT replaced with NULL operators).
	Reason string
}

// Rebuilt reports whether this rebuild result actually replaced the target's rows.
func (r DriveRebuildResult) Rebuilt() bool { return r.Reason == "" }

func (s *SweepSession) recordDriveRebuild(batchID, reason string) {
	if s == nil {
		return
	}
	s.driveRebuilds = append(s.driveRebuilds, DriveRebuildResult{BatchID: batchID, Reason: reason})
}

// DriveRebuilds returns every merged-batch drive-assignment rebuild outcome recorded this session
// (BUG-041), in order. Callers and tests use it to assert a rebuild happened, or that a skip was
// surfaced with its reason rather than silently swallowed.
func (s *SweepSession) DriveRebuilds() []DriveRebuildResult {
	if s == nil {
		return nil
	}
	return append([]DriveRebuildResult(nil), s.driveRebuilds...)
}

// shotCapClaim records the vaccine/priority that most recently filled a visit's LAST cap slot,
// so a later obligation competing for the same (date, animal) visit can be told apart as either
// a resolvable lower-priority overflow or an ambiguous same-priority tie.
type shotCapClaim struct {
	vaccineCode string
	priority    int32
}

type plannedVaccineClaim struct {
	date        time.Time
	vaccineCode string
	class       vaccineMatrixClass
	combos      []string
}

type shotCapReservation struct {
	obligationID string
	key          string
	vaccineCode  string
	priority     int32
	targetID     string
	date         time.Time
}

type driveCapacityReservation struct {
	obligationID string
	key          string
	cells        int32
	claimed      bool
}

// NewSweepSession starts a fresh cross-version sweep session with empty shot-cap state.
func NewSweepSession() *SweepSession {
	return &SweepSession{
		visitShotCounts:              make(map[string]int32),
		visitClaims:                  make(map[string]shotCapClaim),
		plannedVaccineClaims:         make(map[string][]plannedVaccineClaim),
		vaccinationOperatorLoads:     make(map[string]int32),
		driveCellCounts:              make(map[string]int32),
		driveLoaded:                  make(map[string]struct{}),
		driveTargetClaims:            make(map[string]map[string]struct{}),
		vaccinationOperatorAvailable: make(map[string][]domain.DriveOperatorCapacity),
		loaded:                       make(map[string]struct{}),
	}
}

func vaccinationOperatorLoadKey(tenantID, parkID string, plannedDate time.Time, operatorID string) string {
	return strings.Join([]string{
		strings.TrimSpace(tenantID),
		strings.TrimSpace(parkID),
		businessDate(plannedDate).Format("2006-01-02"),
		strings.TrimSpace(operatorID),
	}, "\x00")
}

func vaccinationOperatorAvailabilityKey(tenantID, parkID string, plannedDate time.Time, capPerOperator int32) string {
	return strings.Join([]string{
		strings.TrimSpace(tenantID),
		strings.TrimSpace(parkID),
		businessDate(plannedDate).Format("2006-01-02"),
		fmt.Sprint(capPerOperator),
	}, "\x00")
}

func cloneDriveOperatorCapacities(values []domain.DriveOperatorCapacity) []domain.DriveOperatorCapacity {
	if len(values) == 0 {
		return nil
	}
	return append([]domain.DriveOperatorCapacity(nil), values...)
}

func (s *SweepSession) cachedVaccinationOperators(tenantID, parkID string, plannedDate time.Time, capPerOperator int32) ([]domain.DriveOperatorCapacity, bool) {
	if s == nil {
		return nil, false
	}
	values, ok := s.vaccinationOperatorAvailable[vaccinationOperatorAvailabilityKey(tenantID, parkID, plannedDate, capPerOperator)]
	if !ok {
		return nil, false
	}
	return cloneDriveOperatorCapacities(values), true
}

func (s *SweepSession) rememberVaccinationOperators(tenantID, parkID string, plannedDate time.Time, capPerOperator int32, operators []domain.DriveOperatorCapacity) {
	if s == nil {
		return
	}
	s.vaccinationOperatorAvailable[vaccinationOperatorAvailabilityKey(tenantID, parkID, plannedDate, capPerOperator)] = cloneDriveOperatorCapacities(operators)
}

func (s *SweepSession) vaccinationOperatorLoad(tenantID, parkID string, plannedDate time.Time, operatorID string) int32 {
	if s == nil {
		return 0
	}
	return s.vaccinationOperatorLoads[vaccinationOperatorLoadKey(tenantID, parkID, plannedDate, operatorID)]
}

func (s *SweepSession) rememberVaccinationOperatorLoad(tenantID, parkID string, plannedDate time.Time, operatorID string, animals int32) {
	if s == nil || animals <= 0 || strings.TrimSpace(operatorID) == "" {
		return
	}
	s.vaccinationOperatorLoads[vaccinationOperatorLoadKey(tenantID, parkID, plannedDate, operatorID)] += animals
}

// unresolvedTargets returns the distinct, non-blank target IDs in targetIDs whose (date, target)
// visit-shot-count key has NOT already been resolved in this session (seeded from a persisted
// count, or explicitly marked zero -- see seedResolved), preserving first-seen order. Callers use
// this to seed only the keys that actually need a DB round trip, so a session shared across many
// due-obligation groups on the same visit date pays the seed cost once per key, not once per
// group.
func (s *SweepSession) unresolvedTargets(date time.Time, targetIDs []string) []string {
	out := make([]string, 0, len(targetIDs))
	seen := make(map[string]struct{}, len(targetIDs))
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		if _, dup := seen[targetID]; dup {
			continue
		}
		seen[targetID] = struct{}{}
		key := visitShotCountKey(date, targetID)
		if _, ok := s.loaded[key]; ok {
			continue
		}
		out = append(out, targetID)
	}
	return out
}

// seedResolved merges a persisted per-target shot count (already committed by a prior sweep pass
// or a concurrent worker, read fresh from Postgres -- see SweeperService.seedAndLockVisitShots)
// into this session's in-memory counts, for every target in targetIDs not already resolved. A
// target with no entry in persisted is seeded at zero. Idempotent per (date, target) key: calling
// this twice for the same key is a no-op the second time, so an already-claimed in-memory count
// from earlier in this SAME pass is never clobbered.
func (s *SweepSession) seedResolved(date time.Time, targetIDs []string, persisted map[string]int32) {
	for _, targetID := range targetIDs {
		key := visitShotCountKey(date, targetID)
		if _, ok := s.loaded[key]; ok {
			continue
		}
		s.loaded[key] = struct{}{}
		s.visitShotCounts[key] += persisted[targetID]
	}
}

// resetResolved SETS this session's in-memory shot count for every (date, target) key to the
// fresh persisted count just read under the per-visit advisory lock (RV-02). Unlike seedResolved
// (additive, once-per-key), this is the WRITE-path primitive called before EACH group's select:
// by the time a group locks a shared visit, every earlier group in this same run has already
// durably committed its claim for that visit, so the fresh persisted count is the AUTHORITATIVE
// starting point -- overwriting (not adding to) the stale in-memory count carried over from the
// earlier group, and simultaneously picking up any shot a concurrent worker committed for the
// same visit in between. It (re)marks the keys loaded so the read-only additive seedResolved can
// never later double-count them. visitClaims is intentionally left intact: it records which
// vaccine last filled a slot so rejectOrTie can still tell a genuine same-priority tie from an
// ordinary overflow after the count is refreshed.
func (s *SweepSession) resetResolved(date time.Time, targetIDs []string, persisted map[string]int32) {
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		key := visitShotCountKey(date, targetID)
		s.loaded[key] = struct{}{}
		s.visitShotCounts[key] = persisted[targetID]
	}
}

// sessionOrNew returns session, or a fresh private one when the caller passed nil.
func sessionOrNew(session *SweepSession) *SweepSession {
	if session != nil {
		return session
	}
	return NewSweepSession()
}

// ShotCapPriorityTieError is the explicit blocker surfaced when more than
// MaxShotsPerAnimalPerDrive vaccines compete for one animal's planned visit and two of the
// competing vaccines resolve to EQUAL vaccine priority. The sweeper has no configured tiebreak
// in that case, so rather than silently pick an arrival-order winner it refuses the drop and
// reports the conflict: fix drive_policy.priority (or the source vaccine matrix) for one of the
// named vaccines to disambiguate, then re-run the sweep.
type ShotCapPriorityTieError struct {
	Date     time.Time
	TargetID string
	VaccineA string
	VaccineB string
	Priority int32
}

func (e *ShotCapPriorityTieError) Error() string {
	return fmt.Sprintf(
		"obligation: shot-cap priority tie for animal %s on %s between vaccines %q and %q (both resolved priority %d) -- configure drive_policy.priority to disambiguate before re-sweeping",
		e.TargetID, e.Date.Format("2006-01-02"), e.VaccineA, e.VaccineB, e.Priority,
	)
}

// claim records a successful shot-cap claim for one visit key, keeping the last-claim
// vaccine/priority so a subsequently rejected competitor can be checked for a tie.
func (s *SweepSession) claim(key, vaccineCode string, priority int32) {
	s.visitShotCounts[key]++
	s.visitClaims[key] = shotCapClaim{vaccineCode: vaccineCode, priority: priority}
}

func (s *SweepSession) rememberPlannedVaccine(targetID string, date time.Time, identity RuleVaccineIdentity) {
	targetID = strings.TrimSpace(targetID)
	vaccineCode := strings.TrimSpace(identity.VaccineCode)
	if s == nil || targetID == "" || vaccineCode == "" || date.IsZero() {
		return
	}
	s.plannedVaccineClaims[targetID] = append(s.plannedVaccineClaims[targetID], plannedVaccineClaim{
		date:        businessDate(date),
		vaccineCode: vaccineCode,
		class:       vaccineIdentityImmunoClass(identity),
		combos:      vaccineComboSessionsForCode(vaccineCode),
	})
}

func (s *SweepSession) releaseClaims(claims []shotCapReservation) {
	for i := len(claims) - 1; i >= 0; i-- {
		claim := claims[i]
		s.releasePlannedVaccine(claim.targetID, claim.date, claim.vaccineCode)
		if s.visitShotCounts[claim.key] > 0 {
			s.visitShotCounts[claim.key]--
		}
		if s.visitShotCounts[claim.key] == 0 {
			delete(s.visitShotCounts, claim.key)
			delete(s.visitClaims, claim.key)
			continue
		}
		last, ok := s.visitClaims[claim.key]
		if ok && strings.EqualFold(last.vaccineCode, claim.vaccineCode) && last.priority == claim.priority {
			delete(s.visitClaims, claim.key)
		}
	}
}

func (s *SweepSession) releasePlannedVaccine(targetID string, date time.Time, vaccineCode string) {
	targetID = strings.TrimSpace(targetID)
	vaccineCode = strings.TrimSpace(vaccineCode)
	if s == nil || targetID == "" || vaccineCode == "" || date.IsZero() {
		return
	}
	claims := s.plannedVaccineClaims[targetID]
	if len(claims) == 0 {
		return
	}
	date = businessDate(date)
	for i := len(claims) - 1; i >= 0; i-- {
		claim := claims[i]
		if claim.date.Equal(date) && strings.EqualFold(claim.vaccineCode, vaccineCode) {
			claims = append(claims[:i], claims[i+1:]...)
			break
		}
	}
	if len(claims) == 0 {
		delete(s.plannedVaccineClaims, targetID)
		return
	}
	s.plannedVaccineClaims[targetID] = claims
}

func (s *SweepSession) overflowSafeRange(targetID string, identity RuleVaccineIdentity) (time.Time, time.Time, bool) {
	targetID = strings.TrimSpace(targetID)
	vaccineCode := strings.TrimSpace(identity.VaccineCode)
	if s == nil || targetID == "" || vaccineCode == "" {
		return time.Time{}, time.Time{}, false
	}
	nextClass := vaccineIdentityImmunoClass(identity)
	var earliest time.Time
	for _, claim := range s.plannedVaccineClaims[targetID] {
		if claim.date.IsZero() || strings.EqualFold(claim.vaccineCode, vaccineCode) {
			continue
		}
		gapDays := crossVaccineSessionGapDays(claim.class, nextClass)
		if gapDays <= 0 {
			continue
		}
		candidate := businessDate(claim.date).AddDate(0, 0, int(gapDays))
		if earliest.IsZero() || candidate.After(earliest) {
			earliest = candidate
		}
	}
	if earliest.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return earliest, earliest.AddDate(0, 0, int(domain.DefaultMaxBatchingHoldDays)), true
}

func (s *SweepSession) overflowSafeRangeForDate(targetID string, identity RuleVaccineIdentity, day time.Time) (time.Time, time.Time, bool) {
	targetID = strings.TrimSpace(targetID)
	vaccineCode := strings.TrimSpace(identity.VaccineCode)
	if s == nil || targetID == "" || vaccineCode == "" {
		return time.Time{}, time.Time{}, false
	}
	nextClass := vaccineIdentityImmunoClass(identity)
	day = businessDate(day)
	var earliest time.Time
	for _, claim := range s.plannedVaccineClaims[targetID] {
		if claim.date.IsZero() || !businessDate(claim.date).Before(day) || strings.EqualFold(claim.vaccineCode, vaccineCode) {
			continue
		}
		gapDays := crossVaccineSessionGapDays(claim.class, nextClass)
		if gapDays <= 0 {
			continue
		}
		candidate := businessDate(claim.date).AddDate(0, 0, int(gapDays))
		if earliest.IsZero() || candidate.After(earliest) {
			earliest = candidate
		}
	}
	if earliest.IsZero() {
		return time.Time{}, time.Time{}, false
	}
	return earliest, earliest.AddDate(0, 0, int(domain.DefaultMaxBatchingHoldDays)), true
}

func (s *SweepSession) sameDayPlannedVaccineCount(targetID string, identity RuleVaccineIdentity, day time.Time) int {
	targetID = strings.TrimSpace(targetID)
	vaccineCode := strings.TrimSpace(identity.VaccineCode)
	if s == nil || targetID == "" || day.IsZero() {
		return 0
	}
	if vaccineIdentityImmunoClass(identity) == vaccineClassUnknown {
		return 0
	}
	day = businessDate(day)
	count := 0
	for _, claim := range s.plannedVaccineClaims[targetID] {
		if claim.date.IsZero() || !businessDate(claim.date).Equal(day) {
			continue
		}
		if strings.EqualFold(claim.vaccineCode, vaccineCode) {
			continue
		}
		if claim.class == vaccineClassUnknown {
			continue
		}
		count++
	}
	return count
}

func (s *SweepSession) sameDayCompatibleWithPlannedVaccines(targetID string, identity RuleVaccineIdentity, day time.Time) bool {
	targetID = strings.TrimSpace(targetID)
	vaccineCode := strings.TrimSpace(identity.VaccineCode)
	if s == nil || targetID == "" || vaccineCode == "" || day.IsZero() {
		return true
	}
	combos := vaccineComboSessionsForCode(vaccineCode)
	if len(combos) == 0 {
		return s.sameDayPlannedVaccineCount(targetID, identity, day) == 0
	}
	day = businessDate(day)
	for _, claim := range s.plannedVaccineClaims[targetID] {
		if claim.date.IsZero() || !businessDate(claim.date).Equal(day) {
			continue
		}
		if strings.EqualFold(claim.vaccineCode, vaccineCode) {
			continue
		}
		if !comboSessionListsOverlap(combos, claim.combos) {
			return false
		}
	}
	return true
}

func vaccineComboSessionsForCode(vaccineCode string) []string {
	return domain.ApprovedVaccineComboSessions(vaccineCode)
}

func comboSessionListsOverlap(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, session := range left {
		seen[session] = struct{}{}
	}
	for _, session := range right {
		if _, ok := seen[session]; ok {
			return true
		}
	}
	return false
}

func (s *SweepSession) vaccineFeasibleOnPlannerDate(now, day time.Time, row driveCandidate, planner domain.DrivePlannerSettings, identity RuleVaccineIdentity) bool {
	if !s.sameDayCompatibleWithPlannedVaccines(row.TargetID, identity, day) {
		return false
	}
	maxShots := planner.MaxShotsPerAnimalPerDrive
	if maxShots <= 0 {
		maxShots = domain.DefaultMaxShotsPerAnimalPerDrive
	}
	if s.sameDayPlannedVaccineCount(row.TargetID, identity, day) >= int(maxShots) {
		return false
	}
	if driveCandidateFeasibleOnPlannerDate(now, day, row, planner) {
		if start, _, ok := s.overflowSafeRangeForDate(row.TargetID, identity, day); ok && businessDate(day).Before(start) {
			return false
		}
		return true
	}
	start, end, ok := s.overflowSafeRangeForDate(row.TargetID, identity, day)
	if !ok {
		return false
	}
	day = businessDate(day)
	return !day.Before(start) && !day.After(end)
}

func (s *SweepSession) latestUnbatchedDriveDateWithOverflow(rows []domain.UnbatchedDue, identity RuleVaccineIdentity) time.Time {
	latest := latestUnbatchedDriveDate(driveCandidatesFromUnbatched(rows))
	for _, row := range rows {
		_, end, ok := s.overflowSafeRange(row.TargetID, identity)
		if ok && (latest.IsZero() || end.After(latest)) {
			latest = end
		}
	}
	return latest
}

func (s *SweepSession) unbatchedObligationsFeasibleOnDateForVaccine(now, day time.Time, rows []domain.UnbatchedDue, planner domain.DrivePlannerSettings, identity RuleVaccineIdentity) []string {
	day = businessDate(day)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if !s.vaccineFeasibleOnPlannerDate(now, day, driveCandidate{
			ObligationID:             row.ObligationID,
			TargetID:                 row.TargetID,
			TargetReproductiveStatus: row.TargetReproductiveStatus,
			DueAt:                    row.DueAt,
			WindowStart:              row.WindowStart,
			WindowEnd:                row.WindowEnd,
			BatchingHoldCount:        row.BatchingHoldCount,
			FirstBatchingHoldUntil:   row.FirstBatchingHoldUntil,
		}, planner, identity) {
			continue
		}
		ids = append(ids, row.ObligationID)
	}
	return ids
}

type vaccineMatrixClass int

const (
	vaccineClassUnknown vaccineMatrixClass = iota
	vaccineClassLive
	vaccineClassKilled
)

func vaccineMatrixImmunoClass(vaccineCode string) vaccineMatrixClass {
	switch normalizedVaccineMatrixCode(vaccineCode) {
	case "ppr", "goat pox", "sheep pox":
		return vaccineClassLive
	case "et+tt", "et tt", "blue tongue", "fmd", "hs":
		return vaccineClassKilled
	default:
		return vaccineClassUnknown
	}
}

func vaccineIdentityImmunoClass(identity RuleVaccineIdentity) vaccineMatrixClass {
	switch strings.ToLower(strings.TrimSpace(identity.VaccineType)) {
	case "live":
		return vaccineClassLive
	case "killed", "toxoid":
		return vaccineClassKilled
	default:
		return vaccineMatrixImmunoClass(identity.VaccineCode)
	}
}

func crossVaccineSessionGapDays(previous, next vaccineMatrixClass) int32 {
	switch {
	case previous == vaccineClassLive && next == vaccineClassLive:
		return 28
	case previous == vaccineClassLive && next == vaccineClassKilled:
		return 14
	case previous == vaccineClassKilled && next == vaccineClassLive:
		return 14
	case previous == vaccineClassKilled && next == vaccineClassKilled:
		return 14
	default:
		return 0
	}
}

func (s *SweepSession) claimDriveCapacity(parkID string, plannedDate time.Time, targetID string, cells int32) driveCapacityReservation {
	if cells <= 0 {
		cells = 1
	}
	key := driveCapacityKey(parkID, plannedDate)
	targetID = strings.TrimSpace(targetID)
	if targetID != "" {
		if s.driveTargetClaims[key] == nil {
			s.driveTargetClaims[key] = make(map[string]struct{})
		}
		if _, exists := s.driveTargetClaims[key][targetID]; exists {
			return driveCapacityReservation{obligationID: targetID, key: key}
		}
		s.driveTargetClaims[key][targetID] = struct{}{}
	}
	s.driveCellCounts[key] += cells
	return driveCapacityReservation{obligationID: targetID, key: key, cells: cells, claimed: true}
}

func (s *SweepSession) claimedDriveTargetCount(parkID string, plannedDate time.Time, targetIDs []string) int32 {
	if s == nil || len(targetIDs) == 0 {
		return 0
	}
	claimed := s.driveTargetClaims[driveCapacityKey(parkID, plannedDate)]
	seen := make(map[string]struct{}, len(targetIDs))
	var count int32
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		if _, duplicate := seen[targetID]; duplicate {
			continue
		}
		seen[targetID] = struct{}{}
		if _, ok := claimed[targetID]; ok {
			count++
		}
	}
	return count
}

func (s *SweepSession) resetDriveCapacity(parkID string, plannedDate time.Time, persisted int32) {
	key := driveCapacityKey(parkID, plannedDate)
	// First refresh seeds the baseline; later refreshes reconcile MONOTONICALLY: adopt the fresh
	// persisted count when it is higher than the session's running count, never lower, and never
	// reset in-session claims (VAXCAP-007). Date probes release the park/date advisory lock
	// between probes, so a concurrent worker can commit cells in the gap -- the final admission's
	// re-lock reads a fresh persisted count and MUST observe those cells (C-2). Adopting only a
	// HIGHER value keeps unpersisted preflight claims intact (persisted <= running count when
	// nothing external changed) and cannot double-count this session's own committed claims
	// (those are already inside the running count).
	if _, loaded := s.driveLoaded[key]; loaded {
		if persisted > s.driveCellCounts[key] {
			s.driveCellCounts[key] = persisted
		}
		return
	}
	s.driveLoaded[key] = struct{}{}
	s.driveCellCounts[key] = persisted
}

func (s *SweepSession) driveCapacityUsed(parkID string, plannedDate time.Time) int32 {
	return s.driveCellCounts[driveCapacityKey(parkID, plannedDate)]
}

func (s *SweepSession) releaseDriveCapacityClaims(claims []driveCapacityReservation) {
	for i := len(claims) - 1; i >= 0; i-- {
		claim := claims[i]
		if !claim.claimed || claim.cells <= 0 {
			continue
		}
		if targets := s.driveTargetClaims[claim.key]; targets != nil && claim.obligationID != "" {
			delete(targets, claim.obligationID)
			if len(targets) == 0 {
				delete(s.driveTargetClaims, claim.key)
			}
		}
		if s.driveCellCounts[claim.key] > claim.cells {
			s.driveCellCounts[claim.key] -= claim.cells
			continue
		}
		delete(s.driveCellCounts, claim.key)
	}
}

func driveCapacityKey(parkID string, plannedDate time.Time) string {
	return strings.TrimSpace(parkID) + "|" + businessDate(plannedDate).Format("2006-01-02")
}

// rejectOrTie returns the explicit blocker when a same-priority, different-vaccine obligation
// competes for a visit key that is already at cap, or nil when the rejection is an ordinary,
// resolvable priority-ordered overflow (including a vaccine overflowing its own earlier claim,
// which is not a cross-vaccine conflict at all).
func (s *SweepSession) rejectOrTie(key, vaccineCode string, priority int32, targetID string, plannedDate time.Time) error {
	last, ok := s.visitClaims[key]
	if !ok {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(last.vaccineCode), strings.TrimSpace(vaccineCode)) {
		return nil
	}
	if last.priority != priority {
		return nil
	}
	return &ShotCapPriorityTieError{
		Date:     plannedDate,
		TargetID: targetID,
		VaccineA: last.vaccineCode,
		VaccineB: vaccineCode,
		Priority: priority,
	}
}

// claimComboBatchTargets credits each target's shot count at the aligned date once a combo
// batch is moved onto it, so a later batch processed in the same alignment pass sees the
// updated count for animals it shares with an earlier-aligned batch.
func (s *SweepSession) claimComboBatchTargets(targetIDs []string, date time.Time) {
	for _, targetID := range targetIDs {
		targetID = strings.TrimSpace(targetID)
		if targetID == "" {
			continue
		}
		s.visitShotCounts[visitShotCountKey(date, targetID)]++
	}
}

// selectIDsWithinVisitShotCapForSession is the cross-version-cap-aware sibling of
// selectIDsWithinVisitShotCap: it claims slots on the shared session (spanning every version
// swept in this run) instead of a private per-call map, and it surfaces
// ShotCapPriorityTieError when the cap would force a same-priority, different-vaccine drop
// instead of silently picking an arrival-order winner.
func selectIDsWithinVisitShotCapForSession(now time.Time, rows []domain.UnbatchedDue, plannedDate *time.Time, planner domain.DrivePlannerSettings, identity RuleVaccineIdentity, session *SweepSession) ([]string, []shotCapReservation, error) {
	if plannedDate == nil {
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ObligationID)
		}
		return ids, nil, nil
	}
	selected := make([]string, 0, len(rows))
	claims := make([]shotCapReservation, 0, len(rows))
	for _, group := range unbatchedShotCapGroups(now, rows, *plannedDate, planner, identity, session) {
		groupClaims := make([]shotCapReservation, 0, len(group.rows))
		groupSelected := make([]string, 0, len(group.rows))
		groupFits := true
		for _, row := range group.rows {
			ids, nextClaims, ok, err := session.tryClaimUnbatchedShotCap(row, *plannedDate, planner, identity)
			if err != nil {
				session.releaseClaims(groupClaims)
				session.releaseClaims(claims)
				return nil, nil, err
			}
			if !ok {
				groupFits = false
				break
			}
			groupSelected = append(groupSelected, ids...)
			groupClaims = append(groupClaims, nextClaims...)
		}
		if groupFits {
			selected = append(selected, groupSelected...)
			claims = append(claims, groupClaims...)
			continue
		}
		session.releaseClaims(groupClaims)
		if planner.MaxGoatsPerDrive <= 0 || int32(group.targetCount) <= planner.MaxGoatsPerDrive {
			continue
		}
		for _, row := range group.rows {
			ids, nextClaims, ok, err := session.tryClaimUnbatchedShotCap(row, *plannedDate, planner, identity)
			if err != nil {
				session.releaseClaims(claims)
				return nil, nil, err
			}
			if !ok {
				continue
			}
			selected = append(selected, ids...)
			claims = append(claims, nextClaims...)
		}
	}
	return selected, claims, nil
}

type unbatchedShotCapGroup struct {
	rows        []domain.UnbatchedDue
	targetCount int
}

func unbatchedShotCapGroups(now time.Time, rows []domain.UnbatchedDue, plannedDate time.Time, planner domain.DrivePlannerSettings, identity RuleVaccineIdentity, session *SweepSession) []unbatchedShotCapGroup {
	byKey := make(map[string]int)
	groups := make([]unbatchedShotCapGroup, 0)
	for _, row := range rows {
		if !session.vaccineFeasibleOnPlannerDate(now, plannedDate, driveCandidate{
			ObligationID:             row.ObligationID,
			TargetID:                 row.TargetID,
			TargetReproductiveStatus: row.TargetReproductiveStatus,
			DueAt:                    row.DueAt,
			WindowStart:              row.WindowStart,
			WindowEnd:                row.WindowEnd,
			BatchingHoldCount:        row.BatchingHoldCount,
			FirstBatchingHoldUntil:   row.FirstBatchingHoldUntil,
		}, planner, identity) {
			continue
		}
		key := unbatchedRouteGroupKey(row)
		idx, ok := byKey[key]
		if !ok {
			idx = len(groups)
			byKey[key] = idx
			groups = append(groups, unbatchedShotCapGroup{})
		}
		groups[idx].rows = append(groups[idx].rows, row)
	}
	for i := range groups {
		groups[i].targetCount = uniqueUnbatchedTargetCount(groups[i].rows)
	}
	return groups
}

func (s *SweepSession) tryClaimUnbatchedShotCap(row domain.UnbatchedDue, plannedDate time.Time, planner domain.DrivePlannerSettings, identity RuleVaccineIdentity) ([]string, []shotCapReservation, bool, error) {
	if planner.MaxShotsPerAnimalPerDrive <= 0 {
		return []string{row.ObligationID}, nil, true, nil
	}
	if strings.TrimSpace(row.TargetID) == "" {
		return []string{row.ObligationID}, nil, true, nil
	}
	key := visitShotCountKey(plannedDate, row.TargetID)
	if s.visitShotCounts[key] >= planner.MaxShotsPerAnimalPerDrive {
		if err := s.rejectOrTie(key, identity.VaccineCode, identity.VaccinePriority, row.TargetID, plannedDate); err != nil {
			return nil, nil, false, err
		}
		return nil, nil, false, nil
	}
	s.claim(key, identity.VaccineCode, identity.VaccinePriority)
	s.rememberPlannedVaccine(row.TargetID, plannedDate, identity)
	claim := shotCapReservation{obligationID: row.ObligationID, key: key, vaccineCode: identity.VaccineCode, priority: identity.VaccinePriority, targetID: row.TargetID, date: plannedDate}
	return []string{row.ObligationID}, []shotCapReservation{claim}, true, nil
}

// ruleVaccineIdentityResolver resolves a park-consolidation candidate's OWN rule to its vaccine
// identity. Park-merge groups are formed by (parkID, species/stage) ONLY -- never by rule -- so a
// single merge candidate set routinely mixes obligations from several DIFFERENT matrix rules/
// vaccines (see R2-05(b)). Passing cfg.getRuleVaccineIdentity as this resolver lets the cap/tie
// selector below compare each row against its OWN real vaccine code/priority instead of one
// version-level wrapper shared by every row.
type ruleVaccineIdentityResolver = func(ruleID string) RuleVaccineIdentity

// selectParkIDsWithinVisitShotCapForSession is the park-consolidation sibling of
// selectIDsWithinVisitShotCapForSession (see its docs). Unlike the shed-batching sibling -- where
// every row in one dueGroup already shares the same rule -- a park-merge candidate set can span
// several rules/vaccines at once, so the caller supplies identityFor to resolve each row's OWN
// vaccine identity (R2-05(b) fix) instead of a single vaccineCode/priority pair applied to every
// row.
func selectParkIDsWithinVisitShotCapForSession(now time.Time, rows []domain.ParkConsolidationCandidate, selected []string, plannedDate *time.Time, planner domain.DrivePlannerSettings, identityFor ruleVaccineIdentityResolver, session *SweepSession) ([]string, []shotCapReservation, error) {
	if plannedDate == nil || len(selected) == 0 {
		return selected, nil, nil
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, id := range selected {
		selectedSet[id] = struct{}{}
	}
	out := make([]string, 0, len(selected))
	claims := make([]shotCapReservation, 0, len(selected))
	for _, group := range parkShotCapGroups(now, rows, selectedSet, *plannedDate, planner, identityFor, session) {
		groupClaims := make([]shotCapReservation, 0, len(group.rows))
		groupSelected := make([]string, 0, len(group.rows))
		groupFits := true
		for _, row := range group.rows {
			identity := identityFor(row.RuleID)
			ids, nextClaims, ok, err := session.tryClaimParkShotCap(row, *plannedDate, planner, identity)
			if err != nil {
				session.releaseClaims(groupClaims)
				session.releaseClaims(claims)
				return nil, nil, err
			}
			if !ok {
				groupFits = false
				break
			}
			groupSelected = append(groupSelected, ids...)
			groupClaims = append(groupClaims, nextClaims...)
		}
		if groupFits {
			out = append(out, groupSelected...)
			claims = append(claims, groupClaims...)
			continue
		}
		session.releaseClaims(groupClaims)
		if planner.MaxGoatsPerDrive <= 0 || int32(group.targetCount) <= planner.MaxGoatsPerDrive {
			continue
		}
		for _, row := range group.rows {
			identity := identityFor(row.RuleID)
			ids, nextClaims, ok, err := session.tryClaimParkShotCap(row, *plannedDate, planner, identity)
			if err != nil {
				session.releaseClaims(claims)
				return nil, nil, err
			}
			if !ok {
				continue
			}
			out = append(out, ids...)
			claims = append(claims, nextClaims...)
		}
	}
	return out, claims, nil
}

type parkShotCapGroup struct {
	rows        []domain.ParkConsolidationCandidate
	targetCount int
}

func parkShotCapGroups(now time.Time, rows []domain.ParkConsolidationCandidate, selectedSet map[string]struct{}, plannedDate time.Time, planner domain.DrivePlannerSettings, identityFor ruleVaccineIdentityResolver, session *SweepSession) []parkShotCapGroup {
	byKey := make(map[string]int)
	groups := make([]parkShotCapGroup, 0)
	for _, row := range rows {
		if _, ok := selectedSet[row.ObligationID]; !ok {
			continue
		}
		identity := identityFor(row.RuleID)
		if !session.vaccineFeasibleOnPlannerDate(now, plannedDate, driveCandidate{
			ObligationID:             row.ObligationID,
			TargetID:                 row.TargetID,
			TargetReproductiveStatus: row.TargetReproductiveStatus,
			DueAt:                    row.DueAt,
			WindowStart:              row.WindowStart,
			WindowEnd:                row.WindowEnd,
			BatchingHoldCount:        row.BatchingHoldCount,
			FirstBatchingHoldUntil:   row.FirstBatchingHoldUntil,
		}, planner, identity) {
			continue
		}
		physicalShed, partition := normalizeAssignmentShed(row.ShedName)
		ruleKey := strings.TrimSpace(row.RuleID)
		if parkCandidateUsesDriveDateOverrideWindow(row) {
			ruleKey = "manual-anchor:" + businessDate(row.DueAt).Format("2006-01-02")
		}
		key := strings.TrimSpace(row.ParkID) + "\x00" + strings.TrimSpace(row.ShedID) + "\x00" + physicalShed + "\x00" + partition + "\x00" + ruleKey
		idx, ok := byKey[key]
		if !ok {
			idx = len(groups)
			byKey[key] = idx
			groups = append(groups, parkShotCapGroup{})
		}
		groups[idx].rows = append(groups[idx].rows, row)
	}
	for i := range groups {
		groups[i].targetCount = distinctParkTargetCount(groups[i].rows)
	}
	return groups
}

func (s *SweepSession) tryClaimParkShotCap(row domain.ParkConsolidationCandidate, plannedDate time.Time, planner domain.DrivePlannerSettings, identity RuleVaccineIdentity) ([]string, []shotCapReservation, bool, error) {
	if planner.MaxShotsPerAnimalPerDrive <= 0 {
		return []string{row.ObligationID}, nil, true, nil
	}
	if strings.TrimSpace(row.TargetID) == "" {
		return []string{row.ObligationID}, nil, true, nil
	}
	key := visitShotCountKey(plannedDate, row.TargetID)
	if s.visitShotCounts[key] >= planner.MaxShotsPerAnimalPerDrive {
		if err := s.rejectOrTie(key, identity.VaccineCode, identity.VaccinePriority, row.TargetID, plannedDate); err != nil {
			return nil, nil, false, err
		}
		return nil, nil, false, nil
	}
	s.claim(key, identity.VaccineCode, identity.VaccinePriority)
	s.rememberPlannedVaccine(row.TargetID, plannedDate, identity)
	claim := shotCapReservation{obligationID: row.ObligationID, key: key, vaccineCode: identity.VaccineCode, priority: identity.VaccinePriority, targetID: row.TargetID, date: plannedDate}
	return []string{row.ObligationID}, []shotCapReservation{claim}, true, nil
}

// SweepVersionPriority pairs a protocol version with its resolved sweep config, letting a
// caller that sweeps several versions in one run (multiple vaccines/protocols competing for the
// same animals) order them by disease priority before sweeping. See SortSweepVersionsByPriority.
type SweepVersionPriority struct {
	VersionID string
	Config    SweepConfig
}

// SortSweepVersionsByPriority orders versions ascending by resolved vaccine priority (lower =
// schedule first, matching VaccineMatrixPriority/drive_policy.priority semantics) so a shared
// SweepSession's per-animal shot cap is claimed by the higher-priority vaccine first,
// regardless of the arbitrary order in which the caller discovered/listed the versions
// (deterministic priority selection, not arrival order).
//
// Equal-priority versions are ordered by VersionID purely to give a stable, reproducible SWEEP
// ORDER across runs; that tiebreak does NOT resolve an actual same-priority visit conflict --
// the shared session itself still returns ShotCapPriorityTieError when two equal-priority
// vaccines genuinely compete for the same animal's over-cap visit (see SweepSession).
func SortSweepVersionsByPriority(versions []SweepVersionPriority) []SweepVersionPriority {
	out := make([]SweepVersionPriority, len(versions))
	copy(out, versions)
	sort.SliceStable(out, func(i, j int) bool {
		pi := resolvedVersionPriority(out[i].Config)
		pj := resolvedVersionPriority(out[j].Config)
		if pi != pj {
			return pi < pj
		}
		return out[i].VersionID < out[j].VersionID
	})
	return out
}

func resolvedVersionPriority(cfg SweepConfig) int32 {
	return normalizedDrivePlannerSettings(cfg.DrivePlanner, cfg.VaccineCode).VaccinePriority
}
