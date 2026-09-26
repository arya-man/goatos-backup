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
//	normal    -> tag never changes; any selection; destination empty or already holding that tag
//
// Three shapes underneath the six types: PROGRESSION (growth, flushing -- the animal changed, take
// the destination tag), TEMPORARY RESIDENCE (breeding, delivery, health -- a visitor; health is the
// exception because being in ICU IS a change), and CAPACITY (spacing -- nothing changed, the tag
// travels and the PEN adapts). NORMAL (maintainer decision 2026-09-12) is the seventh type and the
// plainest: a move with no reason beyond "put these animals there". It stamps nothing, configures
// no pen, and asks only that the destination be empty or already hold an animal carrying the same
// tag -- the case with no legal type before it existed was Yashoda 3 (fattening males) into
// Yashoda 9, a mixed pen authored F2-Female that in fact held fattening males too.
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
	ShiftTypeNormal   = "normal"
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
	"K0":      {"K1"},
	"K1":      {"K2"},
	"K2":      {"K3"},
	"K3":      {"F2", "F2-Male", "F2-Female"},
	"F2":      {"F2-Male", "F2-Female"},
	"F2-Male": {"Buck"},
	// F2-Female -> Mother (maintainer decision 2026-09-26): a fattening female that kids joins the
	// mothers by a growth shift, and takes Mother's adult band in the same write. Non-Pregnant stays
	// FIRST: into an empty pen whose Stage is not Mother it is still the rung she takes.
	"F2-Female":    {"Non-Pregnant", "Mother"},
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
	"Mother":       "female",
	"Non-Pregnant": "female",
	"Pregnant":     "female",
}

// ShiftTypeLabel is the farm word for a shift type -- the same words the phone's raise form offers
// (Growth, Health, Breeding, Delivery, Spacing, Flushing, Normal). An unknown key returns "" so a
// raw category code never reaches a screen.
func ShiftTypeLabel(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case ShiftTypeHealth:
		return "Health"
	case ShiftTypeGrowth:
		return "Growth"
	case ShiftTypeBreeding:
		return "Breeding"
	case ShiftTypeDelivery:
		return "Delivery"
	case ShiftTypeSpacing:
		return "Spacing"
	case ShiftTypeFlushing:
		return "Flushing"
	case ShiftTypeNormal:
		return "Normal"
	}
	return ""
}

// ShiftTypeMoveLabel is ShiftTypeLabel as a clause on an approval line: "Normal move".
func ShiftTypeMoveLabel(category string) string {
	if label := ShiftTypeLabel(category); label != "" {
		return label + " move"
	}
	return ""
}

// FlushingShiftStage is the tag a flushing movement stamps; same constant the stage-resolution
// rules already carry (FlushingStageName) -- aliased here so this file reads self-contained.
const FlushingShiftStage = FlushingStageName

// KnownShiftType reports whether s is one of the seven typed-raise categories.
func KnownShiftType(s string) bool {
	switch s {
	case ShiftTypeHealth, ShiftTypeGrowth, ShiftTypeBreeding, ShiftTypeDelivery, ShiftTypeSpacing, ShiftTypeFlushing, ShiftTypeNormal:
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
	case ShiftTypeNormal:
		return resolveNormalShift(ctx)
	}
	return ShiftTypeDecision{}, refuse("invalid_category",
		"category must be growth, health, breeding, delivery, spacing, flushing, or normal")
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
// edge from its CURRENT stage to the tag it is stamped with, and a sexed tag refuses animals of the
// other or unknown sex. Backward or sideways is refused outright -- "forward only" only means
// something if the system refuses.
//
// WHICH tag (maintainer decision 2026-09-23): a live resident already carrying the group's next
// stage decides it FIRST, ahead of the pen's set stage. The farm moves a K2 into the pen where the
// K3s are; that pen may also hold a sick-bay animal staged by a pen name such as ICU-Kid (which
// read "holds a mix of tags" on 2026-09-23), or still be set to a stage its residents have moved
// past. Clinical residents never count. Only when no resident answers does the pen's set stage (or the residents'
// single shared stage) decide, exactly as before.
func resolveGrowthShift(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	if !ctx.DestinationKnown {
		return ShiftTypeDecision{}, refuse("destination_not_in_catalog", shiftCopyDestinationUnknown)
	}
	for _, animal := range ctx.Animals {
		if strings.TrimSpace(animal.Stage) == "" {
			return ShiftTypeDecision{}, refuse("growth_stage_unknown", shiftCopyGrowthStageUnknown)
		}
	}

	// An EMPTY pen never stops a growth move (maintainer decision 2026-09-23): each animal takes its
	// own next stage directly and the pen adopts it. A leftover Stage cell on an empty pen is not a
	// fact about any animal, so it does not decide anything here.
	if ctx.DestinationHeadCount == 0 && len(ctx.DestinationResidentStages) == 0 {
		return resolveGrowthIntoEmptyPen(ctx)
	}

	resident := resolveGrowthFromResidents(ctx)
	if resident.target != "" {
		return ShiftTypeDecision{TargetStage: resident.target}, nil
	}

	// No single resident answer: the pen's set stage (else the residents' one shared stage).
	decision, refusal := resolveGrowthFromPenTag(ctx)
	if refusal == nil {
		return decision, nil
	}
	switch {
	case resident.split:
		return ShiftTypeDecision{}, refuse("growth_group_needs_split", shiftCopyGrowthGroupNeedsSplit)
	case resident.tied:
		return ShiftTypeDecision{}, refuse("growth_next_stage_ambiguous", shiftCopyGrowthNextStageAmbiguous)
	}
	return ShiftTypeDecision{}, refusal
}

// resolveGrowthIntoEmptyPen stamps every animal with its next rung and has the pen adopt it. Where
// the ladder splits by sex (K3, F2) the animal's own sex picks the rung (maintainer answer
// 2026-09-23: K3 goes straight to F2-Male / F2-Female), so an unrecorded sex is refused rather than
// guessed. A raise stamps ONE tag, so the whole group must land on the same rung.
func resolveGrowthIntoEmptyPen(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	target := ""
	for _, animal := range ctx.Animals {
		next, refusal := growthNextStageForEmptyPen(animal, ctx.WritableStages, ctx.DestinationConfiguredStage)
		if refusal != nil {
			return ShiftTypeDecision{}, refusal
		}
		if target == "" {
			target = next
			continue
		}
		if !strings.EqualFold(target, next) {
			return ShiftTypeDecision{}, refuse("growth_group_needs_split", shiftCopyGrowthGroupNeedsSplit)
		}
	}
	if target == "" {
		return ShiftTypeDecision{}, refuse("growth_stage_unknown", shiftCopyGrowthStageUnknown)
	}
	return ShiftTypeDecision{TargetStage: target, AdoptPenTag: target}, nil
}

// growthNextStageForEmptyPen answers one animal's single next rung on the authored ladder. Where
// one sex has two rungs (F2-Female -> Non-Pregnant or Mother), the empty pen's Stage decides when
// it names one of them, and otherwise the ladder's FIRST rung is taken -- so a move into an empty
// pen behaves exactly as it did before the second rung existed.
func growthNextStageForEmptyPen(animal ShiftTypeAnimal, writable []string, penStage string) (string, *ShiftTypeRefusal) {
	var edges []string
	for stage, nexts := range growthForwardEdges {
		if strings.EqualFold(stage, strings.TrimSpace(animal.Stage)) {
			edges = nexts
			break
		}
	}
	if len(edges) == 0 {
		return "", refuse("growth_no_next_stage", shiftCopyGrowthNoNextStage)
	}
	var sexed []string
	for _, next := range edges {
		if growthStageSexFor(next) != "" {
			sexed = append(sexed, next)
		}
	}
	var chosen []string
	if len(sexed) > 0 {
		sex := strings.TrimSpace(animal.Sex)
		if sex == "" {
			return "", refuse("growth_sex_unknown", shiftCopyGrowthSexUnknown)
		}
		for _, next := range sexed {
			if strings.EqualFold(growthStageSexFor(next), sex) {
				chosen = append(chosen, next)
			}
		}
		if len(chosen) == 0 {
			return "", refuse("growth_sex_mismatch", shiftCopyGrowthSexMismatch)
		}
	} else {
		chosen = edges
	}
	if len(chosen) > 1 && len(sexed) > 0 {
		pick := chosen[0]
		for _, next := range chosen {
			if strings.EqualFold(next, strings.TrimSpace(penStage)) {
				pick = next
			}
		}
		chosen = []string{pick}
	}
	if len(chosen) != 1 {
		return "", refuse("growth_next_stage_ambiguous", shiftCopyGrowthNextStageAmbiguous)
	}
	canonical := canonicalWritableStage(chosen[0], writable)
	if canonical == "" || protocoldomain.IsClinicalManagementStage(canonical) {
		return "", refuse("destination_tag_not_applicable", StageReasonNotApplicable)
	}
	return canonical, nil
}

// growthResidentOutcome is the resident rule's answer. target is set when exactly one next stage
// fits every animal. split: every animal has a resident next stage but no single one fits them all.
// tied: more than one fits them all and the pen's set stage is not among them.
type growthResidentOutcome struct {
	target string
	split  bool
	tied   bool
}

// resolveGrowthFromResidents finds, per animal, the next stages a live non-clinical resident
// already carries (sex-compatible with that animal), and intersects them across the group, since a
// raise stamps ONE tag. An animal with no such stage leaves the whole group to the pen-tag rule.
func resolveGrowthFromResidents(ctx ShiftTypeContext) growthResidentOutcome {
	residents := make([]string, 0, len(ctx.DestinationResidentStages))
	for _, stage := range ctx.DestinationResidentStages {
		stage = strings.TrimSpace(stage)
		if stage == "" || protocoldomain.IsClinicalManagementStage(stage) {
			continue
		}
		if canonical := canonicalWritableStage(stage, ctx.WritableStages); canonical != "" {
			residents = append(residents, canonical)
		}
	}
	if len(residents) == 0 || len(ctx.Animals) == 0 {
		return growthResidentOutcome{}
	}
	var common []string
	for i, animal := range ctx.Animals {
		var fits []string
		for _, stage := range residents {
			if !growthEdgeExists(animal.Stage, stage) {
				continue
			}
			if sex := growthStageSexFor(stage); sex != "" && !strings.EqualFold(strings.TrimSpace(animal.Sex), sex) {
				continue
			}
			if !stageListContains(fits, stage) {
				fits = append(fits, stage)
			}
		}
		if len(fits) == 0 {
			return growthResidentOutcome{}
		}
		if i == 0 {
			common = fits
			continue
		}
		kept := common[:0:0]
		for _, stage := range common {
			if stageListContains(fits, stage) {
				kept = append(kept, stage)
			}
		}
		common = kept
	}
	switch len(common) {
	case 0:
		return growthResidentOutcome{split: true}
	case 1:
		return growthResidentOutcome{target: common[0]}
	}
	if configured := strings.TrimSpace(ctx.DestinationConfiguredStage); configured != "" && stageListContains(common, configured) {
		return growthResidentOutcome{target: canonicalWritableStage(configured, ctx.WritableStages)}
	}
	return growthResidentOutcome{tied: true}
}

// resolveGrowthFromPenTag is the pre-2026-09-23 growth rule, unchanged: the pen's set stage (else
// the residents' single shared stage) must be the next stage for every animal, sex permitting.
func resolveGrowthFromPenTag(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	tag, refusal := destinationEffectiveTag(ctx, false)
	if refusal != nil {
		return ShiftTypeDecision{}, refusal
	}
	requiredSex := growthStageSexFor(tag)
	for _, animal := range ctx.Animals {
		if !growthEdgeExists(strings.TrimSpace(animal.Stage), tag) {
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

// resolveNormalShift: the plain move (maintainer decision 2026-09-12). Nothing is stamped and no
// pen is configured; every animal keeps its own tag. Two destinations are acceptable and nothing
// else is:
//
//   - an EMPTY pen accepts any selection, tagged or not;
//   - an OCCUPIED pen accepts the selection only when, for every tag the selection carries, at
//     least one live resident already carries that same tag. The check reads the RESIDENTS, never
//     the pen's authored tag -- a pen authored F2-Female that holds fattening males is, in fact, a
//     pen fattening males may join, while a pen holding only fattening females is not.
//
// Unlike spacing there is no whole-pen rule and no source requirement: any subset of any pen moves.
func resolveNormalShift(ctx ShiftTypeContext) (ShiftTypeDecision, *ShiftTypeRefusal) {
	if !ctx.DestinationKnown {
		return ShiftTypeDecision{}, refuse("destination_not_in_catalog", shiftCopyDestinationUnknown)
	}
	if ctx.DestinationHeadCount == 0 {
		return ShiftTypeDecision{}, nil
	}
	for _, animal := range ctx.Animals {
		stage := strings.TrimSpace(animal.Stage)
		if stage == "" {
			return ShiftTypeDecision{}, refuse("group_stage_unknown", shiftCopyGroupStageUnknown)
		}
		if !stageListContains(ctx.DestinationResidentStages, stage) {
			return ShiftTypeDecision{}, refuse("normal_destination_tag_mismatch", shiftCopyNormalDestinationMismatch)
		}
	}
	return ShiftTypeDecision{}, nil
}

func stageListContains(stages []string, stage string) bool {
	for _, s := range stages {
		if strings.EqualFold(strings.TrimSpace(s), stage) {
			return true
		}
	}
	return false
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
	shiftCopyDestinationUnknown = "This destination is not in the current pen list. Refresh and pick it again"
	shiftCopyGrowthStageUnknown = "An animal in this group has no tag yet, so its next stage cannot be checked"
	shiftCopyGrowthNotNext      = "This destination's tag is not the next stage for every animal in the group"
	shiftCopyGrowthSexMismatch  = "This destination's tag does not match the sex of every animal in the group"

	shiftCopyGrowthNoNextStage        = "An animal in this group has no next growth stage, so it cannot move as growth"
	shiftCopyGrowthSexUnknown         = "An animal in this group has no sex recorded, so its next stage cannot be chosen"
	shiftCopyGrowthGroupNeedsSplit    = "These animals need different next stages. Move each stage in its own shifting"
	shiftCopyGrowthNextStageAmbiguous = "This pen holds more than one next stage for these animals. Set the pen's stage on Counts Breakdown, or pick another pen"

	shiftCopySpacingSourceUnknown       = "These animals do not share one current pen, so this cannot be a spacing move"
	shiftCopySpacingPartialGroup        = "Spacing moves the whole pen together. Select every animal in the pen"
	shiftCopySpacingDestinationMismatch = "This destination carries a different tag. Pick a pen with the same tag, or an empty pen"
	shiftCopySpacingDestinationOccupied = "This destination has no tag set but is not empty. Pick a pen with the same tag, or an empty pen"

	shiftCopyFlushingFemaleOnly          = "Only female animals can move onto flushing"
	shiftCopyFlushingDestinationMismatch = "Flushing needs an empty pen or a pen already on flushing"

	shiftCopyNormalDestinationMismatch = "This pen holds no animal with the same tag as the ones you are moving. Pick an empty pen, or a pen already holding this tag"

	shiftCopyGroupStageUnknown = "An animal in this group has no tag, so the group's tag cannot be carried"
	shiftCopyGroupStageMixed   = "These animals carry different tags, so one tag cannot be carried for the group"
)

// ProductNamedStageCodes is every stage code this package names literally: the newborn stage
// births are recorded at, the flushing cohort, and every rung of the growth ladder. The
// Configuration register protects these rows from archive and delete (a farm may still rename
// them), because removing one silently breaks births, flushing or a growth step. Pinned by
// configuration/domain's TestProtectedStageCodesCoverEveryStageTheProductNames.
func ProductNamedStageCodes() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(code string) {
		key := strings.ToLower(strings.TrimSpace(code))
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, code)
	}
	add(NewbornStageCode)
	add(FlushingStageName)
	for stage, nexts := range growthForwardEdges {
		add(stage)
		for _, next := range nexts {
			add(next)
		}
	}
	for stage := range growthStageSex {
		add(stage)
	}
	return out
}
