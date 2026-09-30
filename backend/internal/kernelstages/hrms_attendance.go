package kernelstages

import (
	"context"
	"errors"
	"fmt"

	hrmssoppg "github.com/vgoats/goatos/backend/internal/hrmssop/adapters/postgres"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// HRMSAttendanceStage is the automatic clock-in check (maintainer decisions 2026-09-30): every
// tick it raises ONE waiting violation per person, day and kind for anyone mapped to a timed shift
// who clocked in past the HRMS SOP's grace ("late") or had not clocked in by the shift's end ("did
// not clock in"), on today or yesterday (IST), skipping days covered by leave the person applied
// for. HR keeps or closes each on People / HRMS > Violations. The natural key makes an overlapping
// tick insert nothing twice; one tick is one bounded read and one set-based insert.
type HRMSAttendanceStage struct {
	service  *workforceapp.DisciplineService
	tenantID string
}

func NewHRMSAttendanceStage(deps Deps, tenantID string) *HRMSAttendanceStage {
	repo := workforcepg.NewRepository(deps.Pool, deps.PgCfg.QueryTimeout)
	return &HRMSAttendanceStage{
		service:  workforceapp.NewDisciplineService(repo, repo, hrmssoppg.NewSource(deps.Pool, deps.PgCfg.QueryTimeout), deps.Logger),
		tenantID: tenantID,
	}
}

func (s *HRMSAttendanceStage) Name() string { return "hrms-attendance" }

func (s *HRMSAttendanceStage) Run(ctx context.Context) error {
	if s.tenantID == "" {
		return errors.New("hrms attendance: tenant id is required")
	}
	if _, err := s.service.RaiseAttendanceViolations(ctx, s.tenantID); err != nil {
		return fmt.Errorf("hrms attendance: %w", err)
	}
	return nil
}
