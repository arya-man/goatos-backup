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
	out.AsOf = time.Now().UTC()
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
		where = append(where, fmt.Sprintf("metadata->>'domain' = $%d", len(args)))
	}
	if q.Module != nil {
		args = append(args, *q.Module)
		where = append(where, fmt.Sprintf("metadata->>'module' = $%d", len(args)))
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
	if q.Cursor != nil {
		args = append(args, q.Cursor.RecordedAt, q.Cursor.AuditID)
		where = append(where, fmt.Sprintf("(recorded_at, audit_id) < ($%d::timestamptz, $%d::uuid)", len(args)-1, len(args)))
	}
	if q.AnomaliesOnly {
		where = append(where, anomalySQL())
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

func awaitingVerificationSQL() string {
	return `(action ILIKE '%verification%' AND lower(COALESCE(metadata->>'status', metadata->>'result', '')) IN ('awaiting', 'awaiting_verification', 'pending', 'proof_pending', 'verification_pending') OR lower(COALESCE(metadata->>'status', metadata->>'result', '')) IN ('awaiting_verification', 'verification_pending'))`
}
