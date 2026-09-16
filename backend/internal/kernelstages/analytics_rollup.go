package kernelstages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2/google"
)

// AnalyticsRollupStage dispatches the existing bounded job from the shared
// operational cadence. Daily completion is the job's final audit, not HTTP 200.
type AnalyticsRollupStage struct {
	pool   *pgxpool.Pool
	job    string
	now    func() time.Time
	client func(context.Context) (*http.Client, error)
}

func NewAnalyticsRollupStage(deps Deps) *AnalyticsRollupStage {
	return &AnalyticsRollupStage{pool: deps.Pool, job: getenv("GOATOS_ANALYTICS_ROLLUP_JOB"), now: time.Now, client: func(ctx context.Context) (*http.Client, error) {
		return google.DefaultClient(ctx, "https://www.googleapis.com/auth/cloud-platform")
	}}
}
func (s *AnalyticsRollupStage) Name() string { return "analytics-rollup-dispatch" }

var rollupJobName = regexp.MustCompile(`^projects/[a-z][a-z0-9-]+/locations/[a-z0-9-]+/jobs/[a-z][a-z0-9-]+$`)

func rollupDueDate(now time.Time) (string, bool) {
	ist := now.In(time.FixedZone("Asia/Kolkata", 19800))
	if ist.Hour() < 3 || ist.Hour() == 3 && ist.Minute() < 15 {
		return "", false
	}
	return ist.AddDate(0, 0, -1).Format("2006-01-02"), true
}
func (s *AnalyticsRollupStage) Run(ctx context.Context) error {
	if s.job == "" {
		return nil
	}
	if !rollupJobName.MatchString(s.job) {
		return fmt.Errorf("invalid analytics rollup job resource")
	}
	date, due := rollupDueDate(s.now())
	if !due {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var claimed bool
	// One atomic persisted lease across replicas/restarts. Failed API requests keep
	// the lease: an ambiguous accepted request must not cause an immediate duplicate.
	err := s.pool.QueryRow(ctx, `INSERT INTO analytics.rollup_dispatch (source_date,claimed_at,lease_until)
 SELECT $1::date,now(),now()+interval '40 minutes'
 ON CONFLICT(source_date) DO UPDATE SET lease_until=now()+interval '40 minutes'
 WHERE analytics.rollup_dispatch.lease_until < now()
 AND NOT EXISTS (SELECT 1 FROM analytics.rollup_run WHERE source_date=$1::date AND status='succeeded' AND started_at >= analytics.rollup_dispatch.claimed_at)
 RETURNING true`, date).Scan(&claimed)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("claim analytics dispatch: %w", err)
	}
	client, err := s.client(ctx)
	if err != nil {
		return err
	}
	// Reconcile all active executions before retrying a lease whose response may
	// have been lost. Do not start another job while a queued/running one exists.
	active, err := rollupExecutionActive(ctx, client, s.job)
	if err != nil {
		return err
	}
	if active {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"overrides": map[string]any{"containerOverrides": []any{map[string]any{"args": []string{"-timeout=25m", "-source=app_events", "-lookback-days=3", "-source-date=" + date}}}}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://run.googleapis.com/v2/"+s.job+":run", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("dispatch analytics rollup: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("dispatch analytics rollup HTTP %d", resp.StatusCode)
	}
	return nil
}
func rollupExecutionActive(ctx context.Context, client *http.Client, job string) (bool, error) {
	token := ""
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://run.googleapis.com/v2/"+job+"/executions?pageSize=100&pageToken="+url.QueryEscape(token), nil)
		resp, err := client.Do(req)
		if err != nil {
			return false, err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return false, fmt.Errorf("list analytics executions HTTP %d", resp.StatusCode)
		}
		var page struct {
			Executions []struct {
				CompletionTime string `json:"completionTime"`
			} `json:"executions"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&page)
		resp.Body.Close()
		if err != nil {
			return false, err
		}
		for _, execution := range page.Executions {
			if execution.CompletionTime == "" {
				return true, nil
			}
		}
		if page.NextPageToken == "" {
			return false, nil
		}
		token = page.NextPageToken
	}
	return false, fmt.Errorf("analytics execution listing exceeded bounded pages")
}
