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

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	adminuihttp "github.com/vgoats/goatos/backend/internal/adminui/adapters/http"
	adminuipg "github.com/vgoats/goatos/backend/internal/adminui/adapters/postgres"
	adminuiapp "github.com/vgoats/goatos/backend/internal/adminui/app"
	appconfighttp "github.com/vgoats/goatos/backend/internal/appconfig/adapters/http"
	appconfigapp "github.com/vgoats/goatos/backend/internal/appconfig/app"
	bulkstatushttp "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/http"
	bulkstatuspg "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/postgres"
	bulkstatusapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	calendarhttp "github.com/vgoats/goatos/backend/internal/calendar/adapters/http"
	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	ceoai "github.com/vgoats/goatos/backend/internal/ceoai"
	ceoobs "github.com/vgoats/goatos/backend/internal/ceoai/adapters/observability"
	ceoreadtools "github.com/vgoats/goatos/backend/internal/ceoai/adapters/readtools"
	countshttp "github.com/vgoats/goatos/backend/internal/counts/adapters/http"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	feedhttp "github.com/vgoats/goatos/backend/internal/feed/adapters/http"
	feedpg "github.com/vgoats/goatos/backend/internal/feed/adapters/postgres"
	feedapp "github.com/vgoats/goatos/backend/internal/feed/app"
	feedconfighttp "github.com/vgoats/goatos/backend/internal/feedconfig/adapters/http"
	feedconfigpg "github.com/vgoats/goatos/backend/internal/feedconfig/adapters/postgres"
	feedconfigapp "github.com/vgoats/goatos/backend/internal/feedconfig/app"
	feeddirectioncounts "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/counts"
	feeddirectionhttp "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/http"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	identityhttp "github.com/vgoats/goatos/backend/internal/identity/adapters/http"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	locationshttp "github.com/vgoats/goatos/backend/internal/locations/adapters/http"
	locationspg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
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
	"github.com/vgoats/goatos/backend/internal/platform/buildinfo"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/migrationguard"
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
	vaccexecroster "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/roster"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	verificationhttp "github.com/vgoats/goatos/backend/internal/verification/adapters/http"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationproofmedia "github.com/vgoats/goatos/backend/internal/verification/adapters/proofmedia"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
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

const (
	defaultAuthSessionRateLimitPerMinute = 120
	defaultAppCheckJWKSURL               = "https://firebaseappcheck.googleapis.com/v1/jwks"
)

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
	AppCheckMode                     string
	AppCheckIssuer                   string
	AppCheckAudience                 string
	AppCheckJWKSUrl                  string
	AppCheckClockSkew                time.Duration
	AppCheckJWKSCacheTTL             time.Duration
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
			AppCheckMode:                     os.Getenv("GOATOS_APPCHECK_ENFORCE"),
			AppCheckIssuer:                   os.Getenv("GOATOS_APPCHECK_ISSUER"),
			AppCheckAudience:                 os.Getenv("GOATOS_APPCHECK_AUDIENCE"),
			AppCheckJWKSUrl:                  os.Getenv("GOATOS_APPCHECK_JWKS_URL"),
			AppCheckClockSkew:                authDurationFromEnv("GOATOS_APPCHECK_CLOCK_SKEW"),
			AppCheckJWKSCacheTTL:             authDurationFromEnv("GOATOS_APPCHECK_JWKS_CACHE_TTL"),
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
		secret := strings.TrimSpace(os.Getenv("GOATOS_LOCAL_MEDIA_SIGNING_SECRET"))
		if secret == "" {
			return nil, fmt.Errorf("GOATOS_LOCAL_MEDIA_SIGNING_SECRET is required for local proof storage")
		}
		return prooflocal.New(os.Getenv("GOATOS_LOCAL_MEDIA_DIR"), secret), nil
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
	case "local", "test", "development":
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
	appCheckVerifier, err := buildAppCheckVerifier(cfg.Auth)
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

	// Stale-binary migration-drift guard (docs/decisions/stale-binary-migration-drift-guard.md):
	// refuse to serve traffic when database is ahead of binary (DBAhead — stale binary incident).
	// For BinaryAhead (binary ahead of database — pending migrations not yet applied), boot
	// normally and report not-ready via /readyz, which re-checks on every call. Once pending
	// migrations apply, /readyz recovers to healthy without a restart. This prevents crash-loop
	// during normal deploy sequencing (migrate → start service → /readyz recovers).
	// binaryMigrationVersion is captured here (not recomputed per request) because it is fixed
	// for the life of this process; dbMigrationVersion is re-queried on every /version and
	// /readyz call below so drift that appears AFTER this successful startup is still visible.
	binaryMigrationVersion, err := migrationguard.BinaryVersion()
	if err != nil {
		pool.Close()
		return nil, err
	}
	dbMigrationVersion, err := migrationguard.AppliedVersion(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	status, err := migrationguard.Check(dbMigrationVersion, binaryMigrationVersion)
	if err != nil {
		// DBAhead (database ahead of binary) is fatal — the incident this guard prevents.
		// Refuse to boot.
		if status.DBAhead {
			if log != nil {
				log.Error("migration_drift_dbahead_fatal",
					slog.String("db_migration_version", dbMigrationVersion),
					slog.String("binary_migration_version", binaryMigrationVersion),
					slog.String("error", err.Error()))
			}
			pool.Close()
			return nil, err
		}
		// BinaryAhead (binary ahead of database — pending migrations not yet applied) is
		// transient during deploy. Log and boot anyway; /readyz will report not-ready.
		if status.BinaryAhead {
			if log != nil {
				log.Info("migration_drift_binaryahead_transient",
					slog.String("db_migration_version", dbMigrationVersion),
					slog.String("binary_migration_version", binaryMigrationVersion),
					slog.String("error", err.Error()))
			}
			// Continue to boot; /readyz will prevent traffic until migrations apply.
		}
	}

	if err := platformpg.RegisterPoolMetrics(pool); err != nil && log != nil {
		// Pool-stat metrics are an observability nice-to-have, never a
		// reason to fail API startup.
		log.Warn("postgres_pool_metrics_registration_failed", slog.String("error", err.Error()))
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
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	rosterHandler := workforcehttp.NewRosterHandler(rosterService, log)
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
	vaccExecOwnership := vaccexecroster.NewOwnershipAdapter(rosterService)
	vaccExecService := vaccexecapp.NewService(vaccexecpg.NewRepository(pool, cfg.Postgres.QueryTimeout), vaccExecOwnership)
	vaccExecHandler := vaccexechttp.NewHandler(vaccExecService, obligationRepo, log).
		WithOperatorAssignmentConfigWriter(vaccExecService).
		WithCapacityConfigWriter(vaccExecService)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	calendarHandler := calendarhttp.NewHandler(calendarService, log)
	adminUIHandler := adminuihttp.NewHandler(adminuiapp.NewService(adminuipg.NewRepository(pool, cfg.Postgres.QueryTimeout)))
	appConfigHandler := appconfighttp.NewHandler(appconfigapp.NewService(appconfigapp.ConfigFromEnv()), log)
	countsService := countsapp.NewService(countspg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	herdRegisterService := countsapp.NewHerdRegisterService(countspg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	herdRegisterHandler := countshttp.NewHandler(herdRegisterService, log)
	// App-tier Counts writes (shifting/birth/death) + the lifecycle approval workflow.
	//
	// Per the maintainer decision (2026-07-19) the three submit routes RECORD a pending request
	// rather than applying it; approving one applies the effect through the identity module's
	// transaction-scoped seam, in the SAME transaction as the approval's status flip.
	//
	// countsApprovalRepo therefore carries the identity write seam (goat create / guarded critical-
	// death exit / bulk relocate). identityService supplies the Prepare* validators, which validate
	// a payload at submit time without applying it.
	countsApprovalRepo := countspg.NewRepository(pool, cfg.Postgres.QueryTimeout).
		WithIdentityTxWriter(identityRepo)
	countsApprovalService := countsapp.NewApprovalService(countsApprovalRepo, identityService, nil)
	// Shifting execution shares countsApprovalRepo because that repository already carries the
	// identity transaction seam the relocation runs through -- and the relocation now happens HERE,
	// at completion, rather than at approval.
	countsShiftingExecutionService := countsapp.NewShiftingExecutionService(countsApprovalRepo, nil)
	countsAppWriteHandler := countshttp.NewAppWriteHandler(countsService, log).
		WithApprovalWorkflow(countsApprovalService, identityService).
		WithShiftingExecutionWorkflow(countsShiftingExecutionService)
	feedService := feedapp.NewService(feedpg.NewRepository(pool, cfg.Postgres.QueryTimeout)).
		WithCountsProjectionProvider(countsService).
		WithCountsProjectionExceptionResolver(countsService).
		WithCountsProjectionExceptionLister(countsService).
		// Live-herd feed projection (maintainer decision 2026-07-19): this farm runs no physical
		// counting workflow, so the live goats table is the count and approved-but-unexecuted
		// shiftings are the only pending change. Parallel to the anchor-replay provider above,
		// which stays wired for the counts-source import and parity tooling.
		WithFeedProjectedCounts(countsService)
	feedHandler := feedhttp.NewHandler(feedService, log)
	// Authored feed CONFIGURATION is its own module, wired alongside feed direction rather than into
	// it. feedapp owns execution (what goes to each shed today); feedconfig owns the ration grid and
	// dispatch clock that execution reads from, and is the only writer of those tables.
	feedConfigService := feedconfigapp.NewService(feedconfigpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	feedConfigHandler := feedconfighttp.NewHandler(feedConfigService, log)
	// Feed-direction GENERATION is a third, separate module, and the split is the point: it OWNS NO
	// TABLES and writes nothing. It is a pure read-only generator over the other two -- projected
	// shed counts from counts, the authored ration grid from feedconfig -- so it depends on both
	// through narrow ports and neither of them depends on it.
	//
	// The counts dependency is wired through feeddirectioncounts.NewReader rather than passing
	// countsService straight in, so counts keeps sole ownership of the census SQL and the generator
	// stays testable against a fake reader.
	// One repository instance owns the config/scope reads AND the frozen-issue tables plus the
	// feed_schedule_config clock, so the generator, the serve path and the lifecycle all share it.
	feedDirectionRepo := feeddirectionpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	feedDirectionService := feeddirectionapp.NewService(
		feedDirectionRepo,
		feeddirectioncounts.NewReader(countsService),
	).
		WithIssueStore(feedDirectionRepo).
		WithScheduleReader(feedDirectionRepo).
		WithGeneratedBy("goatos-api")
	feedDirectionHandler := feeddirectionhttp.NewHandler(feedDirectionService, log)
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
	// Generic Verification vertical (context/architecture/verification-module-design.md): a
	// standalone bounded context producers plug into via the type registry. Media is resolved
	// through the EXISTING proof signed-URL port, never proxied/duplicated.
	verificationRepo := verificationpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	verificationMedia := verificationproofmedia.NewResolver(proofService)
	verificationService := verificationapp.NewService(verificationRepo, verificationMedia)
	if err := verificationService.RegisterCategory(verificationdomain.CategoryDefinition{
		Vertical:      "preventive_care",
		Module:        "vaccination",
		Category:      sopbridge.VaccinationVerificationCategory,
		ExpectedMedia: []string{"video"},
	}); err != nil {
		pool.Close()
		return nil, err
	}
	verificationHandler := verificationhttp.NewHandler(verificationService, log)

	// Leadership read-only assistant (CEO AI). Wired end-to-end: the Vertex
	// Gemini planner (when MESHA_AI_PROVIDER=vertex + ADC available; else the
	// deterministic keyword planner, mode=fallback), the Cube governed-metric
	// service (tier 1), the MCP Toolbox curated tools (tier 3), the safety
	// moderator, durable conversation persistence, plus the observability
	// adapters so a live POST /ceo-ai/ask emits the assistant_* OTel metrics and
	// persists an internal step trace the admin-only
	// GET /ceo-ai/admin/trace/{request_id} endpoint reads back. Each port
	// degrades independently: an unconfigured Cube/Toolbox/Vertex is nil and the
	// orchestrator falls to the tiers that are wired instead of failing boot.
	ceoTraceStore := ceoobs.NewPostgresTraceStore(pool, cfg.Postgres.QueryTimeout)
	ceoVertex := ceoai.NewVertexProvider(ctx, log)

	// Build read tool executors. counts_breakdown and feed_direction_today do
	// NOT have a wired in-process reader here (no direct DB call from this
	// tier yet); their planner-routed API tool name now matches the executor
	// registered under it (fixed P1-1: planner and registry.go both agree on
	// "feed_direction_today"), and when the reader is unwired the executor
	// reports it via ToolResult.Err instead of silently returning empty. The
	// orchestrator's runtime fallback (app/fallback.go) retries that same
	// question through Cube (active_animals) or the MCP Toolbox
	// (mesha_count_by_scope / mesha_feed_direction_summary) so a question
	// never dead-ends on an unwired API executor (P1-2, P1-3).
	readToolExecs := ceoreadtools.NewToolExecutors()

	// Wire in-process readers for operational domains. The reader functions
	// themselves live in ceoai_readers.go (unit-tested against fakes in
	// ceoai_readers_test.go) so this block is just registration.
	parkResolver := newLocationsParkResolver(locationsService)

	for _, exec := range readToolExecs {
		switch exec.Spec().Name {
		case "vaccination_shed_summary":
			ceoreadtools.SetVaccinationDataReader(readToolExecs, buildVaccinationReader(vaccExecService))
		case "procurement_source_entry_loads":
			ceoreadtools.SetProcurementDataReader(exec, buildProcurementReader(procurementService))
		case "admin_roster_coverage":
			ceoreadtools.SetWorkforceDataReader(exec, buildWorkforceReader(rosterService))
		case "verification_queue":
			ceoreadtools.SetVerificationDataReader(exec, buildVerificationReader(verificationService))
		case "action_center_obligations":
			ceoreadtools.SetActionCenterDataReader(exec, buildActionCenterReader(processIntegrityService, parkResolver))
		case "operations_kernel_health":
			ceoreadtools.SetOpsKernelHealthDataReader(exec, buildOpsKernelHealthReader(processIntegrityService))
		case "operations_audit_summary":
			ceoreadtools.SetOpsAuditSummaryDataReader(exec, buildOpsAuditSummaryReader(operationsAuditService))
			// admin_location_usage is intentionally left on the Toolbox/fallback
			// path: locationsService only exposes per-location Usage/ListCapacity
			// reads (a single location_id argument), not a bounded listing across
			// parks/sheds suited to a capacity-variance question. Building that
			// would require a new read model/API, which is out of scope here.
		}
	}

	ceoOpts := ceoai.Options{
		Metrics:   ceoai.NewCubeMetricService(log),
		ReadTools: readToolExecs,
		Toolbox:   ceoai.NewToolbox(log),
		Moderator: ceoai.NewModerator(),
		Convo:     ceoai.NewConversationStore(pool, cfg.Postgres.QueryTimeout),
		Audit:     ceoobs.NewAuditTraceSink(ceoTraceStore),
		Telemetry: ceoobs.NewMetrics(),
		Traces:    ceoTraceStore,
		// Thread surface backing GET/POST /ceo-ai/conversations* and the leadership
		// starters probe GET /ceo-ai/starters (the launcher visibility gate).
		ConvStore: ceoai.NewConversationHTTPStore(pool, cfg.Postgres.QueryTimeout),
		Logger:    log,
	}
	if ceoVertex != nil {
		ceoOpts.Provider = ceoVertex
		ceoOpts.Critic = ceoVertex
	}
	ceoService := ceoai.Build(ceoOpts)

	bus := eventbus.NewInProcessBus()
	obligationapp.NewGoatShiftedHandler(obligationRepo).Register(bus)
	obligationapp.NewGoatExitedHandler(obligationRepo).Register(bus)
	obligationapp.NewOperatorConfigReplanHandler(obligationRepo).Register(bus)
	// Operator-config auto-cascade producers (backend/internal/obligation/app/operator_config_replan.go
	// is the consumer registered above): wire the SAME in-process bus into the services whose writes
	// change vaccination operator N/default-operator or leave, so the consumer fires without a manual
	// recompute CLI run.
	vaccExecService.WithBus(bus)
	rosterService.WithBus(bus)
	vaccinationapp.NewGoatCreatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewGoatRecheckHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).WithClosureProjector(sopService).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
	// Notification PUSH LAYER ONLY (docs/decisions/vaccination-notification-rules.md §4c): read-only
	// consumers of vaccination.verification.awaiting_review and vaccination.verify.rejected/accepted
	// events published by sopbridge. They resolve each completion to its obligation context, then
	// route pending/rework/close notifications to the correct park, verifier, and leadership audience.
	notificationbridge.NewVerificationEventConsumer(rosterService, calendarService, log).Register(bus)
	notificationbridge.NewVerificationNotifier(calendarService, rosterService, calendarService, log).Register(bus)
	sopService.
		WithSubmissionHook(sopbridge.NewVaccinationSubmissionBridge(vaccinationService).
			WithVerificationProducer(verificationService)).
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
	authz, err := buildAuthMiddleware(cfg.Auth, verifier, appCheckVerifier, grantSource, log)
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
		// Re-check migration drift on every call, not just at startup: this is
		// exactly the incident scenario, where the DB gets migrated forward
		// WHILE this process keeps running. A launcher that reuses an
		// already-"ready" process (see tools/dev/run-local-stack-supervised.sh's
		// start_api, which short-circuits on a healthy /readyz) must see this go
		// unhealthy instead of silently continuing to serve against a schema it
		// no longer matches.
		if dbVersion, err := migrationguard.AppliedVersion(r.Context(), pool); err == nil {
			if _, err := migrationguard.Check(dbVersion, binaryMigrationVersion); err != nil {
				http.Error(w, "migration drift: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	// /version is a diagnostic surface (bypasses auth, see
	// httpmiddleware.isPublicHealthRoute) so an operator or curl can see
	// build/migration staleness instantly without needing a token. It never
	// fails the request itself - migration_drift is reported as a boolean
	// plus a direction so a stale-but-still-running process can still be
	// introspected instead of only going dark.
	protectedMux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		dbVersion, dbErr := migrationguard.AppliedVersion(r.Context(), pool)
		status, checkErr := migrationguard.Check(dbVersion, binaryMigrationVersion)
		direction := ""
		switch {
		case status.DBAhead:
			direction = "db_ahead_of_binary"
		case status.BinaryAhead:
			direction = "binary_ahead_of_db"
		}
		body := struct {
			Service                string `json:"service"`
			BuildSHA               string `json:"build_sha"`
			BinaryMigrationVersion string `json:"binary_migration_version"`
			DBMigrationVersion     string `json:"db_migration_version"`
			MigrationDrift         bool   `json:"migration_drift"`
			MigrationDriftReason   string `json:"migration_drift_reason,omitempty"`
			Error                  string `json:"error,omitempty"`
		}{
			Service:                "api",
			BuildSHA:               buildinfo.Current(),
			BinaryMigrationVersion: binaryMigrationVersion,
			DBMigrationVersion:     dbVersion,
			MigrationDrift:         checkErr != nil,
			MigrationDriftReason:   direction,
		}
		if dbErr != nil {
			body.Error = dbErr.Error()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
	identityhttp.Register(protectedMux, identityHandler)
	bulkstatushttp.Register(protectedMux, bulkStatusHandler)
	locationshttp.Register(protectedMux, locationsHandler)
	workforcehttp.Register(protectedMux, workforceHandler)
	workforcehttp.RegisterRoster(protectedMux, rosterHandler)
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
	appconfighttp.Register(protectedMux, appConfigHandler)
	countshttp.Register(protectedMux, herdRegisterHandler)
	countshttp.RegisterAppWrites(protectedMux, countsAppWriteHandler)
	countshttp.RegisterApprovals(protectedMux, countsAppWriteHandler)
	countshttp.RegisterShiftingExecution(protectedMux, countsAppWriteHandler)
	feedhttp.Register(protectedMux, feedHandler)
	feedconfighttp.Register(protectedMux, feedConfigHandler)
	feeddirectionhttp.Register(protectedMux, feedDirectionHandler)
	passporthttp.Register(protectedMux, passportHandler)
	verificationhttp.Register(protectedMux, verificationHandler)
	ceoService.Register(protectedMux)

	// otelhttp owns real span creation for every protected request (server
	// spans, W3C trace-context propagation); httpmiddleware.Metrics records
	// the RED metrics (http.server.request.duration/requests/active_requests)
	// using the SAME matched route template via protectedMux.Handler, so
	// route/method/status_class/tenant stay bounded-cardinality per
	// docs/observability/OBSERVABILITY_DESIGN.md sections 2.1-2.2. Metrics is
	// innermost (closest to protectedMux) so it always observes the final
	// response status regardless of what otelhttp's own wrapping does to the
	// ResponseWriter.
	routeOf := httpmiddleware.MuxRoutePattern(protectedMux)
	instrumentedProtectedMux := otelhttp.NewHandler(
		httpmiddleware.Metrics(routeOf)(protectedMux),
		"goatos.api",
		otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string {
			if pattern := routeOf(r); pattern != "" {
				return pattern
			}
			return operation
		}),
	)

	mux := http.NewServeMux()
	authaudit.Register(mux, authAuditHandler)
	proofhttp.RegisterSigned(mux, proofHandler)
	mux.Handle("/", authz.Wrap(instrumentedProtectedMux))

	// PanicRecovery is outermost so it catches panics in auth and RequestContext.
	// RequestContext's own request_id/trace_id log fields are independent of
	// the otelhttp span created below it: when the caller sends a W3C
	// traceparent header, both derive the same trace id (RequestContext
	// passes it through verbatim; otelhttp's TraceContext propagator parses
	// the same header to continue the trace), so logs and traces correlate
	// for propagated requests. For a request with no incoming traceparent,
	// RequestContext still logs its own generated request id (matching
	// existing behavior/tests) while otelhttp mints an independent root span -
	// see docs/observability/OBSERVABILITY_DESIGN.md section 2.2 and the
	// SetupTelemetry doc comment for why this ordering was chosen.
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

func buildAppCheckVerifier(cfg AuthConfig) (httpmiddleware.TokenVerifier, error) {
	mode := strings.ToLower(strings.TrimSpace(cfg.AppCheckMode))
	if mode == "" {
		mode = httpmiddleware.AppCheckModeOff
	}
	switch mode {
	case httpmiddleware.AppCheckModeOff:
		return nil, nil
	case httpmiddleware.AppCheckModeMonitor, httpmiddleware.AppCheckModeEnforce:
	default:
		return nil, fmt.Errorf("%w: GOATOS_APPCHECK_ENFORCE must be off, monitor, or enforce", httpmiddleware.ErrInvalidAuthConfig)
	}

	issuer := strings.TrimSpace(cfg.AppCheckIssuer)
	audience := strings.TrimSpace(cfg.AppCheckAudience)
	if issuer == "" || audience == "" {
		return nil, fmt.Errorf("%w: GOATOS_APPCHECK_ISSUER and GOATOS_APPCHECK_AUDIENCE are required when App Check is enabled", httpmiddleware.ErrInvalidAuthConfig)
	}
	jwksURL := strings.TrimSpace(cfg.AppCheckJWKSUrl)
	if jwksURL == "" {
		jwksURL = defaultAppCheckJWKSURL
	}
	return platformauth.NewJWKSVerifier(platformauth.JWKSConfig{
		JWKSURL:             jwksURL,
		Issuer:              issuer,
		Audience:            audience,
		AllowedAlgs:         []string{platformauth.AlgorithmRS256},
		ClockSkew:           cfg.AppCheckClockSkew,
		CacheTTL:            cfg.AppCheckJWKSCacheTTL,
		RequireTokenTypeJWT: true,
	})
}

func buildAuthMiddleware(cfg AuthConfig, verifier, appCheckVerifier httpmiddleware.TokenVerifier, grants permissions.GrantSource, log *slog.Logger) (*httpmiddleware.AuthMiddleware, error) {
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
		Mode:             mode,
		AppCheckMode:     cfg.AppCheckMode,
		AppCheckVerifier: appCheckVerifier,

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
