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
	eventTypeAbortion = "abortion"

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
	IdentityState     string
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
	IdentityReviewUpdates   int            `json:"identity_review_updates"`
	IdentityCleanUpdates    int            `json:"identity_clean_updates"`
	LifecycleConflicts      int            `json:"lifecycle_conflicts"`
	StaleConflictsClosed    int            `json:"stale_lifecycle_conflicts_closed"`
	PatchesPlanned          int            `json:"patches_planned"`
	PatchesApplied          int            `json:"patches_applied"`

	currentLifecycleConflictGoatIDs map[string]struct{} `json:"-"`
}

type patch struct {
	GoatID          string
	BeforeLifecycle string
	AfterLifecycle  string
	BeforeIdentity  string
	AfterIdentity   string
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
	Conflict        *lifecycleConflict
}

type goatEvidence struct {
	events         []Event
	keys           map[string]struct{}
	all            lifecycleEvidence
	rfid           lifecycleEvidence
	oldTag         lifecycleEvidence
	latestLocation *Event
}

type lifecycleEvidence struct {
	lifecycle string
	eventDate string
	eventRank int
	events    []lifecycleEvent
}

type lifecycleEvent struct {
	lifecycle string
	eventType string
	date      string
	rank      int
}

type identifierLifecycleConflict struct {
	Reason            string
	TerminalLifecycle string
	TerminalEventDate string
	LaterLifecycle    string
	LaterEventDate    string
	IdentifierKind    string
	IdentifierKey     string
}

type lifecycleConflict struct {
	RFIDLifecycle     string
	OldTagLifecycle   string
	RFIDKey           string
	OldTagKey         string
	Reason            string
	IdentifierKind    string
	IdentifierKey     string
	TerminalLifecycle string
	TerminalEventDate string
	LaterLifecycle    string
	LaterEventDate    string
	RecommendedState  string
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
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
	staleConflicts, cleanableGoats, err := countStaleLifecycleConflicts(ctx, pool, opts.TenantID, summary.currentLifecycleConflictGoatIDs)
	if err != nil {
		return nil, err
	}
	summary.StaleConflictsClosed = staleConflicts
	summary.IdentityCleanUpdates = cleanableGoats
	if !opts.Execute {
		return summary, nil
	}
	applied, err := applyPatches(ctx, pool, opts.TenantID, opts.TraceID, patches, summary.currentLifecycleConflictGoatIDs, summary)
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
		if err := normalizeEventDates(events); err != nil {
			return nil, err
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
	if err := normalizeEventDates(events); err != nil {
		return nil, err
	}
	return events, nil
}

func normalizeEventDates(events []Event) error {
	for i := range events {
		date := strings.TrimSpace(events[i].Date)
		if date == "" {
			return fmt.Errorf("BQ event[%d] date is required in YYYY-MM-DD format", i)
		}
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil || parsed.Format("2006-01-02") != date {
			return fmt.Errorf("BQ event[%d] date %q must use YYYY-MM-DD format", i, events[i].Date)
		}
		events[i].Date = date
	}
	return nil
}

func Plan(events []Event, goats []LocalGoat, locations LocationLookup) (*Summary, []patch) {
	return PlanWithLocations(events, nil, goats, locations)
}

func PlanWithLocations(events []Event, locationEvents []Event, goats []LocalGoat, locations LocationLookup) (*Summary, []patch) {
	summary := &Summary{
		EventsRead:                      len(events),
		LocalGoatsRead:                  len(goats),
		LifecycleUpdates:                map[string]int{},
		currentLifecycleConflictGoatIDs: map[string]struct{}{},
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
		applyLifecycleEvent(&ev.all, event)
		switch identifierKindForKey(key) {
		case "rfid":
			applyLifecycleEvent(&ev.rfid, event)
		case "old_tag":
			applyLifecycleEvent(&ev.oldTag, event)
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
		nextLifecycle, conflict := targetLifecycle(evidence)
		nextIdentity := goat.IdentityState
		if conflict != nil {
			nextIdentity = "needs_review"
			summary.LifecycleConflicts++
			summary.currentLifecycleConflictGoatIDs[goatID] = struct{}{}
		}
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
			nextIdentity == goat.IdentityState &&
			nextCurrent == goat.CurrentLocationID &&
			nextFarm == goat.FarmID &&
			nextPark == goat.ParkID &&
			nextShed == goat.ShedID {
			continue
		}
		if nextLifecycle != goat.LifecycleStatus {
			summary.LifecycleUpdates[nextLifecycle]++
		}
		if nextIdentity != goat.IdentityState {
			summary.IdentityReviewUpdates++
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
			BeforeIdentity:  goat.IdentityState,
			AfterIdentity:   nextIdentity,
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
			Conflict:        conflict,
		})
	}
	summary.PatchesPlanned = len(patches)
	return summary, patches
}

func applyLifecycleEvent(evidence *lifecycleEvidence, event Event) {
	lifecycle, rank := lifecycleForEvent(event)
	if lifecycle == "" {
		return
	}
	eventType := canonicalEventType(event.Event)
	eventDate := strings.TrimSpace(event.Date)
	evidence.events = append(evidence.events, lifecycleEvent{
		lifecycle: lifecycle,
		eventType: eventType,
		date:      eventDate,
		rank:      rank,
	})
	if evidence.lifecycle == "" ||
		lifecycleEventAfter(eventDate, rank, evidence.eventDate, evidence.eventRank) {
		evidence.lifecycle = lifecycle
		evidence.eventDate = eventDate
		evidence.eventRank = rank
	}
}

func lifecycleForEvent(event Event) (string, int) {
	switch canonicalEventType(event.Event) {
	case eventTypePurchase:
		return "alive", 1
	case eventTypeBirth:
		return "alive", 1
	case eventTypeShifting:
		return "alive", 1
	case eventTypeAbortion:
		return "alive", 1
	case eventTypeSale:
		return "sold", 2
	case eventTypeDeath:
		return "dead", 3
	default:
		return "", 0
	}
}

func lifecycleEventAfter(candidateDate string, candidateRank int, currentDate string, currentRank int) bool {
	switch {
	case candidateDate == "" && currentDate != "":
		return false
	case candidateDate != "" && currentDate == "":
		return true
	case candidateDate != currentDate:
		return candidateDate > currentDate
	default:
		return candidateRank > currentRank
	}
}

func targetLifecycle(evidence *goatEvidence) (string, *lifecycleConflict) {
	rfidLifecycle, rfidConflict := targetLifecycleForEvidence(evidence.rfid, "rfid", firstKeyWithPrefix(evidence.keys, "rfid:"))
	oldTagLifecycle, oldTagConflict := targetLifecycleForEvidence(evidence.oldTag, "old_tag", firstKeyWithPrefix(evidence.keys, "old_tag:"))
	if rfidLifecycle != "" {
		if rfidConflict != nil {
			return rfidLifecycle, lifecycleConflictFromIdentifier(rfidLifecycle, oldTagLifecycle, evidence.keys, rfidConflict)
		}
		if oldTagConflict != nil {
			return rfidLifecycle, lifecycleConflictFromIdentifier(rfidLifecycle, oldTagLifecycle, evidence.keys, oldTagConflict)
		}
		if oldTagLifecycle != "" && oldTagLifecycle != rfidLifecycle {
			return rfidLifecycle, &lifecycleConflict{
				RFIDLifecycle:    rfidLifecycle,
				OldTagLifecycle:  oldTagLifecycle,
				RFIDKey:          firstKeyWithPrefix(evidence.keys, "rfid:"),
				OldTagKey:        firstKeyWithPrefix(evidence.keys, "old_tag:"),
				Reason:           "identifier_lifecycle_disagreement",
				RecommendedState: "RFID evidence wins lifecycle; review reused/manual old-tag evidence before changing identifiers.",
			}
		}
		return rfidLifecycle, nil
	}
	if oldTagLifecycle != "" {
		if oldTagConflict != nil {
			return oldTagLifecycle, lifecycleConflictFromIdentifier(rfidLifecycle, oldTagLifecycle, evidence.keys, oldTagConflict)
		}
		return oldTagLifecycle, nil
	}
	allLifecycle, allConflict := targetLifecycleForEvidence(evidence.all, "matched_identifier", firstKeyWithPrefix(evidence.keys, ""))
	if allConflict != nil {
		return allLifecycle, lifecycleConflictFromIdentifier(rfidLifecycle, oldTagLifecycle, evidence.keys, allConflict)
	}
	return allLifecycle, nil
}

func targetLifecycleForEvidence(evidence lifecycleEvidence, identifierKind, identifierKey string) (string, *identifierLifecycleConflict) {
	death := latestLifecycleEvent(evidence.events, eventTypeDeath)
	if death != nil {
		if later := latestLifecycleActivityAfter(evidence.events, death.date); later != nil {
			return "dead", &identifierLifecycleConflict{
				Reason:            "death_then_later_activity",
				TerminalLifecycle: "dead",
				TerminalEventDate: death.date,
				LaterLifecycle:    later.lifecycle,
				LaterEventDate:    later.date,
				IdentifierKind:    identifierKind,
				IdentifierKey:     identifierKey,
			}
		}
		return "dead", nil
	}
	sale := latestLifecycleEvent(evidence.events, eventTypeSale)
	if sale != nil {
		if purchase := latestLifecycleEventAfter(evidence.events, sale.date, eventTypePurchase); purchase != nil {
			return "alive", nil
		}
		if later := latestLifecycleActivityAfter(evidence.events, sale.date); later != nil {
			return "sold", &identifierLifecycleConflict{
				Reason:            "sale_then_later_nonpurchase_activity",
				TerminalLifecycle: "sold",
				TerminalEventDate: sale.date,
				LaterLifecycle:    later.lifecycle,
				LaterEventDate:    later.date,
				IdentifierKind:    identifierKind,
				IdentifierKey:     identifierKey,
			}
		}
		return "sold", nil
	}
	if alive := latestAliveLifecycleEvent(evidence.events); alive != nil {
		return "alive", nil
	}
	return evidence.lifecycle, nil
}

func latestLifecycleEvent(events []lifecycleEvent, eventTypes ...string) *lifecycleEvent {
	wanted := map[string]struct{}{}
	for _, eventType := range eventTypes {
		wanted[eventType] = struct{}{}
	}
	var out *lifecycleEvent
	for i := range events {
		event := events[i]
		if _, ok := wanted[event.eventType]; !ok {
			continue
		}
		if out == nil || lifecycleEventAfter(event.date, event.rank, out.date, out.rank) {
			out = &events[i]
		}
	}
	return out
}

func latestAliveLifecycleEvent(events []lifecycleEvent) *lifecycleEvent {
	var out *lifecycleEvent
	for i := range events {
		event := events[i]
		if event.lifecycle != "alive" {
			continue
		}
		if out == nil || lifecycleEventAfter(event.date, event.rank, out.date, out.rank) {
			out = &events[i]
		}
	}
	return out
}

func latestLifecycleEventAfter(events []lifecycleEvent, afterDate string, eventTypes ...string) *lifecycleEvent {
	if strings.TrimSpace(afterDate) == "" {
		return nil
	}
	wanted := map[string]struct{}{}
	for _, eventType := range eventTypes {
		wanted[eventType] = struct{}{}
	}
	var out *lifecycleEvent
	for i := range events {
		event := events[i]
		if _, ok := wanted[event.eventType]; !ok {
			continue
		}
		if strings.TrimSpace(event.date) <= afterDate {
			continue
		}
		if out == nil || lifecycleEventAfter(event.date, event.rank, out.date, out.rank) {
			out = &events[i]
		}
	}
	return out
}

func latestLifecycleActivityAfter(events []lifecycleEvent, afterDate string) *lifecycleEvent {
	if strings.TrimSpace(afterDate) == "" {
		return nil
	}
	var out *lifecycleEvent
	for i := range events {
		event := events[i]
		if event.lifecycle == "" {
			continue
		}
		if strings.TrimSpace(event.date) <= afterDate {
			continue
		}
		if out == nil || lifecycleEventAfter(event.date, event.rank, out.date, out.rank) {
			out = &events[i]
		}
	}
	return out
}

func lifecycleConflictFromIdentifier(rfidLifecycle, oldTagLifecycle string, keys map[string]struct{}, conflict *identifierLifecycleConflict) *lifecycleConflict {
	if conflict == nil {
		return nil
	}
	recommended := "BQ lifecycle evidence conflicts inside one identifier stream; keep the terminal state and review possible tag/chip reuse before marking the goat clean."
	if conflict.IdentifierKind == "rfid" {
		recommended = "RFID lifecycle evidence has terminal-plus-later activity; keep the conservative lifecycle and review possible chip reuse before marking the goat clean."
	} else if conflict.IdentifierKind == "old_tag" {
		recommended = "Old-tag lifecycle evidence has terminal-plus-later activity; keep the conservative lifecycle and review reused/manual old-tag evidence before changing identifiers."
	}
	return &lifecycleConflict{
		RFIDLifecycle:     rfidLifecycle,
		OldTagLifecycle:   oldTagLifecycle,
		RFIDKey:           firstKeyWithPrefix(keys, "rfid:"),
		OldTagKey:         firstKeyWithPrefix(keys, "old_tag:"),
		Reason:            conflict.Reason,
		IdentifierKind:    conflict.IdentifierKind,
		IdentifierKey:     conflict.IdentifierKey,
		TerminalLifecycle: conflict.TerminalLifecycle,
		TerminalEventDate: conflict.TerminalEventDate,
		LaterLifecycle:    conflict.LaterLifecycle,
		LaterEventDate:    conflict.LaterEventDate,
		RecommendedState:  recommended,
	}
}

func firstKeyWithPrefix(keys map[string]struct{}, prefix string) string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		if strings.HasPrefix(key, prefix) {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return ""
	}
	return out[0]
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

func identifierKindForKey(key string) string {
	switch {
	case strings.HasPrefix(key, "rfid:"):
		return "rfid"
	case strings.HasPrefix(key, "old_tag:"):
		return "old_tag"
	default:
		return ""
	}
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
  g.identity_state,
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
		var goatID, lifecycle, identity, current, farm, park, shed, idType, value, scope string
		if err := rows.Scan(&goatID, &lifecycle, &identity, &current, &farm, &park, &shed, &idType, &value, &scope); err != nil {
			return nil, fmt.Errorf("scan local goat identifier: %w", err)
		}
		goat := byID[goatID]
		if goat == nil {
			goat = &LocalGoat{
				GoatID:            goatID,
				LifecycleStatus:   lifecycle,
				IdentityState:     identity,
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

func applyPatches(ctx context.Context, pool *pgxpool.Pool, tenantID, traceID string, patches []patch, currentConflictGoatIDs map[string]struct{}, summary *Summary) (int, error) {
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
  identity_state = $4,
  current_location_id = $5::uuid,
  farm_id = $6::uuid,
  park_id = $7::uuid,
  shed_id = $8::uuid,
  row_version = row_version + 1,
  updated_at = now()
WHERE tenant_id = $1::uuid
  AND goat_id = $2::uuid
  AND (
    lifecycle_status IS DISTINCT FROM $3
    OR identity_state IS DISTINCT FROM $4
    OR current_location_id IS DISTINCT FROM $5::uuid
    OR farm_id IS DISTINCT FROM $6::uuid
    OR park_id IS DISTINCT FROM $7::uuid
    OR shed_id IS DISTINCT FROM $8::uuid
  )`,
			tenantID,
			patch.GoatID,
			patch.AfterLifecycle,
			patch.AfterIdentity,
			nullableUUID(patch.AfterCurrent),
			nullableUUID(patch.AfterFarm),
			nullableUUID(patch.AfterPark),
			nullableUUID(patch.AfterShed),
		)
		if err != nil {
			return 0, fmt.Errorf("apply BQ reconciliation patch for goat %s: %w", patch.GoatID, err)
		}
		conflictInserted := false
		if patch.Conflict != nil {
			conflictInserted, err = ensureLifecycleConflict(ctx, tx, tenantID, patch)
			if err != nil {
				return 0, err
			}
		}
		if tag.RowsAffected() == 0 && !conflictInserted {
			continue
		}
		applied++
		before := map[string]string{
			"lifecycle_status":    patch.BeforeLifecycle,
			"identity_state":      patch.BeforeIdentity,
			"current_location_id": patch.BeforeCurrent,
			"farm_id":             patch.BeforeFarm,
			"park_id":             patch.BeforePark,
			"shed_id":             patch.BeforeShed,
		}
		after := map[string]string{
			"lifecycle_status":    patch.AfterLifecycle,
			"identity_state":      patch.AfterIdentity,
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
		if patch.Conflict != nil {
			metadata["identifier_lifecycle_conflict"] = patch.Conflict
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
	closedConflicts, cleanedGoats, err := closeStaleLifecycleConflicts(ctx, tx, tenantID, traceID, currentConflictGoatIDs)
	if err != nil {
		return 0, err
	}
	summary.StaleConflictsClosed = closedConflicts
	summary.IdentityCleanUpdates = cleanedGoats
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit BQ reconciliation: %w", err)
	}
	return applied, nil
}

func ensureLifecycleConflict(ctx context.Context, tx pgx.Tx, tenantID string, patch patch) (bool, error) {
	if patch.Conflict == nil {
		return false, nil
	}
	evidence := map[string]any{
		"source_context":      "bq_reconcile_identifier_lifecycle_conflict",
		"source_system":       "legacy_bigquery",
		"review_note":         patch.Conflict.RecommendedState,
		"reason":              patch.Conflict.Reason,
		"identifier_kind":     patch.Conflict.IdentifierKind,
		"identifier_key":      patch.Conflict.IdentifierKey,
		"terminal_lifecycle":  patch.Conflict.TerminalLifecycle,
		"terminal_event_date": patch.Conflict.TerminalEventDate,
		"later_lifecycle":     patch.Conflict.LaterLifecycle,
		"later_event_date":    patch.Conflict.LaterEventDate,
		"rfid_lifecycle":      patch.Conflict.RFIDLifecycle,
		"old_tag_lifecycle":   patch.Conflict.OldTagLifecycle,
		"rfid_key":            patch.Conflict.RFIDKey,
		"old_tag_key":         patch.Conflict.OldTagKey,
		"matched_keys":        patch.MatchedKeys,
		"latest_event_date":   patch.LatestEventDate,
		"latest_farm_code":    patch.LatestFarmCode,
		"latest_destination":  patch.LatestShed,
	}
	evidenceJSON, _ := json.Marshal(evidence)
	var conflictID string
	err := tx.QueryRow(ctx, `
INSERT INTO identity_conflicts (
  tenant_id,
  conflict_type,
  severity,
  state,
  identifier_type,
  identifier_value,
  goat_ids,
  source_record_ids,
  evidence
)
SELECT
  $1::uuid,
  'status_mismatch',
  'high',
  'open',
  'rfid',
  $3,
  ARRAY[$2::uuid],
  $4::text[],
  $5::jsonb
WHERE NOT EXISTS (
  SELECT 1
  FROM identity_conflicts
  WHERE tenant_id = $1::uuid
    AND conflict_type = 'status_mismatch'
    AND state IN ('open', 'needs_field_check')
    AND goat_ids @> ARRAY[$2::uuid]
    AND evidence->>'source_context' = 'bq_reconcile_identifier_lifecycle_conflict'
)
RETURNING conflict_id::text`,
		tenantID,
		patch.GoatID,
		identifierValueFromKey(patch.Conflict.RFIDKey),
		patch.MatchedKeys,
		evidenceJSON,
	).Scan(&conflictID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("insert BQ lifecycle conflict for goat %s: %w", patch.GoatID, err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO identity_conflict_goats (conflict_id, tenant_id, goat_id, role)
VALUES ($1::uuid, $2::uuid, $3::uuid, 'affected')
ON CONFLICT (conflict_id, goat_id) DO NOTHING`, conflictID, tenantID, patch.GoatID); err != nil {
		return false, fmt.Errorf("link BQ lifecycle conflict for goat %s: %w", patch.GoatID, err)
	}
	return true, nil
}

func countStaleLifecycleConflicts(ctx context.Context, pool *pgxpool.Pool, tenantID string, currentConflictGoatIDs map[string]struct{}) (int, int, error) {
	staleGoatIDs, err := staleLifecycleConflictGoatIDs(ctx, pool, tenantID, currentConflictGoatIDs)
	if err != nil {
		return 0, 0, err
	}
	if len(staleGoatIDs) == 0 {
		return 0, 0, nil
	}
	conflicts, err := countStaleLifecycleConflictRows(ctx, pool, tenantID, staleGoatIDs)
	if err != nil {
		return 0, 0, err
	}
	cleanable, err := countCleanableStaleLifecycleGoats(ctx, pool, tenantID, staleGoatIDs)
	if err != nil {
		return 0, 0, err
	}
	return conflicts, cleanable, nil
}

func staleLifecycleConflictGoatIDs(ctx context.Context, q queryer, tenantID string, currentConflictGoatIDs map[string]struct{}) ([]string, error) {
	current := sortedIDSet(currentConflictGoatIDs)
	rows, err := q.Query(ctx, `
WITH current_conflict_goats AS (
  SELECT unnest($2::uuid[]) AS goat_id
)
SELECT DISTINCT cg.goat_id::text
FROM identity_conflicts c
JOIN identity_conflict_goats cg
  ON cg.tenant_id = c.tenant_id
 AND cg.conflict_id = c.conflict_id
WHERE c.tenant_id = $1::uuid
  AND c.conflict_type = 'status_mismatch'
  AND c.state IN ('open', 'needs_field_check', 'closed')
  AND c.evidence->>'source_context' = 'bq_reconcile_identifier_lifecycle_conflict'
  AND NOT EXISTS (
    SELECT 1
    FROM current_conflict_goats ccg
    WHERE ccg.goat_id = cg.goat_id
  )`, tenantID, current)
	if err != nil {
		return nil, fmt.Errorf("find stale BQ lifecycle conflicts: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var goatID string
		if err := rows.Scan(&goatID); err != nil {
			return nil, fmt.Errorf("scan stale BQ lifecycle conflict goat: %w", err)
		}
		out = append(out, goatID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stale BQ lifecycle conflict goats: %w", err)
	}
	sort.Strings(out)
	return out, nil
}

func closeStaleLifecycleConflicts(ctx context.Context, tx pgx.Tx, tenantID, traceID string, currentConflictGoatIDs map[string]struct{}) (int, int, error) {
	staleGoatIDs, err := staleLifecycleConflictGoatIDs(ctx, tx, tenantID, currentConflictGoatIDs)
	if err != nil {
		return 0, 0, err
	}
	if len(staleGoatIDs) == 0 {
		return 0, 0, nil
	}

	closed, err := closeStaleLifecycleConflictRows(ctx, tx, tenantID, staleGoatIDs)
	if err != nil {
		return 0, 0, err
	}
	cleaned, err := cleanStaleLifecycleGoats(ctx, tx, tenantID, traceID, staleGoatIDs)
	if err != nil {
		return 0, 0, err
	}
	return closed, cleaned, nil
}

func countStaleLifecycleConflictRows(ctx context.Context, q queryer, tenantID string, staleGoatIDs []string) (int, error) {
	var count int
	if err := q.QueryRow(ctx, staleLifecycleConflictRowsSQL("SELECT count(*)::int"), tenantID, staleGoatIDs).Scan(&count); err != nil {
		return 0, fmt.Errorf("count stale BQ lifecycle conflict rows: %w", err)
	}
	return count, nil
}

func closeStaleLifecycleConflictRows(ctx context.Context, tx pgx.Tx, tenantID string, staleGoatIDs []string) (int, error) {
	tag, err := tx.Exec(ctx, `
UPDATE identity_conflicts c
SET state = 'closed',
    resolved_at = now(),
    row_version = row_version + 1
FROM identity_conflict_goats cg
JOIN unnest($2::uuid[]) AS stale(goat_id)
  ON stale.goat_id = cg.goat_id
WHERE c.tenant_id = $1::uuid
  AND c.conflict_id = cg.conflict_id
  AND cg.tenant_id = c.tenant_id
  AND c.conflict_type = 'status_mismatch'
  AND c.state IN ('open', 'needs_field_check')
  AND c.evidence->>'source_context' = 'bq_reconcile_identifier_lifecycle_conflict'`, tenantID, staleGoatIDs)
	if err != nil {
		return 0, fmt.Errorf("close stale BQ lifecycle conflict rows: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

func countCleanableStaleLifecycleGoats(ctx context.Context, q queryer, tenantID string, staleGoatIDs []string) (int, error) {
	var count int
	if err := q.QueryRow(ctx, `
SELECT count(*)::int
FROM goats g
JOIN unnest($2::uuid[]) AS stale(goat_id)
  ON stale.goat_id = g.goat_id
WHERE g.tenant_id = $1::uuid
  AND g.identity_state = 'needs_review'
  AND NOT EXISTS (
    SELECT 1
    FROM identity_conflict_goats cg
    JOIN identity_conflicts c
      ON c.tenant_id = cg.tenant_id
     AND c.conflict_id = cg.conflict_id
    WHERE cg.tenant_id = g.tenant_id
      AND cg.goat_id = g.goat_id
      AND c.state IN ('open', 'needs_field_check')
      AND NOT (
        c.conflict_type = 'status_mismatch'
        AND c.evidence->>'source_context' = 'bq_reconcile_identifier_lifecycle_conflict'
        AND cg.goat_id = ANY($2::uuid[])
      )
  )`, tenantID, staleGoatIDs).Scan(&count); err != nil {
		return 0, fmt.Errorf("count cleanable BQ lifecycle goats: %w", err)
	}
	return count, nil
}

func cleanStaleLifecycleGoats(ctx context.Context, tx pgx.Tx, tenantID, traceID string, staleGoatIDs []string) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
WITH cleaned AS (
UPDATE goats g
SET identity_state = 'clean',
    row_version = row_version + 1,
    updated_at = now()
FROM unnest($2::uuid[]) AS stale(goat_id)
WHERE g.tenant_id = $1::uuid
  AND g.goat_id = stale.goat_id
  AND g.identity_state = 'needs_review'
  AND NOT EXISTS (
    SELECT 1
    FROM identity_conflict_goats cg
    JOIN identity_conflicts c
      ON c.tenant_id = cg.tenant_id
     AND c.conflict_id = cg.conflict_id
    WHERE cg.tenant_id = g.tenant_id
      AND cg.goat_id = g.goat_id
      AND c.state IN ('open', 'needs_field_check')
  )
  RETURNING g.goat_id
),
audited AS (
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
  )
  SELECT
    $1::uuid,
    'system',
    'goat.bq_lifecycle_conflict_cleaned',
    'goat',
    goat_id,
    jsonb_build_object('identity_state', 'needs_review'),
    jsonb_build_object('identity_state', 'clean'),
    jsonb_build_object(
      'source_system', 'legacy_bigquery',
      'source_context', 'bq_reconcile_stale_lifecycle_conflict_cleanup',
      'closed_conflict_source_context', 'bq_reconcile_identifier_lifecycle_conflict'
    ),
    $3
  FROM cleaned
  RETURNING 1
)
SELECT count(*)::int FROM audited`, tenantID, staleGoatIDs, traceID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("clean stale BQ lifecycle goats: %w", err)
	}
	return count, nil
}

func staleLifecycleConflictRowsSQL(prefix string) string {
	return prefix + `
FROM identity_conflicts c
JOIN identity_conflict_goats cg
  ON cg.tenant_id = c.tenant_id
 AND cg.conflict_id = c.conflict_id
JOIN unnest($2::uuid[]) AS stale(goat_id)
  ON stale.goat_id = cg.goat_id
WHERE c.tenant_id = $1::uuid
  AND c.conflict_type = 'status_mismatch'
  AND c.state IN ('open', 'needs_field_check')
  AND c.evidence->>'source_context' = 'bq_reconcile_identifier_lifecycle_conflict'`
}

func sortedIDSet(ids map[string]struct{}) []string {
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func identifierValueFromKey(key string) string {
	parts := strings.Split(key, ":")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
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
