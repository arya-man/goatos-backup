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

	stomach := f.LeftStomach.orDefault("normal")
	if stomach.has("bloating") || f.FrothyMouth {
		out = append(out, EmergencyTube)
	}
	// Water sound plus off-feed is acidosis happening now; concentrate comes off.
	if stomach.has("acidosis") && f.notEating() {
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
	if d.has("HYPOTHERMIA") {
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
	if animal.status() == "periparturient" && (f.down() || f.Activity == "weak") {
		out = append(out, EmergencyCalcium)
	}
	if f.famacha() == 5 {
		out = append(out, EmergencyFamacha5)
	}
	if f.CMT == "pos" && (d.has("HYPOTHERMIA") || f.down()) {
		out = append(out, EmergencyToxicMastitis)
	}
	if f.Flystrike || f.EartagFlystrike {
		out = append(out, EmergencyMaggots)
	}
	if f.Leg == "fracture" {
		out = append(out, EmergencySplint)
	}
	if f.Vulva == "prolapse" && animal.status() == "periparturient" {
		out = append(out, EmergencyUterineProlapse)
	}

	return out
}
