// Package domain is the rulebook of pen routines (maintainer instruction 2026-09-16): the
// configurable recurring pen checks a park head owes -- "each pen cleaned?" -- per park, on a
// daily / weekly / monthly cadence or the day after work of a chosen kind, worked by one of the
// routine's assignees with the questions, photos/videos and pen check-in the routine asks for.
//
// The rule (Definition + Evidence) and the occurrence (Task) both live here, together with every
// string a screen shows: the card title, the cadence line, the state chip, the instruction. The
// phone and the web render those verbatim. The pen's own name comes from oploc, never from here.
// Canonical prose: docs/decisions/pen-routines.md.
package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Scope: which pens of the park a routine covers. ScopePark is a general task: ONE task per
// occurrence for the whole park, naming no pen ("Check the medicine store").
const (
	ScopeAllPens      = "all_pens"
	ScopeSelectedPens = "selected_pens"
	ScopePark         = "park"
)

// Cadence: which business dates raise a task.
const (
	CadenceDaily   = "daily"
	CadenceWeekly  = "weekly"
	CadenceMonthly = "monthly"
	// CadenceEveryNDays raises on StartDate, StartDate + N, StartDate + 2N, ... (N = IntervalDays).
	CadenceEveryNDays = "every_n_days"
	CadenceAfterWork  = "after_work"
)

// Interval bounds of an every_n_days routine. 1 is "daily", and past a quarter the cadence is
// not a routine any more.
const (
	MinIntervalDays = 2
	MaxIntervalDays = 90
)

// Assignable roles: WHO a routine is for (2026-09-17 revision, docs/decisions/pen-routines.md).
// The keys are the RBAC role keys a grant carries; whoever holds one FOR THE ROUTINE'S PARK
// gets the task, and any one of them doing it is enough.
const (
	RoleParkHead            = "park_head"
	RolePCDirector          = "pc_director"
	RoleBreedingDirector    = "breeding_director"
	RoleGrowthDirector      = "growth_director"
	RoleFeedDirector        = "feed_director"
	RoleHealthDirector      = "health_director"
	RoleProcurementDirector = "procurement_director"
	RoleCXO                 = "ceo_internal"
)

// AssignableRoles is the closed vocabulary, in display order. The migration's CHECK on
// pen_routine_definitions.assignee_roles lists the same eight keys.
var AssignableRoles = []string{
	RoleParkHead, RolePCDirector, RoleBreedingDirector, RoleGrowthDirector,
	RoleFeedDirector, RoleHealthDirector, RoleProcurementDirector, RoleCXO,
}

var roleLabels = map[string]string{
	RoleParkHead:            "Park Head",
	RolePCDirector:          "Preventive Care Director",
	RoleBreedingDirector:    "Breeding Director",
	RoleGrowthDirector:      "Growth Director",
	RoleFeedDirector:        "Feed Director",
	RoleHealthDirector:      "Health Director",
	RoleProcurementDirector: "Procurement Director",
	RoleCXO:                 "CXO",
}

// IsAssignableRole reports whether key is in the vocabulary.
func IsAssignableRole(key string) bool { _, ok := roleLabels[key]; return ok }

// RoleLabel is the farm word for a role key. An unknown key renders as itself, underscores
// spaced, so a vocabulary widening never blanks a row.
func RoleLabel(key string) string {
	if label, ok := roleLabels[key]; ok {
		return label
	}
	return strings.ReplaceAll(key, "_", " ")
}

// SortRoles orders role keys in vocabulary order, dropping blanks and duplicates. Unknown keys
// are KEPT (at the end) so validation can name them rather than silently dropping a choice.
func SortRoles(roles []string) []string {
	out := dedupe(roles)
	sort.SliceStable(out, func(i, j int) bool { return roleRank(out[i]) < roleRank(out[j]) })
	return out
}

func roleRank(k string) int {
	for i, known := range AssignableRoles {
		if known == k {
			return i
		}
	}
	return len(AssignableRoles)
}

// Review: what a submit does.
const (
	ReviewVerifier = "verifier"
	ReviewNone     = "none"
)

// Status of a routine definition.
const (
	StatusActive  = "active"
	StatusPaused  = "paused"
	StatusRetired = "retired"
)

// Presence: whether the submitter must check in to the pen first.
const (
	PresenceRequired = "required"
	PresenceOff      = "off"
)

// Question kinds -- the widgets the phone already renders for the weighing SOP and the
// procurement inspection. yes_no is a choice with two fixed options rendered as a toggle.
const (
	QuestionYesNo       = "yes_no"
	QuestionChoice      = "choice"
	QuestionMultiChoice = "multi_choice"
	QuestionNumber      = "number"
	QuestionText        = "text"
)

// After-work kinds: the closed vocabulary an after_work routine may trigger on. Each maps to
// the verification item category the work's submit raises (WorkKindCategory), the pen-visit
// read, so ONE table answers "what happened in this pen today" for every kind.
const (
	WorkVaccination      = "vaccination"
	WorkDeworming        = "deworming"
	WorkAntiProtozoan    = "anti_protozoan"
	WorkTicksRemoval     = "ticks_removal"
	WorkHoofTrimming     = "hoof_trimming"
	WorkHairTrimming     = "hair_trimming"
	WorkWeighing         = "weighing"
	WorkFeedDistribution = "feed_distribution"
	WorkShifting         = "shifting"
)

// WorkKinds is the display order of the vocabulary.
var WorkKinds = []string{
	WorkVaccination, WorkDeworming, WorkAntiProtozoan, WorkTicksRemoval, WorkHoofTrimming,
	WorkHairTrimming, WorkWeighing, WorkFeedDistribution, WorkShifting,
}

// workKindCategories maps a work kind to the verification item category its submit raises.
// The category strings are pinned by the materializer's integration test rather than imported,
// so this package depends on no producer.
var workKindCategories = map[string]string{
	WorkVaccination:      "vaccination_proof",
	WorkDeworming:        "pc_deworming",
	WorkAntiProtozoan:    "pc_anti_protozoan",
	WorkTicksRemoval:     "pc_ticks_removal",
	WorkHoofTrimming:     "pc_hoof_trimming",
	WorkHairTrimming:     "pc_hair_trimming",
	WorkWeighing:         "weighing_proof",
	WorkFeedDistribution: "feed_distribution",
	WorkShifting:         "shifting_move",
}

// WorkKindCategory is the verification category a work kind's submit raises ("" if unknown).
func WorkKindCategory(kind string) string { return workKindCategories[kind] }

// WorkKindForCategory is the inverse: the work kind a verification category implies ("" if the
// category is not pen work a routine can trigger on).
func WorkKindForCategory(category string) string {
	for kind, c := range workKindCategories {
		if c == category {
			return kind
		}
	}
	return ""
}

// WorkKindLabel is the farm word for a work kind. Unknown keys render as themselves so a
// vocabulary widening never blanks a card.
func WorkKindLabel(kind string) string {
	switch kind {
	case WorkVaccination:
		return "Vaccination"
	case WorkDeworming:
		return "Deworming"
	case WorkAntiProtozoan:
		return "Anti protozoan"
	case WorkTicksRemoval:
		return "Ticks removal"
	case WorkHoofTrimming:
		return "Hoof trimming"
	case WorkHairTrimming:
		return "Hair trimming"
	case WorkWeighing:
		return "Weighing"
	case WorkFeedDistribution:
		return "Feed distribution"
	case WorkShifting:
		return "Pen move"
	}
	return strings.ReplaceAll(kind, "_", " ")
}

// IsKnownWorkKind reports whether k is in the vocabulary.
func IsKnownWorkKind(k string) bool { _, ok := workKindCategories[k]; return ok }

// SortWorkKinds orders kinds for display and drops duplicates and unknowns.
func SortWorkKinds(kinds []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(kinds))
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if !IsKnownWorkKind(k) || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.SliceStable(out, func(i, j int) bool { return workKindRank(out[i]) < workKindRank(out[j]) })
	return out
}

func workKindRank(k string) int {
	for i, known := range WorkKinds {
		if known == k {
			return i
		}
	}
	return len(WorkKinds)
}

// Limits on an authored routine. Bounded so a form is an afternoon's walk, not a survey.
const (
	MaxQuestions     = 20
	MaxOptions       = 12
	MaxProofPerKind  = 5
	MaxNameLength    = 80
	MaxTitleLength   = 160
	MaxInstructionLn = 600
	MaxTextAnswer    = 1000
	MaxDueOffsetDays = 30
)

// Option is one choice of a choice / multi_choice question.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Question is one authored question. The shape matches the weighing SOP's question so the phone
// renders it with the widgets it already has.
type Question struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	Title    string   `json:"title"`
	Hint     string   `json:"hint,omitempty"`
	Required bool     `json:"required"`
	Options  []Option `json:"options,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	Unit     string   `json:"unit,omitempty"`
}

// ProofRule is how many captures of one kind a submit carries.
type ProofRule struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// Evidence is what the routine expects from the assignee: pen_routine_versions.evidence.
type Evidence struct {
	Questions []Question `json:"questions"`
	Photo     ProofRule  `json:"photo"`
	Video     ProofRule  `json:"video"`
	Presence  string     `json:"presence"`
}

// PresenceRequired reports whether the submitter must check in to the pen first.
func (e Evidence) PresenceRequired() bool { return e.Presence == PresenceRequired }

// QuestionByID finds an authored question.
func (e Evidence) QuestionByID(id string) (Question, bool) {
	for _, q := range e.Questions {
		if q.ID == id {
			return q, true
		}
	}
	return Question{}, false
}

// ParseEvidence decodes and validates a stored evidence document, failing loud on unknown keys.
func ParseEvidence(raw []byte) (Evidence, error) {
	var e Evidence
	if len(raw) == 0 {
		return Evidence{}, ErrInvalidEvidence
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return Evidence{}, fmt.Errorf("%w: %v", ErrInvalidEvidence, err)
	}
	if err := ValidateEvidence(e); err != nil {
		return Evidence{}, err
	}
	return e, nil
}

// NormalizeEvidence trims and defaults an authored document before validation.
func NormalizeEvidence(e Evidence) Evidence {
	if e.Presence == "" {
		e.Presence = PresenceOff
	}
	for i := range e.Questions {
		q := &e.Questions[i]
		q.ID = strings.TrimSpace(q.ID)
		q.Kind = strings.TrimSpace(q.Kind)
		q.Title = strings.TrimSpace(q.Title)
		q.Hint = strings.TrimSpace(q.Hint)
		q.Unit = strings.TrimSpace(q.Unit)
		if q.Kind == QuestionYesNo {
			q.Options = []Option{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}
		}
		for j := range q.Options {
			q.Options[j].Value = strings.TrimSpace(q.Options[j].Value)
			q.Options[j].Label = strings.TrimSpace(q.Options[j].Label)
			if q.Options[j].Label == "" {
				q.Options[j].Label = q.Options[j].Value
			}
		}
	}
	if e.Questions == nil {
		e.Questions = []Question{}
	}
	return e
}

// ValidateEvidence is the authoring gate: a document that fails here is refused at save, never
// silently defaulted (the validate-or-reject rule).
func ValidateEvidence(e Evidence) error {
	if e.Presence != PresenceRequired && e.Presence != PresenceOff {
		return fmt.Errorf("%w: presence must be %q or %q", ErrInvalidEvidence, PresenceRequired, PresenceOff)
	}
	if len(e.Questions) > MaxQuestions {
		return fmt.Errorf("%w: at most %d questions", ErrInvalidEvidence, MaxQuestions)
	}
	for _, rule := range []struct {
		name string
		r    ProofRule
	}{{"photo", e.Photo}, {"video", e.Video}} {
		if rule.r.Min < 0 || rule.r.Max < 0 || rule.r.Min > rule.r.Max || rule.r.Max > MaxProofPerKind {
			return fmt.Errorf("%w: %s min/max must satisfy 0 <= min <= max <= %d", ErrInvalidEvidence, rule.name, MaxProofPerKind)
		}
	}
	seen := map[string]bool{}
	for i, q := range e.Questions {
		if q.ID == "" || len(q.ID) > 64 || !isKeyLike(q.ID) {
			return fmt.Errorf("%w: question %d needs an id of letters, digits and underscores", ErrInvalidEvidence, i+1)
		}
		if seen[q.ID] {
			return fmt.Errorf("%w: question id %q repeats", ErrInvalidEvidence, q.ID)
		}
		seen[q.ID] = true
		if q.Title == "" || len(q.Title) > MaxTitleLength {
			return fmt.Errorf("%w: question %q needs a title of at most %d characters", ErrInvalidEvidence, q.ID, MaxTitleLength)
		}
		switch q.Kind {
		case QuestionYesNo, QuestionText:
		case QuestionChoice, QuestionMultiChoice:
			if len(q.Options) < 2 || len(q.Options) > MaxOptions {
				return fmt.Errorf("%w: question %q needs 2 to %d options", ErrInvalidEvidence, q.ID, MaxOptions)
			}
			values := map[string]bool{}
			for _, o := range q.Options {
				if o.Value == "" || values[o.Value] {
					return fmt.Errorf("%w: question %q has a blank or repeated option", ErrInvalidEvidence, q.ID)
				}
				values[o.Value] = true
			}
		case QuestionNumber:
			if q.Min != nil && q.Max != nil && *q.Min > *q.Max {
				return fmt.Errorf("%w: question %q has min above max", ErrInvalidEvidence, q.ID)
			}
		default:
			return fmt.Errorf("%w: question %q has unknown kind %q", ErrInvalidEvidence, q.ID, q.Kind)
		}
	}
	return nil
}

func isKeyLike(s string) bool {
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// Definition is one routine as stored, with its current version's renderable fields joined.
type Definition struct {
	RoutineID      string
	TenantID       string
	ParkID         string
	ParkName       string
	Name           string
	Instruction    string
	ScopeKind      string
	OccupiedOnly   bool
	CadenceKind    string
	Weekdays       []int
	MonthDays      []int
	AfterWorkKinds []string
	// IntervalDays is the N of an every_n_days routine (0 otherwise).
	IntervalDays int
	// StartDate (YYYY-MM-DD, IST business date): nothing raises before it, for every cadence.
	StartDate     string
	DueOffsetDays int
	// NotifyTime is the local IST "HH:MM" the day's push goes out.
	NotifyTime     string
	ReviewKind     string
	Status         string
	CurrentVersion int
	Evidence       Evidence
	Pens           []PenRef
	// AssigneeRoles are WHO owes the routine, keys of AssignableRoles.
	AssigneeRoles []string
	// People are the role holders a read resolved for the routine's park (read-only preview;
	// ignored on write).
	People     []Assignee
	CreatedBy  string
	UpdatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	RowVersion int
}

// Assignee is one person who currently holds one of a routine's roles in its park, as a read
// resolves them. RoleKey is the first of the routine's roles they hold, in vocabulary order.
type Assignee struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	RoleKey     string `json:"role"`
}

// PenRef names a pen: the shed id and its partition label ("" for an undivided shed).
type PenRef struct {
	ShedID    string
	Partition string
	// ShedName and Label (the oploc display) are resolved by the read; ignored on write.
	ShedName string
	Label    string
}

// Sentinel errors. The transport maps each to a stable code and a farm-worded message.
var (
	ErrInvalidRoutine = errors.New("pen routine: the routine is not valid")
	// ErrNoRoles wraps ErrInvalidRoutine: a routine for nobody. Its own code on the wire.
	ErrNoRoles         = fmt.Errorf("%w (no roles)", ErrInvalidRoutine)
	ErrInvalidEvidence = errors.New("pen routine: the evidence rules are not valid")
	ErrNotAssignee     = errors.New("pen routine: caller is not an assignee of this routine")
	ErrAlreadyDone     = errors.New("pen routine: already submitted")
	ErrInReview        = errors.New("pen routine: the evidence is with the verifier")
	ErrCanceled        = errors.New("pen routine: task is canceled")
	ErrAnswerInvalid   = errors.New("pen routine: an answer is missing or not valid")
	ErrProofCount      = errors.New("pen routine: the photo or video count is outside the routine's rule")
	ErrInvalidProof    = errors.New("pen routine: a capture could not be verified")
	ErrPresenceMissing = errors.New("pen routine: check in to the pen before submitting")
	ErrPresenceState   = errors.New("pen routine: the check-in is not in a state that allows this")
	ErrVersionConflict = errors.New("pen routine: the task changed since it was loaded")
)

// ValidateDefinition is the authoring gate on the rule itself (the evidence has its own).
func ValidateDefinition(d Definition) error {
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > MaxNameLength {
		return fmt.Errorf("%w: a name of at most %d characters is required", ErrInvalidRoutine, MaxNameLength)
	}
	if len(d.Instruction) > MaxInstructionLn {
		return fmt.Errorf("%w: the instruction is longer than %d characters", ErrInvalidRoutine, MaxInstructionLn)
	}
	if strings.TrimSpace(d.ParkID) == "" {
		return fmt.Errorf("%w: a park is required", ErrInvalidRoutine)
	}
	roles := dedupe(d.AssigneeRoles)
	if len(roles) == 0 {
		return fmt.Errorf("%w: pick at least one role the routine is for", ErrNoRoles)
	}
	if len(roles) != len(d.AssigneeRoles) {
		return fmt.Errorf("%w: a role is chosen twice", ErrInvalidRoutine)
	}
	for _, role := range roles {
		if !IsAssignableRole(role) {
			return fmt.Errorf("%w: %q is not a role a routine can be for", ErrInvalidRoutine, role)
		}
	}
	if _, err := time.Parse("2006-01-02", strings.TrimSpace(d.StartDate)); err != nil {
		return fmt.Errorf("%w: the start date must be a date", ErrInvalidRoutine)
	}
	switch d.ScopeKind {
	case ScopeAllPens:
	case ScopeSelectedPens:
		if len(d.Pens) == 0 {
			return fmt.Errorf("%w: tick at least one pen, or choose all pens", ErrInvalidRoutine)
		}
	case ScopePark:
		if len(d.Pens) > 0 {
			return fmt.Errorf("%w: a whole-park routine names no pens", ErrInvalidRoutine)
		}
		if d.CadenceKind == CadenceAfterWork {
			return fmt.Errorf("%w: a whole-park routine cannot follow work in a pen; choose pens, or a calendar cadence", ErrInvalidRoutine)
		}
	default:
		return fmt.Errorf("%w: unknown scope %q", ErrInvalidRoutine, d.ScopeKind)
	}
	switch d.CadenceKind {
	case CadenceDaily:
	case CadenceWeekly:
		if len(d.Weekdays) == 0 {
			return fmt.Errorf("%w: pick at least one weekday", ErrInvalidRoutine)
		}
		for _, wd := range d.Weekdays {
			if wd < 1 || wd > 7 {
				return fmt.Errorf("%w: weekday %d is not 1..7", ErrInvalidRoutine, wd)
			}
		}
	case CadenceMonthly:
		if len(d.MonthDays) == 0 {
			return fmt.Errorf("%w: pick at least one day of the month", ErrInvalidRoutine)
		}
		for _, md := range d.MonthDays {
			if md < 1 || md > 31 {
				return fmt.Errorf("%w: month day %d is not 1..31", ErrInvalidRoutine, md)
			}
		}
	case CadenceEveryNDays:
		if d.IntervalDays < MinIntervalDays || d.IntervalDays > MaxIntervalDays {
			return fmt.Errorf("%w: every how many days must be %d to %d", ErrInvalidRoutine, MinIntervalDays, MaxIntervalDays)
		}
	case CadenceAfterWork:
		if len(SortWorkKinds(d.AfterWorkKinds)) == 0 || len(SortWorkKinds(d.AfterWorkKinds)) != len(dedupe(d.AfterWorkKinds)) {
			return fmt.Errorf("%w: pick at least one known kind of work", ErrInvalidRoutine)
		}
	default:
		return fmt.Errorf("%w: unknown cadence %q", ErrInvalidRoutine, d.CadenceKind)
	}
	if d.DueOffsetDays < 0 || d.DueOffsetDays > MaxDueOffsetDays {
		return fmt.Errorf("%w: due offset must be 0..%d days", ErrInvalidRoutine, MaxDueOffsetDays)
	}
	if _, _, err := ParseClock(d.NotifyTime); err != nil {
		return fmt.Errorf("%w: notify time must be HH:MM", ErrInvalidRoutine)
	}
	if d.ReviewKind != ReviewVerifier && d.ReviewKind != ReviewNone {
		return fmt.Errorf("%w: review must be %q or %q", ErrInvalidRoutine, ReviewVerifier, ReviewNone)
	}
	if d.Status != "" && d.Status != StatusActive && d.Status != StatusPaused && d.Status != StatusRetired {
		return fmt.Errorf("%w: unknown status %q", ErrInvalidRoutine, d.Status)
	}
	return ValidateEvidence(d.Evidence)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// ParseClock reads a local "HH:MM".
func ParseClock(s string) (hour, minute int, err error) {
	s = strings.TrimSpace(s)
	if len(s) == 8 && s[5] == ':' { // "HH:MM:SS" from Postgres time
		s = s[:5]
	}
	if len(s) != 5 || s[2] != ':' {
		return 0, 0, fmt.Errorf("bad clock %q", s)
	}
	if _, err := fmt.Sscanf(s, "%02d:%02d", &hour, &minute); err != nil {
		return 0, 0, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("bad clock %q", s)
	}
	return hour, minute, nil
}

// StartedBy reports whether the routine has started on the business date: nothing raises
// before StartDate, for every cadence (after_work included). An unreadable date raises nothing.
func (d Definition) StartedBy(businessDate string) bool {
	day, err := time.Parse("2006-01-02", businessDate)
	if err != nil {
		return false
	}
	start, err := time.Parse("2006-01-02", strings.TrimSpace(d.StartDate))
	if err != nil {
		return false
	}
	return !day.Before(start)
}

// RaisesOn reports whether a calendar-cadence routine raises a task for the given business date
// (YYYY-MM-DD). Never before StartDate. An after_work routine never raises from the calendar;
// its dates come from the work the materializer reads.
func (d Definition) RaisesOn(businessDate string) bool {
	day, err := time.Parse("2006-01-02", businessDate)
	if err != nil {
		return false
	}
	if !d.StartedBy(businessDate) {
		return false
	}
	switch d.CadenceKind {
	case CadenceDaily:
		return true
	case CadenceEveryNDays:
		if d.IntervalDays < 1 {
			return false
		}
		start, _ := time.Parse("2006-01-02", strings.TrimSpace(d.StartDate))
		days := int(day.Sub(start).Hours() / 24)
		return days >= 0 && days%d.IntervalDays == 0
	case CadenceWeekly:
		iso := int(day.Weekday())
		if iso == 0 {
			iso = 7
		}
		for _, wd := range d.Weekdays {
			if wd == iso {
				return true
			}
		}
		return false
	case CadenceMonthly:
		last := time.Date(day.Year(), day.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		for _, md := range d.MonthDays {
			effective := md
			if effective > last {
				effective = last // a day past the month's end means its last day
			}
			if effective == day.Day() {
				return true
			}
		}
		return false
	}
	return false
}

// PlannedDateFor is the planned business date for a task raised by sourceDate.
func (d Definition) PlannedDateFor(sourceDate string) (string, error) {
	src, err := time.Parse("2006-01-02", sourceDate)
	if err != nil {
		return "", err
	}
	return src.AddDate(0, 0, d.DueOffsetDays).Format("2006-01-02"), nil
}

// CadenceLine is the farm sentence for a routine's cadence: "Every day", "Mon, Wed, Fri",
// "1st and 15th of the month", "The day after deworming or ticks removal".
func CadenceLine(d Definition) string {
	switch d.CadenceKind {
	case CadenceDaily:
		return "Every day"
	case CadenceEveryNDays:
		return fmt.Sprintf("Every %d days", d.IntervalDays)
	case CadenceWeekly:
		names := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
		days := append([]int(nil), d.Weekdays...)
		sort.Ints(days)
		parts := make([]string, 0, len(days))
		for _, wd := range days {
			if wd >= 1 && wd <= 7 {
				parts = append(parts, names[wd-1])
			}
		}
		if len(parts) == 7 {
			return "Every day"
		}
		return "Every " + strings.Join(parts, ", ")
	case CadenceMonthly:
		days := append([]int(nil), d.MonthDays...)
		sort.Ints(days)
		parts := make([]string, 0, len(days))
		for _, md := range days {
			parts = append(parts, ordinal(md))
		}
		return joinAnd(parts) + " of the month"
	case CadenceAfterWork:
		labels := make([]string, 0, len(d.AfterWorkKinds))
		for _, k := range SortWorkKinds(d.AfterWorkKinds) {
			labels = append(labels, strings.ToLower(WorkKindLabel(k)))
		}
		when := "The day after"
		switch d.DueOffsetDays {
		case 0:
			when = "The same day as"
		case 1:
		default:
			when = fmt.Sprintf("%d days after", d.DueOffsetDays)
		}
		return when + " " + joinOr(labels)
	}
	return ""
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func joinAnd(parts []string) string { return joinWith(parts, "and") }
func joinOr(parts []string) string  { return joinWith(parts, "or") }

func joinWith(parts []string, word string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " " + word + " " + parts[1]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " " + word + " " + parts[len(parts)-1]
}

// EvidenceLine is the one-line summary of what a pen routine asks for: "2 questions · 1 photo ·
// check in to pen".
func EvidenceLine(e Evidence) string { return EvidenceLineForScope(e, ScopeAllPens) }

// EvidenceLineForScope is EvidenceLine worded for the routine's scope: a whole-park routine
// asks to "check in", not to check in to a pen.
func EvidenceLineForScope(e Evidence, scopeKind string) string {
	parts := []string{}
	switch n := len(e.Questions); n {
	case 0:
	case 1:
		parts = append(parts, "1 question")
	default:
		parts = append(parts, fmt.Sprintf("%d questions", n))
	}
	if s := proofPart("photo", e.Photo); s != "" {
		parts = append(parts, s)
	}
	if s := proofPart("video", e.Video); s != "" {
		parts = append(parts, s)
	}
	if e.PresenceRequired() {
		if scopeKind == ScopePark {
			parts = append(parts, "check in")
		} else {
			parts = append(parts, "check in to pen")
		}
	}
	if len(parts) == 0 {
		return "Tap to confirm"
	}
	return strings.Join(parts, " · ")
}

func proofPart(kind string, r ProofRule) string {
	switch {
	case r.Max == 0:
		return ""
	case r.Min == r.Max:
		return plural(r.Min, kind)
	case r.Min == 0:
		return "up to " + plural(r.Max, kind)
	default:
		return fmt.Sprintf("%d to %d %ss", r.Min, r.Max, kind)
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// StatusLabel is the farm word for a routine status.
func StatusLabel(s string) string {
	switch s {
	case StatusActive:
		return "Active"
	case StatusPaused:
		return "Paused"
	case StatusRetired:
		return "Retired"
	}
	return s
}

// CheckAnswers validates a submitted answer set against the pinned evidence: every required
// question answered, every answer of the right shape, no answer to a question that does not
// exist. Returns the normalized answers to store.
func CheckAnswers(e Evidence, raw map[string]json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	for id := range raw {
		if _, ok := e.QuestionByID(id); !ok {
			return nil, fmt.Errorf("%w: %q is not a question of this routine", ErrAnswerInvalid, id)
		}
	}
	for _, q := range e.Questions {
		v, present := raw[q.ID]
		if !present || len(v) == 0 || string(v) == "null" {
			if q.Required {
				return nil, fmt.Errorf("%w: %q needs an answer", ErrAnswerInvalid, q.Title)
			}
			continue
		}
		switch q.Kind {
		case QuestionYesNo, QuestionChoice:
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, fmt.Errorf("%w: %q needs one choice", ErrAnswerInvalid, q.Title)
			}
			s = strings.TrimSpace(s)
			if s == "" {
				if q.Required {
					return nil, fmt.Errorf("%w: %q needs an answer", ErrAnswerInvalid, q.Title)
				}
				continue
			}
			if !hasOption(q, s) {
				return nil, fmt.Errorf("%w: %q is not a choice of %q", ErrAnswerInvalid, s, q.Title)
			}
			out[q.ID] = s
		case QuestionMultiChoice:
			var list []string
			if err := json.Unmarshal(v, &list); err != nil {
				return nil, fmt.Errorf("%w: %q needs a list of choices", ErrAnswerInvalid, q.Title)
			}
			list = dedupe(list)
			if len(list) == 0 {
				if q.Required {
					return nil, fmt.Errorf("%w: %q needs at least one choice", ErrAnswerInvalid, q.Title)
				}
				continue
			}
			for _, s := range list {
				if !hasOption(q, s) {
					return nil, fmt.Errorf("%w: %q is not a choice of %q", ErrAnswerInvalid, s, q.Title)
				}
			}
			out[q.ID] = list
		case QuestionNumber:
			var f float64
			if err := json.Unmarshal(v, &f); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				return nil, fmt.Errorf("%w: %q needs a number", ErrAnswerInvalid, q.Title)
			}
			if (q.Min != nil && f < *q.Min) || (q.Max != nil && f > *q.Max) {
				return nil, fmt.Errorf("%w: %q is outside its range", ErrAnswerInvalid, q.Title)
			}
			out[q.ID] = f
		case QuestionText:
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return nil, fmt.Errorf("%w: %q needs text", ErrAnswerInvalid, q.Title)
			}
			s = strings.TrimSpace(s)
			if len(s) > MaxTextAnswer {
				return nil, fmt.Errorf("%w: %q is longer than %d characters", ErrAnswerInvalid, q.Title, MaxTextAnswer)
			}
			if s == "" {
				if q.Required {
					return nil, fmt.Errorf("%w: %q needs an answer", ErrAnswerInvalid, q.Title)
				}
				continue
			}
			out[q.ID] = s
		}
	}
	return out, nil
}

func hasOption(q Question, value string) bool {
	for _, o := range q.Options {
		if o.Value == value {
			return true
		}
	}
	return false
}

// RenderAnswer is the verifier's / web's reading of one stored answer: option labels, "Yes",
// numbers with their unit, text verbatim. "" when unanswered.
func RenderAnswer(q Question, v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return optionLabel(q, val)
	case []string:
		parts := make([]string, 0, len(val))
		for _, s := range val {
			parts = append(parts, optionLabel(q, s))
		}
		return strings.Join(parts, ", ")
	case []any:
		parts := make([]string, 0, len(val))
		for _, s := range val {
			if str, ok := s.(string); ok {
				parts = append(parts, optionLabel(q, str))
			}
		}
		return strings.Join(parts, ", ")
	case float64:
		s := formatNumber(val)
		if q.Unit != "" {
			return s + " " + q.Unit
		}
		return s
	}
	return fmt.Sprint(v)
}

func optionLabel(q Question, value string) string {
	for _, o := range q.Options {
		if o.Value == value {
			return o.Label
		}
	}
	return value
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprintf("%d", int64(f))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", f), "0"), ".")
}
