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

// ResolveAnimal maps a GoatOS goat onto the engine's Animal, using the farm's AUTHORED
// routing to choose the diagnosis type.
//
// The routing arrives as a parameter rather than being read here so this function stays pure:
// the repository loads the rows inside the same transaction as the observation it is writing,
// which is what stops a route edit landing between the read and the write.
//
// An EMPTY routing refuses every animal rather than falling back to the shipped map. That is
// deliberate and is the fail-closed property one layer up: a farm whose routing was never
// seeded must say so out loud, because the alternative is diagnosing every animal off a table
// nobody chose. Migration 000395 seeds every tenant that holds goats or a register, so the
// empty case means something is genuinely wrong rather than merely new.
func ResolveAnimal(facts GoatFacts, routing StageRouting) (diagnosis.Animal, error) {
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

	// CLASS RESOLUTION STILL FAILS CLOSED -- now on the farm's AUTHORED routing rather than on
	// a map compiled into this binary.
	//
	// The engine carries a register per type, and they differ in ways that make picking the
	// wrong one worse than picking none. A fattening kid diagnosed off the milk register would
	// never be checked for acidosis, the single thing most likely to kill it; a milk kid
	// diagnosed off the weaning register would never get the drop test, which is the only way
	// floppy kid is caught while it is still cheap to treat. So an animal nothing routes is
	// REFUSED, and the refusal names the stage so a director can fix it on Health Config.
	//
	// WHAT MOVED, AND WHAT DID NOT. The 2026-08-17 decision read `animal_stage_lookup` through
	// a closed Go map, and the safety property was that "a stage this table does not name is
	// refused, not defaulted". That property is unchanged. What changed is WHO may write the
	// table: on 2026-09-23 `Warmup` was missing from it, 58 live kids could not be observed at
	// all, and the repair was a deploy. Maintainer instruction the same day: adding a type and
	// pointing a stage at it is dashboard work. Migration 000395 seeds the shipped map row for
	// row, so this is a change of authority and not of behaviour.
	//
	// ADULTS ARE ROUTED THROUGH THE SAME LOOKUP, which is new. The old code branched on
	// `age_band == "adult"` and never read an adult's stage, so every doe, buck and adult in a
	// clinical pen reached the adult register. The seed expresses that as an adult WILDCARD row
	// rather than as an `if`, so a farm can peel one stage off it -- Mother onto its own type --
	// or delete it to make adults fail-closed like kids. Both are now visible decisions.
	typeKey, subStage, routed := routing.Resolve(facts.AgeBand, facts.ManagementStage)
	if !routed {
		// The refusal text is what the OPERATOR reads: writeDiagnosisError sends err.Error()
		// straight out as the 422 body, and the phone renders it verbatim. So it names the farm
		// fact and the stage that caused it, and leaves the register mechanics here where the
		// next developer needs them and the manager does not.
		return diagnosis.Animal{}, RouteRefusal(facts.ManagementStage)
	}

	return diagnosis.Animal{
		Class:   typeKey,
		Stage:   subStage,
		Species: species,
		Sex:     sex,
		Status:  resolveStatus(facts),
	}, nil
}

// The shipped stage -> type map that used to live here is now
// domain.BuiltinStageRoutes() in stage_routes.go, where it is the golden ORACLE for migration
// 000395's seed and is read by nothing on the runtime path. Routing is authored data; see
// StageRouting for the resolution rule and why the adult wildcard exists.

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
