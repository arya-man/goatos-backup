package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// questionHash is the sha256 of the normalized question text. It satisfies the
// NOT NULL question_hash column and keeps raw question text out of indexes.
func questionHash(q string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(q))))
	return hex.EncodeToString(sum[:])
}

// ErrTraceNotFound is returned when no trace matches (wrong request_id, wrong
// tenant, or not yet written). It is deliberately indistinguishable from a
// cross-tenant miss so a probing admin in tenant A cannot tell "exists in B"
// from "does not exist".
var ErrTraceNotFound = errors.New("ceoai/observability: trace not found")

const defaultQueryTimeout = 3 * time.Second

// TraceStore is the internal step-trace port. RecordTrace is called by the
// orchestrator at request end; GetTrace backs the admin-only debug endpoint.
// Every method is tenant-scoped from the server session — never user text.
type TraceStore interface {
	RecordTrace(ctx context.Context, t TraceRecord) error
	GetTrace(ctx context.Context, tenantID, requestID string) (TraceRecord, error)
}

// PostgresTraceStore persists traces to ceo_ai_assistant_audit (migration
// 000021). Redaction is enforced on write via TraceRecord.Sanitize so a secret
// can never land in the internal store.
type PostgresTraceStore struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

var _ TraceStore = (*PostgresTraceStore)(nil)

// NewPostgresTraceStore binds a store to a pgx pool.
func NewPostgresTraceStore(pool *pgxpool.Pool, queryTimeout time.Duration) *PostgresTraceStore {
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}
	return &PostgresTraceStore{pool: pool, queryTimeout: queryTimeout}
}

func (s *PostgresTraceStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.queryTimeout)
}

// RecordTrace inserts one internal audit/trace row. The step trace is stored as
// jsonb; every scalar is cast to its column type so the plan stays SARGable.
func (s *PostgresTraceStore) RecordTrace(ctx context.Context, t TraceRecord) error {
	t = t.Sanitize()
	steps, err := json.Marshal(t.Steps)
	if err != nil {
		return err
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	var convID any
	if t.ConversationID != "" {
		convID = t.ConversationID
	}
	var actorRole any
	if t.ActorRole != "" {
		actorRole = t.ActorRole
	}
	// source_views is text[] NOT NULL DEFAULT '{}'. A nil Go slice binds as SQL
	// NULL and fails the NOT NULL constraint, which — because the caller
	// (AuditTraceSink.Record) ignores the error — silently drops EVERY audit
	// row and leaves the admin trace endpoint returning 404. Bind an empty
	// array instead so the insert succeeds.
	if t.SourceViews == nil {
		t.SourceViews = []string{}
	}

	const q = `
INSERT INTO ceo_ai_assistant_audit (
    tenant_id, actor_role, conversation_id, request_id, question_hash,
    question_redacted, route_tier, tool_called, source_views, row_count,
    latency_ms, status, rejection_reason, step_trace, review_verdict,
    model_version, prompt_version
) VALUES (
    $1::uuid, $2, $3::uuid, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, $14::jsonb, $15,
    $16, $17
)`
	_, err = s.pool.Exec(ctx, q,
		t.TenantID, actorRole, convID, t.RequestID, questionHash(t.QuestionRedacted),
		t.QuestionRedacted, t.RouteTier, t.ToolCalled, t.SourceViews, t.RowCount,
		t.LatencyMS, t.Status, t.RejectionReason, steps, t.ReviewVerdict,
		t.ModelVersion, t.PromptVersion,
	)
	return err
}

// GetTrace fetches the most recent trace for (tenant, request_id). It reads via
// the ceo_ai_assistant_audit_request_idx index (tenant_id, request_id).
func (s *PostgresTraceStore) GetTrace(ctx context.Context, tenantID, requestID string) (TraceRecord, error) {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	const q = `
SELECT actor_role, conversation_id, question_redacted, route_tier, tool_called,
       source_views, row_count, latency_ms, status, rejection_reason,
       step_trace, review_verdict, model_version, prompt_version, created_at
FROM ceo_ai_assistant_audit
WHERE tenant_id = $1::uuid AND request_id = $2
ORDER BY created_at DESC, audit_id DESC
LIMIT 1`
	row := s.pool.QueryRow(ctx, q, tenantID, requestID)

	var (
		rec       TraceRecord
		actorRole *string
		convID    *string
		rejection *string
		verdict   *string
		modelV    *string
		promptV   *string
		steps     []byte
	)
	err := row.Scan(
		&actorRole, &convID, &rec.QuestionRedacted, &rec.RouteTier, &rec.ToolCalled,
		&rec.SourceViews, &rec.RowCount, &rec.LatencyMS, &rec.Status, &rejection,
		&steps, &verdict, &modelV, &promptV, &rec.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return TraceRecord{}, ErrTraceNotFound
	}
	if err != nil {
		return TraceRecord{}, err
	}
	rec.RequestID = requestID
	rec.TenantID = tenantID
	rec.ActorRole = deref(actorRole)
	rec.ConversationID = deref(convID)
	rec.RejectionReason = deref(rejection)
	rec.ReviewVerdict = deref(verdict)
	rec.ModelVersion = deref(modelV)
	rec.PromptVersion = deref(promptV)
	if len(steps) > 0 {
		_ = json.Unmarshal(steps, &rec.Steps)
	}
	// Sanitize on read too: defense-in-depth against a pre-redaction row.
	return rec.Sanitize(), nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// MemoryTraceStore is an in-process, bounded trace store for tests and degraded
// (no-Postgres) local runs. It keeps the last capacity traces per tenant.
type MemoryTraceStore struct {
	mu       sync.RWMutex
	capacity int
	byTenant map[string]map[string]TraceRecord
	order    map[string][]string // insertion order of request_ids per tenant
}

var _ TraceStore = (*MemoryTraceStore)(nil)

// NewMemoryTraceStore builds a bounded in-memory store. capacity<=0 defaults to
// 256 traces per tenant.
func NewMemoryTraceStore(capacity int) *MemoryTraceStore {
	if capacity <= 0 {
		capacity = 256
	}
	return &MemoryTraceStore{
		capacity: capacity,
		byTenant: make(map[string]map[string]TraceRecord),
		order:    make(map[string][]string),
	}
}

// RecordTrace stores a sanitized copy, evicting the oldest when at capacity.
func (m *MemoryTraceStore) RecordTrace(_ context.Context, t TraceRecord) error {
	t = t.Sanitize()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = time.Now()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	tid := t.TenantID
	if m.byTenant[tid] == nil {
		m.byTenant[tid] = make(map[string]TraceRecord)
	}
	if _, exists := m.byTenant[tid][t.RequestID]; !exists {
		m.order[tid] = append(m.order[tid], t.RequestID)
		for len(m.order[tid]) > m.capacity {
			oldest := m.order[tid][0]
			m.order[tid] = m.order[tid][1:]
			delete(m.byTenant[tid], oldest)
		}
	}
	m.byTenant[tid][t.RequestID] = t
	return nil
}

// GetTrace returns the trace for (tenant, request_id) or ErrTraceNotFound.
func (m *MemoryTraceStore) GetTrace(_ context.Context, tenantID, requestID string) (TraceRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if byReq, ok := m.byTenant[tenantID]; ok {
		if rec, ok := byReq[requestID]; ok {
			return rec, nil
		}
	}
	return TraceRecord{}, ErrTraceNotFound
}
