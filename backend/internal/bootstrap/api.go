package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	identityhttp "github.com/vgoats/goatos/backend/internal/identity/adapters/http"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	legacysynchttp "github.com/vgoats/goatos/backend/internal/legacy_sync/adapters/http"
	legacysyncpg "github.com/vgoats/goatos/backend/internal/legacy_sync/adapters/postgres"
	legacysyncapp "github.com/vgoats/goatos/backend/internal/legacy_sync/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	reportinghttp "github.com/vgoats/goatos/backend/internal/reporting/adapters/http"
	reportingpg "github.com/vgoats/goatos/backend/internal/reporting/adapters/postgres"
	reportingapp "github.com/vgoats/goatos/backend/internal/reporting/app"
)

type Config struct {
	HTTPAddr string
	Postgres platformpg.Config
	Auth     AuthConfig
}

// AuthModeJWKS selects asymmetric RS256/ES256 token verification via a remote
// JWKS endpoint. This is the intended production auth mode. Requires
// GOATOS_AUTH_ISSUER, GOATOS_AUTH_AUDIENCE, and GOATOS_AUTH_JWKS_URL.
const AuthModeJWKS = "jwks"

type AuthConfig struct {
	Mode              string
	Issuer            string
	Audience          string
	HS256Secret       string
	MaxTokenTTL       time.Duration
	DevHeadersAllowed bool
	Environment       string
	// JWKS mode fields.
	JWKSUrl      string
	ClockSkew    time.Duration
	AllowedAlgs  []string
	JWKSCacheTTL time.Duration
}

type API struct {
	Server *http.Server
	Close  func()
}

func ConfigFromEnv() Config {
	addr := os.Getenv("GOATOS_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	return Config{
		HTTPAddr: addr,
		Postgres: platformpg.ConfigFromEnv(),
		Auth: AuthConfig{
			Mode:              os.Getenv("GOATOS_AUTH_MODE"),
			Issuer:            os.Getenv("GOATOS_AUTH_ISSUER"),
			Audience:          os.Getenv("GOATOS_AUTH_AUDIENCE"),
			HS256Secret:       os.Getenv("GOATOS_AUTH_HS256_SECRET"),
			MaxTokenTTL:       authMaxTokenTTLFromEnv(),
			DevHeadersAllowed: strings.EqualFold(os.Getenv("GOATOS_DEV_HEADERS_ALLOW"), "true"),
			Environment:       os.Getenv("GOATOS_ENV"),
			// JWKS mode env vars.
			JWKSUrl:      os.Getenv("GOATOS_AUTH_JWKS_URL"),
			ClockSkew:    authDurationFromEnv("GOATOS_AUTH_CLOCK_SKEW"),
			AllowedAlgs:  authAllowedAlgsFromEnv(),
			JWKSCacheTTL: authDurationFromEnv("GOATOS_AUTH_JWKS_CACHE_TTL"),
		},
	}
}

func NewAPI(ctx context.Context, cfg Config, log *slog.Logger) (*API, error) {
	verifier, err := buildAuthVerifier(cfg.Auth, log)
	if err != nil {
		return nil, err
	}

	pool, err := platformpg.Connect(ctx, cfg.Postgres)
	if err != nil {
		return nil, err
	}

	identityRepo := identitypg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	identityService := identityapp.NewService(identityRepo)
	identityHandler := identityhttp.NewHandler(identityService, log)
	reportingRepo := reportingpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	reportingService := reportingapp.NewService(reportingRepo)
	reportingHandler := reportinghttp.NewHandler(reportingService, log)
	legacySyncRepo := legacysyncpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	legacySyncService := legacysyncapp.NewService(legacySyncRepo, cfg.Auth.Environment)
	legacySyncHandler := legacysynchttp.NewHandler(legacySyncService, log)
	grantSource := permissionspg.NewGrantSource(pool, cfg.Postgres.QueryTimeout)
	authz, err := buildAuthMiddleware(cfg.Auth, verifier, grantSource, log)
	if err != nil {
		pool.Close()
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := identityRepo.Ping(r.Context()); err != nil {
			http.Error(w, "postgres not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	identityhttp.Register(mux, identityHandler)
	reportinghttp.Register(mux, reportingHandler)
	legacysynchttp.Register(mux, legacySyncHandler)

	// PanicRecovery is outermost so it catches panics in auth and RequestContext.
	handler := httpmiddleware.PanicRecovery(log)(httpmiddleware.RequestContext(log)(authz.Wrap(mux)))
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return &API{
		Server: server,
		Close:  pool.Close,
	}, nil
}

func buildAuthVerifier(cfg AuthConfig, log *slog.Logger) (httpmiddleware.TokenVerifier, error) {
	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" {
		mode = httpmiddleware.AuthModeBearer
	}
	switch mode {
	case httpmiddleware.AuthModeBearer:
		// HS256 bearer is a local/dev convenience only. Warn loudly when used
		// in a non-development environment so operators know to switch to jwks.
		if log != nil && !httpmiddleware.DevHeadersEnvironmentAllowed(cfg.Environment) {
			log.Warn("HS256 bearer auth mode is not a production auth mode; set GOATOS_AUTH_MODE=jwks for production deployments")
		}
		return platformauth.NewHS256Verifier(platformauth.Config{
			Issuer:   cfg.Issuer,
			Audience: cfg.Audience,
			Secret:   []byte(cfg.HS256Secret),
			MaxTTL:   cfg.MaxTokenTTL,
		})
	case AuthModeJWKS:
		if strings.TrimSpace(cfg.JWKSUrl) == "" {
			return nil, fmt.Errorf("%w: GOATOS_AUTH_JWKS_URL is required for jwks mode", httpmiddleware.ErrInvalidAuthConfig)
		}
		return platformauth.NewJWKSVerifier(platformauth.JWKSConfig{
			JWKSURL:     cfg.JWKSUrl,
			Issuer:      cfg.Issuer,
			Audience:    cfg.Audience,
			AllowedAlgs: cfg.AllowedAlgs,
			ClockSkew:   cfg.ClockSkew,
			MaxTTL:      cfg.MaxTokenTTL,
			CacheTTL:    cfg.JWKSCacheTTL,
		})
	case httpmiddleware.AuthModeDevHeaders:
		if !cfg.DevHeadersAllowed || !httpmiddleware.DevHeadersEnvironmentAllowed(cfg.Environment) {
			return nil, httpmiddleware.ErrInvalidAuthConfig
		}
		return nil, nil
	default:
		return nil, httpmiddleware.ErrInvalidAuthConfig
	}
}

func buildAuthMiddleware(cfg AuthConfig, verifier httpmiddleware.TokenVerifier, grants permissions.GrantSource, log *slog.Logger) (*httpmiddleware.AuthMiddleware, error) {
	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" {
		mode = httpmiddleware.AuthModeBearer
	}
	// jwks is a verifier-selection concern; the middleware treats it the same
	// as bearer (token presented in Authorization: Bearer header, verified via
	// the injected TokenVerifier).
	if mode == AuthModeJWKS {
		mode = httpmiddleware.AuthModeBearer
	}
	return httpmiddleware.NewAuthMiddleware(httpmiddleware.AuthConfig{
		Mode:              mode,
		DevHeadersAllowed: cfg.DevHeadersAllowed,
		Environment:       cfg.Environment,
	}, verifier, grants, log)
}

func authMaxTokenTTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("GOATOS_AUTH_MAX_TOKEN_TTL"))
	if raw == "" {
		return platformauth.DefaultMaxTokenTTL
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil {
		return -1
	}
	if ttl <= 0 {
		return -1
	}
	return ttl
}

// authDurationFromEnv parses a time.Duration from the named environment
// variable. Returns zero (not an error) when the variable is unset.
func authDurationFromEnv(envKey string) time.Duration {
	raw := strings.TrimSpace(os.Getenv(envKey))
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

// authAllowedAlgsFromEnv parses GOATOS_AUTH_ALLOWED_ALGS as a comma-separated
// list of algorithm names (e.g. "RS256,ES256"). Returns nil when unset, which
// causes JWKSVerifier to use its default allow-list.
func authAllowedAlgsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("GOATOS_AUTH_ALLOWED_ALGS"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	algs := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			algs = append(algs, p)
		}
	}
	if len(algs) == 0 {
		return nil
	}
	return algs
}
