// Command counts-source-parity-check compares sanitized source parity fixtures
// against canonical Counts/Shifting projections. It is CSG10 evidence only:
// passing fixtures move readiness to pending, never ready, because broader
// source parity, observability, and seeded E2E remain separate closure gates.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	horizonCountAsOf      = "count_as_of"
	horizonFeedTargetDate = "feed_target_date"
	modeSample            = "sample"
	modeExact             = "exact"
	defaultLimit          = int32(1000)
	maxLimit              = int32(5000)
)

type config struct {
	File      string
	TenantID  string
	SourceRef string
	Limit     int32
	DryRun    bool
	Timeout   time.Duration
}

type parityFile struct {
	TenantID      string              `json:"tenant_id"`
	SourceRef     string              `json:"source_ref"`
	Horizon       string              `json:"horizon"`
	ParkID        string              `json:"park_id"`
	TargetDate    string              `json:"target_date"`
	AsOf          string              `json:"as_of"`
	CoverageMode  string              `json:"coverage_mode"`
	ExpectedRows  []expectedParityRow `json:"expected_rows"`
	targetDate    time.Time
	asOf          time.Time
	effectiveMode string
}

type expectedParityRow struct {
	ShedID                       string  `json:"shed_id"`
	BreedKey                     string  `json:"breed_key"`
	StageTag                     *string `json:"stage_tag"`
	AgeClass                     *string `json:"age_class"`
	Sex                          *string `json:"sex"`
	HeadCount                    int32   `json:"head_count"`
	PregnantCount                int32   `json:"pregnant_count"`
	LactatingCount               int32   `json:"lactating_count"`
	WarmupCount                  int32   `json:"warmup_count"`
	RationContextResolutionState *string `json:"ration_context_resolution_state"`
}

type parityResult struct {
	Expected   int
	Actual     int
	Missing    []string
	Mismatched []string
	Unexpected []string
	Truncated  bool
}

type projectionReader interface {
	CountAsOf(context.Context, countsdomain.CountProjectionRequest) (countsdomain.CountProjection, error)
	ProjectedCountFor(context.Context, countsdomain.CountProjectionRequest) (countsdomain.CountProjection, error)
}

type readinessWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	reader, closeFn, err := inputReader(cfg.File)
	if err != nil {
		return err
	}
	defer closeFn()

	fixture, err := parseParityFile(reader, cfg, time.Now)
	if err != nil {
		return err
	}
	if cfg.DryRun {
		fmt.Fprintf(stdout, "counts source parity dry-run ok source_ref=%s expected_rows=%d horizon=%s mode=%s\n",
			fixture.SourceRef, len(fixture.ExpectedRows), fixture.Horizon, fixture.effectiveMode)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	repo := countspg.NewRepository(pool, pgCfg.QueryTimeout)
	result, err := checkSourceParity(ctx, repo, fixture, cfg.Limit)
	if err != nil {
		return err
	}
	status, blocker := parityStatus(fixture.SourceRef, result)
	if err := upsertCSG10Readiness(ctx, pool, fixture.TenantID, fixture.SourceRef, status, blocker); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "counts source parity status=%s source_ref=%s expected=%d actual=%d missing=%d mismatched=%d unexpected=%d truncated=%t\n",
		status, fixture.SourceRef, result.Expected, result.Actual, len(result.Missing),
		len(result.Mismatched), len(result.Unexpected), result.Truncated)
	if status == "blocked" {
		return errors.New(blocker)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("counts-source-parity-check", flag.ContinueOnError)
	fs.StringVar(&cfg.File, "file", getenv("GOATOS_COUNTS_SOURCE_PARITY_FILE"), "sanitized source parity JSON fixture")
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id override")
	fs.StringVar(&cfg.SourceRef, "source-ref", getenv("GOATOS_COUNTS_SOURCE_PARITY_SOURCE_REF"), "source finding or fixture reference override")
	fs.BoolVar(&cfg.DryRun, "dry-run", boolEnv("GOATOS_COUNTS_SOURCE_PARITY_DRY_RUN"), "validate fixture without DB reads or readiness writes")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_SOURCE_PARITY_TIMEOUT", 120*time.Second), "worker timeout")
	limit := fs.Int("limit", intEnv("GOATOS_COUNTS_SOURCE_PARITY_LIMIT", int(defaultLimit)), "bounded projection read limit")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.File = strings.TrimSpace(cfg.File)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.SourceRef = strings.TrimSpace(cfg.SourceRef)
	if cfg.File == "" {
		return config{}, errors.New("file is required")
	}
	if *limit <= 0 || *limit > int(maxLimit) {
		return config{}, fmt.Errorf("limit must be between 1 and %d", maxLimit)
	}
	cfg.Limit = int32(*limit)
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}

func parseParityFile(r io.Reader, cfg config, now func() time.Time) (parityFile, error) {
	var file parityFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return parityFile{}, err
	}
	file.TenantID = defaultString(strings.TrimSpace(cfg.TenantID), strings.TrimSpace(file.TenantID))
	file.SourceRef = defaultString(strings.TrimSpace(cfg.SourceRef), strings.TrimSpace(file.SourceRef))
	file.Horizon = strings.TrimSpace(file.Horizon)
	file.ParkID = strings.TrimSpace(file.ParkID)
	file.effectiveMode = defaultString(strings.TrimSpace(file.CoverageMode), modeSample)
	if file.TenantID == "" {
		return parityFile{}, errors.New("tenant_id is required in flags or fixture")
	}
	if file.SourceRef == "" {
		return parityFile{}, errors.New("source_ref is required in flags or fixture")
	}
	if file.ParkID == "" {
		return parityFile{}, errors.New("park_id is required")
	}
	if file.Horizon != horizonCountAsOf && file.Horizon != horizonFeedTargetDate {
		return parityFile{}, errors.New("horizon must be count_as_of or feed_target_date")
	}
	if file.effectiveMode != modeSample && file.effectiveMode != modeExact {
		return parityFile{}, errors.New("coverage_mode must be sample or exact")
	}
	if len(file.ExpectedRows) == 0 {
		return parityFile{}, errors.New("expected_rows must not be empty")
	}
	if now == nil {
		now = time.Now
	}
	file.asOf = now().UTC()
	if strings.TrimSpace(file.AsOf) != "" {
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(file.AsOf))
		if err != nil {
			return parityFile{}, errors.New("as_of must be RFC3339")
		}
		file.asOf = parsed.UTC()
	}
	if file.Horizon == horizonFeedTargetDate {
		if strings.TrimSpace(file.TargetDate) == "" {
			return parityFile{}, errors.New("target_date is required for feed_target_date parity")
		}
		parsed, err := parseDateOrInstant(strings.TrimSpace(file.TargetDate))
		if err != nil {
			return parityFile{}, err
		}
		file.targetDate = dateOnly(parsed)
	} else {
		file.targetDate = dateOnly(file.asOf)
	}
	seen := map[string]bool{}
	for i := range file.ExpectedRows {
		row := &file.ExpectedRows[i]
		row.ShedID = strings.TrimSpace(row.ShedID)
		row.BreedKey = aliasNorm(row.BreedKey)
		if row.StageTag != nil {
			trimmed := aliasNorm(*row.StageTag)
			row.StageTag = &trimmed
		}
		if row.AgeClass != nil {
			trimmed := aliasNorm(*row.AgeClass)
			row.AgeClass = &trimmed
		}
		if row.Sex != nil {
			trimmed := aliasNorm(*row.Sex)
			row.Sex = &trimmed
		}
		if row.RationContextResolutionState != nil {
			trimmed := strings.TrimSpace(*row.RationContextResolutionState)
			row.RationContextResolutionState = &trimmed
		}
		if row.ShedID == "" || row.BreedKey == "" {
			return parityFile{}, fmt.Errorf("expected_rows[%d] shed_id and breed_key are required", i)
		}
		if row.HeadCount < 0 {
			return parityFile{}, fmt.Errorf("expected_rows[%d] head_count must be non-negative", i)
		}
		if row.PregnantCount < 0 || row.LactatingCount < 0 || row.WarmupCount < 0 {
			return parityFile{}, fmt.Errorf("expected_rows[%d] high-risk counts must be non-negative", i)
		}
		if row.PregnantCount > row.HeadCount || row.LactatingCount > row.HeadCount || row.WarmupCount > row.HeadCount {
			return parityFile{}, fmt.Errorf("expected_rows[%d] pregnant/lactating/warmup counts must not exceed head_count", i)
		}
		key := parityRowKey(row.ShedID, row.BreedKey, ptrValue(row.StageTag), ptrValue(row.AgeClass), ptrValue(row.Sex))
		if seen[key] {
			return parityFile{}, fmt.Errorf("duplicate expected row for %s", key)
		}
		seen[key] = true
	}
	sort.Slice(file.ExpectedRows, func(i, j int) bool {
		left := file.ExpectedRows[i]
		right := file.ExpectedRows[j]
		return parityRowKey(left.ShedID, left.BreedKey, ptrValue(left.StageTag), ptrValue(left.AgeClass), ptrValue(left.Sex)) <
			parityRowKey(right.ShedID, right.BreedKey, ptrValue(right.StageTag), ptrValue(right.AgeClass), ptrValue(right.Sex))
	})
	return file, nil
}

func checkSourceParity(ctx context.Context, reader projectionReader, fixture parityFile, limit int32) (parityResult, error) {
	req := countsdomain.CountProjectionRequest{
		TenantID: fixture.TenantID,
		ParkID:   fixture.ParkID,
		AsOf:     fixture.asOf,
		Limit:    limit,
	}
	if fixture.Horizon == horizonFeedTargetDate {
		req.TargetDate = fixture.targetDate
	} else {
		req.TargetDate = fixture.asOf
	}
	var projection countsdomain.CountProjection
	var err error
	if fixture.Horizon == horizonFeedTargetDate {
		projection, err = reader.ProjectedCountFor(ctx, req)
	} else {
		projection, err = reader.CountAsOf(ctx, req)
	}
	if err != nil {
		return parityResult{}, err
	}
	return compareParityRows(fixture, projection), nil
}

func compareParityRows(fixture parityFile, projection countsdomain.CountProjection) parityResult {
	result := parityResult{
		Expected:  len(fixture.ExpectedRows),
		Actual:    len(projection.Rows),
		Truncated: projection.NextCursor != nil,
	}
	actual := map[string]countsdomain.ProjectionRow{}
	for _, row := range projection.Rows {
		key := parityRowKey(row.ShedID, row.BreedKey, ptrValue(row.StageTag), ptrValue(row.AgeClass), ptrValue(row.Sex))
		actual[key] = row
	}
	expectedKeys := map[string]bool{}
	for _, expected := range fixture.ExpectedRows {
		key := parityRowKey(expected.ShedID, expected.BreedKey, ptrValue(expected.StageTag), ptrValue(expected.AgeClass), ptrValue(expected.Sex))
		expectedKeys[key] = true
		got, ok := actual[key]
		if !ok {
			result.Missing = append(result.Missing, key)
			continue
		}
		if got.HeadCount != expected.HeadCount {
			result.Mismatched = append(result.Mismatched,
				fmt.Sprintf("%s head_count got=%d want=%d", key, got.HeadCount, expected.HeadCount))
		}
		if got.PregnantCount != expected.PregnantCount {
			result.Mismatched = append(result.Mismatched,
				fmt.Sprintf("%s pregnant_count got=%d want=%d", key, got.PregnantCount, expected.PregnantCount))
		}
		if got.LactatingCount != expected.LactatingCount {
			result.Mismatched = append(result.Mismatched,
				fmt.Sprintf("%s lactating_count got=%d want=%d", key, got.LactatingCount, expected.LactatingCount))
		}
		if got.WarmupCount != expected.WarmupCount {
			result.Mismatched = append(result.Mismatched,
				fmt.Sprintf("%s warmup_count got=%d want=%d", key, got.WarmupCount, expected.WarmupCount))
		}
		if expected.RationContextResolutionState != nil &&
			got.RationContextResolutionState != strings.TrimSpace(*expected.RationContextResolutionState) {
			result.Mismatched = append(result.Mismatched,
				fmt.Sprintf("%s ration_state got=%q want=%q", key, got.RationContextResolutionState, *expected.RationContextResolutionState))
		}
	}
	if fixture.effectiveMode == modeExact {
		for key := range actual {
			if !expectedKeys[key] {
				result.Unexpected = append(result.Unexpected, key)
			}
		}
	}
	sort.Strings(result.Missing)
	sort.Strings(result.Mismatched)
	sort.Strings(result.Unexpected)
	return result
}

func parityStatus(sourceRef string, result parityResult) (string, string) {
	failures := []string{}
	if result.Truncated {
		failures = append(failures, "projection read returned next_cursor; parity check must use a bounded complete fixture/page")
	}
	if len(result.Missing) > 0 {
		failures = append(failures, "missing="+strings.Join(result.Missing, ","))
	}
	if len(result.Mismatched) > 0 {
		failures = append(failures, "mismatched="+strings.Join(result.Mismatched, ";"))
	}
	if len(result.Unexpected) > 0 {
		failures = append(failures, "unexpected="+strings.Join(result.Unexpected, ","))
	}
	if len(failures) > 0 {
		return "blocked", fmt.Sprintf("Counts source parity failed for %s: %s", sourceRef, strings.Join(failures, "; "))
	}
	return "pending", fmt.Sprintf(
		"Counts source parity passed for %d expected rows from %s; broader source parity, observability breadth, and seeded local E2E remain before CSG10 can turn ready.",
		result.Expected, sourceRef)
}

func upsertCSG10Readiness(ctx context.Context, db readinessWriter, tenantID, sourceRef, status, blocker string) error {
	_, err := db.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG10', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/counts-source-parity-check;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
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
		tenantID, status, "counts-source-parity-check:"+sourceRef+":"+time.Now().UTC().Format(time.RFC3339), blocker)
	if err != nil {
		return fmt.Errorf("counts: upsert CSG10 source parity readiness: %w", err)
	}
	return nil
}

func inputReader(path string) (io.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}

func parityRowKey(shedID, breedKey, stageTag, ageClass, sex string) string {
	return strings.TrimSpace(shedID) + "\x00" + aliasNorm(breedKey) + "\x00" +
		aliasNorm(stageTag) + "\x00" + aliasNorm(ageClass) + "\x00" + aliasNorm(sex)
}

func aliasNorm(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), "_"))
}

func ptrValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func parseDateOrInstant(raw string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("target_date must be YYYY-MM-DD or RFC3339")
	}
	return t.UTC(), nil
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), t.UTC().Month(), t.UTC().Day(), 0, 0, 0, 0, time.UTC)
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func boolEnv(name string) bool {
	raw := strings.ToLower(getenv(name))
	return raw == "1" || raw == "true" || raw == "yes"
}

func intEnv(name string, fallback int) int {
	raw := getenv(name)
	if raw == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
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
