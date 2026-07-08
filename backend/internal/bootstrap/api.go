package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	adminuihttp "github.com/vgoats/goatos/backend/internal/adminui/adapters/http"
	adminuipg "github.com/vgoats/goatos/backend/internal/adminui/adapters/postgres"
	adminuiapp "github.com/vgoats/goatos/backend/internal/adminui/app"
	bulkstatushttp "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/http"
	bulkstatuspg "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/postgres"
	bulkstatusapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	calendarhttp "github.com/vgoats/goatos/backend/internal/calendar/adapters/http"
	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	feedhttp "github.com/vgoats/goatos/backend/internal/feed/adapters/http"
	feedpg "github.com/vgoats/goatos/backend/internal/feed/adapters/postgres"
	feedapp "github.com/vgoats/goatos/backend/internal/feed/app"
	identityhttp "github.com/vgoats/goatos/backend/internal/identity/adapters/http"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	locationshttp "github.com/vgoats/goatos/backend/internal/locations/adapters/http"
	locationspg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	operationsaudithttp "github.com/vgoats/goatos/backend/internal/operationsaudit/adapters/http"
	operationsauditpg "github.com/vgoats/goatos/backend/internal/operationsaudit/adapters/postgres"
	operationsauditapp "github.com/vgoats/goatos/backend/internal/operationsaudit/app"
	outboxhttp "github.com/vgoats/goatos/backend/internal/outbox/adapters/http"
	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	passporthttp "github.com/vgoats/goatos/backend/internal/passport/adapters/http"
	passportapp "github.com/vgoats/goatos/backend/internal/passport/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
	platformaudit "github.com/vgoats/goatos/backend/internal/platform/audit"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	processintegrityhttp "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/http"
	processintegritypg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	processintegrityapp "github.com/vgoats/goatos/backend/internal/processintegrity/app"
	procurementhttp "github.com/vgoats/goatos/backend/internal/procurement/adapters/http"
	procurementpg "github.com/vgoats/goatos/backend/internal/procurement/adapters/postgres"
	procurementapp "github.com/vgoats/goatos/backend/internal/procurement/app"
	proofhttp "github.com/vgoats/goatos/backend/internal/proof/adapters/http"
	proofpg "github.com/vgoats/goatos/backend/internal/proof/adapters/postgres"
	proofgcs "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/gcs"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofports "github.com/vgoats/goatos/backend/internal/proof/ports"
	protocolhttp "github.com/vgoats/goatos/backend/internal/protocol/adapters/http"
	protocolpg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protocolapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	sophttp "github.com/vgoats/goatos/backend/internal/sop/adapters/http"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccinationhttp "github.com/vgoats/goatos/backend/internal/vaccination/adapters/http"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexechttp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/http"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	workforcehttp "github.com/vgoats/goatos/backend/internal/workforce/adapters/http"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

type Config struct {
	HTTPAddr                    string
	Postgres                    platformpg.Config
	Auth                        AuthConfig
	BulkImportPreviewSigningKey string
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
		HTTPAddr:                    addr,
		Postgres:                    platformpg.ConfigFromEnv(),
		BulkImportPreviewSigningKey: os.Getenv("GOATOS_BULK_IMPORT_PREVIEW_SIGNING_KEY"),
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

type gcsServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

func buildProofStorage() (proofports.Storage, error) {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_MEDIA_STORAGE")))
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	if mode == "" {
		if !localProofStorageAllowed(env) {
			return nil, fmt.Errorf("GOATOS_MEDIA_STORAGE must be set to gcs for GOATOS_ENV=%q", env)
		}
		mode = "local"
	}
	switch mode {
	case "local":
		if !localProofStorageAllowed(env) {
			return nil, fmt.Errorf("GOATOS_MEDIA_STORAGE=local is only allowed for local/test environments")
		}
		return prooflocal.New(os.Getenv("GOATOS_LOCAL_MEDIA_DIR"), os.Getenv("GOATOS_LOCAL_MEDIA_SIGNING_SECRET")), nil
	case "gcs":
		bucket := strings.TrimSpace(os.Getenv("GOATOS_GCS_BUCKET"))
		email := strings.TrimSpace(os.Getenv("GOATOS_GCS_CLIENT_EMAIL"))
		privateKey := os.Getenv("GOATOS_GCS_PRIVATE_KEY")
		if raw := strings.TrimSpace(os.Getenv("GOATOS_GCS_SERVICE_ACCOUNT_JSON")); raw != "" {
			var sa gcsServiceAccount
			if err := json.Unmarshal([]byte(raw), &sa); err != nil {
				return nil, fmt.Errorf("invalid GOATOS_GCS_SERVICE_ACCOUNT_JSON: %w", err)
			}
			if email == "" {
				email = sa.ClientEmail
			}
			if privateKey == "" {
				privateKey = sa.PrivateKey
			}
		}
		storage, err := proofgcs.New(bucket, email, privateKey)
		if err != nil {
			return nil, fmt.Errorf("proof GCS storage: %w", err)
		}
		return storage, nil
	default:
		return nil, fmt.Errorf("unsupported GOATOS_MEDIA_STORAGE %q", mode)
	}
}

func localProofStorageAllowed(env string) bool {
	switch env {
	case "", "local", "test", "development":
		return true
	default:
		return false
	}
}

func NewAPI(ctx context.Context, cfg Config, log *slog.Logger) (*API, error) {
	bulkPreviewSigningKey, err := bulkImportPreviewSigningKey(cfg)
	if err != nil {
		return nil, err
	}
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
	identityService := identityapp.NewService(identityRepo).WithBulkPreviewSigningKey(bulkPreviewSigningKey)
	identityHandler := identityhttp.NewHandler(identityService, log)
	bulkStatusRepo := bulkstatuspg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	bulkStatusService := bulkstatusapp.NewService(bulkStatusRepo, bulkStatusRepo).WithSigningKey(bulkPreviewSigningKey)
	bulkStatusHandler := bulkstatushttp.NewHandler(bulkStatusService, log)
	locationsRepo := locationspg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	locationsService := locationsapp.NewService(locationsRepo)
	locationsHandler := locationshttp.NewHandler(locationsService, log)
	workforceRepo := workforcepg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	workforceService := workforceapp.NewService(workforceRepo)
	workforceHandler := workforcehttp.NewHandler(workforceService, log)
	proofStorage, err := buildProofStorage()
	if err != nil {
		pool.Close()
		return nil, err
	}
	proofService := proofapp.NewService(proofpg.NewRepository(pool, cfg.Postgres.QueryTimeout), proofStorage)
	proofHandler := proofhttp.NewHandler(proofService, log)
	sopRepo := soppg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	sopService := sopapp.NewService(sopRepo).WithProofValidator(proofService)

	protocolRepo := protocolpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	businessAuditRecorder := platformaudit.NewPostgresRecorder(pool, cfg.Postgres.QueryTimeout)
	outboxRepo := outboxpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	outboxHandler := outboxhttp.NewHandler(outboxRepo, businessAuditRecorder, log)
	operationsAuditService := operationsauditapp.NewService(operationsauditpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	operationsAuditHandler := operationsaudithttp.NewHandler(operationsAuditService, log)
	processIntegrityService := processintegrityapp.NewService(processintegritypg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	processIntegrityHandler := processintegrityhttp.NewHandler(processIntegrityService, log)
	vaccExecService := vaccexecapp.NewService(vaccexecpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	vaccExecHandler := vaccexechttp.NewHandler(vaccExecService, log)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	calendarHandler := calendarhttp.NewHandler(calendarService, log)
	adminUIHandler := adminuihttp.NewHandler(adminuiapp.NewService(adminuipg.NewRepository(pool, cfg.Postgres.QueryTimeout)))
	countsService := countsapp.NewService(countspg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	feedService := feedapp.NewService(feedpg.NewRepository(pool, cfg.Postgres.QueryTimeout)).
		WithCountsReadiness(countsService).
		WithCountsProjectionProvider(countsService).
		WithCountsProjectionExceptionResolver(countsService).
		WithCountsProjectionExceptionLister(countsService)
	feedHandler := feedhttp.NewHandler(feedService, log)
	procurementService := procurementapp.NewService(procurementpg.NewRepository(pool, cfg.Postgres.QueryTimeout)).WithVaccinationCanceler(obligationRepo)
	procurementHandler := procurementhttp.NewHandler(procurementService, log)
	vaccinationRepo := vaccinationpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	vaccinationService := vaccinationapp.NewService(vaccinationRepo)
	inventoryService := inventoryapp.NewService(inventorypg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	vaccinationCompletion := vaccinationapp.NewCompletionService(vaccinationService, obligationRepo, inventoryService)
	vaccinationBooster := vaccinationapp.NewBoosterService(protocolRepo, obligationRepo).WithGoatReader(vaccinationRepo).WithCrossVaccineGapReader(vaccinationRepo)
	vaccinationGeneration := vaccinationapp.NewGenerationService(protocolRepo, vaccinationRepo, obligationRepo)
	protocolService := protocolapp.NewService(protocolRepo)
	protocolHandler := protocolhttp.NewHandler(protocolService, log)
	bus := eventbus.NewInProcessBus()
	obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
	obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
	vaccinationapp.NewGoatCreatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewGoatRecheckHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewManualCampaignHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	sopService.
		WithSubmissionHook(sopbridge.NewVaccinationSubmissionBridge(vaccinationService)).
		WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, bus))
	sopHandler := sophttp.NewHandler(sopService, log)
	vaccinationHandler := vaccinationhttp.NewHandler(vaccinationService, vaccinationCompletion, log).
		WithManualCampaignGenerator(vaccinationGeneration)
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
	bulkstatushttp.Register(protectedMux, bulkStatusHandler)
	locationshttp.Register(protectedMux, locationsHandler)
	workforcehttp.Register(protectedMux, workforceHandler)
	proofhttp.Register(protectedMux, proofHandler)
	sophttp.Register(protectedMux, sopHandler)
	protocolhttp.Register(protectedMux, protocolHandler)
	outboxhttp.Register(protectedMux, outboxHandler)
	operationsaudithttp.Register(protectedMux, operationsAuditHandler)
	processintegrityhttp.Register(protectedMux, processIntegrityHandler)
	procurementhttp.Register(protectedMux, procurementHandler)
	vaccinationhttp.Register(protectedMux, vaccinationHandler)
	vaccexechttp.Register(protectedMux, vaccExecHandler)
	calendarhttp.Register(protectedMux, calendarHandler)
	adminuihttp.Register(protectedMux, adminUIHandler)
	feedhttp.Register(protectedMux, feedHandler)
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

func bulkImportPreviewSigningKey(cfg Config) (string, error) {
	key := strings.TrimSpace(cfg.BulkImportPreviewSigningKey)
	if key != "" {
		return key, nil
	}
	if bulkImportPreviewDevSigningKeyAllowed(cfg.Auth.Environment) {
		return identityapp.DevBulkPreviewSigningKey(), nil
	}
	return "", fmt.Errorf("GOATOS_BULK_IMPORT_PREVIEW_SIGNING_KEY is required when GOATOS_ENV is %q", cfg.Auth.Environment)
}

func bulkImportPreviewDevSigningKeyAllowed(env string) bool {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "local", "test":
		return true
	default:
		return false
	}
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
