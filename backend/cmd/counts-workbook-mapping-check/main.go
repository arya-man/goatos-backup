// Command counts-workbook-mapping-check validates sanitized workbook column
// mappings used by Feed Direction G2 source parity. It is CSG10 evidence only:
// passing mappings move readiness to pending, never ready, because owner review,
// full parity, observability breadth, and seeded E2E remain separate gates.
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

	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	defaultTimeout = 120 * time.Second
)

var allowedCanonicalFields = map[string]bool{
	"age_class":                   true,
	"area_sq_ft":                  true,
	"breed":                       true,
	"capacity_value":              true,
	"consumed_quantity":           true,
	"consumption_processed_state": true,
	"counted_at":                  true,
	"counted_by":                  true,
	"dry_matter_factor":           true,
	"energy_density":              true,
	"energy_requirement":          true,
	"event_time":                  true,
	"farm_code":                   true,
	"feed_date":                   true,
	"feed_factor":                 true,
	"feed_item":                   true,
	"feed_quantity_as_fed":        true,
	"feed_session_total":          true,
	"head_count":                  true,
	"message_ref":                 true,
	"net_energy":                  true,
	"packing_processed_state":     true,
	"potential_tag":               true,
	"proof_media_ref":             true,
	"proof_timestamp":             true,
	"protein_requirement":         true,
	"session_label":               true,
	"shed_name":                   true,
	"shed_tag":                    true,
	"stage_tag":                   true,
	"transport_time":              true,
	"user_ref":                    true,
	"wastage_factor":              true,
	"wastage_quantity":            true,
	"water_proof_media_ref":       true,
}

var allowedSourceRoles = map[string]bool{
	"base_count_anchor":            true,
	"consumption_wastage_proof":    true,
	"count_import_stage":           true,
	"feed_direction_output":        true,
	"feed_vector_candidate":        true,
	"future_count_candidate":       true,
	"packing_proof":                true,
	"projected_count_candidate":    true,
	"ration_requirement_candidate": true,
	"session_template":             true,
	"shed_profile":                 true,
	"supply_planning_candidate":    true,
	"transport_proof":              true,
}

var allowedValueTypes = map[string]bool{
	"boolean":   true,
	"date":      true,
	"decimal":   true,
	"enum":      true,
	"formula":   true,
	"integer":   true,
	"media_ref": true,
	"text":      true,
	"time":      true,
	"timestamp": true,
	"url":       true,
}

var requiredFieldsByRole = map[string][]string{
	"base_count_anchor": {
		"counted_at", "farm_code", "shed_name", "shed_tag", "breed", "age_class", "head_count",
	},
	"count_import_stage": {
		"counted_at", "farm_code", "shed_name", "shed_tag", "breed", "age_class", "head_count",
	},
	"future_count_candidate": {
		"counted_at", "farm_code", "shed_name", "shed_tag", "breed", "age_class", "head_count",
	},
	"projected_count_candidate": {
		"counted_at", "farm_code", "shed_name", "shed_tag", "breed", "age_class", "head_count",
	},
	"feed_direction_output": {
		"feed_date", "farm_code", "session_label", "shed_name", "shed_tag", "breed",
		"age_class", "head_count", "feed_item", "feed_quantity_as_fed",
	},
	"feed_vector_candidate": {
		"feed_item", "energy_density", "dry_matter_factor", "wastage_factor", "net_energy",
	},
	"ration_requirement_candidate": {
		"breed", "stage_tag", "energy_requirement", "feed_factor",
	},
	"supply_planning_candidate": {
		"feed_date", "farm_code", "shed_name", "shed_tag", "breed", "age_class",
		"head_count", "feed_item", "feed_quantity_as_fed",
	},
	"session_template": {
		"farm_code", "session_label", "feed_item",
	},
	"packing_proof": {
		"feed_date", "farm_code", "shed_name", "session_label", "feed_item",
		"feed_quantity_as_fed", "proof_media_ref",
	},
	"consumption_wastage_proof": {
		"feed_date", "farm_code", "shed_name", "session_label", "consumed_quantity",
		"wastage_quantity", "proof_media_ref",
	},
	"transport_proof": {
		"feed_date", "farm_code", "shed_name", "transport_time", "proof_media_ref",
	},
	"shed_profile": {
		"shed_name", "shed_tag", "capacity_value",
	},
}

type config struct {
	File      string
	TenantID  string
	SourceRef string
	DryRun    bool
	Timeout   time.Duration
}

type workbookMappingFile struct {
	SourceRef     string         `json:"source_ref"`
	MappedColumns []mappedColumn `json:"mapped_columns"`
}

type mappedColumn struct {
	SourceSystem   string `json:"source_system"`
	Workbook       string `json:"workbook"`
	Sheet          string `json:"sheet"`
	SourceRole     string `json:"source_role"`
	ColumnName     string `json:"column_name"`
	CanonicalField string `json:"canonical_field"`
	ValueType      string `json:"value_type"`
	Notes          string `json:"notes,omitempty"`
}

type workbookMappingResult struct {
	Columns int
	Groups  int
	Missing []string
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

	mapping, err := parseWorkbookMappingFile(reader)
	if err != nil {
		return err
	}
	if cfg.SourceRef == "" {
		cfg.SourceRef = mapping.SourceRef
	}
	if cfg.SourceRef == "" {
		return errors.New("source-ref is required in flags or file")
	}
	result := checkWorkbookMapping(mapping)
	status, blocker := mappingStatus(cfg.SourceRef, result)
	if cfg.DryRun {
		fmt.Fprintf(stdout, "counts workbook mapping dry-run status=%s source_ref=%s mapped_columns=%d role_groups=%d missing=%d\n",
			status, cfg.SourceRef, result.Columns, result.Groups, len(result.Missing))
		if status == "blocked" {
			return errors.New(blocker)
		}
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

	if err := upsertCSG10MappingReadiness(ctx, pool, cfg.TenantID, cfg.SourceRef, status, blocker); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "counts workbook mapping status=%s source_ref=%s mapped_columns=%d role_groups=%d missing=%d\n",
		status, cfg.SourceRef, result.Columns, result.Groups, len(result.Missing))
	if status == "blocked" {
		return errors.New(blocker)
	}
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("counts-workbook-mapping-check", flag.ContinueOnError)
	fs.StringVar(&cfg.File, "file", getenv("GOATOS_COUNTS_WORKBOOK_MAPPING_FILE"), "sanitized workbook mapping JSON file")
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "tenant id")
	fs.StringVar(&cfg.SourceRef, "source-ref", getenv("GOATOS_COUNTS_WORKBOOK_MAPPING_SOURCE_REF"), "source finding or reviewed file reference")
	fs.BoolVar(&cfg.DryRun, "dry-run", boolEnv("GOATOS_COUNTS_WORKBOOK_MAPPING_DRY_RUN"), "validate mapping without DB writes")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_WORKBOOK_MAPPING_TIMEOUT", defaultTimeout), "worker timeout")
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

func parseWorkbookMappingFile(r io.Reader) (workbookMappingFile, error) {
	var file workbookMappingFile
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return workbookMappingFile{}, err
	}
	file.SourceRef = strings.TrimSpace(file.SourceRef)
	if len(file.MappedColumns) == 0 {
		return workbookMappingFile{}, errors.New("mapped_columns must not be empty")
	}
	seen := map[string]bool{}
	for i := range file.MappedColumns {
		column := &file.MappedColumns[i]
		column.SourceSystem = strings.TrimSpace(column.SourceSystem)
		column.Workbook = strings.TrimSpace(column.Workbook)
		column.Sheet = strings.TrimSpace(column.Sheet)
		column.SourceRole = strings.TrimSpace(column.SourceRole)
		column.ColumnName = strings.TrimSpace(column.ColumnName)
		column.CanonicalField = strings.TrimSpace(column.CanonicalField)
		column.ValueType = strings.TrimSpace(column.ValueType)
		column.Notes = strings.TrimSpace(column.Notes)
		if column.SourceSystem == "" || column.Workbook == "" || column.Sheet == "" || column.ColumnName == "" {
			return workbookMappingFile{}, fmt.Errorf("mapped_columns[%d] source_system, workbook, sheet, and column_name are required", i)
		}
		if !allowedSourceRoles[column.SourceRole] {
			return workbookMappingFile{}, fmt.Errorf("mapped_columns[%d].source_role is invalid", i)
		}
		if !allowedCanonicalFields[column.CanonicalField] {
			return workbookMappingFile{}, fmt.Errorf("mapped_columns[%d].canonical_field is invalid", i)
		}
		if !allowedValueTypes[column.ValueType] {
			return workbookMappingFile{}, fmt.Errorf("mapped_columns[%d].value_type is invalid", i)
		}
		key := strings.Join([]string{
			column.SourceSystem, column.Workbook, column.Sheet, column.SourceRole, column.ColumnName, column.CanonicalField,
		}, "\x00")
		if seen[key] {
			return workbookMappingFile{}, fmt.Errorf("duplicate mapped column %q/%q/%q/%q/%q",
				column.SourceSystem, column.Workbook, column.Sheet, column.SourceRole, column.ColumnName)
		}
		seen[key] = true
	}
	sort.Slice(file.MappedColumns, func(i, j int) bool {
		left := file.MappedColumns[i]
		right := file.MappedColumns[j]
		return mappingSortKey(left) < mappingSortKey(right)
	})
	return file, nil
}

func checkWorkbookMapping(file workbookMappingFile) workbookMappingResult {
	groups := map[string]map[string]bool{}
	labels := map[string]string{}
	for _, column := range file.MappedColumns {
		key := roleGroupKey(column)
		if groups[key] == nil {
			groups[key] = map[string]bool{}
			labels[key] = roleGroupLabel(column)
		}
		groups[key][column.CanonicalField] = true
	}
	result := workbookMappingResult{Columns: len(file.MappedColumns), Groups: len(groups)}
	for key, fields := range groups {
		role := roleFromGroupKey(key)
		for _, required := range requiredFieldsByRole[role] {
			if !fields[required] {
				result.Missing = append(result.Missing, labels[key]+"/"+required)
			}
		}
	}
	sort.Strings(result.Missing)
	return result
}

func mappingStatus(sourceRef string, result workbookMappingResult) (string, string) {
	if len(result.Missing) > 0 {
		return "blocked", fmt.Sprintf("Counts workbook mapping coverage failed for %s: missing=%s",
			sourceRef, strings.Join(result.Missing, ","))
	}
	return "pending", fmt.Sprintf(
		"Counts workbook mapping manifest covers %d columns across %d role groups from %s; owner-approved mapping review, full source parity, observability breadth, and seeded local E2E remain before CSG10 can turn ready.",
		result.Columns, result.Groups, sourceRef)
}

func upsertCSG10MappingReadiness(ctx context.Context, db readinessWriter, tenantID, sourceRef, status, blocker string) error {
	_, err := db.Exec(ctx, `
INSERT INTO counts_shifting_readiness_subgates (
  tenant_id, subgate_id, status, owner, evidence_ref, blocker_reason, implementation_ref, last_checked_at, updated_at
) VALUES (
  $1::uuid, 'CSG10', $2, 'Counts/Shifting + Feed Direction',
  $3, $4, 'backend/cmd/counts-workbook-mapping-check;docs/feed-direction/COUNTS-SHIFTING-CLOSURE-TRD.md',
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
		tenantID, status, "counts-workbook-mapping-check:"+sourceRef+":"+time.Now().UTC().Format(time.RFC3339), blocker)
	if err != nil {
		return fmt.Errorf("counts: upsert CSG10 workbook mapping readiness: %w", err)
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

func mappingSortKey(column mappedColumn) string {
	return roleGroupKey(column) + "\x00" + column.ColumnName + "\x00" + column.CanonicalField
}

func roleGroupKey(column mappedColumn) string {
	return strings.Join([]string{column.SourceSystem, column.Workbook, column.Sheet, column.SourceRole}, "\x00")
}

func roleFromGroupKey(key string) string {
	parts := strings.Split(key, "\x00")
	if len(parts) < 4 {
		return ""
	}
	return parts[3]
}

func roleGroupLabel(column mappedColumn) string {
	return column.SourceSystem + "/" + column.Workbook + "/" + column.Sheet + "/" + column.SourceRole
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
