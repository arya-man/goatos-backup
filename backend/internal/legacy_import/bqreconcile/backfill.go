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
	PlannedCreates     int            `json:"planned_creates"`
	PlannedSkips       int            `json:"planned_skips"`
	Applied            int            `json:"applied_creates"`
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

	summary := &BackfillSummary{
		DryRun:             !opts.Execute,
		TenantID:           opts.TenantID,
		TraceID:            opts.TraceID,
		CandidatesCSV:      opts.CandidatesCSVPath,
		CandidatesCSVHash:  hash,
		CandidatesRead:     len(candidates),
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

	if !opts.Execute {
		if err := writeReports(summary.ReportDir, rows); err != nil {
			return nil, err
		}
		return summary, nil
	}

	applied, finalRows, err := applyCandidateBackfill(ctx, pool, opts, candidates, parks, sheds)
	if err != nil {
		return nil, err
	}
	summary.Applied = applied
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
	case "inactive":
		return "inactive", true
	default:
		return "", false
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

// applyCandidateBackfill performs the writes inside one transaction guarded by
// the shared bq-reconcile advisory lock. It re-plans against fresh in-tx state
// so replay is idempotent.
func applyCandidateBackfill(ctx context.Context, pool *pgxpool.Pool, opts Options, candidates []Candidate, parks map[string]string, sheds map[string]LocationTarget) (int, []BackfillPlanRow, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, nil, fmt.Errorf("begin backfill transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Share the reconcile lock so backfill and reconcile never race on goats.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "bq-reconcile:"+opts.TenantID); err != nil {
		return 0, nil, fmt.Errorf("acquire backfill lock: %w", err)
	}

	custodianPartyID, err := resolveMeshaPartyID(ctx, tx)
	if err != nil {
		return 0, nil, err
	}
	existing, err := loadExistingOldTagKeys(ctx, tx, opts.TenantID)
	if err != nil {
		return 0, nil, err
	}
	rows := planBackfill(candidates, existing)
	attachLocations(rows, parks, sheds)

	applied := 0
	for i := range rows {
		if rows[i].Action != "create" {
			continue
		}
		goatID, displayID, err := insertBackfillGoat(ctx, tx, opts.TenantID, custodianPartyID, rows[i])
		if err != nil {
			return 0, nil, fmt.Errorf("create backfill goat (old_tag %s %s): %w", rows[i].ScopeKey, rows[i].OldTag, err)
		}
		if err := insertBackfillIdentifier(ctx, tx, opts.TenantID, goatID, rows[i]); err != nil {
			return 0, nil, fmt.Errorf("attach old_tag identifier for goat %s: %w", goatID, err)
		}
		if err := insertBackfillAudit(ctx, tx, opts.TenantID, opts.TraceID, goatID, displayID, rows[i]); err != nil {
			return 0, nil, fmt.Errorf("audit backfill goat %s: %w", goatID, err)
		}
		rows[i].GoatID = goatID
		rows[i].DisplayID = displayID
		applied++
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, nil, fmt.Errorf("commit backfill: %w", err)
	}
	return applied, rows, nil
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
