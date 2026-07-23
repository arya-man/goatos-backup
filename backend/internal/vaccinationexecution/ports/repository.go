// Package ports defines vaccination-execution repository boundaries.
package ports

import (
	"context"
	"errors"

	"github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
)

// ErrOperatorAssignmentConfigConflict is the shared optimistic-concurrency sentinel for
// UpsertOperatorAssignmentConfig: a stale RowVersion never silently clobbers a concurrent admin write.
// Defined here (not in the postgres adapter) so both the postgres implementation and the app-layer
// service can errors.Is against one stable value without an adapter-to-adapter import.
var ErrOperatorAssignmentConfigConflict = errors.New("vaccination execution: operator assignment config: row version conflict")

type Repository interface {
	ListVaccinationExecution(ctx context.Context, q domain.ExecutionQuery) ([]domain.ExecutionProjection, error)
	// ListVaccinationExecutionPage returns one stable keyset page of execution projections plus the
	// pre-cursor window total, so the request path never fetches the whole server-filtered set.
	ListVaccinationExecutionPage(ctx context.Context, q domain.ExecutionQuery) (domain.ExecutionProjectionPage, error)
	VaccinationOperations(ctx context.Context, q domain.OperationsQuery) ([]domain.OperationsRow, error)
	// VaccinationSchedule returns the canonical Full Schedule month/window rows.
	VaccinationSchedule(ctx context.Context, q domain.ScheduleQuery) ([]domain.OperationsRow, error)
	// DriveAssignments returns persisted operator-cap drive assignments for the selected month.
	DriveAssignments(ctx context.Context, q domain.DriveAssignmentQuery) ([]domain.DriveAssignmentRow, error)
	// ScanRoster returns one keyset page of per-animal scan obligations for the mobile scan screen
	// (Rows + NextCursor); the shed's completion counts are computed server-side, not by loading the
	// full roster on the device.
	ScanRoster(ctx context.Context, q domain.ScanRosterQuery) (domain.ScanRosterResult, error)
	// TaskOptionValues returns the authored option sources (vaccine lots FEFO-ranked, route/site) for a
	// SOP task's capture form, so the device never enumerates inventory to populate a picker.
	TaskOptionValues(ctx context.Context, tenantID, taskID string) (domain.TaskOptionValuesResponse, error)
	// VaccinationGaps returns the bounded, keyset-paginated PER-ANIMAL exclusion rows for the mobile data
	// gaps overlay. Every row is one goat; there is no by-reason aggregate in the contract.
	VaccinationGaps(ctx context.Context, q domain.GapsQuery) ([]domain.GapProjectionRow, error)
	// ShedSummary returns the filtered, offset-paginated shed-wise rollup rows with ANIMAL-LEVEL Due
	// counts and a window total for pagination. Manager/Backup are attached by the service via the
	// ShedOwnershipReader port, not by this query. Served directly from canonical tables (5k-50k
	// envelope, docs/decisions/operational-kernel-5k-50k-scale-envelope.md); the prior projection read
	// model was dropped in migrations 000187/000188.
	ShedSummary(ctx context.Context, q domain.ShedSummaryQuery) ([]domain.ShedSummaryProjection, error)
	// ShedAnimals returns the shed's alive animals (Display ID + the two tag identities + an animal-level
	// status), keyset-paginated by goat_id, for the shed-detail roster.
	ShedAnimals(ctx context.Context, q domain.ShedAnimalQuery) ([]domain.ShedAnimalRow, error)
	// CapacityConfig returns the tenant's daily operator animal cap config (falls back to the code
	// default when no row is authored). Drives session-splitting in ShedSummary and the shed-detail plan.
	CapacityConfig(ctx context.Context, tenantID string) (domain.CapacityConfig, error)
	// OperatorAssignmentConfig returns the park's N-active-operators-per-day + default-operator config
	// (Phase 1 config-only; see domain.OperatorAssignmentConfig doc). Returns found=false when no row is
	// authored yet (never a silent default -- the caller must reject reads that require one).
	OperatorAssignmentConfig(ctx context.Context, tenantID, parkID string) (cfg domain.OperatorAssignmentConfig, found bool, err error)
	// OperatorShifts returns the authored shift rows for every operator with a shift-config row in this
	// park, ordered by shift label then operator id.
	OperatorShifts(ctx context.Context, tenantID, parkID string) ([]domain.OperatorShift, error)
	// UpsertOperatorAssignmentConfig idempotently writes the park's assignment config. When rowVersion is
	// 0 the row must not already exist (first write); otherwise rowVersion must match the current stored
	// value or ErrOperatorAssignmentConfigConflict is returned.
	UpsertOperatorAssignmentConfig(ctx context.Context, tenantID string, cfg domain.OperatorAssignmentConfig) (domain.OperatorAssignmentConfig, error)
}
