package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	identityhttp "github.com/vgoats/goatos/backend/internal/identity/adapters/http"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
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

type AuthConfig struct {
	Mode              string
	Issuer            string
	Audience          string
	HS256Secret       string
	MaxTokenTTL       time.Duration
	DevHeadersAllowed bool
	Environment       string
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
		},
	}
}

func NewAPI(ctx context.Context, cfg Config, log *slog.Logger) (*API, error) {
	verifier, err := buildAuthVerifier(cfg.Auth)
	if err != nil {
		return nil, err
	}

	pool, err := platformpg.Connect(ctx, cfg.Postgres)
	if err != nil {
		return nil, err
	}

	identityRepo := identitypg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	identityService := identityapp.NewService(identityRepo)
	identityHandler := identityhttp.NewHandler(identityService)
	reportingRepo := reportingpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	reportingService := reportingapp.NewService(reportingRepo)
	reportingHandler := reportinghttp.NewHandler(reportingService)
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

	handler := httpmiddleware.RequestContext(log)(authz.Wrap(mux))
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

func buildAuthVerifier(cfg AuthConfig) (httpmiddleware.TokenVerifier, error) {
	mode := strings.TrimSpace(cfg.Mode)
	if mode == "" {
		mode = httpmiddleware.AuthModeBearer
	}
	switch mode {
	case httpmiddleware.AuthModeBearer:
		return platformauth.NewHS256Verifier(platformauth.Config{
			Issuer:   cfg.Issuer,
			Audience: cfg.Audience,
			Secret:   []byte(cfg.HS256Secret),
			MaxTTL:   cfg.MaxTokenTTL,
		})
	case httpmiddleware.AuthModeDevHeaders:
		if !cfg.DevHeadersAllowed || !devHeadersEnvironmentAllowed(cfg.Environment) {
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
	return httpmiddleware.NewAuthMiddleware(httpmiddleware.AuthConfig{
		Mode:              mode,
		DevHeadersAllowed: cfg.DevHeadersAllowed,
		Environment:       cfg.Environment,
	}, verifier, grants, log)
}

func devHeadersEnvironmentAllowed(env string) bool {
	env = strings.ToLower(strings.TrimSpace(env))
	return env == "local" || env == "dev" || env == "test"
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
