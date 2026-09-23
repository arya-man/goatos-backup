package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The authoring vocabulary for diagnosis TYPES and the stages that reach them
// (migration 000395). The rules here are the ones a screen must not be able to talk the
// backend out of, so they live beside the resolution rule they protect rather than in a
// handler.

var (
	// ErrInvalidHealthConfig is a malformed type or route write. Every message wrapping it is
	// returned to the screen verbatim, so each one names the field and says what a good value
	// looks like rather than reporting that validation failed.
	ErrInvalidHealthConfig = errors.New("health: that diagnosis configuration is not valid")

	// ErrTypeKeyInUse is a second type claiming a key this tenant already has.
	ErrTypeKeyInUse = errors.New("health: a diagnosis type with that key already exists")

	// ErrTypeNotFound is an unknown type key.
	ErrTypeNotFound = errors.New("health: diagnosis type not found")

	// ErrBuiltinTypeNotRetirable is an attempt to retire one of the four shipped types.
	//
	// Their registers are the committed rulebook every other type is authored beside, and a
	// farm with none of them has nothing to diagnose against. They may be RELABELLED and
	// RE-ROUTED freely -- what is refused is removing them from the vocabulary.
	ErrBuiltinTypeNotRetirable = errors.New("health: a built-in diagnosis type cannot be retired")

	// ErrTypeStillRouted is an attempt to retire a type that stages still point at.
	//
	// Retiring it would refuse those animals with "its stage is not mapped", which names the
	// stage but not the real cause, so the author is asked to move the stages first. It is the
	// one case where the order of two edits matters, and saying so beats a puzzling refusal in
	// a shed.
	ErrTypeStillRouted = errors.New("health: stages still route to this diagnosis type")

	// ErrRouteTypeUnknown is a route naming a type that does not exist or is retired.
	ErrRouteTypeUnknown = errors.New("health: that route names a diagnosis type that is not active")

	// ErrRouteNotFound is a delete addressing a route that is not there.
	ErrRouteNotFound = errors.New("health: stage route not found")

	// ErrStageUnknown is a route naming a management stage this farm does not have.
	//
	// It is refused rather than stored, because a route keyed on a stage no animal is ever on
	// matches NOTHING, for ever, and says so only as a zero in a column. On 2026-09-23 the
	// maintainer typed `mothers` where the farm's stage is `Mother`, and the screen accepted it,
	// showed "0 animals", and left five does routed to the adult wildcard -- a dead rule that
	// looked authored. A clinical routing table cannot have those in it.
	ErrStageUnknown = errors.New("health: that management stage is not one this farm uses")
)

// typeKeyShape mirrors the CHECK on health_diagnosis_types.type_key. The key is written into
// register versions and stored runs, so it stays machine-safe and lower-case; the LABEL is
// where a farm puts its own words.
var typeKeyShape = regexp.MustCompile(`^[a-z][a-z0-9_]{0,48}$`)

// BuiltinTypeKeys are the four types the engine shipped with.
func BuiltinTypeKeys() []string {
	return []string{"adult", "kid_milk", "kid_weaning", "kid_fattening"}
}

// IsBuiltinTypeKey reports whether a key is one of the shipped four.
func IsBuiltinTypeKey(key string) bool {
	for _, k := range BuiltinTypeKeys() {
		if k == strings.ToLower(strings.TrimSpace(key)) {
			return true
		}
	}
	return false
}

// DiagnosisType is one authored type as a screen reads it.
type DiagnosisType struct {
	TypeKey   string `json:"type_key"`
	Label     string `json:"label"`
	Status    string `json:"status"`
	SortOrder int    `json:"sort_order"`
	IsBuiltin bool   `json:"is_builtin"`

	// RouteCount is how many stages reach this type, so a screen can warn before a retire
	// and show at a glance which types are actually in use.
	RouteCount int `json:"route_count"`

	// HasPublishedRegister says whether this type can diagnose anything yet. A type with no
	// published register is a real and expected state -- it is what a farm has between
	// creating the type and authoring its rules -- and the screen must say so rather than
	// leave the reader to infer it from a blank row.
	HasPublishedRegister bool      `json:"has_published_register"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// StageRouteRow is one authored route as a screen reads it, with the labels resolved.
type StageRouteRow struct {
	AgeBand   string `json:"age_band"`
	StageCode string `json:"stage_code"`
	TypeKey   string `json:"type_key"`
	SubStage  string `json:"sub_stage"`

	// StageLabel is the farm's own name for the stage from animal_stage_lookup ("Milk
	// drinking" for k2). Blank for the wildcard and for a stage the catalog no longer holds.
	StageLabel string `json:"stage_label"`
	TypeLabel  string `json:"type_label"`

	// IsWildcard marks the band-wide row, which a screen must render differently: deleting it
	// makes a whole age band fail-closed, which is not what a reader expects from a row that
	// otherwise looks like the others.
	IsWildcard bool `json:"is_wildcard"`

	// LiveAnimals is how many alive animals sit on this stage right now. It is the number that
	// makes a routing screen worth opening -- it tells a director whether a change touches five
	// animals or seven hundred.
	LiveAnimals int `json:"live_animals"`

	// StageRetired marks a route whose stage the catalog no longer holds. Such a route matches
	// nothing and is shown so it can be removed, rather than being hidden and left to puzzle
	// somebody later.
	StageRetired bool `json:"stage_retired"`
}

// AvailableStage is one management stage this farm actually uses, offered to the routing screen
// so a stage code is PICKED rather than typed.
//
// The picker is the fix for a whole failure class, not a convenience: a typed stage code that
// matches no animal produces a route that can never fire, and the only symptom is a zero in a
// column nobody is watching.
type AvailableStage struct {
	AgeBand     string `json:"age_band"`
	StageCode   string `json:"stage_code"`
	StageLabel  string `json:"stage_label"`
	LiveAnimals int    `json:"live_animals"`
	// Routed says a route already names this stage, so the screen can show what is already
	// covered without a second read.
	Routed bool `json:"routed"`
}

// UnroutedStage is a stage that holds animals and reaches no type.
//
// This is the whole reason the screen exists. On 2026-09-23 `Warmup` was in exactly this state
// with 58 live kids, and nothing anywhere said so -- the only symptom was a manager being
// refused in a shed. Surfacing it is how that stops being a discovery.
type UnroutedStage struct {
	AgeBand     string `json:"age_band"`
	StageCode   string `json:"stage_code"`
	StageLabel  string `json:"stage_label"`
	LiveAnimals int    `json:"live_animals"`

	// ClinicalPlacement marks ICU / Quarantine-style stages. They say WHERE an animal is
	// rather than what it eats or how old it is, so they cannot choose a register and a farm
	// should NOT be nagged to map them. Kids in ICU-Kid have always been refused, by design
	// (2026-08-17), and showing that as a gap to close would invite exactly the wrong fix.
	ClinicalPlacement bool `json:"clinical_placement"`
}

// DiagnosisRoutingView is the whole Types screen in one read.
type DiagnosisRoutingView struct {
	Types          []DiagnosisType  `json:"types"`
	Routes         []StageRouteRow  `json:"routes"`
	UnroutedStages []UnroutedStage  `json:"unrouted_stages"`
	Stages         []AvailableStage `json:"stages"`
}

// SaveDiagnosisTypeCommand creates or relabels a type.
type SaveDiagnosisTypeCommand struct {
	TenantID           string
	ActorID            string
	IdempotencyKey     string
	RequestFingerprint string

	TypeKey   string
	Label     string
	Status    string
	SortOrder int
}

// Validate normalises and checks a type write.
//
// The KEY is only accepted on creation and is never rewritten afterwards, because a stored run
// names the type it was judged under; the repository enforces that half. Here we only insist the
// key is machine-safe and the label is a real word.
func (c *SaveDiagnosisTypeCommand) Validate() error {
	c.TypeKey = strings.ToLower(strings.TrimSpace(c.TypeKey))
	c.Label = strings.TrimSpace(c.Label)
	c.Status = strings.ToLower(strings.TrimSpace(c.Status))
	if c.Status == "" {
		c.Status = "active"
	}

	if !typeKeyShape.MatchString(c.TypeKey) {
		return fmt.Errorf("%w: a type key is lower-case letters, digits and underscores, starting with a letter", ErrInvalidHealthConfig)
	}
	if c.Label == "" {
		return fmt.Errorf("%w: a diagnosis type needs a name", ErrInvalidHealthConfig)
	}
	if len([]rune(c.Label)) > 80 {
		return fmt.Errorf("%w: that name is too long for a screen to show", ErrInvalidHealthConfig)
	}
	if c.Status != "active" && c.Status != "retired" {
		return fmt.Errorf("%w: a type is active or retired", ErrInvalidHealthConfig)
	}
	if c.Status == "retired" && IsBuiltinTypeKey(c.TypeKey) {
		return ErrBuiltinTypeNotRetirable
	}
	return nil
}

// SaveStageRouteCommand points one stage, or a whole age band, at a type.
type SaveStageRouteCommand struct {
	TenantID           string
	ActorID            string
	IdempotencyKey     string
	RequestFingerprint string

	AgeBand   string
	StageCode string
	TypeKey   string
	SubStage  string
}

// Validate normalises and checks a route write.
//
// The SUB-STAGE is validated against the type it is being written for, because it is not
// decoration: inside kid_milk the register runs a different ladder for a week-old K1 than for a
// K2 on the free-choice bar. A sub-stage on a type whose register reads none is accepted as
// blank rather than refused -- it is inert there, and refusing it would make the routing screen
// fussy about a field it does not show.
func (c *SaveStageRouteCommand) Validate() error {
	c.AgeBand = strings.ToLower(strings.TrimSpace(c.AgeBand))
	c.StageCode = strings.ToLower(strings.TrimSpace(c.StageCode))
	c.TypeKey = strings.ToLower(strings.TrimSpace(c.TypeKey))
	c.SubStage = strings.TrimSpace(c.SubStage)

	if c.AgeBand != AgeBandAdult && c.AgeBand != AgeBandKid {
		return fmt.Errorf("%w: a route is for adults or kids", ErrInvalidHealthConfig)
	}
	if c.StageCode == "" {
		return fmt.Errorf("%w: a route needs a management stage, or %q for every stage in the band", ErrInvalidHealthConfig, StageWildcard)
	}
	if len(c.StageCode) > 64 {
		return fmt.Errorf("%w: that stage code is too long", ErrInvalidHealthConfig)
	}
	if !typeKeyShape.MatchString(c.TypeKey) {
		return fmt.Errorf("%w: a route must name a diagnosis type", ErrInvalidHealthConfig)
	}
	if len(c.SubStage) > 16 {
		return fmt.Errorf("%w: that sub-stage is too long", ErrInvalidHealthConfig)
	}
	return nil
}

// DeleteStageRouteCommand removes one route.
type DeleteStageRouteCommand struct {
	TenantID           string
	ActorID            string
	IdempotencyKey     string
	RequestFingerprint string

	AgeBand   string
	StageCode string
}

// Validate normalises and checks a route delete.
func (c *DeleteStageRouteCommand) Validate() error {
	c.AgeBand = strings.ToLower(strings.TrimSpace(c.AgeBand))
	c.StageCode = strings.ToLower(strings.TrimSpace(c.StageCode))
	if c.AgeBand != AgeBandAdult && c.AgeBand != AgeBandKid {
		return fmt.Errorf("%w: a route is for adults or kids", ErrInvalidHealthConfig)
	}
	if c.StageCode == "" {
		return fmt.Errorf("%w: which stage's route should be removed?", ErrInvalidHealthConfig)
	}
	return nil
}

// ClinicalPlacementStages are the stages that say WHERE an animal is rather than what it eats
// or how old it is.
//
// They are reported as such rather than as routing gaps. A farm mapping `ICU` onto a diagnosis
// type would be making a medical call from a placement fact, which is the thing
// protocol/domain.IsClinicalManagementStage exists to prevent on the movement side; the same
// reasoning applies here and is why 2026-08-17 left ICU-Kid refused on purpose.
func IsClinicalPlacementStage(stage string) bool {
	s := strings.ToLower(strings.TrimSpace(stage))
	for _, prefix := range []string{"icu", "quarantine", "sick", "under_treatment", "recovering"} {
		if s == prefix || strings.HasPrefix(s, prefix+"-") || strings.HasPrefix(s, prefix+" ") {
			return true
		}
	}
	return false
}
