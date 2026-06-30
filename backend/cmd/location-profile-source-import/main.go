// Command location-profile-source-import imports reviewed source rows into the
// canonical Locations service. It accepts typed JSONL evidence rows, not raw
// workbooks: Sheds DB and legacy sheets remain source evidence until reviewed
// rows reference canonical GoatOS locations or explicit review work.
package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	locationpg "github.com/vgoats/goatos/backend/internal/locations/adapters/postgres"
	locationapp "github.com/vgoats/goatos/backend/internal/locations/app"
	locationdomain "github.com/vgoats/goatos/backend/internal/locations/domain"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	kindLocationAlias      = "location_alias"
	kindLocationCapacity   = "location_capacity"
	kindLocationReviewItem = "location_review_item"
	defaultSource          = "sheds_db"
)

type config struct {
	File           string
	TenantID       string
	ActorID        string
	SourceContext  string
	CapacitySource string
	TraceID        string
	DryRun         bool
	Timeout        time.Duration
}

type importRow struct {
	Kind                string
	SourceLine          int
	Alias               *aliasRow
	Capacity            *capacityRow
	Review              *reviewRow
	DerivedIdempotency  bool
	DerivedEvidenceHash bool
}

type aliasRow struct {
	Kind           string `json:"kind"`
	TenantID       string `json:"tenant_id"`
	LocationID     string `json:"location_id"`
	AliasCode      string `json:"alias_code"`
	SourceContext  string `json:"source_context"`
	SourceRef      string `json:"source_ref"`
	Notes          string `json:"notes"`
	IdempotencyKey string `json:"idempotency_key"`
}

type capacityRow struct {
	Kind           string  `json:"kind"`
	TenantID       string  `json:"tenant_id"`
	LocationID     string  `json:"location_id"`
	CapacityKind   string  `json:"capacity_kind"`
	CapacityValue  int     `json:"capacity_value"`
	EffectiveFrom  string  `json:"effective_from"`
	EffectiveTo    *string `json:"effective_to"`
	Source         string  `json:"source"`
	SourceRef      string  `json:"source_ref"`
	Notes          string  `json:"notes"`
	IdempotencyKey string  `json:"idempotency_key"`
}

type reviewRow struct {
	Kind                  string          `json:"kind"`
	TenantID              string          `json:"tenant_id"`
	ReviewType            string          `json:"review_type"`
	SourceContext         string          `json:"source_context"`
	SourceLabel           string          `json:"source_label"`
	NormalizedSourceLabel string          `json:"normalized_source_label"`
	CanonicalLocationID   string          `json:"canonical_location_id"`
	CandidateLocationIDs  []string        `json:"candidate_location_ids"`
	Evidence              json.RawMessage `json:"evidence"`
	EvidenceHash          string          `json:"evidence_hash"`
	IdempotencyKey        string          `json:"idempotency_key"`
}

type rowEnvelope struct {
	Kind string `json:"kind"`
}

type importSummary struct {
	Rows       int
	Aliases    int
	Capacities int
	Reviews    int
	FailedRows int
	DryRun     bool
}

type locationImportService interface {
	CreateLocationAlias(context.Context, locationapp.CreateLocationAliasInput) (*locationdomain.LocationAliasResponse, error)
	CreateLocationCapacity(context.Context, locationapp.CreateLocationCapacityInput) (*locationdomain.LocationCapacityResponse, error)
	CreateLocationReviewItem(context.Context, locationapp.CreateLocationReviewItemInput) (*locationdomain.LocationReviewItemResponse, error)
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	reader, closeFn, err := inputReader(cfg.File, stdin)
	if err != nil {
		return err
	}
	defer closeFn()

	rows, err := parseImportRows(reader, cfg)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("no import rows found")
	}
	if cfg.DryRun {
		summary := summarize(rows, true)
		fmt.Fprintf(stdout, "location profile source import dry-run ok rows=%d aliases=%d capacities=%d reviews=%d\n",
			summary.Rows, summary.Aliases, summary.Capacities, summary.Reviews)
		return nil
	}
	if strings.TrimSpace(cfg.ActorID) == "" {
		return errors.New("actor-id is required when dry-run=false")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	pgCfg := platformpg.ConfigFromEnv()
	pool, err := platformpg.Connect(ctx, pgCfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	service := locationapp.NewService(locationpg.NewRepository(pool, pgCfg.QueryTimeout))
	summary, err := importRows(ctx, service, cfg, rows, stdout)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "location profile source import complete rows=%d aliases=%d capacities=%d reviews=%d\n",
		summary.Rows, summary.Aliases, summary.Capacities, summary.Reviews)
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("location-profile-source-import", flag.ContinueOnError)
	fs.StringVar(&cfg.File, "file", getenv("GOATOS_LOCATION_PROFILE_IMPORT_FILE"), "JSONL import file; default stdin")
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "default tenant id for rows that omit tenant_id")
	fs.StringVar(&cfg.ActorID, "actor-id", getenv("GOATOS_ACTOR_ID"), "actor id used for execute-mode writes")
	fs.StringVar(&cfg.SourceContext, "source-context", getenvDefault("GOATOS_LOCATION_PROFILE_SOURCE_CONTEXT", defaultSource), "default alias/review source_context")
	fs.StringVar(&cfg.CapacitySource, "capacity-source", getenvDefault("GOATOS_LOCATION_PROFILE_CAPACITY_SOURCE", defaultSource), "default capacity source")
	fs.StringVar(&cfg.TraceID, "trace-id", getenv("GOATOS_TRACE_ID"), "optional trace id")
	fs.BoolVar(&cfg.DryRun, "dry-run", boolEnv("GOATOS_LOCATION_PROFILE_IMPORT_DRY_RUN"), "validate rows without writing to Postgres")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_LOCATION_PROFILE_IMPORT_TIMEOUT", 120*time.Second), "import timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.File = strings.TrimSpace(cfg.File)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.ActorID = strings.TrimSpace(cfg.ActorID)
	cfg.SourceContext = defaultString(strings.TrimSpace(cfg.SourceContext), defaultSource)
	cfg.CapacitySource = defaultString(strings.TrimSpace(cfg.CapacitySource), defaultSource)
	cfg.TraceID = strings.TrimSpace(cfg.TraceID)
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}

func inputReader(path string, stdin io.Reader) (io.Reader, func(), error) {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(path) == "-" {
		return stdin, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return f, func() { _ = f.Close() }, nil
}

func parseImportRows(r io.Reader, cfg config) ([]importRow, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	rows := []importRow{}
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env rowEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			return nil, fmt.Errorf("line %d: decode envelope: %w", lineNo, err)
		}
		switch strings.TrimSpace(env.Kind) {
		case kindLocationAlias:
			row, err := parseAliasRow([]byte(line), cfg, lineNo)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		case kindLocationCapacity:
			row, err := parseCapacityRow([]byte(line), cfg, lineNo)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		case kindLocationReviewItem:
			row, err := parseReviewRow([]byte(line), cfg, lineNo)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		default:
			return nil, fmt.Errorf("line %d: unsupported kind %q", lineNo, env.Kind)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func parseAliasRow(raw []byte, cfg config, lineNo int) (importRow, error) {
	var row aliasRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return importRow{}, fmt.Errorf("line %d: decode location_alias: %w", lineNo, err)
	}
	row.TenantID = defaultString(strings.TrimSpace(row.TenantID), cfg.TenantID)
	row.LocationID = strings.TrimSpace(row.LocationID)
	row.AliasCode = strings.TrimSpace(row.AliasCode)
	row.SourceContext = defaultString(strings.TrimSpace(row.SourceContext), cfg.SourceContext)
	row.SourceRef = strings.TrimSpace(row.SourceRef)
	row.Notes = mergeSourceRef(strings.TrimSpace(row.Notes), row.SourceRef)
	row.IdempotencyKey = strings.TrimSpace(row.IdempotencyKey)
	derived := false
	if row.IdempotencyKey == "" {
		row.IdempotencyKey = derivedIdempotency(kindLocationAlias, raw)
		derived = true
	}
	if row.TenantID == "" || row.LocationID == "" || row.AliasCode == "" || row.SourceContext == "" {
		return importRow{}, fmt.Errorf("line %d: location_alias requires tenant_id, location_id, alias_code, and source_context", lineNo)
	}
	return importRow{Kind: kindLocationAlias, SourceLine: lineNo, Alias: &row, DerivedIdempotency: derived}, nil
}

func parseCapacityRow(raw []byte, cfg config, lineNo int) (importRow, error) {
	var row capacityRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return importRow{}, fmt.Errorf("line %d: decode location_capacity: %w", lineNo, err)
	}
	row.TenantID = defaultString(strings.TrimSpace(row.TenantID), cfg.TenantID)
	row.LocationID = strings.TrimSpace(row.LocationID)
	row.CapacityKind = defaultString(strings.TrimSpace(row.CapacityKind), "goat_occupancy")
	row.EffectiveFrom = strings.TrimSpace(row.EffectiveFrom)
	row.Source = defaultString(strings.TrimSpace(row.Source), cfg.CapacitySource)
	row.SourceRef = strings.TrimSpace(row.SourceRef)
	row.Notes = strings.TrimSpace(row.Notes)
	row.IdempotencyKey = strings.TrimSpace(row.IdempotencyKey)
	derived := false
	if row.IdempotencyKey == "" {
		row.IdempotencyKey = derivedIdempotency(kindLocationCapacity, raw)
		derived = true
	}
	if row.TenantID == "" || row.LocationID == "" || row.CapacityKind == "" || row.EffectiveFrom == "" || row.Source == "" {
		return importRow{}, fmt.Errorf("line %d: location_capacity requires tenant_id, location_id, capacity_kind, effective_from, and source", lineNo)
	}
	if row.CapacityValue <= 0 {
		return importRow{}, fmt.Errorf("line %d: location_capacity capacity_value must be positive", lineNo)
	}
	return importRow{Kind: kindLocationCapacity, SourceLine: lineNo, Capacity: &row, DerivedIdempotency: derived}, nil
}

func parseReviewRow(raw []byte, cfg config, lineNo int) (importRow, error) {
	var row reviewRow
	if err := json.Unmarshal(raw, &row); err != nil {
		return importRow{}, fmt.Errorf("line %d: decode location_review_item: %w", lineNo, err)
	}
	row.TenantID = defaultString(strings.TrimSpace(row.TenantID), cfg.TenantID)
	row.ReviewType = defaultString(strings.TrimSpace(row.ReviewType), "unknown_alias")
	row.SourceContext = defaultString(strings.TrimSpace(row.SourceContext), cfg.SourceContext)
	row.SourceLabel = strings.TrimSpace(row.SourceLabel)
	row.NormalizedSourceLabel = strings.TrimSpace(row.NormalizedSourceLabel)
	row.CanonicalLocationID = strings.TrimSpace(row.CanonicalLocationID)
	row.EvidenceHash = strings.TrimSpace(row.EvidenceHash)
	row.IdempotencyKey = strings.TrimSpace(row.IdempotencyKey)
	if len(row.Evidence) == 0 {
		evidence, err := json.Marshal(map[string]any{
			"source_context": row.SourceContext,
			"source_label":   row.SourceLabel,
			"source_line":    lineNo,
		})
		if err != nil {
			return importRow{}, err
		}
		row.Evidence = evidence
	}
	derivedEvidenceHash := false
	if row.EvidenceHash == "" {
		row.EvidenceHash = stableHash("location-profile-review-evidence", row.Evidence)
		derivedEvidenceHash = true
	}
	derivedIdem := false
	if row.IdempotencyKey == "" {
		row.IdempotencyKey = derivedIdempotency(kindLocationReviewItem, raw)
		derivedIdem = true
	}
	if row.TenantID == "" || row.ReviewType == "" {
		return importRow{}, fmt.Errorf("line %d: location_review_item requires tenant_id and review_type", lineNo)
	}
	if row.SourceLabel == "" && row.NormalizedSourceLabel == "" && row.CanonicalLocationID == "" {
		return importRow{}, fmt.Errorf("line %d: location_review_item requires source_label, normalized_source_label, or canonical_location_id", lineNo)
	}
	return importRow{Kind: kindLocationReviewItem, SourceLine: lineNo, Review: &row, DerivedIdempotency: derivedIdem, DerivedEvidenceHash: derivedEvidenceHash}, nil
}

func importRows(ctx context.Context, service locationImportService, cfg config, rows []importRow, stdout io.Writer) (importSummary, error) {
	summary := importSummary{Rows: len(rows)}
	for _, row := range rows {
		var err error
		switch row.Kind {
		case kindLocationAlias:
			err = importAlias(ctx, service, cfg, *row.Alias)
			if err == nil {
				summary.Aliases++
			}
		case kindLocationCapacity:
			err = importCapacity(ctx, service, cfg, *row.Capacity)
			if err == nil {
				summary.Capacities++
			}
		case kindLocationReviewItem:
			err = importReview(ctx, service, cfg, *row.Review)
			if err == nil {
				summary.Reviews++
			}
		}
		if err != nil {
			summary.FailedRows++
			return summary, fmt.Errorf("line %d %s: %w", row.SourceLine, row.Kind, err)
		}
		fmt.Fprintf(stdout, "imported line=%d kind=%s\n", row.SourceLine, row.Kind)
	}
	return summary, nil
}

func importAlias(ctx context.Context, service locationImportService, cfg config, row aliasRow) error {
	body := map[string]any{
		"alias_code":     row.AliasCode,
		"source_context": row.SourceContext,
	}
	if row.Notes != "" {
		body["notes"] = row.Notes
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = service.CreateLocationAlias(ctx, locationapp.CreateLocationAliasInput{
		TenantID:       row.TenantID,
		ActorID:        cfg.ActorID,
		IdempotencyKey: row.IdempotencyKey,
		TraceID:        cfg.TraceID,
		LocationID:     row.LocationID,
		RawBody:        raw,
	})
	return err
}

func importCapacity(ctx context.Context, service locationImportService, cfg config, row capacityRow) error {
	body := map[string]any{
		"capacity_kind":  row.CapacityKind,
		"capacity_value": row.CapacityValue,
		"effective_from": row.EffectiveFrom,
		"source":         row.Source,
	}
	if row.EffectiveTo != nil {
		body["effective_to"] = strings.TrimSpace(*row.EffectiveTo)
	}
	if row.SourceRef != "" {
		body["source_ref"] = row.SourceRef
	}
	if row.Notes != "" {
		body["notes"] = row.Notes
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = service.CreateLocationCapacity(ctx, locationapp.CreateLocationCapacityInput{
		TenantID:       row.TenantID,
		ActorID:        cfg.ActorID,
		IdempotencyKey: row.IdempotencyKey,
		TraceID:        cfg.TraceID,
		LocationID:     row.LocationID,
		RawBody:        raw,
	})
	return err
}

func importReview(ctx context.Context, service locationImportService, cfg config, row reviewRow) error {
	body := map[string]any{
		"review_type":   row.ReviewType,
		"evidence":      json.RawMessage(row.Evidence),
		"evidence_hash": row.EvidenceHash,
	}
	if row.SourceContext != "" {
		body["source_context"] = row.SourceContext
	}
	if row.SourceLabel != "" {
		body["source_label"] = row.SourceLabel
	}
	if row.NormalizedSourceLabel != "" {
		body["normalized_source_label"] = row.NormalizedSourceLabel
	}
	if row.CanonicalLocationID != "" {
		body["canonical_location_id"] = row.CanonicalLocationID
	}
	if len(row.CandidateLocationIDs) > 0 {
		body["candidate_location_ids"] = row.CandidateLocationIDs
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	_, err = service.CreateLocationReviewItem(ctx, locationapp.CreateLocationReviewItemInput{
		TenantID:       row.TenantID,
		ActorID:        cfg.ActorID,
		IdempotencyKey: row.IdempotencyKey,
		TraceID:        cfg.TraceID,
		RawBody:        raw,
	})
	return err
}

func summarize(rows []importRow, dryRun bool) importSummary {
	summary := importSummary{Rows: len(rows), DryRun: dryRun}
	for _, row := range rows {
		switch row.Kind {
		case kindLocationAlias:
			summary.Aliases++
		case kindLocationCapacity:
			summary.Capacities++
		case kindLocationReviewItem:
			summary.Reviews++
		}
	}
	return summary
}

func mergeSourceRef(notes, sourceRef string) string {
	if sourceRef == "" {
		return notes
	}
	sourceNote := "source_ref=" + sourceRef
	if notes == "" {
		return sourceNote
	}
	return notes + "; " + sourceNote
}

func derivedIdempotency(kind string, raw []byte) string {
	return "location-profile-source-import:" + kind + ":" + stableHash(kind, raw)[7:39]
}

func stableHash(prefix string, raw []byte) string {
	sum := sha256.Sum256(append([]byte(prefix+":"), raw...))
	return "sha256:" + hex.EncodeToString(sum[:])
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

func getenvDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
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
