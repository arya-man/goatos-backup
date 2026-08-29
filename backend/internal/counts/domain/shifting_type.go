package domain

import (
	"strings"

	protocoldomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

// Shifting rewrite: THE SHIFT TYPE DECIDES THE TAG (maintainer decisions 2026-08-20; canonical
// prose docs/features/shifting/shifting-rewrite-tag-rules.md).
//
// The raiser no longer chooses tag behaviour -- the 2026-08-15 stage_mode toggle is retired for
// typed raises. Every raise carries a category naming WHY the animals move, and the category
// carries a fixed rule for what happens to their tag:
//
//	health    -> destination tag on both legs, INCLUDING a clinical state (the one type allowed to)
//	growth    -> destination tag, FORWARD ONLY along the authored ladder (one reverse edge)
//	breeding  -> tag never changes (a visitor placed with a mate)
//	delivery  -> destination tag, except it NEVER stamps the newborn stage (that tag is the kids')
//	spacing   -> tag travels with the animals; the whole pen moves; the destination must agree
//	flushing  -> the Flushing tag; destination must be empty or already flushing
//
// Three shapes underneath the six types: PROGRESSION (growth, flushing -- the animal changed, take
// the destination tag), TEMPORARY RESIDENCE (breeding, delivery, health -- a visitor; health is the
// exception because being in ICU IS a change), and CAPACITY (spacing -- nothing changed, the tag
// travels and the PEN adapts).
//
// Everything here is pure Go so the whole rulebook is unit-testable without a database. The
// handler supplies catalog + goat facts; this file answers with either a decision (target stage +
// optional pen adoption) or a refusal carrying backend-owned farm copy.

// Shift types. Stored in shifting_events.category (vocabulary widened by migration 000179).
const (
	ShiftTypeHealth   = "health"
	ShiftTypeGrowth   = "growth"
	ShiftTypeBreeding = "breeding"
	ShiftTypeDelivery = "delivery"
	ShiftTypeSpacing  = "spacing"
	ShiftTypeFlushing = "flushing"
)

// NewbornStageCode is the stage a newborn already receives at birth registration. Delivery
// movements must never stamp it on a mother accompanying her kids into the kidding pen. Kept as
// one constant so the rule reads from a single place; the handler still canonicalizes every stage
// against the tenant's live animal_stage_lookup vocabulary.
const NewbornStageCode = "K0"

// growthForwardEdges is the authored lifecycle ladder (maintainer dictation 2026-08-20).
//
//	K0 -> K1 -> K2 -> K3 -+- F2 -+- F2-Male   -> Buck
//	                      |      +- F2-Female -> Non-Pregnant <-> Pregnant
//	                      +------- (K3 may also split by sex directly)
//
// AUTHORED DATA, NOT sort_order: animal_stage_lookup.sort_order is display order and runs
// Mother -> Milking -> M0 -> Pregnant -> Non-Pregnant, which would make Non-Pregnant the final
// rung and forbid the one reverse edge the maintainer explicitly allowed (a pregnancy that does
// not hold returns to Non-Pregnant). So the ladder is its own explicit edge set.
//
// DELIBERATE ABSENCES, recorded in the rules doc as open decisions: Mother, Milking, M0 and
// Warmup are real vocabulary stages with NO growth edges -- a growth raise touching them is
// refused until the maintainer places them. F2-Male -> Buck is present per the maintainer's
// 2026-08-20 confirmation ("it is" a growth step); most fattening males exit by sale instead,
// which is not a shifting.
//
// Keys and values are canonical stage codes; comparison is case-insensitive via growthEdgeExists.
var growthForwardEdges = map[string][]string{
	"K0":           {"K1"},
	"K1":           {"K2"},
	"K2":           {"K3"},
	"K3":           {"F2", "F2-Male", "F2-Female"},
	"F2":           {"F2-Male", "F2-Female"},
	"F2-Male":      {"Buck"},
	"F2-Female":    {"Non-Pregnant"},
	"Non-Pregnant": {"Pregnant"},
	// The ONE permitted reverse edge: a pregnancy that does not hold, or completes, returns her.
	"Pregnant": {"Non-Pregnant"},
}

// growthStageSex is the sex a stage implies. A growth step into a sexed stage refuses an animal of
// the other (or unknown) sex -- an F2 pen's mixed group splits by sex across TWO raises, one per
// sexed destination, and each raise checks every animal it actually carries.
var growthStageSex = map[string]string{
	"F2-Male":      "male",
	"Buck":         "male",
	"F2-Female":    "female",
	"Non-Pregnant": "female",
	"Pregnant":     "female",
}

// FlushingShiftStage is the tag a flushing movement stamps; same constant the stage-resolution
// rules already carry (FlushingStageName) -- aliased here so this file reads self-contained.
const FlushingShiftStage = FlushingStageName

// KnownShiftType reports whether s is one of the six typed-raise categories.
func KnownShiftType(s string) bool {
	switch s {
	case ShiftTypeHealth, ShiftTypeGrowth, ShiftTypeBreeding, ShiftTypeDelivery, ShiftTypeSpacing, ShiftTypeFlushing:
		return true
	}
	return false
}

// ShiftTypeAnimal is one moving animal's facts as the rulebook needs them.
type ShiftTypeAnimal struct {
	GoatID string
	// Stage is the animal's CURRENT management stage ("" when it has none recorded).
	Stage string
	// Sex is "male"/"female"/"" (unknown).
	Sex string
}

// ShiftTypeContext is everything a typed raise's tag decision depends on. The handler assembles it
// from the destination catalog (the SAME catalog the form renders) and the animals' canonical facts.
type ShiftTypeContext struct {
	Type string

	// Destination pen facts, from the catalog entry the raise addressed (shed + partition).
	// ConfiguredStage is the pen's AUTHORED tag ("" when nobody set one); ResidentStages the
	// distinct non-clinical stages its shed's live residents carry (the legacy fallback signal);
	// HeadCount the live animals standing in this exact pen (0 = empty).
	DestinationConfiguredStage string
	DestinationResidentStages  []string
	DestinationHeadCount       int
	// DestinationKnown is false when the raise's destination pen was not found in the active
	// catalog. The relocation's own ground-truth check would fail it later anyway; typed raises
	// that need pen facts fail it here, before approval.
	DestinationKnown bool

	// Source pen facts, resolved from the animals' shared current placement. SourceKnown is false
	// when the animals do not share one source pen or it is absent from the catalog.
	SourceConfiguredStage string
	SourceHeadCount       int
	SourceKnown           bool

	// Animals are the moving group's per-animal facts.
	Animals []ShiftTypeAnimal

	// WritableStages is the tenant's active animal_stage_lookup vocabulary (canonical casing).
	WritableStages []string
}

// ShiftTypeDecision is a typed raise's resolved outcome.
type ShiftTypeDecision struct {
	// TargetStage is the tag stamped on every moved animal at apply ("" = each animal keeps its
	// own current stage), in the writable vocabulary's canonical casing.
	TargetStage string
	// AdoptPenTag, when non-empty, is the tag the DESTINATION PEN itself adopts at apply (an empty
	// pen receiving a spacing/delivery/flushing group). Snapshotted on the event and re-validated
	// under the apply row lock.
	AdoptPenTag string
	// AllowClinicalTarget marks a health movement whose target is a clinical state; the relocation
	// path refuses clinical tags for every other caller.
	AllowClinicalTarget bool
}

// ShiftTypeRefusal is a raise-time refusal. Code is the machine error code; Message is
// BACKEND-OWNED FARM COPY rendered verbatim to the operator (golden frontend rule -- the phone
// must never compose its own reason).
type ShiftTypeRefusal struct {
	Code    string
	Message string
}

func refuse(code, message string) *ShiftTypeRefusal {
	return &ShiftTypeRefusal{Code: code, Message: message}
}

// ResolveShiftTypeDecision runs the typed rulebook. Exactly one of (decision, refusal) is
// meaningful: a non-nil refusal means the raise must be rejected with that code and copy.
func ResolveShiftTypeDecision(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	switch ctx.Type {
	case ShiftTypeHealth:
		return resolveHealthShift(ctx)
	case ShiftTypeGrowth:
		return resolveGrowthShift(ctx)
	case ShiftTypeBreeding:
		// A visitor placed with a mate: the tag never changes, and any destination is acceptable
		// because nothing is stamped. The pen briefly holding a tag not its own is one of the two
		// accepted temporary mixed-tag cases.
		return ShiftTypeDecision{}, nil
	case ShiftTypeDelivery:
		return resolveDeliveryShift(ctx)
	case ShiftTypeSpacing:
		return resolveSpacingShift(ctx)
	case ShiftTypeFlushing:
		return resolveFlushingShift(ctx)
	}
	return ShiftTypeDecision{}, refuse("invalid_category",
		"category must be growth, health, breeding, delivery, spacing, or flushing")
}

// destinationEffectiveTag is the tag the destination pen offers a movement: the authored tag
// first (what somebody decided the pen is for), else the residents' single shared stage, else "".
// allowClinical governs whether a clinical state may be offered (health movements only).
func destinationEffectiveTag(ctx ShiftTypeContext, allowClinical bool) (string, *ShiftTypeRefusal) {
	if configured := strings.TrimSpace(ctx.DestinationConfiguredStage); configured != "" {
		if protocoldomain.IsClinicalManagementStage(configured) && !allowClinical {
			return "", refuse("destination_tag_not_applicable", StageReasonNotApplicable)
		}
		if canonical := canonicalWritableStage(configured, ctx.WritableStages); canonical != "" {
			return canonical, nil
		}
		return "", refuse("destination_tag_not_applicable", StageReasonNotApplicable)
	}
	resolution := ResolveShiftingDestinationStageDetailed(ctx.DestinationResidentStages, ctx.WritableStages)
	if resolution.Stage != "" {
		return resolution.Stage, nil
	}
	switch resolution.Reason {
	case StageReasonMixed:
		return "", refuse("destination_tag_mixed", StageReasonMixed)
	case StageReasonNotApplicable:
		return "", refuse("destination_tag_not_applicable", StageReasonNotApplicable)
	}
	return "", refuse("destination_tag_missing", StageReasonNoTag)
}

// resolveHealthShift: destination tag on both legs, and this is the ONE type allowed to stamp a
// clinical state -- a health shifting IS the health team acting (maintainer decision 2026-08-20,
// superseding the 2026-08-15 clinical lock FOR THIS TYPE ONLY). The return leg is just another
// health shift whose destination happens to be a normal pen; there is no memory of the pre-ICU tag.
func resolveHealthShift(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	if !ctx.DestinationKnown {
		return ShiftTypeDecision{}, refuse("destination_not_in_catalog", shiftCopyDestinationUnknown)
	}
	tag, refusal := destinationEffectiveTag(ctx, true)
	if refusal != nil {
		return ShiftTypeDecision{}, refusal
	}
	return ShiftTypeDecision{
		TargetStage:         tag,
		AllowClinicalTarget: protocoldomain.IsClinicalManagementStage(tag),
	}, nil
}

// resolveGrowthShift: destination tag, forward only. Every animal in the raise must have a ladder
// edge from its CURRENT stage to the destination tag, and a sexed destination refuses animals of
// the other or unknown sex. Backward or sideways is refused outright -- "forward only" only means
// something if the system refuses.
func resolveGrowthShift(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	if !ctx.DestinationKnown {
		return ShiftTypeDecision{}, refuse("destination_not_in_catalog", shiftCopyDestinationUnknown)
	}
	tag, refusal := destinationEffectiveTag(ctx, false)
	if refusal != nil {
		return ShiftTypeDecision{}, refusal
	}
	requiredSex := growthStageSexFor(tag)
	for _, animal := range ctx.Animals {
		stage := strings.TrimSpace(animal.Stage)
		if stage == "" {
			return ShiftTypeDecision{}, refuse("growth_stage_unknown", shiftCopyGrowthStageUnknown)
		}
		if !growthEdgeExists(stage, tag) {
			return ShiftTypeDecision{}, refuse("growth_not_next_stage", shiftCopyGrowthNotNext)
		}
		if requiredSex != "" && !strings.EqualFold(strings.TrimSpace(animal.Sex), requiredSex) {
			return ShiftTypeDecision{}, refuse("growth_sex_mismatch", shiftCopyGrowthSexMismatch)
		}
	}
	return ShiftTypeDecision{TargetStage: tag}, nil
}

// resolveDeliveryShift: destination tag, except it never stamps the newborn stage -- a mother
// accompanying her kids into the kidding pen keeps her own tag; the K0 tag belongs to the kids.
// The destination itself says which leg of the journey this is, so no inbound/outbound flag exists.
// A delivery into an EMPTY untagged pen (the one-day recovery shed) keeps the mother's tag and
// tags that pen with it (maintainer decision 2026-08-20: "mother only at that time").
func resolveDeliveryShift(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	if !ctx.DestinationKnown {
		return ShiftTypeDecision{}, refuse("destination_not_in_catalog", shiftCopyDestinationUnknown)
	}
	configured := strings.TrimSpace(ctx.DestinationConfiguredStage)
	if configured == "" && ctx.DestinationHeadCount == 0 {
		// The empty recovery shed: the mother keeps her tag, the shed takes it.
		sourceTag, refusal := movingGroupSharedStage(ctx)
		if refusal != nil {
			return ShiftTypeDecision{}, refusal
		}
		return ShiftTypeDecision{AdoptPenTag: sourceTag}, nil
	}
	tag, refusal := destinationEffectiveTag(ctx, false)
	if refusal != nil {
		return ShiftTypeDecision{}, refusal
	}
	if strings.EqualFold(tag, NewbornStageCode) {
		// Into the kidding pen: keep her own tag. Not a refusal -- the movement is right, the
		// stamp would be wrong.
		return ShiftTypeDecision{}, nil
	}
	return ShiftTypeDecision{TargetStage: tag}, nil
}

// resolveSpacingShift: the whole pen moves, the tag travels, the destination must agree.
//
//   - every animal keeps its own current stage (TargetStage "");
//   - the ENTIRE source pen moves -- "half-half is not an option" -- so the group must equal the
//     pen's live population and the source pen is left empty;
//   - the destination must already carry the same tag, or be EMPTY, in which case it adopts the
//     source tag; anything else is refused at raise, before approval and before any video.
func resolveSpacingShift(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	if !ctx.DestinationKnown {
		return ShiftTypeDecision{}, refuse("destination_not_in_catalog", shiftCopyDestinationUnknown)
	}
	if !ctx.SourceKnown {
		return ShiftTypeDecision{}, refuse("spacing_source_unresolved", shiftCopySpacingSourceUnknown)
	}
	if len(ctx.Animals) != ctx.SourceHeadCount {
		return ShiftTypeDecision{}, refuse("spacing_partial_group", shiftCopySpacingPartialGroup)
	}
	sourceTag := strings.TrimSpace(ctx.SourceConfiguredStage)
	if sourceTag == "" {
		// Nobody authored the source pen's tag; the animals' own shared stage is the truthful one
		// they carry. A group that does not share one is blocked -- the approver-chooses-tag
		// capability is a recorded follow-up, not built yet.
		shared, refusal := movingGroupSharedStage(ctx)
		if refusal != nil {
			return ShiftTypeDecision{}, refusal
		}
		sourceTag = shared
	}
	if canonical := canonicalWritableStage(sourceTag, ctx.WritableStages); canonical != "" {
		sourceTag = canonical
	}
	destinationTag := strings.TrimSpace(ctx.DestinationConfiguredStage)
	switch {
	case destinationTag != "" && strings.EqualFold(destinationTag, sourceTag):
		return ShiftTypeDecision{}, nil
	case destinationTag == "" && ctx.DestinationHeadCount == 0:
		return ShiftTypeDecision{AdoptPenTag: sourceTag}, nil
	case destinationTag == "" && ctx.DestinationHeadCount > 0:
		return ShiftTypeDecision{}, refuse("spacing_destination_occupied", shiftCopySpacingDestinationOccupied)
	default:
		return ShiftTypeDecision{}, refuse("spacing_destination_mismatch", shiftCopySpacingDestinationMismatch)
	}
}

// resolveFlushingShift: a non-pregnant female moves onto flushing ration and adopts the Flushing
// tag. The destination must be empty (it becomes a flushing pen) or already flushing.
func resolveFlushingShift(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	if !ctx.DestinationKnown {
		return ShiftTypeDecision{}, refuse("destination_not_in_catalog", shiftCopyDestinationUnknown)
	}
	for _, animal := range ctx.Animals {
		if !strings.EqualFold(strings.TrimSpace(animal.Sex), "female") {
			return ShiftTypeDecision{}, refuse("flushing_requires_female", shiftCopyFlushingFemaleOnly)
		}
	}
	flushing := canonicalWritableStage(FlushingShiftStage, ctx.WritableStages)
	if flushing == "" {
		// The tenant's vocabulary does not carry Flushing as an active stage (migration 000171
		// backfills it); refusing beats stamping a tag the relocation would then reject.
		return ShiftTypeDecision{}, refuse("destination_tag_not_applicable", StageReasonNotApplicable)
	}
	destinationTag := strings.TrimSpace(ctx.DestinationConfiguredStage)
	switch {
	case destinationTag != "" && strings.EqualFold(destinationTag, flushing):
		return ShiftTypeDecision{TargetStage: flushing}, nil
	case destinationTag == "" && ctx.DestinationHeadCount == 0:
		return ShiftTypeDecision{TargetStage: flushing, AdoptPenTag: flushing}, nil
	case destinationTag == "" && ctx.DestinationHeadCount > 0 && residentsAllFlushing(ctx.DestinationResidentStages):
		// An unconfigured pen whose residents are all flushing animals is a flushing pen in fact;
		// she joins them, and the pen's missing configuration is adopted while we are here.
		return ShiftTypeDecision{TargetStage: flushing, AdoptPenTag: flushing}, nil
	default:
		return ShiftTypeDecision{}, refuse("flushing_destination_mismatch", shiftCopyFlushingDestinationMismatch)
	}
}

func residentsAllFlushing(stages []string) bool {
	if len(stages) == 0 {
		return false
	}
	for _, stage := range stages {
		if !strings.EqualFold(strings.TrimSpace(stage), FlushingShiftStage) {
			return false
		}
	}
	return true
}

// movingGroupSharedStage returns the single stage every moving animal carries, or a refusal when
// the group is mixed or unstamped -- the cases where "the group's tag" is not a real fact.
func movingGroupSharedStage(ctx ShiftTypeContext) (string, *ShiftTypeRefusal) {
	shared := ""
	for _, animal := range ctx.Animals {
		stage := strings.TrimSpace(animal.Stage)
		if stage == "" {
			return "", refuse("group_stage_unknown", shiftCopyGroupStageUnknown)
		}
		if shared == "" {
			shared = stage
			continue
		}
		if !strings.EqualFold(shared, stage) {
			return "", refuse("group_stage_mixed", shiftCopyGroupStageMixed)
		}
	}
	if shared == "" {
		return "", refuse("group_stage_unknown", shiftCopyGroupStageUnknown)
	}
	if canonical := canonicalWritableStage(shared, ctx.WritableStages); canonical != "" {
		return canonical, nil
	}
	return shared, nil
}

func growthEdgeExists(from, to string) bool {
	for stage, nexts := range growthForwardEdges {
		if !strings.EqualFold(stage, strings.TrimSpace(from)) {
			continue
		}
		for _, next := range nexts {
			if strings.EqualFold(next, strings.TrimSpace(to)) {
				return true
			}
		}
	}
	return false
}

func growthStageSexFor(stage string) string {
	for s, sex := range growthStageSex {
		if strings.EqualFold(s, strings.TrimSpace(stage)) {
			return sex
		}
	}
	return ""
}

// canonicalWritableStage returns the vocabulary's canonical casing for stage, or "" when the
// tenant's active vocabulary does not carry it.
func canonicalWritableStage(stage string, writable []string) string {
	stage = strings.TrimSpace(stage)
	if stage == "" {
		return ""
	}
	for _, w := range writable {
		if strings.EqualFold(strings.TrimSpace(w), stage) {
			return strings.TrimSpace(w)
		}
	}
	return ""
}

// Farm-worded refusal copy (backend-owned; rendered verbatim -- golden frontend rule). Written in
// the same voice as the StageReason* constants: what the farm needs to do, no internal vocabulary.
const (
	shiftCopyDestinationUnknown = "This destination is not in the current shed list. Refresh and pick it again"
	shiftCopyGrowthStageUnknown = "An animal in this group has no tag yet, so its next stage cannot be checked"
	shiftCopyGrowthNotNext      = "This destination's tag is not the next stage for every animal in the group"
	shiftCopyGrowthSexMismatch  = "This destination's tag does not match the sex of every animal in the group"

	shiftCopySpacingSourceUnknown       = "These animals do not share one current pen, so this cannot be a spacing move"
	shiftCopySpacingPartialGroup        = "Spacing moves the whole pen together. Select every animal in the pen"
	shiftCopySpacingDestinationMismatch = "This destination carries a different tag. Pick a pen with the same tag, or an empty pen"
	shiftCopySpacingDestinationOccupied = "This destination has no tag set but is not empty. Pick a pen with the same tag, or an empty pen"

	shiftCopyFlushingFemaleOnly          = "Only female animals can move onto flushing"
	shiftCopyFlushingDestinationMismatch = "Flushing needs an empty pen or a pen already on flushing"

	shiftCopyGroupStageUnknown = "An animal in this group has no tag, so the group's tag cannot be carried"
	shiftCopyGroupStageMixed   = "These animals carry different tags, so one tag cannot be carried for the group"
)
