package diagnosis

// Kid-specific behaviour lives here so the clinical differences between the
// three kid slices can be read in one place.
//
// The engine's shape is class-agnostic -- validate, derive, emergencies, match,
// reconcile, safety net, housing -- and the registers carry most of the
// difference as data. What is left is the part no rule table can express: how a
// missed feed becomes a problem, what goes into the animal, and where it sleeps.

// Kids director flags. These are the milk/suckle compiler's output: they tell
// the Director what to put into the animal, and several of them are refusals
// rather than instructions.
const (
	FlagMilkOff            = "milk_off"
	FlagResumeMilk         = "resume_milk"
	FlagOfferMilk          = "offer_milk"
	FlagOfferORS           = "offer_ors"
	FlagNoOralMilk         = "no_oral_milk"
	FlagBicarb             = "bicarb"
	FlagCheckUntilDrinking = "check_until_drinking"
	FlagForceMilk          = "force_milk"
	FlagNoForceFeedDown    = "no_force_feed_down"
	FlagRLSQ               = "rl_sq"
	FlagSessionBottle      = "session_bottle"
	FlagK2Refusal          = "k2_refusal"
	FlagK3Refusal          = "k3_refusal"
	FlagThiamine           = "thiamine"
)

// Kid register ids that exist on no adult table.
const (
	IDFloppyKid   = "FLOPPY_KID"
	IDHypothermia = "HYPOTHERMIA"
	IDNavelIll    = "NAVEL_ILL"
)

// SOPMilkRefusal is the card a not-drinking kid is worked from. It replaces the
// generic Supportive card on Undifferentiated, because "this kid has stopped
// drinking" has a specific ladder -- offer, then force, then fluids -- and
// Supportive does not.
const SOPMilkRefusal = "Milk refusal"

// kidState is everything the compiler and the housing rules need to agree on,
// computed once. They must agree: a kid told to drink but housed where nobody
// checks, or housed in ICU with no instruction, is a half-treated animal.
type kidState struct {
	isKid   bool
	class   string
	stage   string
	refusal int

	// milkProblem is a not-drinking ENERGY problem, which is NOT the same as
	// having missed a feed. See milkProblem for why the three slices count a
	// miss differently.
	milkProblem bool

	// floppy is the standing collapse the drop test catches. Either the register
	// rule fired or the emergency did; they read the same three facts, so this
	// is belt and braces rather than two sources of truth.
	floppy bool

	// crash is the recumbent-or-cold kid on the dextrose ladder.
	crash bool

	suckles bool
	warm    bool

	// k2Overlay is a K2 kid that has come off the free-choice bar and been put
	// onto counted bottles so the refusal can be MEASURED. It is a measuring
	// instrument, not a bottle program -- K2 earned free choice and is meant to
	// go back to it.
	k2Overlay bool
}

func newKidState(animal Animal, f Findings, p *Proposal) kidState {
	s := kidState{
		isKid:       animal.isKid(),
		class:       animal.class(),
		stage:       animal.Stage,
		refusal:     f.refusals(),
		milkProblem: milkProblem(animal, f),
		suckles:     f.Suckle == "present",
		warm:        f.temp() > 100.0,
	}
	if !s.isKid {
		return s
	}
	s.floppy = contains(p.Problems, IDFloppyKid) || contains(p.Emergencies, EmergencyFloppyNow)
	s.crash = contains(p.Emergencies, EmergencyIPDextrose)
	s.k2Overlay = animal.isMilkBar() && (f.MilkIntake.has("not_drinking") || s.refusal >= 1)
	return s
}

// isMilk / isWeaning name the two slices that COUNT feeds. Fattening kids eat
// concentrate and are not on a milk ladder at all.
func (s kidState) isMilk() bool    { return s.class == ClassKidMilk }
func (s kidState) isWeaning() bool { return s.class == ClassKidWeaning }
func (s kidState) countsFeeds() bool {
	return s.isMilk() || s.isWeaning()
}

// applyKidCompiler decides what goes into the kid.
//
// The single rule underneath all of it: NEVER PUT MILK INTO AN ANIMAL THAT
// CANNOT SWALLOW IT. Suckle is the gate, and a kid that fails it gets fluids
// under the skin instead. The second rule is that a down kid is never
// force-fed -- the reflex to get food into a collapsing animal is exactly the
// one that drowns it.
func (r *Register) applyKidCompiler(p *Proposal, s kidState, f Findings) {
	if !s.isKid {
		return
	}

	// Floppy: the milk comes OFF and bicarbonate goes in. Milk into a floppy kid
	// makes the acidosis worse, which is why this branch excludes every drinking
	// instruction below.
	if s.floppy {
		p.DirectorFlags = append(p.DirectorFlags, FlagMilkOff, FlagBicarb)
	}
	if f.down() {
		p.DirectorFlags = append(p.DirectorFlags, FlagNoForceFeedDown)
	}
	if f.Suckle == "absent" {
		p.DirectorFlags = append(p.DirectorFlags, FlagNoOralMilk)
	}

	// The K2 overlay is a measuring instrument: a kid that stopped drinking at
	// the bar goes onto counted bottles so the refusal can be counted at all.
	if s.k2Overlay && !s.floppy && !s.crash {
		p.DirectorFlags = append(p.DirectorFlags, FlagSessionBottle)
	}
	if s.class == ClassKidMilk && s.stage == "K2" && s.milkProblem && !s.floppy {
		p.DirectorFlags = append(p.DirectorFlags, FlagK2Refusal)
	}
	if s.isWeaning() && s.milkProblem && !s.floppy {
		p.DirectorFlags = append(p.DirectorFlags, FlagK3Refusal)
	}

	// Fluids under the skin when the ladder has run past what the mouth can
	// take. On the crash path this is already an EMERGENCY, so it is not
	// repeated as advice.
	if s.countsFeeds() && s.refusal >= 2 && !s.floppy && !s.crash &&
		!contains(p.Emergencies, EmergencyRLSQ) {
		p.DirectorFlags = append(p.DirectorFlags, FlagRLSQ)
	}

	r.applyKidDrinkOrders(p, s, f)

	if s.countsFeeds() && s.milkProblem && contains(p.Problems, IDUndifferentiated) {
		p.DirectorFlags = append(p.DirectorFlags, FlagCheckUntilDrinking)
	}
	if contains(p.Problems, "NEURO") {
		// Thiamine is the kid presentation of neuro. Star-gazing is a Neuro
		// sign, deliberately NOT labelled polioencephalomalacia: naming the
		// disease would commit the Director to a diagnosis the form cannot
		// support.
		p.DirectorFlags = append(p.DirectorFlags, FlagThiamine)
	}
}

// applyKidDrinkOrders is the drinking ladder: offer, force, or neither.
//
// Order matters and is clinical. A kid that will not take the bar is first
// OFFERED milk; only once it has refused enough is milk FORCED; and the
// oral-rehydration salt is for the in-between case where the kid is drinking
// something but not enough. Every rung is gated on suckle, and none of them
// applies to a floppy kid -- that one is on bicarbonate with the milk off.
func (r *Register) applyKidDrinkOrders(p *Proposal, s kidState, f Findings) {
	if s.floppy {
		return
	}

	// On the crash path the mouth is only used if the kid is warm enough to
	// process what goes into it. Below 100degF the gut has stopped and milk sits
	// there; fluids go under the skin instead.
	if s.crash {
		if s.suckles && s.warm {
			p.DirectorFlags = append(p.DirectorFlags, FlagOfferMilk)
		}
		return
	}

	if !s.suckles {
		return
	}

	switch {
	case s.isMilk() && s.refusal >= 3:
		// Every session refused. Past offering.
		p.DirectorFlags = append(p.DirectorFlags, FlagForceMilk)
	case s.isWeaning() && s.refusal >= 2:
		// Both measured bottles missed.
		p.DirectorFlags = append(p.DirectorFlags, FlagForceMilk)
	case s.isWeaning() && (s.refusal == 1 || f.notEating()):
		// One 200 ml bottle missed, or off concentrate. Offer -- do NOT reach
		// for the milk-kid ORS rung, which is a three-session ladder this slice
		// does not run.
		p.DirectorFlags = append(p.DirectorFlags, FlagOfferMilk)
	case s.class == ClassKidFattening && f.notEating():
		// A fattening kid can still take a bottle where an adult cannot, and
		// that is the whole reason this branch exists.
		p.DirectorFlags = append(p.DirectorFlags, FlagOfferMilk)
	case s.isMilk() && s.milkProblem:
		p.DirectorFlags = append(p.DirectorFlags, FlagOfferMilk)
	}

	// Oral rehydration is the light rung, and it is a MILK-KID rung only.
	if s.isMilk() {
		switch {
		case s.stage != "K2" && s.refusal == 1:
			// K1's first miss is training, not disease: the kid is watched and
			// offered salts, not treated.
			p.DirectorFlags = append(p.DirectorFlags, FlagOfferORS)
		case s.refusal >= 2 && s.refusal < 3:
			p.DirectorFlags = append(p.DirectorFlags, FlagOfferORS)
		case s.milkProblem && !s.k2Overlay && s.refusal < 3:
			// Past every session refused the answer is force, not salts.
			p.DirectorFlags = append(p.DirectorFlags, FlagOfferORS)
		}
	}
}

// applyKidHousing raises acuity for the kid-only presentations.
//
// A kid decompensates far faster than an adult, so the thresholds are lower and
// the evening round is bought more readily: a kid that has stopped drinking must
// be looked at again the same day, because "we will see it tomorrow" is how a
// refusal becomes a death.
func (r *Register) applyKidHousing(p *Proposal, s kidState, f Findings, d derived) {
	if !s.isKid {
		return
	}
	h := &p.Housing

	// Floppy and hypothermia are ICU on sight for a kid: both are energy
	// collapse, and both are treatable in hours if someone is watching.
	if contains(p.Problems, IDFloppyKid) || contains(p.Problems, IDHypothermia) {
		h.Acuity = AcuityICU
	}
	// A febrile kid that is also down is not a standing fever.
	if d.has("FEVER") && f.down() {
		h.Acuity = AcuityICU
	}
	// Every session refused, or both bottles: the mouth route has failed.
	if s.isMilk() && s.refusal >= 3 {
		h.Acuity = AcuityICU
	}
	if s.isWeaning() && s.refusal >= 2 {
		h.Acuity = AcuityICU
	}

	// A counted refusal that is not yet ICU is a ward case with BOTH rounds --
	// the second look is the point.
	if s.milkProblem && h.Acuity != AcuityICU {
		h.Acuity = AcuityWard
	}

	// Not drinking is as much a competition problem as not eating: a kid that
	// has to fight for the bar will not start again while it is being shoved.
	if s.milkProblem {
		h.LowCompetition = true
	}
}

// applyKidShifts buys the evening round for a kid that has stopped drinking.
//
// This is the one documented exception to "ward is once a day". It is bought
// deliberately and narrowly: only for a counted milk refusal, and only on the
// two slices that count feeds.
func (r *Register) applyKidShifts(p *Proposal, s kidState) {
	if !s.isKid || !s.countsFeeds() || !s.milkProblem {
		return
	}
	if p.Housing.Acuity != AcuityWard {
		return
	}
	p.Housing.MorningWalk = true
	p.Housing.EveningWalk = true
	p.DirectorFlags = removeString(p.DirectorFlags, FlagNoEveningWard)
	p.DirectorFlags = append(p.DirectorFlags, FlagBothShifts)
}

// kidUndifferentiatedSOP names the card an Undifferentiated kid is worked from.
// A kid that has stopped drinking is worked from the refusal ladder, not the
// generic supportive card.
func kidUndifferentiatedSOP(s kidState) string {
	// On the MILK slices the bar is the entire diet, so an undifferentiated milk
	// kid is worked from the refusal ladder whatever the count says -- getting
	// milk into it is the treatment.
	if s.isMilk() {
		return SOPMilkRefusal
	}
	// A weaning kid also eats concentrate, so it lands on the refusal card only
	// when the BOTTLES are what it refused. Off concentrate with both bottles
	// taken is an ordinary supportive case.
	if s.isWeaning() && s.milkProblem {
		return SOPMilkRefusal
	}
	return "Supportive"
}
