package app

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestCrossVaccineGapDaysLiveToLive(t *testing.T) {
	policy := genCompatibilityPolicy{LiveToLiveGapDays: 28}
	got := crossVaccineGapDays(immunoLiveViral, immunoLiveViral, policy)
	if got != 28 {
		t.Fatalf("live→live gap = %d, want 28", got)
	}
}

func TestCrossVaccineGapDaysLiveToKilledDefaultGap(t *testing.T) {
	policy := genCompatibilityPolicy{LiveToKilledGapDays: 14}
	got := crossVaccineGapDays(immunoLive, immunoKilled, policy)
	if got != 14 {
		t.Fatalf("live→killed gap = %d, want 14", got)
	}
}

func TestCrossVaccineGapDaysLiveViralToKilledBacterialSameDayAllowed(t *testing.T) {
	policy := genCompatibilityPolicy{LiveToKilledGapDays: 14}
	got := crossVaccineGapDays(immunoLiveViral, immunoKilledBacterial, policy)
	if got != 0 {
		t.Fatalf("live viral+bacterial same day gap = %d, want 0", got)
	}
}

func TestCrossVaccineGapDaysBacterialViralSameDay(t *testing.T) {
	policy := genCompatibilityPolicy{}
	if gap := crossVaccineGapDays(immunoKilledBacterial, immunoLiveViral, policy); gap != 0 {
		t.Fatalf("bacterial+live viral same day gap = %d, want 0", gap)
	}
	if gap := crossVaccineGapDays(immunoLiveViral, immunoKilledBacterial, policy); gap != 0 {
		t.Fatalf("live viral+bacterial same day gap = %d, want 0", gap)
	}
}

func TestCrossVaccineGapDaysLiveViralKilledViralSameDay(t *testing.T) {
	policy := genCompatibilityPolicy{}
	if gap := crossVaccineGapDays(immunoLiveViral, immunoKilledViral, policy); gap != 0 {
		t.Fatalf("live viral+killed viral same day gap = %d, want 0", gap)
	}
}

func TestApplyCrossVaccineGapFloorDelaysGoatPoxAfterPPR(t *testing.T) {
	pprAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	last := &domain.RecentVaccineAdministration{
		AdministeredAt: pprAt,
		VaccineCode:    "PPR",
		VaccineType:    "live",
		PathogenClass:  "viral",
	}
	next := vaccineProfile{
		Code:          "Goat Pox",
		Type:          "live",
		PathogenClass: "viral",
		Class:         immunoLiveViral,
	}
	baseDue := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC) // 1 week after PPR — too soon
	got := applyCrossVaccineGapFloor(baseDue, last, next, genCompatibilityPolicy{LiveToLiveGapDays: 28})
	want := businessDayStart(pprAt).AddDate(0, 0, 28) // 28 India business days after PPR
	if !got.Equal(want) {
		t.Fatalf("due = %s, want %s (4-week live→live gap)", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func TestApplyCrossVaccineGapFloorSkipsSameVaccineCode(t *testing.T) {
	last := &domain.RecentVaccineAdministration{
		AdministeredAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		VaccineCode:    "ET+TT",
		VaccineType:    "killed",
		PathogenClass:  "bacterial",
	}
	next := vaccineProfile{Code: "ET+TT", Type: "killed", PathogenClass: "bacterial", Class: immunoKilledBacterial}
	baseDue := time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)
	got := applyCrossVaccineGapFloor(baseDue, last, next, genCompatibilityPolicy{})
	if !got.Equal(baseDue) {
		t.Fatalf("same vaccine code should not apply cross gap, got %s", got)
	}
}
