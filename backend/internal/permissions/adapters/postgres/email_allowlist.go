package postgres

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/authallow"
)

// AllowedEmailSource is the DB-backed half of the auth email allowlist: the
// active rows of auth_allowed_emails, written by the workforce create-person
// transaction and consulted by the auth middleware IN UNION with the env list
// (authallow.AllowsWithDynamic). It removes the Secret Manager + Cloud Run
// revision step from onboarding.
//
// The whole active set is loaded in one query behind a short in-process TTL —
// the table holds one row per staff login, so the load is a few hundred rows at
// most. On a load failure the source serves its last-known-good set (an auth
// path must not flap on a transient DB hiccup) and fails CLOSED when it has
// never loaded: the env list still works, so a cold-start DB outage degrades to
// exactly today's behavior.
type AllowedEmailSource struct {
	pool    *pgxpool.Pool
	timeout time.Duration
	ttl     time.Duration
	log     *slog.Logger
	now     func() time.Time

	mu        sync.Mutex
	emails    map[string]struct{}
	loadedAt  time.Time
	hasLoaded bool
}

func NewAllowedEmailSource(pool *pgxpool.Pool, timeout time.Duration, log *slog.Logger) *AllowedEmailSource {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &AllowedEmailSource{
		pool:    pool,
		timeout: timeout,
		ttl:     30 * time.Second,
		log:     log,
		now:     time.Now,
	}
}

var _ authallow.DynamicEmailSource = (*AllowedEmailSource)(nil)

// EmailAllowed implements authallow.DynamicEmailSource.
func (s *AllowedEmailSource) EmailAllowed(ctx context.Context, normalizedEmail string) bool {
	if normalizedEmail == "" {
		return false
	}
	set := s.activeSet(ctx)
	_, ok := set[normalizedEmail]
	return ok
}

func (s *AllowedEmailSource) activeSet(ctx context.Context) map[string]struct{} {
	s.mu.Lock()
	fresh := s.hasLoaded && s.now().Sub(s.loadedAt) < s.ttl
	cached := s.emails
	s.mu.Unlock()
	if fresh {
		return cached
	}

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()
	rows, err := s.pool.Query(loadCtx, `
SELECT normalized_email
FROM auth_allowed_emails
WHERE status = 'active'`)
	if err != nil {
		s.log.Warn("auth_allowed_emails_load_failed", slog.Any("error", err))
		return cached
	}
	loaded := map[string]struct{}{}
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			rows.Close()
			s.log.Warn("auth_allowed_emails_scan_failed", slog.Any("error", err))
			return cached
		}
		loaded[email] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		s.log.Warn("auth_allowed_emails_load_failed", slog.Any("error", err))
		return cached
	}

	s.mu.Lock()
	s.emails = loaded
	s.loadedAt = s.now()
	s.hasLoaded = true
	s.mu.Unlock()
	return loaded
}
