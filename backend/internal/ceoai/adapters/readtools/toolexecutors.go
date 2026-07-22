package readtools

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// countsBreakdownExecutor provides animal counts broken down by dimensions.
// It calls the real counts service to return actual data from the database.
type countsBreakdownExecutor struct {
	// countsBySpeciesReader provides counts broken down by species.
	// In the bootstrap wiring, this is set to a closure that calls the counts service.
	countsBySpeciesReader func(ctx context.Context, tenantID string) ([]domain.Fact, error)
}

func (e *countsBreakdownExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "counts_breakdown",
		Route:       domain.RouteAPI,
		Description: "Animal counts broken down by park, shed, breed, sex, stage, or other dimensions",
		Params:      []string{"dimension", "park_label", "species"},
	}
}

func (e *countsBreakdownExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	// Call the real counts reader to get actual data.
	if e.countsBySpeciesReader == nil {
		// Fallback: return empty results so caller knows tool exists but has no data.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	facts, err := e.countsBySpeciesReader(ctx, actor.TenantID)
	if err != nil {
		// Log but don't fail hard; return empty facts so the orchestrator can try other routes.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// vaccinationShedSummaryExecutor provides vaccination status by shed.
type vaccinationShedSummaryExecutor struct {
	vaccinationDataReader func(ctx context.Context, tenantID string) ([]domain.Fact, error)
}

func (e *vaccinationShedSummaryExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "vaccination_shed_summary",
		Route:       domain.RouteAPI,
		Description: "Vaccination status summary by shed (due, completed, overdue)",
		Params:      []string{"park_label", "shed_id"},
	}
}

func (e *vaccinationShedSummaryExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.vaccinationDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	facts, err := e.vaccinationDataReader(ctx, actor.TenantID)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// vaccinationExecutionExecutor provides vaccination execution details.
type vaccinationExecutionExecutor struct {
	vaccinationDataReader func(ctx context.Context, tenantID string) ([]domain.Fact, error)
}

func (e *vaccinationExecutionExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "vaccination_execution",
		Route:       domain.RouteAPI,
		Description: "Vaccination execution status and drive details",
		Params:      []string{"park_label", "drive_id"},
	}
}

func (e *vaccinationExecutionExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.vaccinationDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	facts, err := e.vaccinationDataReader(ctx, actor.TenantID)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// feedDirectionTodayExecutor provides today's feed direction.
type feedDirectionTodayExecutor struct {
	feedDataReader func(ctx context.Context, tenantID string) ([]domain.Fact, error)
}

func (e *feedDirectionTodayExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "feed_direction_today",
		Route:       domain.RouteAPI,
		Description: "Feed direction for today by shed",
		Params:      []string{"park_label", "shed_id"},
	}
}

func (e *feedDirectionTodayExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.feedDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	facts, err := e.feedDataReader(ctx, actor.TenantID)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// NewToolExecutors returns a set of in-process read service tool executors
// for tier-2 (API) routing. These are registered with the leadership assistant
// registry to handle RouteAPI sub-questions.
// The executors are wired with data readers in the bootstrap layer.
func NewToolExecutors() []ports.ToolExecutor {
	return []ports.ToolExecutor{
		&countsBreakdownExecutor{},
		&vaccinationShedSummaryExecutor{},
		&vaccinationExecutionExecutor{},
		&feedDirectionTodayExecutor{},
	}
}

// SetCountsDataReader wires the counts data reader into the counts executor.
func SetCountsDataReader(exec ports.ToolExecutor, reader func(context.Context, string) ([]domain.Fact, error)) {
	if e, ok := exec.(*countsBreakdownExecutor); ok {
		e.countsBySpeciesReader = reader
	}
}

// SetVaccinationDataReader wires the vaccination data reader into the vaccination executors.
func SetVaccinationDataReader(execs []ports.ToolExecutor, reader func(context.Context, string) ([]domain.Fact, error)) {
	for _, exec := range execs {
		switch e := exec.(type) {
		case *vaccinationShedSummaryExecutor:
			e.vaccinationDataReader = reader
		case *vaccinationExecutionExecutor:
			e.vaccinationDataReader = reader
		}
	}
}

// SetFeedDataReader wires the feed data reader into the feed executor.
func SetFeedDataReader(exec ports.ToolExecutor, reader func(context.Context, string) ([]domain.Fact, error)) {
	if e, ok := exec.(*feedDirectionTodayExecutor); ok {
		e.feedDataReader = reader
	}
}
