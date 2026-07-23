package readtools

import (
	"context"
	"fmt"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// scopedReader reads real data for a tool, honoring the sub-question's
// advertised scope params (park_label, species, shed_id, dimension, …) and the
// as-of business date (params["as_of"], injected by the orchestrator from
// Question.AsOf). Passing params through — instead of dropping everything but
// tenantID — is what lets "how many goats in Castro 1" and "counts as of
// yesterday" actually scope/back-date the read (P1-4).
type scopedReader func(ctx context.Context, tenantID string, params map[string]any) ([]domain.Fact, error)

// countsBreakdownExecutor provides animal counts broken down by dimensions.
// It calls the real counts service to return actual data from the database.
type countsBreakdownExecutor struct {
	// countsBySpeciesReader provides counts broken down by species/park/shed/
	// breed/sex/stage. In the bootstrap wiring, this is set to a closure that
	// calls the counts service (ceo_ai.animal_current_scope).
	countsBySpeciesReader scopedReader
}

func (e *countsBreakdownExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "counts_breakdown",
		Route:       domain.RouteAPI,
		Description: "Animal counts broken down by park, shed, breed, sex, stage, or other dimensions",
		Params:      []string{"dimension", "park_label", "shed_id", "species"},
	}
}

func (e *countsBreakdownExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	// Call the real counts reader to get actual data.
	if e.countsBySpeciesReader == nil {
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("counts data reader not wired"),
		}, nil
	}

	facts, err := e.countsBySpeciesReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
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
	vaccinationDataReader scopedReader
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
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("vaccination data reader not wired"),
		}, nil
	}

	facts, err := e.vaccinationDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
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
	vaccinationDataReader scopedReader
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
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("vaccination data reader not wired"),
		}, nil
	}

	facts, err := e.vaccinationDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// feedDirectionTodayExecutor provides today's feed direction. Its Spec().Name
// ("feed_direction_today") MUST match the tool name the planner routes feed
// questions to (keywordplanner rule) — a mismatch here is what P1-1 fixed:
// registry.Execute looks executors up strictly by name, so a planner tool name
// that doesn't match this Spec silently dead-ends on "no read-service executor".
type feedDirectionTodayExecutor struct {
	feedDataReader scopedReader
}

func (e *feedDirectionTodayExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "feed_direction_today",
		Route:       domain.RouteAPI,
		Description: "Feed direction for today by shed",
		Params:      []string{"park_label", "shed_id", "as_of"},
	}
}

func (e *feedDirectionTodayExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.feedDataReader == nil {
		// Unwired reader: return error instead of silently returning empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("feed data reader not wired"),
		}, nil
	}

	facts, err := e.feedDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		// Propagate the error so it's logged and triggers fallback; do not swallow into empty.
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// procurementExecutor provides procurement loads (source entry) information.
type procurementExecutor struct {
	procurementDataReader scopedReader
}

func (e *procurementExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "procurement_source_entry_loads",
		Route:       domain.RouteAPI,
		Description: "Procurement source entry loads and their status",
		Params:      []string{"status", "park_label"},
	}
}

func (e *procurementExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.procurementDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("procurement data reader not wired"),
		}, nil
	}

	facts, err := e.procurementDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// workforceExecutor provides roster coverage information.
type workforceExecutor struct {
	workforceDataReader scopedReader
}

func (e *workforceExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "admin_roster_coverage",
		Route:       domain.RouteAPI,
		Description: "Roster coverage by position and date range",
		Params:      []string{"position_id", "start_date", "end_date"},
	}
}

func (e *workforceExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.workforceDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("workforce data reader not wired"),
		}, nil
	}

	facts, err := e.workforceDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// verificationExecutor provides verification queue information.
type verificationExecutor struct {
	verificationDataReader scopedReader
}

func (e *verificationExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "verification_queue",
		Route:       domain.RouteAPI,
		Description: "Verification queue items pending review",
		Params:      []string{"status", "category", "vertical", "module"},
	}
}

func (e *verificationExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.verificationDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("verification data reader not wired"),
		}, nil
	}

	facts, err := e.verificationDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
		}, nil
	}

	return domain.ToolResult{
		Surface:  "Mesha read API",
		ToolName: sub.ToolName,
		Facts:    facts,
	}, nil
}

// actionCenterExecutor provides action center obligations.
type actionCenterExecutor struct {
	actionCenterDataReader scopedReader
}

func (e *actionCenterExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "action_center_obligations",
		Route:       domain.RouteAPI,
		Description: "Action center obligations requiring attention",
		Params:      []string{"work_state", "park_label", "shed_id"},
	}
}

func (e *actionCenterExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.actionCenterDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("action center data reader not wired"),
		}, nil
	}

	facts, err := e.actionCenterDataReader(ctx, actor.TenantID, sub.Params)
	if err != nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      err,
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
		&procurementExecutor{},
		&workforceExecutor{},
		&verificationExecutor{},
		&actionCenterExecutor{},
	}
}

// SetCountsDataReader wires the counts data reader into the counts executor.
func SetCountsDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*countsBreakdownExecutor); ok {
		e.countsBySpeciesReader = reader
	}
}

// SetVaccinationDataReader wires the vaccination data reader into the vaccination executors.
func SetVaccinationDataReader(execs []ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
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
func SetFeedDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*feedDirectionTodayExecutor); ok {
		e.feedDataReader = reader
	}
}

// SetProcurementDataReader wires the procurement data reader into the procurement executor.
func SetProcurementDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*procurementExecutor); ok {
		e.procurementDataReader = reader
	}
}

// SetWorkforceDataReader wires the workforce data reader into the workforce executor.
func SetWorkforceDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*workforceExecutor); ok {
		e.workforceDataReader = reader
	}
}

// SetVerificationDataReader wires the verification data reader into the verification executor.
func SetVerificationDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*verificationExecutor); ok {
		e.verificationDataReader = reader
	}
}

// SetActionCenterDataReader wires the action center data reader into the action center executor.
func SetActionCenterDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*actionCenterExecutor); ok {
		e.actionCenterDataReader = reader
	}
}
