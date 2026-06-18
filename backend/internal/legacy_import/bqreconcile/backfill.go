package bqreconcile

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// backfillSourceContext tags every passport this command creates so the rows
// can be audited, reviewed, and (if ever needed) reversed deterministically.
const backfillSourceContext = "safe_old_tag_passport_backfill"

// backfillNormalizerVersion mirrors the normalizer version the RFID importer
// stamps on identifiers so old-tag identity keys collide correctly with the
// existing population.
const backfillNormalizerVersion = "identifier_normalizer_v1"

// backfillSourceSystem records that these passports originate from the legacy
// BigQuery/Sheets bridge, not from direct in-app capture.
const backfillSourceSystem = "legacy_bigquery"

// Candidate is one row of the safe old-tag backfill candidate CSV. Only the
// columns this command trusts are mapped; unknown extra columns are ignored.
type Candidate struct {
	RowNumber         int
	Source            string
	Farm              string
	ScopeKey          string
	OldTag            string // raw legacy goat_id used as the old_tag value
	Breed             string
	Gender            string
	Status            string
	LastEvent         string
	EventDate         string
	LastShed          string
	LocalMatchCount   string
	RecommendedAction string
	Reason            string
}

// BackfillPlanRow is the deterministic plan/result for a single candidate.
type BackfillPlanRow struct {
	RowNumber       int    `json:"row_number"`
	Source          string `json:"candidate_source"`
	Farm            string `json:"farm"`
	ScopeKey        string `json:"scope_key"`
	OldTag          string `json:"old_tag"`
	NormalizedValue string `json:"normalized_value"`
	Breed           string `json:"breed"`
	Sex             string `json:"sex"`
	LastShed        string `json:"last_shed,omitempty"`
	CandidateStatus string `json:"candidate_status,omitempty"`
	Lifecycle       string `json:"lifecycle_status"`
	IdentityState   string `json:"identity_state"`
	Action          string `json:"action"` // "create" or "skip"
	SkipReason      string `json:"skip_reason,omitempty"`
	Detail          string `json:"detail,omitempty"`
	ParkID          string `json:"park_id,omitempty"`
	ShedID          string `json:"shed_id,omitempty"`
	CurrentLocID    string `json:"current_location_id,omitempty"`
	GoatID          string `json:"goat_id,omitempty"`
	DisplayID       string `json:"display_id,omitempty"`
}

// CountSnapshot is a direct read of canonical goat counts from the goats table.
type CountSnapshot struct {
	Total          int `json:"total"`
	Alive          int `json:"alive"`
	Sold           int `json:"sold"`
	Dead           int `json:"dead"`
	Inactive       int `json:"inactive"`
	IdentityClean  int `json:"identity_clean"`
	IdentityReview int `json:"identity_needs_review"`
	OpenConflicts  int `json:"open_conflicts"`
}

// BackfillSummary is the JSON result of a backfill run (dry-run or execute).
type BackfillSummary struct {
	DryRun             bool           `json:"dry_run"`
	TenantID           string         `json:"tenant_id"`
	TraceID            string         `json:"trace_id"`
	CandidatesCSV      string         `json:"candidates_csv"`
	CandidatesCSVHash  string         `json:"candidates_csv_sha256"`
	CandidatesRead     int            `json:"candidates_read"`
	LatestOverrides    int            `json:"latest_location_lifecycle_overrides"`
	PlannedCreates     int            `json:"planned_creates"`
	PlannedSkips       int            `json:"planned_skips"`
	PlannedUpdates     int            `json:"planned_existing_lifecycle_updates"`
	Applied            int            `json:"applied_creates"`
	AppliedUpdates     int            `json:"applied_existing_lifecycle_updates"`
	BySource           map[string]int `json:"candidates_by_source"`
	ByFarm             map[string]int `json:"candidates_by_farm"`
	ByStatus           map[string]int `json:"candidates_by_status"`
	CreatesByLifecycle map[string]int `json:"planned_creates_by_lifecycle"`
	SkipsByReason      map[string]int `json:"planned_skips_by_reason"`
	CurrentTotals      CountSnapshot  `json:"current_totals"`
	ExpectedTotals     CountSnapshot  `json:"expected_totals"`
	FinalTotals        *CountSnapshot `json:"final_totals,omitempty"`
	ReportDir          string         `json:"report_dir,omitempty"`
}

// RunCandidateBackfill is the deterministic, fail-closed old-tag passport
// backfill. It is the ONLY path in this package that creates new goats, and it
// only creates passports for unique, deterministic candidate rows supplied
// explicitly via the candidate CSV.
func RunCandidateBackfill(ctx context.Context, pool *pgxpool.Pool, opts Options) (*BackfillSummary, error) {
	if strings.TrimSpace(opts.TenantID) == "" {
		return nil, errors.New("tenant-id is required")
	}
	candidates, hash, err := ReadCandidatesFile(opts.CandidatesCSVPath)
	if err != nil {
		return nil, err
	}
	latestOverrides := 0
	var locationEvents []Event
	if strings.TrimSpace(opts.LocationsPath) != "" {
		events, err := ReadEventsFile(opts.LocationsPath)
		if err != nil {
			return nil, err
		}
		locationEvents = events
		candidates, latestOverrides = applyLatestLocationLifecycleEvidence(candidates, locationEvents)
	}

	summary := &BackfillSummary{
		DryRun:             !opts.Execute,
		TenantID:           opts.TenantID,
		TraceID:            opts.TraceID,
		CandidatesCSV:      opts.CandidatesCSVPath,
		CandidatesCSVHash:  hash,
		CandidatesRead:     len(candidates),
		LatestOverrides:    latestOverrides,
		BySource:           map[string]int{},
		ByFarm:             map[string]int{},
		ByStatus:           map[string]int{},
		CreatesByLifecycle: map[string]int{},
		SkipsByReason:      map[string]int{},
		ReportDir:          opts.ReportDir,
	}
	for _, c := range candidates {
		summary.BySource[orUnknown(c.Source)]++
		summary.ByFarm[orUnknown(c.Farm)]++
		summary.ByStatus[orUnknown(c.Status)]++
	}

	// Plan against current DB state (read-only) for the dry-run report.
	existing, err := loadExistingOldTagKeys(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	existingBackfill, err := loadExistingBackfillOldTagGoats(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	lifecycleRepairRows := desiredBackfillRowsByKey(candidates)
	for key, row := range latestLocationLifecycleRows(locationEvents) {
		if _, ok := lifecycleRepairRows[key]; !ok {
			lifecycleRepairRows[key] = row
		}
	}
	parks, sheds, err := loadParkAndShedLookup(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	rows := planBackfill(candidates, existing)
	attachLocations(rows, parks, sheds)

	current, err := snapshotCounts(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	summary.CurrentTotals = current
	summarizePlan(summary, rows)
	summary.ExpectedTotals = expectedTotals(current, rows)
	summary.PlannedUpdates = applyExpectedLifecycleUpdateDelta(&summary.ExpectedTotals, lifecycleRepairRows, existingBackfill)

	if !opts.Execute {
		if err := writeReports(summary.ReportDir, rows); err != nil {
			return nil, err
		}
		return summary, nil
	}

	applied, appliedUpdates, finalRows, err := applyCandidateBackfill(ctx, pool, opts, candidates, lifecycleRepairRows, parks, sheds)
	if err != nil {
		return nil, err
	}
	summary.Applied = applied
	summary.AppliedUpdates = appliedUpdates
	// Recount planned creates/skips from the authoritative in-transaction plan.
	summary.CreatesByLifecycle = map[string]int{}
	summary.SkipsByReason = map[string]int{}
	summarizePlan(summary, finalRows)

	final, err := snapshotCounts(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	summary.FinalTotals = &final
	if err := writeReports(summary.ReportDir, finalRows); err != nil {
		return nil, err
	}
	return summary, nil
}

// ReadCandidatesFile parses the candidate CSV and returns its SHA-256 hash so
// the source artifact is auditable.
func ReadCandidatesFile(path string) ([]Candidate, string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, "", errors.New("backfill-candidates-csv path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read candidate CSV: %w", err)
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])

	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil {
		return nil, "", fmt.Errorf("parse candidate CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, "", errors.New("candidate CSV is empty")
	}
	header := records[0]
	idx := map[string]int{}
	for i, name := range header {
		idx[strings.ToLower(strings.TrimSpace(name))] = i
	}
	required := []string{"candidate_source", "farm", "scope_key", "goat_id", "status"}
	for _, col := range required {
		if _, ok := idx[col]; !ok {
			return nil, "", fmt.Errorf("candidate CSV missing required column %q", col)
		}
	}
	get := func(rec []string, name string) string {
		i, ok := idx[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}

	candidates := make([]Candidate, 0, len(records)-1)
	for n, rec := range records[1:] {
		candidates = append(candidates, Candidate{
			RowNumber:         n + 2, // 1-based incl. header
			Source:            get(rec, "candidate_source"),
			Farm:              get(rec, "farm"),
			ScopeKey:          get(rec, "scope_key"),
			OldTag:            get(rec, "goat_id"),
			Breed:             get(rec, "breed"),
			Gender:            get(rec, "gender"),
			Status:            get(rec, "status"),
			LastEvent:         get(rec, "last_event"),
			EventDate:         get(rec, "event_date"),
			LastShed:          get(rec, "last_shed"),
			LocalMatchCount:   get(rec, "local_match_count"),
			RecommendedAction: get(rec, "recommended_action"),
			Reason:            get(rec, "reason"),
		})
	}
	return candidates, hash, nil
}

// lifecycleFromCandidateStatus maps a candidate status to a supported
// lifecycle_status. The second return is false when the status has no
// deterministic, supported lifecycle target and the row must be skipped.
func lifecycleFromCandidateStatus(status string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return "alive", true
	case "sold":
		return "sold", true
	case "dead":
		return "dead", true
	case "inactive":
		return "inactive", true
	default:
		return "", false
	}
}

type latestBackfillEvidence struct {
	Status string
	Event  string
	Date   string
	Shed   string
}

func applyLatestLocationLifecycleEvidence(candidates []Candidate, events []Event) ([]Candidate, int) {
	latest := latestBackfillEvidenceByKey(events)
	if len(latest) == 0 {
		return candidates, 0
	}

	out := append([]Candidate(nil), candidates...)
	overrides := 0
	for i := range out {
		if !strings.EqualFold(strings.TrimSpace(out[i].Source), "census_plus_bq_unique_farm") {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(out[i].Status), "inactive") {
			continue
		}
		scope := derivedScopeKey(out[i].Farm)
		normalized := CanonicalIdentifier(out[i].OldTag)
		if scope == "" || normalized == "" {
			continue
		}
		evidence, ok := latest[scope+"|"+normalized]
		if !ok || strings.EqualFold(strings.TrimSpace(out[i].Status), evidence.Status) {
			continue
		}
		out[i].Status = evidence.Status
		out[i].LastEvent = evidence.Event
		out[i].EventDate = evidence.Date
		if evidence.Shed != "" {
			out[i].LastShed = evidence.Shed
		}
		overrides++
	}
	return out, overrides
}

func latestBackfillEvidenceByKey(events []Event) map[string]latestBackfillEvidence {
	latest := map[string]latestBackfillEvidence{}
	for _, event := range events {
		scope := derivedScopeKey(event.Farm)
		normalized := CanonicalIdentifier(event.GoatID)
		lifecycle, _ := lifecycleForEvent(event)
		status := candidateStatusFromLifecycle(lifecycle)
		if scope == "" || normalized == "" || status == "" {
			continue
		}
		key := scope + "|" + normalized
		next := latestBackfillEvidence{
			Status: status,
			Event:  strings.TrimSpace(event.Event),
			Date:   strings.TrimSpace(event.Date),
			Shed:   destinationShed(event),
		}
		current, ok := latest[key]
		currentEvent := Event{Date: current.Date, Event: current.Event}
		if !ok || eventDateAfter(Event{Date: next.Date, Event: next.Event}, &currentEvent) {
			latest[key] = next
		}
	}
	return latest
}

func latestLocationLifecycleRows(events []Event) map[string]BackfillPlanRow {
	rows := map[string]BackfillPlanRow{}
	for key, evidence := range latestBackfillEvidenceByKey(events) {
		lifecycle, ok := lifecycleFromCandidateStatus(evidence.Status)
		scope, normalized, split := strings.Cut(key, "|")
		if !ok || !split || scope == "" || normalized == "" {
			continue
		}
		farm := strings.TrimPrefix(scope, "park:")
		rows[key] = BackfillPlanRow{
			Source:          "bq_latest_locations",
			Farm:            farm,
			ScopeKey:        scope,
			OldTag:          normalized,
			NormalizedValue: normalized,
			LastShed:        evidence.Shed,
			CandidateStatus: evidence.Status,
			Lifecycle:       lifecycle,
			IdentityState:   "clean",
		}
	}
	return rows
}

func candidateStatusFromLifecycle(lifecycle string) string {
	switch lifecycle {
	case "alive":
		return "Active"
	case "sold":
		return "Sold"
	case "dead":
		return "Dead"
	default:
		return ""
	}
}

// derivedScopeKey is the canonical park scope key for an old_tag, derived from
// the farm code so the command never blindly trusts the CSV scope_key column.
func derivedScopeKey(farm string) string {
	code := strings.ToUpper(strings.TrimSpace(farm))
	if code == "" {
		return ""
	}
	return "park:" + code
}

// planBackfill produces a deterministic create/skip decision per candidate.
// existing is the set of active old_tag identity keys already in the DB.
// It never mutates its inputs.
func planBackfill(candidates []Candidate, existing map[string]struct{}) []BackfillPlanRow {
	// First pass: group by identity key to detect intra-CSV duplicates and
	// whether duplicates conflict on the attributes we would write.
	type group struct {
		count      int
		conflict   bool
		firstAttrs string
	}
	groups := map[string]*group{}
	attrFingerprint := func(c Candidate) string {
		lifecycle, _ := lifecycleFromCandidateStatus(c.Status)
		return strings.ToLower(strings.TrimSpace(c.Breed)) + "|" +
			normalizeSex(c.Gender) + "|" + lifecycle
	}
	for _, c := range candidates {
		scope := derivedScopeKey(c.Farm)
		normalized := CanonicalIdentifier(c.OldTag)
		if scope == "" || normalized == "" {
			continue
		}
		key := scope + "|" + normalized
		g := groups[key]
		if g == nil {
			groups[key] = &group{count: 1, firstAttrs: attrFingerprint(c)}
			continue
		}
		g.count++
		if g.firstAttrs != attrFingerprint(c) {
			g.conflict = true
		}
	}

	created := map[string]struct{}{}
	rows := make([]BackfillPlanRow, 0, len(candidates))
	for _, c := range candidates {
		scope := derivedScopeKey(c.Farm)
		normalized := CanonicalIdentifier(c.OldTag)
		sex := normalizeSex(c.Gender)
		if sex == "" && strings.TrimSpace(c.Gender) != "" {
			// Gender was supplied but is not one of male/female; record the
			// supported 'unknown' value rather than NULL so "supplied but
			// unresolvable" stays distinct from "never supplied".
			sex = "unknown"
		}
		lifecycle, lifecycleOK := lifecycleFromCandidateStatus(c.Status)

		row := BackfillPlanRow{
			RowNumber:       c.RowNumber,
			Source:          c.Source,
			Farm:            c.Farm,
			ScopeKey:        scope,
			OldTag:          strings.TrimSpace(c.OldTag),
			NormalizedValue: normalized,
			Breed:           strings.TrimSpace(c.Breed),
			Sex:             sex,
			LastShed:        strings.TrimSpace(c.LastShed),
			CandidateStatus: strings.TrimSpace(c.Status),
			Lifecycle:       lifecycle,
			IdentityState:   "clean",
		}

		skip := func(reason, detail string) {
			row.Action = "skip"
			row.SkipReason = reason
			row.Detail = detail
			row.IdentityState = ""
			row.Lifecycle = ""
		}

		switch {
		case strings.TrimSpace(c.OldTag) == "" || normalized == "":
			skip("missing_identity", "empty or non-canonical old_tag/goat_id")
		case scope == "":
			skip("missing_identity", "empty farm/scope_key")
		case !strings.EqualFold(strings.TrimSpace(c.ScopeKey), scope):
			skip("scope_mismatch", fmt.Sprintf("csv scope_key %q != derived %q", c.ScopeKey, scope))
		case !lifecycleOK:
			skip("unsupported_status", fmt.Sprintf("status %q has no supported lifecycle", c.Status))
		default:
			key := scope + "|" + normalized
			g := groups[key]
			switch {
			case existsKey(existing, key):
				skip("already_present", "active old_tag passport already exists")
			case g != nil && g.count > 1 && g.conflict:
				skip("ambiguous_duplicate", fmt.Sprintf("%d conflicting CSV rows share this old_tag/scope", g.count))
			case g != nil && g.count > 1:
				if _, done := created[key]; done {
					skip("duplicate_in_csv", "identical duplicate of an earlier candidate row")
				} else {
					created[key] = struct{}{}
					row.Action = "create"
				}
			default:
				created[key] = struct{}{}
				row.Action = "create"
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func existsKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// attachLocations resolves deterministic park/shed locations for create rows.
// farm_id is intentionally left unset: parks (CBE/CPT) are top-level here and
// existing goats carry no farm_id.
func attachLocations(rows []BackfillPlanRow, parks map[string]string, sheds map[string]LocationTarget) {
	for i := range rows {
		if rows[i].Action != "create" {
			continue
		}
		farmCode := strings.ToUpper(strings.TrimSpace(rows[i].Farm))
		parkID := parks[farmCode]
		shedID := ""
		// shed text comes from the candidate's last_shed; resolve via the same
		// alias table the reconciler uses.
		if shed := strings.TrimSpace(rows[i].LastShed); shed != "" && shed != "-" {
			if target, ok := sheds[locationAliasKey(rows[i].Farm, shed)]; ok {
				shedID = target.LocationID
				if target.ParentLocationID != "" {
					parkID = target.ParentLocationID
				}
			}
		}
		if parkID == "" {
			// Fail closed: a park-scoped old tag whose farm/park is not a known
			// active location cannot be placed deterministically and would
			// pollute tenant totals while staying invisible to park/shed
			// counters. Skip and export instead of creating a location-less goat.
			reason := "unknown_park"
			detail := fmt.Sprintf("farm %q has no active park location", rows[i].Farm)
			if shedID != "" {
				reason = "unknown_shed_parent"
				detail = fmt.Sprintf("shed %q resolved but has no parent park", rows[i].LastShed)
			}
			rows[i].Action = "skip"
			rows[i].SkipReason = reason
			rows[i].Detail = detail
			rows[i].Lifecycle = ""
			rows[i].IdentityState = ""
			continue
		}
		rows[i].ParkID = parkID
		rows[i].ShedID = shedID
		switch {
		case shedID != "":
			rows[i].CurrentLocID = shedID
		default:
			rows[i].CurrentLocID = parkID
		}
	}
}

func summarizePlan(summary *BackfillSummary, rows []BackfillPlanRow) {
	creates, skips := 0, 0
	for _, r := range rows {
		switch r.Action {
		case "create":
			creates++
			summary.CreatesByLifecycle[r.Lifecycle]++
		case "skip":
			skips++
			summary.SkipsByReason[r.SkipReason]++
		}
	}
	summary.PlannedCreates = creates
	summary.PlannedSkips = skips
}

func expectedTotals(current CountSnapshot, rows []BackfillPlanRow) CountSnapshot {
	expected := current
	for _, r := range rows {
		if r.Action != "create" {
			continue
		}
		expected.Total++
		expected.IdentityClean++ // creates are identity_state='clean'
		switch r.Lifecycle {
		case "alive":
			expected.Alive++
		case "sold":
			expected.Sold++
		case "dead":
			expected.Dead++
		case "inactive":
			expected.Inactive++
		}
	}
	return expected
}

type existingBackfillGoat struct {
	GoatID    string
	DisplayID string
	Lifecycle string
}

func desiredBackfillRowsByKey(candidates []Candidate) map[string]BackfillPlanRow {
	rows := planBackfill(candidates, map[string]struct{}{})
	desired := map[string]BackfillPlanRow{}
	for _, row := range rows {
		if row.Action != "create" || row.Lifecycle == "" {
			continue
		}
		desired[row.ScopeKey+"|"+row.NormalizedValue] = row
	}
	return desired
}

func applyExpectedLifecycleUpdateDelta(next *CountSnapshot, desired map[string]BackfillPlanRow, existing map[string]existingBackfillGoat) int {
	planned := 0
	for key, row := range desired {
		current, ok := existing[key]
		if !ok || current.Lifecycle == row.Lifecycle {
			continue
		}
		decrementLifecycle(next, current.Lifecycle)
		incrementLifecycle(next, row.Lifecycle)
		planned++
	}
	return planned
}

func incrementLifecycle(snapshot *CountSnapshot, lifecycle string) {
	switch lifecycle {
	case "alive":
		snapshot.Alive++
	case "sold":
		snapshot.Sold++
	case "dead":
		snapshot.Dead++
	case "inactive":
		snapshot.Inactive++
	}
}

func decrementLifecycle(snapshot *CountSnapshot, lifecycle string) {
	switch lifecycle {
	case "alive":
		snapshot.Alive--
	case "sold":
		snapshot.Sold--
	case "dead":
		snapshot.Dead--
	case "inactive":
		snapshot.Inactive--
	}
}

// applyCandidateBackfill performs the writes inside one transaction guarded by
// the shared bq-reconcile advisory lock. It re-plans against fresh in-tx state
// so replay is idempotent.
func applyCandidateBackfill(ctx context.Context, pool *pgxpool.Pool, opts Options, candidates []Candidate, lifecycleRepairRows map[string]BackfillPlanRow, parks map[string]string, sheds map[string]LocationTarget) (int, int, []BackfillPlanRow, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, 0, nil, fmt.Errorf("begin backfill transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Share the reconcile lock so backfill and reconcile never race on goats.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "bq-reconcile:"+opts.TenantID); err != nil {
		return 0, 0, nil, fmt.Errorf("acquire backfill lock: %w", err)
	}

	custodianPartyID, err := resolveMeshaPartyID(ctx, tx)
	if err != nil {
		return 0, 0, nil, err
	}
	existing, err := loadExistingOldTagKeys(ctx, tx, opts.TenantID)
	if err != nil {
		return 0, 0, nil, err
	}
	rows := planBackfill(candidates, existing)
	attachLocations(rows, parks, sheds)
	updates, err := updateExistingBackfillLifecycles(ctx, tx, opts.TenantID, opts.TraceID, lifecycleRepairRows)
	if err != nil {
		return 0, 0, nil, err
	}
	if _, err := mergeObsoleteRFIDBackfillGoats(ctx, tx, opts.TenantID, opts.TraceID); err != nil {
		return 0, 0, nil, err
	}

	applied := 0
	for i := range rows {
		if rows[i].Action != "create" {
			continue
		}
		goatID, displayID, err := insertBackfillGoat(ctx, tx, opts.TenantID, custodianPartyID, rows[i])
		if err != nil {
			return 0, 0, nil, fmt.Errorf("create backfill goat (old_tag %s %s): %w", rows[i].ScopeKey, rows[i].OldTag, err)
		}
		if err := insertBackfillIdentifier(ctx, tx, opts.TenantID, goatID, rows[i]); err != nil {
			return 0, 0, nil, fmt.Errorf("attach old_tag identifier for goat %s: %w", goatID, err)
		}
		if err := insertBackfillAudit(ctx, tx, opts.TenantID, opts.TraceID, goatID, displayID, rows[i]); err != nil {
			return 0, 0, nil, fmt.Errorf("audit backfill goat %s: %w", goatID, err)
		}
		rows[i].GoatID = goatID
		rows[i].DisplayID = displayID
		applied++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, 0, nil, fmt.Errorf("commit backfill: %w", err)
	}
	return applied, updates, rows, nil
}

type obsoleteRFIDBackfillMerge struct {
	BackfillGoatID    string
	BackfillDisplayID string
	SurvivorGoatID    string
	SurvivorDisplayID string
	IdentifierID      string
	NormalizedValue   string
	ScopeKey          string
	BeforeRowVersion  int
	AfterRowVersion   int
}

func mergeObsoleteRFIDBackfillGoats(ctx context.Context, tx pgx.Tx, tenantID, traceID string) (int, error) {
	rows, err := tx.Query(ctx, `
SELECT
  bg.goat_id::text AS backfill_goat_id,
  bg.display_id AS backfill_display_id,
  rg.goat_id::text AS survivor_goat_id,
  rg.display_id AS survivor_display_id,
  bi.identifier_id::text,
  bi.normalized_value,
  bi.scope_key,
  bg.row_version AS before_row_version
FROM goat_identifiers bi
JOIN goats bg
  ON bg.tenant_id = bi.tenant_id
 AND bg.goat_id = bi.goat_id
JOIN goat_identifiers ri
  ON ri.tenant_id = bi.tenant_id
 AND ri.identifier_type = 'rfid'
 AND ri.status = 'active'
 AND ri.normalized_value = bi.normalized_value
JOIN goats rg
  ON rg.tenant_id = ri.tenant_id
 AND rg.goat_id = ri.goat_id
WHERE bi.tenant_id = $1::uuid
  AND bi.identifier_type = 'old_tag'
  AND bi.status = 'active'
  AND bi.source_system = $2
  AND bi.source_record_id LIKE $3
  AND bi.normalized_value ~ '^[0-9]{12,}$'
  AND bg.identity_state <> 'merged'
  AND rg.identity_state <> 'merged'
  AND bg.goat_id <> rg.goat_id
ORDER BY bi.normalized_value, bg.display_id
FOR UPDATE OF bg, bi`, tenantID, backfillSourceSystem, backfillSourceContext+":%")
	if err != nil {
		return 0, fmt.Errorf("merge obsolete RFID-like backfill goats: %w", err)
	}
	defer rows.Close()

	merges := []obsoleteRFIDBackfillMerge{}
	for rows.Next() {
		var merge obsoleteRFIDBackfillMerge
		if err := rows.Scan(
			&merge.BackfillGoatID,
			&merge.BackfillDisplayID,
			&merge.SurvivorGoatID,
			&merge.SurvivorDisplayID,
			&merge.IdentifierID,
			&merge.NormalizedValue,
			&merge.ScopeKey,
			&merge.BeforeRowVersion,
		); err != nil {
			return 0, fmt.Errorf("scan obsolete RFID-like backfill merge: %w", err)
		}
		merges = append(merges, merge)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("read obsolete RFID-like backfill merges: %w", err)
	}
	for i := range merges {
		merge := &merges[i]
		if _, err := tx.Exec(ctx, `
UPDATE goat_identifiers
SET status = 'retired',
    valid_to = now(),
    is_primary_for_goat = false,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND identifier_id = $2::uuid`, tenantID, merge.IdentifierID); err != nil {
			return 0, fmt.Errorf("retire obsolete RFID-like backfill identifier %s: %w", merge.IdentifierID, err)
		}
		if err := tx.QueryRow(ctx, `
UPDATE goats
SET identity_state = 'merged',
    merged_into_goat_id = $3::uuid,
    row_version = row_version + 1,
    updated_at = now()
WHERE tenant_id = $1::uuid
  AND goat_id = $2::uuid
RETURNING row_version`, tenantID, merge.BackfillGoatID, merge.SurvivorGoatID).Scan(&merge.AfterRowVersion); err != nil {
			return 0, fmt.Errorf("merge obsolete RFID-like backfill goat %s: %w", merge.BackfillGoatID, err)
		}
		if err := insertObsoleteRFIDBackfillMergeAudit(ctx, tx, tenantID, traceID, *merge); err != nil {
			return 0, err
		}
	}
	return len(merges), nil
}

func insertObsoleteRFIDBackfillMergeAudit(ctx context.Context, tx pgx.Tx, tenantID, traceID string, merge obsoleteRFIDBackfillMerge) error {
	before := map[string]any{
		"goat_id":          merge.BackfillGoatID,
		"display_id":       merge.BackfillDisplayID,
		"identity_state":   "clean",
		"normalized_value": merge.NormalizedValue,
		"scope_key":        merge.ScopeKey,
		"row_version":      merge.BeforeRowVersion,
	}
	after := map[string]any{
		"goat_id":             merge.BackfillGoatID,
		"display_id":          merge.BackfillDisplayID,
		"identity_state":      "merged",
		"merged_into_goat_id": merge.SurvivorGoatID,
		"row_version":         merge.AfterRowVersion,
	}
	metadata := map[string]any{
		"source_system":       backfillSourceSystem,
		"source_context":      backfillSourceContext,
		"command":             "bq-reconcile --backfill-candidates-csv",
		"merge_reason":        "rfid_like_backfill_superseded_by_active_rfid",
		"survivor_goat_id":    merge.SurvivorGoatID,
		"survivor_display_id": merge.SurvivorDisplayID,
		"identifier_id":       merge.IdentifierID,
		"normalized_value":    merge.NormalizedValue,
		"scope_key":           merge.ScopeKey,
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	metadataJSON, _ := json.Marshal(metadata)
	_, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  before_state,
  after_state,
  metadata,
  trace_id
) VALUES (
  $1::uuid,
  'system',
  'goat.old_tag_backfill_merged_into_rfid',
  'goat',
  $2::uuid,
  $3::jsonb,
  $4::jsonb,
  $5::jsonb,
  $6
)`, tenantID, merge.BackfillGoatID, beforeJSON, afterJSON, metadataJSON, traceID)
	if err != nil {
		return fmt.Errorf("audit obsolete RFID-like backfill merge %s: %w", merge.BackfillGoatID, err)
	}
	return nil
}

func insertBackfillGoat(ctx context.Context, tx pgx.Tx, tenantID, custodianPartyID string, row BackfillPlanRow) (string, string, error) {
	breedID := ""
	if row.Breed != "" {
		id, err := ensureBQBreed(ctx, tx, row.Breed)
		if err != nil {
			return "", "", fmt.Errorf("resolve breed %q: %w", row.Breed, err)
		}
		breedID = id
	}
	var goatID, displayID string
	err := tx.QueryRow(ctx, `
INSERT INTO goats (
  tenant_id,
  species,
  breed,
  breed_id,
  sex,
  lifecycle_status,
  identity_state,
  custodian_party_id,
  current_location_id,
  park_id,
  shed_id,
  source_confidence,
  created_at,
  updated_at,
  created_by
) VALUES (
  $1::uuid,
  'goat',
  $2,
  $3::uuid,
  $4,
  $5,
  $6,
  $7::uuid,
  $8::uuid,
  $9::uuid,
  $10::uuid,
  $11,
  now(),
  now(),
  NULL
)
RETURNING goat_id::text, display_id`,
		tenantID,
		nullableText(row.Breed),
		nullableUUID(breedID),
		nullableText(row.Sex),
		row.Lifecycle,
		row.IdentityState,
		custodianPartyID,
		nullableUUID(row.CurrentLocID),
		nullableUUID(row.ParkID),
		nullableUUID(row.ShedID),
		0.7, // bridge-sourced deterministic evidence; below in-app capture confidence
	).Scan(&goatID, &displayID)
	if err != nil {
		return "", "", err
	}
	return goatID, displayID, nil
}

func insertBackfillIdentifier(ctx context.Context, tx pgx.Tx, tenantID, goatID string, row BackfillPlanRow) error {
	_, err := tx.Exec(ctx, `
INSERT INTO goat_identifiers (
  tenant_id,
  goat_id,
  identifier_type,
  identifier_value,
  normalized_value,
  scope_key,
  is_primary_for_goat,
  status,
  valid_from,
  source_system,
  source_record_id,
  normalizer_version,
  confidence
) VALUES (
  $1::uuid,
  $2::uuid,
  'old_tag',
  $3,
  $4,
  $5,
  true,
  'active',
  now(),
  $6,
  $7,
  $8,
  0.7
)`,
		tenantID,
		goatID,
		row.OldTag,
		row.NormalizedValue,
		row.ScopeKey,
		backfillSourceSystem,
		fmt.Sprintf("%s:%s:%s", backfillSourceContext, row.ScopeKey, row.NormalizedValue),
		backfillNormalizerVersion,
	)
	return err
}

func insertBackfillAudit(ctx context.Context, tx pgx.Tx, tenantID, traceID, goatID, displayID string, row BackfillPlanRow) error {
	after := map[string]any{
		"goat_id":             goatID,
		"display_id":          displayID,
		"old_tag":             row.OldTag,
		"normalized_value":    row.NormalizedValue,
		"scope_key":           row.ScopeKey,
		"lifecycle_status":    row.Lifecycle,
		"identity_state":      row.IdentityState,
		"sex":                 row.Sex,
		"breed":               row.Breed,
		"park_id":             row.ParkID,
		"shed_id":             row.ShedID,
		"current_location_id": row.CurrentLocID,
	}
	metadata := map[string]any{
		"source_system":    backfillSourceSystem,
		"source_context":   backfillSourceContext,
		"candidate_source": row.Source,
		"candidate_status": row.CandidateStatus,
		"candidate_row":    row.RowNumber,
		"command":          "bq-reconcile --backfill-candidates-csv",
	}
	afterJSON, _ := json.Marshal(after)
	metadataJSON, _ := json.Marshal(metadata)
	_, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  before_state,
  after_state,
  metadata,
  trace_id
) VALUES (
  $1::uuid,
  'system',
  'goat.old_tag_backfill_created',
  'goat',
  $2::uuid,
  NULL,
  $3::jsonb,
  $4::jsonb,
  $5
)`, tenantID, goatID, afterJSON, metadataJSON, traceID)
	return err
}

func insertBackfillLifecycleRepairAudit(ctx context.Context, tx pgx.Tx, tenantID, traceID string, repair existingBackfillLifecycleRepair) error {
	before := map[string]any{
		"goat_id":          repair.GoatID,
		"display_id":       repair.DisplayID,
		"old_tag":          repair.Row.OldTag,
		"normalized_value": repair.Row.NormalizedValue,
		"scope_key":        repair.Row.ScopeKey,
		"lifecycle_status": repair.BeforeLifecycle,
		"row_version":      repair.BeforeRowVersion,
	}
	after := map[string]any{
		"goat_id":          repair.GoatID,
		"display_id":       repair.DisplayID,
		"old_tag":          repair.Row.OldTag,
		"normalized_value": repair.Row.NormalizedValue,
		"scope_key":        repair.Row.ScopeKey,
		"lifecycle_status": repair.AfterLifecycle,
		"row_version":      repair.AfterRowVersion,
	}
	metadata := map[string]any{
		"source_system":    backfillSourceSystem,
		"source_context":   backfillSourceContext,
		"candidate_source": repair.Row.Source,
		"candidate_status": repair.Row.CandidateStatus,
		"candidate_row":    repair.Row.RowNumber,
		"command":          "bq-reconcile --backfill-candidates-csv",
		"repair_reason":    "latest_location_lifecycle_evidence",
	}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	metadataJSON, _ := json.Marshal(metadata)
	_, err := tx.Exec(ctx, `
INSERT INTO audit_log (
  tenant_id,
  actor_type,
  action,
  resource_type,
  resource_id,
  before_state,
  after_state,
  metadata,
  trace_id
) VALUES (
  $1::uuid,
  'system',
  'goat.old_tag_backfill_lifecycle_repaired',
  'goat',
  $2::uuid,
  $3::jsonb,
  $4::jsonb,
  $5::jsonb,
  $6
)`, tenantID, repair.GoatID, beforeJSON, afterJSON, metadataJSON, traceID)
	return err
}

func resolveMeshaPartyID(ctx context.Context, q queryer) (string, error) {
	rows, err := q.Query(ctx, `
SELECT p.party_id::text
FROM parties p
JOIN orgs o ON o.party_id = p.party_id
WHERE p.party_type = 'org'
  AND p.status = 'active'
  AND o.org_type = 'mesha'
  AND o.status = 'active'
ORDER BY p.party_id`)
	if err != nil {
		return "", fmt.Errorf("find Mesha org party: %w", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", fmt.Errorf("scan Mesha org party: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(ids) {
	case 0:
		return "", errors.New("active Mesha org party not found")
	case 1:
		return ids[0], nil
	default:
		return "", errors.New("active Mesha org party is ambiguous")
	}
}

func loadExistingOldTagKeys(ctx context.Context, q queryer, tenantID string) (map[string]struct{}, error) {
	rows, err := q.Query(ctx, `
SELECT scope_key, normalized_value
FROM goat_identifiers
WHERE tenant_id = $1::uuid
  AND identifier_type = 'old_tag'
  AND status = 'active'`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load existing old_tag identifiers: %w", err)
	}
	defer rows.Close()
	set := map[string]struct{}{}
	for rows.Next() {
		var scope, normalized string
		if err := rows.Scan(&scope, &normalized); err != nil {
			return nil, fmt.Errorf("scan existing old_tag identifier: %w", err)
		}
		// normalized_value is already canonical in the DB; re-canonicalizing is
		// idempotent and guards against any legacy non-canonical stored value.
		set[strings.TrimSpace(scope)+"|"+CanonicalIdentifier(normalized)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return set, nil
}

func loadExistingBackfillOldTagGoats(ctx context.Context, q queryer, tenantID string) (map[string]existingBackfillGoat, error) {
	rows, err := q.Query(ctx, `
SELECT i.scope_key, i.normalized_value, g.goat_id::text, g.display_id, g.lifecycle_status
FROM goat_identifiers i
JOIN goats g
  ON g.tenant_id = i.tenant_id
 AND g.goat_id = i.goat_id
WHERE i.tenant_id = $1::uuid
  AND i.identifier_type = 'old_tag'
  AND i.status = 'active'
  AND i.source_system = $2
  AND g.merged_into_goat_id IS NULL`, tenantID, backfillSourceSystem)
	if err != nil {
		return nil, fmt.Errorf("load existing backfill old_tag goats: %w", err)
	}
	defer rows.Close()
	existing := map[string]existingBackfillGoat{}
	for rows.Next() {
		var scope, normalized, goatID, displayID, lifecycle string
		if err := rows.Scan(&scope, &normalized, &goatID, &displayID, &lifecycle); err != nil {
			return nil, fmt.Errorf("scan existing backfill old_tag goat: %w", err)
		}
		key := strings.TrimSpace(scope) + "|" + CanonicalIdentifier(normalized)
		existing[key] = existingBackfillGoat{
			GoatID:    goatID,
			DisplayID: displayID,
			Lifecycle: strings.TrimSpace(lifecycle),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return existing, nil
}

type existingBackfillLifecycleRepair struct {
	Row              BackfillPlanRow
	GoatID           string
	DisplayID        string
	BeforeLifecycle  string
	AfterLifecycle   string
	BeforeRowVersion int
	AfterRowVersion  int
}

func updateExistingBackfillLifecycles(ctx context.Context, tx pgx.Tx, tenantID, traceID string, desired map[string]BackfillPlanRow) (int, error) {
	applied := 0
	keys := make([]string, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		row := desired[key]
		scope, normalized, ok := strings.Cut(key, "|")
		if !ok || scope == "" || normalized == "" || row.Lifecycle == "" {
			continue
		}
		rows, err := tx.Query(ctx, `
WITH target AS (
  SELECT
    g.goat_id,
    g.display_id,
    g.lifecycle_status AS before_lifecycle,
    g.row_version AS before_row_version
  FROM goats g
  JOIN goat_identifiers i
    ON i.tenant_id = g.tenant_id
   AND i.goat_id = g.goat_id
  WHERE i.tenant_id = $1::uuid
    AND i.identifier_type = 'old_tag'
    AND i.status = 'active'
    AND i.source_system = $5
    AND i.scope_key = $2
    AND i.normalized_value = $3
    AND g.merged_into_goat_id IS NULL
    AND g.lifecycle_status <> $4
  FOR UPDATE OF g
), updated AS (
  UPDATE goats g
  SET lifecycle_status = $4,
      row_version = g.row_version + 1,
      updated_at = now()
  FROM target t
  WHERE g.tenant_id = $1::uuid
    AND g.goat_id = t.goat_id
  RETURNING
    g.goat_id::text,
    g.display_id,
    t.before_lifecycle,
    g.lifecycle_status,
    t.before_row_version,
    g.row_version
)
SELECT goat_id, display_id, before_lifecycle, lifecycle_status, before_row_version, row_version
FROM updated`, tenantID, scope, normalized, row.Lifecycle, backfillSourceSystem)
		if err != nil {
			return 0, fmt.Errorf("update existing backfill lifecycle %s: %w", key, err)
		}
		repairs := []existingBackfillLifecycleRepair{}
		for rows.Next() {
			repair := existingBackfillLifecycleRepair{Row: row}
			if err := rows.Scan(
				&repair.GoatID,
				&repair.DisplayID,
				&repair.BeforeLifecycle,
				&repair.AfterLifecycle,
				&repair.BeforeRowVersion,
				&repair.AfterRowVersion,
			); err != nil {
				rows.Close()
				return 0, fmt.Errorf("scan existing backfill lifecycle repair %s: %w", key, err)
			}
			repairs = append(repairs, repair)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return 0, fmt.Errorf("read existing backfill lifecycle repairs %s: %w", key, err)
		}
		rows.Close()
		for _, repair := range repairs {
			if err := insertBackfillLifecycleRepairAudit(ctx, tx, tenantID, traceID, repair); err != nil {
				return 0, fmt.Errorf("audit existing backfill lifecycle repair %s: %w", key, err)
			}
		}
		applied += len(repairs)
	}
	return applied, nil
}

func loadParkAndShedLookup(ctx context.Context, q queryer, tenantID string) (map[string]string, map[string]LocationTarget, error) {
	parks := map[string]string{}
	prows, err := q.Query(ctx, `
SELECT location_code, location_id::text
FROM locations
WHERE tenant_id = $1::uuid
  AND location_type = 'park'
  AND status = 'active'
  AND location_code IS NOT NULL`, tenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("load park locations: %w", err)
	}
	for prows.Next() {
		var code, id string
		if err := prows.Scan(&code, &id); err != nil {
			prows.Close()
			return nil, nil, fmt.Errorf("scan park location: %w", err)
		}
		parks[strings.ToUpper(strings.TrimSpace(code))] = id
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return nil, nil, err
	}

	sheds := map[string]LocationTarget{}
	srows, err := q.Query(ctx, `
SELECT la.alias_code, l.location_id::text, COALESCE(l.parent_location_id::text, '')::text
FROM location_aliases la
JOIN locations l
  ON l.tenant_id = la.tenant_id
 AND l.location_id = la.canonical_location_id
WHERE la.tenant_id = $1::uuid
  AND la.source_context = $2
  AND l.location_type = 'shed'
  AND l.status = 'active'`, tenantID, aliasSourceContext)
	if err != nil {
		return nil, nil, fmt.Errorf("load BQ shed aliases: %w", err)
	}
	defer srows.Close()
	for srows.Next() {
		var alias, locationID, parentID string
		if err := srows.Scan(&alias, &locationID, &parentID); err != nil {
			return nil, nil, fmt.Errorf("scan BQ shed alias: %w", err)
		}
		sheds[strings.ToUpper(strings.TrimSpace(alias))] = LocationTarget{LocationID: locationID, ParentLocationID: parentID}
	}
	if err := srows.Err(); err != nil {
		return nil, nil, err
	}
	return parks, sheds, nil
}

func snapshotCounts(ctx context.Context, q queryer, tenantID string) (CountSnapshot, error) {
	var snap CountSnapshot
	err := q.QueryRow(ctx, `
SELECT
  count(*),
  count(*) FILTER (WHERE lifecycle_status = 'alive'),
  count(*) FILTER (WHERE lifecycle_status = 'sold'),
  count(*) FILTER (WHERE lifecycle_status = 'dead'),
  count(*) FILTER (WHERE lifecycle_status = 'inactive'),
  count(*) FILTER (WHERE identity_state = 'clean'),
  count(*) FILTER (WHERE identity_state = 'needs_review')
FROM goats
WHERE tenant_id = $1::uuid
  AND identity_state <> 'merged'`, tenantID).Scan(
		&snap.Total, &snap.Alive, &snap.Sold, &snap.Dead, &snap.Inactive,
		&snap.IdentityClean, &snap.IdentityReview,
	)
	if err != nil {
		return snap, fmt.Errorf("snapshot goat counts: %w", err)
	}
	if err := q.QueryRow(ctx, `
SELECT count(*)
FROM identity_conflicts
WHERE tenant_id = $1::uuid
  AND state IN ('open', 'needs_field_check')`, tenantID).Scan(&snap.OpenConflicts); err != nil {
		return snap, fmt.Errorf("snapshot open conflicts: %w", err)
	}
	return snap, nil
}

// writeReports writes deterministic backfilled/skipped CSV artifacts. The
// directory is created if missing. A blank dir disables reporting.
func writeReports(dir string, rows []BackfillPlanRow) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create report dir: %w", err)
	}
	sorted := append([]BackfillPlanRow(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].RowNumber < sorted[j].RowNumber })

	if err := writeCSV(filepath.Join(dir, "old-tag-backfill-created.csv"),
		[]string{"row_number", "candidate_source", "farm", "scope_key", "old_tag", "normalized_value", "breed", "sex", "lifecycle_status", "identity_state", "park_id", "shed_id", "current_location_id", "goat_id", "display_id"},
		sorted, func(r BackfillPlanRow) ([]string, bool) {
			if r.Action != "create" {
				return nil, false
			}
			return []string{
				itoa(r.RowNumber), r.Source, r.Farm, r.ScopeKey, r.OldTag, r.NormalizedValue,
				r.Breed, r.Sex, r.Lifecycle, r.IdentityState, r.ParkID, r.ShedID, r.CurrentLocID, r.GoatID, r.DisplayID,
			}, true
		}); err != nil {
		return err
	}

	return writeCSV(filepath.Join(dir, "old-tag-backfill-skipped.csv"),
		[]string{"row_number", "candidate_source", "farm", "scope_key", "old_tag", "skip_reason", "detail"},
		sorted, func(r BackfillPlanRow) ([]string, bool) {
			if r.Action != "skip" {
				return nil, false
			}
			return []string{itoa(r.RowNumber), r.Source, r.Farm, r.ScopeKey, r.OldTag, r.SkipReason, r.Detail}, true
		})
}

func writeCSV(path string, header []string, rows []BackfillPlanRow, project func(BackfillPlanRow) ([]string, bool)) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		rec, ok := project(r)
		if !ok {
			continue
		}
		if err := w.Write(rec); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(blank)"
	}
	return strings.TrimSpace(s)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
