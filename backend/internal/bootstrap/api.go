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

	countssoppg "github.com/vgoats/goatos/backend/internal/countssop/adapters/postgres"
	countssopapp "github.com/vgoats/goatos/backend/internal/countssop/app"
	feedsoppg "github.com/vgoats/goatos/backend/internal/feedsop/adapters/postgres"
	feedsopapp "github.com/vgoats/goatos/backend/internal/feedsop/app"
	"github.com/vgoats/goatos/backend/internal/platform/readcache"
	shiftingsoppg "github.com/vgoats/goatos/backend/internal/shiftingsop/adapters/postgres"
	shiftingsopapp "github.com/vgoats/goatos/backend/internal/shiftingsop/app"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	adminuihttp "github.com/vgoats/goatos/backend/internal/adminui/adapters/http"
	adminuipg "github.com/vgoats/goatos/backend/internal/adminui/adapters/postgres"
	adminuiapp "github.com/vgoats/goatos/backend/internal/adminui/app"
	alertshttp "github.com/vgoats/goatos/backend/internal/alerts/adapters/http"
	alertspg "github.com/vgoats/goatos/backend/internal/alerts/adapters/postgres"
	alertsapp "github.com/vgoats/goatos/backend/internal/alerts/app"
	appanalyticshttp "github.com/vgoats/goatos/backend/internal/appanalytics/adapters/http"
	appconfighttp "github.com/vgoats/goatos/backend/internal/appconfig/adapters/http"
	appconfigapp "github.com/vgoats/goatos/backend/internal/appconfig/app"
	"github.com/vgoats/goatos/backend/internal/browserpush"
	browserpushhttp "github.com/vgoats/goatos/backend/internal/browserpush/adapters/http"
	browserpushpg "github.com/vgoats/goatos/backend/internal/browserpush/adapters/postgres"
	bulkstatushttp "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/http"
	bulkstatuspg "github.com/vgoats/goatos/backend/internal/bulkstatus/adapters/postgres"
	bulkstatusapp "github.com/vgoats/goatos/backend/internal/bulkstatus/app"
	calendarhttp "github.com/vgoats/goatos/backend/internal/calendar/adapters/http"
	calendarpg "github.com/vgoats/goatos/backend/internal/calendar/adapters/postgres"
	calendarapp "github.com/vgoats/goatos/backend/internal/calendar/app"
	ceoai "github.com/vgoats/goatos/backend/internal/ceoai"
	ceoobs "github.com/vgoats/goatos/backend/internal/ceoai/adapters/observability"
	ceoreadtools "github.com/vgoats/goatos/backend/internal/ceoai/adapters/readtools"
	"github.com/vgoats/goatos/backend/internal/ceoai/sqlguard"
	configurationhttp "github.com/vgoats/goatos/backend/internal/configuration/adapters/http"
	configurationpg "github.com/vgoats/goatos/backend/internal/configuration/adapters/postgres"
	configurationapp "github.com/vgoats/goatos/backend/internal/configuration/app"
	countsboard "github.com/vgoats/goatos/backend/internal/counts/adapters/boardsource"
	countshttp "github.com/vgoats/goatos/backend/internal/counts/adapters/http"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsproof "github.com/vgoats/goatos/backend/internal/counts/adapters/proof"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsbridge "github.com/vgoats/goatos/backend/internal/countsbridge"
	eventwiring "github.com/vgoats/goatos/backend/internal/eventwiring"
	feedhttp "github.com/vgoats/goatos/backend/internal/feed/adapters/http"
	feedpg "github.com/vgoats/goatos/backend/internal/feed/adapters/postgres"
	feedapp "github.com/vgoats/goatos/backend/internal/feed/app"
	feedconfigcounts "github.com/vgoats/goatos/backend/internal/feedconfig/adapters/counts"
	feedconfighttp "github.com/vgoats/goatos/backend/internal/feedconfig/adapters/http"
	feedconfigpg "github.com/vgoats/goatos/backend/internal/feedconfig/adapters/postgres"
	feedconfigapp "github.com/vgoats/goatos/backend/internal/feedconfig/app"
	feedboard "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/boardsource"
	feeddirectioncounts "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/counts"
	feeddirectionhttp "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/http"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionproof "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/proof"
	feeddirectionverificationbridge "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/verificationbridge"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	fwrpg "github.com/vgoats/goatos/backend/internal/feedwaterremoval/adapters/postgres"
	growthdirectorhttp "github.com/vgoats/goatos/backend/internal/growthdirector/adapters/http"
	growthdirectorpg "github.com/vgoats/goatos/backend/internal/growthdirector/adapters/postgres"
	growthdirectorapp "github.com/vgoats/goatos/backend/internal/growthdirector/app"
	healthboard "github.com/vgoats/goatos/backend/internal/health/adapters/boardsource"
	healthhttp "github.com/vgoats/goatos/backend/internal/health/adapters/http"
	healthpg "github.com/vgoats/goatos/backend/internal/health/adapters/postgres"
	healthverificationbridge "github.com/vgoats/goatos/backend/internal/health/adapters/verificationbridge"
	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	herdsignalshttp "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/http"
	herdsignalspg "github.com/vgoats/goatos/backend/internal/herdsignals/adapters/postgres"
	herdsignalsapp "github.com/vgoats/goatos/backend/internal/herdsignals/app"
	identityhttp "github.com/vgoats/goatos/backend/internal/identity/adapters/http"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	"github.com/vgoats/goatos/backend/internal/identity/adapters/salesbridge"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	inventorypg "github.com/vgoats/goatos/backend/internal/inventory/adapters/postgres"
	inventoryapp "github.com/vgoats/goatos/backend/internal/inventory/app"
	leadershiptaskshttp "github.com/vgoats/goatos/backend/internal/leadershiptasks/adapters/http"
	leadershiptaskspg "github.com/vgoats/goatos/backend/internal/leadershiptasks/adapters/postgres"
	leadershiptasksproof "github.com/vgoats/goatos/backend/internal/leadershiptasks/adapters/proof"
	leadershiptasksapp "github.com/vgoats/goatos/backend/internal/leadershiptasks/app"
	locationshttp "github.com/vgoats/goatos/backend/internal/locations/adapters/http"
	locationspg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationsapp "github.com/vgoats/goatos/backend/internal/locations/app"
	markethttp "github.com/vgoats/goatos/backend/internal/market/adapters/http"
	marketpg "github.com/vgoats/goatos/backend/internal/market/adapters/postgres"
	marketapp "github.com/vgoats/goatos/backend/internal/market/app"
	notificationaudiencehttp "github.com/vgoats/goatos/backend/internal/notificationaudience/adapters/http"
	notificationaudiencepg "github.com/vgoats/goatos/backend/internal/notificationaudience/adapters/postgres"
	notificationaudienceapp "github.com/vgoats/goatos/backend/internal/notificationaudience/app"
	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	notificationcentrehttp "github.com/vgoats/goatos/backend/internal/notificationcentre/adapters/http"
	notificationcentrepg "github.com/vgoats/goatos/backend/internal/notificationcentre/adapters/postgres"
	notificationcentreapp "github.com/vgoats/goatos/backend/internal/notificationcentre/app"
	obligationpg "github.com/vgoats/goatos/backend/internal/obligation/adapters/postgres"
	obligationapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	operationsaudithttp "github.com/vgoats/goatos/backend/internal/operationsaudit/adapters/http"
	operationsauditpg "github.com/vgoats/goatos/backend/internal/operationsaudit/adapters/postgres"
	operationsauditapp "github.com/vgoats/goatos/backend/internal/operationsaudit/app"
	outboxhttp "github.com/vgoats/goatos/backend/internal/outbox/adapters/http"
	outboxpg "github.com/vgoats/goatos/backend/internal/outbox/adapters/postgres"
	passporthttp "github.com/vgoats/goatos/backend/internal/passport/adapters/http"
	passportapp "github.com/vgoats/goatos/backend/internal/passport/app"
	pccareboard "github.com/vgoats/goatos/backend/internal/pccare/adapters/boardsource"
	pccarehttp "github.com/vgoats/goatos/backend/internal/pccare/adapters/http"
	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	pccareproof "github.com/vgoats/goatos/backend/internal/pccare/adapters/proof"
	pccareverificationbridge "github.com/vgoats/goatos/backend/internal/pccare/adapters/verificationbridge"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	penroutinesboard "github.com/vgoats/goatos/backend/internal/penroutines/adapters/boardsource"
	penroutineshttp "github.com/vgoats/goatos/backend/internal/penroutines/adapters/http"
	penroutinespg "github.com/vgoats/goatos/backend/internal/penroutines/adapters/postgres"
	penroutinesproof "github.com/vgoats/goatos/backend/internal/penroutines/adapters/proof"
	penroutinesapp "github.com/vgoats/goatos/backend/internal/penroutines/app"
	penvisitsboard "github.com/vgoats/goatos/backend/internal/penvisits/adapters/boardsource"
	penvisitshttp "github.com/vgoats/goatos/backend/internal/penvisits/adapters/http"
	penvisitspg "github.com/vgoats/goatos/backend/internal/penvisits/adapters/postgres"
	penvisitsproof "github.com/vgoats/goatos/backend/internal/penvisits/adapters/proof"
	penvisitsapp "github.com/vgoats/goatos/backend/internal/penvisits/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	permissionspg "github.com/vgoats/goatos/backend/internal/permissions/adapters/postgres"
	platformaudit "github.com/vgoats/goatos/backend/internal/platform/audit"
	platformauth "github.com/vgoats/goatos/backend/internal/platform/auth"
	"github.com/vgoats/goatos/backend/internal/platform/authallow"
	"github.com/vgoats/goatos/backend/internal/platform/authaudit"
	"github.com/vgoats/goatos/backend/internal/platform/buildinfo"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	piboard "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/boardsource"
	tasksboard "github.com/vgoats/goatos/backend/internal/tasks/adapters/boardsource"
	toxinboard "github.com/vgoats/goatos/backend/internal/toxin/adapters/boardsource"
	toxinhttp "github.com/vgoats/goatos/backend/internal/toxin/adapters/http"
	toxinpg "github.com/vgoats/goatos/backend/internal/toxin/adapters/postgres"
	toxinproof "github.com/vgoats/goatos/backend/internal/toxin/adapters/proof"
	toxinapp "github.com/vgoats/goatos/backend/internal/toxin/app"
	workboardhttp "github.com/vgoats/goatos/backend/internal/workboard/adapters/http"
	workboardpg "github.com/vgoats/goatos/backend/internal/workboard/adapters/postgres"
	workboardapp "github.com/vgoats/goatos/backend/internal/workboard/app"
	workboardports "github.com/vgoats/goatos/backend/internal/workboard/ports"
	"golang.org/x/oauth2"

	animalpurchasehttp "github.com/vgoats/goatos/backend/internal/animalpurchase/adapters/http"
	animalpurchasepg "github.com/vgoats/goatos/backend/internal/animalpurchase/adapters/postgres"
	animalpurchaseproof "github.com/vgoats/goatos/backend/internal/animalpurchase/adapters/proof"
	animalpurchaseapp "github.com/vgoats/goatos/backend/internal/animalpurchase/app"
	pccaresoppg "github.com/vgoats/goatos/backend/internal/pccaresop/adapters/postgres"
	pccaresopapp "github.com/vgoats/goatos/backend/internal/pccaresop/app"
	"github.com/vgoats/goatos/backend/internal/platform/firebaseidentity"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/migrationguard"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	"github.com/vgoats/goatos/backend/internal/platform/uuidutil"
	processintegrityhttp "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/http"
	processintegritypg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	processintegrityapp "github.com/vgoats/goatos/backend/internal/processintegrity/app"
	procurementhttp "github.com/vgoats/goatos/backend/internal/procurement/adapters/http"
	procurementpg "github.com/vgoats/goatos/backend/internal/procurement/adapters/postgres"
	procurementproof "github.com/vgoats/goatos/backend/internal/procurement/adapters/proof"
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
	saleshttp "github.com/vgoats/goatos/backend/internal/sales/adapters/http"
	salespg "github.com/vgoats/goatos/backend/internal/sales/adapters/postgres"
	salesapp "github.com/vgoats/goatos/backend/internal/sales/app"
	sophttp "github.com/vgoats/goatos/backend/internal/sop/adapters/http"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	taskshttp "github.com/vgoats/goatos/backend/internal/tasks/adapters/http"
	taskspg "github.com/vgoats/goatos/backend/internal/tasks/adapters/postgres"
	tasksproofkinds "github.com/vgoats/goatos/backend/internal/tasks/adapters/proofkinds"
	tasksverificationbridge "github.com/vgoats/goatos/backend/internal/tasks/adapters/verificationbridge"
	tasksapp "github.com/vgoats/goatos/backend/internal/tasks/app"
	tasksdomain "github.com/vgoats/goatos/backend/internal/tasks/domain"
	vaccinationhttp "github.com/vgoats/goatos/backend/internal/vaccination/adapters/http"
	vaccinationpg "github.com/vgoats/goatos/backend/internal/vaccination/adapters/postgres"
	vaccinationapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	vaccexechttp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/http"
	vaccexecpg "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/postgres"
	vaccexecroster "github.com/vgoats/goatos/backend/internal/vaccinationexecution/adapters/roster"
	vaccexecapp "github.com/vgoats/goatos/backend/internal/vaccinationexecution/app"
	verificationadminuibridge "github.com/vgoats/goatos/backend/internal/verification/adapters/adminuibridge"
	verificationboard "github.com/vgoats/goatos/backend/internal/verification/adapters/boardsource"
	verificationhttp "github.com/vgoats/goatos/backend/internal/verification/adapters/http"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationproofmedia "github.com/vgoats/goatos/backend/internal/verification/adapters/proofmedia"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	weighingboard "github.com/vgoats/goatos/backend/internal/weighing/adapters/boardsource"
	weighinghttp "github.com/vgoats/goatos/backend/internal/weighing/adapters/http"
	weighingpg "github.com/vgoats/goatos/backend/internal/weighing/adapters/postgres"
	weighingverificationbridge "github.com/vgoats/goatos/backend/internal/weighing/adapters/verificationbridge"
	weighingapp "github.com/vgoats/goatos/backend/internal/weighing/app"
	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
	weighingsoppg "github.com/vgoats/goatos/backend/internal/weighingsop/adapters/postgres"
	weighingsopapp "github.com/vgoats/goatos/backend/internal/weighingsop/app"
	workforcehttp "github.com/vgoats/goatos/backend/internal/workforce/adapters/http"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
	workforceports "github.com/vgoats/goatos/backend/internal/workforce/ports"
)

type Config struct {
	HTTPAddr                    string
	Postgres                    platformpg.Config
	Auth                        AuthConfig
	BulkImportPreviewSigningKey string
	RouteMode                   string
}

// AuthModeJWKS selects asymmetric RS256/ES256 token verification via a remote
// JWKS endpoint. This is the intended production auth mode. Requires
// GOATOS_AUTH_ISSUER, GOATOS_AUTH_AUDIENCE, and GOATOS_AUTH_JWKS_URL.
const AuthModeJWKS = "jwks"

const (
	defaultAuthSessionRateLimitPerMinute = 120
	defaultAppCheckJWKSURL               = "https://firebaseappcheck.googleapis.com/v1/jwks"
	apiRouteModeEvents                   = "events"
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
		RouteMode:                   strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_API_ROUTE_MODE"))),
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

// weighingExportProofURLResolver adapts proof ids into backend proof routes for the campaign CSV
// export. It deliberately avoids minting signed GCS URLs while exporting many rows.
type weighingExportProofURLResolver struct {
	baseURL string
}

func newWeighingExportProofURLResolver(_ any, httpAddr string) weighingExportProofURLResolver {
	return weighingExportProofURLResolver{baseURL: weighingExportPublicBaseURL(httpAddr)}
}

func (w weighingExportProofURLResolver) ResolveProofDownloadURL(_ context.Context, _, proofID string) (string, error) {
	proofID = strings.TrimSpace(proofID)
	if proofID == "" {
		return "", nil
	}
	url := "/app/proofs/" + proofID + "/download"
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return url, nil
	}
	// PROTOCOL-RELATIVE GUARD: a path beginning with "//" is scheme-relative ("//evil.com/x"),
	// and naively concatenating it after baseURL would leave a URL some parsers/clients resolve as
	// pointing at a THIRD-PARTY host, not this API -- an open-redirect shape in a value that ends up
	// clickable in an exported sheet. Collapse any leading slashes down to exactly one first, so the
	// result can only ever be a path on this API's own baseURL.
	for strings.HasPrefix(url, "//") {
		url = url[1:]
	}
	// Host-relative backend route: make it absolute against the API's own public base URL so the
	// cell is clickable from wherever the sheet is opened, not just from a browser already pointed
	// at this host.
	if !strings.HasPrefix(url, "/") {
		url = "/" + url
	}
	return w.baseURL + url, nil
}

// weighingExportPublicBaseURL resolves the host+scheme the API is reachable at for turning a
// backend proof route into an absolute, clickable URL.
//
// GOATOS_API_PUBLIC_BASE_URL is the explicit override for stg/prod (or any deployment behind a
// load balancer/proxy, where the bind address is not the public address) and takes precedence
// when set. With no override, this falls back to http://127.0.0.1<GOATOS_HTTP_ADDR> for the
// local/E2E stack, where the API's bind address IS the reachable address.
func weighingExportPublicBaseURL(httpAddr string) string {
	if base := strings.TrimSpace(os.Getenv("GOATOS_API_PUBLIC_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	addr := strings.TrimSpace(httpAddr)
	if addr == "" {
		addr = ":8080"
	}
	if strings.HasPrefix(addr, ":") {
		return "http://127.0.0.1" + addr
	}
	return "http://" + addr
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
	if err := validateRouteMode(cfg.RouteMode); err != nil {
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

	pgCfg := cfg.Postgres
	pgCfg.ApplicationName = platformpg.ServiceApplicationName("api")
	// New pooled connections are warmed in the background (heavy reads prepared / planned once per
	// connection off the request path); statements are registered and the loop started below.
	connWarmer := platformpg.NewConnWarmer(log)
	pgCfg.Warmer = connWarmer
	pool, err := platformpg.Connect(ctx, pgCfg)
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

	grantSource := permissionspg.NewGrantSource(pool, cfg.Postgres.QueryTimeout)
	// DB-backed email allowlist: written by the workforce create-person flow,
	// consulted in union with the GOATOS_AUTH_ALLOWED_EMAILS env list by both
	// the auth middleware and the session-events handler.
	allowedEmailSource := permissionspg.NewAllowedEmailSource(pool, cfg.Postgres.QueryTimeout, log)
	// Grants and person access are coalesced per (tenant, user) for authAccessTTL: one statement
	// per burst instead of per request (auth_read_cache.go).
	authReadCache := newAuthReadCache()
	authz, err := buildAuthMiddleware(cfg.Auth, verifier, appCheckVerifier, cachedGrantSource{inner: grantSource, cache: authReadCache}, allowedEmailSource, log)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if cfg.RouteMode == apiRouteModeEvents {
		return newEventsAPI(cfg, log, pool, binaryMigrationVersion, authz, func(protectedMux *http.ServeMux) {
			appanalyticshttp.Register(protectedMux, appanalyticshttp.NewHandler(pool, log))
		}), nil
	}

	bulkPreviewSigningKey := ""
	if cfg.RouteMode != apiRouteModeEvents {
		var err error
		bulkPreviewSigningKey, err = bulkImportPreviewSigningKey(cfg)
		if err != nil {
			pool.Close()
			return nil, err
		}
	}

	// ONE process-wide read cache for the heavy weighing / Growth Director analytics reads
	// (platform/readcache): single-flight, stale-while-revalidate, bounded LRU, and scoped
	// eviction on committed weighing / feed-issue / assumptions writes -- locally after commit and
	// on sibling instances through the dedicated LISTEN connection started below.
	analyticsReadCache := readcache.New(readcache.DefaultOptions("analytics"))
	identityRepo := identitypg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)
	identityService := identityapp.NewService(identityRepo).WithBulkPreviewSigningKey(bulkPreviewSigningKey)
	identityHandler := identityhttp.NewHandler(identityService, log)
	// The sale-count gate needs one fact from the sales ledger (how many animals a deal
	// is for). It arrives through a BRIDGE rather than a join, so identity's own queries
	// stay clear of the sales schema -- see migration 000177.
	saleAllocationHandler := identityhttp.NewSaleAllocationHandler(
		identityapp.NewSaleAllocationService(identityRepo, identityRepo, salesbridge.New(pool)), log)
	bulkStatusRepo := bulkstatuspg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	bulkStatusService := bulkstatusapp.NewService(bulkStatusRepo, bulkStatusRepo).WithSigningKey(bulkPreviewSigningKey)
	bulkStatusHandler := bulkstatushttp.NewHandler(bulkStatusService, log)
	locationsRepo := locationspg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)
	locationsService := locationsapp.NewService(locationsRepo)
	locationsHandler := locationshttp.NewHandler(locationsService, log)
	workforceRepo := workforcepg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	// The feed & water removal cutoff is CONFIG (maintainer decision 2026-09-07):
	// one Postgres reader over feed_water_removal_config, handed as an opaque
	// value to weighing, PC Care and the phone bootstrap. Weighing never names
	// the table — it is isolated from every non-weighing table.
	feedWaterRemovalCutoffs := fwrpg.NewReader(pool, cfg.Postgres.QueryTimeout)
	workforceService := workforceapp.NewService(workforceRepo).
		WithFeedWaterRemovalCutoff(feedWaterRemovalCutoffs)
	workforceHandler := workforcehttp.NewHandler(workforceService, log)
	// People/HRMS directory + in-app onboarding. The Firebase identity adapter
	// activates only when a Firebase project can be resolved (explicit env or a
	// securetoken issuer); local dev-headers/HS256 environments run without it
	// and the create-person route fails closed with identity_unavailable.
	var workforceIdentity workforceports.IdentityProvider
	if projectID := firebaseIdentityProjectID(cfg.Auth.Issuer); projectID != "" {
		var identityOpts []firebaseidentity.Option
		// Emulator/local override: point the adapter at a Firebase Auth
		// emulator (or a local stand-in) instead of the live Identity Toolkit.
		// The emulator accepts any bearer, so a static token source suffices.
		if baseURL := strings.TrimSpace(os.Getenv("GOATOS_FIREBASE_IDENTITY_BASE_URL")); baseURL != "" {
			identityOpts = append(identityOpts,
				firebaseidentity.WithBaseURL(baseURL),
				firebaseidentity.WithTokenSource(oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "emulator"})),
			)
		}
		identityClient, err := firebaseidentity.New(projectID, identityOpts...)
		if err != nil {
			pool.Close()
			return nil, err
		}
		workforceIdentity = identityClient
	}
	peopleService := workforceapp.NewPeopleService(workforceRepo, workforceIdentity, cfg.Auth.Issuer)
	peopleHandler := workforcehttp.NewPeopleHandler(peopleService, log)
	// Per-person module access (maintainer decision 2026-08-24). Its own repository
	// because it owns its own tables; the SAME pool, so a save and the read that
	// enforces it see one database.
	accessRepo := workforcepg.NewAccessRepository(pool)
	accessService := workforceapp.NewAccessService(accessRepo)
	accessHandler := workforcehttp.NewAccessHandler(accessService, log)
	notificationAudienceHandler := notificationaudiencehttp.NewHandler(
		notificationaudienceapp.NewConfigService(notificationaudiencepg.NewRepository(pool, cfg.Postgres.QueryTimeout)), log)
	// Browser (Chrome) web push registrations for admin-web. Built beside the designation matrix
	// because both answer "who hears a notification", from opposite ends: that one is the tenant's
	// per-designation audience, this one is one person's own delivery addresses.
	browserPushService := browserpush.NewService(browserpushpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	browserPushHandler := browserpushhttp.NewHandler(browserPushService, log)
	rosterService := workforceapp.NewRosterService(workforceRepo, workforceRepo)
	rosterHandler := workforcehttp.NewRosterHandler(rosterService, log)
	// Clock In / Out (docs/features/clock-in-out/plan.md): punches + presence.
	// Leave requests (docs/features/leave-requests/plan.md): raise, withdraw,
	// park head + HR approval, CEO routing config. The clock status read
	// carries the requester's leave beside their clockings.
	leaveService := workforceapp.NewLeaveService(workforceRepo, workforceRepo)
	leaveHandler := workforcehttp.NewLeaveHandler(leaveService, log)
	// HRMS Timetable (maintainer request 2026-09-30): who works which shift at each park.
	timetableHandler := workforcehttp.NewTimetableHandler(workforceapp.NewTimetableService(workforceRepo), log)
	clockService := workforceapp.NewClockService(workforceRepo, workforceRepo, workforceRepo).WithLeave(leaveService)
	clockHandler := workforcehttp.NewClockHandler(clockService, log)
	proofStorage, err := buildProofStorage()
	if err != nil {
		pool.Close()
		return nil, err
	}
	proofRepo := proofpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	sopRepo := soppg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	proofService := proofapp.NewService(proofRepo, proofStorage)
	proofHandler := proofhttp.NewHandler(proofService, log)
	sopService := sopapp.NewService(sopRepo).WithProofValidator(proofService).
		WithTaskTypeSource(sopRepo).
		WithDesignationSource(sopRepo).
		// The animal-purchase inspection document is validated by the module that compiles it.
		WithFormDSLContract(animalpurchaseapp.InspectionSOPContract).
		// VENDOR FORM (2026-09-19): the sales.vendor version's `vendor_form` -- the questions
		// the Add / Edit vendor screens ask -- is validated by the register that runs it.
		WithFormDSLContract(procurementapp.VendorFormSOPContract).
		// THE FEED PURCHASE FORM IS AUTHORED (2026-09-20): the procurement.feed_purchase_form
		// version's `feed_purchase_form` -- what the Record purchase screens ask -- is validated
		// by the ledger that runs it.
		WithFormDSLContract(procurementapp.FeedPurchaseFormSOPContract).
		// WEIGHING SOP (maintainer decision 2026-09-15): the weighing.session version's
		// `weighing` section is validated here so a document the planner could not run
		// is never saved.
		WithFormDSLContract(weighingsopapp.WeighingSOPContract).
		// PC CARE SOP (maintainer decision 2026-09-22): the `pc_care` section of pc_care.tasks
		// is validated at save so a card the operators could not run is never published.
		WithFormDSLContract(pccaresopapp.PCCareSOPContract).
		// FEED SOP (maintainer decision 2026-09-16): the feed.direction / feed.packing /
		// feed.transport versions' `feed` section -- the cards the crew runs -- is validated here
		// so a card the phone could not render is never saved.
		WithFormDSLContract(feedsopapp.FeedSOPContract).
		WithFormDSLContract(shiftingsopapp.ShiftingSOPContract).
		// HERD OPERATIONS CAPTURE CARD (2026-09-16): counts.birth / counts.death `capture_card`.
		WithFormDSLContract(countssopapp.CaptureCardContract).
		// THE TOXIN PROCEDURE IS AUTHORED (2026-09-20): the procurement.toxin_test version's
		// `toxin` section -- the steps of the aflatoxin test -- is validated here, so a document
		// that renumbers its steps, loses its reading step or gates one on a later step is
		// refused at publish rather than discovered by a tester standing over a strip.
		WithFormDSLContract(toxinapp.ToxinSOPContract)

	protocolRepo := protocolpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	obligationRepo := obligationpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)
	businessAuditRecorder := platformaudit.NewPostgresRecorder(pool, cfg.Postgres.QueryTimeout)
	outboxRepo := outboxpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	outboxHandler := outboxhttp.NewHandler(outboxRepo, businessAuditRecorder, log)
	operationsAuditService := operationsauditapp.NewService(operationsauditpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	operationsAuditHandler := operationsaudithttp.NewHandler(operationsAuditService, log)
	processIntegrityRepo := processintegritypg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	processIntegrityService := processintegrityapp.NewService(processIntegrityRepo)
	processIntegrityHandler := processintegrityhttp.NewHandler(processIntegrityService, log)
	vaccExecOwnership := vaccexecroster.NewOwnershipAdapter(rosterService)
	vaccExecRepo := vaccexecpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	vaccExecService := vaccexecapp.NewService(vaccExecRepo, vaccExecOwnership).
		WithProofURLResolver(newWeighingExportProofURLResolver(proofService, cfg.HTTPAddr))
	vaccExecHandler := vaccexechttp.NewHandler(vaccExecService, obligationRepo, log).
		WithOperatorAssignmentConfigWriter(vaccExecService).
		WithOperatorShiftWriter(vaccExecService).
		WithCapacityConfigWriter(vaccExecService)
	weighingRepo := weighingpg.NewRepository(pool, cfg.Postgres.QueryTimeout).
		WithProofURLResolver(newWeighingExportProofURLResolver(proofService, cfg.HTTPAddr)).
		WithReadCache(analyticsReadCache)
	// PHASE 2: the same repository also serves the Calendar / Control Tower
	// weighing process-state read model (declared `weighing_work_item` grain).
	weighingService := weighingapp.NewService(weighingRepo).
		WithProcessStateReader(weighingRepo).
		// The fasting (feed & water removal) precondition store rides the same
		// repository (maintainer decision 2026-09-03, weighing/domain/fasting.go).
		WithFastingStore(weighingRepo).
		WithFeedWaterRemovalCutoff(feedWaterRemovalCutoffs).
		// WEIGHING SOP: the rules a task is planned on and runs under, read through the
		// out-of-package adapter (weighing itself never names sop_versions).
		WithSOPRules(weighingsoppg.NewRulesSource(pool, cfg.Postgres.QueryTimeout), weighingRepo)
	weighingHandler := weighinghttp.NewHandler(weighingService, log).WithMediaResolver(proofService)
	// Growth Director: read-only reporting over weighing + herd + feed tables.
	// Deliberately its OWN module, outside backend/internal/weighing, because
	// weighing is isolated from the herd and these widgets need breed/sex and
	// the feed sheet.
	growthDirectorRepo := growthdirectorpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCache(analyticsReadCache)
	growthDirectorService := growthdirectorapp.NewService(growthDirectorRepo)
	growthDirectorHandler := growthdirectorhttp.NewHandler(growthDirectorService, log)
	calendarService := calendarapp.NewService(calendarpg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	calendarHandler := calendarhttp.NewHandler(calendarService, log)
	adminUIService := adminuiapp.NewService(adminuipg.NewRepository(pool, cfg.Postgres.QueryTimeout))
	adminUIHandler := adminuihttp.NewHandler(adminUIService)
	appAnalyticsHandler := appanalyticshttp.NewHandler(pool, log)
	appConfigHandler := appconfighttp.NewHandler(appconfigapp.NewService(appconfigapp.ConfigFromEnv()), log)
	// Counts Breakdown filter facets, evicted cross-instance on committed herd-register writes
	// (readcache.Listener below; migration 000417 triggers).
	countsReadCache := readcache.New(readcache.DefaultOptions("counts"))
	countsRepo := countspg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithFacetCache(countsReadCache)
	countsService := countsapp.NewService(countsRepo)
	countsProofValidator := countsproof.NewValidator(proofRepo)
	// The cause-of-death vocabulary: the diagnosis register plus the tenant's Health Config
	// diseases (wired below, once the Health repository exists), folded into a searchable
	// list. It is the death form's dropdown AND the check that refuses a cause the register
	// does not name — one source, so the list an operator picks from and the list the
	// server accepts cannot drift apart. Built here, ahead of Health's own wiring below,
	// because Counts -> Mortality also reads it to NAME a recorded cause.
	healthDeathCauseService := healthapp.NewDeathCauseCatalogService()
	herdRegisterService := countsapp.NewHerdRegisterService(countsRepo).
		WithMilkPreparationProofValidator(countsProofValidator).
		WithMilkFeedingProofValidator(countsProofValidator).
		WithDeathCauseLabeler(healthDeathCauseService)
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
	// The identity seam lets the Record shed fallback step place a kid inside that action write's
	// own transaction (tasks/adapters/postgres/newborn_placement.go). Without it the step would
	// record an answer and leave the kid where it was.
	tasksWorkflowRepo := taskspg.NewRepository(pool, cfg.Postgres.QueryTimeout).
		WithIdentityTxWriter(identityRepo)
	healthRepo := healthpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	healthService := healthapp.NewService(healthRepo)
	healthHandler := healthhttp.NewHandler(healthService, log)
	// Herd Signals: BLE ear-tag telemetry ingest + live/timeline/gateways/insights read model.
	// Owns its own four tables (herd_signal_gateways/packets/tag_latest/activity_windows) and
	// reads goat_identifiers/goats/locations/shed_partitions read-only to resolve a tag to an
	// animal and operational location -- it writes to none of them.
	herdSignalsRepo := herdsignalspg.NewRepository(pool).WithReadCacheInvalidator(analyticsReadCache)
	herdSignalsService := herdsignalsapp.NewService(herdSignalsRepo, log)
	herdSignalsHandler := herdsignalshttp.NewHandler(herdSignalsService, log)
	// The authored treatment rulebook behind /health/config. Same repository, because the
	// protocol tables belong to the Health module and a second package writing them would be the
	// cross-module table write AGENTS.md bans -- the authoring surface is a different API over
	// the same module, not a different module.
	// The medicine picker's registry: a treatment step names an item from
	// /configuration/items, never free text (maintainer instruction 2026-09-21).
	// The cause-of-death list is the built-in register PLUS what this tenant authored in
	// Health Config (maintainer decision 2026-09-25), so it reads the same repository and
	// drops a tenant's cached list the moment this process publishes a change to it.
	healthDeathCauseService.WithTenantSource(healthRepo)
	healthConfigService := healthapp.NewConfigService(healthRepo).WithMedicineCatalog(healthRepo).
		WithRulebookChanged(healthDeathCauseService.Invalidate)
	healthConfigHandler := healthhttp.NewConfigHandler(healthConfigService, log)
	// The diagnosis register is the second tab of the same screen and shares its
	// repository: one Health Config, one rulebook.
	// The routing half: which diagnosis types exist and which animals reach each one. The
	// register editor is given the same source so a type created here can have its rules
	// authored -- otherwise the screen would mint types nobody could ever write for.
	healthDiagnosisTypeService := healthapp.NewDiagnosisTypeService(healthRepo)
	healthDiagnosisTypeHandler := healthhttp.NewDiagnosisTypeHandler(healthDiagnosisTypeService, log)
	healthRegisterConfigService := healthapp.NewRegisterConfigService(healthRepo).WithTypes(healthRepo).
		WithRulebookChanged(healthDeathCauseService.Invalidate)
	healthRegisterConfigHandler := healthhttp.NewRegisterConfigHandler(healthRegisterConfigService, log)
	// Download a type's rulebook, edit it, upload it back. The same service, because an upload
	// lands in the ordinary draft and publishes through the ordinary gate.
	healthRegisterSheetHandler := healthhttp.NewRegisterSheetHandler(healthRegisterConfigService, log)
	// The diagnosis engine reads the tenant's published Health Config register
	// inside the observation transaction, with the committed seed as the fallback
	// for unseeded tenants.
	healthDiagnosisService, err := healthapp.NewDiagnosisService(healthpg.NewDiagnosisRepository(healthRepo))
	if err != nil {
		pool.Close()
		return nil, err
	}
	healthDiagnosisHandler := healthhttp.NewDiagnosisHandler(healthDiagnosisService, log)
	// Health Analytics: the leadership read behind /health/analytics. Same
	// repository for the same reason the authoring surface shares it -- the
	// clinical tables belong to the Health module, and a second package reading
	// them would be the cross-module coupling this repo bans. Read-only: this
	// service opens no case and completes no session.
	healthAnalyticsService := healthapp.NewAnalyticsService(healthRepo)
	healthAnalyticsHandler := healthhttp.NewAnalyticsHandler(healthAnalyticsService, log)
	healthDeathCauseHandler := healthhttp.NewDeathCauseHandler(healthDeathCauseService, log)
	countsApprovalRepo := countspg.NewRepository(pool, cfg.Postgres.QueryTimeout).
		WithIdentityTxWriter(identityRepo).
		WithDeathEvidenceTxGate(tasksWorkflowRepo)
	countsApprovalService := countsapp.NewApprovalService(countsApprovalRepo, identityService, nil)
	// Shifting execution shares countsApprovalRepo because that repository already carries the
	// identity transaction seam the relocation runs through -- and the relocation now happens HERE,
	// at completion, rather than at approval.
	countsShiftingExecutionService := countsapp.NewShiftingExecutionService(countsApprovalRepo, nil).
		// SHIFTING SOP (maintainer decision 2026-09-16): the raise extras, completion card and
		// high-priority card come from the PUBLISHED shifting SOP version, pinned per movement at
		// raise; shiftingsop is the only reader of sop_versions on counts' behalf, and the proof
		// register check answers each slot's kind by name.
		WithSOPRules(shiftingsoppg.NewRulesSource(pool, cfg.Postgres.QueryTimeout), countsApprovalRepo).
		WithProofMedia(countsProofValidator)
	// Pen reconciliation (maintainer decision 2026-09-02): the Reconcile queue and the
	// operator's return-video submission. The bare countsRepo suffices — no identity seam,
	// because a reconciliation never rewrites the register.
	countsPenReconciliationService := countsapp.NewPenReconciliationService(countsRepo, nil)
	countsAppWriteHandler := countshttp.NewAppWriteHandler(countsService, log).
		WithApprovalWorkflow(countsApprovalService, identityService).
		// The clinical vocabulary a death may be attributed to belongs to Health. Counts
		// holds a narrow port over it so a coded cause is refused at RAISE time, in front
		// of the operator, rather than surfacing later in an approver's queue.
		WithDeathCauses(healthDeathCauseService).
		// Raiser and shed NAMES for the approvals queue, so neither the phone nor admin-web
		// renders a UUID at an approver (golden frontend rule: the label is backend-owned).
		WithApprovalNames(countsApprovalRepo).
		WithShiftingExecutionWorkflow(countsShiftingExecutionService).
		WithPenReconciliationWorkflow(countsPenReconciliationService).
		// SOP capture card (2026-09-16): the Add birth / Add death extras, read from the SOP
		// library through countssop and judged against the proof register.
		WithCaptureCards(countsapp.NewCaptureCardService(countssoppg.NewCaptureCardSource(pool, cfg.Postgres.QueryTimeout), countsProofValidator))
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
	// Experiment pens read their head count from the SAME projection and pen matching the feed sheet
	// multiplies by (maintainer decision 2026-09-24), through feed direction's own grain reader.
	feedConfigService := feedconfigapp.NewService(feedconfigpg.NewRepository(pool, cfg.Postgres.QueryTimeout)).
		WithPenHeadCounts(feedconfigcounts.New(feeddirectioncounts.NewReader(countsService)))
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
	feedDirectionRepo := feeddirectionpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)
	feedDirectionService := feeddirectionapp.NewService(
		feedDirectionRepo,
		feeddirectioncounts.NewReader(countsService),
	).
		WithIssueStore(feedDirectionRepo).
		WithScheduleReader(feedDirectionRepo).
		// The old instant completion store (feed_direction_session_completions) is deliberately NOT wired:
		// with no CompletionStore, CompleteSession fails closed with ports.ErrCompletionUnavailable, so the
		// pre-gate path cannot write 'completed' at operator submit and walk around the verification gate.
		// Its route is unregistered too (feeddirection/adapters/http.Register).
		// Feed DISTRIBUTION verification gate (maintainer decision, 2026-07-26): a SEPARATE store on a NEW
		// table (feed_distribution_completions). The enqueue seam is wired below, once verificationService
		// exists.
		WithDistributionStore(feedDirectionRepo).
		// Feed PACKING verification gate (maintainer decision, 2026-07-26, SUPERSEDING the "packing stays
		// instant" rule): a SEPARATE store on a NEW table (feed_packing_completions). The packing overlay
		// now reads verified rows from here, and the enqueue seam is wired below.
		WithPackingStore(feedDirectionRepo).
		// Feed WASTAGE verification gate (maintainer decision, 2026-08-18): a SEPARATE store on a
		// NEW table (feed_wastage_completions), one pen-day task per EXPERIMENT pen. The enqueue
		// seam is wired below, once verificationService exists.
		WithWastageStore(feedDirectionRepo).
		WithTransportStore(feedDirectionRepo).
		// FEED SOP (2026-09-16): the cards -- capture slots and questions per stage -- from the
		// published feed.* SOP versions; the sheet pins them at issue, the completions judge
		// against the pin, the reads serve them to the phone.
		WithSOPRules(feedsoppg.NewRulesSource(pool, cfg.Postgres.QueryTimeout)).
		WithProofValidator(feeddirectionproof.NewValidatorWithPool(proofRepo, pool)).
		WithAnalyticsReader(feedDirectionRepo).
		// The feed module's own lifecycle alerts feed (GET /app/feed/alerts), the twin of
		// weighing/vaccination's alerts feeds. Same repository instance already used for
		// config/issue/schedule/completion reads implements ports.AlertsRepository.
		WithAlertsRepository(feedDirectionRepo).
		WithGeneratedBy("goatos-api")
	feedDirectionHandler := feeddirectionhttp.NewHandler(feedDirectionService, log)
	// PC Care (module_key pc_care, maintainer decision 2026-08-21): planner-assigned deworming /
	// ticks removal / hoof trimming / hair trimming tasks with per-animal live-camera video
	// proof. The verification enqueue seam is wired below, once verificationService exists.
	pcCareRepo := pccarepg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	pcCareService := pccareapp.NewService(pcCareRepo).
		WithRoundStore(pcCareRepo).
		// Care Coverage board (maintainer request 2026-09-25): pens x the five care categories.
		WithPenCareCoverage(pcCareRepo).
		WithFeedWaterRemovalCutoff(feedWaterRemovalCutoffs).
		WithProofValidator(pccareproof.NewValidator(proofRepo)).
		// PC CARE SOP (maintainer decision 2026-09-22): the rules a task is planned on and runs
		// under come from the published pc_care.tasks version, read outside the module.
		WithSOPRules(pccaresoppg.NewRulesSource(pool, cfg.Postgres.QueryTimeout), pcCareRepo)
	pcCareHandler := pccarehttp.NewHandler(pcCareService, log)
	procurementService := procurementapp.NewService(procurementpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)).WithVaccinationCanceler(obligationRepo)
	procurementHandler := procurementhttp.NewHandler(procurementService, log)
	// The vendor register shares procurement's postgres repository (it owns procurement_vendors)
	// but has its own thin service: a contact book has no state machine to orchestrate.
	procurementVendorHandler := procurementhttp.NewVendorHandler(
		procurementapp.NewVendorService(procurementpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)).
			// A vendor's voice note is a proof (audio, in-app microphone) and is checked against
			// the proof store before it is stored on the vendor (maintainer decision 2026-09-03).
			WithVoiceNoteValidator(procurementproof.NewValidator(proofRepo)).
			// The Add / Edit vendor form is the published sales.vendor SOP (2026-09-19); answers
			// are checked against the version the screen rendered.
			WithVendorFormSource(procurementpg.NewVendorFormSource(pool)), log)
	// The feed PURCHASE ledger (maintainer decision 2026-08-24, retiring the read-only half of
	// migration 000174's lock). Procurement owns the write; feeddirection keeps the stock read.
	procurementFeedPurchaseHandler := procurementhttp.NewFeedPurchaseHandler(
		procurementapp.NewFeedPurchaseService(procurementpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)).
			WithFormSource(procurementpg.NewFeedPurchaseFormSource(pool)), log)
	// The Sales page's LOAD-WISE reconciliation and the load-cost entry (maintainer decision
	// 2026-08-31, docs/decisions/sales-loadwise.md).
	procurementLoadwiseHandler := procurementhttp.NewLoadwiseHandler(
		procurementapp.NewLoadwiseService(procurementpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)), log)
	// Buyer analytics (maintainer request 2026-09-15): the same recorded procurement-reads-sales
	// shape as load-wise, one read behind /sales/buyer-analytics.
	procurementBuyerAnalyticsHandler := procurementhttp.NewBuyerAnalyticsHandler(
		procurementapp.NewBuyerAnalyticsService(procurementpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)), log)
	// Farm born (maintainer request 2026-09-18): the animals born on this farm, the same
	// recorded procurement-reads-sales shape as load-wise, one read behind /sales/farm-born.
	procurementFarmBornHandler := procurementhttp.NewFarmBornSalesHandler(
		procurementapp.NewFarmBornSalesService(procurementpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)), log)
	// Animal purchases (maintainer decision 2026-09-13): the buying desk's loads and candidate
	// animals with a video each, and the CEO/CXO's accept / reject.
	animalPurchaseHandler := animalpurchasehttp.NewHandler(
		animalpurchaseapp.NewService(
			animalpurchasepg.NewRepository(pool, cfg.Postgres.QueryTimeout),
			animalpurchaseproof.NewValidator(proofRepo),
			animalpurchaseproof.NewMedia(proofService)).
			// PROCUREMENT SOP: the inspection questionnaire is the published SOP version.
			WithCatalogSource(animalpurchasepg.NewCatalogSource(pool)), log)
	// Toxin (maintainer decision 2026-08-25): the aflatoxin strip-test module. Tasks are
	// born from procurement.feed_purchase.reached (consumer wired in kernelstages); the
	// routes here serve the tester's guided step flow and the CEO/CXO-only review.
	toxinHandler := toxinhttp.NewHandler(
		toxinapp.NewService(toxinpg.NewRepository(pool, cfg.Postgres.QueryTimeout), toxinproof.NewValidator(proofRepo)).
			WithProcedureSource(toxinpg.NewProcedureSource(pool)), log)
	// Market survey (maintainer decision 2026-09-14): the morning market-price calls. Config
	// and analytics under Sales on admin-web; the day's cards and the entry write on the phone.
	marketHandler := markethttp.NewHandler(marketapp.NewService(marketpg.NewRepository(pool, cfg.Postgres.QueryTimeout)), log).
		// Reporters on the Sales SOP page (2026-09-19): who makes the calls, toggled through the
		// one person-access write path.
		WithReporterSource(workforceapp.NewMarketReporterSource(accessService, accessRepo))
	// Leadership Tasks (maintainer decision 2026-09-04): a director's ask of the CXO desk.
	// The service also feeds the drawer badge (unseen assigned tasks) into /app/bootstrap.
	leadershipTasksService := leadershiptasksapp.NewService(
		leadershiptaskspg.NewRepository(pool, cfg.Postgres.QueryTimeout), leadershiptasksproof.NewResolver(proofRepo)).
		WithAttachmentDownloader(proofService)
	leadershipTasksHandler := leadershiptaskshttp.NewHandler(leadershipTasksService, log)
	// In-app notification centre: the caller's own notification feed and the mark-as-read
	// write, read straight off notification_requests. It owns no table and produces no
	// notification -- the bridge consumers remain the only writers of the feed's rows.
	notificationCentreHandler := notificationcentrehttp.NewHandler(
		notificationcentreapp.NewService(notificationcentrepg.NewRepository(pool, cfg.Postgres.QueryTimeout)), log)
	// Work Board (maintainer decision 2026-09-10): the cross-module read. Every module
	// contributes a Source over its OWN tables; the board composes them here and reads no
	// table itself. Registration order does not matter -- the service sorts into board order.
	workBoardService := newWorkBoardService(pool, cfg.Postgres.QueryTimeout, processIntegrityRepo)
	workBoardHandler := workboardhttp.NewHandler(workBoardService, log).WithFlags(workboardapp.NewFlagService(workBoardService, leadershipTasksService, workboardpg.NewParkHeadResolver(pool, cfg.Postgres.QueryTimeout)))
	// Alerts (maintainer decision 2026-09-16): the page below the Work Board. It stores only
	// its rule config; every alert is derived per request from rows other modules froze --
	// the feed sheet, the shifting register -- and the stock rule reuses the feed module's
	// own low-stock read so the page, the Stock tab and the daily push agree.
	// Per-tenant low-stock value for the alerts stock rule, evicted cross-instance on committed
	// feed-stock writes (readcache.Listener below; migration 000416 triggers).
	alertsReadCache := readcache.New(readcache.DefaultOptions("alerts"))
	alertsRepo := alertspg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	alertsService := alertsapp.NewService(alertsRepo, alertsRepo, alertsRepo, alertspg.NewLowStockReader(feedDirectionRepo).WithCache(alertsReadCache), alertsRepo, log).
		// Composed event alerts (maintainer request 2026-09-16): the farm adds "tell me when a
		// birth / death / sale / shifting / feed purchase happens" from the drawer; the readers
		// are per catalog kind over the owning module's tables.
		WithEvents(alertsRepo, alertsRepo)
	alertsHandler := alertshttp.NewHandler(alertsService, log)
	// Pen visits (maintainer decisions 2026-09-07 and 2026-09-14): the Tasks module's "For me"
	// tab -- the work the system owes this person; pen visits are its first card type. The
	// kernel raises them; this serves the visitor's list and the submit that carries the live
	// video. The module badge is the SUM of both halves of Tasks: unseen asks plus visits still
	// owed. The visit is NOT attached to the PC Care task or the vaccination shed card any
	// more (the 2026-09-12 fold is retired); the parent still closes on the visit's approval.
	penVisitsRepo := penvisitspg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	penVisitsService := penvisitsapp.NewService(penVisitsRepo).
		WithProofValidator(penvisitsproof.NewValidator(proofRepo))
	penVisitsHandler := penvisitshttp.NewHandler(penVisitsService, log)
	// Pen routines (maintainer instruction 2026-09-16, docs/decisions/pen-routines.md): the
	// configurable recurring pen checks. The kernel raises them; this serves the assignee's
	// list, check-in and submit, and the CXO's authoring routes behind /routines. The module
	// badge chains over the Tasks one so the workforce service keeps ONE badge source.
	penRoutinesRepo := penroutinespg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	penRoutinesService := penroutinesapp.NewService(penRoutinesRepo).
		WithProofValidator(penroutinesproof.NewValidator(proofRepo))
	penRoutinesHandler := penroutineshttp.NewHandler(penRoutinesService, log)
	penRoutinesAdminHandler := penroutineshttp.NewAdminHandler(penroutinesapp.NewAuthoringService(penRoutinesRepo), log)
	// Configuration -> Items and settings (2026-09-18): the reference registers, read on
	// configuration.read and written on configuration.write (route table).
	configurationRepo := configurationpg.NewRepository(pool, cfg.Postgres.QueryTimeout).WithReadCacheInvalidator(analyticsReadCache)
	configurationService := configurationapp.NewService(configurationRepo)
	// Bulk sheets (2026-09-18): uploads are staged and validated/applied by an in-process
	// importer kicked per job; the kernel worker's ConfigurationImportStage finishes any job whose
	// claim lapsed. Animals sheets ride identity's own bulk pipeline.
	configurationImporter := configurationapp.NewImporter(configurationService, configurationRepo, identityService, "", log)
	configurationHandler := configurationhttp.NewHandler(configurationService, log).
		WithBulk(configurationService, configurationImporter, configurationRepo)
	// The Approvals module badge (2026-09-25) chains outermost: pending requests THIS caller may
	// decide, from the same decidable types and park scope the queue applies.
	workforceService.WithModuleBadges(countshttp.NewApprovalsBadges(
		penroutinesapp.NewModuleBadges(penvisitsapp.NewModuleBadges(leadershipTasksService, penVisitsService), penRoutinesService),
		countsApprovalService))
	// The sales module: its own bounded ledger (sales_*) with a thin service -- a commercial
	// record with no state machine to orchestrate.
	// The feed store is wired in so a sale taking more feed than it holds asks the desk to confirm
	// it once (migration 000422). Without this the confirmation silently never fires, so the
	// wiring is asserted by a test rather than left to this line being noticed.
	salesService := salesapp.NewSalesService(salespg.NewRepository(pool, cfg.Postgres.QueryTimeout)).
		WithFeedStock(feedDirectionRepo)
	salesHandler := saleshttp.NewSalesHandler(
		salesService, log)
	vaccinationRepo := vaccinationpg.NewRepository(pool, cfg.Postgres.QueryTimeout)
	vaccinationService := vaccinationapp.NewService(vaccinationRepo).WithAnchorObligationSuppressor(obligationRepo)
	vaccinationService.WithProofCompletionObligationCompleter(obligationRepo)
	proofService.WithCompletedProofObserver(vaccinationService)
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
	verificationMedia := verificationproofmedia.NewResolver(proofService).
		WithActionPresentationResolver(tasksWorkflowRepo)
	processIntegrityService.WithMediaResolver(verificationMedia)
	verificationService := verificationapp.NewService(verificationRepo, verificationMedia)
	if err := verificationService.RegisterCategory(verificationcatalog.Vaccination); err != nil {
		pool.Close()
		return nil, err
	}
	// Weighing uses the weighingdomain constants rather than literals: main's weighing
	// feature filters its own queue by those same constants, so a hand-written vertical
	// here would silently not match its reads.
	for _, def := range []verificationdomain.CategoryDefinition{
		verificationcatalog.Weighing,
		verificationcatalog.WeighingFasting,
		verificationcatalog.HealthAdults,
		verificationcatalog.HealthKids,
	} {
		if err := verificationService.RegisterCategory(def); err != nil {
			pool.Close()
			return nil, err
		}
	}
	// Health treatment-evidence verification (2026-08-29): a proof-carrying treatment completion
	// enqueues one item into health_adults/health_kids (the categories registered just above,
	// which until now had no producer). Post-task evidence review only -- the verdict applier
	// lives in eventwiring.RegisterVerificationAppliers.
	healthService.WithVerificationEnqueuer(healthverificationbridge.New(verificationService))
	weighingVerificationBridge := weighingverificationbridge.New(verificationService)
	weighingService.WithVerificationEnqueuer(weighingVerificationBridge)
	// Same bridge, retire direction: a reopened lump-sum bucket withdraws its
	// submission, so the item raised for it must stop being decidable.
	weighingService.WithVerificationWithdrawer(weighingVerificationBridge)
	// Same bridge, RELABEL direction: the verifier's weight correction replaces the
	// weight the item's subject label states, so the label is recomposed or the
	// queue keeps showing the number she just replaced.
	//
	// The correction is served by its own service and its own narrow store, NOT by
	// weighingService: it is the verifier's act on one observation and must not be
	// able to reach the planner/execution writes.
	weighingWeightCorrections := weighingapp.NewWeightCorrectionService(weighingRepo, log).
		WithVerificationRelabeler(weighingVerificationBridge)
	weighingHandler.WithWeightCorrector(weighingWeightCorrections)
	// THE APPROVE CARRIES THE WEIGHT (maintainer decision 2026-08-20). The same correction service,
	// reached by verification when the verifier's approve carries a number, so she types it and
	// presses Approve once instead of saving and then approving -- a pair whose save relabelled the
	// item, bumped row_version, and fenced out the approve that followed. The standalone route above
	// stays served for installed APKs that still show their own save button.
	if err := verificationService.RegisterMeasurementApplier(
		weighingdomain.VerificationCategoryWeighing,
		weighingverificationbridge.NewMeasurementApplier(weighingWeightCorrections),
	); err != nil {
		pool.Close()
		return nil, err
	}
	// Shifting-move verification (maintainer decision, 2026-07-26): a shed move is applied only after
	// a verifier approves the operator's mandatory video, so shifting is a verification producer just
	// like vaccination. Register its category and wire the enqueue seam into the execution service now
	// that the verification service exists.
	if err := verificationService.RegisterCategory(verificationcatalog.Shifting); err != nil {
		pool.Close()
		return nil, err
	}
	countsShiftingExecutionService.WithVerificationEnqueuer(
		countsbridge.NewShiftingVerificationEnqueuer(verificationService))
	// Pen reconciliation (maintainer decision 2026-09-02): the herd register is truth; a
	// weighing submit that finds a scanned animal in the wrong pen raises a Reconcile card,
	// and the operator's return video is the item this category reviews. There is NO approver
	// step — the verifier's verdict is the only gate — and neither the completion nor the
	// verdict ever rewrites the register.
	if err := verificationService.RegisterCategory(verificationcatalog.PenReconciliation); err != nil {
		pool.Close()
		return nil, err
	}
	countsPenReconciliationService.WithVerificationEnqueuer(
		countsbridge.NewPenReconciliationVerificationEnqueuer(verificationService))
	// Milk preparation is a park-day work item. Every applicable step owns a distinct live-camera
	// video (five with goat milk, two without), and all videos travel on one verifier item so one
	// verdict completes or reworks the whole preparation attempt.
	if err := verificationService.RegisterCategory(verificationcatalog.MilkPreparation); err != nil {
		pool.Close()
		return nil, err
	}
	herdRegisterService.WithMilkPreparationVerificationEnqueuer(
		countsbridge.NewMilkPreparationVerificationEnqueuer(verificationService))
	if err := verificationService.RegisterCategory(verificationcatalog.MilkFeeding); err != nil {
		pool.Close()
		return nil, err
	}
	herdRegisterService.WithMilkFeedingVerificationEnqueuer(countsbridge.NewMilkFeedingVerificationEnqueuer(verificationService))
	// Feed distribution verification (maintainer decision, 2026-07-26): a feed-direction session is
	// completed only after a verifier approves the operator's video + water proof, so feed is a
	// verification producer just like vaccination and shifting. Register its category and wire the
	// enqueue seam into the feed-direction service now that verificationService exists. Weight photo,
	// feed-distribution video, and water-distribution video travel together on one verification item.
	if err := verificationService.RegisterCategory(verificationcatalog.FeedDistribution); err != nil {
		pool.Close()
		return nil, err
	}
	feedDirectionService.WithDistributionVerificationEnqueuer(
		feeddirectionverificationbridge.New(verificationService))
	// THE DISTRIBUTION VERIFIER RECORDS THE TOTAL FEED (maintainer decision 2026-09-28): the item
	// carries one blind "Total feed given (kg)" box and her approve carries the reading, written
	// onto feed_distribution_completions through the producer's own store BEFORE the verdict, and
	// checked against the planned pen-session total (warned once, direction only, past 5%).
	if err := verificationService.RegisterMeasurementApplier(
		feeddirectiondomain.VerificationCategoryFeed,
		feeddirectionverificationbridge.NewDistributionMeasurementApplier(feedDirectionRepo),
	); err != nil {
		pool.Close()
		return nil, err
	}
	// Feed PACKING verification (maintainer decision, 2026-07-26, SUPERSEDING the "packing stays instant"
	// rule): a feed PACKING session is completed only after a verifier approves the operator's ONE
	// mandatory packing video, so packing is a verification producer too. Same feed module as
	// distribution, but a DISTINCT category (feed_packing) and ref_type so the two feed gates never
	// cross-fire. Register the category and wire the enqueue seam.
	// BLIND PER-ITEM QUANTITY ENTRY on packing review (maintainer decision 2026-08-21): the item
	// carries the pen-session's feed item NAMES as entry boxes (MeasurementFields, enqueued by the
	// producer -- the planned quantities are deliberately hidden from the verifier), she types the
	// packed weight she can see for each, and the approve carries every reading. A verifier who
	// cannot see a usable video rejects -> rework, unchanged. The intended-vs-entered variance
	// surfaces only on the leadership feed analytics execution view.
	if err := verificationService.RegisterCategory(verificationcatalog.FeedPacking); err != nil {
		pool.Close()
		return nil, err
	}
	feedDirectionService.WithPackingVerificationEnqueuer(
		feeddirectionverificationbridge.NewPacking(verificationService))
	// THE APPROVE CARRIES THE NUMBERS: the readings land on feed_packing_verified_quantities
	// through the producer's own store, BEFORE the verdict is recorded, so a refusal stops the
	// whole approve rather than approving beside readings that never landed.
	if err := verificationService.RegisterMeasurementApplier(
		feeddirectiondomain.VerificationCategoryPacking,
		feeddirectionverificationbridge.NewPackingMeasurementApplier(feedDirectionRepo),
	); err != nil {
		pool.Close()
		return nil, err
	}
	if err := verificationService.RegisterCategory(verificationcatalog.FeedTransport); err != nil {
		pool.Close()
		return nil, err
	}
	feedDirectionService.WithTransportVerificationEnqueuer(feeddirectionverificationbridge.NewTransport(verificationService))
	// Feed WASTAGE verification (maintainer decision, 2026-08-18): a daily task on EXPERIMENT pens
	// only — one mandatory leftover-feed video per pen per feed day, reviewed under the same feed
	// module with its own category so the four feed gates never cross-fire.
	//
	// THE VERIFIER RECORDS THE MEASURED VALUE. Wastage is the second category (after weighing) to
	// declare a correctable/recordable measurement: the operator submits only a video, and the
	// number is born on the verifier's screen — she reads the leftover weight off the clip, records
	// it, and approves; an unreadable value is a rejection, never a guess. Every visible word lives
	// here because the backend owns labels (her phone and admin-web drawer render this same copy).
	if err := verificationService.RegisterCategory(verificationcatalog.FeedWastage); err != nil {
		pool.Close()
		return nil, err
	}
	feedWastageBridge := feeddirectionverificationbridge.NewWastage(verificationService)
	feedDirectionService.WithWastageVerificationEnqueuer(feedWastageBridge)
	// The measurement is served by its own small service and the SAME bridge's relabel seam, NOT by
	// feedDirectionService: it is the verifier's act on one completion and must not be able to
	// reach the generation/completion writes (same shape as the weighing weight correction).
	feedWastageMeasurements := feeddirectionapp.NewWastageMeasurementService(feedDirectionRepo, log).
		WithVerificationRelabeler(feedWastageBridge)
	feedDirectionHandler.WithWastageMeasurer(feedWastageMeasurements)
	// THE APPROVE CARRIES THE NUMBER (maintainer decision 2026-08-20), and for wastage it must:
	// RequiredForApprove above refuses an approve that carries no reading and finds none already
	// recorded, BEFORE the verdict -- rather than letting the verdict commit and having the
	// consumer's ErrWastageMeasurementRequired strand the item mid-apply.
	if err := verificationService.RegisterMeasurementApplier(
		feeddirectiondomain.VerificationCategoryWastage,
		feeddirectionverificationbridge.NewWastageMeasurementApplier(feedWastageMeasurements, feedDirectionRepo),
	); err != nil {
		pool.Close()
		return nil, err
	}
	// PC Care verification (maintainer decision 2026-08-21): a PC Care task is completed only
	// when a verifier approves the operators' whole video set, so pc_care is a verification
	// producer like feed and weighing. ONE module (pc_care) with FOUR categories — one per work
	// category — all sharing NavigationModule "pc_care" so the verifier gets ONE Verify tab and
	// the categories split as queue page filters (never one tab per category). All four share
	// ref_type pc_care_task; the verdict consumer filters on module+ref_type.
	for _, def := range verificationcatalog.PCCare() {
		if err := verificationService.RegisterCategory(def); err != nil {
			pool.Close()
			return nil, err
		}
	}
	pcCareService.WithVerificationEnqueuer(pccareverificationbridge.New(verificationService))
	// Pen visit video (maintainer decision 2026-09-12): the last clip of a pen's care chain,
	// ONE category in the Preventive Care verify tab, filed against the visit row.
	if err := verificationService.RegisterCategory(verificationcatalog.PenVisit); err != nil {
		pool.Close()
		return nil, err
	}
	// Routine checks (maintainer instruction 2026-09-16): ONE category under its own Routines
	// navigation module, filed against the task row.
	if err := verificationService.RegisterCategory(verificationcatalog.PenRoutine); err != nil {
		pool.Close()
		return nil, err
	}
	// Death evidence verification (maintainer decision 2026-07-28, docs/decisions/
	// birth-death-workflows.md): after admin approval, the death workflow's two mandatory videos
	// travel to Verify as
	// ONE generic verification item (category death_evidence, both proofs on the item), so tasks is a
	// verification producer just like shifting and feed. Register the category and wire the enqueue
	// seam into the tasks workflow service.
	if err := verificationService.RegisterCategory(verificationcatalog.DeathEvidence); err != nil {
		pool.Close()
		return nil, err
	}
	if err := verificationService.RegisterCategory(verificationcatalog.BirthEvidence); err != nil {
		pool.Close()
		return nil, err
	}
	// Birth/death follow-up workflow engine (tasks module): per-goat SOP work opened by
	// goat.created/goat.exited, listed by the mobile /counts/birth and /counts/death modules.
	tasksWorkflowService := tasksapp.NewService(tasksWorkflowRepo, log).
		WithVerificationEnqueuer(tasksverificationbridge.New(verificationService)).
		// SOP parity (2026-09-16): a step's proof kinds come from the proof register, so the
		// verifier item names what the store judged, never what the client claimed.
		WithProofKindResolver(tasksproofkinds.New(proofRepo)).
		WithCaptureReshootListener(countsapp.NewBirthCaptureReshootService(countsApprovalRepo,
			countsbridge.NewBirthCaptureVerificationEnqueuer(verificationService))).
		// SOP-driven reconcile (2026-09-13): the tasks engine runs the card's questionnaire and
		// reports the last step done to the counts service, which runs its ordinary completion.
		WithCompletionHook(tasksdomain.TemplateKeyReconcile, countsPenReconciliationService)
	countsPenReconciliationService.WithWorkflowEngine(countsbridge.NewReconcileWorkflowEngine(tasksWorkflowService))
	tasksWorkflowHandler := taskshttp.NewHandler(tasksWorkflowService, log)
	// Verifier video-review analytics (CEO integrity signal): shares the same pool/timeout as the
	// verdict/queue repository above but is a distinct bounded concern, see
	// verification/adapters/postgres/review_events.go.
	verificationReviewEventRepo := verificationpg.NewReviewEventRepository(pool, cfg.Postgres.QueryTimeout)
	verificationHandler := verificationhttp.NewHandler(verificationService, log).
		WithModuleDutyReader(workforceRepo).
		WithReviewEventRepository(verificationReviewEventRepo)
	// The verifier-only admin-web workspace composes its sidebar from the registry above, so this
	// must be wired AFTER every RegisterCategory call — a module registered later would otherwise
	// be missing from the verifier's evidence groups.
	adminUIService.WithVerificationModules(verificationadminuibridge.New(verificationService)).
		WithModuleDutyReader(workforceRepo).
		// PAGE-GRAIN ACCESS (maintainer decision 2026-08-27). The sidebar is narrowed to
		// the pages this person is ticked for on /people, which is what retired the
		// hand-coded procurement-director lens. A person with no stored rows is not
		// narrowed at all.
		WithPersonPageAccess(accessRepo)

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

	// Build read tool executors. feed_direction_today does not yet have a
	// direct DB reader in this tier, so the orchestrator's runtime fallback
	// retries the same question through the MCP Toolbox.
	readToolExecs := ceoreadtools.NewToolExecutors()

	// Wire in-process readers for operational domains. The reader functions
	// themselves live in ceoai_readers.go (unit-tested against fakes in
	// ceoai_readers_test.go) so this block is just registration.
	parkResolver := newLocationsParkResolver(locationsService)

	for _, exec := range readToolExecs {
		switch exec.Spec().Name {
		case "counts_breakdown":
			ceoreadtools.SetCountsDataReader(exec, buildCountsReader(herdRegisterService, parkResolver))
		case "vaccination_shed_summary":
			ceoreadtools.SetVaccinationDataReader(readToolExecs, buildVaccinationReader(vaccExecService))
		case "procurement_source_entry_loads":
			ceoreadtools.SetProcurementDataReader(exec, buildProcurementReader(procurementService))
		case "sales_overview":
			ceoreadtools.SetSalesDataReader(exec, buildSalesOverviewReader(salesService))
		case "feed_weight_band_summary":
			ceoreadtools.SetFeedWeightBandReader(exec, buildFeedWeightBandReader(growthDirectorService, parkResolver))
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
		Parks:     ceoai.NewParkDirectory(pool, cfg.Postgres.QueryTimeout),
		Traces:    ceoTraceStore,
		// Thread surface backing GET/POST /ceo-ai/conversations* and the leadership
		// starters probe GET /ceo-ai/starters (the launcher visibility gate).
		ConvStore: ceoai.NewConversationHTTPStore(pool, cfg.Postgres.QueryTimeout),
		Logger:    log,
	}
	if ceoSQL, err := sqlguard.NewExecutor(ctx, sqlguard.PoolConfigFromEnv()); err == nil {
		ceoOpts.SQL = ceoai.NewSQLFallback(ceoSQL)
	} else {
		log.Warn("ceoai: sql fallback unavailable", "err", err)
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
	vaccinationapp.NewGoatReinstatedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewProtocolPublishedHandler(vaccinationGeneration).Register(bus)
	vaccinationapp.NewVerificationHandler(vaccinationCompletion).WithClosureProjector(sopService).Register(bus)
	vaccinationapp.NewVaccinationCompletedHandler(vaccinationService, obligationRepo, vaccinationBooster).Register(bus)
	calendarapp.NewObligationMissedHandler(calendarService).Register(bus)
	// Shifting + feed verification appliers: the ONE shared registration (internal/eventwiring), also
	// called by cmd/outbox-relay and cmd/domain-event-consumer so the three buses cannot drift.
	// NOTE: this API in-process bus does NOT receive the async verdict events — the verification service
	// publishes verdicts only to the outbox, so these appliers actually fire in the durable-bus
	// consumers above. Registering here keeps parity through the same helper. Each handler filters
	// strictly on source.module + source.ref_type, so no cross-fire.
	eventwiring.RegisterVerificationAppliers(bus, feedDirectionRepo, countsApprovalRepo, countsRepo, countsRepo, weighingRepo, weighingVerificationBridge, pcCareRepo, healthRepo, penVisitsRepo, pcCareRepo, penRoutinesRepo, log)
	countsapp.NewPenReconciliationRaiser(countsRepo, log, nil).Register(bus)
	countsapp.NewPenReconciliationVerificationHandler(countsRepo, nil).Register(bus)
	// Birth/death workflow consumers: same single-registration pattern (internal/eventwiring), also
	// called by cmd/outbox-relay, cmd/domain-event-consumer, domainconsumer/wiring, and kernelstages.
	eventwiring.RegisterWorkflowConsumers(bus, tasksWorkflowService, log)
	// SOP capture card: the birth report's own proofs -> verifier, verdict -> approval row.
	eventwiring.RegisterCountsCaptureConsumers(bus, countsbridge.NewBirthCaptureVerificationEnqueuer(verificationService), countsApprovalRepo, tasksWorkflowService)
	// A failed sale releases its tagged animals back into the herd (2026-09-25).
	eventwiring.RegisterSaleReleaseConsumers(bus, pool, cfg.Postgres.QueryTimeout)
	healthapp.NewDeathLifecycleHandler(healthRepo).Register(bus)
	// Notification PUSH LAYER ONLY (docs/decisions/vaccination-notification-rules.md §4c): read-only
	// consumers of vaccination.verification.awaiting_review and vaccination.verify.rejected/accepted
	// events published by sopbridge. They resolve each completion to its obligation context, then
	// route pending/rework/close notifications to the correct park, verifier, and leadership audience.
	vaccineLabels := notificationbridge.NewVaccineLabelResolver(pool, log)
	// Push copy needs a human park name, not a bare UUID (confirmed maintainer defect: pushes are
	// too abstract to act on). locationNames is a tiny, dependency-free lookup owned entirely by
	// notificationbridge (see location_names.go) -- no other module's port changes. Declared here
	// (moved up from below VerificationEventConsumer's registration) because C-defect-B
	// (2026-08-04) found it was built but only ever chained onto VerificationNotifier, never onto
	// VerificationEventConsumer -- so every pending/rework/approved/closed push this bus produced
	// (the ones that actually enrich with park/shed/vaccine names) silently degraded to generic
	// copy. See the identical fix in internal/kernelstages/bus.go, the durable bus that is the
	// one actually delivering pushes in production/E2E.
	locationNames := notificationbridge.NewLocationNameResolver(pool)
	// WHO hears each leadership push is per-designation config (maintainer decision 2026-09-08):
	// every upward-routing consumer below resolves its audience through the stored override,
	// falling back to the catalog default. Pinned by notificationbridge's audience wiring test.
	// THE DASHBOARD IS A DEVICE (2026-09-18). The roster's device registry is Android-only by its
	// own CHECK constraint, so every push below reached phones and left Chrome -- where the CEOs
	// actually spend the day -- silent. This decorator returns a person's subscribed browsers
	// BESIDE their phones at the one seam every consumer already talks to, so no consumer, no
	// notification type and no copy changes. Additive and fail-open: a browser lookup that errors
	// logs and returns the phones untouched. See notificationbridge/browser_push_recipients.go for
	// what it deliberately does NOT cover (the position/module-duty paths).
	notifyRecipients := notificationbridge.WithBrowserRecipients(rosterService, browserPushService, log)
	leadershipAudience := notificationbridge.NewStoredAudience(notifyRecipients, notificationaudiencepg.NewRepository(pool, cfg.Postgres.QueryTimeout), log)
	notificationbridge.NewVerificationEventConsumer(notifyRecipients, calendarService, log).WithAudience(leadershipAudience).WithVaccineLabels(vaccineLabels).WithLocationNames(locationNames).Register(bus)
	notificationbridge.NewWeighingSubmissionEventConsumer(notifyRecipients, calendarService, log).WithAudience(leadershipAudience).Register(bus)
	// Weighing publish/verdict/close pushes. Registered next to the submission
	// consumer so no weighing state change is push-silent.
	notificationbridge.NewWeighingLifecycleEventConsumer(notifyRecipients, calendarService, log).WithAudience(leadershipAudience).Register(bus)
	// Leadership Tasks: raised and every status change, gated per designation (2026-09-08).
	notificationbridge.NewLeadershipTaskNotifyConsumer(notifyRecipients, calendarService, log).WithAudience(leadershipAudience).Register(bus)
	notificationbridge.NewLeadershipTaskActivityNotifyConsumer(notifyRecipients, calendarService, log).Register(bus)
	notificationbridge.NewLeaveRequestNotifyConsumer(notifyRecipients, calendarService, log).WithAudience(leadershipAudience).Register(bus)
	notificationbridge.NewAnimalPurchaseNotifyConsumer(notifyRecipients, calendarService, log).WithAudience(leadershipAudience).Register(bus)
	notificationbridge.NewVerificationNotifier(calendarService, notifyRecipients, calendarService, log).WithLocationNames(locationNames).Register(bus)
	sopService.
		WithSubmissionHook(sopbridge.NewVaccinationSubmissionBridge(vaccinationService).
			WithVerificationProducer(verificationService).
			WithObligationCompleter(obligationRepo)).
		WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, bus))
	sopHandler := sophttp.NewHandler(sopService, log)
	vaccinationHandler := vaccinationhttp.NewHandler(vaccinationService, vaccinationCompletion, log).
		WithManualCampaignGenerator(vaccinationGeneration).
		WithAnchorManager(vaccinationService)
	passportService := passportapp.NewService(vaccinationService, obligationRepo, obligationRepo)
	passportHandler := passporthttp.NewHandler(passportService, log)
	authPool, err := connectAuthPool(ctx, pgCfg, log)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := platformpg.RegisterNamedPoolMetrics(authPool, "auth"); err != nil && log != nil {
		log.Warn("postgres_auth_pool_metrics_registration_failed", slog.String("error", err.Error()))
	}
	// Acquire-wait logging for both pools: silent unless an acquire had to wait.
	pressureCtx, stopPressureLogs := context.WithCancel(context.Background())
	platformpg.LogPoolAcquirePressure(pressureCtx, pool, "main", log, 30*time.Second)
	platformpg.LogPoolAcquirePressure(pressureCtx, authPool, "auth", log, 30*time.Second)
	connWarmer.Register(apiConnWarmups()...)
	connWarmer.Start(pressureCtx, pool)
	closePools := func() {
		stopPressureLogs()
		authPool.Close()
		pool.Close()
	}
	authAuditHandler := newAuthSessionHandler(authPool, cfg.Postgres, verifier, authAuditOptions, log)
	// PER-PERSON ACCESS (maintainer decision 2026-08-24). From here a request's
	// permissions come from the person's own stored module rows; the route rules are
	// unchanged. A person with no rows yet still authorizes from their role, logged
	// each time -- see the middleware for why that bridge exists and when it goes.
	authz.SetPersonAccessSource(cachedPersonAccess{inner: accessRepo, cache: authReadCache})

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
	identityhttp.RegisterSaleAllocation(protectedMux, saleAllocationHandler)
	bulkstatushttp.Register(protectedMux, bulkStatusHandler)
	locationshttp.Register(protectedMux, locationsHandler)
	workforcehttp.Register(protectedMux, workforceHandler)
	workforcehttp.RegisterRoster(protectedMux, rosterHandler)
	workforcehttp.RegisterPeople(protectedMux, peopleHandler)
	workforcehttp.RegisterAccess(protectedMux, accessHandler)
	notificationaudiencehttp.Register(protectedMux, notificationAudienceHandler)
	browserpushhttp.Register(protectedMux, browserPushHandler)
	workforcehttp.RegisterClock(protectedMux, clockHandler)
	workforcehttp.RegisterLeave(protectedMux, leaveHandler)
	workforcehttp.RegisterTimetable(protectedMux, timetableHandler)
	proofhttp.Register(protectedMux, proofHandler)
	sophttp.Register(protectedMux, sopHandler)
	protocolhttp.Register(protectedMux, protocolHandler)
	outboxhttp.Register(protectedMux, outboxHandler)
	operationsaudithttp.Register(protectedMux, operationsAuditHandler)
	processintegrityhttp.Register(protectedMux, processIntegrityHandler)
	procurementhttp.Register(protectedMux, procurementHandler)
	procurementhttp.RegisterVendors(protectedMux, procurementVendorHandler)
	procurementhttp.RegisterFeedPurchases(protectedMux, procurementFeedPurchaseHandler)
	procurementhttp.RegisterLoadwise(protectedMux, procurementLoadwiseHandler)
	procurementhttp.RegisterBuyerAnalytics(protectedMux, procurementBuyerAnalyticsHandler)
	procurementhttp.RegisterFarmBornSales(protectedMux, procurementFarmBornHandler)
	animalpurchasehttp.Register(protectedMux, animalPurchaseHandler)
	toxinhttp.Register(protectedMux, toxinHandler)
	markethttp.Register(protectedMux, marketHandler)
	leadershiptaskshttp.Register(protectedMux, leadershipTasksHandler)
	notificationcentrehttp.Register(protectedMux, notificationCentreHandler)
	workboardhttp.Register(protectedMux, workBoardHandler)
	alertshttp.Register(protectedMux, alertsHandler)
	penvisitshttp.Register(protectedMux, penVisitsHandler)
	penroutineshttp.Register(protectedMux, penRoutinesHandler)
	penroutineshttp.RegisterAdmin(protectedMux, penRoutinesAdminHandler)
	configurationhttp.Register(protectedMux, configurationHandler)
	configurationhttp.RegisterBulk(protectedMux, configurationHandler)
	saleshttp.Register(protectedMux, salesHandler)
	vaccinationhttp.Register(protectedMux, vaccinationHandler)
	vaccexechttp.Register(protectedMux, vaccExecHandler)
	weighinghttp.Register(protectedMux, weighingHandler)
	growthdirectorhttp.Register(protectedMux, growthDirectorHandler)
	calendarhttp.Register(protectedMux, calendarHandler)
	adminuihttp.Register(protectedMux, adminUIHandler)
	appanalyticshttp.Register(protectedMux, appAnalyticsHandler)
	appconfighttp.Register(protectedMux, appConfigHandler)
	countshttp.Register(protectedMux, herdRegisterHandler)
	countshttp.RegisterAppWrites(protectedMux, countsAppWriteHandler)
	countshttp.RegisterApprovals(protectedMux, countsAppWriteHandler)
	countshttp.RegisterAdminWebApprovals(protectedMux, countsAppWriteHandler)
	countshttp.RegisterShiftingExecution(protectedMux, countsAppWriteHandler)
	countshttp.RegisterPenReconciliation(protectedMux, countsAppWriteHandler)
	taskshttp.Register(protectedMux, tasksWorkflowHandler)
	healthhttp.Register(protectedMux, healthHandler)
	healthhttp.RegisterConfig(protectedMux, healthConfigHandler)
	healthhttp.RegisterRegisterConfig(protectedMux, healthRegisterConfigHandler)
	healthhttp.RegisterDiagnosisTypes(protectedMux, healthDiagnosisTypeHandler)
	healthhttp.RegisterRegisterSheets(protectedMux, healthRegisterSheetHandler)
	herdsignalshttp.Register(protectedMux, herdSignalsHandler)
	healthhttp.RegisterDiagnosis(protectedMux, healthDiagnosisHandler)
	healthhttp.RegisterAnalytics(protectedMux, healthAnalyticsHandler)
	healthhttp.RegisterDeathCauses(protectedMux, healthDeathCauseHandler)
	feedhttp.Register(protectedMux, feedHandler)
	feedconfighttp.Register(protectedMux, feedConfigHandler)
	feeddirectionhttp.Register(protectedMux, feedDirectionHandler)
	pccarehttp.Register(protectedMux, pcCareHandler)
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
	if cfg.RouteMode != apiRouteModeEvents {
		authaudit.Register(mux, authAuditHandler)
		proofhttp.RegisterSigned(mux, proofHandler)
	}
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
	liveNotifyCtx, cancelLiveNotify := context.WithCancel(ctx)
	herdSignalsHandler.WithLiveNotifications(liveNotifyCtx, herdsignalspg.NewLiveNotificationSource(pool, log))
	// Persisted risk classification (000402): one pass per interval across the cluster (advisory
	// lock), stopped with the live-notify context on Close.
	go herdSignalsService.RunRiskClassifier(liveNotifyCtx, herdSignalsRiskClassifierInterval)
	// Cross-instance eviction feed on its own connection (not a pool slot), then a background,
	// serial warm-up of the landing read every Weights/ADG visit blocks on.
	// Warm-up waits for the first successful LISTEN (so the connect-time evict-all cannot wipe what
	// it loads), then starts after a per-instance jitter and runs its reads one at a time.
	analyticsListener := // The vaccination read cache rides the same feed: migration 000411's write triggers notify with
		// caches=["vaccination"], so a dose/drive/obligation write evicts only that cache.
		readcache.NewListener(pool, log, analyticsReadCache, alertsReadCache, countsReadCache, vaccExecRepo.ReadCache())
	analyticsListener.OnFirstConnect(func(ctx context.Context) {
		analyticsReadCache.StartWarmup(ctx, log, readcache.Jitter(time.Second, 10*time.Second), 20*time.Second,
			weighingRepo.WarmLandingReads,
			// Build/drain the FCR feed-day rollup so the first FCR read after a deploy is not the
			// one that pays for it.
			func(ctx context.Context) error { _, err := growthDirectorRepo.RefreshFCRRollup(ctx, ""); return err })
	})
	analyticsListener.Start(liveNotifyCtx)
	return &API{
		Server: server,
		Close: func() {
			cancelLiveNotify()
			closePools()
		},
	}, nil
}

// herdSignalsRiskClassifierInterval is how often the persisted Herd Signals risk_state is
// recomputed. Risk inputs are 15m/24h-window signals, so a minute of lag is well inside their
// own resolution.
const herdSignalsRiskClassifierInterval = time.Minute

func newEventsAPI(
	cfg Config,
	log *slog.Logger,
	pool *pgxpool.Pool,
	binaryMigrationVersion string,
	authz *httpmiddleware.AuthMiddleware,
	registerProtected func(*http.ServeMux),
) *API {
	protectedMux := http.NewServeMux()
	protectedMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	protectedMux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	protectedMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "postgres not ready", http.StatusServiceUnavailable)
			return
		}
		if dbVersion, err := migrationguard.AppliedVersion(r.Context(), pool); err == nil {
			if _, err := migrationguard.Check(dbVersion, binaryMigrationVersion); err != nil {
				http.Error(w, "migration drift: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
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
	registerProtected(protectedMux)

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
	mux.Handle("/", authz.Wrap(instrumentedProtectedMux))
	handler := httpmiddleware.PanicRecovery(log)(httpmiddleware.RequestContext(log)(mux))
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return &API{Server: server, Close: pool.Close}
}

func validateRouteMode(mode string) error {
	switch strings.TrimSpace(mode) {
	case "", apiRouteModeEvents:
		return nil
	default:
		return fmt.Errorf("unsupported GOATOS_API_ROUTE_MODE %q", mode)
	}
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

func buildAuthMiddleware(cfg AuthConfig, verifier, appCheckVerifier httpmiddleware.TokenVerifier, grants permissions.GrantSource, dynamicEmails authallow.DynamicEmailSource, log *slog.Logger) (*httpmiddleware.AuthMiddleware, error) {
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

		DevHeadersAllowed:    cfg.DevHeadersAllowed,
		Environment:          cfg.Environment,
		AllowedEmails:        cfg.AllowedEmails,
		DynamicAllowedEmails: dynamicEmails,
	}, verifier, grants, log)
}

// firebaseIdentityProjectID resolves the Firebase project the create-person
// flow mints login accounts in: an explicit GOATOS_FIREBASE_PROJECT_ID wins,
// else it is derived from a securetoken.google.com auth issuer. Empty means
// "no Firebase in this environment" (local HS256 dev) and the identity adapter
// is not constructed.
func firebaseIdentityProjectID(issuer string) string {
	if explicit := strings.TrimSpace(os.Getenv("GOATOS_FIREBASE_PROJECT_ID")); explicit != "" {
		return explicit
	}
	return firebaseidentity.ProjectIDFromIssuer(issuer)
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

// newWorkBoardService is the Work Board's source registry, shared by newAPI and the page
// round-trip integration test so the test measures exactly the production wiring.
func newWorkBoardService(pool *pgxpool.Pool, timeout time.Duration, processIntegrityRepo *processintegritypg.Repository) *workboardapp.Service {
	return workboardapp.NewService(newWorkBoardSources(pool, timeout, processIntegrityRepo)...).
		WithStatementBatch(workboardpg.NewStatementBatch(pool, timeout))
}

// newWorkBoardSources is every module's board source, in registration order.
func newWorkBoardSources(pool *pgxpool.Pool, timeout time.Duration, processIntegrityRepo *processintegritypg.Repository) []workboardports.Source {
	sources := []workboardports.Source{
		weighingboard.New(pool, timeout),
		feedboard.New(pool, timeout),
		verificationboard.New(pool, timeout),
		countsboard.NewApprovals(pool, timeout),
		countsboard.NewMilkFeeding(pool, timeout),
		// Milk preparation is its own task, the day before the feeding (maintainer, 2026-09-25).
		countsboard.NewMilkPreparation(pool, timeout),
		healthboard.New(pool, timeout),
		pccareboard.New(pool, timeout),
		// The next-day pen visit (maintainer decision 2026-09-14) rows on the day it is due
		// under TASKS -- a task of its own, as on the phone's "For me" tab -- whatever work
		// raised it.
		penvisitsboard.New(pool, timeout),
		// Pen routines (maintainer instruction 2026-09-16) row under TASKS on the day a check
		// is due, beside the pen visits.
		penroutinesboard.New(pool, timeout),
		// Vaccination reuses the process-integrity read behind the port; the member
		// resolver is what lets the operator lens narrow it by user id.
		piboard.New(processIntegrityRepo).
			WithMemberResolver(piboard.NewPoolMemberResolver(pool, timeout)).
			// The per-animal subtask drill is the source's own SQL and needs the pool.
			WithPool(pool, timeout),
		// Toxin (2026-09-25): every aflatoxin round a feed load owes, until it is signed off.
		toxinboard.New(pool, timeout),
	}
	// EVERY workflow the shared tasks engine runs -- herd operations, the procurement intakes,
	// the sale, general SOP runs, and any module that plugs into the engine later -- rows on the
	// board through these, one per lane (tasks/adapters/boardsource.engineModuleLanes). A new
	// SOP-driven module is on the board without a line here.
	for _, src := range tasksboard.Sources(pool, timeout) {
		sources = append(sources, src)
	}
	return sources
}

// apiConnWarmups is every statement the API warms on a new main-pool connection
// (platformpg.ConnWarmer), in priority order: the per-request auth reads first, then the heavy
// canonical reads whose first plan on a fresh backend costs the most.
func apiConnWarmups() []platformpg.ConnWarmup {
	var all []platformpg.ConnWarmup
	all = append(all, workforcepg.AccessConnWarmups()...)
	all = append(all, permissionspg.ConnWarmups()...)
	all = append(all, calendarpg.ConnWarmups()...)
	all = append(all, vaccexecpg.ConnWarmups()...)
	all = append(all, processintegritypg.ConnWarmups()...)
	all = append(all, weighingpg.ConnWarmups()...)
	all = append(all, feeddirectionpg.ConnWarmups()...)
	all = append(all, growthdirectorpg.ConnWarmups()...)
	return all
}
