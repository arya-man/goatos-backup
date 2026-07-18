package app

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestPreflightDetectsSingleVersionMatrixTie is the RV-01 guard: ONE plan (protocol version) that
// carries three per-rule vaccines at equal priority must still surface the shot-cap tie for a
// two-shot visit -- the old len(plans) < 2 early-out equated plan count with vaccine count and let a
// single-version matrix tie slip past the gate, partial-committing at real-sweep time.
func TestPreflightDetectsSingleVersionMatrixTie(t *testing.T) {
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	repo := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-matrix": {
				{ObligationID: "obl-a", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-b", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-c", RuleID: "rule-c", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
			},
		},
		attachAll: true,
	}
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)
	// One plan, three rule-vaccines at the SAME priority, two-shot cap: the third rule ties.
	cfg := SweepConfig{
		VaccineCode:  "Matrix Version",
		DrivePlanner: domain.DrivePlannerSettings{MaxShotsPerAnimalPerDrive: 2},
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-a": {VaccineCode: "FMD", VaccinePriority: 5},
			"rule-b": {VaccineCode: "PPR", VaccinePriority: 5},
			"rule-c": {VaccineCode: "HS", VaccinePriority: 5},
		},
	}
	plans := []SweepVersionPriority{{VersionID: "v-matrix", Config: cfg}}

	err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due, time.Time{})
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError from a single-version matrix tie", err)
	}
	if tieErr.TargetID != "goat-1" {
		t.Fatalf("tie target = %q, want goat-1", tieErr.TargetID)
	}
	if repo.createBatchCalls != 0 || tasks.calls != 0 || reserver.calls != 0 {
		t.Fatalf("side effects: batches=%d tasks=%d stock=%d, want 0/0/0", repo.createBatchCalls, tasks.calls, reserver.calls)
	}
}

// keysetFakeRepo makes a fakeSweepRepo satisfy unbatchedDueKeysetLister so the preflight pages ALL
// candidates instead of only the first. It keyset-pages the version's rows off the same ORDER-BY
// tuple the real query uses. Isolated to this file so the plain fakeSweepRepo keeps exercising the
// first-page fallback in every other test.
type keysetFakeRepo struct {
	*fakeSweepRepo
}

func lessUnbatched(a, b domain.UnbatchedDue) bool {
	for _, cmp := range []struct{ x, y string }{
		{a.ScopeType, b.ScopeType},
		{a.ScopeID, b.ScopeID},
		{a.RuleID, b.RuleID},
	} {
		if cmp.x != cmp.y {
			return cmp.x < cmp.y
		}
	}
	if !a.DueAt.Equal(b.DueAt) {
		return a.DueAt.Before(b.DueAt)
	}
	return a.ObligationID < b.ObligationID
}

func afterCursor(r domain.UnbatchedDue, c *domain.UnbatchedDueCursor) bool {
	cur := domain.UnbatchedDue{
		ScopeType: c.ScopeType, ScopeID: c.ScopeID, RuleID: c.RuleID,
		DueAt: c.DueAt, ObligationID: c.ObligationID,
	}
	return lessUnbatched(cur, r)
}

func (k *keysetFakeRepo) ListUnbatchedDueForVersionKeyset(_ context.Context, _, versionID string, _ time.Time, after *domain.UnbatchedDueCursor, limit int32) ([]domain.UnbatchedDue, error) {
	all := append([]domain.UnbatchedDue(nil), k.rowsByVersion[versionID]...)
	sort.SliceStable(all, func(i, j int) bool { return lessUnbatched(all[i], all[j]) })
	out := make([]domain.UnbatchedDue, 0, limit)
	for _, r := range all {
		if after != nil && !afterCursor(r, after) {
			continue
		}
		out = append(out, r)
		if int32(len(out)) >= limit {
			break
		}
	}
	return out, nil
}

// TestPreflightDetectsTieBeyondFirstPage is the RV-02 guard: with a one-row page size, a conflicting
// obligation on the THIRD page must still be caught write-free. The first-page-only scan saw only
// obl-a and passed; the keyset scan pages through every candidate and raises the tie before any
// batch/task/reservation is written.
func TestPreflightDetectsTieBeyondFirstPage(t *testing.T) {
	due := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 8, 5, 0, 0, 0, 0, time.UTC)
	base := &fakeSweepRepo{
		rowsByVersion: map[string][]domain.UnbatchedDue{
			"v-matrix": {
				{ObligationID: "obl-a", RuleID: "rule-a", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-b", RuleID: "rule-b", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
				{ObligationID: "obl-c", RuleID: "rule-c", ScopeType: "shed", ScopeID: "shed-1", TargetID: "goat-1", DueAt: due, WindowEnd: &winEnd},
			},
		},
		attachAll: true,
	}
	repo := &keysetFakeRepo{fakeSweepRepo: base}
	tasks := &fakeSweepTaskCreator{id: "task-1"}
	reserver := &fakeSweepStockReserver{}
	svc := NewSweeperService(repo, tasks, reserver)
	svc.page = 1 // force the conflicting obl-c onto a page beyond the first
	cfg := SweepConfig{
		VaccineCode:  "Matrix Version",
		DrivePlanner: domain.DrivePlannerSettings{MaxShotsPerAnimalPerDrive: 2},
		RuleVaccineIDs: map[string]RuleVaccineIdentity{
			"rule-a": {VaccineCode: "FMD", VaccinePriority: 5},
			"rule-b": {VaccineCode: "PPR", VaccinePriority: 5},
			"rule-c": {VaccineCode: "HS", VaccinePriority: 5},
		},
	}
	plans := []SweepVersionPriority{{VersionID: "v-matrix", Config: cfg}}

	err := svc.PreflightVisitShotCapTies(context.Background(), "tenant-1", plans, due, time.Time{})
	var tieErr *ShotCapPriorityTieError
	if err == nil || !errors.As(err, &tieErr) {
		t.Fatalf("err = %v, want *ShotCapPriorityTieError caught on a page beyond the first", err)
	}
	if base.createBatchCalls != 0 || tasks.calls != 0 || reserver.calls != 0 {
		t.Fatalf("side effects: batches=%d tasks=%d stock=%d, want 0/0/0", base.createBatchCalls, tasks.calls, reserver.calls)
	}
}
