package bqreconcile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	eventTypeBirth    = "birth"
	eventTypeDeath    = "death"
	eventTypePurchase = "purchase"
	eventTypeSale     = "sale"
	eventTypeShifting = "shifting"

	aliasSourceContext = "legacy_bq_dashboard_shed"
)

var nonAlnum = regexp.MustCompile(`[^A-Z0-9]+`)

type Event struct {
	Age         string `json:"age"`
	Breed       string `json:"breed"`
	CurrentShed string `json:"current_shed"`
	Date        string `json:"date"`
	DstShed     string `json:"dst_shed"`
	Event       string `json:"event"`
	Farm        string `json:"farm"`
	FarmGoatID  string `json:"farm_goat_id"`
	Gender      string `json:"gender"`
	GoatID      string `json:"goat_id"`
	SrcShed     string `json:"src_shed"`
}

type LocalGoat struct {
	GoatID            string
	LifecycleStatus   string
	CurrentLocationID string
	FarmID            string
	ParkID            string
	ShedID            string
	Identifiers       []LocalIdentifier
}

type LocalIdentifier struct {
	IdentifierType  string
	NormalizedValue string
	ScopeKey        string
}

type LocationLookup struct {
	ShedsByAlias map[string]LocationTarget
	ParksByCode  map[string]string
}

type LocationTarget struct {
	LocationID       string
	ParentLocationID string
}

type Options struct {
	TenantID      string
	EventsPath    string
	LocationsPath string
	Execute       bool
	TraceID       string
}

type Summary struct {
	DryRun                  bool           `json:"dry_run"`
	EventsRead              int            `json:"events_read"`
	LocalGoatsRead          int            `json:"local_goats_read"`
	MatchedGoats            int            `json:"matched_goats"`
	UnmatchedLocalGoats     int            `json:"unmatched_local_goats"`
	AmbiguousBQKeys         int            `json:"ambiguous_bq_keys"`
	SkippedBQEvents         int            `json:"skipped_bq_events"`
	LifecycleUpdates        map[string]int `json:"lifecycle_updates"`
	LocationShedUpdates     int            `json:"location_shed_updates"`
	LocationParkOnlyUpdates int            `json:"location_park_only_updates"`
	NoLocationEvidence      int            `json:"no_location_evidence"`
	PatchesPlanned          int            `json:"patches_planned"`
	PatchesApplied          int            `json:"patches_applied"`
}

type patch struct {
	GoatID          string
	BeforeLifecycle string
	AfterLifecycle  string
	BeforeCurrent   string
	BeforeFarm      string
	BeforePark      string
	BeforeShed      string
	AfterCurrent    string
	AfterFarm       string
	AfterPark       string
	AfterShed       string
	MatchedKeys     []string
	LatestEventDate string
	LatestFarmCode  string
	LatestShed      string
}

type goatEvidence struct {
	events         []Event
	keys           map[string]struct{}
	hasBirth       bool
	hasDeath       bool
	hasPurchase    bool
	hasSale        bool
	hasShifting    bool
	latestLocation *Event
}

func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) (*Summary, error) {
	if strings.TrimSpace(opts.TenantID) == "" {
		return nil, errors.New("tenant-id is required")
	}
	if strings.TrimSpace(opts.EventsPath) == "" {
		return nil, errors.New("events-json is required")
	}
	events, err := ReadEventsFile(opts.EventsPath)
	if err != nil {
		return nil, err
	}
	locationEvents := []Event(nil)
	if strings.TrimSpace(opts.LocationsPath) != "" {
		locationEvents, err = ReadEventsFile(opts.LocationsPath)
		if err != nil {
			return nil, err
		}
	}
	goats, err := loadLocalGoats(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	locations, err := loadLocationLookup(ctx, pool, opts.TenantID)
	if err != nil {
		return nil, err
	}
	summary, patches := PlanWithLocations(events, locationEvents, goats, locations)
	summary.DryRun = !opts.Execute
	if !opts.Execute {
		return summary, nil
	}
	applied, err := applyPatches(ctx, pool, opts.TenantID, opts.TraceID, patches)
	if err != nil {
		return nil, err
	}
	summary.PatchesApplied = applied
	return summary, nil
}

func ReadEventsFile(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read BQ events JSON: %w", err)
	}
	return ReadEvents(bytes.NewReader(data))
}

func ReadEvents(r io.Reader) ([]Event, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read BQ events: %w", err)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("BQ events input is empty")
	}
	var events []Event
	if data[0] == '[' {
		if err := json.Unmarshal(data, &events); err != nil {
			return nil, fmt.Errorf("parse BQ events JSON array: %w", err)
		}
		return events, nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return nil, fmt.Errorf("parse BQ events JSONL: %w", err)
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan BQ events JSONL: %w", err)
	}
	return events, nil
}

func Plan(events []Event, goats []LocalGoat, locations LocationLookup) (*Summary, []patch) {
	return PlanWithLocations(events, nil, goats, locations)
}

func PlanWithLocations(events []Event, locationEvents []Event, goats []LocalGoat, locations LocationLookup) (*Summary, []patch) {
	summary := &Summary{
		EventsRead:       len(events),
		LocalGoatsRead:   len(goats),
		LifecycleUpdates: map[string]int{},
	}
	keyToGoat := map[string]string{}
	ambiguousKeys := map[string]struct{}{}
	byGoat := map[string]LocalGoat{}
	for _, goat := range goats {
		byGoat[goat.GoatID] = goat
		for _, identifier := range goat.Identifiers {
			key := localIdentifierKey(identifier)
			if key == "" {
				continue
			}
			if existing, ok := keyToGoat[key]; ok && existing != goat.GoatID {
				ambiguousKeys[key] = struct{}{}
				delete(keyToGoat, key)
				continue
			}
			if _, ambiguous := ambiguousKeys[key]; !ambiguous {
				keyToGoat[key] = goat.GoatID
			}
		}
	}
	summary.AmbiguousBQKeys = len(ambiguousKeys)

	evidenceByGoat := map[string]*goatEvidence{}
	for _, event := range events {
		key := bqEventKey(event)
		if key == "" {
			summary.SkippedBQEvents++
			continue
		}
		goatID, ok := keyToGoat[key]
		if !ok {
			summary.SkippedBQEvents++
			continue
		}
		ev := evidenceByGoat[goatID]
		if ev == nil {
			ev = &goatEvidence{keys: map[string]struct{}{}}
			evidenceByGoat[goatID] = ev
		}
		ev.events = append(ev.events, event)
		ev.keys[key] = struct{}{}
		switch canonicalEventType(event.Event) {
		case eventTypeBirth:
			ev.hasBirth = true
		case eventTypeDeath:
			ev.hasDeath = true
		case eventTypePurchase:
			ev.hasPurchase = true
		case eventTypeSale:
			ev.hasSale = true
		case eventTypeShifting:
			ev.hasShifting = true
		}
	}
	locationSource := locationEvents
	if len(locationSource) == 0 {
		locationSource = events
	}
	for _, event := range locationSource {
		if destinationShed(event) == "" {
			continue
		}
		key := bqEventKey(event)
		if key == "" {
			continue
		}
		goatID, ok := keyToGoat[key]
		if !ok {
			continue
		}
		ev := evidenceByGoat[goatID]
		if ev == nil {
			ev = &goatEvidence{keys: map[string]struct{}{}}
			evidenceByGoat[goatID] = ev
		}
		if eventDateAfter(event, ev.latestLocation) {
			copied := event
			ev.latestLocation = &copied
		}
	}
	summary.MatchedGoats = len(evidenceByGoat)
	summary.UnmatchedLocalGoats = len(goats) - len(evidenceByGoat)

	patches := make([]patch, 0)
	for goatID, evidence := range evidenceByGoat {
		goat := byGoat[goatID]
		nextLifecycle := targetLifecycle(evidence)
		nextCurrent := goat.CurrentLocationID
		nextFarm := goat.FarmID
		nextPark := goat.ParkID
		nextShed := goat.ShedID
		latestFarm := ""
		latestShed := ""
		latestDate := ""
		if evidence.latestLocation != nil {
			latestFarm = farmCode(*evidence.latestLocation)
			latestShed = destinationShed(*evidence.latestLocation)
			latestDate = strings.TrimSpace(evidence.latestLocation.Date)
			aliasKey := locationAliasKey(latestFarm, latestShed)
			if target, ok := locations.ShedsByAlias[aliasKey]; ok {
				nextCurrent = target.LocationID
				nextPark = target.ParentLocationID
				nextShed = target.LocationID
				nextFarm = ""
			} else if parkID := locations.ParksByCode[latestFarm]; parkID != "" {
				nextCurrent = parkID
				nextPark = parkID
				nextShed = ""
				nextFarm = ""
			} else {
				summary.NoLocationEvidence++
			}
		} else {
			summary.NoLocationEvidence++
		}
		if nextLifecycle == "" {
			nextLifecycle = goat.LifecycleStatus
		}
		if nextLifecycle == goat.LifecycleStatus &&
			nextCurrent == goat.CurrentLocationID &&
			nextFarm == goat.FarmID &&
			nextPark == goat.ParkID &&
			nextShed == goat.ShedID {
			continue
		}
		if nextLifecycle != goat.LifecycleStatus {
			summary.LifecycleUpdates[nextLifecycle]++
		}
		if nextShed != "" && nextShed != goat.ShedID {
			summary.LocationShedUpdates++
		} else if nextShed == "" && nextPark != "" && nextPark != goat.ParkID {
			summary.LocationParkOnlyUpdates++
		}
		patches = append(patches, patch{
			GoatID:          goatID,
			BeforeLifecycle: goat.LifecycleStatus,
			AfterLifecycle:  nextLifecycle,
			BeforeCurrent:   goat.CurrentLocationID,
			BeforeFarm:      goat.FarmID,
			BeforePark:      goat.ParkID,
			BeforeShed:      goat.ShedID,
			AfterCurrent:    nextCurrent,
			AfterFarm:       nextFarm,
			AfterPark:       nextPark,
			AfterShed:       nextShed,
			MatchedKeys:     sortedKeys(evidence.keys),
			LatestEventDate: latestDate,
			LatestFarmCode:  latestFarm,
			LatestShed:      latestShed,
		})
	}
	summary.PatchesPlanned = len(patches)
	return summary, patches
}

func targetLifecycle(evidence *goatEvidence) string {
	if evidence.hasDeath {
		return "dead"
	}
	if evidence.hasSale {
		return "sold"
	}
	if evidence.hasBirth || evidence.hasPurchase || evidence.hasShifting {
		return "alive"
	}
	return ""
}

func localIdentifierKey(identifier LocalIdentifier) string {
	value := CanonicalIdentifier(identifier.NormalizedValue)
	if value == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(identifier.IdentifierType)) {
	case "rfid":
		return "rfid:global:" + value
	case "old_tag":
		scope := strings.ToLower(strings.TrimSpace(identifier.ScopeKey))
		if scope == "" {
			return ""
		}
		return "old_tag:" + scope + ":" + value
	default:
		return ""
	}
}

func bqEventKey(event Event) string {
	value := CanonicalIdentifier(event.GoatID)
	if value == "" {
		return ""
	}
	if isRFIDLike(value) {
		return "rfid:global:" + value
	}
	farm := farmCode(event)
	if farm == "" {
		return ""
	}
	return "old_tag:park:" + strings.ToLower(farm) + ":" + value
}

func CanonicalIdentifier(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if strings.ContainsAny(s, "eE") {
		if normalized, ok := canonicalScientific(s); ok {
			s = normalized
		}
	} else if strings.Contains(s, ".") {
		left, right, ok := strings.Cut(s, ".")
		if ok && strings.Trim(right, "0") == "" {
			s = left
		}
	}
	s = strings.ToUpper(s)
	return nonAlnum.ReplaceAllString(s, "")
}

func canonicalScientific(raw string) (string, bool) {
	f, _, err := big.ParseFloat(raw, 10, 256, big.ToNearestEven)
	if err != nil {
		return "", false
	}
	i, _ := f.Int(nil)
	if i == nil {
		return "", false
	}
	return i.String(), true
}

func isRFIDLike(value string) bool {
	if len(value) < 12 {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func canonicalEventType(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func farmCode(event Event) string {
	if farm := strings.TrimSpace(event.Farm); farm != "" {
		return strings.ToUpper(nonAlnum.ReplaceAllString(farm, ""))
	}
	id := strings.TrimSpace(event.FarmGoatID)
	var b strings.Builder
	for _, ch := range id {
		if ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' {
			b.WriteRune(ch)
			continue
		}
		break
	}
	return strings.ToUpper(b.String())
}

func locationAliasKey(farm, shed string) string {
	farm = strings.ToUpper(strings.TrimSpace(farm))
	shed = strings.ToUpper(strings.TrimSpace(shed))
	if farm == "" || shed == "" {
		return ""
	}
	return farm + ":" + shed
}

func destinationShed(event Event) string {
	if shed := strings.TrimSpace(event.DstShed); shed != "" {
		return shed
	}
	return strings.TrimSpace(event.CurrentShed)
}

func eventDateAfter(candidate Event, current *Event) bool {
	if current == nil {
		return true
	}
	candidateDate := strings.TrimSpace(candidate.Date)
	currentDate := strings.TrimSpace(current.Date)
	if candidateDate == "" {
		return false
	}
	if currentDate == "" {
		return true
	}
	return candidateDate >= currentDate
}

func sortedKeys(keys map[string]struct{}) []string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func loadLocalGoats(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]LocalGoat, error) {
	rows, err := pool.Query(ctx, `
SELECT
  g.goat_id::text,
  g.lifecycle_status,
  COALESCE(g.current_location_id::text, '')::text,
  COALESCE(g.farm_id::text, '')::text,
  COALESCE(g.park_id::text, '')::text,
  COALESCE(g.shed_id::text, '')::text,
  gi.identifier_type,
  gi.normalized_value,
  gi.scope_key
FROM goats g
JOIN goat_identifiers gi
  ON gi.tenant_id = g.tenant_id
 AND gi.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid
  AND g.identity_state <> 'merged'
  AND gi.status = 'active'
  AND gi.identifier_type IN ('rfid', 'old_tag')
ORDER BY g.goat_id::text, gi.identifier_type, gi.normalized_value`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("load local goat identifiers: %w", err)
	}
	defer rows.Close()

	byID := map[string]*LocalGoat{}
	order := []string{}
	for rows.Next() {
		var goatID, lifecycle, current, farm, park, shed, idType, value, scope string
		if err := rows.Scan(&goatID, &lifecycle, &current, &farm, &park, &shed, &idType, &value, &scope); err != nil {
			return nil, fmt.Errorf("scan local goat identifier: %w", err)
		}
		goat := byID[goatID]
		if goat == nil {
			goat = &LocalGoat{
				GoatID:            goatID,
				LifecycleStatus:   lifecycle,
				CurrentLocationID: current,
				FarmID:            farm,
				ParkID:            park,
				ShedID:            shed,
			}
			byID[goatID] = goat
			order = append(order, goatID)
		}
		goat.Identifiers = append(goat.Identifiers, LocalIdentifier{
			IdentifierType:  idType,
			NormalizedValue: value,
			ScopeKey:        scope,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate local goat identifiers: %w", err)
	}
	goats := make([]LocalGoat, 0, len(order))
	for _, goatID := range order {
		goats = append(goats, *byID[goatID])
	}
	return goats, nil
}

func loadLocationLookup(ctx context.Context, pool *pgxpool.Pool, tenantID string) (LocationLookup, error) {
	lookup := LocationLookup{
		ShedsByAlias: map[string]LocationTarget{},
		ParksByCode:  map[string]string{},
	}
	rows, err := pool.Query(ctx, `
SELECT location_code, location_id::text
FROM locations
WHERE tenant_id = $1::uuid
  AND location_type = 'park'
  AND status = 'active'
  AND location_code IS NOT NULL`, tenantID)
	if err != nil {
		return lookup, fmt.Errorf("load park locations: %w", err)
	}
	for rows.Next() {
		var code, id string
		if err := rows.Scan(&code, &id); err != nil {
			rows.Close()
			return lookup, fmt.Errorf("scan park location: %w", err)
		}
		lookup.ParksByCode[strings.ToUpper(strings.TrimSpace(code))] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return lookup, fmt.Errorf("iterate park locations: %w", err)
	}

	rows, err = pool.Query(ctx, `
SELECT
  la.alias_code,
  l.location_id::text,
  COALESCE(l.parent_location_id::text, '')::text
FROM location_aliases la
JOIN locations l
  ON l.tenant_id = la.tenant_id
 AND l.location_id = la.canonical_location_id
WHERE la.tenant_id = $1::uuid
  AND la.source_context = $2
  AND l.location_type = 'shed'
  AND l.status = 'active'`, tenantID, aliasSourceContext)
	if err != nil {
		return lookup, fmt.Errorf("load BQ shed aliases: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var alias, locationID, parentID string
		if err := rows.Scan(&alias, &locationID, &parentID); err != nil {
			return lookup, fmt.Errorf("scan BQ shed alias: %w", err)
		}
		lookup.ShedsByAlias[strings.ToUpper(strings.TrimSpace(alias))] = LocationTarget{
			LocationID:       locationID,
			ParentLocationID: parentID,
		}
	}
	if err := rows.Err(); err != nil {
		return lookup, fmt.Errorf("iterate BQ shed aliases: %w", err)
	}
	return lookup, nil
}

func applyPatches(ctx context.Context, pool *pgxpool.Pool, tenantID, traceID string, patches []patch) (int, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin BQ reconciliation transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "bq-reconcile:"+tenantID); err != nil {
		return 0, fmt.Errorf("acquire BQ reconciliation lock: %w", err)
	}
	applied := 0
	for _, patch := range patches {
		tag, err := tx.Exec(ctx, `
UPDATE goats
SET
  lifecycle_status = $3,
  current_location_id = $4::uuid,
  farm_id = $5::uuid,
  park_id = $6::uuid,
  shed_id = $7::uuid,
  row_version = row_version + 1,
  updated_at = now()
WHERE tenant_id = $1::uuid
  AND goat_id = $2::uuid
  AND (
    lifecycle_status IS DISTINCT FROM $3
    OR current_location_id IS DISTINCT FROM $4::uuid
    OR farm_id IS DISTINCT FROM $5::uuid
    OR park_id IS DISTINCT FROM $6::uuid
    OR shed_id IS DISTINCT FROM $7::uuid
  )`,
			tenantID,
			patch.GoatID,
			patch.AfterLifecycle,
			nullableUUID(patch.AfterCurrent),
			nullableUUID(patch.AfterFarm),
			nullableUUID(patch.AfterPark),
			nullableUUID(patch.AfterShed),
		)
		if err != nil {
			return 0, fmt.Errorf("apply BQ reconciliation patch for goat %s: %w", patch.GoatID, err)
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		applied++
		before := map[string]string{
			"lifecycle_status":    patch.BeforeLifecycle,
			"current_location_id": patch.BeforeCurrent,
			"farm_id":             patch.BeforeFarm,
			"park_id":             patch.BeforePark,
			"shed_id":             patch.BeforeShed,
		}
		after := map[string]string{
			"lifecycle_status":    patch.AfterLifecycle,
			"current_location_id": patch.AfterCurrent,
			"farm_id":             patch.AfterFarm,
			"park_id":             patch.AfterPark,
			"shed_id":             patch.AfterShed,
		}
		metadata := map[string]any{
			"source_system":           "legacy_bigquery",
			"source_context":          "goatos_sheets_dashboard",
			"matched_keys":            patch.MatchedKeys,
			"latest_event_date":       patch.LatestEventDate,
			"latest_farm_code":        patch.LatestFarmCode,
			"latest_destination_shed": patch.LatestShed,
		}
		beforeJSON, _ := json.Marshal(before)
		afterJSON, _ := json.Marshal(after)
		metadataJSON, _ := json.Marshal(metadata)
		if _, err := tx.Exec(ctx, `
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
  'goat.bq_reconciled',
  'goat',
  $2::uuid,
  $3::jsonb,
  $4::jsonb,
  $5::jsonb,
  $6
)`, tenantID, patch.GoatID, beforeJSON, afterJSON, metadataJSON, traceID); err != nil {
			return 0, fmt.Errorf("audit BQ reconciliation patch for goat %s: %w", patch.GoatID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit BQ reconciliation: %w", err)
	}
	return applied, nil
}

func nullableUUID(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func MaxEventDate(events []Event) string {
	max := ""
	for _, event := range events {
		date := strings.TrimSpace(event.Date)
		if date > max {
			max = date
		}
	}
	if _, err := time.Parse("2006-01-02", max); err != nil {
		return max
	}
	return max
}
