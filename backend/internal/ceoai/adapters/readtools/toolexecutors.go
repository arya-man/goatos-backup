package readtools

import (
	"context"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// countsBreakdownExecutor provides animal counts broken down by dimensions.
type countsBreakdownExecutor struct {
	// In production, this would call countsService.GetBreakdown or similar.
	// For now, we return a placeholder that indicates the tool is available.
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
	// Degrade to SQL fallback or return informational result.
	// The real implementation would call countsService.GetBreakdown(ctx, actor.TenantID, params).
	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts: []domain.Fact{
			{Label: "status", Value: "Tool available; use Cube active_animals metric or SQL fallback for detailed breakdown"},
		},
	}, nil
}

// vaccinationSheddSummaryExecutor provides vaccination status by shed.
type vaccinationShedSummaryExecutor struct{}

func (e *vaccinationShedSummaryExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "vaccination_shed_summary",
		Route:       domain.RouteAPI,
		Description: "Vaccination status summary by shed (due, completed, overdue)",
		Params:      []string{"park_label", "shed_id"},
	}
}

func (e *vaccinationShedSummaryExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts: []domain.Fact{
			{Label: "status", Value: "Tool available; use Cube vaccination_due/overdue metrics or SQL fallback for detailed breakdown"},
		},
	}, nil
}

// vaccinationExecutionExecutor provides vaccination execution details.
type vaccinationExecutionExecutor struct{}

func (e *vaccinationExecutionExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "vaccination_execution",
		Route:       domain.RouteAPI,
		Description: "Vaccination execution status and drive details",
		Params:      []string{"park_label", "drive_id"},
	}
}

func (e *vaccinationExecutionExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts: []domain.Fact{
			{Label: "status", Value: "Tool available; use SQL fallback for execution details"},
		},
	}, nil
}

// feedDirectionTodayExecutor provides today's feed direction.
type feedDirectionTodayExecutor struct{}

func (e *feedDirectionTodayExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "feed_direction_today",
		Route:       domain.RouteAPI,
		Description: "Feed direction for today by shed",
		Params:      []string{"park_label", "shed_id"},
	}
}

func (e *feedDirectionTodayExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts: []domain.Fact{
			{Label: "status", Value: "Tool available; use SQL fallback for feed direction details"},
		},
	}, nil
}

// NewToolExecutors returns a set of in-process read service tool executors
// for tier-2 (API) routing. These are registered with the leadership assistant
// registry to handle RouteAPI sub-questions.
func NewToolExecutors() []ports.ToolExecutor {
	return []ports.ToolExecutor{
		&countsBreakdownExecutor{},
		&vaccinationShedSummaryExecutor{},
		&vaccinationExecutionExecutor{},
		&feedDirectionTodayExecutor{},
	}
}
