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

// config holds the resolved flag+env configuration for one analytics-rollup
// run. Field names/env sources are documented per-field because this job
// straddles two naming conventions: infra/envs/stg/analytics_rollup.tf
// (already deployed, source of truth for what the Cloud Run Job actually
// passes) and the task's originally-suggested env names - both are honored,
// infra's name wins when both are set. See the "env reconciliation" note in
// the handoff for the full list.
type config struct {
	TenantID string

	// SourceDate is the single GA4 event_date partition this run
	// aggregates, in Asia/Kolkata business-calendar terms. Defaults to
	// "yesterday" because a GA4 daily export for date D is typically not
	// stable/complete until sometime on D+1, and the Cloud Scheduler
	// trigger (03:15 IST) runs well after that export lands.
	SourceDate time.Time

	Timeout time.Duration

	// GA4BQProject is the GCP project holding the GA4 BigQuery export
	// dataset. Firebase always creates the export dataset in the same
	// project as the Firebase project itself, so this defaults to
	// GOOGLE_CLOUD_PROJECT (the project Cloud Run already sets) and only
	// needs an override if GA4 export ever lives in a different project.
	GA4BQProject string

	// GA4Dataset is the GA4 BigQuery export dataset id, e.g.
	// "analytics_123456789". Empty means "GA4 export not linked yet" -
	// the job must log and exit 0, never crash a scheduled run before the
	// manual Firebase-console linking step
	// (docs/observability/INFRA.md section 9) has happened.
	GA4Dataset string

	// BQLocation is the BigQuery job location. Must match the dataset's
	// actual location or every query fails with a location mismatch.
	BQLocation string

	// MaxBytesBilled caps every BigQuery query this run issues.
	MaxBytesBilled int64

	// CrashlyticsTable, if set, is a fully-qualified
	// `project.dataset.table` for the Crashlytics BigQuery export.
	// Crashlytics is optional per the task - an empty value skips the
	// crash rollup step entirely (analytics.crash_daily is simply not
	// written this run) without failing the job.
	CrashlyticsTable string

	// FunnelSteps is the ordered (step_key -> GA4 event_name) funnel
	// definition. Configurable via env because
	// apps/goatos-android/core/core-analytics/AnalyticsEvents.kt only has
	// login_attempt/login_success/bootstrap_loaded wired today - the
	// drive-open/scan/vaccination-capture/submit steps named in
	// OBSERVABILITY_DESIGN.md section 2.5 are not yet emitted by the app.
	// Missing event names simply produce zero rows for that step (GA4
	// GROUP BY on a nonexistent event_name is a harmless no-op), so
	// shipping the full intended funnel now is safe and needs no code
	// change once Android wires the remaining events - only an env
	// update if the eventual event names differ from the placeholders
	// below.
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
	tenantID := fs.String("tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id (required; matches infra GOATOS_TENANT_ID)")
	sourceDateRaw := fs.String("source-date", getenv("GOATOS_ANALYTICS_SOURCE_DATE"), "YYYY-MM-DD GA4 event_date partition to roll up; default yesterday (Asia/Kolkata)")
	timeout := fs.Duration("timeout", durationEnv("GOATOS_ANALYTICS_ROLLUP_TIMEOUT", 20*time.Minute), "job timeout")
	ga4Project := fs.String("ga4-bq-project", firstNonEmptyEnv("GOATOS_GA4_BQ_PROJECT", "GOOGLE_CLOUD_PROJECT"), "GCP project holding the GA4 BigQuery export dataset")
	ga4Dataset := fs.String("ga4-bq-dataset", firstNonEmptyEnv("GOATOS_GA4_EXPORT_DATASET", "GOATOS_GA4_BQ_DATASET"), "GA4 BigQuery export dataset id; empty means GA4 export is not linked yet")
	bqLocation := fs.String("bq-location", firstNonEmptyEnv("GOATOS_BQ_LOCATION"), "BigQuery job location")
	maxBytesBilled := fs.Int64("max-bytes-billed", int64Env("GOATOS_ANALYTICS_BQ_MAX_BYTES", defaultMaxBytesBilled), "BigQuery MaxBytesBilled cap per query")
	crashlyticsTable := fs.String("crashlytics-bq-table", getenv("GOATOS_CRASHLYTICS_BQ_TABLE"), "optional fully-qualified project.dataset.table for the Crashlytics BigQuery export; empty skips crash rollup")
	if err := fs.Parse(args); err != nil {
		return config{}, err
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
		TenantID:         strings.TrimSpace(*tenantID),
		SourceDate:       sourceDate,
		Timeout:          *timeout,
		GA4BQProject:     strings.TrimSpace(*ga4Project),
		GA4Dataset:       strings.TrimSpace(*ga4Dataset),
		BQLocation:       loc,
		MaxBytesBilled:   *maxBytesBilled,
		CrashlyticsTable: strings.TrimSpace(*crashlyticsTable),
		FunnelSteps:      defaultFunnelSteps(),
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
