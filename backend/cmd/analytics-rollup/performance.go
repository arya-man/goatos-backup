package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vgoats/goatos/backend/internal/platform/observability"
)

// Documented Firebase Performance export schema:
// https://firebase.google.com/docs/perf-mon/bigquery-export
// event_name is a categorized URL pattern. Duration includes the full client
// request until response completion, not just backend execution or TCP RTT.
const appNetworkSQL = `WITH samples AS (
 SELECT COALESCE(app_display_version, '') app_version, event_name route_pattern,
 CASE WHEN UPPER(network_info.request_http_method) IN ('GET','HEAD','POST','PUT','PATCH','DELETE','OPTIONS','CONNECT','TRACE')
 THEN UPPER(network_info.request_http_method) ELSE 'OTHER' END http_method,
 CASE WHEN network_info.response_code BETWEEN 100 AND 599
 THEN CONCAT(CAST(DIV(network_info.response_code,100) AS STRING),'xx') ELSE 'unknown' END response_class,
 network_info.response_completed_time_us / 1000.0 duration_ms
 FROM %s
 WHERE event_timestamp >= @start AND event_timestamp < @end
 AND event_type = 'NETWORK_REQUEST'
 AND event_name IS NOT NULL AND event_name != ''
 AND network_info.response_completed_time_us IS NOT NULL
 AND network_info.response_completed_time_us >= 0
)
SELECT app_version,route_pattern,http_method,response_class,COUNT(*) total_requests,
 APPROX_QUANTILES(duration_ms,100)[OFFSET(50)] p50_ms,
 APPROX_QUANTILES(duration_ms,100)[OFFSET(95)] p95_ms,
 APPROX_QUANTILES(duration_ms,100)[OFFSET(99)] p99_ms
FROM samples GROUP BY app_version,route_pattern,http_method,response_class`

type appNetworkRow struct {
	AppVersion    string  `bigquery:"app_version"`
	RoutePattern  string  `bigquery:"route_pattern"`
	HTTPMethod    string  `bigquery:"http_method"`
	ResponseClass string  `bigquery:"response_class"`
	TotalRequests int64   `bigquery:"total_requests"`
	P50MS         float64 `bigquery:"p50_ms"`
	P95MS         float64 `bigquery:"p95_ms"`
	P99MS         float64 `bigquery:"p99_ms"`
}

func validatePerformanceConfig(cfg config) error {
	if cfg.PerformanceTable == "" {
		return nil
	}
	if !qualifiedCrashTable.MatchString(cfg.PerformanceTable) {
		return fmt.Errorf("Performance export requires a fully-qualified project.dataset.table")
	}
	if strings.Split(cfg.PerformanceTable, ".")[0] != cfg.GA4BQProject {
		return fmt.Errorf("Performance export project must match configured BigQuery project")
	}
	if cfg.SourceAppID == "" {
		return fmt.Errorf("Performance source-app-id is required: exports are app-wide")
	}
	return nil
}

func runOptionalPerformance(ctx context.Context, pool *pgxpool.Pool, cfg config) (int64, int64, error) {
	if err := validatePerformanceConfig(cfg); err != nil {
		return 0, 0, err
	}
	if cfg.PerformanceTable == "" {
		observability.New(observability.Config{Service: "analytics-rollup"}).InfoContext(ctx, "performance_export_unavailable", "reason", "Firebase Performance export is not configured")
		return 0, 0, nil
	}
	client, err := bigquery.NewClient(ctx, cfg.GA4BQProject)
	if err != nil {
		return 0, 0, err
	}
	defer client.Close()
	sql := fmt.Sprintf(appNetworkSQL, "`"+cfg.PerformanceTable+"`")
	it, billed, err := runAggregationQuery(ctx, client, cfg.BQLocation, cfg.MaxBytesBilled, sql, bigquery.QueryParameter{Name: "start", Value: cfg.SourceDate}, bigquery.QueryParameter{Name: "end", Value: cfg.SourceDate.AddDate(0, 0, 1)})
	if err != nil {
		return 0, billed, err
	}
	var rows []appNetworkRow
	for {
		var row appNetworkRow
		err = it.Next(&row)
		if isDone(err) {
			break
		}
		if err != nil {
			return 0, billed, err
		}
		rows = append(rows, row)
	}
	written, err := replaceAppNetworkRows(ctx, pool, cfg, rows)
	return written, billed, err
}

func replaceAppNetworkRows(ctx context.Context, pool *pgxpool.Pool, cfg config, rows []appNetworkRow) (int64, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "app_network_daily:"+cfg.SourceAppID+":"+cfg.SourceDate.Format("2006-01-02")); err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM analytics.app_network_daily WHERE source_app_id=$1 AND event_date=$2`, cfg.SourceAppID, cfg.SourceDate); err != nil {
		return 0, err
	}
	batch := make([][]any, 0, len(rows))
	for _, r := range rows {
		if r.TotalRequests <= 0 || r.RoutePattern == "" || r.P50MS < 0 || r.P95MS < r.P50MS || r.P99MS < r.P95MS || math.IsNaN(r.P50MS) || math.IsNaN(r.P95MS) || math.IsNaN(r.P99MS) || math.IsInf(r.P99MS, 0) {
			return 0, fmt.Errorf("invalid Performance duration aggregate")
		}
		batch = append(batch, []any{cfg.SourceAppID, cfg.SourceDate, r.AppVersion, r.RoutePattern, r.HTTPMethod, r.ResponseClass, r.TotalRequests, r.P50MS, r.P95MS, r.P99MS})
	}
	if len(batch) > 0 {
		if _, err = tx.CopyFrom(ctx, pgx.Identifier{"analytics", "app_network_daily"}, []string{"source_app_id", "event_date", "app_version", "route_pattern", "http_method", "response_class", "total_requests", "p50_ms", "p95_ms", "p99_ms"}, pgx.CopyFromRows(batch)); err != nil {
			return 0, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}
