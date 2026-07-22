// Package observability also bridges the orchestrator's audit port to the
// internal step-trace store. The app layer emits one ports.AuditRecord per
// request (RequestID, tenant, routes, tools, step trace, review verdict); this
// adapter converts it to the TraceRecord the admin-only debug endpoint reads
// and persists it. Actor identity is deliberately dropped — the trace surface
// is about HOW the answer was produced, not WHO asked — and the question text
// is never carried here (only its hash lives in the audit row's own columns).
package observability

import (
	"context"
	"strconv"
	"strings"

	"github.com/vgoats/goatos/backend/internal/ceoai/domain"
	"github.com/vgoats/goatos/backend/internal/ceoai/ports"
)

// Compile-time proof the metric facade satisfies the orchestrator telemetry
// port, so a live /ask actually drives assistant_* instruments.
var _ ports.Telemetry = (*Metrics)(nil)

// AuditTraceSink adapts a TraceStore to ports.AuditSink. It is the wiring that
// makes the admin trace endpoint reachable on a live request: the orchestrator
// calls Record at request end, and the row it writes is exactly what
// GET /ceo-ai/admin/trace/{request_id} later returns.
type AuditTraceSink struct {
	store TraceStore
}

// NewAuditTraceSink binds the audit port to a trace store (Postgres in
// production, in-memory in degraded/local runs).
func NewAuditTraceSink(store TraceStore) *AuditTraceSink {
	return &AuditTraceSink{store: store}
}

var _ ports.AuditSink = (*AuditTraceSink)(nil)

// Record converts one per-request AuditRecord into the internal TraceRecord and
// persists it. A nil store is a no-op so an unwired boot never panics.
func (s *AuditTraceSink) Record(ctx context.Context, rec ports.AuditRecord) error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.RecordTrace(ctx, toTraceRecord(rec))
}

// toTraceRecord maps the app audit payload to the persisted trace shape. Route
// tier and tools are joined into the bounded scalar columns; the per-step
// timeline is converted to the observability StepTrace so the admin surface can
// render each hop.
func toTraceRecord(rec ports.AuditRecord) TraceRecord {
	return TraceRecord{
		RequestID:      rec.RequestID,
		TenantID:       rec.TenantID,
		ConversationID: rec.ConversationID,
		// Actor identity is sensitive and intentionally not persisted as a role.
		ActorRole: "",
		// Question text is never carried into the trace bridge; the audit row's
		// own question_hash column (derived in the store) is the correlation key.
		QuestionRedacted: "",
		RouteTier:        joinRoutes(rec.Routes),
		ToolCalled:       strings.Join(dedupeNonEmpty(rec.ToolsCalled), ","),
		RowCount:         rec.RowCount,
		LatencyMS:        int(rec.LatencyMS),
		Status:           string(rec.Mode),
		ReviewVerdict:    formatVerdict(rec.Review),
		ModelVersion:     rec.ModelVersion,
		PromptVersion:    rec.PromptVersion,
		Steps:            toStepTraces(rec.Steps),
	}
}

func toStepTraces(in []domain.StepTrace) []StepTrace {
	if len(in) == 0 {
		return nil
	}
	out := make([]StepTrace, 0, len(in))
	for _, s := range in {
		out = append(out, StepTrace{
			SubQuestion: s.SubQuestionID,
			Route:       string(s.Route),
			ToolName:    s.ToolName,
			RowCount:    s.RowCount,
			DurationMS:  s.DurationMS,
			Err:         s.Err,
		})
	}
	return out
}

func joinRoutes(routes []domain.Route) string {
	seen := make(map[string]struct{}, len(routes))
	parts := make([]string, 0, len(routes))
	for _, r := range routes {
		v := string(r)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		parts = append(parts, v)
	}
	return strings.Join(parts, ",")
}

func dedupeNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// formatVerdict renders the review verdict as a compact, bounded string for the
// review_verdict column (no free-form question/answer text).
func formatVerdict(v domain.ReviewVerdict) string {
	parts := []string{
		"grounded=" + strconv.FormatBool(v.Grounded),
		"scope_safe=" + strconv.FormatBool(v.ScopeSafe),
		"complete=" + strconv.FormatBool(v.Complete),
	}
	if v.Downgraded {
		parts = append(parts, "downgraded=true")
	}
	if len(v.FailReasons) > 0 {
		parts = append(parts, "reasons="+strings.Join(v.FailReasons, "|"))
	}
	return strings.Join(parts, " ")
}
