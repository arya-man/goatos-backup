package app

import (
	"context"
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

type capacityConfigFakeRepo struct {
	fakeRepo
	upserted     domain.CapacityConfig
	upsertCalled bool
}

func (r *capacityConfigFakeRepo) UpsertCapacityConfig(_ context.Context, _ string, cfg domain.CapacityConfig) (domain.CapacityConfig, error) {
	r.upsertCalled = true
	cfg.RowVersion++
	r.upserted = cfg
	return cfg, nil
}

// TestUpdateCapacityConfig_ValidWriteCallsRepo proves a valid request reaches the repository write
// (the outbox cascade + row_version bump are the repository's job -- see the postgres adapter tests).
func TestUpdateCapacityConfig_ValidWriteCallsRepo(t *testing.T) {
	repo := &capacityConfigFakeRepo{}
	svc := NewService(repo)
	shots := 3
	updated, code, msg, err := svc.UpdateCapacityConfig(context.Background(), "tenant-1", domain.CapacityConfig{
		MaxPerDay:                 150,
		CapacityScope:             "tenant",
		MaxBufferDays:             7,
		OverflowPolicy:            "split_within_safe_window_last_safe_may_exceed_cap",
		RowVersion:                1,
		MaxShotsPerAnimalPerDrive: &shots,
	})
	if err != nil || code != "" {
		t.Fatalf("unexpected reject: code=%s msg=%s err=%v", code, msg, err)
	}
	if !repo.upsertCalled {
		t.Fatal("expected repo.UpsertCapacityConfig to be called")
	}
	if updated.RowVersion != 2 {
		t.Fatalf("RowVersion = %d want 2", updated.RowVersion)
	}
}

// TestUpdateCapacityConfig_RejectsMaxPerDayOutOfRange proves out-of-range maxPerDay is rejected with a
// 400-shaped (code, message) pair and never reaches the repository write (validate-or-reject, never
// silently clamped).
func TestUpdateCapacityConfig_RejectsMaxPerDayOutOfRange(t *testing.T) {
	repo := &capacityConfigFakeRepo{}
	svc := NewService(repo)

	_, code, _, err := svc.UpdateCapacityConfig(context.Background(), "tenant-1", domain.CapacityConfig{
		MaxPerDay:      0,
		CapacityScope:  "tenant",
		MaxBufferDays:  7,
		OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap",
		RowVersion:     1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "invalid_max_per_day" {
		t.Fatalf("code = %s want invalid_max_per_day", code)
	}
	if repo.upsertCalled {
		t.Fatal("repo write must not be reached on validation failure")
	}

	_, code, _, err = svc.UpdateCapacityConfig(context.Background(), "tenant-1", domain.CapacityConfig{
		MaxPerDay:      100001,
		CapacityScope:  "tenant",
		MaxBufferDays:  7,
		OverflowPolicy: "split_within_safe_window_last_safe_may_exceed_cap",
		RowVersion:     1,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "invalid_max_per_day" {
		t.Fatalf("code = %s want invalid_max_per_day", code)
	}
}

// TestUpdateCapacityConfig_RejectsMaxShotsOutOfRange proves an out-of-range (but present)
// maxShotsPerAnimalPerDrive override is rejected -- never silently defaulted.
func TestUpdateCapacityConfig_RejectsMaxShotsOutOfRange(t *testing.T) {
	repo := &capacityConfigFakeRepo{}
	svc := NewService(repo)
	zero := 0
	_, code, _, err := svc.UpdateCapacityConfig(context.Background(), "tenant-1", domain.CapacityConfig{
		MaxPerDay:                 150,
		CapacityScope:             "tenant",
		MaxBufferDays:             7,
		OverflowPolicy:            "split_within_safe_window_last_safe_may_exceed_cap",
		RowVersion:                1,
		MaxShotsPerAnimalPerDrive: &zero,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "invalid_max_shots_per_animal_per_drive" {
		t.Fatalf("code = %s want invalid_max_shots_per_animal_per_drive", code)
	}
	if repo.upsertCalled {
		t.Fatal("repo write must not be reached on validation failure")
	}
}

func (r *capacityConfigFakeRepo) ListAlerts(
	_ context.Context, _, _ string, _ bool, _ []string, _ string, _ int,
) (domain.AlertPage, error) {
	return domain.AlertPage{Items: []domain.Alert{}}, nil
}
