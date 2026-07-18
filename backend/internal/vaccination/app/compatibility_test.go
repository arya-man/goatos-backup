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

func TestCompatibilityPolicyUsesApprovedComboSessionCap(t *testing.T) {
	got := genCompatibilityPolicy{MaxVaccinesPerComboSession: 1}.withDefaults().MaxVaccinesPerComboSession
	if got != DefaultMaxVaccinesPerComboSession {
		t.Fatalf("max combo session cap = %d, want approved cap %d", got, DefaultMaxVaccinesPerComboSession)
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
	// BUG #3: with the switch true, this should return 0 (same-day allowed).
	policy := genCompatibilityPolicy{BacterialViralSameDayAllowed: true, LiveToKilledGapDays: 14}
	got := crossVaccineGapDays(immunoLiveViral, immunoKilledBacterial, policy)
	if got != 0 {
		t.Fatalf("live viral+bacterial same day gap = %d, want 0", got)
	}
}

func TestCrossVaccineGapDaysBacterialViralSameDay(t *testing.T) {
	// BUG #3: with the switch true, both directions should return 0 (same-day allowed).
	policy := genCompatibilityPolicy{BacterialViralSameDayAllowed: true}
	if gap := crossVaccineGapDays(immunoKilledBacterial, immunoLiveViral, policy); gap != 0 {
		t.Fatalf("bacterial+live viral same day gap = %d, want 0", gap)
	}
	if gap := crossVaccineGapDays(immunoLiveViral, immunoKilledBacterial, policy); gap != 0 {
		t.Fatalf("live viral+bacterial same day gap = %d, want 0", gap)
	}
}

func TestCrossVaccineGapDaysLiveViralKilledViralSameDay(t *testing.T) {
	// BUG #3: with the switch true, this should return 0 (same-day allowed).
	policy := genCompatibilityPolicy{LiveKilledViralSameDayAllowed: true}
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

// BUG #3 tests: verify that same-day switches actually control whether same-day is allowed.
func TestCrossVaccineGapDaysBacterialViralSameDaySwitch(t *testing.T) {
	tests := []struct {
		name    string
		prior   vaccineImmunoClass
		next    vaccineImmunoClass
		policy  genCompatibilityPolicy
		wantGap int32
	}{
		{
			name:    "bacterial→viral same-day allowed=true returns 0",
			prior:   immunoKilledBacterial,
			next:    immunoLiveViral,
			policy:  genCompatibilityPolicy{BacterialViralSameDayAllowed: true, LiveToKilledGapDays: 14},
			wantGap: 0,
		},
		{
			name:    "bacterial→viral same-day allowed=false returns live-to-killed gap",
			prior:   immunoLiveViral,
			next:    immunoKilledBacterial,
			policy:  genCompatibilityPolicy{BacterialViralSameDayAllowed: false, LiveToKilledGapDays: 14},
			wantGap: 14,
		},
		{
			name:    "viral→bacterial same-day allowed=true returns 0",
			prior:   immunoKilledViral,
			next:    immunoKilledBacterial,
			policy:  genCompatibilityPolicy{BacterialViralSameDayAllowed: true, KilledToKilledGapDays: 14},
			wantGap: 0,
		},
		{
			name:    "viral→bacterial same-day allowed=false returns killed-to-killed gap",
			prior:   immunoKilledViral,
			next:    immunoKilledBacterial,
			policy:  genCompatibilityPolicy{BacterialViralSameDayAllowed: false, KilledToKilledGapDays: 14},
			wantGap: 14,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := crossVaccineGapDays(tt.prior, tt.next, tt.policy)
			if got != tt.wantGap {
				t.Errorf("gap = %d, want %d", got, tt.wantGap)
			}
		})
	}
}

func TestCrossVaccineGapDaysLiveKilledViralSameDaySwitch(t *testing.T) {
	tests := []struct {
		name    string
		prior   vaccineImmunoClass
		next    vaccineImmunoClass
		policy  genCompatibilityPolicy
		wantGap int32
	}{
		{
			name:    "live viral→killed viral same-day allowed=true returns 0",
			prior:   immunoLiveViral,
			next:    immunoKilledViral,
			policy:  genCompatibilityPolicy{LiveKilledViralSameDayAllowed: true},
			wantGap: 0,
		},
		{
			name:    "live viral→killed viral same-day allowed=false returns live-to-killed gap",
			prior:   immunoLiveViral,
			next:    immunoKilledViral,
			policy:  genCompatibilityPolicy{LiveKilledViralSameDayAllowed: false, LiveToKilledGapDays: 14},
			wantGap: 14,
		},
		{
			name:    "killed viral→live viral same-day allowed=true returns 0",
			prior:   immunoKilledViral,
			next:    immunoLiveViral,
			policy:  genCompatibilityPolicy{LiveKilledViralSameDayAllowed: true},
			wantGap: 0,
		},
		{
			name:    "killed viral→live viral same-day allowed=false returns live-to-killed gap (symmetric)",
			prior:   immunoKilledViral,
			next:    immunoLiveViral,
			policy:  genCompatibilityPolicy{LiveKilledViralSameDayAllowed: false, LiveToKilledGapDays: 14, LiveToLiveGapDays: 28},
			wantGap: 14, // killed→live is symmetric with live→killed
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := crossVaccineGapDays(tt.prior, tt.next, tt.policy)
			if got != tt.wantGap {
				t.Errorf("gap = %d, want %d", got, tt.wantGap)
			}
		})
	}
}

// BUG #4 test: verify that unknown immunoclass fails-closed (returns strictest gap, not 0).
func TestCrossVaccineGapDaysUnknownClassFailsClosed(t *testing.T) {
	policy := genCompatibilityPolicy{LiveToLiveGapDays: 28}
	tests := []struct {
		name    string
		prior   vaccineImmunoClass
		next    vaccineImmunoClass
		wantGap int32
	}{
		{
			name:    "prior unknown returns strictest gap",
			prior:   immunoUnknown,
			next:    immunoLiveViral,
			wantGap: 28,
		},
		{
			name:    "next unknown returns strictest gap",
			prior:   immunoLiveViral,
			next:    immunoUnknown,
			wantGap: 28,
		},
		{
			name:    "both unknown returns strictest gap",
			prior:   immunoUnknown,
			next:    immunoUnknown,
			wantGap: 28,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := crossVaccineGapDays(tt.prior, tt.next, policy)
			if got != tt.wantGap {
				t.Errorf("gap = %d, want %d (should be strictest gap, not 0)", got, tt.wantGap)
			}
		})
	}
}

// R2-02: Verify that cross-class gaps are symmetric (prior/next swapped with both switches false).
func TestCrossVaccineGapDaysSymmetricLiveKilled(t *testing.T) {
	tests := []struct {
		name   string
		policy genCompatibilityPolicy
	}{
		{
			name:   "live-to-killed=14, switches false",
			policy: genCompatibilityPolicy{LiveToKilledGapDays: 14, BacterialViralSameDayAllowed: false, LiveKilledViralSameDayAllowed: false},
		},
		{
			name:   "live-to-killed=21, switches false",
			policy: genCompatibilityPolicy{LiveToKilledGapDays: 21, BacterialViralSameDayAllowed: false, LiveKilledViralSameDayAllowed: false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Live viral → Killed viral should return LiveToKilledGapDays
			liveToKilled := crossVaccineGapDays(immunoLiveViral, immunoKilledViral, tt.policy)
			// Killed viral → Live viral should return the same (symmetric)
			killedToLive := crossVaccineGapDays(immunoKilledViral, immunoLiveViral, tt.policy)
			if liveToKilled != killedToLive {
				t.Errorf("live→killed gap = %d, killed→live gap = %d; gaps should be symmetric", liveToKilled, killedToLive)
			}
			if liveToKilled != tt.policy.LiveToKilledGapDays {
				t.Errorf("live→killed gap = %d, want %d", liveToKilled, tt.policy.LiveToKilledGapDays)
			}
		})
	}
}

// R2-02: Verify that killed→live gap is correctly applied when same-day switch is false.
func TestCrossVaccineGapDaysKilledToLiveSwitchFalse(t *testing.T) {
	policy := genCompatibilityPolicy{
		LiveToKilledGapDays:           14,
		BacterialViralSameDayAllowed:  false,
		LiveKilledViralSameDayAllowed: false,
	}
	// When LiveKilledViralSameDayAllowed=false, killed viral → live viral must return the gap
	got := crossVaccineGapDays(immunoKilledViral, immunoLiveViral, policy)
	if got != 14 {
		t.Errorf("killed viral→live viral gap (switch false) = %d, want 14", got)
	}
}
