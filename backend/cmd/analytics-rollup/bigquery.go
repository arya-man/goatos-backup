package main

import (
	"context"
	"fmt"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
)

// runAggregationQuery executes one BigQuery aggregation query with a
// MaxBytesBilled cap and returns the resulting RowIterator plus the actual
// bytes billed for the query (for the run's cost audit trail /
// kmetrics.RecordAnalyticsRollupRun).
//
// This is the ONLY way this job talks to BigQuery: every caller supplies a
// query that already does its own GROUP BY / percentile aggregation and is
// scoped to a single _TABLE_SUFFIX (or, for the Crashlytics table, a single
// day filter) - see funnel.go/journey.go/engagement.go/crash.go. Raw event
// rows are never pulled into Go; only the small aggregated result set is
// iterated here.
func runAggregationQuery(ctx context.Context, client *bigquery.Client, location string, maxBytesBilled int64, sql string, params ...bigquery.QueryParameter) (*bigquery.RowIterator, int64, error) {
	q := client.Query(sql)
	q.Location = location
	q.Parameters = params
	q.MaxBytesBilled = maxBytesBilled

	job, err := q.Run(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("run bigquery job: %w", err)
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("wait bigquery job: %w", err)
	}
	if err := status.Err(); err != nil {
		return nil, 0, fmt.Errorf("bigquery job failed (possibly MaxBytesBilled=%d exceeded - check the query has a single-partition filter): %w", maxBytesBilled, err)
	}

	var bytesBilled int64
	if stats, ok := status.Statistics.Details.(*bigquery.QueryStatistics); ok && stats != nil {
		bytesBilled = stats.TotalBytesBilled
	}

	it, err := job.Read(ctx)
	if err != nil {
		return nil, bytesBilled, fmt.Errorf("read bigquery results: %w", err)
	}
	return it, bytesBilled, nil
}

// isDone reports whether err is the BigQuery/iterator "no more rows" marker,
// matching this package's other callers' `for { ... if isDone(err) { break } }`
// loop shape without importing the iterator package everywhere.
func isDone(err error) bool {
	return err == iterator.Done
}
