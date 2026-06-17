package authaudit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Event struct {
	TenantID     string
	ActorID      string
	ActorType    string
	Action       string
	ResourceType string
	ScopeType    string
	ScopeID      string
	Metadata     map[string]any
	TraceID      string
}

type PostgresRecorder struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewPostgresRecorder(pool *pgxpool.Pool, timeout time.Duration) *PostgresRecorder {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &PostgresRecorder{pool: pool, timeout: timeout}
}

func (r *PostgresRecorder) Record(ctx context.Context, event Event) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id,
  actor_id,
  actor_type,
  action,
  resource_type,
  scope_type,
  scope_id,
  metadata,
  trace_id
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  $7,
  $8::jsonb,
  $9
)`,
		nullableString(event.TenantID),
		nullableString(event.ActorID),
		event.ActorType,
		event.Action,
		event.ResourceType,
		nullableString(event.ScopeType),
		nullableString(event.ScopeID),
		metadataBytes,
		nullableString(event.TraceID),
	)
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
