// Command location-profile-coverage-check verifies that reviewed Sheds DB
// profile rows have landed in canonical Locations alias/capacity tables before
// Feed Direction treats profile-tag coverage as proven. Passing this command is
// CSG7 evidence only: it can move readiness to pending, never ready, because
// owner approval, source parity, and seeded E2E remain separate evidence.
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

const defaultSource = "sheds_db"

type config struct {
	File      string
	TenantID  string
	SourceRef string
	DryRun    bool
	Timeout   time.Duration
}

type coverageFile struct {
	SourceRef        string            `json:"source_ref"`
	RequiredProfiles []requiredProfile `json:"required_profiles"`
}

type requiredProfile struct {
	LocationID    string `json:"location_id"`
	AliasCode     string `json:"alias_code"`
	SourceContext string `json:"source_context"`
	CapacityKind  string `json:"capacity_kind"`
	CapacityValue int    `json:"capacity_value"`
	EffectiveFrom string `json:"effective_from"`
	Source        string `json:"source"`
}

type coverageResult struct {
	Total   int
	Missing []missingProfile
}

type missingProfile struct {
	Profile requiredProfile
	Reason  string
}

type profileLookup interface {
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

	coverage, err := parseRequiredProfiles(reader)
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
		fmt.Fprintf(stdout, "location profile coverage dry-run ok source_ref=%s required_profiles=%d\n",
			cfg.SourceRef, len(coverage.RequiredProfiles))
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

	result, err := checkProfileCoverage(ctx, pool, cfg.TenantID, coverage.RequiredProfiles)
	if err != nil {
		return err
	}
	status, blocker := coverageStatus(cfg.SourceRef, result)
	if err := upsertCSG7Readiness(ctx, pool, cfg.TenantID, cfg.SourceRef, status, blocker); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "location profile coverage status=%s source_ref=%s required_profiles=%d missing=%d\n",
		status, cfg.SourceRef, result.Total, len(result.Missing))
	for _, missing := range result.Missing {
		fmt.Fprintf(stdout, "location profile coverage missing location_id=%s alias_code=%q reason=%s\n",
			missing.Profile.LocationID, missing.Profile.AliasCode, missing.Reason)
	}
	if status == "blocked" {
		return errors.New(blocker)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("location-profile-coverage-check", flag.ContinueOnError)
	fs.StringVar(&cfg.File, "file", getenv("GOATOS_LOCATION_PROFILE_COVERAGE_FILE"), "required profile coverage JSON file")
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.SourceRef, "source-ref", getenv("GOATOS_LOCATION_PROFILE_COVERAGE_SOURCE_REF"), "source finding or reviewed file reference")
	fs.BoolVar(&cfg.DryRun, "dry-run", boolEnv("GOATOS_LOCATION_PROFILE_COVERAGE_DRY_RUN"), "validate required profile file without DB writes")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_LOCATION_PROFILE_COVERAGE_TIMEOUT", 120*time.Second), "worker timeout")
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

func parseRequiredProfiles(r io.Reader) (coverageFile, error) {
	var file coverageFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return coverageFile{}, err
	}
	file.SourceRef = strings.TrimSpace(file.SourceRef)
	if len(file.RequiredProfiles) == 0 {
		return coverageFile{}, errors.New("required_profiles must not be empty")
	}
	seen := map[string]bool{}
	for i := range file.RequiredProfiles {
		profile := &file.RequiredProfiles[i]
		profile.LocationID = strings.TrimSpace(profile.LocationID)
		profile.AliasCode = strings.TrimSpace(profile.AliasCode)
		profile.SourceContext = defaultString(strings.TrimSpace(profile.SourceContext), defaultSource)
		profile.CapacityKind = defaultString(strings.TrimSpace(profile.CapacityKind), "goat_occupancy")
		profile.EffectiveFrom = strings.TrimSpace(profile.EffectiveFrom)
		profile.Source = defaultString(strings.TrimSpace(profile.Source), defaultSource)
		if profile.LocationID == "" {
			return coverageFile{}, fmt.Errorf("required_profiles[%d].location_id is required", i)
		}
		if profile.AliasCode == "" {
			return coverageFile{}, fmt.Errorf("required_profiles[%d].alias_code is required", i)
		}
		if profile.CapacityValue <= 0 {
			return coverageFile{}, fmt.Errorf("required_profiles[%d].capacity_value must be positive", i)
		}
		if profile.EffectiveFrom == "" {
			return coverageFile{}, fmt.Errorf("required_profiles[%d].effective_from is required", i)
		}
		key := profile.LocationID + "\x00" + profile.SourceContext + "\x00" + profileNorm(profile.AliasCode) + "\x00" + profile.CapacityKind
		if seen[key] {
			return coverageFile{}, fmt.Errorf("duplicate required profile %q", profile.AliasCode)
		}
		seen[key] = true
	}
	sort.Slice(file.RequiredProfiles, func(i, j int) bool {
		left := file.RequiredProfiles[i]
		right := file.RequiredProfiles[j]
		return left.LocationID+"\x00"+profileNorm(left.AliasCode) <
			right.LocationID+"\x00"+profileNorm(right.AliasCode)
	})
	return file, nil
}

func checkProfileCoverage(ctx context.Context, db profileLookup, tenantID string, required []requiredProfile) (coverageResult, error) {
	result := coverageResult{Total: len(required)}
	for _, profile := range required {
		aliasOK, err := hasActiveAlias(ctx, db, tenantID, profile)
		if err != nil {
			return coverageResult{}, err
		}
		if !aliasOK {
			result.Missing = append(result.Missing, missingProfile{Profile: profile, Reason: "missing_active_alias"})
			continue
		}
		capacityOK, err := hasCapacity(ctx, db, tenantID, profile)
		if err != nil {
			return coverageResult{}, err
		}
		if !capacityOK {
			result.Missing = append(result.Missing, missingProfile{Profile: profile, Reason: "missing_capacity"})
		}
	}
	return result, nil
}

func hasActiveAlias(ctx context.Context, db profileLookup, tenantID string, profile requiredProfile) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM location_aliases
  WHERE tenant_id = $1::uuid
    AND canonical_location_id = $2::uuid
    AND lower(btrim(regexp_replace(alias_code, '[[:space:]]+', ' ', 'g'))) = $3
    AND source_context = $4
    AND status = 'active'
)`, tenantID, profile.LocationID, profileNorm(profile.AliasCode), profile.SourceContext).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("locations: check alias coverage %s/%s: %w", profile.LocationID, profile.AliasCode, err)
	}
	return ok, nil
}

func hasCapacity(ctx context.Context, db profileLookup, tenantID string, profile requiredProfile) (bool, error) {
	var ok bool
	err := db.QueryRow(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM location_capacity_records
  WHERE tenant_id = $1::uuid
    AND location_id = $2::uuid
    AND capacity_kind = $3
    AND capacity_value = $4
    AND effective_from = $5::date
    AND source = $6
)`, tenantID, profile.LocationID, profile.CapacityKind, profile.CapacityValue, profile.EffectiveFrom, profile.Source).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("locations: check capacity coverage %s/%s: %w", profile.LocationID, profile.AliasCode, err)
	}
	return ok, nil
}

func coverageStatus(sourceRef string, result coverageResult) (string, string) {
	if len(result.Missing) == 0 {
		return "pending", fmt.Sprintf(
			"Sheds DB location-profile coverage passed for %d required profiles from %s; owner-approved review, source parity, projection replay, and seeded local E2E remain before CSG7 can turn ready.",
			result.Total, sourceRef)
	}
	missing := make([]string, 0, len(result.Missing))
	for _, item := range result.Missing {
		missing = append(missing, item.Profile.LocationID+"/"+item.Profile.SourceContext+"/"+item.Profile.AliasCode+"/"+item.Reason)
	}
	sort.Strings(missing)
	return "blocked", fmt.Sprintf(
		"Sheds DB location-profile coverage is missing %d/%d reviewed profile rows from %s: %s",
		len(result.Missing), result.Total, sourceRef, strings.Join(missing, ", "))
}

func upsertCSG7Readiness(ctx context.Context, db readinessWriter, tenantID, sourceRef, status, blocker string) error {
	_, err := db.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG7', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/location-profile-coverage-check;backend/cmd/location-profile-source-import;context/source-findings/sheds-db-source-findings.md;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
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
		tenantID, status, fmt.Sprintf("location-profile-coverage-check:%s:%s", sourceRef, status), blocker)
	if err != nil {
		return fmt.Errorf("locations: upsert CSG7 profile coverage readiness: %w", err)
	}
	return nil
}

func profileNorm(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func getenv(key string) string {
	return os.Getenv(key)
}

func boolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}
