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
		// park_label is intentionally NOT advertised: procurementdomain.LoadQuery
		// has no park-scoping field at all (not even a park_id), so the closure
		// has nothing to map it onto. Advertising it would silently promise
		// scoping the pipeline cannot honor. Revisit if/when the source-entry
		// load read model gains a park column.
		Params: []string{"status"},
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
		Description: "Roster coverage by scope (position/shed)",
		// position_id/start_date/end_date were previously advertised but
		// workforceports.ListCoverageParams has none of those fields -- the
		// pipeline could never honor them. scope_type/scope_id ARE real
		// ListCoverageParams fields and the closure maps them; that is the set
		// actually wired end-to-end today. park_label is NOT advertised: roster
		// coverage scope is shed/position-based (scope_type="shed"), not
		// park-based, and there is no park-label resolution that fits this
		// scope model.
		Params: []string{"scope_type", "scope_id"},
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
		// work_state and shed_id map directly onto processintegritydomain.Query
		// fields. park_label is resolved to Query.ParkID via the park resolver
		// wired in bootstrap (see api.go / ceoai_readers.go) -- this is the P1
		// fix: previously advertised but silently dropped. shed_id is a plain
		// shed-location ID; there is no sub-shed "partition" concept on this
		// Query (partition_label lives only on vaccination_drive_assignments /
		// goat_shed_partitions, which this tool does not read), so partition is
		// intentionally NOT advertised.
		Params: []string{"work_state", "park_label", "shed_id"},
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

// opsKernelHealthExecutor provides the process-integrity control-tower view
// (open exceptions / broken-or-at-risk obligations) for kernel-health questions.
type opsKernelHealthExecutor struct {
	opsKernelHealthDataReader scopedReader
}

func (e *opsKernelHealthExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "operations_kernel_health",
		Route:       domain.RouteAPI,
		Description: "Open exceptions / broken-or-at-risk obligations (kernel health)",
		// severity, work_state and shed_id are real processintegritydomain.Query
		// fields the closure maps directly (shed_id is a plain shed-location ID,
		// same field ParkID's sibling ShedID -- see action_center). park_label is
		// NOT advertised for the same reason as action_center's ParkID -- see the
		// api.go wiring comment; unlike action_center this tool has no park
		// resolver wired yet. There is no sub-shed "partition" concept on this
		// Query (partition_label lives only on vaccination_drive_assignments /
		// goat_shed_partitions, which this tool does not read), so partition is
		// NOT advertised either.
		Params: []string{"severity", "work_state", "shed_id"},
	}
}

func (e *opsKernelHealthExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.opsKernelHealthDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("operations kernel health data reader not wired"),
		}, nil
	}

	facts, err := e.opsKernelHealthDataReader(ctx, actor.TenantID, sub.Params)
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

// opsAuditSummaryExecutor provides the operations/business audit summary.
type opsAuditSummaryExecutor struct {
	opsAuditSummaryDataReader scopedReader
}

func (e *opsAuditSummaryExecutor) Spec() ports.ToolSpec {
	return ports.ToolSpec{
		Name:        "operations_audit_summary",
		Route:       domain.RouteAPI,
		Description: "Operations/business audit activity summary",
		// category/module/status are real operationsaudit domain.Query fields
		// the closure maps directly. No park field exists on that Query, so
		// park_label is not advertised.
		Params: []string{"category", "module", "status"},
	}
}

func (e *opsAuditSummaryExecutor) Execute(ctx context.Context, actor domain.Actor, sub domain.SubQuestion) (domain.ToolResult, error) {
	if e.opsAuditSummaryDataReader == nil {
		return domain.ToolResult{
			Surface:  "Mesha read API",
			ToolName: sub.ToolName,
			Facts:    []domain.Fact{},
			Err:      fmt.Errorf("operations audit summary data reader not wired"),
		}, nil
	}

	facts, err := e.opsAuditSummaryDataReader(ctx, actor.TenantID, sub.Params)
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
		&opsKernelHealthExecutor{},
		&opsAuditSummaryExecutor{},
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

// SetOpsKernelHealthDataReader wires the data reader into the operations kernel health executor.
func SetOpsKernelHealthDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*opsKernelHealthExecutor); ok {
		e.opsKernelHealthDataReader = reader
	}
}

// SetOpsAuditSummaryDataReader wires the data reader into the operations audit summary executor.
func SetOpsAuditSummaryDataReader(exec ports.ToolExecutor, reader func(context.Context, string, map[string]any) ([]domain.Fact, error)) {
	if e, ok := exec.(*opsAuditSummaryExecutor); ok {
		e.opsAuditSummaryDataReader = reader
	}
}
