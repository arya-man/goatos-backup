package kernelstages

import (
	"context"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type rollupTransport func(*http.Request) (*http.Response, error)

func (f rollupTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAnalyticsRollupBusinessDateBoundary(t *testing.T) {
	for _, tc := range []struct {
		now, date string
		due       bool
	}{{"2026-09-16T03:14:59+05:30", "", false}, {"2026-09-16T03:15:00+05:30", "2026-09-15", true}, {"2026-09-17T00:00:00+05:30", "", false}, {"2026-09-17T03:15:00+05:30", "2026-09-16", true}} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		date, due := rollupDueDate(now)
		if date != tc.date || due != tc.due {
			t.Fatalf("%s: %s %v", tc.now, date, due)
		}
	}
}
func TestAnalyticsRollupDurableRetryPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := pool.Exec(ctx, "TRUNCATE analytics.rollup_dispatch,analytics.rollup_run")
	if err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, "2026-09-16T04:00:00+05:30")
	posts := 0
	active := false
	fail := true
	client := &http.Client{Transport: rollupTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"executions":[]}`
		code := 200
		if r.Method == "POST" {
			posts++
			payload, _ := io.ReadAll(r.Body)
			date, _ := rollupDueDate(now)
			if !strings.Contains(string(payload), "-source-date="+date) || !strings.Contains(string(payload), "-lookback-days=3") {
				t.Error(string(payload))
			}
			if fail {
				code = 503
			}
		} else if active {
			body = `{"executions":[{"name":"running"}]}`
		}
		return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	s := &AnalyticsRollupStage{pool: pool, job: "projects/goatos-stg/locations/asia-south1/jobs/goatos-stg-analytics-rollup", now: func() time.Time { return now }, client: func(context.Context) (*http.Client, error) { return client, nil }}
	if err := s.Run(ctx); err == nil {
		t.Fatal("failed dispatch accepted")
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatal("duplicate during lease")
	}
	expire := func() {
		t.Helper()
		if _, err := pool.Exec(ctx, "UPDATE analytics.rollup_dispatch SET lease_until=now()-interval '1 second'"); err != nil {
			t.Fatal(err)
		}
	}
	expire()
	active = true
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatal("duplicate active execution after ambiguous failure")
	}
	expire()
	active = false
	fail = false
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != 2 {
		t.Fatal("failed execution not retried")
	}
	_, err = pool.Exec(ctx, `INSERT INTO analytics.rollup_run(source_date,started_at,finished_at,status) VALUES ('2026-09-15',now(),now(),'succeeded')`)
	if err != nil {
		t.Fatal(err)
	}
	expire()
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != 2 {
		t.Fatal("completed day rerun")
	}
	now = now.AddDate(0, 0, 1)
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != 3 {
		t.Fatal("next day not dispatched")
	}
}

func TestAnalyticsRollupActiveExecutionOnLaterPage(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: rollupTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"executions":[{"completionTime":"2026-09-16T00:00:00Z"}],"nextPageToken":"next token"}`
		if calls == 2 {
			if r.URL.Query().Get("pageToken") != "next token" {
				t.Fatal("lost page token")
			}
			body = `{"executions":[{"name":"queued"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	active, err := rollupExecutionActive(context.Background(), client, "projects/goatos-stg/locations/asia-south1/jobs/rollup")
	if err != nil || !active || calls != 2 {
		t.Fatalf("active=%v calls=%d err=%v", active, calls, err)
	}
}

// A job that exits non-zero every time must not be re-fired on every lease:
// backoff doubles per attempt and the date stops after rollupMaxAttemptsPerDay.
// A degraded run (first-party committed, optional export failed) completes the
// date, and a fresh 'running' audit row blocks stacking a second run.
func TestAnalyticsRollupDispatchBackoffAndDailyCapPostgres(t *testing.T) {
	pool := pgtest.StartPostgres(t, context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, "TRUNCATE analytics.rollup_dispatch,analytics.rollup_run"); err != nil {
		t.Fatal(err)
	}
	now, _ := time.Parse(time.RFC3339, "2026-09-16T04:00:00+05:30")
	posts := 0
	client := &http.Client{Transport: rollupTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			posts++
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"executions":[]}`)), Header: make(http.Header)}, nil
	})}
	s := &AnalyticsRollupStage{pool: pool, job: "projects/goatos-stg/locations/asia-south1/jobs/goatos-stg-analytics-rollup", now: func() time.Time { return now }, client: func(context.Context) (*http.Client, error) { return client, nil }}
	var gaps []float64
	for i := 0; i < 8; i++ {
		if err := s.Run(ctx); err != nil {
			t.Fatal(err)
		}
		var minutes float64
		if err := pool.QueryRow(ctx, `SELECT extract(epoch FROM lease_until-now())/60 FROM analytics.rollup_dispatch`).Scan(&minutes); err != nil {
			t.Fatal(err)
		}
		gaps = append(gaps, minutes)
		// Simulate the job failing and the lease expiring.
		if _, err := pool.Exec(ctx, `UPDATE analytics.rollup_dispatch SET lease_until=now()-interval '1 second'`); err != nil {
			t.Fatal(err)
		}
	}
	if posts != rollupMaxAttemptsPerDay {
		t.Fatalf("posts=%d; want capped at %d per day", posts, rollupMaxAttemptsPerDay)
	}
	for i, want := range []float64{40, 80, 160, 320} {
		if gaps[i] < want-1 || gaps[i] > want+1 {
			t.Fatalf("attempt %d backoff=%.1fm; want %.0fm (all=%v)", i+1, gaps[i], want, gaps)
		}
	}
	// Next day: an in-progress run blocks dispatch; a degraded run completes it.
	now = now.AddDate(0, 0, 1)
	if _, err := pool.Exec(ctx, `INSERT INTO analytics.rollup_run(source_date,started_at,status) VALUES ('2026-09-16',now(),'running')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != rollupMaxAttemptsPerDay {
		t.Fatal("dispatched while a run is in progress")
	}
	if _, err := pool.Exec(ctx, `UPDATE analytics.rollup_run SET status='degraded',finished_at=now()`); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != rollupMaxAttemptsPerDay+1 {
		t.Fatalf("posts=%d; next day not dispatched after run finished", posts)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO analytics.rollup_run(source_date,started_at,finished_at,status) VALUES ('2026-09-16',now(),now(),'degraded'); UPDATE analytics.rollup_dispatch SET lease_until=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posts != rollupMaxAttemptsPerDay+1 {
		t.Fatal("degraded (first-party committed) day was re-dispatched")
	}
}
