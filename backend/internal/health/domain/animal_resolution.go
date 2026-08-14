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
var ErrGoatNotDiagnosable = fmt.Errorf("health: goat record cannot be resolved for diagnosis")

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

	ageBand := strings.ToLower(strings.TrimSpace(facts.AgeBand))
	class := diagnosis.ScopeAdult
	if ageBand != AgeBandAdult {
		// Anything not explicitly adult is out of the v1 diagnosis scope. The
		// engine still emits emergencies for it -- that ordering is the whole
		// point of running red flags before the scope check.
		class = "kid_milk"
	}

	return diagnosis.Animal{
		Class:   class,
		Species: species,
		Sex:     sex,
		Status:  resolveStatus(facts),
	}, nil
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
