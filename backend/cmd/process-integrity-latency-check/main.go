// Command process-integrity-latency-check measures hot process-integrity reads
// against a real Postgres database and fails when p95 latency exceeds the gate.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/localtarget"
	processintegritypg "github.com/vgoats/goatos/backend/internal/processintegrity/adapters/postgres"
	processintegrityapp "github.com/vgoats/goatos/backend/internal/processintegrity/app"
	"github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

const defaultTenantID = "00000000-0000-4000-8000-000000000001"

type checkResult struct {
	Name        string  `json:"name"`
	Iterations  int     `json:"iterations"`
	Failures    int     `json:"failures"`
	FirstError  *string `json:"first_error,omitempty"`
	P50MS       float64 `json:"p50_ms"`
	P90MS       float64 `json:"p90_ms"`
	P95MS       float64 `json:"p95_ms"`
	P99MS       float64 `json:"p99_ms"`
	MaxMS       float64 `json:"max_ms"`
	ThresholdMS float64 `json:"threshold_ms"`
	Passed      bool    `json:"passed"`
}

type report struct {
	TenantID   string            `json:"tenant_id"`
	AsOf       string            `json:"as_of"`
	Projection *projectionResult `json:"projection,omitempty"`
	Checks     []checkResult     `json:"checks"`
}

type projectionResult struct {
	Required               bool   `json:"required"`
	Recomputed             bool   `json:"recomputed"`
	StalePathMeasured      bool   `json:"stale_path_measured"`
	Rows                   int64  `json:"rows"`
	ProjectionVersion      int64  `json:"projection_version"`
	ServingProjectionState string `json:"serving_projection_state,omitempty"`
	ProjectedAt            string `json:"projected_at,omitempty"`
}

type projectionStateSnapshot struct {
	ProjectionVersion        int64
	ServingProjectionVersion pgtype.Int8
	ProjectedAt              time.Time
	AsOf                     time.Time
	RowCount                 int64
	FreshnessStatus          string
	ServingState             string
	LastError                pgtype.Text
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("process-integrity-latency-check", flag.ExitOnError)
	databaseURL := fs.String("database-url", getenv("DATABASE_URL"), "Postgres DATABASE_URL")
	tenantID := fs.String("tenant-id", getenvDefault("GOATOS_TENANT_ID", defaultTenantID), "tenant UUID")
	category := fs.String("category", getenvDefault("GOATOS_PROCESS_INTEGRITY_CATEGORY", domain.CategoryVaccination), "category filter: vaccination, feed_direction, or blank")
	iterations := fs.Int("iterations", intEnv("GOATOS_PROCESS_INTEGRITY_LATENCY_ITERATIONS", 10), "measured iterations per check")
	warmup := fs.Int("warmup", intEnv("GOATOS_PROCESS_INTEGRITY_LATENCY_WARMUP", 1), "warmup iterations per check")
	limit := fs.Int("limit", intEnv("GOATOS_PROCESS_INTEGRITY_LATENCY_LIMIT", 50), "row limit")
	queryTimeout := fs.Duration("query-timeout", durationEnv("GOATOS_PROCESS_INTEGRITY_QUERY_TIMEOUT", 15*time.Second), "per repository query timeout")
	asOfRaw := fs.String("as-of", getenv("GOATOS_PROCESS_INTEGRITY_AS_OF"), "RFC3339 as-of; default now")
	dueBeforeRaw := fs.String("due-before", getenv("GOATOS_PROCESS_INTEGRITY_DUE_BEFORE"), "RFC3339 due-before; default service horizon")
	recomputeProjection := fs.Bool("recompute-projection", boolEnv("GOATOS_PROCESS_INTEGRITY_RECOMPUTE_PROJECTION", true), "rebuild process-integrity projection before measuring")
	requireProjection := fs.Bool("require-projection", boolEnv("GOATOS_PROCESS_INTEGRITY_REQUIRE_PROJECTION", true), "fail unless a fresh projection serves this as-of")
	measureStaleProjection := fs.Bool("measure-stale-projection", boolEnv("GOATOS_PROCESS_INTEGRITY_MEASURE_STALE_PROJECTION", true), "temporarily mark the projection stale and require the hot path to stay projected/fast")
	maxActionCenterP95 := fs.Duration("max-action-center-p95", durationEnv("GOATOS_PROCESS_INTEGRITY_MAX_ACTION_CENTER_P95", 1200*time.Millisecond), "Action Center repository p95 threshold")
	maxControlTowerP95 := fs.Duration("max-control-tower-p95", durationEnv("GOATOS_PROCESS_INTEGRITY_MAX_CONTROL_TOWER_P95", 1200*time.Millisecond), "Control Tower service p95 threshold")
	maxProtocolAdherenceP95 := fs.Duration("max-protocol-adherence-p95", durationEnv("GOATOS_PROCESS_INTEGRITY_MAX_PROTOCOL_ADHERENCE_P95", 1200*time.Millisecond), "Protocol Adherence service p95 threshold")
	maxCountP95 := fs.Duration("max-count-p95", durationEnv("GOATOS_PROCESS_INTEGRITY_MAX_COUNT_P95", 250*time.Millisecond), "counts-only repository p95 threshold")
	failOnThreshold := fs.Bool("fail-on-threshold", boolEnv("GOATOS_PROCESS_INTEGRITY_FAIL_ON_THRESHOLD", true), "exit nonzero on threshold failure")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if strings.TrimSpace(*databaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if err := validateDatabaseTarget(*databaseURL); err != nil {
		return err
	}
	if *iterations <= 0 {
		return fmt.Errorf("iterations must be > 0")
	}
	if *warmup < 0 {
		return fmt.Errorf("warmup must be >= 0")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, *databaseURL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	asOf := time.Now()
	if strings.TrimSpace(*asOfRaw) != "" {
		asOf, err = time.Parse(time.RFC3339, strings.TrimSpace(*asOfRaw))
		if err != nil {
			return fmt.Errorf("parse as-of: %w", err)
		}
	}
	q := domain.Query{TenantID: *tenantID, AsOf: asOf, Limit: *limit}
	if strings.TrimSpace(*category) != "" {
		c := strings.TrimSpace(*category)
		q.Category = &c
	}
	if strings.TrimSpace(*dueBeforeRaw) != "" {
		q.DueBefore, err = time.Parse(time.RFC3339, strings.TrimSpace(*dueBeforeRaw))
		if err != nil {
			return fmt.Errorf("parse due-before: %w", err)
		}
	}

	repo := processintegritypg.NewRepository(pool, *queryTimeout)
	projection := &projectionResult{Required: *requireProjection, Recomputed: *recomputeProjection}
	if *recomputeProjection {
		res, err := repo.RecomputeProjection(ctx, domain.ProjectionRecomputeRequest{TenantID: *tenantID, AsOf: asOf})
		if err != nil {
			return fmt.Errorf("recompute projection: %w", err)
		}
		projection.Rows = res.Rows
		projection.ProjectionVersion = res.ProjectionVersion
		projection.ProjectedAt = res.ProjectedAt.UTC().Format(time.RFC3339)
	}
	if *requireProjection {
		if err := requireFreshProjection(ctx, pool, *tenantID, asOf); err != nil {
			return err
		}
	}
	if state, err := projectionServingState(ctx, pool, *tenantID); err == nil {
		projection.ServingProjectionState = state
	}
	svc := processintegrityapp.NewService(repo).WithClock(func() time.Time { return asOf })
	controlTowerQ := q
	countQ := q
	countQ.OnlyBrokenOrAtRisk = true
	countQ.IncludeCompleted = false

	checks := []checkResult{
		measure("action_center_repository", *iterations, *warmup, *maxActionCenterP95, func(ctx context.Context) error {
			_, err := repo.ListRows(ctx, q)
			return err
		}),
		measure("control_tower_service", *iterations, *warmup, *maxControlTowerP95, func(ctx context.Context) error {
			_, err := svc.ControlTower(ctx, controlTowerQ)
			return err
		}),
		measure("protocol_adherence_service", *iterations, *warmup, *maxProtocolAdherenceP95, func(ctx context.Context) error {
			_, err := svc.ProtocolAdherence(ctx, q)
			return err
		}),
		measure("control_tower_counts_only", *iterations, *warmup, *maxCountP95, func(ctx context.Context) error {
			_, err := repo.CountByWorkState(ctx, countQ)
			return err
		}),
	}
	if *measureStaleProjection {
		restore, err := markProjectionStaleForMeasurement(ctx, pool, *tenantID, asOf.Add(-10*time.Minute))
		if err != nil {
			return err
		}
		projection.StalePathMeasured = true
		staleChecks := []checkResult{
			measure("stale_action_center_repository", *iterations, *warmup, *maxActionCenterP95, func(ctx context.Context) error {
				_, err := repo.ListRows(ctx, q)
				return err
			}),
			measure("stale_control_tower_counts_only", *iterations, *warmup, *maxCountP95, func(ctx context.Context) error {
				_, err := repo.CountByWorkState(ctx, countQ)
				return err
			}),
		}
		checks = append(checks, staleChecks...)
		if err := restore(ctx); err != nil {
			return err
		}
	}

	rep := report{TenantID: *tenantID, AsOf: asOf.UTC().Format(time.RFC3339), Projection: projection, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return err
	}

	if *failOnThreshold {
		for _, check := range checks {
			if !check.Passed {
				return fmt.Errorf("%s p95 %.1fms exceeded threshold %.1fms", check.Name, check.P95MS, check.ThresholdMS)
			}
		}
	}
	return nil
}

func validateDatabaseTarget(databaseURL string) error {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("GOATOS_ENV")))
	if env == "stg" {
		return localtarget.ValidateStagingCloudSQLDatabaseTarget("process-integrity-latency-check", env, databaseURL)
	}
	return localtarget.ValidateLocalDatabaseTarget("process-integrity-latency-check", env, databaseURL, "local", "dev")
}

func requireFreshProjection(ctx context.Context, pool *pgxpool.Pool, tenantID string, asOf time.Time) error {
	var projectedAsOf time.Time
	err := pool.QueryRow(ctx, `
SELECT as_of
FROM process_integrity_projection_state
WHERE tenant_id = $1::uuid
  AND serving_state = 'fresh'`, tenantID).Scan(&projectedAsOf)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("process-integrity projection is required but has no fresh state for tenant %s", tenantID)
	}
	if err != nil {
		return fmt.Errorf("read process-integrity projection state: %w", err)
	}
	skew := asOf.Sub(projectedAsOf)
	if skew < -5*time.Minute || skew > 5*time.Minute {
		return fmt.Errorf("process-integrity projection is stale for as_of %s; projected as_of %s", asOf.Format(time.RFC3339), projectedAsOf.Format(time.RFC3339))
	}
	return nil
}

func projectionServingState(ctx context.Context, pool *pgxpool.Pool, tenantID string) (string, error) {
	var servingState string
	var servingVersion pgtype.Int8
	err := pool.QueryRow(ctx, `
SELECT serving_state, serving_projection_version
FROM process_integrity_projection_state
WHERE tenant_id = $1::uuid`, tenantID).Scan(&servingState, &servingVersion)
	if err != nil {
		return "", err
	}
	if servingVersion.Valid {
		return fmt.Sprintf("%s:%d", servingState, servingVersion.Int64), nil
	}
	return servingState + ":none", nil
}

func markProjectionStaleForMeasurement(ctx context.Context, pool *pgxpool.Pool, tenantID string, staleAsOf time.Time) (func(context.Context) error, error) {
	var snap projectionStateSnapshot
	err := pool.QueryRow(ctx, `
SELECT projection_version, serving_projection_version, projected_at, as_of, row_count,
       freshness_status, serving_state, last_error
FROM process_integrity_projection_state
WHERE tenant_id = $1::uuid`, tenantID).Scan(
		&snap.ProjectionVersion,
		&snap.ServingProjectionVersion,
		&snap.ProjectedAt,
		&snap.AsOf,
		&snap.RowCount,
		&snap.FreshnessStatus,
		&snap.ServingState,
		&snap.LastError,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("cannot measure stale projection path: no projection state for tenant %s", tenantID)
	}
	if err != nil {
		return nil, fmt.Errorf("read projection state before stale measurement: %w", err)
	}
	if !snap.ServingProjectionVersion.Valid {
		return nil, fmt.Errorf("cannot measure stale projection path: no serving projection version for tenant %s", tenantID)
	}
	if _, err := pool.Exec(ctx, `
UPDATE process_integrity_projection_state
SET serving_state = 'stale',
    freshness_status = 'yellow',
    as_of = $2::timestamptz,
    updated_at = now()
WHERE tenant_id = $1::uuid`, tenantID, staleAsOf); err != nil {
		return nil, fmt.Errorf("mark projection stale for measurement: %w", err)
	}
	return func(restoreCtx context.Context) error {
		_, err := pool.Exec(restoreCtx, `
UPDATE process_integrity_projection_state
SET projection_version = $2::bigint,
    serving_projection_version = $3::bigint,
    projected_at = $4::timestamptz,
    as_of = $5::timestamptz,
    row_count = $6::bigint,
    freshness_status = $7::text,
    serving_state = $8::text,
    last_error = $9::text,
    updated_at = now()
WHERE tenant_id = $1::uuid`,
			tenantID,
			snap.ProjectionVersion,
			nullableInt8(snap.ServingProjectionVersion),
			snap.ProjectedAt,
			snap.AsOf,
			snap.RowCount,
			snap.FreshnessStatus,
			snap.ServingState,
			nullableText(snap.LastError),
		)
		if err != nil {
			return fmt.Errorf("restore projection state after stale measurement: %w", err)
		}
		return nil
	}, nil
}

func nullableInt8(v pgtype.Int8) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

func nullableText(v pgtype.Text) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

func measure(name string, iterations, warmup int, threshold time.Duration, fn func(context.Context) error) checkResult {
	for i := 0; i < warmup; i++ {
		_ = fn(context.Background())
	}
	samples := make([]float64, 0, iterations)
	failures := 0
	var firstErr *string
	for i := 0; i < iterations; i++ {
		start := time.Now()
		if err := fn(context.Background()); err != nil {
			failures++
			if firstErr == nil {
				msg := err.Error()
				firstErr = &msg
			}
		} else {
			samples = append(samples, float64(time.Since(start))/float64(time.Millisecond))
		}
	}
	sort.Float64s(samples)
	thresholdMS := float64(threshold) / float64(time.Millisecond)
	p95 := percentile(samples, 95)
	return checkResult{
		Name:        name,
		Iterations:  iterations,
		Failures:    failures,
		FirstError:  firstErr,
		P50MS:       percentile(samples, 50),
		P90MS:       percentile(samples, 90),
		P95MS:       p95,
		P99MS:       percentile(samples, 99),
		MaxMS:       percentile(samples, 100),
		ThresholdMS: thresholdMS,
		Passed:      failures == 0 && p95 <= thresholdMS,
	}
}

func percentile(sorted []float64, pct int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if pct <= 0 {
		return sorted[0]
	}
	if pct >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := (pct*len(sorted) + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func getenvDefault(key, def string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return def
}

func intEnv(key string, def int) int {
	raw := getenv(key)
	if raw == "" {
		return def
	}
	var value int
	if _, err := fmt.Sscanf(raw, "%d", &value); err != nil || value <= 0 {
		return def
	}
	return value
}

func boolEnv(key string, def bool) bool {
	switch strings.ToLower(getenv(key)) {
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	default:
		return def
	}
}

func durationEnv(key string, def time.Duration) time.Duration {
	raw := getenv(key)
	if raw == "" {
		return def
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return def
	}
	return value
}
