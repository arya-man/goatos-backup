package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/operationsaudit/domain"
	"github.com/vgoats/goatos/backend/internal/operationsaudit/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

const defaultQueryTimeout = 3 * time.Second

type Repository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewRepository(pool *pgxpool.Pool, queryTimeout time.Duration) *Repository {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &Repository{pool: pool, timeout: queryTimeout}
}

var _ ports.Repository = (*Repository)(nil)

func (r *Repository) List(ctx context.Context, q domain.Query) ([]domain.AuditRow, *domain.Cursor, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	q = normalizeQuery(q)
	args, where := listArgs(q)
	args = append(args, q.Limit+1)
	limitArg := len(args)
	query := `
SELECT
  audit_id::text,
  recorded_at,
  actor_type,
  actor_id::text,
  action,
  resource_type,
  resource_id::text,
  scope_type,
  scope_id::text,
  metadata,
  trace_id,
  ` + anomalySQL() + ` AS anomaly
FROM audit_log
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY recorded_at DESC, audit_id DESC
LIMIT $` + fmt.Sprint(limitArg)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("operationsaudit: list: %w", err)
	}
	defer rows.Close()
	out := make([]domain.AuditRow, 0, q.Limit)
	for rows.Next() {
		row, err := scanAuditRow(rows)
		if err != nil {
			return nil, nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("operationsaudit: iterate: %w", err)
	}
	var next *domain.Cursor
	if len(out) > q.Limit {
		out = out[:q.Limit]
		last := out[len(out)-1]
		next = &domain.Cursor{RecordedAt: last.RecordedAt, AuditID: last.AuditID}
	}
	return out, next, nil
}

func (r *Repository) Summary(ctx context.Context, q domain.Query) (domain.SummaryResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	q = normalizeQuery(q)
	args, where := listArgs(q)
	var out domain.SummaryResponse
	err := r.pool.QueryRow(ctx, `
SELECT
  count(*)::bigint AS actions,
  count(*) FILTER (WHERE `+awaitingVerificationSQL()+`)::bigint AS awaiting_verification,
  count(*) FILTER (WHERE resource_type = 'proof' OR action ILIKE '%proof%' OR action ILIKE '%sop%')::bigint AS proof_events,
  count(*) FILTER (WHERE action ILIKE '%reject%' OR metadata->>'result' = 'rejected')::bigint AS rejected,
  count(*) FILTER (WHERE action ILIKE '%rework%' OR metadata->>'result' = 'rework')::bigint AS rework,
  count(*) FILTER (WHERE `+anomalySQL()+`)::bigint AS anomalies
FROM audit_log
WHERE `+strings.Join(where, " AND "), args...).Scan(&out.Actions, &out.AwaitingVerification, &out.ProofEvents, &out.Rejected, &out.Rework, &out.Anomalies)
	if err != nil {
		return domain.SummaryResponse{}, fmt.Errorf("operationsaudit: summary: %w", err)
	}
	if out.Actions > 0 {
		out.ProofCoveragePercent = int64(math.Min(100, math.Round(float64(out.ProofEvents)*100/float64(out.Actions))))
	}
	out.From = *q.From
	out.To = *q.To
	out.AsOf = time.Now().In(biztime.DefaultLocation())
	return out, nil
}

func normalizeQuery(q domain.Query) domain.Query {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	if q.Limit > 500 {
		q.Limit = 500
	}
	return q
}

func listArgs(q domain.Query) ([]any, []string) {
	args := []any{q.TenantID}
	where := []string{"tenant_id = $1::uuid"}
	if q.From != nil {
		args = append(args, *q.From)
		where = append(where, fmt.Sprintf("recorded_at >= $%d::timestamptz", len(args)))
	}
	if q.To != nil {
		args = append(args, *q.To)
		where = append(where, fmt.Sprintf("recorded_at <= $%d::timestamptz", len(args)))
	}
	if q.ActorType != nil {
		args = append(args, *q.ActorType)
		where = append(where, fmt.Sprintf("actor_type = $%d", len(args)))
	}
	if q.ActorID != nil {
		args = append(args, *q.ActorID)
		where = append(where, fmt.Sprintf("actor_id = $%d::uuid", len(args)))
	}
	if q.Action != nil {
		args = append(args, *q.Action)
		where = append(where, fmt.Sprintf("action = $%d", len(args)))
	}
	if q.ResourceType != nil {
		args = append(args, *q.ResourceType)
		where = append(where, fmt.Sprintf("resource_type = $%d", len(args)))
	}
	if q.ResourceID != nil {
		args = append(args, *q.ResourceID)
		where = append(where, fmt.Sprintf("resource_id = $%d::uuid", len(args)))
	}
	if q.ScopeType != nil {
		args = append(args, *q.ScopeType)
		where = append(where, fmt.Sprintf("scope_type = $%d", len(args)))
	}
	if q.ScopeID != nil {
		args = append(args, *q.ScopeID)
		where = append(where, fmt.Sprintf("scope_id = $%d::uuid", len(args)))
	}
	if q.Domain != nil {
		args = append(args, *q.Domain)
		where = append(where, fmt.Sprintf("%s = $%d", auditDomainSQL(), len(args)))
	}
	if q.Module != nil {
		args = append(args, *q.Module)
		where = append(where, fmt.Sprintf("%s = $%d", auditModuleSQL(), len(args)))
	}
	if q.Category != nil {
		args = append(args, *q.Category)
		where = append(where, fmt.Sprintf("metadata->>'category' = $%d", len(args)))
	}
	if q.Result != nil {
		args = append(args, *q.Result)
		where = append(where, fmt.Sprintf("COALESCE(metadata->>'result', metadata->>'status') = $%d", len(args)))
	}
	if q.Status != nil {
		args = append(args, *q.Status)
		where = append(where, fmt.Sprintf("metadata->>'status' = $%d", len(args)))
	}
	if q.Search != nil {
		needle := "%" + strings.ToLower(strings.TrimSpace(*q.Search)) + "%"
		if needle != "%%" {
			args = append(args, needle)
			where = append(where, fmt.Sprintf(`(
				lower(action) LIKE $%[1]d
				OR lower(actor_type) LIKE $%[1]d
				OR lower(COALESCE(actor_id::text, '')) LIKE $%[1]d
				OR lower(resource_type) LIKE $%[1]d
				OR lower(COALESCE(resource_id::text, '')) LIKE $%[1]d
				OR lower(COALESCE(scope_type, '')) LIKE $%[1]d
				OR lower(COALESCE(scope_id::text, '')) LIKE $%[1]d
				OR lower(COALESCE(metadata->>'domain', '')) LIKE $%[1]d
				OR lower(COALESCE(metadata->>'module', '')) LIKE $%[1]d
				OR lower(COALESCE(metadata->>'category', '')) LIKE $%[1]d
				OR lower(COALESCE(metadata->>'result', '')) LIKE $%[1]d
				OR lower(COALESCE(metadata->>'status', '')) LIKE $%[1]d
				OR lower(COALESCE(metadata->>'operator_name', '')) LIKE $%[1]d
				OR lower(COALESCE(metadata->>'target_label', '')) LIKE $%[1]d
			)`, len(args)))
		}
	}
	if q.Cursor != nil {
		args = append(args, q.Cursor.RecordedAt, q.Cursor.AuditID)
		where = append(where, fmt.Sprintf("(recorded_at, audit_id) < ($%d::timestamptz, $%d::uuid)", len(args)-1, len(args)))
	}
	if q.AnomaliesOnly {
		where = append(where, anomalySQL())
	}
	if q.ProofGapsOnly {
		where = append(where, proofGapSQL())
	}
	return args, where
}

func scanAuditRow(row interface{ Scan(dest ...any) error }) (domain.AuditRow, error) {
	var out domain.AuditRow
	var actorID, resourceID, scopeType, scopeID, traceID sql.NullString
	var metadata []byte
	if err := row.Scan(
		&out.AuditID,
		&out.RecordedAt,
		&out.ActorType,
		&actorID,
		&out.Action,
		&out.ResourceType,
		&resourceID,
		&scopeType,
		&scopeID,
		&metadata,
		&traceID,
		&out.Anomaly,
	); err != nil {
		return domain.AuditRow{}, fmt.Errorf("operationsaudit: scan row: %w", err)
	}
	out.ActorID = stringPtr(actorID)
	out.ResourceID = stringPtr(resourceID)
	out.ScopeType = stringPtr(scopeType)
	out.ScopeID = stringPtr(scopeID)
	out.TraceID = stringPtr(traceID)
	out.Metadata = map[string]any{}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &out.Metadata)
	}
	domainValue, hasDomain := out.Metadata["domain"].(string)
	moduleValue, _ := out.Metadata["module"].(string)
	if !hasDomain || strings.TrimSpace(domainValue) == "" || (domainValue == "pc" && moduleValue == "vaccination") {
		out.Metadata["domain"] = auditDomain(out.Action, out.ResourceType)
	}
	if moduleValue == "" {
		out.Metadata["module"] = auditModule(out.Action, out.ResourceType)
	}
	return out, nil
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func anomalySQL() string {
	return `(action ILIKE '%fail%' OR action ILIKE '%reject%' OR action ILIKE '%rollback%' OR action ILIKE '%delete%' OR action ILIKE '%skip%' OR action ILIKE '%mismatch%' OR action ILIKE '%rework%' OR metadata ? 'error' OR metadata ? 'anomaly_reason' OR lower(COALESCE(metadata->>'anomaly', '')) IN ('true', '1', 'yes') OR lower(COALESCE(metadata->>'result', '')) IN ('failed', 'rejected', 'rollback', 'rework', 'mismatch', 'skipped', 'deleted') OR lower(COALESCE(metadata->>'status', '')) IN ('failed', 'rejected', 'rollback', 'rework', 'mismatch', 'skipped', 'deleted') OR lower(COALESCE(metadata->>'category', '')) IN ('stock_mismatch', 'delete', 'deleted', 'skip', 'silent_skip', 'rework'))`
}

func auditDomainSQL() string {
	return `CASE
		WHEN metadata->>'domain' = 'pc' AND metadata->>'module' = 'vaccination' THEN 'vaccination'
		WHEN COALESCE(metadata->>'domain', '') <> '' THEN metadata->>'domain'
		WHEN action LIKE 'vaccination.%' OR resource_type IN ('vaccination_drive', 'vaccination_drive_assignment') THEN 'vaccination'
		WHEN action LIKE 'feed.%' OR resource_type LIKE 'feed_%' THEN 'feed'
		WHEN action LIKE 'weighing.%' OR resource_type LIKE 'weighing_%' THEN 'weighing'
		WHEN action LIKE 'health.%' OR action LIKE 'clinical.%' OR action LIKE 'treatment.%' OR resource_type LIKE 'health_%' OR resource_type LIKE 'clinical_%' OR resource_type LIKE 'treatment_%' THEN 'health'
		WHEN action LIKE 'milk.%' OR resource_type LIKE 'milk_%' THEN 'milk'
		WHEN action LIKE 'procurement.%' OR action LIKE 'source.%' OR action = 'goat.created' OR resource_type IN ('source_load', 'purchase_source') THEN 'procurement'
		WHEN action LIKE 'goat.%' OR action LIKE 'counts.%' OR action LIKE 'census.%' OR resource_type = 'census_count' THEN 'counts'
		WHEN action LIKE 'sop.%' OR action LIKE 'protocol.%' OR action LIKE 'auth.%' OR action LIKE 'app.device.%' OR action LIKE 'notification.%' OR action LIKE 'calendar.%' OR resource_type LIKE 'sop_%' OR resource_type LIKE 'protocol_%' OR resource_type IN ('auth_session', 'workforce_member_device', 'calendar_notification', 'calendar_notification_batch') THEN 'admin'
		ELSE 'other'
	END`
}

func auditModuleSQL() string {
	return `COALESCE(NULLIF(metadata->>'module', ''), CASE
		WHEN action LIKE 'vaccination.%' OR resource_type IN ('vaccination_drive', 'vaccination_drive_assignment') THEN 'vaccination'
		WHEN action LIKE 'feed.packing.%' OR resource_type = 'feed_packing_completion' THEN 'feed_packing'
		WHEN action LIKE 'feed.distribution.%' OR resource_type = 'feed_distribution_completion' THEN 'feed_distribution'
		WHEN action LIKE 'feed.transport.%' OR resource_type = 'feed_transport_task' THEN 'feed_transport'
		WHEN action LIKE 'feed.wastage.%' OR resource_type = 'feed_wastage_completion' THEN 'feed_wastage'
		WHEN action LIKE 'feed.%' OR resource_type LIKE 'feed_%' THEN 'feed'
		WHEN action LIKE 'weighing.%' OR resource_type LIKE 'weighing_%' THEN 'weights'
		WHEN action LIKE 'health.%' OR action LIKE 'clinical.%' OR action LIKE 'treatment.%' OR resource_type LIKE 'health_%' OR resource_type LIKE 'clinical_%' OR resource_type LIKE 'treatment_%' THEN 'health'
		WHEN action LIKE 'milk.%' OR resource_type LIKE 'milk_%' THEN 'milk'
		WHEN action LIKE 'procurement.%' OR action LIKE 'source.%' OR action = 'goat.created' OR resource_type IN ('source_load', 'purchase_source') THEN 'source_entry'
		WHEN action LIKE 'goat.%' OR action LIKE 'counts.%' OR action LIKE 'census.%' OR resource_type = 'census_count' THEN 'herd_register'
		WHEN action LIKE 'sop.%' OR resource_type LIKE 'sop_%' THEN 'sop'
		WHEN action LIKE 'protocol.%' OR resource_type LIKE 'protocol_%' THEN 'protocol'
		WHEN action LIKE 'auth.%' THEN 'auth'
		WHEN action LIKE 'app.device.%' OR resource_type = 'workforce_member_device' THEN 'devices'
		WHEN action LIKE 'notification.%' OR action LIKE 'calendar.%' OR resource_type IN ('calendar_notification', 'calendar_notification_batch') THEN 'notifications'
		ELSE 'other'
	END)`
}

func auditDomain(action, resourceType string) string {
	switch {
	case strings.HasPrefix(action, "vaccination.") || resourceType == "vaccination_drive" || resourceType == "vaccination_drive_assignment":
		return "vaccination"
	case strings.HasPrefix(action, "feed.") || strings.HasPrefix(resourceType, "feed_"):
		return "feed"
	case strings.HasPrefix(action, "weighing.") || strings.HasPrefix(resourceType, "weighing_"):
		return "weighing"
	case strings.HasPrefix(action, "health.") || strings.HasPrefix(action, "clinical.") || strings.HasPrefix(action, "treatment.") || strings.HasPrefix(resourceType, "health_") || strings.HasPrefix(resourceType, "clinical_") || strings.HasPrefix(resourceType, "treatment_"):
		return "health"
	case strings.HasPrefix(action, "milk.") || strings.HasPrefix(resourceType, "milk_"):
		return "milk"
	case strings.HasPrefix(action, "procurement.") || strings.HasPrefix(action, "source.") || action == "goat.created" || resourceType == "source_load" || resourceType == "purchase_source":
		return "procurement"
	case strings.HasPrefix(action, "goat.") || strings.HasPrefix(action, "counts.") || strings.HasPrefix(action, "census.") || resourceType == "census_count":
		return "counts"
	case strings.HasPrefix(action, "sop.") || strings.HasPrefix(action, "protocol.") || strings.HasPrefix(action, "auth.") || strings.HasPrefix(action, "app.device.") || strings.HasPrefix(action, "notification.") || strings.HasPrefix(action, "calendar.") || strings.HasPrefix(resourceType, "sop_") || strings.HasPrefix(resourceType, "protocol_") || resourceType == "auth_session" || resourceType == "workforce_member_device" || resourceType == "calendar_notification" || resourceType == "calendar_notification_batch":
		return "admin"
	default:
		return "other"
	}
}

func auditModule(action, resourceType string) string {
	switch {
	case strings.HasPrefix(action, "vaccination.") || resourceType == "vaccination_drive" || resourceType == "vaccination_drive_assignment":
		return "vaccination"
	case strings.HasPrefix(action, "feed.packing.") || resourceType == "feed_packing_completion":
		return "feed_packing"
	case strings.HasPrefix(action, "feed.distribution.") || resourceType == "feed_distribution_completion":
		return "feed_distribution"
	case strings.HasPrefix(action, "feed.transport.") || resourceType == "feed_transport_task":
		return "feed_transport"
	case strings.HasPrefix(action, "feed.wastage.") || resourceType == "feed_wastage_completion":
		return "feed_wastage"
	case strings.HasPrefix(action, "feed.") || strings.HasPrefix(resourceType, "feed_"):
		return "feed"
	case strings.HasPrefix(action, "weighing.") || strings.HasPrefix(resourceType, "weighing_"):
		return "weights"
	case strings.HasPrefix(action, "health.") || strings.HasPrefix(action, "clinical.") || strings.HasPrefix(action, "treatment.") || strings.HasPrefix(resourceType, "health_") || strings.HasPrefix(resourceType, "clinical_") || strings.HasPrefix(resourceType, "treatment_"):
		return "health"
	case strings.HasPrefix(action, "milk.") || strings.HasPrefix(resourceType, "milk_"):
		return "milk"
	case strings.HasPrefix(action, "procurement.") || strings.HasPrefix(action, "source.") || action == "goat.created" || resourceType == "source_load" || resourceType == "purchase_source":
		return "source_entry"
	case strings.HasPrefix(action, "goat.") || strings.HasPrefix(action, "counts.") || strings.HasPrefix(action, "census.") || resourceType == "census_count":
		return "herd_register"
	case strings.HasPrefix(action, "sop.") || strings.HasPrefix(resourceType, "sop_"):
		return "sop"
	case strings.HasPrefix(action, "protocol.") || strings.HasPrefix(resourceType, "protocol_"):
		return "protocol"
	case strings.HasPrefix(action, "auth."):
		return "auth"
	case strings.HasPrefix(action, "app.device.") || resourceType == "workforce_member_device":
		return "devices"
	case strings.HasPrefix(action, "notification.") || strings.HasPrefix(action, "calendar.") || resourceType == "calendar_notification" || resourceType == "calendar_notification_batch":
		return "notifications"
	default:
		return "other"
	}
}

func awaitingVerificationSQL() string {
	return `(action ILIKE '%verification%' AND lower(COALESCE(metadata->>'status', metadata->>'result', '')) IN ('awaiting', 'awaiting_verification', 'pending', 'proof_pending', 'verification_pending') OR lower(COALESCE(metadata->>'status', metadata->>'result', '')) IN ('awaiting_verification', 'verification_pending'))`
}

func proofGapSQL() string {
	return `(
		(resource_type = 'proof' OR action ILIKE '%proof%' OR action ILIKE '%sop%' OR action ILIKE '%vaccination%' OR lower(COALESCE(metadata->>'proof_required', metadata->>'requires_proof', '')) IN ('true', '1', 'yes'))
		AND COALESCE(metadata->>'proof_id', metadata->>'proof_ref_id', metadata->>'media_proof_id', metadata->>'proof_url', metadata->>'evidence_id', '') = ''
	)`
}
