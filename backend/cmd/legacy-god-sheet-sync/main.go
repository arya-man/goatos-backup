// Command legacy-god-sheet-sync bootstraps and refreshes the migration god
// sheet used to clean legacy Sheets/BigQuery data before any Goat OS import.
//
// It writes only the target god-sheet workbook. It never writes to legacy source
// sheets and never imports rows into Goat OS Postgres.
package main

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const (
	defaultSpreadsheetID                  = "1QhW22Awg7WKGhYf7tCaj_pXE-9kwzPyfwhSwenDW2RM"
	defaultTimeout                        = 120 * time.Second
	defaultRunType                        = "manual_bootstrap"
	defaultSheetURL                       = "https://docs.google.com/spreadsheets/d/1QhW22Awg7WKGhYf7tCaj_pXE-9kwzPyfwhSwenDW2RM/edit"
	defaultAllowedGooglePrincipalSuffixes = "@mesha.sg,@goatos-sheets.iam.gserviceaccount.com,@goatos-dev.iam.gserviceaccount.com,@goatos-stg.iam.gserviceaccount.com,@goatos-prod.iam.gserviceaccount.com"
)

type config struct {
	SpreadsheetID                  string
	RunType                        string
	BusinessDate                   string
	Actor                          string
	ExpectedGooglePrincipal        string
	AllowedGooglePrincipalSuffixes string
	SkipADCIdentityGuard           bool
	Apply                          bool
	ReplaceManagedTabs             bool
	OutputJSON                     bool
	Timeout                        time.Duration
}

type tabSpec struct {
	Name string
	Cols []string
	Rows [][]string
}

type sourceCatalogRow struct {
	SourceID                string `json:"source_id"`
	SourceFamily            string `json:"source_family"`
	SourceSystem            string `json:"source_system"`
	Dataset                 string `json:"dataset"`
	TableName               string `json:"table_name"`
	SheetURL                string `json:"sheet_url"`
	TabName                 string `json:"tab_name"`
	Grain                   string `json:"grain"`
	PrimaryKeys             string `json:"primary_keys"`
	Owner                   string `json:"owner"`
	Extractor               string `json:"extractor"`
	FreshnessSLA            string `json:"freshness_sla"`
	WatermarkField          string `json:"watermark_field"`
	LastSuccessfulWatermark string `json:"last_successful_watermark"`
	LastSchemaHash          string `json:"last_schema_hash"`
	LastRowCount            string `json:"last_row_count"`
	Notes                   string `json:"notes"`
}

type validationRuleRow struct {
	RuleID          string `json:"rule_id"`
	RuleName        string `json:"rule_name"`
	Severity        string `json:"severity"`
	Blocking        string `json:"blocking"`
	Owner           string `json:"owner"`
	SourceFamily    string `json:"source_family"`
	Description     string `json:"description"`
	CheckType       string `json:"check_type"`
	SQLOrFormulaRef string `json:"sql_or_formula_ref"`
	ExpectedResult  string `json:"expected_result"`
	FailureMessage  string `json:"failure_message"`
	LastRunID       string `json:"last_run_id"`
	LastStatus      string `json:"last_status"`
}

type issueSeedRow struct {
	IssueID           string `json:"issue_id"`
	IssueType         string `json:"issue_type"`
	Severity          string `json:"severity"`
	Blocking          string `json:"blocking"`
	AnimalIdentifier1 string `json:"animal_identifier_1"`
	AnimalIdentifier2 string `json:"animal_identifier_2"`
	LoadID            string `json:"load_id"`
	SourceID          string `json:"source_id"`
	SourceRecordID    string `json:"source_record_id"`
	Evidence          string `json:"evidence"`
	Owner             string `json:"owner"`
	NextAction        string `json:"next_action"`
	SLADate           string `json:"sla_date"`
	Status            string `json:"status"`
	ResolvedBy        string `json:"resolved_by"`
	ResolvedAt        string `json:"resolved_at"`
	ResolutionNote    string `json:"resolution_note"`
}

type syncSummary struct {
	RunID              string   `json:"run_id"`
	SpreadsheetID      string   `json:"spreadsheet_id"`
	BusinessDate       string   `json:"business_date"`
	RunType            string   `json:"run_type"`
	Apply              bool     `json:"apply"`
	ReplaceManagedTabs bool     `json:"replace_managed_tabs"`
	Tabs               int      `json:"tabs"`
	SourceCatalogRows  int      `json:"source_catalog_rows"`
	ValidationRules    int      `json:"validation_rules"`
	IssueSeeds         int      `json:"issue_seeds"`
	CreatedTabs        []string `json:"created_tabs,omitempty"`
	WrittenTabs        []string `json:"written_tabs,omitempty"`
	SkippedTabs        []string `json:"skipped_tabs,omitempty"`
	AppendedRows       []string `json:"appended_rows,omitempty"`
}

type sheetsPort interface {
	GetSpreadsheet(ctx context.Context, spreadsheetID string) (*sheets.Spreadsheet, error)
	BatchUpdateSpreadsheet(ctx context.Context, spreadsheetID string, request *sheets.BatchUpdateSpreadsheetRequest) (*sheets.BatchUpdateSpreadsheetResponse, error)
	GetValues(ctx context.Context, spreadsheetID string, readRange string) (*sheets.ValueRange, error)
	ClearValues(ctx context.Context, spreadsheetID string, writeRange string) (*sheets.ClearValuesResponse, error)
	UpdateValues(ctx context.Context, spreadsheetID string, writeRange string, values *sheets.ValueRange) (*sheets.UpdateValuesResponse, error)
	AppendValues(ctx context.Context, spreadsheetID string, writeRange string, values *sheets.ValueRange) (*sheets.AppendValuesResponse, error)
}

type googleSheetsPort struct {
	service *sheets.Service
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
	runID := newRunID()
	sourceRows := sourceCatalogRows()
	ruleRows := validationRuleRows()
	issueRows := issueSeedRows()
	specs := workbookTabs(cfg, runID, sourceRows, ruleRows, issueRows)
	summary := syncSummary{
		RunID:              runID,
		SpreadsheetID:      cfg.SpreadsheetID,
		BusinessDate:       cfg.BusinessDate,
		RunType:            cfg.RunType,
		Apply:              cfg.Apply,
		ReplaceManagedTabs: cfg.ReplaceManagedTabs,
		Tabs:               len(specs),
		SourceCatalogRows:  len(sourceRows),
		ValidationRules:    len(ruleRows),
		IssueSeeds:         len(issueRows),
	}
	if !cfg.Apply {
		return printSummary(stdout, summary, cfg.OutputJSON, "legacy god sheet sync dry-run")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	service, err := newSheetsClient(ctx, cfg)
	if err != nil {
		return err
	}
	applied, err := applyWorkbook(ctx, service, cfg, specs)
	if err != nil {
		return err
	}
	summary.CreatedTabs = applied.CreatedTabs
	summary.WrittenTabs = applied.WrittenTabs
	summary.SkippedTabs = applied.SkippedTabs
	summary.AppendedRows = applied.AppendedRows
	return printSummary(stdout, summary, cfg.OutputJSON, "legacy god sheet sync applied")
}

func parseFlags(args []string) (config, error) {
	cfg := config{}
	fs := flag.NewFlagSet("legacy-god-sheet-sync", flag.ContinueOnError)
	fs.StringVar(&cfg.SpreadsheetID, "spreadsheet-id", getenvDefault("GOATOS_LEGACY_GOD_SHEET_ID", defaultSpreadsheetID), "target god-sheet spreadsheet id")
	fs.StringVar(&cfg.RunType, "run-type", getenvDefault("GOATOS_LEGACY_GOD_SHEET_RUN_TYPE", defaultRunType), "sync run type")
	fs.StringVar(&cfg.BusinessDate, "business-date", getenv("GOATOS_LEGACY_GOD_SHEET_BUSINESS_DATE"), "explicit business date YYYY-MM-DD; default yesterday IST")
	fs.StringVar(&cfg.Actor, "actor", getenvDefault("GOATOS_LEGACY_GOD_SHEET_ACTOR", "codex"), "actor/source writing the run row")
	fs.StringVar(&cfg.ExpectedGooglePrincipal, "expected-google-principal", getenv("GOATOS_LEGACY_GOD_SHEET_EXPECTED_GOOGLE_PRINCIPAL"), "exact Google ADC principal allowed to write")
	fs.StringVar(&cfg.AllowedGooglePrincipalSuffixes, "allowed-google-principal-suffixes", getenvDefault("GOATOS_LEGACY_GOD_SHEET_ALLOWED_GOOGLE_PRINCIPAL_SUFFIXES", defaultAllowedGooglePrincipalSuffixes), "comma-separated Google ADC principal suffix allowlist")
	fs.BoolVar(&cfg.SkipADCIdentityGuard, "skip-adc-identity-guard", boolEnv("GOATOS_LEGACY_GOD_SHEET_SKIP_ADC_IDENTITY_GUARD"), "disable Google ADC principal guard; emergency use only")
	fs.BoolVar(&cfg.Apply, "apply", boolEnv("GOATOS_LEGACY_GOD_SHEET_APPLY"), "write to Google Sheets; default is dry-run only")
	fs.BoolVar(&cfg.ReplaceManagedTabs, "replace-managed-tabs", boolEnv("GOATOS_LEGACY_GOD_SHEET_REPLACE_MANAGED_TABS"), "refresh seed/config tabs only; human/data tabs are always preserved")
	fs.BoolVar(&cfg.OutputJSON, "json", boolEnv("GOATOS_LEGACY_GOD_SHEET_JSON"), "print machine-readable summary")
	fs.DurationVar(&cfg.Timeout, "timeout", durationEnv("GOATOS_LEGACY_GOD_SHEET_TIMEOUT", defaultTimeout), "sync timeout")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	cfg.SpreadsheetID = strings.TrimSpace(cfg.SpreadsheetID)
	cfg.RunType = defaultString(strings.TrimSpace(cfg.RunType), defaultRunType)
	cfg.Actor = defaultString(strings.TrimSpace(cfg.Actor), "codex")
	cfg.BusinessDate = strings.TrimSpace(cfg.BusinessDate)
	cfg.ExpectedGooglePrincipal = strings.TrimSpace(cfg.ExpectedGooglePrincipal)
	cfg.AllowedGooglePrincipalSuffixes = strings.TrimSpace(cfg.AllowedGooglePrincipalSuffixes)
	if cfg.SpreadsheetID == "" {
		return config{}, errors.New("spreadsheet-id is required")
	}
	if cfg.BusinessDate == "" {
		cfg.BusinessDate = defaultBusinessDate(time.Now())
	}
	if _, err := time.Parse("2006-01-02", cfg.BusinessDate); err != nil {
		return config{}, errors.New("business-date must be YYYY-MM-DD")
	}
	if cfg.Timeout <= 0 {
		return config{}, errors.New("timeout must be positive")
	}
	return cfg, nil
}

type applySummary struct {
	CreatedTabs  []string
	WrittenTabs  []string
	SkippedTabs  []string
	AppendedRows []string
}

func newSheetsClient(ctx context.Context, cfg config) (sheetsPort, error) {
	creds, err := google.FindDefaultCredentials(ctx, sheets.SpreadsheetsScope)
	if err != nil {
		return nil, fmt.Errorf("find Google application default credentials with Sheets scope: %w", err)
	}
	if err := assertGoogleIdentity(ctx, creds, cfg); err != nil {
		return nil, err
	}
	service, err := sheets.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, err
	}
	return googleSheetsPort{service: service}, nil
}

func (p googleSheetsPort) GetSpreadsheet(ctx context.Context, spreadsheetID string) (*sheets.Spreadsheet, error) {
	return withSheetsRetry(ctx, func() (*sheets.Spreadsheet, error) {
		return p.service.Spreadsheets.Get(spreadsheetID).Context(ctx).Do()
	})
}

func (p googleSheetsPort) BatchUpdateSpreadsheet(ctx context.Context, spreadsheetID string, request *sheets.BatchUpdateSpreadsheetRequest) (*sheets.BatchUpdateSpreadsheetResponse, error) {
	return withSheetsRetry(ctx, func() (*sheets.BatchUpdateSpreadsheetResponse, error) {
		return p.service.Spreadsheets.BatchUpdate(spreadsheetID, request).Context(ctx).Do()
	})
}

func (p googleSheetsPort) GetValues(ctx context.Context, spreadsheetID string, readRange string) (*sheets.ValueRange, error) {
	return withSheetsRetry(ctx, func() (*sheets.ValueRange, error) {
		return p.service.Spreadsheets.Values.Get(spreadsheetID, readRange).Context(ctx).Do()
	})
}

func (p googleSheetsPort) ClearValues(ctx context.Context, spreadsheetID string, writeRange string) (*sheets.ClearValuesResponse, error) {
	return withSheetsRetry(ctx, func() (*sheets.ClearValuesResponse, error) {
		return p.service.Spreadsheets.Values.Clear(spreadsheetID, writeRange, &sheets.ClearValuesRequest{}).Context(ctx).Do()
	})
}

func (p googleSheetsPort) UpdateValues(ctx context.Context, spreadsheetID string, writeRange string, values *sheets.ValueRange) (*sheets.UpdateValuesResponse, error) {
	return withSheetsRetry(ctx, func() (*sheets.UpdateValuesResponse, error) {
		return p.service.Spreadsheets.Values.Update(spreadsheetID, writeRange, values).ValueInputOption("RAW").Context(ctx).Do()
	})
}

func (p googleSheetsPort) AppendValues(ctx context.Context, spreadsheetID string, writeRange string, values *sheets.ValueRange) (*sheets.AppendValuesResponse, error) {
	return withSheetsRetry(ctx, func() (*sheets.AppendValuesResponse, error) {
		return p.service.Spreadsheets.Values.Append(spreadsheetID, writeRange, values).ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	})
}

func applyWorkbook(ctx context.Context, service sheetsPort, cfg config, specs []tabSpec) (applySummary, error) {
	spreadsheet, err := service.GetSpreadsheet(ctx, cfg.SpreadsheetID)
	if err != nil {
		return applySummary{}, fmt.Errorf("read spreadsheet metadata: %w", err)
	}
	existing := map[string]int64{}
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties != nil {
			existing[sheet.Properties.Title] = sheet.Properties.SheetId
		}
	}
	summary := applySummary{}
	requests := []*sheets.Request{}
	for _, spec := range specs {
		if _, ok := existing[spec.Name]; ok {
			continue
		}
		requests = append(requests, &sheets.Request{AddSheet: &sheets.AddSheetRequest{
			Properties: &sheets.SheetProperties{
				Title: spec.Name,
				GridProperties: &sheets.GridProperties{
					FrozenRowCount: 1,
				},
			},
		}})
		summary.CreatedTabs = append(summary.CreatedTabs, spec.Name)
	}
	if len(requests) > 0 {
		resp, err := service.BatchUpdateSpreadsheet(ctx, cfg.SpreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{Requests: requests})
		if err != nil {
			return applySummary{}, fmt.Errorf("create missing tabs: %w", err)
		}
		for _, reply := range resp.Replies {
			if reply.AddSheet != nil && reply.AddSheet.Properties != nil {
				existing[reply.AddSheet.Properties.Title] = reply.AddSheet.Properties.SheetId
			}
		}
		sort.Strings(summary.CreatedTabs)
	}

	for _, spec := range specs {
		state, err := readTabState(ctx, service, cfg.SpreadsheetID, spec)
		if err != nil {
			return applySummary{}, err
		}
		if state.Populated && !state.HeaderMatches {
			if cfg.ReplaceManagedTabs && isReplaceableSeedTab(spec.Name) {
				if err := clearTab(ctx, service, cfg.SpreadsheetID, spec.Name); err != nil {
					return applySummary{}, err
				}
				if err := writeTab(ctx, service, cfg.SpreadsheetID, spec); err != nil {
					return applySummary{}, err
				}
				summary.WrittenTabs = append(summary.WrittenTabs, spec.Name)
				continue
			}
			return applySummary{}, fmt.Errorf("%s has data but its header does not match the managed schema; fix the header or refresh the seed/config tab explicitly", spec.Name)
		}
		if state.Populated {
			if spec.Name == "Sync_Runs" {
				if err := appendSyncRun(ctx, service, cfg.SpreadsheetID, spec); err != nil {
					return applySummary{}, err
				}
				summary.AppendedRows = append(summary.AppendedRows, spec.Name)
				continue
			}
			if cfg.ReplaceManagedTabs && isReplaceableSeedTab(spec.Name) {
				if err := clearTab(ctx, service, cfg.SpreadsheetID, spec.Name); err != nil {
					return applySummary{}, err
				}
				if err := writeTab(ctx, service, cfg.SpreadsheetID, spec); err != nil {
					return applySummary{}, err
				}
				summary.WrittenTabs = append(summary.WrittenTabs, spec.Name)
				continue
			}
			summary.SkippedTabs = append(summary.SkippedTabs, spec.Name)
			continue
		}
		if err := writeTab(ctx, service, cfg.SpreadsheetID, spec); err != nil {
			return applySummary{}, err
		}
		summary.WrittenTabs = append(summary.WrittenTabs, spec.Name)
	}
	sort.Strings(summary.WrittenTabs)
	sort.Strings(summary.SkippedTabs)
	sort.Strings(summary.AppendedRows)
	return summary, nil
}

type tabState struct {
	Populated     bool
	HeaderMatches bool
}

func readTabState(ctx context.Context, service sheetsPort, spreadsheetID string, spec tabSpec) (tabState, error) {
	resp, err := service.GetValues(ctx, spreadsheetID, quoteSheet(spec.Name)+"!1:2")
	if err != nil {
		return tabState{}, fmt.Errorf("read %s: %w", spec.Name, err)
	}
	state := tabState{HeaderMatches: len(spec.Cols) == 0}
	for _, row := range resp.Values {
		for _, cell := range row {
			if strings.TrimSpace(fmt.Sprint(cell)) != "" {
				state.Populated = true
				break
			}
		}
		if state.Populated {
			break
		}
	}
	if len(resp.Values) > 0 {
		state.HeaderMatches = headerMatches(resp.Values[0], spec.Cols)
	}
	return state, nil
}

func headerMatches(row []any, cols []string) bool {
	if len(row) < len(cols) {
		return false
	}
	for i, col := range cols {
		if strings.TrimSpace(fmt.Sprint(row[i])) != col {
			return false
		}
	}
	return true
}

func clearTab(ctx context.Context, service sheetsPort, spreadsheetID string, tab string) error {
	if _, err := service.ClearValues(ctx, spreadsheetID, quoteSheet(tab)); err != nil {
		return fmt.Errorf("clear %s: %w", tab, err)
	}
	return nil
}

func writeTab(ctx context.Context, service sheetsPort, spreadsheetID string, spec tabSpec) error {
	values := tabValues(spec)
	_, err := service.UpdateValues(ctx, spreadsheetID, quoteSheet(spec.Name)+"!A1", &sheets.ValueRange{
		Values: values,
	})
	if err != nil {
		return fmt.Errorf("write %s: %w", spec.Name, err)
	}
	return nil
}

func appendSyncRun(ctx context.Context, service sheetsPort, spreadsheetID string, spec tabSpec) error {
	values := tabValues(spec)
	if len(values) <= 1 {
		return nil
	}
	_, err := service.AppendValues(ctx, spreadsheetID, quoteSheet(spec.Name)+"!A1", &sheets.ValueRange{
		Values: values[1:],
	})
	if err != nil {
		return fmt.Errorf("append %s: %w", spec.Name, err)
	}
	return nil
}

func isReplaceableSeedTab(name string) bool {
	switch name {
	case "README_Problem_Statement", "Source_Catalog", "Validation_Rules", "Species_Taxonomy_Crosswalk":
		return true
	default:
		return false
	}
}

func tabValues(spec tabSpec) [][]any {
	values := make([][]any, 0, len(spec.Rows)+1)
	header := make([]any, len(spec.Cols))
	for i, col := range spec.Cols {
		header[i] = col
	}
	values = append(values, header)
	for _, row := range spec.Rows {
		out := make([]any, len(spec.Cols))
		for i := range out {
			if i < len(row) {
				out[i] = row[i]
			} else {
				out[i] = ""
			}
		}
		values = append(values, out)
	}
	return values
}

func workbookTabs(cfg config, runID string, sources []sourceCatalogRow, rules []validationRuleRow, issues []issueSeedRow) []tabSpec {
	specs := []tabSpec{
		{
			Name: "README_Problem_Statement",
			Cols: []string{"section", "content"},
			Rows: [][]string{
				{"purpose", "Temporary migration and reconciliation god sheet for cleaning legacy animal, vaccination, procurement, shifting, mortality, sales, feed, health, weight, dashboard, and audit data before Goat OS import."},
				{"red", "RED rows block Goat OS import until missing evidence, source mismatch, stale data, or identity conflict is resolved."},
				{"amber", "AMBER rows are usable only under an explicit temporary rule, such as animal_identifier_2 being optional until double RFID rollout is complete."},
				{"green", "GREEN rows have required Goat OS fields, evidence refs, unique RFID/tag identifier, resolved current status/location, and no open blocking issue."},
				{"runtime_warning", "This workbook is migration staging only. Long-term writes go through Goat OS backend APIs, not direct Sheet-to-Postgres imports."},
				{"rfid_gate", "animal_identifier_1 is the primary RFID/tag and is required. goat_id, farm_goat_id, inp_goat_id, and dst_tag do not satisfy it. Reused fallen/retired RFID tags are RED dirty data."},
			},
		},
		tableSpec("Source_Catalog", sourceCatalogColumns(), sourceCatalogValues(sources)),
		tableSpec("Sync_Runs", syncRunColumns(), nil),
		tableSpec("Raw_Source_Snapshots", []string{"run_id", "source_id", "business_date", "source_grain", "source_row_id", "source_row_key", "source_row_hash", "raw_payload_json", "read_at", "snapshot_created_at"}, [][]string{manifestSnapshotRow(runID, cfg.BusinessDate, sources, rules, issues)}),
		tableSpec("Mapping_Crosswalks", []string{"crosswalk_id", "source_id", "source_record_id", "raw_identifier_value", "normalized_identifier_value", "identifier_type_candidate", "animal_identifier_1", "animal_identifier_2", "goat_os_animal_id", "match_status", "match_confidence", "duplicate_group_id", "merge_blocker", "chosen_canonical_reason", "last_reviewed_by", "last_reviewed_at"}, nil),
		tableSpec("Animal_Master", []string{"goat_os_animal_id", "animal_identifier_1", "animal_identifier_2", "legacy_ids", "species", "sex", "breed", "dob", "dob_estimated", "age_class", "origin_type", "entry_date", "current_status", "current_farm", "current_park", "current_shed", "current_stage", "management_stage", "health_status", "reproductive_status", "mother_identifier", "sire_or_lot", "current_weight_kg", "photo_url", "dob_estimation_method", "sex_source", "source_record_id", "source_links", "evidence_refs", "verification_status", "issue_reason", "ground_owner", "last_verified_at", "ready_for_goat_os_import"}, nil),
		tableSpec("Animal_Identifier_History", []string{"animal_identifier", "identifier_type", "animal_identifier_1", "animal_identifier_2", "goat_os_animal_id", "status", "valid_from", "valid_to", "source_system", "source_record_id", "reason", "approved_by", "review_status"}, nil),
		tableSpec("Identity_Grain_Audit", []string{"source_id", "event_type", "id_column", "distinct_count", "coalesced_definition", "canonical_candidate", "delta_vs_procurement", "example_duplicate_group_ids", "decision_owner", "decision_status", "decision_note"}, identitySeedRows()),
		tableSpec("Location_Profile", []string{"location_key", "farm", "park_code", "shed_code", "shed_name", "shed_tag", "stage_profile", "capacity", "species_allowed", "sex_grouping", "is_holding", "is_quarantine", "is_icu", "usable_for_vaccination", "source_link", "verification_status"}, nil),
		tableSpec("Species_Taxonomy_Crosswalk", []string{"source_system", "source_field", "raw_value", "normalized_value", "canonical_species", "confidence", "mapping_rule", "approved_by", "approved_at", "source_link", "verification_status"}, speciesSeedRows()),
		tableSpec("Current_Location_Status", []string{"animal_identifier_1", "animal_identifier_2", "current_farm", "current_park", "current_shed", "current_stage", "source_priority_used", "latest_event_farm", "latest_shifting_dst_shed", "latest_shifting_dst_tag", "fallback_shed_evidence", "counting_db_status", "goats_db_status", "shiftings_status", "procurement_status", "resolved_status", "resolution_reason", "verification_status"}, nil),
		tableSpec("Birth_Events", []string{"event_id", "kid_identifier", "mother_identifier", "birth_date", "farm", "shed", "breed", "kid_status", "source_system", "source_record_id", "source_link", "evidence_refs", "verification_status"}, nil),
		tableSpec("Breeding_Delivery_History", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "event_type", "event_date", "farm", "shed", "mate_or_sire_identifier", "delivery_outcome", "medicine_or_protocol", "source_system", "source_record_id", "source_link", "verification_status"}, nil),
		tableSpec("Death_Events", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "death_date", "cause", "farm", "source_system", "source_record_id", "source_link", "verified_by", "verification_status"}, nil),
		tableSpec("Sale_Events", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "sale_date", "farm", "buyer_or_vendor", "sale_amount", "sale_weight_kg", "sales_id", "source_record_id", "source_link", "verification_status"}, nil),
		tableSpec("Procurement_Source_Entry", []string{"load_id", "source_party", "source_location", "animal_identifier_1", "animal_identifier_2", "species", "sex", "breed", "purchase_date", "loaded_at", "arrived_at", "accepted_intake_at", "source_entry_state", "current_state", "ownership_state", "health_state", "warmup_started_at", "warmup_ended_at", "holding_location", "proof_refs", "verification_status"}, nil),
		tableSpec("Procurement_Load_Reconciliation", []string{"load_id", "procurement_db_count", "goats_db_purchase_distinct_farm_goat_id", "goats_db_purchase_distinct_goat_id", "goats_db_purchase_distinct_inp_goat_id", "goats_db_purchase_distinct_coalesced", "loadwise_status_rows", "loadwise_summary_accounted_count", "delta_by_canonical_id", "unmatched_procurement_ids", "unmatched_goats_db_ids", "owner", "next_action", "verification_status"}, nil),
		tableSpec("Movement_Stage_History", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "from_farm", "to_farm", "from_shed", "to_shed", "from_stage", "to_stage", "shift_date", "stage_entry_date", "days_in_stage", "source_system", "source_record_id", "source_link", "verification_status"}, nil),
		tableSpec("Fattening_Shifting_Reconciliation", []string{"farm", "stage", "fattening_source_count", "shiftings_current_stage_count", "delta", "candidate_animal_ids", "candidate_load_ids", "source_decision", "owner", "next_action", "verification_status"}, nil),
		tableSpec("Weight_History", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "weight_date", "weight_kg", "farm", "shed", "stage", "source_system", "source_record_id", "source_link", "verification_status"}, nil),
		tableSpec("Health_Treatment_History", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "problem", "diagnosis", "treatment", "medicine", "dose", "follow_up_date", "vet_or_staff", "source_system", "source_record_id", "source_link", "verification_status"}, nil),
		tableSpec("Milk_Lactation_History", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "event_type", "event_date", "farm", "shed", "lactation_status", "milk_quantity_l", "feeding_quantity_l", "kid_identifier", "source_system", "source_record_id", "source_link", "verification_status"}, nil),
		tableSpec("Vaccination_History", []string{"event_id", "animal_identifier_1", "animal_identifier_2", "goat_os_animal_id", "protocol_version_id", "rule_id", "vaccine_code", "vaccine_name", "dose_code", "dose_number", "administered_at", "dose_ml", "batch_or_vial_lot", "administered_by", "farm_at_time", "park_at_time", "shed_at_time", "proof_link", "source_context", "trust_status", "review_status", "suppresses_due", "source_record_id", "verification_status"}, nil),
		tableSpec("Vaccination_Due_View", []string{"animal_identifier_1", "animal_identifier_2", "goat_os_animal_id", "species", "breed", "sex", "dob", "age_days", "farm", "park", "shed", "stage", "vaccine_code", "vaccine_name", "dose_code", "last_trusted_dose_date", "next_due_date", "latest_safe_date", "due_status", "due_reason", "blocking_issue_id", "verification_status"}, nil),
		tableSpec("Counts_Snapshots", []string{"business_date", "source_id", "farm", "park", "shed", "stage", "breed", "age_class", "source_count", "canonical_count", "delta", "source_link", "verification_status"}, countsSeedRows(cfg.BusinessDate)),
		tableSpec("Feed_Sales_Reconciliation", []string{"month", "farm", "sales_animals", "sales_value", "feed_spend", "source_sales", "source_feed", "delta", "verification_status"}, nil),
		tableSpec("Validation_Rules", validationRuleColumns(), validationRuleValues(rules)),
		tableSpec("Issue_Queue", issueColumns(), issueValues(issues)),
		tableSpec("Import_Batches", []string{"batch_id", "created_at", "created_by", "source_run_id", "row_count", "green_rows", "amber_rows", "red_rows", "preview_status", "goat_os_preview_request_id", "goat_os_import_request_id", "idempotency_key", "import_status", "imported_rows", "failed_rows", "failure_summary"}, nil),
	}
	schemaHash := workbookSchemaHash(specs)
	for i := range specs {
		if specs[i].Name == "Sync_Runs" {
			specs[i].Rows = [][]string{syncRunValue(cfg, runID, sources, rules, issues, schemaHash)}
			break
		}
	}
	return specs
}

func tableSpec(name string, cols []string, rows [][]string) tabSpec {
	return tabSpec{Name: name, Cols: cols, Rows: rows}
}

func sourceCatalogColumns() []string {
	return []string{"source_id", "source_family", "source_system", "dataset", "table_name", "sheet_url", "tab_name", "grain", "primary_keys", "owner", "extractor", "freshness_sla", "watermark_field", "last_successful_watermark", "last_schema_hash", "last_row_count", "notes"}
}

func syncRunColumns() []string {
	return []string{"run_id", "run_type", "scheduled_for_ist", "started_at", "completed_at", "status", "business_date", "source_id", "rows_read", "rows_written", "failed_rows", "source_hash", "schema_hash", "bq_job_id", "cloud_run_job_id", "error_summary"}
}

func validationRuleColumns() []string {
	return []string{"rule_id", "rule_name", "severity", "blocking", "owner", "source_family", "description", "check_type", "sql_or_formula_ref", "expected_result", "failure_message", "last_run_id", "last_status"}
}

func issueColumns() []string {
	return []string{"issue_id", "issue_type", "severity", "blocking", "animal_identifier_1", "animal_identifier_2", "load_id", "source_id", "source_record_id", "evidence", "owner", "next_action", "sla_date", "status", "resolved_by", "resolved_at", "resolution_note"}
}

func syncRunValue(cfg config, runID string, sources []sourceCatalogRow, rules []validationRuleRow, issues []issueSeedRow, schemaHash string) []string {
	now := time.Now().UTC().Format(time.RFC3339)
	return []string{runID, cfg.RunType, "", now, now, "bootstrapped", cfg.BusinessDate, "manifest", "0", strconv.Itoa(len(sources) + len(rules) + len(issues)), "0", manifestHash(sources, rules, issues), schemaHash, "", "", "metadata/bootstrap only; no legacy source rows imported"}
}

func manifestSnapshotRow(runID, businessDate string, sources []sourceCatalogRow, rules []validationRuleRow, issues []issueSeedRow) []string {
	payload := compactJSON(map[string]any{
		"source_catalog_rows": len(sources),
		"validation_rules":    len(rules),
		"issue_seeds":         len(issues),
		"sheet_url":           defaultSheetURL,
	})
	hash := hashString(payload)
	now := time.Now().UTC().Format(time.RFC3339)
	return []string{runID, "manifest", businessDate, "control_metadata", "legacy-god-sheet-sync-manifest", "manifest|" + businessDate, hash, payload, now, now}
}

func sourceCatalogRows() []sourceCatalogRow {
	rows := []sourceCatalogRow{
		source("audit_script", "dashboard_audit", "repo", "", "scripts/ceo-dashboard-audit.mjs", "", "", "audit_run", "run_id", "data/dev", "repo+Cloud Run job", "twice_daily", "", "", "", "", "Scheduled Slack audit inventory and findings."),
		source("legacy_dashboard_api", "dashboard_api", "https", "", "", "https://dashboard--goatos-sheets.us-central1.hosted.app", "", "api_response", "endpoint,business_date", "data/dev", "https", "hourly", "business_date", "", "", "", "Read-only legacy dashboard API host."),
		source("daily_summary_dev", "counts", "bigquery", "farm", "daily_summary_dev", "", "", "business_date", "date", "data/dev", "bigquery", "daily_after_0130_ist", "date", "", "", "", "Canonical top /api/counts KPI source; same-source hash drift must be tracked."),
		source("counting_db_with_holding_dev", "counts", "bigquery", "ceo_dashboard", "counting_db_with_holding_dev", "https://docs.google.com/spreadsheets/d/1vWtbgZI2Yz__noTocwPWzCtEDcS-UXW7mJ9ZqSQ5w_4/edit?gid=0#gid=0", "DB", "aggregate_count_row", "date,farm,shed,stage,breed,age", "data/dev", "bigquery", "daily_after_0130_ist", "date", "", "", "", "Aggregate source-count evidence; not animal-grain import proof."),
		source("counting_kpis_daily", "counts", "bigquery", "ceo_dashboard", "counting_kpis_daily", "", "", "business_date", "date", "data/dev", "bigquery", "daily_after_0130_ist", "date", "", "", "", "Fallback age/gender/fattening KPI rows."),
		source("goats_db_clean", "goats_db", "bigquery", "goatsDB", "goats_db_clean", "https://docs.google.com/spreadsheets/d/1R648AutCSXS247DZb7dc07dDgyue6_R4mZDd93oW3M8/edit?gid=0#gid=0", "DB", "animal_event", "goat_id,farm_goat_id,inp_goat_id,date,event", "data/dev", "bigquery", "hourly", "date", "", "", "", "Event spine; source IDs are crosswalk only, not animal_identifier_1."),
		source("goats_db_active_shedwise_details", "goats_db", "bigquery", "goatsDB", "active-goats-list-shedwise-details", "", "", "animal_or_shed_detail", "", "data/dev", "bigquery", "hourly", "", "", "", "", "Active shedwise details where available."),
		source("goats_db_rfid_mapping", "identity", "drive_sheet_external_bigquery", "goatsDB", "goatsDB_rfid_mapping", "", "", "animal_identifier", "RFID,Old_Tag_ID", "data/dev", "drive+sheets", "hourly", "", "", "", "", "Drive/Sheets-gated RFID/Gender source required for animal_identifier_1 and sex backfill."),
		source("cpt_rfid_beetal", "identity", "drive_sheet_external_bigquery", "goatsDB", "CPT_RFID_Beetal", "", "", "animal_identifier", "RFID,Old_Rtag", "data/dev", "drive+sheets", "hourly", "", "", "", "", "Drive/Sheets-gated old/new tag source; not an independent non-Drive path."),
		source("farm_goat_id_mapping", "identity", "bigquery", "goatsDB", "farm_goat_id_mapping", "", "", "id_mapping", "old_farm_goat_id,new_farm_goat_id", "data/dev", "bigquery", "daily", "", "", "", "", "Retag/renumber resolver for farm-goat-id grain only."),
		source("mother_kid_facts", "births", "bigquery", "goatsDB", "mother_kid_facts", "", "", "birth_fact", "mother_id,kid_id,is_birth", "data/dev", "bigquery", "daily", "", "", "", "", "Birth provenance; use is_birth=1."),
		source("kids_tag_ids", "identity", "bigquery", "goatsDB", "kids_tag_ids", "", "", "kid_tag", "", "data/dev", "bigquery", "daily", "", "", "", "", "Kid tag ID evidence, subject to RFID/tag uniqueness validation."),
		source("shiftings_reports_clean", "movement", "bigquery", "Shiftings", "shiftings_reports_clean", "https://docs.google.com/spreadsheets/d/1QXhAbV0wAT739S84LfUhHMWPaGw-oUlbZLZHPxEr5G4/edit?resourcekey=&gid=589667268#gid=589667268", "Shifting Reports", "movement_event", "shifting_id,date,farm,dst_shed,dst_tag", "data/dev", "bigquery", "hourly", "date", "", "", "", "Animal-grain movement evidence."),
		source("shiftings_fact", "movement", "bigquery", "Shiftings", "shiftings_fact", "", "", "movement_event", "", "data/dev", "bigquery", "hourly", "date", "", "", "", "Shifting fact rows."),
		source("cbe_kids_current_stage_days", "stage", "bigquery", "Shiftings", "cbe_kids_current_stage_days", "", "", "current_stage", "farm,stage", "data/dev", "bigquery", "hourly", "", "", "", "", "CBE current-stage source for parity."),
		source("cpt_kids_current_stage_days", "stage", "bigquery", "Shiftings", "cpt_kids_current_stage_days", "", "", "current_stage", "farm,stage", "data/dev", "bigquery", "hourly", "", "", "", "", "CPT current-stage source for parity."),
		source("growth_farmwise_weighing", "fattening", "bigquery", "ceo_dashboard", "growth_farmwise_weighing", "", "", "farm_stage_count", "farm,stage", "data/dev", "bigquery", "hourly", "", "", "", "", "Fattening stage source compared to current-stage tables."),
		source("sales_db_clean", "sales", "bigquery", "salesDB", "salesDB_clean", "https://docs.google.com/spreadsheets/d/1ACQJIQIZRkoQx76HtO_vORsGCVekHsI6TFFH-vohT8I/edit?gid=0#gid=0", "", "sale_event", "sales_id,date,farm", "data/dev", "bigquery", "daily", "date", "", "", "", "Sales animal count and value."),
		source("monthly_feed_vs_sales", "sales_feed", "bigquery", "ceo_dashboard", "monthly_feed_vs_sales", "", "", "month_farm", "month,farm", "data/dev", "bigquery", "daily", "month", "", "", "", "Sales vs feed comparison."),
		source("procurement_db_clean", "procurement", "bigquery", "procurement_farm", "procurement_dB_clean", "https://docs.google.com/spreadsheets/d/1_856nbDd5jHORrq1ISpaELGn6dewzG2AedlsxE1_bXE/edit?gid=0#gid=0", "DB", "procurement_event", "load_id,Record_Type", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Use Record_Type='Purchase' for purchase total."),
		source("load_wise_procurement_with_status", "procurement", "bigquery", "procurement_farm", "load_wise_procurement_with_status", "", "", "load_status", "load_id", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Loadwise status rollup source."),
		source("loadwise_summary", "procurement", "bigquery", "procurement_farm", "loadwise_summary", "", "", "load_summary", "load_id", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Procurement loadwise summary."),
		source("procurement_holding_farm_clean", "procurement", "bigquery", "procurement_farm", "procurement_holding_farm_clean", "https://docs.google.com/spreadsheets/d/1_856nbDd5jHORrq1ISpaELGn6dewzG2AedlsxE1_bXE/edit?gid=1331687687#gid=1331687687", "", "holding_event", "", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Procurement holding clean rows."),
		source("shed_tag_table", "locations", "bigquery", "Shiftings", "shed_tag_table", "https://docs.google.com/spreadsheets/d/1Qo5k40CIkS4Lu0074DYSRsiFrmW8LAA8Oi0vMK0X1oI/edit?gid=0#gid=0", "", "location_profile", "shed_tag", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Shed tag/location mapping."),
		source("counting_shed_tag_capacity", "locations", "bigquery", "counting", "shed_tag_capacity_external_table", "https://docs.google.com/spreadsheets/d/1Qo5k40CIkS4Lu0074DYSRsiFrmW8LAA8Oi0vMK0X1oI/edit?gid=0#gid=0", "", "location_capacity", "shed_tag", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Shed capacity external source."),
		source("health_db_clean_dev", "health", "bigquery", "healthDB", "health_db_clean_dev", "https://docs.google.com/spreadsheets/d/1uvDO_vipNsLcB4S0O7Bj-L8VCSMCd0eX5U9cJS8F-QE/edit?gid=474314888#gid=474314888", "DB", "health_event", "", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Health event rows."),
		source("diagnosis_clean_table", "health", "bigquery", "healthDB", "diagnosis_clean_table", "https://docs.google.com/spreadsheets/d/1uvDO_vipNsLcB4S0O7Bj-L8VCSMCd0eX5U9cJS8F-QE/edit?gid=515434741#gid=515434741", "Diagnosis Form", "diagnosis_event", "", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Diagnosis rows."),
		source("vaccination_external_table", "vaccination", "drive_sheet_external_bigquery", "ceo_dashboard", "vaccination_external_table", "https://docs.google.com/spreadsheets/d/1L1fZG37ZL9ZmPyOHZxoR6rSYqMrPrWbYbR4ppbRy-G4/edit?gid=0#gid=0", "", "shed_count_vaccination_note", "date,farm,shed,vaccine", "data/dev", "drive+sheets", "daily", "Date", "", "", "", "Shed/count grained legacy notes only; cannot suppress animal due work."),
		source("vaccination_dashboard", "vaccination", "bigquery", "ceo_dashboard", "vaccination_dashboard", "", "", "vaccination_rollup", "", "data/dev", "bigquery", "daily", "", "", "", "", "Legacy vaccination dashboard rollup."),
		source("feed_db_clean", "feed", "bigquery", "feedDB", "feedDB_clean", "https://docs.google.com/spreadsheets/d/1HXaHFTEquc0iVxfC_ZeEm9pB3kAxE58-0oiGtiQpSp8/edit?gid=0#gid=0", "DB", "feed_event", "", "data/dev", "bigquery", "daily", "", "", "", "", "Feed source rows."),
		source("feed_directions_clean", "feed", "bigquery", "feedDB", "feedDirections_clean", "https://docs.google.com/spreadsheets/d/1OEr8j_9fYYWmQm0VkP6UgZ1YBcWIs-Km08XGs44W6Hg/edit?gid=723978225#gid=723978225", "", "feed_direction", "", "data/dev", "bigquery", "daily", "", "", "", "", "Feed direction rows."),
		source("feed_daily_spend", "feed", "bigquery", "feedDB", "feed_daily_spend", "", "", "daily_spend", "date,farm", "data/dev", "bigquery", "daily", "date", "", "", "", "Feed spend source."),
		source("weights_db_clean", "weights", "bigquery", "weights", "weights_db_clean", "https://docs.google.com/spreadsheets/d/1TlSf-Pg1dyIB6ZKbd_FQ5ILiJX3AkkZp9Pg4dVmak2g/edit?gid=1889151758#gid=1889151758", "", "weight_event", "", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Raw weight rows."),
		source("weights_db_standardized", "weights", "bigquery", "weights", "weights_db_standardized", "", "", "weight_event_standardized", "", "data/dev+ground/source team", "bigquery", "daily", "", "", "", "", "Standardized weight rows."),
		source("mortality_total_dev", "mortality", "bigquery", "ceo_dashboard", "mortality_total_dev", "", "", "mortality_total", "date", "data/dev", "bigquery", "daily", "date", "", "", "", "Mortality top total."),
		source("monthly_mortality_rate", "mortality", "bigquery", "ceo_dashboard", "monthly_mortality_rate", "", "", "month", "month", "data/dev", "bigquery", "daily", "month", "", "", "", "Mortality monthly rows."),
		source("breeding_external_table", "breeding", "drive_sheet_external_bigquery", "breedingDB", "breedingDB_external_table", "https://docs.google.com/spreadsheets/d/1h04WpLExJBdZ-J-H2YLGXyHnlWSdjtldiaB6n3x3rYg/edit?gid=0#gid=0", "DB", "breeding_event", "", "data/dev+ground/source team", "drive+sheets", "daily", "", "", "", "", "Breeding event rows."),
		source("parent_stock_table", "breeding", "bigquery", "ceo_dashboard", "parent_stock_table", "", "", "parent_stock", "", "data/dev", "bigquery", "daily", "", "", "", "", "Parent stock view."),
		source("total_summary_breeding", "breeding", "bigquery", "ceo_dashboard", "total_summary_breeding", "", "", "breeding_summary", "", "data/dev", "bigquery", "daily", "", "", "", "", "Breeding total summary."),
		source("birth_db_unclean", "delivery_birth", "drive_sheet_external_bigquery", "deliveryDB", "birthDB_unclean", "https://docs.google.com/spreadsheets/d/1bYNW8c6BMb6wgBXEIHkWO57nYRTkYc4mnPkDE14S2Yw/edit?gid=32106927#gid=32106927", "DB", "birth_delivery_raw", "", "data/dev+ground/source team", "drive+sheets", "daily", "", "", "", "", "Raw delivery/birth external mirror."),
		source("delivery_db_clean_dev", "delivery_birth", "bigquery", "deliveryDB", "delivery_db_clean_dev", "", "", "birth_delivery_clean", "", "data/dev", "bigquery", "daily", "", "", "", "", "Cleaned delivery/birth read source."),
		source("farmer_crops_db", "farmer_network", "drive_sheet_external_bigquery", "farmersDB", "farmer_crops_db", "https://docs.google.com/spreadsheets/d/16bEJqIVZ4gFGSPFCPvTud8Z0dZEa8Wur5tUM0JNwJaM/edit?gid=0#gid=0", "DB", "farmer_network", "", "data/dev", "drive+sheets", "daily", "", "", "", "", "Farmer network table."),
		source("milk_consumption_db", "milk_lactation", "drive_sheet_external_bigquery", "ceo_dashboard", "milk_consumption_db", "https://docs.google.com/spreadsheets/d/1qJRQK2DDy2C4y359CHVBh1OhWBk0K7FJMMVvXCUqr0A/edit?gid=1263357772#gid=1263357772", "Milk Consumption DB", "milk_consumption", "", "data/dev", "drive+sheets", "daily", "", "", "", "", "Milk consumption rows."),
		source("milk_feeding_summary", "milk_lactation", "drive_sheet_external_bigquery", "ceo_dashboard", "milk_feeding_summary", "https://docs.google.com/spreadsheets/d/1qJRQK2DDy2C4y359CHVBh1OhWBk0K7FJMMVvXCUqr0A/edit?gid=1556648101#gid=1556648101", "Milk Feeding Summary", "milk_feeding", "", "data/dev", "drive+sheets", "daily", "", "", "", "", "Milk feeding summary rows."),
		source("milk_consumption_external_table", "milk_lactation", "drive_sheet_external_bigquery", "ceo_dashboard", "milk_consumption_external_table", "https://docs.google.com/spreadsheets/d/1J3WWJbuFp3PPpj-UzB4zmzYA7g7G0-KvMonx9-cE9FE/edit?gid=0#gid=0", "", "milk_lactation_alt", "", "data/dev", "drive+sheets", "daily", "", "", "", "", "Alternate milk/lactation source."),
		source("milking_goats_list", "milk_lactation", "bigquery", "ceo_dashboard", "milking_goats_list", "", "", "lactating_mother", "", "data/dev", "bigquery", "daily", "", "", "", "", "Direct lactation status seed."),
		source("dashboard_users", "rbac", "drive_sheet_external_bigquery", "ceo_dashboard", "dashboard_users", "https://docs.google.com/spreadsheets/d/13TtoRv0pKYtatsuc3-YDZHcTdWALekBd-e-WMblrniY/edit", "", "user_role", "email", "data/dev", "drive+sheets", "daily", "", "", "", "", "Role/owner/escalation mapping only; no password fields."),
		source("goat_history_complete", "history", "bigquery_view", "historyAutomation", "goat_history_complete", "", "", "animal_history", "animal_id,event_date,event_type", "data/dev", "bigquery", "daily", "", "", "", "", "Complete history view for lifecycle reconciliation."),
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].SourceID < rows[j].SourceID })
	return rows
}

func source(sourceID, family, system, dataset, tableName, sheetURL, tabName, grain, keys, owner, extractor, sla, watermark, lastWatermark, schemaHash, rowCount, notes string) sourceCatalogRow {
	return sourceCatalogRow{SourceID: sourceID, SourceFamily: family, SourceSystem: system, Dataset: dataset, TableName: tableName, SheetURL: sheetURL, TabName: tabName, Grain: grain, PrimaryKeys: keys, Owner: owner, Extractor: extractor, FreshnessSLA: sla, WatermarkField: watermark, LastSuccessfulWatermark: lastWatermark, LastSchemaHash: schemaHash, LastRowCount: rowCount, Notes: notes}
}

func sourceCatalogValues(rows []sourceCatalogRow) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{r.SourceID, r.SourceFamily, r.SourceSystem, r.Dataset, r.TableName, r.SheetURL, r.TabName, r.Grain, r.PrimaryKeys, r.Owner, r.Extractor, r.FreshnessSLA, r.WatermarkField, r.LastSuccessfulWatermark, r.LastSchemaHash, r.LastRowCount, r.Notes})
	}
	return out
}

func validationRuleRows() []validationRuleRow {
	rows := []validationRuleRow{
		rule("business_date_row_hash_drift", "Business date row hash drift", "P1", "conditional", "data/dev", "all", "Detect same-source same-key row mutation across runs.", "snapshot_hash_compare", "Raw_Source_Snapshots.source_row_hash by source_row_key", "Stable row hash or intentional refresh recorded.", "Source row changed in place without an accepted refresh."),
		rule("active_spine_dashboard_reconcile", "Active spine vs dashboard aggregate", "P1", "import", "data/dev", "counts", "Compare event-spine active candidates to dashboard aggregate total.", "bq_sql+api", "active_spine_candidates vs dashboard_active_total", "Delta is explained and row-level source decision exists.", "Event spine and dashboard active totals are unresolved."),
		rule("dashboard_rows_are_aggregate_only", "Dashboard aggregate rows are not animal rows", "P1", "yes", "data/dev", "counts", "Prevent aggregate dashboard rows from seeding Animal_Master.", "static_rule", "Source_Catalog grain", "Aggregate rows only validate totals.", "Aggregate source was used as row-level animal proof."),
		rule("raw_source_row_has_identifier", "Raw source row has candidate identifier", "P1", "yes", "data/dev + ground/source team", "identity", "Every raw animal-event source row must have at least one usable candidate identifier before grouping.", "row_validation", "Mapping_Crosswalks", "Rows missing all identifiers create one RED issue each.", "Raw row is missing all candidate identifiers."),
		rule("animal_identifier_1_present", "Primary RFID/tag present", "P1", "yes", "data/dev", "identity", "Import candidate must have trusted primary RFID/tag animal_identifier_1 from Drive-readable RFID mapping or validated old-tag/new-tag columns.", "row_validation", "Animal_Master.animal_identifier_1", "Trusted RFID/tag value exists and is globally unique.", "Missing trusted primary RFID/tag; legacy source IDs and dst_tag do not satisfy this."),
		rule("animal_identifier_uniqueness", "RFID/tag globally single-use", "P1", "yes", "data/dev", "identity", "Normalized RFID/tag identifiers must be globally single-use across current and historical identifier sources.", "duplicate_check", "Animal_Identifier_History", "No duplicate current/historical RFID/tag owners.", "Reused fallen/retired RFID tag detected."),
		rule("species_evidence_present", "Species evidence present", "P1", "yes", "data/dev + ground/source team", "taxonomy", "Import candidate must have trusted species from explicit source type or approved Species_Taxonomy_Crosswalk.", "row_validation", "Animal_Master.species", "Species is goat or sheep with evidence.", "Missing or ambiguous species."),
		rule("breed_species_taxonomy_reconcile", "Breed/species taxonomy reconcile", "P2", "conditional", "data/dev + ground/source team", "taxonomy", "Compare normalized breed/species vocabulary across event spine, dashboard count rows, procurement, and crosswalk.", "reconciliation", "Species_Taxonomy_Crosswalk", "No unmapped one-sided taxonomy values affecting import/counts.", "Breed/species vocabulary mismatch needs review."),
		rule("dob_evidence_or_approved_estimate", "DOB evidence or approved estimate", "P1", "yes", "data/dev + ground/source team", "births", "Active import candidates must have trusted Birth-event date evidence or approved estimated DOB evidence.", "row_validation", "Animal_Master.dob,dob_estimated", "DOB exists or approved estimated DOB evidence exists.", "Missing DOB evidence."),
		rule("birth_time_not_dob", "birth_time is not DOB", "P1", "yes", "data/dev", "births", "birth_time must never satisfy DOB evidence; it is stored only as time-of-day provenance.", "static_rule", "goats_db_clean.birth_time", "birth_time ignored for DOB proof.", "birth_time was treated as date-of-birth."),
		rule("purchase_no_birth_no_age_red", "Purchase no birth no age is RED", "P2", "import", "ground/source team", "births", "Purchase-origin candidates with no Birth date and no usable age evidence remain RED.", "row_validation", "goats_db_clean event/date/age", "Rows stay RED until source/ground DOB evidence arrives.", "Purchase-origin animal has no Birth date or usable age evidence."),
		rule("sex_evidence_present", "Sex evidence present", "P1", "yes", "data/dev + ground/source team", "identity", "Active import candidates must have trusted sex evidence from native events, Drive RFID mapping, or another approved source.", "row_validation", "Animal_Master.sex", "Sex is female or male with evidence.", "Missing or conflicting sex."),
		rule("current_location_status_resolved", "Current location/status resolved", "P1", "yes", "data/dev + ground/source team", "locations", "Active import candidates must have current farm/park/shed/stage resolved from latest event/Shifting animal-grain evidence plus Location_Profile mapping.", "row_validation", "Current_Location_Status", "Current location and status are resolved.", "Current location/status unresolved."),
		rule("vaccination_legacy_count_not_suppressing_due", "Legacy vaccination count does not suppress due", "P1", "yes", "data/dev", "vaccination", "Legacy shed-count vaccination rows may be stored as notes but must not suppress animal-level due work unless trusted animal-level evidence exists.", "row_validation", "Vaccination_History.trust_status,suppresses_due", "Only trusted reviewed animal evidence suppresses due work.", "Untrusted legacy note is suppressing due work."),
		rule("procurement_identity_grain_resolved", "Procurement identity grain resolved", "P1", "procurement_import", "data/dev + ground/source team", "procurement", "Procurement and GOATS DB purchase deltas must be evaluated at each candidate identity grain.", "reconciliation", "Identity_Grain_Audit", "Canonical grain decision exists before row-level blocker.", "Procurement mismatch has unresolved identity grain."),
		rule("fattening_shiftings_stage_parity", "Fattening vs shiftings parity", "P2", "no", "data/dev + ground/source team", "fattening", "Fattening and current-stage sources must be compared by farm/stage.", "reconciliation", "Fattening_Shifting_Reconciliation", "Differences have source refs and owner.", "Fattening/current-stage source mismatch."),
		rule("source_stale_past_sla", "Source stale past SLA", "P1", "conditional", "data/dev", "all", "Source watermark must be within configured SLA for its source family.", "watermark_check", "Source_Catalog.last_successful_watermark", "Source is fresh enough for dependent rows.", "Source stale past SLA."),
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].RuleID < rows[j].RuleID })
	return rows
}

func rule(id, name, severity, blocking, owner, family, desc, checkType, ref, expected, failure string) validationRuleRow {
	return validationRuleRow{RuleID: id, RuleName: name, Severity: severity, Blocking: blocking, Owner: owner, SourceFamily: family, Description: desc, CheckType: checkType, SQLOrFormulaRef: ref, ExpectedResult: expected, FailureMessage: failure, LastStatus: "seeded"}
}

func validationRuleValues(rows []validationRuleRow) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{r.RuleID, r.RuleName, r.Severity, r.Blocking, r.Owner, r.SourceFamily, r.Description, r.CheckType, r.SQLOrFormulaRef, r.ExpectedResult, r.FailureMessage, r.LastRunID, r.LastStatus})
	}
	return out
}

func issueSeedRows() []issueSeedRow {
	rows := []issueSeedRow{
		issue("seed-drive-rfid-access", "source_schema_or_access", "P1", "yes", "", "", "", "goats_db_rfid_mapping", "", "Drive/Sheets credential required before RFID/Gender extraction can pass.", "data/dev", "Wire Drive/Sheets read credentials and verify RFID mapping headers.", "", "open"),
		issue("seed-animal-import-gate-feasibility", "import_gate_feasibility", "P1", "yes", "", "", "", "goats_db_clean", "", "Track species, DOB, sex, RFID, and location coverage before Goat OS DB import.", "data/dev + ground/source team", "Run full sync and resolve RED blockers before import preview.", "", "open"),
		issue("seed-event-spine-dashboard-gap", "aggregate_reconciliation", "P1", "import", "", "", "", "daily_summary_dev", "", "Event-spine active and dashboard aggregate can disagree; dashboard is aggregate-only.", "data/dev", "Write Counts_Snapshots deltas and identify row-level source priority.", "", "open"),
		issue("seed-purchase-dob-gap", "missing_dob", "P2", "import", "", "", "", "goats_db_clean", "", "Purchase-origin rows without Birth date or usable age require source/ground DOB verification.", "ground/source team", "Supply DOB evidence or approve estimated-DOB policy.", "", "open"),
		issue("seed-procurement-identity-grain", "identity_grain_mismatch", "P1", "procurement_import", "", "", "", "procurement_db_clean", "", "Procurement vs GOATS DB purchase deltas must be evaluated at every candidate identity grain.", "data/dev + ground/source team", "Compute Identity_Grain_Audit and choose canonical grain.", "", "open"),
		issue("seed-procurement-loadwise-rollup", "source_reconciliation", "P2", "conditional", "", "", "", "load_wise_procurement_with_status", "", "Procurement loadwise status rollup must reconcile to loadwise summary.", "data/dev + ground/source team", "Compare load-wise procurement with loadwise_summary.", "", "open"),
		issue("seed-fattening-shiftings-stage", "source_reconciliation", "P2", "no", "", "", "", "growth_farmwise_weighing", "", "Fattening source and current-stage sources can disagree by farm/stage.", "data/dev + ground/source team", "Reconcile growth_farmwise_weighing with CBE/CPT current-stage tables.", "", "open"),
		issue("seed-taxonomy-reconcile", "taxonomy_mapping", "P2", "conditional", "", "", "", "goats_db_clean", "", "Breed/species vocabulary must reconcile across event spine, dashboard counts, procurement, and crosswalk.", "data/dev + ground/source team", "Populate Species_Taxonomy_Crosswalk and resolve unmapped values.", "", "open"),
		issue("seed-vaccination-shed-count-only", "vaccination_trust", "P1", "yes", "", "", "", "vaccination_external_table", "", "Legacy vaccination source is shed/count grained and cannot seed animal-level Vaccination_History or suppress due work.", "data/dev", "Store as untrusted_legacy_note only.", "", "open"),
		issue("seed-dropped-no-identifier-rows", "missing_identifier", "P2", "yes", "", "", "", "goats_db_clean", "", "Raw event rows missing all candidate identifiers must become one RED issue per source row.", "data/dev + ground/source team", "Run raw source identifier scan.", "", "open"),
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].IssueID < rows[j].IssueID })
	return rows
}

func issue(id, typ, severity, blocking, a1, a2, loadID, sourceID, sourceRecordID, evidence, owner, nextAction, sla, status string) issueSeedRow {
	return issueSeedRow{IssueID: id, IssueType: typ, Severity: severity, Blocking: blocking, AnimalIdentifier1: a1, AnimalIdentifier2: a2, LoadID: loadID, SourceID: sourceID, SourceRecordID: sourceRecordID, Evidence: evidence, Owner: owner, NextAction: nextAction, SLADate: sla, Status: status}
}

func issueValues(rows []issueSeedRow) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{r.IssueID, r.IssueType, r.Severity, r.Blocking, r.AnimalIdentifier1, r.AnimalIdentifier2, r.LoadID, r.SourceID, r.SourceRecordID, r.Evidence, r.Owner, r.NextAction, r.SLADate, r.Status, r.ResolvedBy, r.ResolvedAt, r.ResolutionNote})
	}
	return out
}

func identitySeedRows() [][]string {
	return [][]string{
		{"goats_db_clean", "Purchase", "farm_goat_id", "", "distinct non-empty farm_goat_id; collapse farm_goat_id_mapping before decision", "candidate", "", "", "data/dev + ground/source team", "pending_live_compute", "Filled by sync from BigQuery; no static numbers in runbook or seed."},
		{"goats_db_clean", "Purchase", "goat_id", "", "distinct non-empty global goat_id", "candidate", "", "", "data/dev + ground/source team", "pending_live_compute", "Filled by sync from BigQuery; goat_id is crosswalk only, not animal_identifier_1."},
		{"goats_db_clean", "Purchase", "inp_goat_id", "", "distinct non-empty inp_goat_id", "candidate", "", "", "data/dev + ground/source team", "pending_live_compute", "Filled by sync from BigQuery; inp_goat_id is crosswalk only, not animal_identifier_1."},
		{"goats_db_clean", "Purchase", "coalesced_source_id", "", "COALESCE(goat_id,farm_goat_id,inp_goat_id)", "candidate", "", "", "data/dev + ground/source team", "pending_live_compute", "For audit grain only; does not satisfy RFID/tag import gate."},
	}
}

func speciesSeedRows() [][]string {
	return [][]string{
		{"procurement_farm.procurement_dB_clean", "Animal_Type", "Goat", "goat", "goat", "high", "explicit_source_type", "", "", "", "pending_review"},
		{"procurement_farm.procurement_dB_clean", "Animal_Type", "Sheep", "sheep", "sheep", "high", "explicit_source_type", "", "", "", "pending_review"},
		{"goatsDB.goats_db_clean", "breed", "Beetal", "beetal", "goat", "medium", "breed_crosswalk", "", "", "", "pending_review"},
		{"goatsDB.goats_db_clean", "breed", "Boer", "boer", "goat", "medium", "breed_crosswalk", "", "", "", "pending_review"},
		{"goatsDB.goats_db_clean", "breed", "Sirohi", "sirohi", "goat", "medium", "breed_crosswalk", "", "", "", "pending_review"},
		{"goatsDB.goats_db_clean", "breed", "Anantapur Sheep", "anantapur sheep", "sheep", "medium", "breed_crosswalk", "", "", "", "pending_review"},
		{"goatsDB.goats_db_clean", "breed", "Sojat", "sojat", "sheep", "medium", "breed_crosswalk", "", "", "", "pending_review"},
	}
}

func countsSeedRows(businessDate string) [][]string {
	return [][]string{
		{businessDate, "dashboard_active_total", "", "", "", "", "", "", "", "", "", "legacy dashboard /api/counts + farm.daily_summary_dev", "pending_live_compute"},
		{businessDate, "event_spine_active_total", "", "", "", "", "", "", "", "", "", "goatsDB.goats_db_clean pinned active-spine SQL", "pending_live_compute"},
		{businessDate, "event_spine_dashboard_delta", "", "", "", "", "", "", "", "", "", "active_spine_candidates - dashboard_active_total", "pending_live_compute"},
	}
}

func printSummary(stdout io.Writer, summary syncSummary, jsonOut bool, label string) error {
	if jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	fmt.Fprintf(stdout, "%s run_id=%s spreadsheet=%s business_date=%s tabs=%d source_catalog=%d validation_rules=%d issue_seeds=%d apply=%t replace_managed_tabs=%t\n",
		label, summary.RunID, summary.SpreadsheetID, summary.BusinessDate, summary.Tabs,
		summary.SourceCatalogRows, summary.ValidationRules, summary.IssueSeeds,
		summary.Apply, summary.ReplaceManagedTabs)
	if summary.Apply {
		fmt.Fprintf(stdout, "created_tabs=%d written_tabs=%d skipped_tabs=%d appended_rows=%d\n",
			len(summary.CreatedTabs), len(summary.WrittenTabs), len(summary.SkippedTabs), len(summary.AppendedRows))
	}
	return nil
}

func defaultBusinessDate(now time.Time) string {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*60*60+30*60)
	}
	return now.In(loc).AddDate(0, 0, -1).Format("2006-01-02")
}

func newRunID() string {
	return "legacy-god-sheet-" + time.Now().UTC().Format("20060102T150405000000000Z") + "-" + randomHexSuffix(4)
}

func randomHexSuffix(byteCount int) string {
	if byteCount <= 0 {
		byteCount = 4
	}
	buf := make([]byte, byteCount)
	if _, err := cryptorand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func quoteSheet(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func manifestHash(sources []sourceCatalogRow, rules []validationRuleRow, issues []issueSeedRow) string {
	return hashString(compactJSON(map[string]any{"sources": sources, "rules": rules, "issues": issues}))
}

func workbookSchemaHash(specs []tabSpec) string {
	type schemaRow struct {
		Name string   `json:"name"`
		Cols []string `json:"cols"`
	}
	rows := make([]schemaRow, 0, len(specs))
	for _, spec := range specs {
		rows = append(rows, schemaRow{Name: spec.Name, Cols: spec.Cols})
	}
	return hashString(compactJSON(rows))
}

func compactJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func getenv(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

func getenvDefault(name, fallback string) string {
	if value := getenv(name); value != "" {
		return value
	}
	return fallback
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func boolEnv(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func withSheetsRetry[T any](ctx context.Context, op func() (T, error)) (T, error) {
	var zero T
	delays := []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second}
	for attempt := 0; ; attempt++ {
		value, err := op()
		if err == nil {
			return value, nil
		}
		if attempt >= len(delays) || !isRetryableSheetsError(err) {
			return zero, err
		}
		timer := time.NewTimer(delays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}

func isRetryableSheetsError(err error) bool {
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusTooManyRequests || apiErr.Code >= http.StatusInternalServerError
	}
	return false
}

type googleIdentity struct {
	Principal string
	ProjectID string
	Source    string
}

func assertGoogleIdentity(ctx context.Context, creds *google.Credentials, cfg config) error {
	identity, err := resolveGoogleIdentity(ctx, creds)
	if err != nil {
		return err
	}
	if cfg.SkipADCIdentityGuard {
		fmt.Fprintf(os.Stderr, "legacy-god-sheet-sync warning: Google ADC identity guard skipped principal=%s project=%s source=%s\n", identity.Principal, identity.ProjectID, identity.Source)
		return nil
	}
	if err := validateGoogleIdentity(identity, cfg.ExpectedGooglePrincipal, cfg.AllowedGooglePrincipalSuffixes); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "legacy-god-sheet-sync Google ADC principal=%s project=%s source=%s\n", identity.Principal, identity.ProjectID, identity.Source)
	return nil
}

func resolveGoogleIdentity(ctx context.Context, creds *google.Credentials) (googleIdentity, error) {
	identity := googleIdentity{ProjectID: strings.TrimSpace(creds.ProjectID)}
	var file struct {
		Type           string `json:"type"`
		ClientEmail    string `json:"client_email"`
		ClientID       string `json:"client_id"`
		QuotaProjectID string `json:"quota_project_id"`
	}
	if len(creds.JSON) > 0 {
		_ = json.Unmarshal(creds.JSON, &file)
	}
	if identity.ProjectID == "" {
		identity.ProjectID = strings.TrimSpace(file.QuotaProjectID)
	}
	if file.ClientEmail != "" {
		identity.Principal = strings.TrimSpace(file.ClientEmail)
		identity.Source = "adc_client_email"
		return identity, nil
	}
	email, err := tokenInfoEmail(ctx, creds)
	if err == nil && email != "" {
		identity.Principal = email
		identity.Source = "oauth_tokeninfo_email"
		return identity, nil
	}
	if account := gcloudConfigValue(ctx, "account"); account != "" && account != "(unset)" {
		identity.Principal = account
		identity.Source = "gcloud_config_account"
		if identity.ProjectID == "" {
			identity.ProjectID = gcloudConfigValue(ctx, "project")
		}
		return identity, nil
	}
	if file.ClientID != "" {
		identity.Principal = strings.TrimSpace(file.ClientID)
		identity.Source = "adc_client_id"
		return identity, nil
	}
	if err != nil {
		return googleIdentity{}, fmt.Errorf("resolve Google ADC principal: %w", err)
	}
	return googleIdentity{}, errors.New("resolve Google ADC principal: no client_email, tokeninfo email, or client_id found")
}

func tokenInfoEmail(ctx context.Context, creds *google.Credentials) (string, error) {
	token, err := creds.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("get Google ADC token: %w", err)
	}
	if token.AccessToken == "" {
		return "", errors.New("Google ADC token has no access token")
	}
	endpoint := "https://oauth2.googleapis.com/tokeninfo?access_token=" + url.QueryEscape(token.AccessToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("read Google tokeninfo: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		Email            string `json:"email"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode Google tokeninfo: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(body.ErrorDescription)
		if msg == "" {
			msg = strings.TrimSpace(body.Error)
		}
		if msg == "" {
			msg = resp.Status
		}
		return "", fmt.Errorf("Google tokeninfo rejected ADC token: %s", msg)
	}
	return strings.TrimSpace(body.Email), nil
}

func gcloudConfigValue(ctx context.Context, key string) string {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "gcloud", "config", "get-value", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func validateGoogleIdentity(identity googleIdentity, expectedPrincipal string, allowedSuffixes string) error {
	principal := strings.TrimSpace(identity.Principal)
	if principal == "" {
		return errors.New("Google ADC principal is empty")
	}
	if expected := strings.TrimSpace(expectedPrincipal); expected != "" {
		if !strings.EqualFold(principal, expected) {
			return fmt.Errorf("Google ADC principal %q does not match expected %q", principal, expected)
		}
		return nil
	}
	for _, suffix := range splitCSV(allowedSuffixes) {
		if strings.HasSuffix(strings.ToLower(principal), strings.ToLower(suffix)) {
			return nil
		}
	}
	return fmt.Errorf("Google ADC principal %q is outside allowed suffixes %q; set GOATOS_LEGACY_GOD_SHEET_EXPECTED_GOOGLE_PRINCIPAL for the intended writer", principal, allowedSuffixes)
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
