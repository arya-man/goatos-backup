package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/sqlbind"
)

// Connection warm-up.
//
// Every heavy read statement pays a large one-off cost the first time it runs on a NEW backend
// connection: parse/analyze of a ~2,000-line CTE, a cold relcache/catcache, and -- for statements run
// under GenericPlanReadTx -- building the generic plan itself (STG, 2026-09-25: the calendar
// single-event CTE took ~430 ms on a fresh connection vs ~80 ms once warm; calendar history 166 vs
// 40 ms). Without warm-up the first request on every pooled connection paid that, once per statement.
//
// ConnWarmer moves that cost off the request path:
//   - AfterConnect only marks the new connection cold (it never blocks the Acquire that dialed it);
//   - a background loop picks cold connections up while they sit idle (AcquireAllIdle), runs every
//     registered ConnWarmup on them one connection at a time, then releases them;
//   - Config.WarmIdleConns (pgxpool MinIdleConns) keeps a spare idle connection, dialed by the pool's
//     own health check, so the connection a request picks up has normally been warmed already.
//
// Plan modes follow the call sites (see GenericPlanReadTx):
//   - WarmGenericPlan statements execute once inside a GenericPlanReadTx, which builds and caches the
//     generic plan in pgx's per-connection prepared statement exactly like the request path;
//   - WarmCachedStatement statements are prepared through the pgx statement cache and executed once
//     (plan_cache_mode stays auto, so the request path keeps choosing custom vs generic as before);
//   - WarmCustomPlan statements run once as an unnamed statement (nothing cached; the call site pins a
//     custom plan with QueryExecModeExec / DescribeExec) and only warm the backend's catalog caches.
// Arguments are all NULL unless the warm-up says otherwise, so the executions read (next to) nothing;
// each one runs in its own READ ONLY transaction under a short statement_timeout.
//
// Warm-up is best effort: a failing statement is logged and skipped, a connection that breaks is
// dropped by the pool as usual, and the whole pass per connection is bounded by WarmupBudget.

// WarmMode says how a ConnWarmup executes its statement; it must match the request call site.
type WarmMode int

const (
	// WarmCachedStatement: default pgx mode (statement cache), plan_cache_mode untouched.
	WarmCachedStatement WarmMode = iota
	// WarmGenericPlan: statement cache inside GenericPlanReadTx (force_generic_plan).
	WarmGenericPlan
	// WarmCustomPlan: unnamed statement, custom plan -- catalog caches only (Args must be strings).
	WarmCustomPlan
)

// ConnWarmup is one statement to run on every new pooled connection.
type ConnWarmup struct {
	Name string
	SQL  string
	Mode WarmMode
	// Args overrides positional arguments (index 0 = $1); every other parameter is NULL. Pass
	// WarmupTenantID for the tenant parameter: a NULL tenant can defeat `$n IS NULL OR ...` guards and
	// turn the warm-up execution into a scan, while a tenant that does not exist reads nothing.
	Args map[int]any
	// Begin overrides the warm-up transaction's BEGIN (it must open a READ ONLY transaction) when the
	// call site plans under extra SET LOCALs: a generic plan is cached with the settings in force when
	// it is built, so the warm-up must build it under the same ones.
	Begin string
}

// WarmupTenantID is a tenant id that never exists (the nil UUID), for ConnWarmup.Args.
const WarmupTenantID = "00000000-0000-0000-0000-000000000000"

// TenantFirst is ConnWarmup.Args for statements whose $1 is the tenant id.
func TenantFirst() map[int]any { return map[int]any{0: WarmupTenantID} }

const (
	// WarmupStatementTimeout bounds one warm-up statement (planning + execution).
	WarmupStatementTimeout = 3 * time.Second
	// WarmupBudget bounds the warm-up pass of one connection.
	WarmupBudget = 15 * time.Second
	warmupRescan = 10 * time.Second
)

// ConnWarmer tracks connections that have not been warmed yet and warms them in the background.
type ConnWarmer struct {
	log *slog.Logger

	mu      sync.Mutex
	warmups []ConnWarmup
	cold    map[*pgx.Conn]struct{}
	started bool
	wake    chan struct{}
}

// NewConnWarmer returns an empty warmer; pass it in Config.Warmer before Connect, Register the
// statements, then Start it.
func NewConnWarmer(log *slog.Logger) *ConnWarmer {
	return &ConnWarmer{log: log, cold: map[*pgx.Conn]struct{}{}, wake: make(chan struct{}, 1)}
}

// Register adds warm-ups. Connections that are already warm do not re-run them.
func (w *ConnWarmer) Register(ws ...ConnWarmup) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.warmups = append(w.warmups, ws...)
	w.mu.Unlock()
}

func (w *ConnWarmer) install(poolCfg *pgxpool.Config, warmIdleConns int32) {
	prevAfter := poolCfg.AfterConnect
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		if prevAfter != nil {
			if err := prevAfter(ctx, conn); err != nil {
				return err
			}
		}
		w.markCold(conn)
		return nil
	}
	// A connection dialed by a request is cold while that request uses it; wake the loop when it
	// comes back so it is warmed before the next request picks it up.
	prevRelease := poolCfg.AfterRelease
	poolCfg.AfterRelease = func(conn *pgx.Conn) bool {
		if w.isCold(conn) {
			w.poke()
		}
		if prevRelease != nil {
			return prevRelease(conn)
		}
		return true
	}
	prevClose := poolCfg.BeforeClose
	poolCfg.BeforeClose = func(conn *pgx.Conn) {
		w.mu.Lock()
		delete(w.cold, conn)
		w.mu.Unlock()
		if prevClose != nil {
			prevClose(conn)
		}
	}
	if warmIdleConns > 0 {
		if warmIdleConns > poolCfg.MaxConns-1 {
			warmIdleConns = max(poolCfg.MaxConns-1, 0)
		}
		poolCfg.MinIdleConns = warmIdleConns
	}
}

func (w *ConnWarmer) markCold(conn *pgx.Conn) {
	w.mu.Lock()
	w.cold[conn] = struct{}{}
	w.mu.Unlock()
	w.poke()
}

func (w *ConnWarmer) poke() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *ConnWarmer) isCold(conn *pgx.Conn) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.cold[conn]
	return ok
}

// Start runs the background warm-up loop until ctx is done. Safe to call once; later calls no-op.
func (w *ConnWarmer) Start(ctx context.Context, pool *pgxpool.Pool) {
	if w == nil || pool == nil {
		return
	}
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.mu.Unlock()
	go w.loop(ctx, pool)
}

func (w *ConnWarmer) loop(ctx context.Context, pool *pgxpool.Pool) {
	ticker := time.NewTicker(warmupRescan)
	defer ticker.Stop()
	for {
		// Warm one cold idle connection per pass so at most one pooled connection is held at a time.
		for ctx.Err() == nil && w.warmOneIdle(ctx, pool) {
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// warmOneIdle warms one idle cold connection and reports whether it found one.
func (w *ConnWarmer) warmOneIdle(ctx context.Context, pool *pgxpool.Pool) bool {
	w.mu.Lock()
	anyCold := len(w.cold) > 0
	w.mu.Unlock()
	if !anyCold {
		return false
	}
	var target *pgxpool.Conn
	for _, c := range pool.AcquireAllIdle(ctx) {
		if target == nil && w.isCold(c.Conn()) {
			target = c
			continue
		}
		c.Release()
	}
	if target == nil {
		return false
	}
	defer target.Release()
	w.WarmConn(ctx, target.Conn())
	return true
}

// WarmConn runs every registered warm-up on conn (exported for tests and probes) and marks it warm.
func (w *ConnWarmer) WarmConn(ctx context.Context, conn *pgx.Conn) {
	w.mu.Lock()
	warmups := append([]ConnWarmup(nil), w.warmups...)
	delete(w.cold, conn)
	w.mu.Unlock()
	if len(warmups) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, WarmupBudget)
	defer cancel()
	start := time.Now()
	failed := 0
	for _, wu := range warmups {
		if ctx.Err() != nil || conn.IsClosed() {
			failed++
			continue
		}
		t0 := time.Now()
		if err := runWarmup(ctx, conn, wu); err != nil {
			failed++
			if w.log != nil {
				w.log.Warn("postgres_conn_warmup_statement_failed",
					slog.String("warmup", wu.Name),
					slog.Duration("duration", time.Since(t0)),
					slog.String("error", err.Error()))
			}
			continue
		}
		if w.log != nil {
			w.log.Debug("postgres_conn_warmup_statement",
				slog.String("warmup", wu.Name),
				slog.Duration("duration", time.Since(t0)))
		}
	}
	if w.log != nil {
		w.log.Info("postgres_conn_warmup",
			slog.Int("statements", len(warmups)),
			slog.Int("failed", failed),
			slog.Duration("duration", time.Since(start)))
	}
}

func runWarmup(ctx context.Context, conn *pgx.Conn, wu ConnWarmup) error {
	ctx, cancel := context.WithTimeout(ctx, WarmupStatementTimeout)
	defer cancel()
	nParams, err := warmupParamCount(wu.SQL)
	if err != nil {
		return err
	}
	args := make([]any, nParams)
	for i := range args {
		args[i] = wu.Args[i]
	}
	bound, err := sqlbind.Bind(wu.SQL, args...)
	if err != nil {
		return err
	}
	opts := pgx.TxOptions{AccessMode: pgx.ReadOnly, BeginQuery: "BEGIN READ ONLY; SET LOCAL statement_timeout = '3s'"}
	if wu.Mode == WarmGenericPlan {
		opts.BeginQuery = GenericPlanReadTx.BeginQuery + "; SET LOCAL statement_timeout = '3s'"
	}
	if wu.Begin != "" {
		opts.BeginQuery = wu.Begin + "; SET LOCAL statement_timeout = '3s'"
	}
	tx, err := conn.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if wu.Mode == WarmCustomPlan {
		// Unnamed statement, text parameters typed by the server: a one-off custom plan, nothing
		// left in the pgx statement cache -- the same shape as the call sites' QueryExecModeExec /
		// QueryExecModeDescribeExec.
		values := make([][]byte, len(args))
		for i, a := range args {
			switch v := a.(type) {
			case nil:
			case string:
				values[i] = []byte(v)
			default:
				return fmt.Errorf("postgres: custom-plan warm-up %s: argument $%d must be a string or nil", wu.Name, i+1)
			}
		}
		_, err := tx.Conn().PgConn().ExecParams(ctx, bound.SQL(), values, nil, nil, nil).Close()
		return err
	}
	rows, err := tx.Query(ctx, bound.SQL(), bound.Args()...)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	rows.Close()
	return rows.Err()
}

// warmupParamCount is the highest $n placeholder in sql (parsed locally, no round trip).
func warmupParamCount(sql string) (int, error) {
	ordinals, err := sqlbind.PlaceholderOrdinals(sql)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range ordinals {
		n = max(n, o)
	}
	return n, nil
}
