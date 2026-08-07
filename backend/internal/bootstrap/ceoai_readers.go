package bootstrap

// This file holds the leadership-assistant (CEO AI) in-process read-tool
// reader constructors, split out of api.go so they are unit-testable against
// fakes without booting the whole HTTP server (see ceoai_readers_test.go).
//
// The core correctness rule these functions exist to enforce: a scoped
// question (e.g. "...at Castro 1") must never silently fall back to
// tenant-wide data because the planner's park_label param got dropped
// between the tool Spec, the reader closure, and the underlying service
// query. Each constructor below maps ONLY the params that are both
// advertised in the executor's Spec().Params (readtools/toolexecutors.go)
// and actually supported by the target service's query struct -- see the
// per-tool comments for what is NOT wired and why.

import (
	"context"
	"fmt"
	"strings"
	"time"

	ceodomain "github.com/vgoats/goatos/backend/internal/ceoai/domain"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	locationsdomain "github.com/vgoats/goatos/backend/internal/locations/domain"
	locationsports "github.com/vgoats/goatos/backend/internal/locations/ports"
	operationsauditdomain "github.com/vgoats/goatos/backend/internal/operationsaudit/domain"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	processintegritydomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	procurementdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
	vaccexecd "github.com/vgoats/goatos/backend/internal/vaccinationexecution/domain"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationports "github.com/vgoats/goatos/backend/internal/verification/ports"
	workforcedomain "github.com/vgoats/goatos/backend/internal/workforce/domain"
	workforceports "github.com/vgoats/goatos/backend/internal/workforce/ports"
)

// --- narrow read-only ports the closures below depend on. Kept minimal
// (only the one method each reader calls) so tests can supply a fake that
// captures the query it received, instead of standing up a real Postgres
// pool. The concrete app services (procurementapp.Service etc.) already
// satisfy these structurally. ---

type procurementLoadLister interface {
	ListLoads(ctx context.Context, q procurementdomain.LoadQuery) (procurementdomain.LoadListResult, error)
}

type rosterCoverageLister interface {
	ListCoverage(ctx context.Context, params workforceports.ListCoverageParams, traceID string) (*workforcedomain.CoverageListResponse, error)
}

type verificationQueueLister interface {
	ListQueue(ctx context.Context, params verificationports.ListQueueParams) (verificationapp.QueueResult, error)
}

type actionCenterLister interface {
	ActionCenter(ctx context.Context, q processintegritydomain.Query) (processintegritydomain.ActionCenterResponse, error)
}

type vaccinationShedSummaryLister interface {
	ShedSummary(ctx context.Context, q vaccexecd.ShedSummaryQuery) (vaccexecd.ShedSummaryResponse, error)
}

type opsKernelHealthLister interface {
	ControlTower(ctx context.Context, q processintegritydomain.Query) (processintegritydomain.ControlTowerResponse, error)
}

type opsAuditSummarizer interface {
	Summary(ctx context.Context, q operationsauditdomain.Query, traceID string) (operationsauditdomain.SummaryResponse, error)
}

type countsBreakdownLister interface {
	GetBreakdown(ctx context.Context, req countsdomain.CountsBreakdownQuery) (countsdomain.CountsBreakdown, error)
}

// parkResolver resolves the planner's human park_label (e.g. "Castro 1") to
// the internal park location UUID that scale-safe queries actually filter
// on. It is the ONLY place park_label is honored for tools whose underlying
// query needs an ID, not a label -- tools without a resolver call intentionally
// leave park_label off their Spec (see toolexecutors.go comments) rather than
// silently ignoring it.
type parkResolver interface {
	ResolveParkID(ctx context.Context, tenantID, parkLabel string) (string, bool, error)
}

// locationsParkResolver resolves via the already-wired locations read
// service: an exact, case-insensitive name match scoped to LocationType
// "park". No result and no error both mean "unresolvable" (ok=false) --
// callers must treat that as "cannot honor the scope" rather than falling
// through to tenant-wide.
type locationsParkResolver struct {
	svc locationsLister
}

type locationsLister interface {
	ListLocations(ctx context.Context, params locationsports.ListParams, traceID string) (*locationsdomain.LocationListResponse, error)
}

func newLocationsParkResolver(svc locationsLister) parkResolver {
	return &locationsParkResolver{svc: svc}
}

func (r *locationsParkResolver) ResolveParkID(ctx context.Context, tenantID, parkLabel string) (string, bool, error) {
	if r == nil || r.svc == nil || parkLabel == "" {
		return "", false, nil
	}
	resp, err := r.svc.ListLocations(ctx, locationsports.ListParams{
		TenantID:     tenantID,
		LocationType: "park",
		Search:       parkLabel,
		Limit:        10,
	}, "")
	if err != nil {
		return "", false, err
	}
	if resp == nil {
		return "", false, nil
	}
	for _, item := range resp.Items {
		if item.Name == parkLabel {
			return item.LocationID, true, nil
		}
	}
	return "", false, nil
}

// buildCountsReader maps park_label/shed_id/stage/breed/sex onto
// countsdomain.CountsBreakdownQuery's real fields, plus "partition_label" --
// which the underlying grain (ceo_ai.animal_current_scope's counts-domain
// twin) already carries per row (CountsBreakdownRow.PartitionLabel /
// OperationalLocationDisplay) even though CountsBreakdownQuery itself has no
// partition filter field. So "at Castro 1" is honored two ways: every row's
// Scope renders through oploc.OperationalLocation.Display() -- which already
// disambiguates "Castro 1" from "Castro 2" instead of collapsing both under
// the bare "Castro" shed label -- and, when the caller names a specific
// partition, rows for every OTHER partition of that shed are dropped before
// the fact list is built (oploc.SamePartition, so "1" and "Part 1" match the
// same partition). A shed with no partitions renders its bare shed name,
// never the "whole" sentinel, per oploc.OperationalLocation.Display().
func buildCountsReader(svc countsBreakdownLister, resolver parkResolver) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		q := countsdomain.CountsBreakdownQuery{TenantID: tenantID, Limit: 10}
		if parkID, ok := params["park_id"].(string); ok && parkID != "" {
			q.ParkID = &parkID
		} else if parkLabel, ok := params["park_label"].(string); ok && parkLabel != "" {
			parkID, found, err := resolver.ResolveParkID(ctx, tenantID, parkLabel)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, fmt.Errorf("park_label %q could not be resolved", parkLabel)
			}
			q.ParkID = &parkID
		}
		if parkID, ok := params["park_id"].(string); ok && parkID != "" {
			q.ParkID = &parkID
		}
		if shedID, ok := params["shed_id"].(string); ok && shedID != "" {
			q.ShedID = &shedID
		}
		if stage, ok := params["stage"].(string); ok && stage != "" {
			q.ManagementStage = &stage
		}
		if breed, ok := params["breed"].(string); ok && breed != "" {
			q.Breed = &breed
		}
		if sex, ok := params["sex"].(string); ok && sex != "" {
			q.Sex = &sex
		}

		result, err := svc.GetBreakdown(ctx, q)
		if err != nil {
			return nil, err
		}

		partitionFilter, hasPartitionFilter := params["partition_label"].(string)
		hasPartitionFilter = hasPartitionFilter && partitionFilter != ""

		facts := []ceodomain.Fact{{
			Label: "Active animals",
			Value: fmt.Sprintf("%d", result.TotalCount),
		}}
		if wantsSpeciesSplit(params) {
			goats, sheep := speciesCountsFromBreeds(result.Charts.Breed)
			facts = append(facts, ceodomain.Fact{Label: "Goats", Value: fmt.Sprintf("%d", goats)})
			facts = append(facts, ceodomain.Fact{Label: "Sheep", Value: fmt.Sprintf("%d", sheep)})
			return facts, nil
		}
		if result.TotalKids > 0 || result.TotalAdults > 0 {
			facts = append(facts, ceodomain.Fact{
				Label: "Age bands",
				Value: fmt.Sprintf("Kids: %d, Adults: %d", result.TotalKids, result.TotalAdults),
			})
		}
		for _, row := range result.Items {
			if hasPartitionFilter && !oploc.SamePartition(row.PartitionLabel, partitionFilter) {
				continue
			}
			loc := oploc.OperationalLocation{ShedName: row.ShedLabel, PartitionLabel: row.PartitionLabel}
			scope := row.ParkLabel
			if row.ShedLabel != "" {
				scope = fmt.Sprintf("%s / %s", row.ParkLabel, loc.Display())
			}
			facts = append(facts, ceodomain.Fact{
				Label: "Counts breakdown",
				Value: fmt.Sprintf("Stage: %s, Breed: %s, Sex: %s, Count: %d", row.ManagementStage, row.Breed, row.Sex, row.Count),
				Scope: scope,
			})
		}
		return facts, nil
	}
}

func wantsSpeciesSplit(params map[string]any) bool {
	if groupBy, ok := params["group_by"].(string); ok && groupBy == "species" {
		return true
	}
	if dims, ok := params["dimensions"].(string); ok && dims == "species" {
		return true
	}
	return false
}

func speciesCountsFromBreeds(points []countsdomain.CountsBreakdownSeriesPoint) (goats int64, sheep int64) {
	for _, point := range points {
		if strings.Contains(strings.ToLower(point.Label), "sheep") {
			sheep += point.Count
		} else {
			goats += point.Count
		}
	}
	return goats, sheep
}

// buildProcurementReader maps ONLY "status" -- the sole advertised param
// (readtools' procurementExecutor.Spec().Params) that procurementdomain.LoadQuery
// actually has a field for. LoadQuery has no park-scoping field at all, so
// park_label is not read here even if present in params.
func buildProcurementReader(svc procurementLoadLister) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		q := procurementdomain.LoadQuery{TenantID: tenantID}
		if status, ok := params["status"].(string); ok {
			q.Status = status
		}
		if limit, ok := params["limit"].(int); ok {
			q.Limit = limit
		}
		result, err := svc.ListLoads(ctx, q) // scale-guard:ignore: one-time executor-registration scan, not per-row I/O; closure calls the service once per assistant request
		if err != nil {
			return nil, err
		}
		facts := make([]ceodomain.Fact, 0, len(result.Items))
		for _, item := range result.Items {
			scope := item.LoadID
			if item.SourcePartyName != "" {
				scope = scope + " / " + item.SourcePartyName
			}
			facts = append(facts, ceodomain.Fact{
				Label: "Load " + item.Status,
				Value: fmt.Sprintf("Expected %d animals, Status: %s", item.ExpectedCount, item.Status),
				Scope: scope,
			})
		}
		return facts, nil
	}
}

// buildWorkforceReader maps "scope_type"/"scope_id" -- the two advertised
// params that are real workforceports.ListCoverageParams fields. Roster
// coverage scope is shed/position-based (scope_type="shed"), not park-based,
// so park_label is intentionally not read here.
func buildWorkforceReader(svc rosterCoverageLister) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		queryParams := workforceports.ListCoverageParams{TenantID: tenantID, Active: true}
		if scopeType, ok := params["scope_type"].(string); ok {
			queryParams.ScopeType = scopeType
		}
		if scopeID, ok := params["scope_id"].(string); ok {
			queryParams.ScopeID = scopeID
		}
		result, err := svc.ListCoverage(ctx, queryParams, "") // scale-guard:ignore: one-time executor-registration scan, not per-row I/O; closure calls the service once per assistant request
		if err != nil {
			return nil, err
		}
		if result == nil {
			return []ceodomain.Fact{}, nil
		}
		facts := make([]ceodomain.Fact, 0, len(result.Items))
		for _, item := range result.Items {
			scope := item.CoveredPositionCode
			if item.CoveredPositionTitle != nil {
				scope = scope + " / " + *item.CoveredPositionTitle
			}
			coveringName := "Uncovered"
			if item.CoveringMemberName != nil {
				coveringName = *item.CoveringMemberName
			}
			facts = append(facts, ceodomain.Fact{
				Label: "Coverage",
				Value: fmt.Sprintf("Covering from %s to %s by %s", item.StartDate, item.EndDate, coveringName),
				Scope: scope,
			})
		}
		return facts, nil
	}
}

// buildVerificationReader maps status/category/vertical/module -- unchanged
// from before this fix; ListQueueParams has all four fields and the closure
// already mapped all four, so this tool was never dropping planner params.
func buildVerificationReader(svc verificationQueueLister) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		queryParams := verificationports.ListQueueParams{TenantID: tenantID}
		if status, ok := params["status"].(string); ok {
			queryParams.Status = status
		}
		if category, ok := params["category"].(string); ok {
			queryParams.Category = category
		}
		if vertical, ok := params["vertical"].(string); ok {
			queryParams.Vertical = vertical
		}
		if module, ok := params["module"].(string); ok {
			queryParams.Module = module
		}
		result, err := svc.ListQueue(ctx, queryParams) // scale-guard:ignore: one-time executor-registration scan, not per-row I/O; closure calls the service once per assistant request
		if err != nil {
			return nil, err
		}
		facts := make([]ceodomain.Fact, 0, len(result.Items))
		for _, item := range result.Items {
			scope := item.Item.Category
			if item.Item.Vertical != "" {
				scope = item.Item.Vertical + " / " + scope
			}
			facts = append(facts, ceodomain.Fact{
				Label: "Verification Item",
				Value: fmt.Sprintf("Status: %s, Evidence Available: %v", item.Item.Status, item.EvidenceLinkResolved),
				Scope: scope,
			})
		}
		return facts, nil
	}
}

// buildVaccinationReader maps shed_id/as_of onto the same canonical shed
// summary service that backs GET /vaccination/sheds. park_label is advertised
// by the executor for future planner compatibility but is intentionally not
// read here because ShedSummaryQuery takes park_id, not a label; callers that
// need park scoping should pass shed_id or add a park resolver before expanding
// the advertised contract.
func buildVaccinationReader(svc vaccinationShedSummaryLister) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		asOf := biztime.BusinessDayStart(time.Now())
		if raw, ok := params["as_of"].(string); ok && raw != "" {
			parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
			if err != nil {
				return nil, fmt.Errorf("invalid as_of %q: %w", raw, err)
			}
			asOf = parsed
		}
		q := vaccexecd.ShedSummaryQuery{
			TenantID:  tenantID,
			AsOf:      asOf,
			DueBefore: asOf.Add(45 * 24 * time.Hour),
			Sort:      vaccexecd.ShedSortStatus,
			Limit:     100,
		}
		if shedID, ok := params["shed_id"].(string); ok && shedID != "" {
			q.ShedID = &shedID
		}
		result, err := svc.ShedSummary(ctx, q) // scale-guard:ignore: one-time executor-registration scan, not per-row I/O; closure calls the service once per assistant request
		if err != nil {
			return nil, err
		}
		facts := make([]ceodomain.Fact, 0, len(result.Rows)+1)
		totalAnimals, totalDue, totalDone, totalSessions := 0, 0, 0, 0
		metricLabel := vaccinationMetricLabel(params)
		aggregateTotal := stringParam(params, "aggregate_total") == "true"
		for _, row := range result.Rows {
			totalAnimals += row.Animals
			totalDue += row.Due
			totalDone += row.Done
			totalSessions += row.Sessions
			// Use the BACKEND-COMPOSED operational location, not the bare shed name. A
			// partitioned shed answered "CBE / Godel 1" to a CEO question when the animals
			// are actually in "Godel 1 - Part 3" -- the assistant is a user-facing surface
			// and the partition rule applies to it exactly as it does to a screen.
			scope := row.ParkName
			if shedLabel := shedScopeLabel(row.OperationalLocationDisplay, row.ShedName, nil); shedLabel != "" {
				scope = scope + " / " + shedLabel
			}
			if metricLabel != "" {
				facts = append(facts, ceodomain.Fact{
					Label: metricLabel,
					Value: fmt.Sprintf("%d", row.Due),
					Scope: scope,
				})
				continue
			}
			facts = append(facts, ceodomain.Fact{
				Label: "Vaccination shed",
				Value: fmt.Sprintf("Animals: %d, Due: %d, Done: %d, Sessions: %d, Status: %s",
					row.Animals, row.Due, row.Done, row.Sessions, row.Status),
				Scope: scope,
			})
		}
		facts = append([]ceodomain.Fact{{
			Label: "Vaccination summary",
			Value: fmt.Sprintf("Sheds: %d, Animals: %d, Due: %d, Done: %d, Sessions: %d",
				len(result.Rows), totalAnimals, totalDue, totalDone, totalSessions),
		}}, facts...)
		if metricLabel != "" && aggregateTotal {
			return []ceodomain.Fact{{
				Label: metricLabel,
				Value: fmt.Sprintf("%d", totalDue),
				Scope: "all parks",
			}}, nil
		}
		return facts, nil
	}
}

func vaccinationMetricLabel(params map[string]any) string {
	switch stringParam(params, "vaccination_intent") {
	case "missed":
		return "Vaccinations missed"
	case "overdue":
		return "Vaccinations overdue"
	}
	tool, _ := params["_fallback_from_tool"].(string)
	switch tool {
	case "vaccination_overdue":
		return "Vaccinations overdue"
	case "vaccination_due", "vaccination_due_today":
		return "Vaccinations due"
	case "vaccination_compliance":
		return "Vaccination completion"
	default:
		return ""
	}
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, _ := params[key].(string)
	return v
}

// buildActionCenterReader maps work_state, park_label (resolved to ParkID via
// resolver), and shed_id -- ALL THREE tools advertised in Spec().Params, and
// all three are now actually honored end-to-end. This is the P1 fix: park_label
// was previously advertised but silently dropped. If the label does not
// resolve to a known park, the reader returns an error rather than silently
// falling back to tenant-wide data (fail closed, per the correctness rule).
func buildActionCenterReader(svc actionCenterLister, resolver parkResolver) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		q := processintegritydomain.Query{TenantID: tenantID}
		if workState, ok := params["work_state"].(string); ok {
			ws := processintegritydomain.WorkState(workState)
			q.WorkState = &ws
		}
		if shedID, ok := params["shed_id"].(string); ok && shedID != "" {
			q.ShedID = &shedID
		}
		if parkLabel, ok := params["park_label"].(string); ok && parkLabel != "" {
			parkID, resolved, err := resolver.ResolveParkID(ctx, tenantID, parkLabel)
			if err != nil {
				return nil, fmt.Errorf("resolving park_label %q: %w", parkLabel, err)
			}
			if !resolved {
				return nil, fmt.Errorf("park_label %q did not resolve to a known park; refusing to answer tenant-wide", parkLabel)
			}
			q.ParkID = &parkID
		}
		result, err := svc.ActionCenter(ctx, q) // scale-guard:ignore: one-time executor-registration scan, not per-row I/O; closure calls the service once per assistant request
		if err != nil {
			return nil, err
		}
		facts := make([]ceodomain.Fact, 0, len(result.Items))
		for _, item := range result.Items {
			scope := item.Category
			if item.ParkName != "" {
				scope = item.ParkName + " / " + scope
			}
			// Same rule: the composed location, so an alert names the partition the work is
			// actually in rather than the parent shed.
			if shedLabel := shedScopeLabel(item.OperationalLocationDisplay, item.ShedName, item.PartitionLabel); shedLabel != "" {
				scope = scope + " / " + shedLabel
			}
			facts = append(facts, ceodomain.Fact{
				Label: item.Category,
				Value: fmt.Sprintf("Work State: %s, Rule: %s", item.WorkState, item.DoseCode),
				Scope: scope,
			})
		}
		return facts, nil
	}
}

// buildOpsKernelHealthReader maps severity/work_state -- real
// processintegritydomain.Query fields. No park resolver is wired for this
// tool yet (unlike action_center); park_label is intentionally not advertised
// or read here (see toolexecutors.go Spec comment).
func buildOpsKernelHealthReader(svc opsKernelHealthLister) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		q := processintegritydomain.Query{TenantID: tenantID}
		if severity, ok := params["severity"].(string); ok && severity != "" {
			sev := processintegritydomain.Severity(severity)
			q.Severity = &sev
		}
		if workState, ok := params["work_state"].(string); ok && workState != "" {
			ws := processintegritydomain.WorkState(workState)
			q.WorkState = &ws
		}
		if shedID, ok := params["shed_id"].(string); ok && shedID != "" {
			q.ShedID = &shedID
		}
		result, err := svc.ControlTower(ctx, q) // scale-guard:ignore: one-time executor-registration scan, not per-row I/O; closure calls the service once per assistant request
		if err != nil {
			return nil, err
		}
		facts := make([]ceodomain.Fact, 0, len(result.Alerts)+1)
		facts = append(facts, ceodomain.Fact{
			Label: "Kernel health",
			Value: fmt.Sprintf("Open gaps: %d, Critical: %d, Warning: %d, Verification backlog: %d",
				result.Summary.OpenGapCount, result.Summary.CriticalCount, result.Summary.WarningCount, result.Summary.VerificationBacklog),
		})
		for _, alert := range result.Alerts {
			scope := alert.ParkName
			if shedLabel := shedScopeLabel("", alert.ShedName, alert.PartitionLabel); shedLabel != "" {
				scope = scope + " / " + shedLabel
			}
			facts = append(facts, ceodomain.Fact{
				Label: alert.Title,
				Value: alert.Detail,
				Scope: scope,
			})
		}
		return facts, nil
	}
}

// buildOpsAuditSummaryReader maps category/module/status -- real
// operationsaudit domain.Query fields. That Query has no park field, so
// park_label is not advertised or read.
func buildOpsAuditSummaryReader(svc opsAuditSummarizer) func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
	return func(ctx context.Context, tenantID string, params map[string]any) ([]ceodomain.Fact, error) {
		q := operationsauditdomain.Query{TenantID: tenantID}
		if category, ok := params["category"].(string); ok && category != "" {
			q.Category = &category
		}
		if module, ok := params["module"].(string); ok && module != "" {
			q.Module = &module
		}
		if status, ok := params["status"].(string); ok && status != "" {
			q.Status = &status
		}
		result, err := svc.Summary(ctx, q, "") // scale-guard:ignore: one-time executor-registration scan, not per-row I/O; closure calls the service once per assistant request
		if err != nil {
			return nil, err
		}
		facts := []ceodomain.Fact{{
			Label: "Audit summary",
			Value: fmt.Sprintf("Actions: %d, Awaiting verification: %d, Rejected: %d, Rework: %d, Anomalies: %d",
				result.Actions, result.AwaitingVerification, result.Rejected, result.Rework, result.Anomalies),
		}}
		return facts, nil
	}
}

// shedScopeLabel is the ONE place this file turns a shed into assistant-visible text.
// Prefer the backend-composed display; when a producer has not filled it, compose from the
// raw partition rather than falling back to a bare shed name -- a fallback that drops the
// partition is the same defect as never composing at all, it just fails less often.
func shedScopeLabel(display, shedName string, partitionLabel *string) string {
	if composed := strings.TrimSpace(display); composed != "" {
		return composed
	}
	label := ""
	if partitionLabel != nil {
		label = *partitionLabel
	}
	return oploc.OperationalLocation{ShedName: shedName, PartitionLabel: label}.Display()
}
