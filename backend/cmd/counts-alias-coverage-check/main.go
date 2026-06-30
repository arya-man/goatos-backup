// Command counts-alias-coverage-check verifies that source-required Counts
// dimension aliases have reviewed mappings before Feed Direction consumes
// projections. It is CSG7 evidence only: a clean run can move CSG7 to pending,
// not ready, because source parity and owner approval remain separate gates.
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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

var allowedDimensions = map[string]bool{
	"breed": true, "stage_tag": true, "age_class": true, "sex": true, "shed_tag": true,
}

type config struct {
	File      string
	TenantID  string
	SourceRef string
	DryRun    bool
	Timeout   time.Duration
}

type coverageFile struct {
	SourceRef       string          `json:"source_ref"`
	RequiredAliases []requiredAlias `json:"required_aliases"`
}

type requiredAlias struct {
	Dimension    string `json:"dimension"`
	SourceSystem string `json:"source_system"`
	SourceValue  string `json:"source_value"`
}

type aliasCoverageResult struct {
	Total   int
	Missing []requiredAlias
}

type aliasLookup interface {
	QueryRow(context.Context, string, ...any) pgx.Row
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

	coverage, err := parseRequiredAliases(reader)
	if err != nil {
		return err
	}
	if cfg.SourceRef == "" {
		cfg.SourceRef = strings.TrimSpace(coverage.SourceRef)
	}
	if cfg.SourceRef == "" {
		return errors.New("source-ref is required in flags or file")
	}
	if cfg.DryRun {
		fmt.Fprintf(stdout, "counts alias coverage dry-run ok source_ref=%s required_aliases=%d\n",
			cfg.SourceRef, len(coverage.RequiredAliases))
		return nil
	}
	if cfg.TenantID == "" {
		return errors.New("tenant-id is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	result, err := checkAliasCoverage(ctx, pool, cfg.TenantID, coverage.RequiredAliases)
	if err != nil {
		return err
	}
	status, blocker := coverageStatus(cfg.SourceRef, result)
	if err := upsertCSG7Readiness(ctx, pool, cfg.TenantID, cfg.SourceRef, status, blocker); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "counts alias coverage status=%s source_ref=%s required_aliases=%d missing=%d\n",
		status, cfg.SourceRef, result.Total, len(result.Missing))
	for _, missing := range result.Missing {
		fmt.Fprintf(stdout, "counts alias coverage missing dimension=%s source_system=%s source_value=%s\n",
			missing.Dimension, missing.SourceSystem, missing.SourceValue)
	}
	if status == "blocked" {
		return errors.New(blocker)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("counts-alias-coverage-check", flag.ContinueOnError)
	fs.StringVar(&cfg.File, "file", getenv("GOATOS_COUNTS_ALIAS_COVERAGE_FILE"), "required alias coverage JSON file")
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.SourceRef, "source-ref", getenv("GOATOS_COUNTS_ALIAS_COVERAGE_SOURCE_REF"), "source finding or reviewed file reference")
	fs.BoolVar(&cfg.DryRun, "dry-run", boolEnv("GOATOS_COUNTS_ALIAS_COVERAGE_DRY_RUN"), "validate required-alias file without DB writes")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_ALIAS_COVERAGE_TIMEOUT", 120*time.Second), "worker timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.File = strings.TrimSpace(cfg.File)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.SourceRef = strings.TrimSpace(cfg.SourceRef)
	if cfg.File == "" {
		return config{}, errors.New("file is required")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}

func inputReader(path string) (io.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}

func parseRequiredAliases(r io.Reader) (coverageFile, error) {
	var file coverageFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return coverageFile{}, err
	}
	file.SourceRef = strings.TrimSpace(file.SourceRef)
	if len(file.RequiredAliases) == 0 {
		return coverageFile{}, errors.New("required_aliases must not be empty")
	}
	seen := map[string]bool{}
	for i := range file.RequiredAliases {
		alias := &file.RequiredAliases[i]
		alias.Dimension = strings.TrimSpace(alias.Dimension)
		alias.SourceSystem = defaultString(strings.TrimSpace(alias.SourceSystem), "*")
		alias.SourceValue = strings.TrimSpace(alias.SourceValue)
		if !allowedDimensions[alias.Dimension] {
			return coverageFile{}, fmt.Errorf("required_aliases[%d].dimension is invalid", i)
		}
		if alias.SourceValue == "" {
			return coverageFile{}, fmt.Errorf("required_aliases[%d].source_value is required", i)
		}
		key := alias.Dimension + "\x00" + alias.SourceSystem + "\x00" + aliasNorm(alias.SourceValue)
		if seen[key] {
			return coverageFile{}, fmt.Errorf("duplicate required alias %q", alias.SourceValue)
		}
		seen[key] = true
	}
	sort.Slice(file.RequiredAliases, func(i, j int) bool {
		left := file.RequiredAliases[i]
		right := file.RequiredAliases[j]
		return left.Dimension+"\x00"+left.SourceSystem+"\x00"+aliasNorm(left.SourceValue) <
			right.Dimension+"\x00"+right.SourceSystem+"\x00"+aliasNorm(right.SourceValue)
	})
	return file, nil
}

func checkAliasCoverage(ctx context.Context, db aliasLookup, tenantID string, required []requiredAlias) (aliasCoverageResult, error) {
	result := aliasCoverageResult{Total: len(required)}
	for _, alias := range required {
		ok, err := hasApprovedAlias(ctx, db, tenantID, alias)
		if err != nil {
			return aliasCoverageResult{}, err
		}
		if !ok {
			result.Missing = append(result.Missing, alias)
		}
	}
	return result, nil
}

func hasApprovedAlias(ctx context.Context, db aliasLookup, tenantID string, alias requiredAlias) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM count_dimension_aliases
  WHERE tenant_id = $1::uuid
    AND dimension = $2
    AND source_value_norm = $3
    AND review_status = 'approved'
    AND (source_system = $4 OR source_system = '*')
    AND effective_from <= CURRENT_DATE
    AND (effective_to IS NULL OR effective_to > CURRENT_DATE)
)`, tenantID, alias.Dimension, aliasNorm(alias.SourceValue), alias.SourceSystem).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("counts: check alias coverage %s/%s/%s: %w",
			alias.Dimension, alias.SourceSystem, alias.SourceValue, err)
	}
	return ok, nil
}

func coverageStatus(sourceRef string, result aliasCoverageResult) (string, string) {
	if len(result.Missing) == 0 {
		return "pending", fmt.Sprintf(
			"Counts alias coverage check passed for %d required aliases from %s; full source workbook parity, Sheds DB profile publish flow, seeded local E2E, and owner-approved review remain before CSG7 can turn ready.",
			result.Total, sourceRef)
	}
	missing := make([]string, 0, len(result.Missing))
	for _, alias := range result.Missing {
		missing = append(missing, alias.Dimension+"/"+alias.SourceSystem+"/"+alias.SourceValue)
	}
	sort.Strings(missing)
	return "blocked", fmt.Sprintf(
		"Counts alias coverage is missing %d/%d reviewed aliases from %s: %s",
		len(result.Missing), result.Total, sourceRef, strings.Join(missing, ", "))
}

func upsertCSG7Readiness(ctx context.Context, db readinessWriter, tenantID, sourceRef, status, blocker string) error {
	_, err := db.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG7', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/counts-alias-coverage-check;context/source-findings/sheds-db-source-findings.md;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
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
		tenantID, status, "counts-alias-coverage-check:"+sourceRef+":"+time.Now().UTC().Format(time.RFC3339), blocker)
	if err != nil {
		return fmt.Errorf("counts: upsert CSG7 alias coverage readiness: %w", err)
	}
	return nil
}

func aliasNorm(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(value)), "_"))
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func getenv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func boolEnv(name string) bool {
	raw := strings.ToLower(getenv(name))
	return raw == "1" || raw == "true" || raw == "yes"
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
