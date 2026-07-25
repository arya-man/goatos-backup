package app

import (
	"testing"

	"github.com/vgoats/goatos/backend/internal/obligation/domain"
)

// TestApplyCapacityShotCapOverride_NilLeavesDSLValue proves a nil/unset tenant capacity-config
// override (the common case: no admin override authored) leaves the DSL-resolved planner value
// untouched.
func TestApplyCapacityShotCapOverride_NilLeavesDSLValue(t *testing.T) {
	planner := domain.DrivePlannerSettings{MaxShotsPerAnimalPerDrive: 4}
	got := ApplyCapacityShotCapOverride(planner, nil)
	if got.MaxShotsPerAnimalPerDrive != 4 {
		t.Fatalf("MaxShotsPerAnimalPerDrive = %d want 4 (DSL value preserved)", got.MaxShotsPerAnimalPerDrive)
	}
}

// TestApplyCapacityShotCapOverride_SetOverridesDSLValue proves a non-nil admin override (from
// vaccination_capacity_config.max_shots_per_animal_per_drive) wins over the DSL-resolved value.
func TestApplyCapacityShotCapOverride_SetOverridesDSLValue(t *testing.T) {
	planner := domain.DrivePlannerSettings{MaxShotsPerAnimalPerDrive: 4}
	override := int32(1)
	got := ApplyCapacityShotCapOverride(planner, &override)
	if got.MaxShotsPerAnimalPerDrive != 1 {
		t.Fatalf("MaxShotsPerAnimalPerDrive = %d want 1 (admin override wins)", got.MaxShotsPerAnimalPerDrive)
	}
}

// TestApplyCapacityShotCapOverride_ZeroOrNegativeIgnored proves an invalid (<=0) override value is
// ignored rather than disabling the shot cap entirely -- domain.CapacityConfig.Validate already
// rejects <1 at the write path, but this is defence in depth against a bad value slipping through.
func TestApplyCapacityShotCapOverride_ZeroOrNegativeIgnored(t *testing.T) {
	planner := domain.DrivePlannerSettings{MaxShotsPerAnimalPerDrive: 4}
	zero := int32(0)
	got := ApplyCapacityShotCapOverride(planner, &zero)
	if got.MaxShotsPerAnimalPerDrive != 4 {
		t.Fatalf("MaxShotsPerAnimalPerDrive = %d want 4 (zero override ignored)", got.MaxShotsPerAnimalPerDrive)
	}
}
