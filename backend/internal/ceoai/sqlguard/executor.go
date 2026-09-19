package sqlguard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultStatementTimeout bounds a single fallback query at the session level,
// independent of any role-level statement_timeout, so a slow/hostile plan cannot
// pin a connection. Matches the role guidance in mcp-toolbox-plan.md (8000ms).
const DefaultStatementTimeout = 8 * time.Second

// Row is one result row as a column->value map. Values are whatever pgx decodes
// (int64, string, time.Time, bool, []byte, nil, ...).
type Row = map[string]any

// PoolConfig describes the dedicated read-only pool for the fallback executor.
// It intentionally does NOT reuse the app's DATABASE_URL/app role: the fallback
// must connect as mesha_ceo_readonly, which has read-only (SELECT-only) grants
// and no write/DDL. As of migration 000359 (D0, plan-v3) this role holds SELECT
// on ceo_ai.* ONLY — the 000031 public.* grant is revoked so the blast radius of
// any guard bypass is the governed reporting views, never a raw operator table.
// Query scope is still constrained by this SQL guard (single SELECT, tenant
// predicate, LIMIT, ceo_ai allowlist) independently of the grants. See
// mcp-toolbox-plan.md "Database Role".
type PoolConfig struct {
	// DatabaseURL is a full DSN for the mesha_ceo_readonly role. When empty it is
	// assembled from the MESHA_MCP_DB_* / MESHA_DATABASE_NAME env vars.
	DatabaseURL string
	MaxConns    int32
	// StatementTimeout is applied per transaction via SET LOCAL.
	StatementTimeout time.Duration
	// ConnectTimeout bounds pool creation / initial ping.
	ConnectTimeout time.Duration
}

// PoolConfigFromEnv builds a read-only pool config from Mesha assistant env
// names without embedding any secret value in code. Password comes only from the
// environment (sourced from Secret Manager at deploy/local-fetch time).
func PoolConfigFromEnv() PoolConfig {
	return PoolConfig{
		DatabaseURL:      firstEnv("MESHA_CEO_READONLY_DATABASE_URL", "MESHA_CEO_READONLY_DB_URL", "MESHA_MCP_DB_DSN"),
		MaxConns:         envInt32("MESHA_CEO_READONLY_MAX_CONNS", 4),
		StatementTimeout: envDuration("MESHA_CEO_READONLY_STATEMENT_TIMEOUT", DefaultStatementTimeout),
		ConnectTimeout:   envDuration("MESHA_CEO_READONLY_CONNECT_TIMEOUT", 5*time.Second),
	}
}

// dsn assembles a DSN from discrete MESHA_MCP_DB_* parts when no full URL is
// supplied. Returns "" if the required parts are missing.
func (c PoolConfig) dsn() string {
	if c.DatabaseURL != "" {
		return c.DatabaseURL
	}
	user := os.Getenv("MESHA_MCP_DB_USER")
	pass := os.Getenv("MESHA_MCP_DB_PASSWORD")
	host := envOr("MESHA_MCP_DB_HOST", "127.0.0.1")
	port := envOr("MESHA_MCP_DB_PORT", "5433")
	dbname := envOr("MESHA_DATABASE_NAME", os.Getenv("MESHA_MCP_DB_NAME"))
	if user == "" || dbname == "" {
		return ""
	}
	// Force read-only + a safe search_path at the connection level as well.
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?default_transaction_read_only=on&options=-c%%20search_path%%3D%s",
		user, pass, host, port, dbname, AllowedSchema,
	)
}

// Executor runs validated read-only SQL on a dedicated non-privileged pool.
type Executor struct {
	pool             *pgxpool.Pool
	statementTimeout time.Duration
}

// NewExecutor opens the read-only pool described by cfg and verifies it. The
// caller owns Close.
func NewExecutor(ctx context.Context, cfg PoolConfig) (*Executor, error) {
	dsn := cfg.dsn()
	if dsn == "" {
		return nil, errors.New("sqlguard: read-only DB config missing (set MESHA_CEO_READONLY_DATABASE_URL, MESHA_CEO_READONLY_DB_URL, MESHA_MCP_DB_DSN, or MESHA_MCP_DB_USER/MESHA_DATABASE_NAME)")
	}
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlguard: parse read-only config: %w", err)
	}
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = 4
	}
	poolCfg.MaxConns = cfg.MaxConns

	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(cctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("sqlguard: open read-only pool: %w", err)
	}
	if err := pool.Ping(cctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("sqlguard: ping read-only pool: %w", err)
	}
	st := cfg.StatementTimeout
	if st <= 0 {
		st = DefaultStatementTimeout
	}
	return &Executor{pool: pool, statementTimeout: st}, nil
}

// NewExecutorWithPool wraps an already-constructed read-only pool. Useful for
// tests and for callers that manage pool lifecycle centrally. The pool MUST be
// connected as a non-privileged read-only role.
func NewExecutorWithPool(pool *pgxpool.Pool, statementTimeout time.Duration) *Executor {
	if statementTimeout <= 0 {
		statementTimeout = DefaultStatementTimeout
	}
	return &Executor{pool: pool, statementTimeout: statementTimeout}
}

// Close releases the pool.
func (e *Executor) Close() {
	if e.pool != nil {
		e.pool.Close()
	}
}

// ErrTenantBinding is returned when a draft's tenant predicate does not bind the
// caller's session tenant. It is a hard reject: the query is never executed.
var ErrTenantBinding = errors.New("sqlguard: tenant predicate does not bind the session tenant")

// ExecuteReadOnlyForTenant is the ONLY entry point the tier-4 SQL fallback port
// should use for MODEL-DRAFTED SQL. It binds tenant server-side: after Validate
// passes (single bounded tenant-scoped SELECT over ceo_ai.*, no top-level OR),
// it extracts EVERY tenant-scoped predicate literal in the statement
// (ExtractAllTenantPredicates) and requires each one to equal sessionTenantID
// byte-for-byte. The tenant value therefore comes from the server session, never
// from the planner's (user-influenced) output — a draft carrying any other tenant
// UUID anywhere (a second `AND tenant_id = '<victim>'`, a `<>`/`IN`/`IS`/column
// or param form, a bare `tenant_id` in the projection) is rejected with
// ErrTenantBinding and never runs. Combined with the validator's rejection of
// top-level OR, JOIN, comma-join, comments, `$` and any nested subquery, there is
// no scope left in which a second predicate can hide (D0, plan-v3).
func (e *Executor) ExecuteReadOnlyForTenant(ctx context.Context, sessionTenantID, sql string) ([]Row, error) {
	if strings.TrimSpace(sessionTenantID) == "" {
		return nil, ErrTenantBinding
	}
	if err := Validate(sql); err != nil {
		return nil, err
	}
	literals, err := ExtractAllTenantPredicates(sql)
	if err != nil {
		return nil, ErrTenantBinding
	}
	for _, got := range literals {
		if got != sessionTenantID {
			return nil, ErrTenantBinding
		}
	}
	return e.execValidated(ctx, sql)
}

// ExecuteTrustedReadOnlyForTenant runs SERVER-AUTHORED read-only SQL. It is NOT
// for model-drafted SQL: callers must only pass deterministic SQL assembled by
// backend code. The tenant is ALWAYS bound as $1 from the session — the SQL text
// must not carry a tenant literal at all — and callers pass args for everything
// else, which are bound as $2.. in order. The statement is rejected when any
// tenant-scoped predicate binds a string literal, a parameter other than $1, or
// anything but another tenant column (an equi-join), and when no `tenant_id = $1`
// occurrence exists. Statement stacking and comments are rejected as well. The
// same read-only transaction, statement timeout and hard row cap apply.
func (e *Executor) ExecuteTrustedReadOnlyForTenant(ctx context.Context, tenantID, sql string, args ...any) ([]Row, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrTenantBinding
	}
	if err := ValidateTrusted(sql); err != nil {
		return nil, err
	}
	bound := make([]any, 0, len(args)+1)
	bound = append(bound, tenantID)
	bound = append(bound, args...)
	return e.execValidatedArgs(ctx, sql, bound)
}

// ValidateTrusted is the trusted-SQL policy check used by
// ExecuteTrustedReadOnlyForTenant: a single SELECT, no stacking/comments, every
// tenant-scoped predicate bound to $1 (or joined column-to-column), at least one
// `tenant_id = $1`. Exported so trusted queries can be asserted at build time in
// tests without a pool.
func ValidateTrusted(sql string) error {
	stmt := strings.TrimSpace(sql)
	if stmt == "" {
		return ErrEmpty
	}
	if !strings.HasPrefix(strings.ToUpper(stmt), "SELECT") {
		return rejit("trusted query must begin with SELECT")
	}
	if strings.Contains(stmt, ";") || strings.Contains(stmt, "--") || strings.Contains(stmt, "/*") || strings.Contains(stmt, "*/") {
		return rejit("trusted query contains disallowed statement/comment syntax")
	}
	return trustedTenantParamCheck(stmt, DefaultTenantScopedColumns)
}

// ExecuteReadOnly validates sql, then runs it inside a READ ONLY transaction with
// a bounded statement timeout, wrapping the query so the result can never exceed
// MaxRowLimit rows even if the LIMIT clause were somehow bypassed. Validation is
// re-run here (never trust a caller to have validated) so this method is safe as
// a standalone entry point.
func (e *Executor) ExecuteReadOnly(ctx context.Context, sql string) ([]Row, error) {
	if err := Validate(sql); err != nil {
		return nil, err
	}
	return e.execValidated(ctx, sql)
}

// execValidated assumes sql already passed Validate.
func (e *Executor) execValidated(ctx context.Context, sql string) ([]Row, error) {
	return e.execValidatedArgs(ctx, sql, nil)
}

// execValidatedArgs assumes sql already passed Validate/ValidateTrusted. args
// are bound positionally ($1..); model SQL never carries any.
func (e *Executor) execValidatedArgs(ctx context.Context, sql string, args []any) ([]Row, error) {
	if e.pool == nil {
		return nil, errors.New("sqlguard: executor has no pool")
	}
	conn, err := e.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("sqlguard: acquire conn: %w", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{
		AccessMode: pgx.ReadOnly,
		IsoLevel:   pgx.ReadCommitted,
	})
	if err != nil {
		return nil, fmt.Errorf("sqlguard: begin read-only tx: %w", err)
	}
	// Always roll back: a read-only tx has nothing to commit, and rollback
	// guarantees no state leaks even on the happy path.
	defer func() { _ = tx.Rollback(ctx) }()

	// Session-local statement timeout, independent of role defaults. SET LOCAL is
	// scoped to this transaction. The value is an integer literal (milliseconds),
	// never interpolated from user input.
	ms := int(e.statementTimeout / time.Millisecond)
	if ms <= 0 {
		ms = int(DefaultStatementTimeout / time.Millisecond)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = "+strconv.Itoa(ms)); err != nil {
		return nil, fmt.Errorf("sqlguard: set statement_timeout: %w", err)
	}
	// Belt-and-suspenders: reaffirm read-only at the transaction level.
	if _, err := tx.Exec(ctx, "SET LOCAL transaction_read_only = on"); err != nil {
		return nil, fmt.Errorf("sqlguard: set transaction_read_only: %w", err)
	}

	// Wrap the validated statement as a derived table and re-cap the row count.
	// The validated SQL is a single flat SELECT with its own LIMIT<=100; this
	// outer LIMIT is a hard ceiling that holds regardless.
	wrapped := "SELECT * FROM (" + sql + ") AS ceo_ai_fallback LIMIT " + strconv.Itoa(MaxRowLimit)

	rows, err := tx.Query(ctx, wrapped, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlguard: query: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	out := make([]Row, 0, 16)
	for rows.Next() {
		vals, verr := rows.Values()
		if verr != nil {
			return nil, fmt.Errorf("sqlguard: scan row: %w", verr)
		}
		row := make(Row, len(fields))
		for i, fd := range fields {
			row[string(fd.Name)] = vals[i]
		}
		out = append(out, row)
		if len(out) >= MaxRowLimit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlguard: read rows: %w", err)
	}
	return out, nil
}

// --- env helpers (local to the package; no secret values) ---

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func envInt32(key string, def int32) int32 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return int32(n)
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}
