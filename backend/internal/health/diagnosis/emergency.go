package diagnosis

// Emergency ids. These are "do this now" — hands have already started and the
// Director is notified after, because the animal would be dead before a review
// completes.
const (
	EmergencyTube            = "tube"
	EmergencyAcidosisNow     = "acidosis_now"
	EmergencyNoUrine         = "no_urine"
	EmergencyDown            = "down"
	EmergencyWarm            = "warm"
	EmergencyCool            = "cool"
	EmergencyFluids          = "fluids"
	EmergencyCalcium         = "calcium"
	EmergencyFamacha5        = "famacha5"
	EmergencyToxicMastitis   = "toxic_mastitis"
	EmergencyMaggots         = "maggots"
	EmergencySplint          = "splint"
	EmergencyUterineProlapse = "uterine_prolapse"

	// Kids.
	EmergencyIPDextrose = "ip_dextrose"
	EmergencyRLSQ       = "rl_sq"
	EmergencyFloppyNow  = "floppy_now"
)

// detectEmergencies returns the red flags for this form.
//
// Two properties are structural, not incidental:
//
//   - They are FINDING-triggered, never diagnosis-triggered. Nothing here
//     consults the register, so an emergency does not depend on a rule matching.
//   - They fire for EVERY CLASS, and they run BEFORE the scope check. A kid with
//     frothy bloat must still raise the alarm even though no adult diagnostic
//     rule applies to it. Scope gates diagnosis; it never gates emergency
//     detection.
//
// Order is stable and deliberate: it is the order a manager would work down the
// animal, and callers compare these as sets regardless.
func detectEmergencies(animal Animal, f Findings, d derived) []string {
	var out []string

	kid := animal.isKid()

	stomach := f.LeftStomach.orDefault("normal")
	if stomach.has("bloating") || f.FrothyMouth {
		out = append(out, EmergencyTube)
	}
	// Water sound plus off SOLIDS is acidosis happening now; concentrate comes off.
	//
	// Two narrowings, both clinical:
	//
	//   - A milk kid has no rumen to acidify and its form never collects water
	//     sound at all -- a slosh in a milk-fed belly is milk, not acid. Firing
	//     here would pull concentrate that the kid is not eating and treat a
	//     healthy animal.
	//   - "Off feed" means off SOLIDS. f.notEating reads the FEED row, never the
	//     milk row, which is why a weaning kid that skipped a bottle while still
	//     eating concentrate does not land here.
	if !isMilkClass(animal) && stomach.has("acidosis") && f.notEating() {
		out = append(out, EmergencyAcidosisNow)
	}
	// Obstruction. This one also vetoes meloxicam and must be finished tonight
	// rather than left for the 06:00 round.
	if animal.Sex == "M" && f.Straining == "no_urine" {
		out = append(out, EmergencyNoUrine)
	}
	if f.down() {
		out = append(out, EmergencyDown)
	}

	// THE KID CRASH LADDER, and the order is the treatment order.
	//
	// A kid that is cold or down is running out of energy before it is running
	// out of anything else, so sugar goes in FIRST and heat second: warming a
	// hypoglycaemic kid without dextrose burns the last of its reserve. Only
	// then is the mouth considered, and only if it can actually swallow --
	// milk when it sucks and is warmer than 100degF, fluids under the skin
	// otherwise. Pouring milk into a cold or non-suckling kid drowns it.
	//
	// A febrile kid that is DOWN still crashes: the fever explains the illness,
	// not the collapse, and it gets dextrose as well as its Fever course.
	crash := kid && (d.has("HYPOTHERMIA") || f.down())
	if crash {
		out = append(out, EmergencyIPDextrose, EmergencyWarm)
		// Fluids under the skin are for the kid whose MOUTH is unusable. A kid
		// that still sucks keeps the oral route: it is either warm enough to be
		// given milk now, or it is warmed first and offered milk once it is --
		// putting a needle into a kid that can still swallow buys nothing.
		if f.Suckle != "present" {
			out = append(out, EmergencyRLSQ)
		}
	}

	// Floppy kid is caught by the drop test while the animal is still STANDING,
	// which is the whole reason that test exists -- once it is down it is the
	// crash above, not floppy. Fever and hypothermia exclude it for the same
	// reason: those name the collapse, and bicarbonate is not their treatment.
	//
	// Derived from findings, exactly like every other emergency here, rather
	// than from the FLOPPY_KID rule firing. The milk register gates that rule on
	// the same three facts, so the two agree by construction instead of one
	// depending on the other.
	if isMilkClass(animal) && !crash && !d.has("FEVER") &&
		isOneOf(f.Landing, "barely", "falls") {
		out = append(out, EmergencyFloppyNow)
	}

	// Adults reach `warm` through hypothermia alone; a kid has already been given
	// it by the crash ladder above, with dextrose ahead of it.
	if !kid && d.has("HYPOTHERMIA") {
		out = append(out, EmergencyWarm)
	}
	if d.has("HIGH_FEVER") {
		out = append(out, EmergencyCool)
	}
	// Tent is the CORRECTED value, so an emaciated animal does not trigger this
	// on body condition alone.
	if d.correctedTent == "gt4" {
		out = append(out, EmergencyFluids)
	}
	// Calcium and toxic mastitis are fresh-doe emergencies. A kid has neither a
	// recent kidding nor an udder, so both are adults-only rather than merely
	// unlikely.
	if !kid && animal.status() == "periparturient" && (f.down() || f.Activity == "weak") {
		out = append(out, EmergencyCalcium)
	}
	if f.famacha() == 5 {
		out = append(out, EmergencyFamacha5)
	}
	if !kid && f.CMT == "pos" && (d.has("HYPOTHERMIA") || f.down()) {
		out = append(out, EmergencyToxicMastitis)
	}
	if f.Flystrike || f.EartagFlystrike {
		out = append(out, EmergencyMaggots)
	}
	if f.Leg == "fracture" {
		out = append(out, EmergencySplint)
	}
	if !kid && f.Vulva == "prolapse" && animal.status() == "periparturient" {
		out = append(out, EmergencyUterineProlapse)
	}

	return dedupe(out)
}

// isMilkClass is the milk-drinking slice (K0-K2). It is the only class that
// carries the landing test and the only one that never collects water sound.
func isMilkClass(animal Animal) bool { return animal.class() == ClassKidMilk }
