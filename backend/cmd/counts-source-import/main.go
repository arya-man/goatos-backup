// Command counts-source-import imports reviewed Counts/Shifting source rows into
// the canonical Counts service path. It intentionally accepts typed JSONL, not
// raw workbook formulas: spreadsheets are evidence, while GoatOS owns the
// validated runtime records consumed by Feed Direction.
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

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
)

const (
	kindBaseCountAnchor = "base_count_anchor"
	kindShiftingEvent   = "shifting_event"
	defaultSourceSystem = "import"
)

type config struct {
	File         string
	TenantID     string
	SourceSystem string
	RecordedBy   string
	DryRun       bool
	Timeout      time.Duration
}

type importRow struct {
	Kind               string
	BaseCountAnchor    *countsdomain.BaseCountAnchor
	ShiftingEvent      *countsdomain.ShiftingEvent
	SourceLine         int
	DerivedSourceHash  bool
	DerivedPayload     bool
	DerivedIdem        bool
	DerivedFingerprint bool
}

type baseCountAnchorJSON struct {
	Kind               string  `json:"kind"`
	TenantID           string  `json:"tenant_id"`
	ParkID             string  `json:"park_id"`
	ShedID             string  `json:"shed_id"`
	BreedID            *string `json:"breed_id"`
	BreedKey           string  `json:"breed_key"`
	BreedLabel         string  `json:"breed_label"`
	CountedAt          string  `json:"counted_at"`
	HeadCount          int32   `json:"head_count"`
	SourceSystem       string  `json:"source_system"`
	SourceRef          string  `json:"source_ref"`
	SourceHash         string  `json:"source_hash"`
	DiscrepancyState   string  `json:"discrepancy_state"`
	IdempotencyKey     string  `json:"idempotency_key"`
	RequestFingerprint string  `json:"request_fingerprint"`
	RecordedBy         *string `json:"recorded_by"`
}

type shiftingEventJSON struct {
	Kind                    string               `json:"kind"`
	TenantID                string               `json:"tenant_id"`
	LogicalShiftingEventKey string               `json:"logical_shifting_event_key"`
	Priority                string               `json:"priority"`
	Category                string               `json:"category"`
	SourceParkID            *string              `json:"source_park_id"`
	SourceShedID            *string              `json:"source_shed_id"`
	DestinationParkID       string               `json:"destination_park_id"`
	DestinationShedID       string               `json:"destination_shed_id"`
	RaisedAt                string               `json:"raised_at"`
	EffectiveAt             string               `json:"effective_at"`
	AuthorizedAt            *string              `json:"authorized_at"`
	AuthorizedBy            *string              `json:"authorized_by"`
	AuthorizationState      string               `json:"authorization_state"`
	VerificationState       string               `json:"verification_state"`
	EventStatus             string               `json:"event_status"`
	SourceSystem            string               `json:"source_system"`
	SourceRef               string               `json:"source_ref"`
	ProofRef                *string              `json:"proof_ref"`
	PayloadHash             string               `json:"payload_hash"`
	IdempotencyKey          string               `json:"idempotency_key"`
	RequestFingerprint      string               `json:"request_fingerprint"`
	Impacts                 []shiftingImpactJSON `json:"impacts"`
}

type shiftingImpactJSON struct {
	GrainKey                     string          `json:"grain_key"`
	BreedID                      *string         `json:"breed_id"`
	BreedKey                     string          `json:"breed_key"`
	BreedLabel                   string          `json:"breed_label"`
	StageTag                     *string         `json:"stage_tag"`
	AgeClass                     *string         `json:"age_class"`
	Sex                          *string         `json:"sex"`
	HeadCount                    int32           `json:"head_count"`
	PregnantCount                int32           `json:"pregnant_count"`
	LactatingCount               int32           `json:"lactating_count"`
	WarmupCount                  int32           `json:"warmup_count"`
	RiskFlagsJSON                json.RawMessage `json:"risk_flags"`
	RationContextResolutionState string          `json:"ration_context_resolution_state"`
	RationContextRef             *string         `json:"ration_context_ref"`
	BlockerReason                *string         `json:"blocker_reason"`
}

type rowEnvelope struct {
	Kind string `json:"kind"`
}

type importSummary struct {
	Rows           int
	BaseAnchors    int
	ShiftingEvents int
	Replayed       int
	DryRun         bool
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
		fmt.Fprintf(stdout, "counts source import dry-run ok rows=%d base_anchors=%d shifting_events=%d\n",
			summary.Rows, summary.BaseAnchors, summary.ShiftingEvents)
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

	service := countsapp.NewService(countspg.NewRepository(pool, pgCfg.QueryTimeout))
	summary, err := importRows(ctx, service, rows, stdout)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "counts source import complete rows=%d base_anchors=%d shifting_events=%d replays=%d\n",
		summary.Rows, summary.BaseAnchors, summary.ShiftingEvents, summary.Replayed)
	return nil
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("counts-source-import", flag.ContinueOnError)
	fs.StringVar(&cfg.File, "file", getenv("GOATOS_COUNTS_SOURCE_IMPORT_FILE"), "JSONL import file; default stdin")
	fs.StringVar(&cfg.TenantID, "tenant-id", getenv("GOATOS_TENANT_ID"), "default tenant id for rows that omit tenant_id")
	fs.StringVar(&cfg.SourceSystem, "source-system", getenvDefault("GOATOS_COUNTS_SOURCE_IMPORT_SOURCE_SYSTEM", defaultSourceSystem), "default source_system for rows that omit source_system")
	fs.StringVar(&cfg.RecordedBy, "recorded-by", getenv("GOATOS_COUNTS_SOURCE_IMPORT_RECORDED_BY"), "default recorded_by actor id for base-count rows")
	fs.BoolVar(&cfg.DryRun, "dry-run", boolEnv("GOATOS_COUNTS_SOURCE_IMPORT_DRY_RUN"), "validate rows without writing to Postgres")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_COUNTS_SOURCE_IMPORT_TIMEOUT", 120*time.Second), "import timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.File = strings.TrimSpace(cfg.File)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.SourceSystem = defaultString(strings.TrimSpace(cfg.SourceSystem), defaultSourceSystem)
	cfg.RecordedBy = strings.TrimSpace(cfg.RecordedBy)
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
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	rows := []importRow{}
	line := 0
	for scanner.Scan() {
		line++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		row, err := parseImportRow([]byte(raw), cfg, line)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func parseImportRow(raw []byte, cfg config, line int) (importRow, error) {
	var env rowEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return importRow{}, fmt.Errorf("line %d: invalid json: %w", line, err)
	}
	switch strings.ToLower(strings.TrimSpace(env.Kind)) {
	case kindBaseCountAnchor:
		anchor, meta, err := parseBaseCountAnchor(raw, cfg)
		if err != nil {
			return importRow{}, fmt.Errorf("line %d: %w", line, err)
		}
		meta.Kind = kindBaseCountAnchor
		meta.BaseCountAnchor = &anchor
		meta.SourceLine = line
		return meta, nil
	case kindShiftingEvent:
		event, meta, err := parseShiftingEvent(raw, cfg)
		if err != nil {
			return importRow{}, fmt.Errorf("line %d: %w", line, err)
		}
		meta.Kind = kindShiftingEvent
		meta.ShiftingEvent = &event
		meta.SourceLine = line
		return meta, nil
	default:
		return importRow{}, fmt.Errorf("line %d: kind must be %q or %q", line, kindBaseCountAnchor, kindShiftingEvent)
	}
}

func parseBaseCountAnchor(raw []byte, cfg config) (countsdomain.BaseCountAnchor, importRow, error) {
	var in baseCountAnchorJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return countsdomain.BaseCountAnchor{}, importRow{}, err
	}
	countedAt, err := parseDateOrInstant(in.CountedAt)
	if err != nil {
		return countsdomain.BaseCountAnchor{}, importRow{}, fmt.Errorf("counted_at: %w", err)
	}
	sourceSystem := strings.ToLower(defaultString(strings.TrimSpace(in.SourceSystem), defaultString(cfg.SourceSystem, defaultSourceSystem)))
	if !oneOf(sourceSystem, "physical_base_count", "manual_review", "import", "goatos_canonical") {
		return countsdomain.BaseCountAnchor{}, importRow{}, fmt.Errorf("source_system %q is not valid for base_count_anchor", sourceSystem)
	}
	recordedBy := trimOptional(in.RecordedBy)
	if recordedBy == nil {
		recordedBy = ptrIfNotEmpty(cfg.RecordedBy)
	}
	sourceHash := strings.TrimSpace(in.SourceHash)
	derivedSourceHash := false
	if sourceHash == "" {
		sourceHash = stableHash("counts-source-import:base-source", raw)
		derivedSourceHash = true
	}
	anchor := countsdomain.BaseCountAnchor{
		TenantID:           defaultString(strings.TrimSpace(in.TenantID), cfg.TenantID),
		ParkID:             strings.TrimSpace(in.ParkID),
		ShedID:             strings.TrimSpace(in.ShedID),
		BreedID:            trimOptional(in.BreedID),
		BreedKey:           strings.TrimSpace(in.BreedKey),
		BreedLabel:         defaultString(strings.TrimSpace(in.BreedLabel), strings.TrimSpace(in.BreedKey)),
		CountedAt:          countedAt,
		HeadCount:          in.HeadCount,
		SourceSystem:       sourceSystem,
		SourceRef:          strings.TrimSpace(in.SourceRef),
		SourceHash:         sourceHash,
		DiscrepancyState:   strings.ToLower(strings.TrimSpace(in.DiscrepancyState)),
		IdempotencyKey:     strings.TrimSpace(in.IdempotencyKey),
		RequestFingerprint: strings.TrimSpace(in.RequestFingerprint),
		RecordedBy:         recordedBy,
	}
	derivedIdem := false
	if anchor.IdempotencyKey == "" {
		anchor.IdempotencyKey = "counts-base-anchor-import:" + stableHash("idempotency", []byte(strings.Join([]string{
			anchor.TenantID, anchor.ParkID, anchor.ShedID, strings.ToLower(anchor.BreedKey),
			anchor.CountedAt.UTC().Format(time.RFC3339Nano), anchor.SourceSystem, anchor.SourceRef, anchor.SourceHash,
		}, "\x00")))
		derivedIdem = true
	}
	derivedFP := false
	if anchor.RequestFingerprint == "" {
		anchor.RequestFingerprint = stableHash("counts-base-anchor-request", mustJSON(anchor))
		derivedFP = true
	}
	return anchor, importRow{
		DerivedSourceHash:  derivedSourceHash,
		DerivedIdem:        derivedIdem,
		DerivedFingerprint: derivedFP,
	}, nil
}

func parseShiftingEvent(raw []byte, cfg config) (countsdomain.ShiftingEvent, importRow, error) {
	var in shiftingEventJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return countsdomain.ShiftingEvent{}, importRow{}, err
	}
	raisedAt, err := parseDateOrInstant(in.RaisedAt)
	if err != nil {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("raised_at: %w", err)
	}
	effectiveAt, err := parseDateOrInstant(in.EffectiveAt)
	if err != nil {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("effective_at: %w", err)
	}
	authorizedAt, err := parseOptionalInstant(in.AuthorizedAt)
	if err != nil {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("authorized_at: %w", err)
	}
	if strings.TrimSpace(in.AuthorizationState) == "" || strings.TrimSpace(in.EventStatus) == "" {
		return countsdomain.ShiftingEvent{}, importRow{}, errors.New("authorization_state and event_status are required for shifting imports")
	}
	sourceSystem := strings.ToLower(defaultString(strings.TrimSpace(in.SourceSystem), defaultString(cfg.SourceSystem, defaultSourceSystem)))
	if !oneOf(sourceSystem, "feed_shiftings_docx", "manual_review", "legacy_slack", "import", "goatos_canonical") {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("source_system %q is not valid for shifting_event", sourceSystem)
	}
	payloadHash := strings.TrimSpace(in.PayloadHash)
	derivedPayload := false
	if payloadHash == "" {
		payloadHash = stableHash("counts-source-import:shifting-payload", raw)
		derivedPayload = true
	}
	impacts, err := parseImpacts(in.DestinationShedID, in.Impacts)
	if err != nil {
		return countsdomain.ShiftingEvent{}, importRow{}, err
	}
	event := countsdomain.ShiftingEvent{
		TenantID:                defaultString(strings.TrimSpace(in.TenantID), cfg.TenantID),
		LogicalShiftingEventKey: strings.TrimSpace(in.LogicalShiftingEventKey),
		Priority:                strings.ToLower(strings.TrimSpace(in.Priority)),
		Category:                strings.ToLower(defaultString(strings.TrimSpace(in.Category), inferredCategory(impacts))),
		SourceParkID:            trimOptional(in.SourceParkID),
		SourceShedID:            trimOptional(in.SourceShedID),
		DestinationParkID:       strings.TrimSpace(in.DestinationParkID),
		DestinationShedID:       strings.TrimSpace(in.DestinationShedID),
		RaisedAt:                raisedAt,
		EffectiveAt:             effectiveAt,
		AuthorizedAt:            authorizedAt,
		AuthorizedBy:            trimOptional(in.AuthorizedBy),
		AuthorizationState:      strings.ToLower(strings.TrimSpace(in.AuthorizationState)),
		VerificationState:       strings.ToLower(strings.TrimSpace(in.VerificationState)),
		EventStatus:             strings.ToLower(strings.TrimSpace(in.EventStatus)),
		SourceSystem:            sourceSystem,
		SourceRef:               strings.TrimSpace(in.SourceRef),
		ProofRef:                trimOptional(in.ProofRef),
		PayloadHash:             payloadHash,
		IdempotencyKey:          strings.TrimSpace(in.IdempotencyKey),
		RequestFingerprint:      strings.TrimSpace(in.RequestFingerprint),
		Impacts:                 impacts,
	}
	derivedIdem := false
	if event.IdempotencyKey == "" {
		event.IdempotencyKey = "counts-shifting-import:" + stableHash("idempotency", []byte(strings.Join([]string{
			event.TenantID, event.LogicalShiftingEventKey, event.SourceSystem, event.SourceRef, event.PayloadHash,
		}, "\x00")))
		derivedIdem = true
	}
	derivedFP := false
	if event.RequestFingerprint == "" {
		event.RequestFingerprint = stableHash("counts-shifting-request", mustJSON(event))
		derivedFP = true
	}
	if event.Priority != "" && !oneOf(event.Priority, "normal", "high", "emergency") {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("priority %q is invalid", event.Priority)
	}
	if event.Category != "" && !oneOf(event.Category, "routine", "high_priority", "pregnancy", "warmup", "medical", "quarantine", "other") {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("category %q is invalid", event.Category)
	}
	if !oneOf(event.AuthorizationState, "pending", "authorized", "rejected") {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("authorization_state %q is invalid", event.AuthorizationState)
	}
	if event.VerificationState != "" && !oneOf(event.VerificationState, "unverified", "verified", "rejected") {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("verification_state %q is invalid", event.VerificationState)
	}
	if !oneOf(event.EventStatus, "pending", "authorized", "applied", "rejected", "canceled", "unresolved") {
		return countsdomain.ShiftingEvent{}, importRow{}, fmt.Errorf("event_status %q is invalid", event.EventStatus)
	}
	return event, importRow{
		DerivedPayload:     derivedPayload,
		DerivedIdem:        derivedIdem,
		DerivedFingerprint: derivedFP,
	}, nil
}

func parseImpacts(destinationShedID string, in []shiftingImpactJSON) ([]countsdomain.ShiftingEventImpact, error) {
	if len(in) == 0 {
		return nil, errors.New("impacts are required for shifting imports")
	}
	out := make([]countsdomain.ShiftingEventImpact, 0, len(in))
	for i, item := range in {
		riskFlags := item.RiskFlagsJSON
		if len(riskFlags) == 0 {
			riskFlags = []byte("{}")
		}
		if !isJSONObject(riskFlags) {
			return nil, fmt.Errorf("impacts[%d].risk_flags must be a json object", i)
		}
		breedKey := strings.TrimSpace(item.BreedKey)
		grainKey := strings.TrimSpace(item.GrainKey)
		if grainKey == "" {
			grainKey = strings.ToLower(strings.TrimSpace(destinationShedID)) + ":" + strings.ToLower(breedKey)
		}
		out = append(out, countsdomain.ShiftingEventImpact{
			GrainKey:                     grainKey,
			BreedID:                      trimOptional(item.BreedID),
			BreedKey:                     breedKey,
			BreedLabel:                   defaultString(strings.TrimSpace(item.BreedLabel), breedKey),
			StageTag:                     trimOptional(item.StageTag),
			AgeClass:                     trimOptional(item.AgeClass),
			Sex:                          trimOptional(item.Sex),
			HeadCount:                    item.HeadCount,
			PregnantCount:                item.PregnantCount,
			LactatingCount:               item.LactatingCount,
			WarmupCount:                  item.WarmupCount,
			RiskFlagsJSON:                riskFlags,
			RationContextResolutionState: strings.TrimSpace(item.RationContextResolutionState),
			RationContextRef:             trimOptional(item.RationContextRef),
			BlockerReason:                trimOptional(item.BlockerReason),
		})
	}
	return out, nil
}

type countsService interface {
	RecordBaseCountAnchor(context.Context, countsdomain.BaseCountAnchor) (string, bool, error)
	RecordShiftingEvent(context.Context, countsdomain.ShiftingEvent) (string, bool, error)
}

func importRows(ctx context.Context, service countsService, rows []importRow, stdout io.Writer) (importSummary, error) {
	var summary importSummary
	for _, row := range rows {
		summary.Rows++
		switch row.Kind {
		case kindBaseCountAnchor:
			id, replay, err := service.RecordBaseCountAnchor(ctx, *row.BaseCountAnchor)
			if err != nil {
				return summary, fmt.Errorf("line %d base_count_anchor import: %w", row.SourceLine, err)
			}
			summary.BaseAnchors++
			if replay {
				summary.Replayed++
			}
			fmt.Fprintf(stdout, "imported line=%d kind=base_count_anchor id=%s replay=%t\n", row.SourceLine, id, replay)
		case kindShiftingEvent:
			id, replay, err := service.RecordShiftingEvent(ctx, *row.ShiftingEvent)
			if err != nil {
				return summary, fmt.Errorf("line %d shifting_event import: %w", row.SourceLine, err)
			}
			summary.ShiftingEvents++
			if replay {
				summary.Replayed++
			}
			fmt.Fprintf(stdout, "imported line=%d kind=shifting_event id=%s replay=%t\n", row.SourceLine, id, replay)
		default:
			return summary, fmt.Errorf("line %d unsupported kind %q", row.SourceLine, row.Kind)
		}
	}
	return summary, nil
}

func summarize(rows []importRow, dryRun bool) importSummary {
	out := importSummary{Rows: len(rows), DryRun: dryRun}
	for _, row := range rows {
		switch row.Kind {
		case kindBaseCountAnchor:
			out.BaseAnchors++
		case kindShiftingEvent:
			out.ShiftingEvents++
		}
	}
	return out
}

func parseDateOrInstant(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("timestamp is required")
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("must be YYYY-MM-DD or RFC3339")
	}
	return t.UTC(), nil
}

func parseOptionalInstant(raw *string) (*time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, nil
	}
	parsed, err := parseDateOrInstant(*raw)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func inferredCategory(impacts []countsdomain.ShiftingEventImpact) string {
	for _, impact := range impacts {
		if impact.PregnantCount > 0 {
			return "pregnancy"
		}
	}
	for _, impact := range impacts {
		if impact.WarmupCount > 0 {
			return "warmup"
		}
	}
	return "routine"
}

func isJSONObject(raw []byte) bool {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	_, ok := v.(map[string]any)
	return ok
}

func stableHash(prefix string, raw []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(prefix))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

func mustJSON(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func trimOptional(v *string) *string {
	if v == nil {
		return nil
	}
	return ptrIfNotEmpty(*v)
}

func ptrIfNotEmpty(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
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

func getenvDefault(key, fallback string) string {
	if value := getenv(key); value != "" {
		return value
	}
	return fallback
}

func boolEnv(key string) bool {
	value := strings.ToLower(getenv(key))
	return value == "1" || value == "true" || value == "yes"
}

func oneOf(value string, allowed ...string) bool {
	for _, item := range allowed {
		if value == item {
			return true
		}
	}
	return false
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
