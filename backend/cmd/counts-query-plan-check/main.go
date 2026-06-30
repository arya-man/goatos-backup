// Command counts-query-plan-check verifies indexed query-plan paths for the
// Counts/Shifting hot reads that Feed Direction depends on. It is a CSG10
// evidence worker: a passing run advances readiness evidence, but does not make
// G2 ready by itself because source parity, observability breadth, and seeded
// E2E remain separate closure proof.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultCheckLimit   = int32(100)
	dummyPlanSnapshotID = "00000000-0000-4000-8000-000000000999"
)

type config struct {
	TenantID   string
	ParkID     string
	ShedID     string
	BreedKey   string
	TargetDate time.Time
	AsOf       time.Time
	Timeout    time.Duration
}

type planCheck struct {
	Name                string
	Query               string
	Args                []any
	ExpectedIndexes     []string
	RequiredIndexGroups [][]string
	ProtectedTables     []string
}

type checkResult struct {
	Name          string
	Passed        bool
	Indexes       []string
	SeqScanTables []string
	Failure       string
}

type explainNode struct {
	NodeType     string        `json:"Node Type"`
	RelationName string        `json:"Relation Name"`
	IndexName    string        `json:"Index Name"`
	Plans        []explainNode `json:"Plans"`
}

type explainRoot struct {
	Plan explainNode `json:"Plan"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := parseFlags(args, time.Now)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	results, err := runChecks(ctx, pool, buildChecks(cfg))
	if err != nil {
		return err
	}
	status, blocker := readinessStatus(results)
	if err := upsertCSG10Readiness(ctx, pool, cfg.TenantID, status, blocker); err != nil {
		return err
	}
	for _, result := range results {
		fmt.Printf("counts query plan check=%s passed=%t indexes=%s seq_scan_tables=%s\n",
			result.Name, result.Passed, strings.Join(result.Indexes, ","), strings.Join(result.SeqScanTables, ","))
		if result.Failure != "" {
			fmt.Printf("counts query plan failure check=%s reason=%s\n", result.Name, result.Failure)
		}
	}
	if status == "blocked" {
		return errors.New(blocker)
	}
	fmt.Printf("counts query plan readiness status=%s evidence=counts-query-plan-check:%s\n",
		status, time.Now().UTC().Format(time.RFC3339))
	return nil
}

func parseFlags(args []string, now func() time.Time) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("counts-query-plan-check", flag.ContinueOnError)
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.ParkID, "park-id", getenv("GOATOS_COUNTS_PARK_ID"), "park/location id")
	fs.StringVar(&cfg.ShedID, "shed-id", getenv("GOATOS_COUNTS_SHED_ID"), "optional shed/location id for row-filter proof")
	fs.StringVar(&cfg.BreedKey, "breed-key", getenv("GOATOS_COUNTS_BREED_KEY"), "optional breed key for row-filter proof")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_QUERY_PLAN_TIMEOUT", 120*time.Second), "worker timeout")
	targetDateRaw := fs.String("target-date", getenv("GOATOS_COUNTS_TARGET_DATE"), "target date as YYYY-MM-DD or RFC3339; default tomorrow UTC")
	asOfRaw := fs.String("as-of", getenv("GOATOS_COUNTS_AS_OF"), "as-of instant as RFC3339; default now")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.ParkID = strings.TrimSpace(cfg.ParkID)
	cfg.ShedID = strings.TrimSpace(cfg.ShedID)
	cfg.BreedKey = strings.TrimSpace(cfg.BreedKey)
	if cfg.TenantID == "" {
		return config{}, errors.New("tenant-id is required")
	}
	if cfg.ParkID == "" {
		return config{}, errors.New("park-id is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	if now == nil {
		now = time.Now
	}
	cfg.AsOf = now().UTC()
	if strings.TrimSpace(*asOfRaw) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(*asOfRaw))
		if err != nil {
			return config{}, errors.New("as-of must be RFC3339")
		}
		cfg.AsOf = parsed.UTC()
	}
	if strings.TrimSpace(*targetDateRaw) == "" {
		cfg.TargetDate = dateOnly(cfg.AsOf.AddDate(0, 0, 1))
	} else {
		parsed, err := parseDateOrInstant(strings.TrimSpace(*targetDateRaw))
		if err != nil {
			return config{}, err
		}
		cfg.TargetDate = dateOnly(parsed)
	}
	return cfg, nil
}

func buildChecks(cfg config) []planCheck {
	targetStart := dateOnly(cfg.TargetDate)
	targetEnd := targetStart.AddDate(0, 0, 1)
	return []planCheck{
		{
			Name: "projection_anchors_latest",
			Query: `
SELECT DISTINCT ON (shed_id, lower(breed_key))
       base_count_anchor_id
FROM count_base_anchors
WHERE tenant_id = $1::uuid
  AND park_id = $2::uuid
  AND counted_at <= $3
  AND anchor_state = 'adopted'
ORDER BY shed_id, lower(breed_key), counted_at DESC, base_count_anchor_id DESC`,
			Args:            []any{cfg.TenantID, cfg.ParkID, cfg.AsOf},
			ExpectedIndexes: []string{"count_base_anchors_hot_idx"},
			ProtectedTables: []string{"count_base_anchors"},
		},
		{
			Name: "projection_movements_feed_target_date",
			Query: `
WITH destination_events AS MATERIALIZED (
  SELECT se.tenant_id, se.shifting_event_id, se.effective_at
  FROM shifting_events se
  WHERE se.tenant_id = $1::uuid
    AND se.destination_park_id = $2::uuid
    AND se.authorization_state = 'authorized'
    AND se.event_status IN ('authorized', 'applied')
    AND se.effective_at >= $3
    AND se.effective_at < $4
),
source_events AS MATERIALIZED (
  SELECT se.tenant_id, se.shifting_event_id, se.effective_at
  FROM shifting_events se
  WHERE se.tenant_id = $1::uuid
    AND se.source_park_id = $2::uuid
    AND se.source_shed_id IS NOT NULL
    AND se.destination_park_id <> $2::uuid
    AND se.authorization_state = 'authorized'
    AND se.event_status IN ('authorized', 'applied')
    AND se.effective_at >= $3
    AND se.effective_at < $4
),
projection_events AS (
  SELECT * FROM destination_events
  UNION ALL
  SELECT * FROM source_events
)
SELECT se.shifting_event_id, sei.shifting_event_impact_id
FROM projection_events se
JOIN LATERAL (
  SELECT shifting_event_impact_id, grain_key
  FROM shifting_event_impacts sei
  WHERE sei.tenant_id = se.tenant_id
    AND sei.shifting_event_id = se.shifting_event_id
  ORDER BY sei.grain_key
) sei ON true
ORDER BY se.effective_at, se.shifting_event_id, sei.grain_key`,
			Args:            []any{cfg.TenantID, cfg.ParkID, targetStart, targetEnd},
			ExpectedIndexes: []string{"shifting_events_destination_park_window_idx", "shifting_events_source_park_window_idx", "shifting_event_impacts_grain_unique", "shifting_event_impacts_event_breed_idx"},
			RequiredIndexGroups: [][]string{
				{"shifting_events_destination_park_window_idx"},
				{"shifting_events_source_park_window_idx"},
				{"shifting_event_impacts_grain_unique", "shifting_event_impacts_event_breed_idx"},
			},
			ProtectedTables: []string{"shifting_events", "shifting_event_impacts"},
		},
		{
			Name: "projection_snapshot_lookup",
			Query: `
SELECT count_projection_snapshot_id
FROM count_projection_snapshots
WHERE tenant_id = $1::uuid
  AND horizon = $2
  AND park_id = $3::uuid
  AND target_date = $4
ORDER BY created_at DESC, count_projection_snapshot_id DESC
LIMIT 1`,
			Args:            []any{cfg.TenantID, "feed_target_date", cfg.ParkID, targetStart},
			ExpectedIndexes: []string{"count_projection_snapshots_hot_idx", "count_projection_snapshots_source_unique"},
			ProtectedTables: []string{"count_projection_snapshots"},
		},
		{
			Name: "projection_rows_feed_hot",
			Query: `
SELECT count_projection_snapshot_row_id
FROM count_projection_snapshot_rows
WHERE tenant_id = $1::uuid
  AND count_projection_snapshot_id = $2::uuid
  AND park_id = $3::uuid
  AND target_date = $4
  AND (nullif($5::text, '')::uuid IS NULL OR shed_id = nullif($5::text, '')::uuid)
  AND (nullif($6::text, '') IS NULL OR lower(breed_key) = lower(nullif($6::text, '')))
  AND (nullif($7::text, '') IS NULL OR ration_context_resolution_state = nullif($7::text, ''))
  AND (nullif($8::text, '')::uuid IS NULL OR count_projection_snapshot_row_id > nullif($8::text, '')::uuid)
ORDER BY count_projection_snapshot_row_id
LIMIT $9`,
			Args:            []any{cfg.TenantID, dummyPlanSnapshotID, cfg.ParkID, targetStart, cfg.ShedID, cfg.BreedKey, "blocked", "", defaultCheckLimit},
			ExpectedIndexes: []string{"count_projection_snapshot_rows_grain_unique", "count_projection_snapshot_rows_feed_hot_idx", "count_projection_snapshot_rows_blocker_idx"},
			ProtectedTables: []string{"count_projection_snapshot_rows"},
		},
		{
			Name: "mismatch_scan_anchor_page",
			Query: `
SELECT base_count_anchor_id
FROM count_base_anchors
WHERE tenant_id = $1::uuid
  AND anchor_state = 'adopted'
  AND discrepancy_state <> 'resolved'
  AND counted_at <= $2
  AND (nullif($3::text, '')::uuid IS NULL OR park_id = nullif($3::text, '')::uuid)
  AND (nullif($4::text, '')::uuid IS NULL OR shed_id = nullif($4::text, '')::uuid)
  AND ($5::timestamptz IS NULL OR counted_at > $5::timestamptz)
  AND (
    $6::timestamptz IS NULL
    OR nullif($7::text, '')::uuid IS NULL
    OR (counted_at, base_count_anchor_id) > ($6::timestamptz, nullif($7::text, '')::uuid)
  )
ORDER BY counted_at, base_count_anchor_id
LIMIT $8`,
			Args:            []any{cfg.TenantID, cfg.AsOf, cfg.ParkID, cfg.ShedID, nil, nil, "", defaultCheckLimit},
			ExpectedIndexes: []string{"count_base_anchors_mismatch_scan_idx", "count_base_anchors_mismatch_scan_scope_idx"},
			ProtectedTables: []string{"count_base_anchors"},
		},
	}
}

func runChecks(ctx context.Context, pool interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}, checks []planCheck) ([]checkResult, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL jit = off"); err != nil {
		return nil, err
	}
	results := make([]checkResult, 0, len(checks))
	for _, check := range checks {
		raw, err := explainJSON(ctx, tx, check.Query, check.Args...)
		if err != nil {
			return nil, fmt.Errorf("explain %s: %w", check.Name, err)
		}
		results = append(results, analyzePlan(check, raw))
	}
	return results, tx.Commit(ctx)
}

func explainJSON(ctx context.Context, tx pgx.Tx, query string, args ...any) ([]byte, error) {
	var raw []byte
	if err := tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON, COSTS OFF) "+query, args...).Scan(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func analyzePlan(check planCheck, raw []byte) checkResult {
	result := checkResult{Name: check.Name}
	var roots []explainRoot
	if err := json.Unmarshal(raw, &roots); err != nil || len(roots) == 0 {
		result.Failure = "invalid EXPLAIN JSON"
		return result
	}
	indexes := map[string]bool{}
	seqTables := map[string]bool{}
	walkPlan(roots[0].Plan, indexes, seqTables)
	result.Indexes = sortedKeys(indexes)
	for _, table := range check.ProtectedTables {
		if seqTables[table] {
			result.SeqScanTables = append(result.SeqScanTables, table)
		}
	}
	missingRequiredGroups := missingIndexGroups(indexes, check.RequiredIndexGroups)
	missing := missingIndexes(indexes, check.ExpectedIndexes)
	switch {
	case len(result.SeqScanTables) > 0:
		result.Failure = "protected table used sequential scan: " + strings.Join(result.SeqScanTables, ",")
	case len(missingRequiredGroups) > 0:
		result.Failure = "missing expected index group: " + strings.Join(missingRequiredGroups, "; ")
	case len(missing) == len(check.ExpectedIndexes):
		result.Failure = "none of the expected indexes appeared: " + strings.Join(check.ExpectedIndexes, ",")
	default:
		result.Passed = true
	}
	return result
}

func walkPlan(node explainNode, indexes map[string]bool, seqTables map[string]bool) {
	if node.IndexName != "" {
		indexes[node.IndexName] = true
	}
	if node.NodeType == "Seq Scan" && node.RelationName != "" {
		seqTables[node.RelationName] = true
	}
	for _, child := range node.Plans {
		walkPlan(child, indexes, seqTables)
	}
}

func missingIndexes(found map[string]bool, expected []string) []string {
	missing := []string{}
	for _, index := range expected {
		if !found[index] {
			missing = append(missing, index)
		}
	}
	return missing
}

func missingIndexGroups(found map[string]bool, groups [][]string) []string {
	missing := []string{}
	for _, group := range groups {
		groupFound := false
		for _, index := range group {
			if found[index] {
				groupFound = true
				break
			}
		}
		if !groupFound {
			missing = append(missing, strings.Join(group, "|"))
		}
	}
	return missing
}

func readinessStatus(results []checkResult) (string, string) {
	failures := []string{}
	for _, result := range results {
		if !result.Passed {
			failures = append(failures, result.Name+": "+result.Failure)
		}
	}
	if len(failures) > 0 {
		return "blocked", "Counts query-plan check failed; " + strings.Join(failures, "; ")
	}
	return "pending", "Counts query-plan index-path checks passed; source parity, full observability, and seeded local E2E evidence remain before CSG10 can turn ready."
}

func upsertCSG10Readiness(ctx context.Context, pool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, tenantID, status, blocker string) error {
	_, err := pool.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG10', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/counts-query-plan-check;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
  now(), now()
)
ON CONFLICT (tenant_id, subgate_id) DO UPDATE
SET status = EXCLUDED.status,
    owner = EXCLUDED.owner,
    evidence_ref = EXCLUDED.evidence_ref,
    blocker_reason = EXCLUDED.blocker_reason,
    implementation_ref = EXCLUDED.implementation_ref,
    last_checked_at = EXCLUDED.last_checked_at,
    updated_at = now()`,
		tenantID, status, "counts-query-plan-check:"+time.Now().UTC().Format(time.RFC3339), blocker)
	if err != nil {
		return fmt.Errorf("counts: upsert CSG10 query-plan readiness: %w", err)
	}
	return nil
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func parseDateOrInstant(raw string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("target-date must be YYYY-MM-DD or RFC3339")
	}
	return t.UTC(), nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	raw := getenv(name)
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
