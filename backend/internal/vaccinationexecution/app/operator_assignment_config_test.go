package app

import (
	"context"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// operatorConfigFakeRepo is a small stateful fake for the operator-assignment config CRUD tests
// (embeds fakeRepo so it satisfies ports.Repository for every other method).
type operatorConfigFakeRepo struct {
	fakeRepo
	shifts   []domain.OperatorShift
	cfg      domain.OperatorAssignmentConfig
	cfgFound bool
}

func (r *operatorConfigFakeRepo) OperatorShifts(_ context.Context, _, _ string) ([]domain.OperatorShift, error) {
	return r.shifts, nil
}

func (r *operatorConfigFakeRepo) OperatorAssignmentConfig(_ context.Context, _, _ string) (domain.OperatorAssignmentConfig, bool, error) {
	return r.cfg, r.cfgFound, nil
}

func (r *operatorConfigFakeRepo) UpsertOperatorAssignmentConfig(_ context.Context, _ string, cfg domain.OperatorAssignmentConfig) (domain.OperatorAssignmentConfig, error) {
	if r.cfgFound && cfg.RowVersion != r.cfg.RowVersion {
		return domain.OperatorAssignmentConfig{}, errors.New("conflict")
	}
	cfg.RowVersion = r.cfg.RowVersion + 1
	r.cfg = cfg
	r.cfgFound = true
	return cfg, nil
}

func cptOperatorShifts() []domain.OperatorShift {
	return []domain.OperatorShift{
		{OperatorID: "amit", DisplayName: "Amit Kumar", ParkID: "cpt", ShiftLabel: "am", ShiftStartMinute: 7 * 60, ShiftEndMinute: 15 * 60, WeekOffWeekday: "friday"},
		{OperatorID: "sagar", DisplayName: "Sagar Mahoor", ParkID: "cpt", ShiftLabel: "pm", ShiftStartMinute: 15 * 60, ShiftEndMinute: 24 * 60, WeekOffWeekday: "saturday"},
		{OperatorID: "darshan", DisplayName: "Darshan Talwar", ParkID: "cpt", ShiftLabel: "rover", ShiftStartMinute: 8*60 + 30, ShiftEndMinute: 18 * 60, WeekOffWeekday: "sunday"},
	}
}

func TestGetOperatorAssignmentConfig_NotFound(t *testing.T) {
	repo := &operatorConfigFakeRepo{shifts: cptOperatorShifts()}
	svc := NewService(repo)
	_, err := svc.GetOperatorAssignmentConfig(context.Background(), "tenant-1", "cpt")
	if !errors.Is(err, ErrOperatorAssignmentConfigNotFound) {
		t.Fatalf("got err=%v, want ErrOperatorAssignmentConfigNotFound", err)
	}
}

func TestGetOperatorAssignmentConfig_ReadBack(t *testing.T) {
	repo := &operatorConfigFakeRepo{
		shifts:   cptOperatorShifts(),
		cfg:      domain.OperatorAssignmentConfig{ParkID: "cpt", ActiveOperatorsPerDay: 1, DefaultOperatorID: "darshan", RowVersion: 3},
		cfgFound: true,
	}
	svc := NewService(repo)
	view, err := svc.GetOperatorAssignmentConfig(context.Background(), "tenant-1", "cpt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.Config.DefaultOperatorID != "darshan" || view.Config.RowVersion != 3 {
		t.Fatalf("unexpected config: %+v", view.Config)
	}
	if len(view.Shifts) != 3 {
		t.Fatalf("got %d shifts, want 3", len(view.Shifts))
	}
}

func TestUpdateOperatorAssignmentConfig_NUpdatePreservesDefault(t *testing.T) {
	repo := &operatorConfigFakeRepo{
		shifts:   cptOperatorShifts(),
		cfg:      domain.OperatorAssignmentConfig{ParkID: "cpt", ActiveOperatorsPerDay: 1, DefaultOperatorID: "darshan", RowVersion: 1},
		cfgFound: true,
	}
	svc := NewService(repo)
	updated, code, _, err := svc.UpdateOperatorAssignmentConfig(context.Background(), "tenant-1", domain.OperatorAssignmentConfig{
		ParkID: "cpt", ActiveOperatorsPerDay: 1, DefaultOperatorID: "darshan", RowVersion: 1,
	})
	if err != nil || code != "" {
		t.Fatalf("unexpected reject: code=%s err=%v", code, err)
	}
	if updated.DefaultOperatorID != "darshan" {
		t.Fatalf("default not preserved: %+v", updated)
	}
}

func TestUpdateOperatorAssignmentConfig_RejectsNOutOfRange(t *testing.T) {
	repo := &operatorConfigFakeRepo{shifts: cptOperatorShifts()}
	svc := NewService(repo)

	_, code, _, err := svc.UpdateOperatorAssignmentConfig(context.Background(), "tenant-1", domain.OperatorAssignmentConfig{
		ParkID: "cpt", ActiveOperatorsPerDay: 0, DefaultOperatorID: "darshan",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "invalid_active_operators_per_day" {
		t.Fatalf("got code=%s, want invalid_active_operators_per_day", code)
	}

	_, code, _, err = svc.UpdateOperatorAssignmentConfig(context.Background(), "tenant-1", domain.OperatorAssignmentConfig{
		ParkID: "cpt", ActiveOperatorsPerDay: 4, DefaultOperatorID: "darshan",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "invalid_active_operators_per_day" {
		t.Fatalf("got code=%s, want invalid_active_operators_per_day", code)
	}
}

func TestUpdateOperatorAssignmentConfig_RejectsN1WithNullDefault(t *testing.T) {
	repo := &operatorConfigFakeRepo{shifts: cptOperatorShifts()}
	svc := NewService(repo)
	_, code, _, err := svc.UpdateOperatorAssignmentConfig(context.Background(), "tenant-1", domain.OperatorAssignmentConfig{
		ParkID: "cpt", ActiveOperatorsPerDay: 1, DefaultOperatorID: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != "missing_default_operator" {
		t.Fatalf("got code=%s, want missing_default_operator", code)
	}
}
