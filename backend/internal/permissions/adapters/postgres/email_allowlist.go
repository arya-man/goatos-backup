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

	mu       sync.Mutex
	byTenant map[string]tenantEmailCache
}

type tenantEmailCache struct {
	emails   map[string]struct{}
	loadedAt time.Time
}

func NewAllowedEmailSource(pool *pgxpool.Pool, timeout time.Duration, log *slog.Logger) *AllowedEmailSource {
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if log == nil {
		log = slog.Default()
	}
	return &AllowedEmailSource{
		pool:     pool,
		timeout:  timeout,
		ttl:      30 * time.Second,
		log:      log,
		now:      time.Now,
		byTenant: map[string]tenantEmailCache{},
	}
}

var _ authallow.DynamicEmailSource = (*AllowedEmailSource)(nil)

// EmailAllowed implements authallow.DynamicEmailSource.
func (s *AllowedEmailSource) EmailAllowed(ctx context.Context, tenantID, normalizedEmail string) bool {
	if tenantID == "" || normalizedEmail == "" {
		return false
	}
	set := s.activeSet(ctx, tenantID)
	_, ok := set[normalizedEmail]
	return ok
}

func (s *AllowedEmailSource) activeSet(ctx context.Context, tenantID string) map[string]struct{} {
	s.mu.Lock()
	entry, hasLoaded := s.byTenant[tenantID]
	fresh := hasLoaded && s.now().Sub(entry.loadedAt) < s.ttl
	cached := entry.emails
	s.mu.Unlock()
	if fresh {
		return cached
	}

	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer cancel()
	rows, err := s.pool.Query(loadCtx, `
SELECT normalized_email
FROM auth_allowed_emails
WHERE tenant_id = $1::uuid AND status = 'active'`, tenantID)
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
	s.byTenant[tenantID] = tenantEmailCache{emails: loaded, loadedAt: s.now()}
	s.mu.Unlock()
	return loaded
}
