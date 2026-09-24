package bootstrap

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

// connectAuthPool opens the small dedicated pool that POST /auth/session-events
// (audit write, pending-grant claim, dynamic email allowlist) runs on, so heavy
// dashboard reads that fill the main pool cannot starve sign-in.
//
// It connects LAZILY: a failed boot ping (Postgres at max_connections during a
// rollout) is logged and counted but never stops the instance from starting --
// the main pool already proved the database is reachable, and the auth pool
// dials again on first use.
func connectAuthPool(ctx context.Context, cfg platformpg.Config, log *slog.Logger) (*pgxpool.Pool, error) {
	pool, pingErr, err := platformpg.ConnectLazy(ctx, platformpg.AuthPoolConfig(cfg))
	if err != nil {
		return nil, err
	}
	if pingErr != nil {
		platformpg.RecordPoolBootPingFailure(ctx, "auth")
		if log != nil {
			log.Warn("postgres_auth_pool_boot_ping_failed", slog.String("error", pingErr.Error()))
		}
	}
	return pool, nil
}

// newAuthSessionHandler wires POST /auth/session-events exactly as newAPI serves it.
func newAuthSessionHandler(authPool *pgxpool.Pool, cfg platformpg.Config, verifier authaudit.TokenVerifier, options []authaudit.Option, log *slog.Logger) *authaudit.Handler {
	timeout := platformpg.AuthPoolConfig(cfg).QueryTimeout
	recorder := authaudit.NewPostgresRecorder(authPool, timeout)
	options = append(append([]authaudit.Option(nil), options...),
		authaudit.WithPendingEmailGrantClaimer(permissionspg.NewPendingEmailGrantClaimer(authPool, timeout)),
		authaudit.WithDynamicAllowedEmails(permissionspg.NewAllowedEmailSource(authPool, timeout, log)),
	)
	options = append(options, authaudit.WithRequestDeadline(timeout))
	return authaudit.NewHandler(verifier, recorder, log, options...)
}
