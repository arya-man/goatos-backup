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

	ceodomain "github.com/vgoats/goatos/backend/internal/ceoai/domain"
	locationsdomain "github.com/vgoats/goatos/backend/internal/locations/domain"
	locationsports "github.com/vgoats/goatos/backend/internal/locations/ports"
	operationsauditdomain "github.com/vgoats/goatos/backend/internal/operationsaudit/domain"
	processintegritydomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
	procurementdomain "github.com/vgoats/goatos/backend/internal/procurement/domain"
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

type opsKernelHealthLister interface {
	ControlTower(ctx context.Context, q processintegritydomain.Query) (processintegritydomain.ControlTowerResponse, error)
}

type opsAuditSummarizer interface {
	Summary(ctx context.Context, q operationsauditdomain.Query, traceID string) (operationsauditdomain.SummaryResponse, error)
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
				Value: fmt.Sprintf("Status: %s, Evidence Available: %v", item.Item.Status, item.EvidenceAvailable),
				Scope: scope,
			})
		}
		return facts, nil
	}
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
			if item.ShedName != "" {
				scope = scope + " / " + item.ShedName
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
			if alert.ShedName != "" {
				scope = scope + " / " + alert.ShedName
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
