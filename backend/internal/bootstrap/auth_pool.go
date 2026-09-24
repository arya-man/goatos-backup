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
// dashboard reads that fill the main pool cannot starve sign-in. The main pool
// is never shared with it.
func connectAuthPool(ctx context.Context, cfg platformpg.Config, _ *pgxpool.Pool) (*pgxpool.Pool, error) {
	return platformpg.Connect(ctx, platformpg.AuthPoolConfig(cfg))
}

// newAuthSessionHandler wires POST /auth/session-events exactly as newAPI serves it.
func newAuthSessionHandler(authPool *pgxpool.Pool, cfg platformpg.Config, verifier authaudit.TokenVerifier, options []authaudit.Option, log *slog.Logger) *authaudit.Handler {
	timeout := platformpg.AuthPoolConfig(cfg).QueryTimeout
	recorder := authaudit.NewPostgresRecorder(authPool, timeout)
	options = append(append([]authaudit.Option(nil), options...),
		authaudit.WithPendingEmailGrantClaimer(permissionspg.NewPendingEmailGrantClaimer(authPool, timeout)),
		authaudit.WithDynamicAllowedEmails(permissionspg.NewAllowedEmailSource(authPool, timeout, log)),
	)
	return authaudit.NewHandler(verifier, recorder, log, options...)
}
