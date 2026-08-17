package domain

import (
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// Resolving a GoatOS animal into the engine's Animal is safety-critical, and it
// is pure and tested for that reason.
//
// The engine gates whole diagnoses on sex and status. Get the mapping wrong and
// a rule does not misfire -- it silently STOPS firing, which is under-firing, and
// under-firing is the failure mode that kills. Concretely, in adult-1:
//
//	sex M      unlocks CALCULI (urinary obstruction)
//	sex F      unlocks MASTITIS, UDDER_EDEMA, METRITIS, PROLAPSE, MILK_FEVER
//	pregnant   is the ONLY way PREG_TOX can fire
//	periparturient is the ONLY way MILK_FEVER or METRITIS can fire, and the only
//	           way the calcium and uterine-prolapse emergencies can fire
//
// None of those failures is visible in the output. The animal simply comes back
// with fewer diagnoses, which reads as a healthier animal.

// PeriparturientWindowDays is the clinical definition: a doe is periparturient
// for 14 days after kidding. It is a business-DAY window in Asia/Kolkata, never
// an hour count.
const PeriparturientWindowDays = 14

// GoatFacts is what GoatOS knows about the animal. It is read from the database
// inside the diagnosis transaction and never accepted from a client: the engine's
// answer depends on these, so a caller able to assert them could steer the
// diagnosis far more easily than by mis-ticking the form.
type GoatFacts struct {
	Species         string
	Sex             string
	AgeBand         string
	ManagementStage string
	LifecycleStatus string

	// DaysSinceKidding is the whole days between this doe's most recent
	// recorded kidding and today, or nil when she has no recorded kidding.
	//
	// NIL IS NOT "not periparturient" -- it is "GoatOS cannot say". The
	// difference matters: goat_births starts empty by design and is only
	// written by the forward birth-capture flow, so an animal that kidded
	// before that flow existed has no row. See MissingKiddingHistory.
	DaysSinceKidding *int
}

// Status values the engine understands.
const (
	AnimalStatusNormal         = "normal"
	AnimalStatusPregnant       = "pregnant"
	AnimalStatusPeriparturient = "periparturient"
	AnimalStatusLactating      = "lactating"
)

// ErrGoatNotDiagnosable is returned when the animal's own record cannot produce
// a usable Animal. It fails loudly rather than defaulting: a defaulted sex would
// silently disable half the register.
// The sentinel's own text is a user-facing prefix, not a log line: every message wrapping
// it is returned verbatim to the phone as the 422 body (see writeDiagnosisError). It reads
// as the farm fact -- this animal cannot be checked -- with the reason supplied by the
// wrapping message.
var ErrGoatNotDiagnosable = fmt.Errorf("this animal cannot be checked")

// ResolveAnimal maps a GoatOS goat onto the engine's Animal.
func ResolveAnimal(facts GoatFacts) (diagnosis.Animal, error) {
	species := strings.ToLower(strings.TrimSpace(facts.Species))
	if species != "goat" && species != "sheep" {
		return diagnosis.Animal{}, fmt.Errorf("%w: unknown species %q", ErrGoatNotDiagnosable, facts.Species)
	}

	// GoatOS stores female/male; the register speaks F/M. There is no default:
	// an unrecognised sex disables one whole half of the sex-gated rules, and
	// doing that quietly is worse than refusing the diagnosis.
	var sex string
	switch strings.ToLower(strings.TrimSpace(facts.Sex)) {
	case "female":
		sex = "F"
	case "male":
		sex = "M"
	default:
		return diagnosis.Animal{}, fmt.Errorf("%w: unknown sex %q", ErrGoatNotDiagnosable, facts.Sex)
	}

	// CLASS RESOLUTION STILL FAILS CLOSED, but on the STAGE rather than on the age band.
	//
	// The engine carries four registers -- adult, kid_milk, kid_weaning, kid_fattening --
	// and they differ in ways that make picking the wrong one worse than picking none. A
	// fattening kid diagnosed off the milk register would never be checked for acidosis,
	// the single thing most likely to kill it; a milk kid diagnosed off the weaning
	// register would never get the drop test, which is the only way floppy kid is caught
	// while it is still cheap to treat.
	//
	// This used to refuse EVERY kid, on the belief that GoatOS records nothing that could
	// tell the three apart. That belief is no longer true: `animal_stage_lookup` is a
	// seeded, tenant-scoped catalog whose codes line up with the spec's own class stages
	// almost one for one (K0 Newborn / K1 Milk training / K2 Milk drinking -> kid_milk,
	// K3 Weaned kids -> kid_weaning, F2* Fattening -> kid_fattening), and `loadGoatFacts`
	// already reads `management_stage` into the facts. The mapping was sitting on the
	// animal's own record, unread. Maintainer decision 2026-08-17.
	//
	// The safety property is preserved by keeping the mapping EXPLICIT and closed: a stage
	// this table does not name is refused, not defaulted. That is what stops a newly-seeded
	// stage from silently inheriting some other cohort's medicine. Note especially that
	// clinical placements (ICU-Kid, Quarantine kids) are deliberately absent -- they say
	// WHERE an animal is, not what it eats or how old it is, so they cannot choose a
	// register and must not guess one.
	//
	// The old code defaulted every kid to `kid_milk`. That was harmless while kids were out
	// of diagnostic scope entirely -- the class only had to exist so emergencies could fire
	// -- and it became dangerous the moment the milk register started producing diagnoses.
	ageBand := strings.ToLower(strings.TrimSpace(facts.AgeBand))
	if ageBand == AgeBandAdult {
		return diagnosis.Animal{
			Class:   diagnosis.ClassAdult,
			Species: species,
			Sex:     sex,
			Status:  resolveStatus(facts),
		}, nil
	}

	class, stage, ok := kidClassForStage(facts.ManagementStage)
	if !ok {
		// The refusal text is what the OPERATOR reads: writeDiagnosisError sends err.Error()
		// straight out as the 422 body, and the phone renders it verbatim. So it names the
		// farm fact and the stage that caused it, and leaves the register mechanics above
		// where the next developer needs them and the manager does not.
		return diagnosis.Animal{}, fmt.Errorf(
			"%w: its stage %q does not say whether it is on milk, weaning or fattening",
			ErrGoatNotDiagnosable, strings.TrimSpace(facts.ManagementStage))
	}

	return diagnosis.Animal{
		Class:   class,
		Stage:   stage,
		Species: species,
		Sex:     sex,
		Status:  resolveStatus(facts),
	}, nil
}

// kidStageClasses maps a GoatOS management stage onto the engine class that treats it,
// plus the sub-stage that class reads.
//
// The sub-stage is not decoration: inside kid_milk the register runs a DIFFERENT ladder for
// a week-old K1 (three milk-bar sessions a day) than for a K2 on the free-choice bar, and
// `offer_ors` / `session_bottle` / `force_milk` all branch on it. Passing the class without
// the stage would produce a confident diagnosis off the wrong half of one register.
//
// Keyed on the stage CODE from `animal_stage_lookup`, compared case-insensitively because the
// same catalog already holds both `kid` and `Kid` age bands from two different import runs.
var kidStageClasses = map[string]struct {
	class string
	stage string
}{
	"k0": {diagnosis.ClassKidMilk, "K0"},
	"k1": {diagnosis.ClassKidMilk, "K1"},
	"k2": {diagnosis.ClassKidMilk, "K2"},
	"k3": {diagnosis.ClassKidWeaning, "K3"},
	// Fattening carries no sub-stage: its register has one cohort and reads no K value.
	"f2":        {diagnosis.ClassKidFattening, ""},
	"f2-male":   {diagnosis.ClassKidFattening, ""},
	"f2-female": {diagnosis.ClassKidFattening, ""},
}

// kidClassForStage resolves a kid's management stage to (class, sub-stage). The bool is false
// for any stage not named above -- including a blank one and a clinical placement -- and the
// caller must refuse rather than default.
func kidClassForStage(managementStage string) (class string, stage string, ok bool) {
	entry, found := kidStageClasses[strings.ToLower(strings.TrimSpace(managementStage))]
	if !found {
		return "", "", false
	}
	return entry.class, entry.stage, true
}

// resolveStatus picks the ONE status the engine accepts, most specific first.
//
// Precedence is periparturient > pregnant > lactating > normal, and the order is
// load-bearing rather than arbitrary. A doe within 14 days of kidding is also
// lactating, and her management stage will say so; if lactating won, MILK_FEVER
// and METRITIS could never fire on the exact animals they exist for.
func resolveStatus(facts GoatFacts) string {
	if facts.DaysSinceKidding != nil && *facts.DaysSinceKidding >= 0 &&
		*facts.DaysSinceKidding <= PeriparturientWindowDays {
		return AnimalStatusPeriparturient
	}
	switch normalizeStage(facts.ManagementStage) {
	case "pregnant":
		return AnimalStatusPregnant
	case "mother", "lactating":
		// "Mother" is the farm's word for a doe raising kids.
		return AnimalStatusLactating
	default:
		return AnimalStatusNormal
	}
}

func normalizeStage(stage string) string {
	s := strings.ToLower(strings.TrimSpace(stage))
	s = strings.ReplaceAll(s, "-", " ")
	return strings.Join(strings.Fields(s), " ")
}

// MissingKiddingHistory reports whether this animal's status could NOT be
// decided on kidding evidence -- a female with no recorded kidding at all.
//
// Callers surface this to the Director rather than hiding it. goat_births is
// written only by the forward birth-capture flow and starts empty, so a doe that
// kidded before that flow existed looks identical to one that never kidded. On
// such an animal MILK_FEVER, METRITIS and the calcium and uterine-prolapse
// emergencies cannot fire, and the proposal will look quieter than the animal is.
//
// It is deliberately NOT an error: refusing to diagnose every doe with no birth
// record would make the module unusable on the existing herd.
func MissingKiddingHistory(facts GoatFacts) bool {
	return strings.EqualFold(strings.TrimSpace(facts.Sex), "female") && facts.DaysSinceKidding == nil
}
