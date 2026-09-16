package main

import (
	"errors"
	"flag"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// defaultMaxBytesBilled caps a single BigQuery query at 2 GiB billed. GA4
// export tables for a single tenant/day partition are orders of magnitude
// smaller than this in practice; the cap exists so a query that accidentally
// loses its _TABLE_SUFFIX/partition filter (a full-table scan across every
// historical day) fails fast with a clear BigQuery "bytes billed exceeds
// limit" error instead of silently running up cost.
const defaultMaxBytesBilled = 2 * 1024 * 1024 * 1024 // 2 GiB

// defaultBQLocation matches the BigQuery dataset location pinned in
// infra/envs/stg/analytics_rollup.tf - GA4/rollup data must stay in
// asia-south1, never BigQuery's own "US" multi-region default.
const defaultBQLocation = "asia-south1"

// config resolves flags and environment for the default first-party app_events
// rollup and optional app-wide Firebase exports. Legacy GA4 requires --source=ga4.
type config struct {
	PerformanceTable string
	TenantID         string
	Source           string // app_events (default) or explicit legacy ga4 source
	LookbackDays     int    // bounded recent-day replacement ending on SourceDate

	// SourceDate is the single business-day partition this run
	// aggregates, in Asia/Kolkata business-calendar terms. Defaults to
	// "yesterday"; an explicit date supports repeatable late-arrival backfills.
	// The shared kernel worker pins this date when triggering the daily job;
	// recent-day lookback handles late ingestion without changing the cohort day.
	SourceDate time.Time

	Timeout time.Duration

	// GA4BQProject names the BigQuery project for optional Firebase exports and
	// legacy GA4. The historical field name is retained for CLI compatibility.
	// Defaults to GOOGLE_CLOUD_PROJECT; configured export tables must match it.
	GA4BQProject string

	// GA4Dataset is used only by explicit legacy --source=ga4. An absent dataset
	// skips that legacy run; the default app_events path has no GA4 dependency.
	GA4Dataset string

	// BQLocation is the BigQuery job location. Must match the dataset's
	// actual location or every query fails with a location mismatch.
	BQLocation string

	// MaxBytesBilled caps every BigQuery query this run issues.
	MaxBytesBilled int64

	// CrashlyticsTable and CrashlyticsSessionsTable are optional fully-qualified
	// Firebase tables. The default path writes app-wide analytics.app_crash_daily
	// only when both are configured; neither configured leaves data unavailable.
	// Legacy GA4 separately retains its analytics.crash_daily compatibility path.
	CrashlyticsTable         string
	CrashlyticsSessionsTable string
	SourceAppID              string

	// FunnelSteps is the legacy GA4 funnel definition. The default first-party
	// ordered flows are defined separately in appEventFlows using emitted events.
	FunnelSteps []FunnelStep
}

// FunnelStep is one step of one funnel rolled into analytics.funnel_daily.
type FunnelStep struct {
	FunnelKey string
	StepKey   string
	StepIndex int
	EventName string
}

// defaultFunnelKey is the single funnel named in
// OBSERVABILITY_DESIGN.md section 2.5/2.6.
const defaultFunnelKey = "goatos_mobile_core"

// defaultFunnelSteps is the design-doc funnel: login -> bootstrap ->
// drive-open -> scan -> vaccination-capture -> submit. The first three map
// to real AnalyticsEvents.kt constants; the last three are placeholder event
// names following the same snake_case convention pending Android
// instrumentation - see the FunnelSteps doc comment.
func defaultFunnelSteps() []FunnelStep {
	return []FunnelStep{
		{FunnelKey: defaultFunnelKey, StepKey: "login", StepIndex: 0, EventName: "login_success"},
		{FunnelKey: defaultFunnelKey, StepKey: "bootstrap", StepIndex: 1, EventName: "bootstrap_loaded"},
		{FunnelKey: defaultFunnelKey, StepKey: "drive_open", StepIndex: 2, EventName: "drive_open"},
		{FunnelKey: defaultFunnelKey, StepKey: "scan", StepIndex: 3, EventName: "scan_completed"},
		{FunnelKey: defaultFunnelKey, StepKey: "vaccination_capture", StepIndex: 4, EventName: "vaccination_capture_recorded"},
		{FunnelKey: defaultFunnelKey, StepKey: "submit", StepIndex: 5, EventName: "vaccination_capture_submitted"},
	}
}

func parseConfig(args []string) (config, error) {
	fs := flag.NewFlagSet("analytics-rollup", flag.ContinueOnError)
	lookback := fs.Int("lookback-days", 1, "refresh 1..7 business days ending on source-date (app_events only)")
	source := fs.String("source", firstNonEmptyEnv("GOATOS_ANALYTICS_SOURCE"), "app_events (default) or ga4")
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id (required; matches infra GOATOS_TENANT_ID)")
	sourceDateRaw := fs.String("source-date", getenv("GOATOS_ANALYTICS_SOURCE_DATE"), "YYYY-MM-DD business date to roll up; default yesterday (Asia/Kolkata)")
	timeout := fs.Duration("timeout", durationEnv("GOATOS_ANALYTICS_ROLLUP_TIMEOUT", 20*time.Minute), "job timeout")
	ga4Project := fs.String("ga4-bq-project", firstNonEmptyEnv("GOATOS_GA4_BQ_PROJECT", "GOOGLE_CLOUD_PROJECT"), "GCP project holding the GA4 BigQuery export dataset")
	ga4Dataset := fs.String("ga4-bq-dataset", firstNonEmptyEnv("GOATOS_GA4_EXPORT_DATASET", "GOATOS_GA4_BQ_DATASET"), "GA4 BigQuery export dataset id; empty means GA4 export is not linked yet")
	bqLocation := fs.String("bq-location", firstNonEmptyEnv("GOATOS_BQ_LOCATION"), "BigQuery job location")
	maxBytesBilled := fs.Int64("max-bytes-billed", int64Env("GOATOS_ANALYTICS_BQ_MAX_BYTES", defaultMaxBytesBilled), "BigQuery MaxBytesBilled cap per query")
	crashlyticsTable := fs.String("crashlytics-bq-table", getenv("GOATOS_CRASHLYTICS_BQ_TABLE"), "optional fully-qualified project.dataset.table for the Crashlytics BigQuery export; empty skips crash rollup")
	performanceTable := fs.String("performance-bq-table", getenv("GOATOS_PERFORMANCE_BQ_TABLE"), "optional Firebase Performance export project.dataset.table")
	crashSessions := fs.String("crashlytics-sessions-table", getenv("GOATOS_CRASHLYTICS_SESSIONS_TABLE"), "optional Firebase sessions export project.dataset.table")
	sourceAppID := fs.String("source-app-id", getenv("GOATOS_ANALYTICS_SOURCE_APP_ID"), "explicit Firebase app identity for app-wide crash statistics")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	if *source == "" {
		*source = "app_events"
	}
	if *source != "app_events" && *source != "ga4" {
		return config{}, errors.New("source must be app_events or ga4")
	}
	if *lookback < 1 || *lookback > 7 {
		return config{}, errors.New("lookback-days must be between 1 and 7")
	}
	if *source != "app_events" && *lookback != 1 {
		return config{}, errors.New("lookback-days requires app_events source")
	}
	if strings.TrimSpace(*tenantID) == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if *timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if *maxBytesBilled <= 0 {
		return config{}, errors.New("max-bytes-billed must be positive")
	}

	loc := strings.TrimSpace(*bqLocation)
	if loc == "" {
		loc = defaultBQLocation
	}

	now := time.Now().In(biztime.DefaultLocation())
	sourceDate := biztime.BusinessDayStart(now.AddDate(0, 0, -1))
	if raw := strings.TrimSpace(*sourceDateRaw); raw != "" {
		parsed, err := time.ParseInLocation("2006-01-02", raw, biztime.DefaultLocation())
		if err != nil {
			return config{}, errors.New("source-date must be YYYY-MM-DD")
		}
		sourceDate = parsed
	}

	return config{
		TenantID:                 strings.TrimSpace(*tenantID),
		Source:                   *source,
		LookbackDays:             *lookback,
		SourceDate:               sourceDate,
		Timeout:                  *timeout,
		GA4BQProject:             strings.TrimSpace(*ga4Project),
		GA4Dataset:               strings.TrimSpace(*ga4Dataset),
		BQLocation:               loc,
		MaxBytesBilled:           *maxBytesBilled,
		CrashlyticsTable:         strings.TrimSpace(*crashlyticsTable),
		CrashlyticsSessionsTable: strings.TrimSpace(*crashSessions),
		SourceAppID:              strings.TrimSpace(*sourceAppID),
		PerformanceTable:         strings.TrimSpace(*performanceTable),
		FunnelSteps:              defaultFunnelSteps(),
	}, nil
}

// eventDateSuffix formats SourceDate as the GA4 events_* table suffix
// (YYYYMMDD, no separators).
func (c config) eventDateSuffix() string {
	return c.SourceDate.Format("20060102")
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func firstNonEmptyEnv(keys ...string) string {
	for _, key := range keys {
		if value := getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func int64Env(key string, fallback int64) int64 {
	raw := getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
