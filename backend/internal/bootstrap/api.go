package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	countshttp "github.com/vgoats/goatos/backend/internal/counts/adapters/http"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	identityhttp "github.com/vgoats/goatos/backend/internal/identity/adapters/http"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	legacysynchttp "github.com/vgoats/goatos/backend/internal/legacy_sync/adapters/http"
	legacysyncpg "github.com/vgoats/goatos/backend/internal/legacy_sync/adapters/postgres"
	legacysyncapp "github.com/vgoats/goatos/backend/internal/legacy_sync/app"
	locationshttp "github.com/vgoats/goatos/backend/internal/locations/adapters/http"
	locationspg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	mortalityhttp "github.com/vgoats/goatos/backend/internal/mortality/adapters/http"
	mortalitypg "github.com/vgoats/goatos/backend/internal/mortality/adapters/postgres"
	mortalityapp "github.com/vgoats/goatos/backend/internal/mortality/app"
	obligationhttp "github.com/vgoats/goatos/backend/internal/obligation/adapters/http"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	passporthttp "github.com/vgoats/goatos/backend/internal/passport/adapters/http"
	passportapp "github.com/vgoats/goatos/backend/internal/passport/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	protocolhttp "github.com/vgoats/goatos/backend/internal/protocol/adapters/http"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	reportinghttp "github.com/vgoats/goatos/backend/internal/reporting/adapters/http"
	reportingpg "github.com/vgoats/goatos/backend/internal/reporting/adapters/postgres"
	reportingapp "github.com/vgoats/goatos/backend/internal/reporting/app"
	sophttp "github.com/vgoats/goatos/backend/internal/sop/adapters/http"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	vaccinationhttp "github.com/vgoats/goatos/backend/internal/vaccination/adapters/http"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	workforcehttp "github.com/vgoats/goatos/backend/internal/workforce/adapters/http"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
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

const defaultAuthSessionRateLimitPerMinute = 120

type AuthConfig struct {
	Mode                             string
	Issuer                           string
	Audience                         string
	HS256Secret                      string
	MaxTokenTTL                      time.Duration
	DevHeadersAllowed                bool
	Environment                      string
	AllowedEmails                    []string
	AuthSessionAllowedTenantIDs      []string
	AuthSessionRateLimitPerMinute    int
	AuthSessionRateLimitInvalidValue bool
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
			Mode:                             os.Getenv("GOATOS_AUTH_MODE"),
			Issuer:                           os.Getenv("GOATOS_AUTH_ISSUER"),
			Audience:                         os.Getenv("GOATOS_AUTH_AUDIENCE"),
			HS256Secret:                      os.Getenv("GOATOS_AUTH_HS256_SECRET"),
			MaxTokenTTL:                      authMaxTokenTTLFromEnv(),
			DevHeadersAllowed:                strings.EqualFold(os.Getenv("GOATOS_DEV_HEADERS_ALLOW"), "true"),
			Environment:                      os.Getenv("GOATOS_ENV"),
			AllowedEmails:                    authStringListFromEnv("GOATOS_AUTH_ALLOWED_EMAILS"),
			AuthSessionAllowedTenantIDs:      authStringListFromEnv("GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS"),
			AuthSessionRateLimitPerMinute:    authSessionRateLimitFromEnv(),
			AuthSessionRateLimitInvalidValue: authSessionRateLimitInvalidFromEnv(),
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
	authAuditOptions, err := buildAuthAuditOptions(cfg.Auth)
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
	locationsRepo := locationspg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	locationsService := locationsapp.NewService(locationsRepo)
	locationsHandler := locationshttp.NewHandler(locationsService, log)
	countsRepo := countspg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	countsService := countsapp.NewServiceWithResolver(countsRepo, locationsService)
	countsHandler := countshttp.NewHandler(countsService, log)
	mortalityRepo := mortalitypg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	mortalityService := mortalityapp.NewServiceWithResolver(mortalityRepo, locationsService)
	mortalityHandler := mortalityhttp.NewHandler(mortalityService, log)
	legacySyncRepo := legacysyncpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	legacySyncService := legacysyncapp.NewService(legacySyncRepo, cfg.Auth.Environment)
	legacySyncHandler := legacysynchttp.NewHandler(legacySyncService, log)
	workforceRepo := workforcepg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	workforceService := workforceapp.NewService(workforceRepo)
	workforceHandler := workforcehttp.NewHandler(workforceService, log)
	sopRepo := soppg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	sopService := sopapp.NewService(sopRepo)
	sopHandler := sophttp.NewHandler(sopService, log)

	protocolRepo := protocolpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	protocolService := protocolapp.NewService(protocolRepo)
	protocolHandler := protocolhttp.NewHandler(protocolService, log)
	obligationRepo := obligationpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	obligationService := obligationapp.NewService(obligationRepo)
	obligationHandler := obligationhttp.NewHandler(obligationService, log)
	vaccinationService := vaccinationapp.NewService(vaccinationpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	inventoryService := inventoryapp.NewService(inventorypg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService).
		WithBooster(vaccinationapp.NewBoosterService(protocolRepo, obligationRepo))
	vaccinationHandler := vaccinationhttp.NewHandler(vaccinationService, vaccinationCompletion, log)
	passportService := passportapp.NewService(vaccinationService, obligationRepo)
	passportHandler := passporthttp.NewHandler(passportService, log)
	grantSource := permissionspg.NewGrantSource(pool, cfg.Postgres.QueryTimeout)
	authAuditRecorder := authaudit.NewPostgresRecorder(pool, cfg.Postgres.QueryTimeout)
	authAuditOptions = append(authAuditOptions, authaudit.WithPendingEmailGrantClaimer(
		permissionspg.NewPendingEmailGrantClaimer(pool, cfg.Postgres.QueryTimeout),
	))
	authAuditHandler := authaudit.NewHandler(verifier, authAuditRecorder, log, authAuditOptions...)
	authz, err := buildAuthMiddleware(cfg.Auth, verifier, grantSource, log)
	if err != nil {
		pool.Close()
		return nil, err
	}

	protectedMux := http.NewServeMux()
	protectedMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	protectedMux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	protectedMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := identityRepo.Ping(r.Context()); err != nil {
			http.Error(w, "postgres not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	identityhttp.Register(protectedMux, identityHandler)
	reportinghttp.Register(protectedMux, reportingHandler)
	countshttp.Register(protectedMux, countsHandler)
	mortalityhttp.Register(protectedMux, mortalityHandler)
	locationshttp.Register(protectedMux, locationsHandler)
	legacysynchttp.Register(protectedMux, legacySyncHandler)
	workforcehttp.Register(protectedMux, workforceHandler)
	sophttp.Register(protectedMux, sopHandler)
	protocolhttp.Register(protectedMux, protocolHandler)
	vaccinationhttp.Register(protectedMux, vaccinationHandler)
	obligationhttp.Register(protectedMux, obligationHandler)
	passporthttp.Register(protectedMux, passportHandler)

	mux := http.NewServeMux()
	authaudit.Register(mux, authAuditHandler)
	mux.Handle("/", authz.Wrap(protectedMux))

	// PanicRecovery is outermost so it catches panics in auth and RequestContext.
	handler := httpmiddleware.PanicRecovery(log)(httpmiddleware.RequestContext(log)(mux))
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
		// HS256 bearer is a local/dev/test convenience only. Shared
		// environments must use jwks so a misconfigured deployment fails closed.
		if !httpmiddleware.DevHeadersEnvironmentAllowed(cfg.Environment) {
			if log != nil {
				log.Error("HS256 bearer auth mode is not allowed outside local/dev/test; set GOATOS_AUTH_MODE=jwks")
			}
			return nil, fmt.Errorf("%w: GOATOS_AUTH_MODE=bearer is allowed only when GOATOS_ENV is local/dev/test", httpmiddleware.ErrInvalidAuthConfig)
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
	if err := validateAuthEmailAllowlist(cfg); err != nil {
		return nil, err
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
		AllowedEmails:     cfg.AllowedEmails,
	}, verifier, grants, log)
}

func buildAuthAuditOptions(cfg AuthConfig) ([]authaudit.Option, error) {
	if cfg.AuthSessionRateLimitInvalidValue || cfg.AuthSessionRateLimitPerMinute < 0 {
		return nil, fmt.Errorf("%w: GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE must be a non-negative integer", httpmiddleware.ErrInvalidAuthConfig)
	}
	for _, tenantID := range cfg.AuthSessionAllowedTenantIDs {
		if !uuidutil.IsUUIDString(tenantID) {
			return nil, fmt.Errorf("%w: GOATOS_AUTH_SESSION_ALLOWED_TENANT_IDS must contain only UUIDs", httpmiddleware.ErrInvalidAuthConfig)
		}
	}
	if err := validateAuthEmailAllowlist(cfg); err != nil {
		return nil, err
	}
	options := []authaudit.Option{
		authaudit.WithAllowedTenantIDs(cfg.AuthSessionAllowedTenantIDs),
		authaudit.WithAllowedEmails(cfg.AllowedEmails),
	}
	if cfg.AuthSessionRateLimitPerMinute > 0 {
		options = append(options, authaudit.WithRateLimiter(authaudit.NewRateLimiter(
			cfg.AuthSessionRateLimitPerMinute,
			time.Minute,
			4096,
		)))
	}
	return options, nil
}

func validateAuthEmailAllowlist(cfg AuthConfig) error {
	set, err := authallow.NewEmailSet(cfg.AllowedEmails)
	if err != nil {
		return fmt.Errorf("%w: GOATOS_AUTH_ALLOWED_EMAILS must contain only valid email addresses", httpmiddleware.ErrInvalidAuthConfig)
	}
	if authEmailAllowlistRequired(cfg) && len(set) == 0 {
		return fmt.Errorf("%w: GOATOS_AUTH_ALLOWED_EMAILS is required when GOATOS_AUTH_MODE=jwks", httpmiddleware.ErrInvalidAuthConfig)
	}
	return nil
}

func authEmailAllowlistRequired(cfg AuthConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Mode), AuthModeJWKS)
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

func authStringListFromEnv(envKey string) []string {
	raw := strings.TrimSpace(os.Getenv(envKey))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}

func authSessionRateLimitFromEnv() int {
	raw := strings.TrimSpace(os.Getenv("GOATOS_AUTH_SESSION_RATE_LIMIT_PER_MINUTE"))
	if raw == "" {
		return defaultAuthSessionRateLimitPerMinute
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return -1
	}
	return value
}

func authSessionRateLimitInvalidFromEnv() bool {
	return authSessionRateLimitFromEnv() < 0
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
