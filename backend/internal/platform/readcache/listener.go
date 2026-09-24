package readcache

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Listener applies committed-write evictions from sibling API instances.
//
// WHY LISTEN/NOTIFY ON A DEDICATED CONNECTION (not a short TTL alone, not a pooled conn):
//   - A TTL alone means every sibling instance serves pre-write numbers for the whole TTL after a
//     close/correction/rework, so the TTL has to stay ~30 s and the heavy reads stay cold. With a
//     commit-ordered NOTIFY the entries can live longer and be served stale-while-revalidate,
//     because a write reaches every instance within one round trip.
//   - LISTEN pins its session. On a pooled conn that is one of the request pool's slots held
//     forever (and on db-g1-small the pool is small); a separate pgx.Conn built from the pool's
//     own config costs one server connection per instance and never competes with requests.
//   - Safety net: while the listener is disconnected the caches are marked NOT coherent (30 s
//     TTL, no stale serving), and every (re)connect evicts everything, because notifications sent
//     while we were not listening are lost.
type Listener struct {
	connect func(ctx context.Context) (*pgx.Conn, error)
	caches  []*Cache
	log     *slog.Logger
	// healthEvery bounds how long a silently dead TCP session can keep the caches coherent, and
	// pingTimeout bounds the liveness probe itself: on a half-open TCP session an unbounded Ping
	// would block forever while the caches stayed "coherent" and kept serving stale entries.
	healthEvery time.Duration
	pingTimeout time.Duration

	firstConnect     func(context.Context)
	firstConnectOnce sync.Once
}

// OnFirstConnect registers fn to run once, in the background, right after the FIRST successful
// LISTEN -- i.e. after the connect-time evict-all -- so a warm-up it starts cannot be wiped by it.
func (l *Listener) OnFirstConnect(fn func(context.Context)) {
	l.firstConnect = fn
}

// connected is the connect-time transition: evict everything (notifications sent while we were
// not listening are lost), mark the caches coherent, then run the first-connect hook once.
func (l *Listener) connected(ctx context.Context) {
	for _, c := range l.caches {
		c.EvictAll(ctx)
	}
	l.setCoherent(true)
	if l.firstConnect != nil {
		l.firstConnectOnce.Do(func() { go l.firstConnect(ctx) })
	}
}

// ping runs one liveness probe bounded by pingTimeout; any failure (including the timeout) is a
// disconnect.
func (l *Listener) ping(ctx context.Context, probe func(context.Context) error) error {
	timeout := l.pingTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return probe(pctx)
}

func NewListener(pool *pgxpool.Pool, log *slog.Logger, caches ...*Cache) *Listener {
	if log == nil {
		log = slog.Default()
	}
	l := &Listener{caches: caches, log: log, healthEvery: 15 * time.Second, pingTimeout: 3 * time.Second}
	if pool != nil {
		cfg := pool.Config().ConnConfig.Copy()
		l.connect = func(ctx context.Context) (*pgx.Conn, error) { return pgx.ConnectConfig(ctx, cfg) }
	}
	return l
}

// Start runs the listen loop in the background and returns immediately.
func (l *Listener) Start(ctx context.Context) {
	if l == nil || l.connect == nil || len(l.caches) == 0 {
		return
	}
	go l.loop(ctx)
}

func (l *Listener) setCoherent(v bool) {
	for _, c := range l.caches {
		c.SetCoherent(v)
	}
}

func (l *Listener) loop(ctx context.Context) {
	backoff := time.Second
	for { // scale-guard:ignore: reconnect loop with exponential backoff, one connection at a time
		err := l.listenOnce(ctx)
		l.setCoherent(false)
		if ctx.Err() != nil {
			return
		}
		l.log.Warn("read_cache_evict_listener_disconnected", "error", errString(err))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (l *Listener) listenOnce(ctx context.Context) error {
	conn, err := l.connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))
	if _, err := conn.Exec(ctx, "LISTEN goatos_read_cache_evict"); err != nil {
		return err
	}
	// Anything cached before this point may have missed a notification.
	l.connected(ctx)
	l.log.Info("read_cache_evict_listener_started", "channel", NotifyChannel)
	for { // scale-guard:ignore: LISTEN loop on one dedicated connection; one blocking wait per notification, not a per-row query
		waitCtx, cancel := context.WithTimeout(ctx, l.healthEvery)
		n, err := conn.WaitForNotification(waitCtx) // scale-guard:ignore: LISTEN loop on one dedicated connection, one blocking wait per notification
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				if pingErr := l.ping(ctx, conn.Ping); pingErr != nil { // scale-guard:ignore: liveness probe at most once per healthEvery on the idle LISTEN connection
					return pingErr
				}
				continue
			}
			return err
		}
		l.apply(ctx, n.Payload)
	}
}

func (l *Listener) apply(ctx context.Context, payload string) {
	p, ok := parsePayload(payload)
	if !ok {
		for _, c := range l.caches {
			c.EvictAll(ctx)
		}
		return
	}
	for _, c := range l.caches {
		if !p.affects(c.Name()) {
			continue
		}
		c.Evict(ctx, p.TenantID, p.ParkIDs...)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// StartWarmup runs warm functions once, in the background, sequentially, after delay, each with
// its own timeout, so instance start never blocks on (or floods the pool with) warm reads. A
// failure is logged and ignored: warm-up is an optimisation, never a readiness gate.
func (c *Cache) StartWarmup(ctx context.Context, log *slog.Logger, delay, perTask time.Duration, warm ...func(context.Context) error) {
	if c == nil || len(warm) == 0 {
		return
	}
	if log == nil {
		log = slog.Default()
	}
	c.warmOnce.Do(func() {
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			for i, fn := range warm {
				if ctx.Err() != nil {
					return
				}
				taskCtx, cancel := context.WithTimeout(ctx, perTask)
				err := fn(taskCtx)
				cancel()
				if err != nil {
					log.Warn("read_cache_warmup_failed", "cache", c.opts.Name, "task", i, "error", err.Error())
				}
			}
			log.Info("read_cache_warmup_done", "cache", c.opts.Name, "entries", c.Stats().Entries)
		}()
	})
}

// Jitter returns base plus a random share of spread, so instances started together (a deploy,
// a scale-out) do not all issue their warm-up reads in the same second.
func Jitter(base, spread time.Duration) time.Duration {
	if spread <= 0 {
		return base
	}
	return base + time.Duration(rand.Int64N(int64(spread)))
}

// WithHealthCheck overrides how often an idle LISTEN connection is probed and how long one probe
// may take (defaults 15 s / 3 s). Tests use it to exercise many probe cycles quickly.
func (l *Listener) WithHealthCheck(every, pingTimeout time.Duration) *Listener {
	if every > 0 {
		l.healthEvery = every
	}
	if pingTimeout > 0 {
		l.pingTimeout = pingTimeout
	}
	return l
}
