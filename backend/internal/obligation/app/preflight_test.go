package app

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestPreflightVisitShotCapTiesDetectsCrossVersionTieWriteFree is the VAX-REV-04 guard: when 3
// vaccines (2 at equal, unresolved priority) compete for one animal's over-cap visit, the
// write-free preflight must surface *ShotCapPriorityTieError WITHOUT ever calling
// CreateBatchWithObligations, spawning a SOP task, or reserving stock -- proving that an
// orchestrator following the "preflight, then sweep for real only if clean" pattern (see
// kernelstages.ObligationSweeperStage.Run and cmd/obligation-sweeper) leaves ZERO new
// batches/tasks/reservations behind for a run that ultimately reports failure, instead of the
// pre-fix behavior where earlier-processed versions' writes were already committed by the time a
// later version's tie aborted the loop.
func TestPreflightVisitShotCapTiesDetectsCrossVersionTieWriteFree(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-alpha": {{ObligationID: "obl-alpha", RuleID: "rule-alpha", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-beta":  {{ObligationID: "obl-beta", RuleID: "rule-beta", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-gamma": {{ObligationID: "obl-gamma", RuleID: "rule-gamma", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
		},
		attachAll: true,
	}
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}

	// v-alpha and v-beta would legitimately fill the visit's 2 slots; v-gamma resolves to the
	// SAME unconfigured default priority as whichever of them claims the last slot, so it ties --
	// a genuine, unresolved cross-version conflict, exactly like
	// TestSweepVersionWithSessionBlocksOnUnresolvedPriorityTie but exercised through the preflight
	// entry point instead of the real per-version sweep loop.
	plans := []SweepVersionPriority{
		{VersionID: "v-alpha", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Alpha", SOPVersionID: "sop-1", VaccineItemID: "vaccine-1", DrivePlanner: planner}},
		{VersionID: "v-beta", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Beta", SOPVersionID: "sop-1", VaccineItemID: "vaccine-1", DrivePlanner: planner}},
		{VersionID: "v-gamma", Config: SweepConfig{VaccineCode: "Unmapped Vaccine Gamma", SOPVersionID: "sop-1", VaccineItemID: "vaccine-1", DrivePlanner: planner}},
	}

	err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due)
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError", err)
	}
	if tieErr.TargetID != "goat-1" {
		t.Fatalf("tie error target = %q, want goat-1", tieErr.TargetID)
	}

	// The core assertion: an orchestrator that checks this error before its real sweep loop (as
	// kernelstages.Run and cmd/obligation-sweeper now do) never reaches CreateBatchWithObligations,
	// SOP task creation, or stock reservation for ANY of the 3 plans -- not just v-gamma.
	if repo.createBatchCalls != 0 {
		t.Fatalf("createBatchCalls = %d, want 0 (preflight must not write any batch)", repo.createBatchCalls)
	}
	if len(repo.createdBatches) != 0 {
		t.Fatalf("createdBatches = %#v, want none", repo.createdBatches)
	}
	if tasks.calls != 0 {
		t.Fatalf("SOP task calls = %d, want 0", tasks.calls)
	}
	if reserver.calls != 0 {
		t.Fatalf("stock reservation calls = %d, want 0", reserver.calls)
	}
}

// TestPreflightVisitShotCapTiesCleanWhenNoConflict is the negative-case companion: when the
// competing plans resolve to distinct priorities and never tie (the same fixture
// TestSweepVersionWithSessionSharesShotCapAcrossVersions exercises for the real sweep), the
// preflight must return nil -- it should never false-positive-block a legitimate multi-vaccine
// sweep. The real sweep is then run against a SEPARATE, fresh session (mirroring how the
// production caller uses the preflight's own throwaway session only for detection) and produces
// the same result as without a preflight at all.
func TestPreflightVisitShotCapTiesCleanWhenNoConflict(t *testing.T) {
	winEnd := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-ettt": {{ObligationID: "obl-ettt", RuleID: "rule-ettt", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-ppr":  {{ObligationID: "obl-ppr", RuleID: "rule-ppr", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
			"v-fmd":  {{ObligationID: "obl-fmd", RuleID: "rule-fmd", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd}},
		},
		attachAll: true,
	}
	svc := NewSweeperService(repo, nil, nil)
	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}
	plans := []SweepVersionPriority{
		{VersionID: "v-ettt", Config: SweepConfig{VaccineCode: "ET+TT", DrivePlanner: planner}},
		{VersionID: "v-ppr", Config: SweepConfig{VaccineCode: "PPR", DrivePlanner: planner}},
		{VersionID: "v-fmd", Config: SweepConfig{VaccineCode: "FMD", DrivePlanner: planner}},
	}
	plans = SortSweepVersionsByPriority(plans)

	if err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due); err != nil {
		t.Fatalf("PreflightVisitShotCapTies: %v, want nil (no unresolved tie)", err)
	}

	session := NewSweepSession()
	for _, plan := range plans {
		if _, err := svc.SweepVersionWithSession(context.Background(), "tenant-1", plan.VersionID, plan.Config, due, session); err != nil {
			t.Fatalf("sweep %s: %v", plan.VersionID, err)
		}
	}
	dates := obligationIDPlannedDates(repo)
	if dates["obl-ettt"] != "2026-07-01" || dates["obl-ppr"] != "2026-07-01" {
		t.Fatalf("dates=%#v, want ET+TT and PPR both on 2026-07-01", dates)
	}
	if dates["obl-fmd"] == "2026-07-01" {
		t.Fatalf("dates=%#v, want FMD overflowed off 2026-07-01", dates)
	}
}

// fakePagedDueRepo models the PRODUCTION unbatched-due read paths for the tie preflight: the plain
// ListUnbatchedDueForVersion respects LIMIT (returns only the first page, in canonical ORDER BY
// order), while ListUnbatchedDueForVersionKeyset drains EVERY page keyset by obligation_id. The
// stock in-memory fakeSweepRepo.ListUnbatchedDueForVersion ignores the limit and returns all rows,
// which would hide the beyond-first-page bug, so this fake overrides it to page like Postgres.
type fakePagedDueRepo struct {
	*fakeSweepRepo
	dueByVersion map[string][]domain.UnbatchedDue // stored in canonical ORDER BY order per version
}

func (f *fakePagedDueRepo) ListUnbatchedDueForVersion(_ context.Context, _, versionID string, _ time.Time, limit int32) ([]domain.UnbatchedDue, error) {
	rows := f.dueByVersion[versionID]
	if limit > 0 && int(limit) < len(rows) {
		return append([]domain.UnbatchedDue(nil), rows[:limit]...), nil
	}
	return append([]domain.UnbatchedDue(nil), rows...), nil
}

func (f *fakePagedDueRepo) ListUnbatchedDueForVersionKeyset(_ context.Context, _, versionID string, _ time.Time, after *domain.UnbatchedDueCursor, limit int32) ([]domain.UnbatchedDue, error) {
	rows := append([]domain.UnbatchedDue(nil), f.dueByVersion[versionID]...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ObligationID < rows[j].ObligationID })
	out := make([]domain.UnbatchedDue, 0, len(rows))
	for _, r := range rows {
		if after != nil && r.ObligationID <= after.ObligationID {
			continue
		}
		out = append(out, r)
		if limit > 0 && int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

// TestPreflightDetectsTieBeyondFirstPage is the FINDING 3 (P1) regression: a same-priority,
// cross-vaccine shot-cap tie among a version's due rows that lie BEYOND the first page must be
// surfaced by the write-free preflight. One animal (goat-1) is due three DIFFERENT same-priority
// vaccines the same day (MaxShotsPerAnimalPerDrive=2); the third, conflicting obligation
// (rule-c/obl-c) only appears on the second keyset page. The preflight must drain all pages,
// detect the tie, and touch ZERO write paths.
//
// FAILING-FIRST: on origin/main PreflightVisitShotCapTies read only ListUnbatchedDueForVersion's
// FIRST page (s.page rows). With s.page=2 that returned rule-a and rule-b only -- which legitimately
// fill goat-1's two slots -- so the preflight saw no tie and returned nil, letting the real sweep
// abort mid-run when it later hit rule-c. This fix drains ListUnbatchedDueForVersionKeyset to
// exhaustion, so rule-c is seen and the tie is caught up front.
func TestPreflightDetectsTieBeyondFirstPage(t *testing.T) {
	due := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	// Three obligations for goat-1, same shed/date, three distinct rules -> three distinct
	// same-priority vaccines. Stored in canonical (rule_id) order so the first-page LIMIT returns
	// rule-a, rule-b and leaves rule-c on page 2.
	v1 := []domain.UnbatchedDue{
		{ObligationID: "obl-a", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
		{ObligationID: "obl-b", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
		{ObligationID: "obl-c", RuleID: "rule-c", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
	}
	repo := &fakePagedDueRepo{
		fakeSweepRepo: &fakeSweepRepo{attachAll: true},
		dueByVersion:  map[string][]domain.UnbatchedDue{"v-1": v1, "v-2": nil},
	}
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)
	svc.page = 2 // force rule-c beyond the first page

	planner := domain.DrivePlannerSettings{Enabled: true, MaxShotsPerAnimalPerDrive: 2}
	// All three rules map to distinct vaccine codes at EQUAL priority -> a genuine unresolved tie.
	ruleIDs := map[string]RuleVaccineIdentity{
		"rule-a": {VaccineCode: "Vaccine A", VaccinePriority: 50},
		"rule-b": {VaccineCode: "Vaccine B", VaccinePriority: 50},
		"rule-c": {VaccineCode: "Vaccine C", VaccinePriority: 50},
	}
	plans := []SweepVersionPriority{
		{VersionID: "v-1", Config: SweepConfig{VaccineCode: "Vaccine A", DrivePlanner: planner, RuleVaccineIDs: ruleIDs}},
		{VersionID: "v-2", Config: SweepConfig{VaccineCode: "Vaccine Z", DrivePlanner: planner}},
	}

	err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due)
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError from a tie beyond the first page", err)
	}
	if tieErr.TargetID != "goat-1" {
		t.Fatalf("tie target = %q, want goat-1", tieErr.TargetID)
	}
	if repo.createBatchCalls != 0 || tasks.calls != 0 || reserver.calls != 0 {
		t.Fatalf("preflight side effects: batches=%d tasks=%d stock=%d, want 0/0/0", repo.createBatchCalls, tasks.calls, reserver.calls)
	}
}
