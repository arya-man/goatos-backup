package audit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultTimeout = 3 * time.Second

type Event struct {
	TenantID     string
	ActorID      string
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	ScopeType    string
	ScopeID      string
	DecisionID   string
	BeforeState  any
	AfterState   any
	Metadata     map[string]any
	TraceID      string
}

type Recorder interface {
	Record(ctx context.Context, event Event) error
	InTx(tx pgx.Tx) TxRecorder
}

type TxRecorder interface {
	Record(ctx context.Context, event Event) error
}

type PostgresRecorder struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewPostgresRecorder(pool *pgxpool.Pool, timeout time.Duration) *PostgresRecorder {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &PostgresRecorder{pool: pool, timeout: timeout}
}

func (r *PostgresRecorder) Record(ctx context.Context, event Event) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return record(ctx, r.pool, event)
}

func (r *PostgresRecorder) InTx(tx pgx.Tx) TxRecorder {
	return NewTxRecorder(tx)
}

type txRecorder struct {
	tx pgx.Tx
}

func NewTxRecorder(tx pgx.Tx) TxRecorder {
	return txRecorder{tx: tx}
}

func (r txRecorder) Record(ctx context.Context, event Event) error {
	return record(ctx, r.tx, event)
}

type executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func record(ctx context.Context, exec executor, event Event) error {
	if event.ActorType == "" {
		return errors.New("audit: actor_type is required")
	}
	if event.Action == "" {
		return errors.New("audit: action is required")
	}
	if event.ResourceType == "" {
		return errors.New("audit: resource_type is required")
	}
	beforeState, err := marshalOptionalJSON(event.BeforeState)
	if err != nil {
		return err
	}
	afterState, err := marshalOptionalJSON(event.AfterState)
	if err != nil {
		return err
	}
	metadata := event.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = exec.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id,
  actor_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  scope_type,
  scope_id,
  decision_id,
  before_state,
  after_state,
  metadata,
  trace_id
) VALUES (
  nullif($1::text, '')::uuid,
  nullif($2::text, '')::uuid,
  $3,
  $4,
  $5,
  nullif($6::text, '')::uuid,
  nullif($7::text, ''),
  nullif($8::text, '')::uuid,
  nullif($9::text, '')::uuid,
  $10::jsonb,
  $11::jsonb,
  $12::jsonb,
  nullif($13::text, '')
)`,
		event.TenantID,
		event.ActorID,
		event.ActorType,
		event.Action,
		event.ResourceType,
		event.ResourceID,
		event.ScopeType,
		event.ScopeID,
		event.DecisionID,
		beforeState,
		afterState,
		metadataBytes,
		event.TraceID,
	)
	return err
}

func marshalOptionalJSON(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case json.RawMessage:
		return validJSONBytes([]byte(typed))
	case []byte:
		return validJSONBytes(typed)
	default:
		return json.Marshal(value)
	}
}

func validJSONBytes(value []byte) ([]byte, error) {
	if len(value) == 0 {
		return nil, nil
	}
	if !json.Valid(value) {
		return nil, errors.New("audit: state must be valid json")
	}
	return value, nil
}
