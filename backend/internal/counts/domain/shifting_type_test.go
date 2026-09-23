package domain

import (
	"strings"
	"testing"
)

// The typed-raise rulebook (docs/features/shifting/shifting-rewrite-tag-rules.md). Each test names
// the rule it pins; several were mutation-tested while writing (see comments) so a future "cleanup"
// that weakens a branch turns a test red instead of silently widening a refusal.

var testWritable = []string{
	"K0", "K1", "K2", "K3", "F2", "F2-Male", "F2-Female", "Buck",
	"Mother", "Milking", "M0", "Warmup", "Pregnant", "Non-Pregnant",
	"ICU", "Quarantine", "ICU-Kid", "Quarantine kids", "Flushing",
}

func knownDest(ctx ShiftTypeContext) ShiftTypeContext {
	ctx.DestinationKnown = true
	if ctx.WritableStages == nil {
		ctx.WritableStages = testWritable
	}
	return ctx
}

func animals(stageSex ...string) []ShiftTypeAnimal {
	out := make([]ShiftTypeAnimal, 0, len(stageSex)/2)
	for i := 0; i+1 < len(stageSex); i += 2 {
		out = append(out, ShiftTypeAnimal{GoatID: "g", Stage: stageSex[i], Sex: stageSex[i+1]})
	}
	return out
}

func wantDecision(t *testing.T, ctx ShiftTypeContext, want ShiftTypeDecision) {
	t.Helper()
	got, refusal := ResolveShiftTypeDecision(ctx)
	if refusal != nil {
		t.Fatalf("unexpected refusal %s (%s)", refusal.Code, refusal.Message)
	}
	if got != want {
		t.Fatalf("decision = %+v, want %+v", got, want)
	}
}

func wantRefusal(t *testing.T, ctx ShiftTypeContext, code string) *ShiftTypeRefusal {
	t.Helper()
	_, refusal := ResolveShiftTypeDecision(ctx)
	if refusal == nil {
		t.Fatalf("expected refusal %s, got a decision", code)
	}
	if refusal.Code != code {
		t.Fatalf("refusal code = %s (%s), want %s", refusal.Code, refusal.Message, code)
	}
	if strings.TrimSpace(refusal.Message) == "" {
		t.Fatalf("refusal %s has no farm copy", code)
	}
	return refusal
}

// --- health -------------------------------------------------------------------------------------

// A health shift into a clinical pen stamps the clinical state -- the ONE type allowed to -- and
// flags the decision so the relocation path lifts its clinical refusal for exactly this movement.
func TestHealthShiftStampsClinicalDestinationTag(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeHealth,
		DestinationConfiguredStage: "ICU",
		Animals:                    animals("K1", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "ICU", AllowClinicalTarget: true})
}

// The clinical PEN tags (ICU-Kid) are not clinical STATES; they resolve without the clinical flag.
func TestHealthShiftIntoClinicalKidPenIsNotAClinicalState(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeHealth,
		DestinationConfiguredStage: "ICU-Kid",
		Animals:                    animals("K1", "male"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "ICU-Kid"})
}

// The return leg is just another health shift: a K1 animal returning to a K3 pen becomes K3, with
// no memory of the pre-ICU tag and no forward-only check (maintainer confirmed 2026-08-20).
func TestHealthReturnLegTakesTheReturnPensTagOutright(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeHealth,
		DestinationConfiguredStage: "K3",
		Animals:                    animals("ICU", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "K3"})
}

// A health shift into a pen nobody tagged and nothing lives in is refused with the farm copy --
// the type's whole meaning is "take the destination tag", so a tagless destination has no answer.
func TestHealthShiftRefusesAnUntaggedEmptyDestination(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:    ShiftTypeHealth,
		Animals: animals("K1", "female"),
	})
	refusal := wantRefusal(t, ctx, "destination_tag_missing")
	if refusal.Message != StageReasonNoTag {
		t.Fatalf("copy = %q, want the shared StageReasonNoTag", refusal.Message)
	}
}

// Residents still answer for an unconfigured pen (the legacy fallback): a health return into an
// untagged pen whose shed holds one cohort adopts that cohort.
func TestHealthShiftFallsBackToResidentCohort(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeHealth,
		DestinationResidentStages: []string{"K2"},
		DestinationHeadCount:      4,
		Animals:                   animals("ICU", "male"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "K2"})
}

// --- growth -------------------------------------------------------------------------------------

func TestGrowthShiftAdvancesOneRung(t *testing.T) {
	for _, step := range [][2]string{{"K0", "K1"}, {"K1", "K2"}, {"K2", "K3"}, {"K3", "F2"}} {
		ctx := knownDest(ShiftTypeContext{
			Type:                       ShiftTypeGrowth,
			DestinationConfiguredStage: step[1],
			Animals:                    animals(step[0], "male", step[0], "female"),
		})
		wantDecision(t, ctx, ShiftTypeDecision{TargetStage: step[1]})
	}
}

// Backward is refused -- there is no backward growth movement. Mutation-tested: removing the edge
// check makes this pass a K3 group into a K1 pen.
func TestGrowthShiftRefusesBackwardMove(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "K1",
		Animals:                    animals("K3", "male"),
	})
	wantRefusal(t, ctx, "growth_not_next_stage")
}

func TestGrowthShiftRefusesSkippingARung(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "K3",
		Animals:                    animals("K1", "female"),
	})
	wantRefusal(t, ctx, "growth_not_next_stage")
}

// The ladder branches by sex: F2 -> F2-Male takes males only. One female in the group refuses the
// whole raise -- each sexed destination gets its own raise carrying only matching animals.
func TestGrowthShiftSexedDestinationRefusesOtherSex(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "F2-Male",
		Animals:                    animals("F2", "male", "F2", "female"),
	})
	wantRefusal(t, ctx, "growth_sex_mismatch")
}

func TestGrowthShiftSexedDestinationRefusesUnknownSex(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "Buck",
		Animals:                    animals("F2-Male", ""),
	})
	wantRefusal(t, ctx, "growth_sex_mismatch")
}

// K3 may split by sex directly (skipping plain F2) OR pass through F2 first; both are authored
// edges, because the farm does both.
func TestGrowthShiftK3MaySplitBySexDirectly(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "F2-Female",
		Animals:                    animals("K3", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "F2-Female"})
}

func TestGrowthShiftFatteningMaleBecomesBuck(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "Buck",
		Animals:                    animals("F2-Male", "male"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "Buck"})
}

// The ONE reverse edge: Pregnant -> Non-Pregnant. Its mirror (Non-Pregnant -> Pregnant) is an
// ordinary forward edge. Both directions resolve; no other stage pair does this.
func TestGrowthShiftPregnancyIsTheOnlyTwoWayEdge(t *testing.T) {
	forward := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "Pregnant",
		Animals:                    animals("Non-Pregnant", "female"),
	})
	wantDecision(t, forward, ShiftTypeDecision{TargetStage: "Pregnant"})
	back := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "Non-Pregnant",
		Animals:                    animals("Pregnant", "female"),
	})
	wantDecision(t, back, ShiftTypeDecision{TargetStage: "Non-Pregnant"})
}

// Stages with no authored edges (Mother, Milking, M0, Warmup) refuse growth raises in BOTH
// directions until the maintainer places them on the ladder -- recorded open decision, not an
// oversight. Mutation-tested: adding a Mother edge turns this red.
func TestGrowthShiftRefusesStagesOffTheLadder(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "Milking",
		Animals:                    animals("Mother", "female"),
	})
	wantRefusal(t, ctx, "growth_not_next_stage")
}

// An animal with no recorded stage cannot be checked forward and refuses the raise with copy that
// names the herd-data gap, not the raiser.
func TestGrowthShiftRefusesAnimalWithNoStage(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "K1",
		Animals:                    animals("", "male"),
	})
	wantRefusal(t, ctx, "growth_stage_unknown")
}

// A growth shift must never stamp a clinical state, whatever the pen says.
func TestGrowthShiftRefusesClinicalDestination(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "ICU",
		Animals:                    animals("K1", "male"),
	})
	refusal := wantRefusal(t, ctx, "destination_tag_not_applicable")
	if refusal.Message != StageReasonNotApplicable {
		t.Fatalf("copy = %q, want the shared health-team reason", refusal.Message)
	}
}

// Case-insensitive matching returns the vocabulary's canonical casing, so the snapshot matches
// what the relocation looks up at the second gate.
func TestGrowthShiftCanonicalizesCasing(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "non-pregnant",
		Animals:                    animals("f2-female", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "Non-Pregnant"})
}

// --- growth: a resident carrying the next stage (maintainer decision 2026-09-23) -------------

// The case that refused on the farm on 2026-09-23: a K2 animal into a pen with no stage set whose
// residents are one K3 and one ICU animal. It used to refuse "holds a mix of tags". A live resident
// already carries K2's next stage, so the move is allowed and the animal becomes K3; the ICU
// resident is a clinical state and never counts as a match.
func TestGrowthShiftJoinsAResidentCarryingTheNextStage(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeGrowth,
		DestinationResidentStages: []string{"ICU", "K3"},
		DestinationHeadCount:      2,
		Animals:                   animals("K2", "male"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "K3"})
}

// The resident rule wins over the pen's set stage (maintainer answer 2026-09-23): Castro 2 is set
// to F2 on Counts Breakdown but a K3 still stands in it, so a K2 moving in becomes K3.
func TestGrowthShiftResidentNextStageWinsOverThePensSetStage(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "F2",
		DestinationResidentStages:  []string{"F2", "K3"},
		DestinationHeadCount:       9,
		Animals:                    animals("K2", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "K3"})
}

// A group whose animals need DIFFERENT next stages cannot share the one tag a raise stamps; it is
// refused with copy telling the operator to split it, not with the generic mixed-pen reason.
func TestGrowthShiftGroupNeedingTwoNextStagesMustBeSplit(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeGrowth,
		DestinationResidentStages: []string{"K2", "K3"},
		DestinationHeadCount:      6,
		Animals:                   animals("K1", "male", "K2", "male"),
	})
	wantRefusal(t, ctx, "growth_group_needs_split")
}

// The sex rule still holds on the resident path: a female K3 cannot become F2-Male because an
// F2-Male lives there.
func TestGrowthShiftResidentMatchStillHonoursSex(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeGrowth,
		DestinationResidentStages: []string{"F2-Male", "ICU"},
		DestinationHeadCount:      3,
		Animals:                   animals("K3", "female"),
	})
	wantRefusal(t, ctx, "destination_tag_mixed")
}

// A clinical resident alone is never a match: a K2 into a pen holding only an ICU animal still
// refuses with the health-team reason, exactly as before.
func TestGrowthShiftClinicalResidentIsNeverAMatch(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeGrowth,
		DestinationResidentStages: []string{"ICU"},
		DestinationHeadCount:      1,
		Animals:                   animals("K2", "male"),
	})
	wantRefusal(t, ctx, "destination_tag_not_applicable")
}

// No resident carries the next stage: the old refusal stands (a K2 into a pen of K1 animals).
func TestGrowthShiftNoResidentNextStageKeepsTheOldRefusal(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeGrowth,
		DestinationResidentStages: []string{"K1"},
		DestinationHeadCount:      5,
		Animals:                   animals("K2", "male"),
	})
	wantRefusal(t, ctx, "growth_not_next_stage")
}

// Two next stages present for the same animals (a male K3 into a pen holding both F2 and F2-Male)
// is an honest tie; the pen's set stage breaks it when it is one of them.
func TestGrowthShiftPensSetStageBreaksAResidentTie(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeGrowth,
		DestinationConfiguredStage: "F2-Male",
		DestinationResidentStages:  []string{"F2", "F2-Male"},
		DestinationHeadCount:       8,
		Animals:                    animals("K3", "male"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "F2-Male"})
}

// With no set stage to break the tie, the raise is refused rather than guessing which rung.
func TestGrowthShiftUnbrokenResidentTieRefuses(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeGrowth,
		DestinationResidentStages: []string{"F2", "F2-Male"},
		DestinationHeadCount:      8,
		Animals:                   animals("K3", "male"),
	})
	wantRefusal(t, ctx, "growth_next_stage_ambiguous")
}

// --- breeding -----------------------------------------------------------------------------------

// A breeding jump changes nothing: no target, no adoption, any destination -- even one carrying a
// different tag, because nothing is stamped.
func TestBreedingShiftNeverChangesTheTag(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeBreeding,
		DestinationConfiguredStage: "Non-Pregnant",
		DestinationHeadCount:       12,
		Animals:                    animals("Buck", "male"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{})
}

// Even an unknown destination does not refuse a breeding jump at the rulebook layer: nothing is
// stamped, and the relocation's own ground-truth check still guards a genuinely bad shed id.
func TestBreedingShiftDoesNotNeedDestinationFacts(t *testing.T) {
	ctx := ShiftTypeContext{Type: ShiftTypeBreeding, Animals: animals("Buck", "male"), WritableStages: testWritable}
	wantDecision(t, ctx, ShiftTypeDecision{})
}

// --- delivery -----------------------------------------------------------------------------------

// Into the kidding pen (destination tagged K0): the mother keeps her own tag. The K0 tag belongs
// to the kids. Mutation-tested: removing the newborn-stage branch stamps her K0.
func TestDeliveryShiftIntoKiddingPenKeepsHerTag(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeDelivery,
		DestinationConfiguredStage: "K0",
		DestinationHeadCount:       9,
		Animals:                    animals("Pregnant", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{})
}

// Out of the kidding pen to a tagged pen: she takes that pen's tag.
func TestDeliveryShiftOutboundTakesDestinationTag(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeDelivery,
		DestinationConfiguredStage: "Milking",
		Animals:                    animals("Pregnant", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "Milking"})
}

// The one-day recovery shed: mother (and kid) into an EMPTY untagged shed. She keeps her tag and
// the shed adopts it -- the mother's tag only (maintainer decision 2026-08-20).
func TestDeliveryShiftIntoEmptyShedAdoptsTheMothersTag(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:    ShiftTypeDelivery,
		Animals: animals("Pregnant", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{AdoptPenTag: "Pregnant"})
}

// A delivery must not stamp a clinical state either -- only health may.
func TestDeliveryShiftRefusesClinicalDestination(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeDelivery,
		DestinationConfiguredStage: "Quarantine",
		Animals:                    animals("Pregnant", "female"),
	})
	wantRefusal(t, ctx, "destination_tag_not_applicable")
}

// --- spacing ------------------------------------------------------------------------------------

func spacingCtx() ShiftTypeContext {
	return knownDest(ShiftTypeContext{
		Type:                  ShiftTypeSpacing,
		SourceKnown:           true,
		SourceConfiguredStage: "K2",
		SourceHeadCount:       3,
		Animals:               animals("K2", "male", "K2", "female", "K2", "male"),
	})
}

// Same-tag destination: the whole pen moves, everyone keeps their tag, nothing is configured.
func TestSpacingShiftIntoMatchingPen(t *testing.T) {
	ctx := spacingCtx()
	ctx.DestinationConfiguredStage = "K2"
	ctx.DestinationHeadCount = 7
	wantDecision(t, ctx, ShiftTypeDecision{})
}

// Empty destination: allowed, and the pen ADOPTS the source tag -- this is how an untagged pen
// gets its tag under "pen tags follow occupancy".
func TestSpacingShiftIntoEmptyPenAdoptsSourceTag(t *testing.T) {
	ctx := spacingCtx()
	wantDecision(t, ctx, ShiftTypeDecision{AdoptPenTag: "K2"})
}

// A differently-tagged destination is refused at raise -- before approval, before any video.
func TestSpacingShiftRefusesMismatchedDestination(t *testing.T) {
	ctx := spacingCtx()
	ctx.DestinationConfiguredStage = "K3"
	wantRefusal(t, ctx, "spacing_destination_mismatch")
}

// An untagged but OCCUPIED destination is refused: adopting onto it would stamp a pen whose
// residents were never part of the decision.
func TestSpacingShiftRefusesUntaggedOccupiedDestination(t *testing.T) {
	ctx := spacingCtx()
	ctx.DestinationHeadCount = 2
	wantRefusal(t, ctx, "spacing_destination_occupied")
}

// "Half-half is not an option": the group must equal the source pen's live population. Two of
// three animals is a partial move and is refused. Mutation-tested: dropping the head-count
// comparison lets a partial group through.
func TestSpacingShiftRefusesPartialGroup(t *testing.T) {
	ctx := spacingCtx()
	ctx.Animals = ctx.Animals[:2]
	wantRefusal(t, ctx, "spacing_partial_group")
}

// A group that cannot name one source pen is not a spacing move.
func TestSpacingShiftRefusesUnresolvedSource(t *testing.T) {
	ctx := spacingCtx()
	ctx.SourceKnown = false
	wantRefusal(t, ctx, "spacing_source_unresolved")
}

// Untagged source pen: the animals' own shared stage is the carried tag.
func TestSpacingShiftUntaggedSourceUsesTheGroupsSharedStage(t *testing.T) {
	ctx := spacingCtx()
	ctx.SourceConfiguredStage = ""
	wantDecision(t, ctx, ShiftTypeDecision{AdoptPenTag: "K2"})
}

// Untagged source pen AND a mixed group: blocked. The approver-chooses-tag capability is a
// recorded follow-up; until it exists the raise refuses rather than guessing.
func TestSpacingShiftRefusesMixedGroupWhenSourceUntagged(t *testing.T) {
	ctx := spacingCtx()
	ctx.SourceConfiguredStage = ""
	ctx.Animals = animals("K2", "male", "K3", "female", "K2", "male")
	wantRefusal(t, ctx, "group_stage_mixed")
}

// --- normal -------------------------------------------------------------------------------------
//
// Normal shifting (maintainer decision 2026-09-12): a plain move that never touches the tag. Any
// selection of animals; the destination must be EMPTY or already hold at least one animal carrying
// the moving animals' tag. Fixture is the live case that had no legal type before this rule:
// Yashoda 3 (17 fattening males) into Yashoda 9, a mixed pen authored F2-Female that in fact
// holds fattening males too.

func normalCtx() ShiftTypeContext {
	return knownDest(ShiftTypeContext{
		Type:    ShiftTypeNormal,
		Animals: animals("F2-Male", "male", "F2-Male", "male"),
	})
}

// Yashoda 3 -> Yashoda 9: the pen's AUTHORED tag says F2-Female, but a resident carries F2-Male,
// so the move is allowed and nobody's tag changes. Mutation-tested: comparing against the authored
// tag instead of the residents turns this red.
func TestNormalShiftIntoPenHoldingTheSameTag(t *testing.T) {
	ctx := normalCtx()
	ctx.DestinationConfiguredStage = "F2-Female"
	ctx.DestinationResidentStages = []string{"F2-Female", "F2-Male", "K3"}
	ctx.DestinationHeadCount = 14
	wantDecision(t, ctx, ShiftTypeDecision{})
}

// An EMPTY pen accepts anything, and the pen is NOT re-tagged: normal shifting configures nothing.
func TestNormalShiftIntoEmptyPenKeepsEveryTag(t *testing.T) {
	ctx := normalCtx()
	ctx.Animals = animals("F2-Male", "male", "K3", "female", "", "female")
	wantDecision(t, ctx, ShiftTypeDecision{})
}

// A pen holding only fattening FEMALES must not receive fattening males under a normal move.
func TestNormalShiftRefusesPenWithoutTheTag(t *testing.T) {
	ctx := normalCtx()
	ctx.DestinationConfiguredStage = "F2-Female"
	ctx.DestinationResidentStages = []string{"F2-Female"}
	ctx.DestinationHeadCount = 3
	wantRefusal(t, ctx, "normal_destination_tag_mismatch")
}

// A mixed selection needs EVERY tag it carries present at the destination, not just one of them.
func TestNormalShiftMixedGroupNeedsEveryTagPresent(t *testing.T) {
	ctx := normalCtx()
	ctx.Animals = animals("F2-Male", "male", "K3", "female")
	ctx.DestinationResidentStages = []string{"F2-Male"}
	ctx.DestinationHeadCount = 5
	wantRefusal(t, ctx, "normal_destination_tag_mismatch")

	ctx.DestinationResidentStages = []string{"F2-Male", "K3"}
	wantDecision(t, ctx, ShiftTypeDecision{})
}

// An untagged animal cannot be matched against an occupied pen.
func TestNormalShiftRefusesUntaggedAnimalIntoOccupiedPen(t *testing.T) {
	ctx := normalCtx()
	ctx.Animals = animals("F2-Male", "male", "", "male")
	ctx.DestinationResidentStages = []string{"F2-Male"}
	ctx.DestinationHeadCount = 5
	wantRefusal(t, ctx, "group_stage_unknown")
}

// Never a partial-group refusal and never a source requirement: normal moves any selection.
func TestNormalShiftDoesNotRequireTheWholeSourcePen(t *testing.T) {
	ctx := normalCtx()
	ctx.SourceKnown = false
	ctx.DestinationResidentStages = []string{"F2-Male"}
	ctx.DestinationHeadCount = 9
	wantDecision(t, ctx, ShiftTypeDecision{})
}

func TestNormalShiftRefusesUnknownDestination(t *testing.T) {
	ctx := normalCtx()
	ctx.DestinationKnown = false
	wantRefusal(t, ctx, "destination_not_in_catalog")
}

// --- flushing -----------------------------------------------------------------------------------

func TestFlushingShiftIntoEmptyPenTagsItFlushing(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:    ShiftTypeFlushing,
		Animals: animals("Non-Pregnant", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "Flushing", AdoptPenTag: "Flushing"})
}

func TestFlushingShiftIntoFlushingPen(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeFlushing,
		DestinationConfiguredStage: "Flushing",
		DestinationHeadCount:       5,
		Animals:                    animals("Non-Pregnant", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "Flushing"})
}

// An unconfigured pen whose residents are all flushing IS a flushing pen in fact: she joins them
// and the pen's missing configuration is adopted.
func TestFlushingShiftAdoptsConfigOntoAFactualFlushingPen(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                      ShiftTypeFlushing,
		DestinationResidentStages: []string{"Flushing"},
		DestinationHeadCount:      4,
		Animals:                   animals("Non-Pregnant", "female"),
	})
	wantDecision(t, ctx, ShiftTypeDecision{TargetStage: "Flushing", AdoptPenTag: "Flushing"})
}

func TestFlushingShiftRefusesMales(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:    ShiftTypeFlushing,
		Animals: animals("Non-Pregnant", "female", "Buck", "male"),
	})
	wantRefusal(t, ctx, "flushing_requires_female")
}

func TestFlushingShiftRefusesUnknownSex(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:    ShiftTypeFlushing,
		Animals: animals("Non-Pregnant", ""),
	})
	wantRefusal(t, ctx, "flushing_requires_female")
}

func TestFlushingShiftRefusesDifferentlyTaggedPen(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:                       ShiftTypeFlushing,
		DestinationConfiguredStage: "K3",
		Animals:                    animals("Non-Pregnant", "female"),
	})
	wantRefusal(t, ctx, "flushing_destination_mismatch")
}

// A tenant whose vocabulary lacks an active Flushing stage refuses rather than stamping a tag the
// relocation would then reject at the second gate.
func TestFlushingShiftRefusesWhenVocabularyLacksFlushing(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{
		Type:           ShiftTypeFlushing,
		Animals:        animals("Non-Pregnant", "female"),
		WritableStages: []string{"K0", "K1", "Non-Pregnant"},
	})
	wantRefusal(t, ctx, "destination_tag_not_applicable")
}

// --- shared -------------------------------------------------------------------------------------

// Destination-fact-dependent types fail closed when the raise's pen is absent from the catalog.
func TestTypedShiftsRefuseAnUncataloguedDestination(t *testing.T) {
	for _, typ := range []string{ShiftTypeHealth, ShiftTypeGrowth, ShiftTypeDelivery, ShiftTypeSpacing, ShiftTypeFlushing} {
		ctx := ShiftTypeContext{Type: typ, Animals: animals("K2", "female"), WritableStages: testWritable, SourceKnown: true, SourceHeadCount: 1, SourceConfiguredStage: "K2"}
		wantRefusal(t, ctx, "destination_not_in_catalog")
	}
}

func TestUnknownTypeRefuses(t *testing.T) {
	ctx := knownDest(ShiftTypeContext{Type: "warmup", Animals: animals("K2", "male")})
	wantRefusal(t, ctx, "invalid_category")
}

func TestKnownShiftTypeVocabulary(t *testing.T) {
	for _, typ := range []string{"health", "growth", "breeding", "delivery", "spacing", "flushing", "normal"} {
		if !KnownShiftType(typ) {
			t.Fatalf("KnownShiftType(%q) = false", typ)
		}
	}
	for _, typ := range []string{"", "warmup", "sale", "GROWTH "} {
		if KnownShiftType(typ) {
			t.Fatalf("KnownShiftType(%q) = true", typ)
		}
	}
}

// Every refusal's copy obeys the firewall: no internal vocabulary in operator-facing strings.
func TestRefusalCopyCarriesNoInternalVocabulary(t *testing.T) {
	banned := []string{"payload", "API", "backend", "route", "Room", "outbox", "idempotency", "TODO", "debug", "mock", "fixture", "catalog id", "uuid"}
	copies := []string{
		shiftCopyDestinationUnknown, shiftCopyGrowthStageUnknown, shiftCopyGrowthNotNext,
		shiftCopyGrowthSexMismatch, shiftCopySpacingSourceUnknown, shiftCopySpacingPartialGroup,
		shiftCopySpacingDestinationMismatch, shiftCopySpacingDestinationOccupied,
		shiftCopyFlushingFemaleOnly, shiftCopyFlushingDestinationMismatch,
		shiftCopyGroupStageUnknown, shiftCopyGroupStageMixed, shiftCopyNormalDestinationMismatch,
	}
	for _, copyText := range copies {
		for _, word := range banned {
			if strings.Contains(strings.ToLower(copyText), strings.ToLower(word)) {
				t.Fatalf("refusal copy %q contains banned word %q", copyText, word)
			}
		}
	}
}
