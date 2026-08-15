package diagnosis

// Housing values. Housing is a DIRECTIVE. This package never moves an animal;
// the policy-pack workflow that owns location is the only writer.
const (
	AcuityHome  = "home"
	AcuityField = "field"
	AcuityWard  = "ward"
	AcuityICU   = "icu"

	ContainmentHome       = "home"
	ContainmentQuarantine = "quarantine"
)

// Director flags. These route a case to a human; they are not diagnoses.
const (
	FlagManagerNeverCloses   = "manager_never_closes"
	FlagNAD                  = "nad"
	FlagNADTwice             = "nad_twice"
	FlagHeatConfirm          = "heat_confirm"
	FlagHeatSOP              = "heat_sop"
	FlagAntihistamineAdjunct = "antihistamine_adjunct"
	FlagTonight              = "tonight"
	FlagNextLook6am          = "next_look_6am"
	FlagNoEveningWard        = "no_evening_ward"
	FlagBothShifts           = "both_shifts"
	FlagOnceDayQuarantine    = "once_day_quarantine"
	FlagSameDayTetanus       = "same_day_tetanus"
	FlagTetanusConfirmed     = "tetanus_confirmed"
	FlagPoorPrognosis        = "poor_prognosis"
	FlagSameDayNeuro         = "same_day_neuro"
	FlagNeuroTrial           = "neuro_trial"
	FlagNoHandleMouth        = "no_handle_mouth"
	FlagCullArthritis        = "cull_arthritis"
	FlagInduce               = "induce"
	FlagEnergy               = "energy"
	FlagRepeatCalcium        = "repeat_calcium"
	FlagDualTrajectory       = "dual_trajectory"
	FlagMandatoryD7          = "mandatory_d7"
	FlagHumaneEndpoint       = "humane_endpoint"
	FlagDeathChain           = "death_chain"
	FlagOverride             = "override"
	FlagContradictionStatus  = "contradiction_status"
	FlagContagionWatch       = "contagion_watch"
	FlagContagionEvent       = "contagion_event"
	FlagPinkeyeCluster       = "pinkeye_cluster"
	FlagVanishedTooFast      = "vanished_too_fast"
	FlagRelapse              = "relapse"
	FlagSupportiveContinues  = "supportive_continues"
	FlagGloves               = "gloves"
	FlagPullConcentrate      = "pull_concentrate"
	FlagConcentrateOff       = "concentrate_off"
)

// HintFreshCleanVulva tells the Director why Metritis did NOT open on a fresh
// doe with a fever: the vulva was clean. Metritis needs bad smell or pus.
const HintFreshCleanVulva = "fresh_clean_vulva"

// Scope values.
const (
	ScopeAdult      = "adult"
	ScopeOutOfScope = "out_of_scope"
)

// icuOnSight are diagnoses that mean ICU regardless of how the animal presents.
var icuOnSight = map[string]bool{
	"BLOAT": true, "CALCULI": true, "TETANUS": true, "NEURO": true, "MILK_FEVER": true,
}

// stickyIDs stay open across a quieter follow-up form. A course under treatment
// does not end because today's observation is milder — only the Director closes.
var stickyIDs = map[string]bool{
	"PPR": true, "POX": true, "ORF": true, "MILK_FEVER": true, "PINKEYE": true,
	"ACIDOSIS": true, "FRACTURE": true, "MASTITIS": true, "WOUNDS": true,
	"FEVER": true, "NEURO": true, "PREG_TOX": true, "UNDIFFERENTIATED": true,
	// Kid courses. A floppy kid that stands up today is not cured -- it is a
	// floppy kid on bicarbonate that is working -- and stopping there is how it
	// relapses overnight.
	IDFloppyKid: true, IDHypothermia: true, IDNavelIll: true,
}

// isNamedProblem reports whether an id is a specific diagnosis from THIS
// register. If none is live, an off-feed animal falls to Undifferentiated rather
// than being called healthy.
//
// Derived from the loaded register rather than listed here. The list used to be
// a hardcoded set of adult ids, which was invisible while adult was the only
// class and wrong the moment a second register arrived: a kid whose only
// diagnosis was HYPOTHERMIA or FLOPPY_KID -- ids no adult table contains -- was
// treated as having nothing named and collected a spurious Undifferentiated
// beside its real one.
func (r *Register) isNamedProblem(id string) bool {
	if id == IDUndifferentiated {
		return false
	}
	rule := r.Rule(id)
	return rule != nil && rule.Kind == KindProblem
}

// IDUndifferentiated is not a register rule. It is the safety net's own label
// for an animal that is clearly unwell with nothing specific to name, and it
// routes to Supportive care rather than to a disease SOP.
const IDUndifferentiated = "UNDIFFERENTIATED"

// Proposal is the engine's output. It is a PROPOSAL: the Health Director
// confirms every Problem before a course opens. Emergencies and field actions
// are the sole exceptions — they have already started.
type Proposal struct {
	Valid        bool   `json:"valid"`
	RejectReason string `json:"reject_reason,omitempty"`
	Scope        string `json:"scope"`

	// RegisterVersion pins the rule table this run used, so the case stays
	// interpretable after the register is edited.
	RegisterVersion string `json:"register_version"`

	Emergencies  []string `json:"emergencies"`
	Problems     []string `json:"problems"`
	Covered      []string `json:"covered"`
	Rechecks     []string `json:"rechecks"`
	FieldActions []string `json:"field_actions"`
	Unexplained  []string `json:"unexplained"`

	Ongoing       []string `json:"ongoing"`
	New           []string `json:"new"`
	ProposeClose  []string `json:"propose_close"`
	ProposeExtend []string `json:"propose_extend"`

	Tiers      map[string]Tier   `json:"tiers"`
	SOP        map[string]string `json:"sop"`
	CourseType map[string]string `json:"course_type"`

	Housing Housing `json:"housing"`

	DirectorFlags []string `json:"hd_flags"`
	Hints         []string `json:"hints"`

	NoMeloxicam bool `json:"no_meloxicam"`
	Club        bool `json:"club"`
}

// Housing is the directive: where the animal should be, and which shift lists it
// appears on. Follow-up cadence comes from housing, never from an hour clock.
type Housing struct {
	Acuity         string `json:"acuity"`
	Containment    string `json:"containment"`
	LowCompetition bool   `json:"low_competition"`
	MorningWalk    bool   `json:"morning_walk"`
	EveningWalk    bool   `json:"evening_walk"`

	// NoDueOvernight is always true: the farm is empty 00:00-06:00 and a due
	// time in that window is work nobody can do. An 20:30 emergency is treated
	// that shift and then sits on the 06:00 list.
	NoDueOvernight bool `json:"no_due_overnight"`
}

// Evaluate runs the full pipeline for one animal and one form.
//
// The order below is mandatory and each position was paid for:
//
//	validate      a contradictory form is not diagnosed at all
//	derive        numbers become meaning
//	emergencies   ALL classes, BEFORE the scope check
//	scope         non-adult stops diagnosis (v1); emergencies already fired
//	match         parallel rules, residual arbitration, suppression, ranking
//	reconcile     against open problems, BEFORE action assignment and BEFORE NAD
//	safety net    Undifferentiated, NAD, the unexplained headline
//	housing       acuity, containment, shifts
//
// Reconcile runs before action assignment because otherwise the engine emits
// "treat" for a problem already under treatment, and every daily follow-up
// duplicates every open problem.
func (r *Register) Evaluate(animal Animal, f Findings, ctx Context) Proposal {
	p := Proposal{
		Valid:           true,
		Scope:           animal.class(),
		RegisterVersion: r.Version,
		Tiers:           map[string]Tier{},
		SOP:             map[string]string{},
		CourseType:      map[string]string{},
		Housing: Housing{
			Acuity:         AcuityHome,
			Containment:    ContainmentHome,
			NoDueOvernight: true,
		},
	}

	// A register may only diagnose the class it was bound to. This is the last
	// line of defence for the spec's loudest never -- do not load adult YAML for
	// a milk kid -- and it fails CLOSED: refusing to diagnose is recoverable,
	// while diagnosing a kid off the adult table produces a confident, wrong,
	// durable medical record.
	if r.boundClass != "" && r.boundClass != animal.class() {
		p.Valid = false
		p.RejectReason = RejectRegisterClassMismatch
		return p
	}

	if reason := validateForm(animal, f); reason != "" {
		p.Valid = false
		p.RejectReason = reason
		return p
	}

	d := deriveTokens(animal, f)
	p.Emergencies = detectEmergencies(animal, f, d)

	evidence := buildEvidence(animal, f, d)
	matched := r.evaluateRegister(animal, evidence)

	p.Unexplained = matched.unexplained
	for id, tier := range matched.tiers {
		p.Tiers[id] = tier
	}

	problems := make([]string, 0, len(matched.problems))
	for _, h := range matched.problems {
		problems = append(problems, h.rule.ID)
	}
	covered := append([]string{}, matched.covered...)

	abnormal := hasAbnormal(f, d, matched)

	problems, covered = r.reconcile(&p, problems, covered, f, d, ctx)
	problems = r.applyUndifferentiated(problems, animal, f, d)
	problems, covered = r.applyCovers(problems, covered)

	// NAD: nothing abnormal, nothing fired, and no follow-up action to take.
	// The default is a recheck, never a close.
	followUpAction := len(p.ProposeClose) > 0 || len(p.ProposeExtend) > 0 ||
		contains(p.DirectorFlags, FlagVanishedTooFast)
	if !abnormal && len(problems) == 0 && !followUpAction {
		p.Problems = nil
		p.DirectorFlags = append(p.DirectorFlags, FlagNAD)
		if ctx.NADPrior7d >= 1 {
			p.DirectorFlags = append(p.DirectorFlags, FlagNADTwice)
		}
		p.Housing.Acuity = AcuityHome
		r.applyDirectorAlways(&p, animal, f, ctx)
		// The kid compiler runs even here. A K1 kid that missed ONE bar session
		// is NAD -- that miss is training, not disease -- and is still offered
		// oral salts. Skipping the compiler on this path would send a kid that
		// is starting to fall behind away with no instruction at all.
		r.applyKidCompiler(&p, newKidState(animal, f, &p), f)
		p.DirectorFlags = dedupe(p.DirectorFlags)
		return p
	}

	p.Problems = dedupe(problems)
	p.Covered = dedupe(covered)
	p.FieldActions = matched.fieldActions
	p.Rechecks = matched.rechecks
	if matched.adjunct {
		p.DirectorFlags = append(p.DirectorFlags, FlagAntihistamineAdjunct)
	}

	r.splitOngoingAndNew(&p, ctx)
	r.applyClinicalFlags(&p, animal, f, d, ctx)
	r.applyDirectorAlways(&p, animal, f, ctx)
	r.applyShedPass(&p, ctx)

	kid := newKidState(animal, f, &p)

	p.Housing = r.decideHousing(p.Problems, animal, f, d, p.Emergencies)
	if len(p.FieldActions) > 0 && len(p.Problems) == 0 && len(p.Rechecks) == 0 {
		p.Housing.Acuity = AcuityField
	}
	r.applyKidHousing(&p, kid, f, d)
	r.applyShiftLists(&p)
	r.applyKidShifts(&p, kid)
	r.applyDrugRules(&p, f, d, ctx)
	r.applyKidCompiler(&p, kid, f)
	r.applyCourseRefs(&p, kid)

	p.DirectorFlags = dedupe(p.DirectorFlags)
	return p
}

// hasAbnormal asks whether the manager recorded anything at all worth noticing.
// It is deliberately broad: NAD is a strong claim, and a finding that fires no
// rule is exactly the case the unexplained channel exists for.
func hasAbnormal(f Findings, d derived, matched matchResult) bool {
	switch {
	case d.any(),
		len(matched.problems) > 0, len(matched.fieldActions) > 0, len(matched.rechecks) > 0,
		f.Diarrhea.Set, f.notEating(), f.Nasal, f.LockedJaw, f.Yellow, f.Flystrike,
		f.BodyEdema, f.Ticks, f.Competition, f.FrothyMouth, f.RedUrine,
		f.EartagFlystrike, f.EartagWound:
		return true
	case f.Activity != "" && f.Activity != "standing" && f.Activity != "normal":
		return true
	case len(f.Neuro) > 0, len(f.Eyes.except("normal")) > 0:
		return true
	case f.Mouth == "orf_scabs", f.RashCharacter != "", f.Hairloss:
		return true
	case f.Leg != "" && f.Leg != "normal":
		return true
	case len(f.wounds().except("none", "no")) > 0:
		return true
	case f.CMT == "pos", f.Lactation == "pus":
		return true
	case f.Vulva != "" && f.Vulva != "nothing" && f.Vulva != "none" && f.Vulva != "discharge_no_smell":
		return true
	case f.Straining == "straining", f.Straining == "no_urine":
		return true
	case f.RumenMovement == "not_felt":
		return true
	case f.Breathing.hasAny("pant", "cough"):
		return true
	case f.Lumps != "":
		return true
	}
	return false
}

// reconcile compares today's form against the courses already open.
//
// It runs BEFORE NAD, so an otherwise-normal form can still propose a close, and
// before action assignment, so an open problem is not re-treated as new.
func (r *Register) reconcile(p *Proposal, problems, covered []string, f Findings, d derived, ctx Context) ([]string, []string) {
	day := ctx.day()
	woundsPresent := len(f.wounds().except("none", "no")) > 0
	eating := f.eating()

	for _, open := range ctx.Open {
		resolved := false
		switch {
		// A foot-rot lesion cannot honestly disappear overnight. This is not a
		// cure — it is evidence the original diagnosis was wrong.
		case open == "FOOT_ROT" && (f.Leg == "" || f.Leg == "normal") && day <= 2:
			p.DirectorFlags = append(p.DirectorFlags, FlagVanishedTooFast)
			resolved = true
		case open == "WOUNDS" && !woundsPresent:
			p.ProposeClose = appendUnique(p.ProposeClose, "WOUNDS")
			resolved = true
		// No longer FEBRILE, which is a class question rather than a number: a
		// kid at 103.5degF still has a fever where an adult does not, so the
		// derived token is asked instead of comparing the temperature here.
		case open == "FEVER" && day >= 3 && !d.has("FEVER") && !eating.has("not_eating"):
			p.ProposeClose = appendUnique(p.ProposeClose, "FEVER")
			resolved = true
		case open == "MASTITIS" && f.CMT == "neg":
			// Type T closes on a TEST, not a calendar: CMT negative twice.
			if ctx.CMTNegStreak+1 >= 2 {
				p.ProposeClose = appendUnique(p.ProposeClose, "MASTITIS")
				resolved = true
			} else if !contains(problems, open) {
				problems = append(problems, open)
			}
		// A floppy kid that is back on its feet and responsive has answered the
		// bicarbonate. The milk goes back ON -- it was taken away as treatment,
		// and leaving it off is its own harm.
		case open == IDFloppyKid && day >= 2 && f.Activity == "standing" &&
			!isOneOf(f.Responsiveness, "dull", "unresponsive"):
			p.ProposeClose = appendUnique(p.ProposeClose, IDFloppyKid)
			p.DirectorFlags = append(p.DirectorFlags, FlagResumeMilk)
			resolved = true
		case open == IDHypothermia && !d.has("HYPOTHERMIA"):
			p.ProposeClose = appendUnique(p.ProposeClose, IDHypothermia)
			resolved = true
		case stickyIDs[open] && !contains(problems, open) && !contains(covered, open):
			problems = append(problems, open)
		}
		if resolved {
			problems = removeString(problems, open)
		}
	}

	// Still febrile on day 3 restarts the course rather than adding silent extra
	// days, so the record shows an extension the Director agreed to.
	if contains(ctx.Open, "FEVER") && contains(problems, "FEVER") && day >= 3 &&
		!contains(p.ProposeClose, "FEVER") && d.has("FEVER") {
		p.ProposeExtend = appendUnique(p.ProposeExtend, "FEVER")
	}

	// A recently closed mastitis that is CMT positive again is a relapse, which
	// is a NEW problem rather than a reopened one.
	if contains(ctx.ClosedRecent, "MASTITIS") && f.CMT == "pos" {
		p.DirectorFlags = append(p.DirectorFlags, FlagRelapse)
		if !contains(problems, "MASTITIS") {
			problems = append(problems, "MASTITIS")
		}
	}

	return problems, covered
}

// applyUndifferentiated is the safety net for an animal that is clearly unwell
// with nothing specific to name. "Not eating" is not a disease, and calling it
// one would be worse than admitting the register has no label for this.
func (r *Register) applyUndifferentiated(problems []string, animal Animal, f Findings, d derived) []string {
	if contains(problems, IDUndifferentiated) {
		return problems
	}
	// A kid that has stopped drinking is off feed even when the FEED row says
	// nothing: on the milk slices the bar IS the diet. Reading only f.notEating
	// here would let a kid that refused every bottle come back as healthy.
	crashing := f.notEating() || d.has("HYPOTHERMIA") || f.RumenMovement == "not_felt" || f.down() ||
		milkProblem(animal, f)
	if !crashing {
		return problems
	}
	if contains(problems, "FEVER") {
		return problems
	}
	for _, id := range problems {
		if r.isNamedProblem(id) {
			return problems
		}
	}
	return append(problems, IDUndifferentiated)
}

// applyCovers re-applies suppression after reconcile has added sticky ids back.
// Without this, a sticky FEVER restored beside a confirmed PPR would open a
// second problem the PPR course already covers.
//
// It loops to a fixed point because a suppression can expose another.
func (r *Register) applyCovers(problems, covered []string) ([]string, []string) {
	ids := dedupe(problems)
	extra := append([]string{}, covered...)

	for changed := true; changed; {
		changed = false
		for _, id := range append([]string{}, ids...) {
			rule := r.Rule(id)
			if rule == nil {
				continue
			}
			targets := append([]string{}, rule.Suppresses...)
			if contains(rule.ExplainsFindings, "FEVER") {
				targets = append(targets, "FEVER")
			}
			for _, other := range targets {
				if other == id || !contains(ids, other) {
					continue
				}
				ids = removeString(ids, other)
				extra = append(extra, other)
				changed = true
			}
		}
	}
	return ids, dedupe(extra)
}

// splitOngoingAndNew separates work already under way from work raised today, so
// a daily follow-up does not read as a fresh outbreak.
func (r *Register) splitOngoingAndNew(p *Proposal, ctx Context) {
	if len(ctx.Open) == 0 {
		p.New = append([]string{}, p.Problems...)
		return
	}
	for _, id := range p.Problems {
		if contains(ctx.Open, id) {
			p.Ongoing = appendUnique(p.Ongoing, id)
		} else {
			p.New = appendUnique(p.New, id)
		}
	}
	// An open sticky course that today's form did not re-evidence is still
	// ongoing — unless it was proposed for close or its evidence vanished
	// impossibly fast.
	for _, open := range ctx.Open {
		if contains(p.Problems, open) {
			p.Ongoing = appendUnique(p.Ongoing, open)
			continue
		}
		if contains(p.ProposeClose, open) || contains(p.DirectorFlags, FlagVanishedTooFast) {
			continue
		}
		if stickyIDs[open] {
			p.Ongoing = appendUnique(p.Ongoing, open)
		}
	}
}

// applyClinicalFlags routes cases to a human. Each of these is a same-day
// Director matter, not something the engine decides.
func (r *Register) applyClinicalFlags(p *Proposal, animal Animal, f Findings, d derived, ctx Context) {
	day := ctx.day()

	// Panting is neither fever nor heat stress. Heat and infection look alike
	// and only the Director may call it.
	if f.Breathing.has("pant") {
		p.DirectorFlags = append(p.DirectorFlags, FlagHeatConfirm)
		if ctx.HeatConfirmed {
			p.DirectorFlags = append(p.DirectorFlags, FlagHeatSOP)
		}
	}

	if animal.status() == "periparturient" && contains(p.Problems, "FEVER") && !contains(p.Problems, "METRITIS") {
		p.Hints = appendUnique(p.Hints, HintFreshCleanVulva)
	}

	if contains(p.Problems, "TETANUS") {
		p.DirectorFlags = append(p.DirectorFlags, FlagSameDayTetanus)
		if f.LockedJaw {
			p.DirectorFlags = append(p.DirectorFlags, FlagTetanusConfirmed)
		}
		p.DirectorFlags = append(p.DirectorFlags, FlagPoorPrognosis)
	}
	if contains(p.Problems, "NEURO") {
		p.DirectorFlags = append(p.DirectorFlags, FlagSameDayNeuro, FlagNoHandleMouth)
		if contains(ctx.Open, "NEURO") && day >= 2 {
			p.DirectorFlags = append(p.DirectorFlags, FlagNeuroTrial)
		}
	}
	if contains(p.Problems, "ARTHRITIS") {
		p.DirectorFlags = append(p.DirectorFlags, FlagCullArthritis)
	}
	if contains(p.Problems, "PREG_TOX") && f.down() {
		p.DirectorFlags = append(p.DirectorFlags, FlagEnergy)
		// The engine does not induce. It says the Director must decide today.
		if day >= 2 {
			p.DirectorFlags = append(p.DirectorFlags, FlagInduce)
		}
	}
	// Up then down again is relapsing hypocalcaemia — repeat calcium, do not
	// reroute to pregnancy toxaemia.
	if contains(p.Problems, "MILK_FEVER") && ctx.PriorImproved {
		p.DirectorFlags = append(p.DirectorFlags, FlagRepeatCalcium)
	}
	// The strongest available signal that a diagnosis is wrong: every problem
	// improving while the animal itself declines.
	if ctx.ProblemImproving && ctx.AnimalWorsening {
		p.DirectorFlags = append(p.DirectorFlags, FlagDualTrajectory)
	}
	// A PPR animal still off feed continues on Supportive; it must not be closed
	// on the 3-day Fever schedule.
	if contains(p.Problems, "PPR") && (day >= 4 || contains(ctx.Open, "PPR")) && f.notEating() {
		p.DirectorFlags = append(p.DirectorFlags, FlagSupportiveContinues)
	}
}

// applyDirectorAlways adds the flags that hold on every run, including an
// out-of-scope or NAD run.
func (r *Register) applyDirectorAlways(p *Proposal, animal Animal, f Findings, ctx Context) {
	// The manager never closes a problem, and the engine never closes one.
	p.DirectorFlags = append(p.DirectorFlags, FlagManagerNeverCloses)

	if ctx.ShiftedOutDays == 7 {
		p.DirectorFlags = append(p.DirectorFlags, FlagMandatoryD7)
	}
	if ctx.Died {
		p.DirectorFlags = append(p.DirectorFlags, FlagDeathChain)
	}
	// An off-register diagnosis is a rule defect by definition, and the override
	// log is the improvement loop.
	if ctx.OffRegister {
		p.DirectorFlags = append(p.DirectorFlags, FlagOverride)
	}
	// A humane endpoint is never automatic. This raises it; the Director decides.
	if ctx.DownFollowups >= 3 {
		p.DirectorFlags = append(p.DirectorFlags, FlagHumaneEndpoint)
	}
	// GoatOS says she is not fresh, but the form shows colostrum or discharge.
	// One of the two records is wrong; the engine must not silently pick one and
	// open Metritis or milk fever on it.
	if animal.status() != "periparturient" && animal.status() != "pregnant" {
		if f.Lactation == "colostrum" ||
			f.Vulva == "discharge_no_smell" || f.Vulva == "discharge_bad_smell" || f.Vulva == "pus" {
			p.DirectorFlags = append(p.DirectorFlags, FlagContradictionStatus)
		}
	}
}

// applyShedPass counts similar presentations in the same shed. One sick animal
// is a case; three is an outbreak, and the difference is who gets woken up.
func (r *Register) applyShedPass(p *Proposal, ctx Context) {
	similar := ctx.shedSimilar()
	viral := contains(p.Problems, "PPR") || contains(p.Problems, "POX") || contains(p.Problems, "ORF")
	feverAndDiarrhea := (contains(p.Problems, "FEVER") || contains(p.Covered, "FEVER")) &&
		(contains(p.Problems, "DIARRHEA") || contains(p.Covered, "DIARRHEA"))

	switch {
	case similar >= 3:
		// Pinkeye clusters and is bacterial and common. It is still not
		// quarantine and still not a contagion event.
		if contains(p.Problems, "PINKEYE") && !viral {
			p.DirectorFlags = append(p.DirectorFlags, FlagPinkeyeCluster)
		} else {
			p.DirectorFlags = append(p.DirectorFlags, FlagContagionEvent)
		}
	case similar == 2 && (viral || feverAndDiarrhea ||
		contains(p.Problems, "POX") || contains(p.Problems, "DIARRHEA") ||
		contains(p.Problems, "FEVER") || contains(p.Problems, "PPR")):
		p.DirectorFlags = append(p.DirectorFlags, FlagContagionWatch)
	}
}

// decideHousing picks acuity and containment on two independent axes, most
// restrictive winning, plus the low-competition overlay.
//
// The overlay exists because EATING IS THE POINT: a fractured or off-feed animal
// loses at the trough. Fracture plus pinkeye is ward and low-competition, not
// quarantine — stacking independent axes rather than escalating one.
func (r *Register) decideHousing(problems []string, animal Animal, f Findings, d derived, emergencies []string) Housing {
	h := Housing{Acuity: AcuityHome, Containment: ContainmentHome, NoDueOvernight: true}

	crashing := f.down() || d.has("HYPOTHERMIA") || d.has("HIGH_FEVER") || d.correctedTent == "gt4"

	for _, id := range problems {
		if icuOnSight[id] {
			h.Acuity = AcuityICU
		}
	}
	if contains(problems, "PREG_TOX") {
		if f.down() {
			h.Acuity = AcuityICU
		} else if h.Acuity != AcuityICU {
			h.Acuity = AcuityWard
		}
	}
	if contains(problems, "ACIDOSIS") {
		if f.notEating() {
			h.Acuity = AcuityICU
		} else if h.Acuity != AcuityICU {
			h.Acuity = AcuityWard
		}
	}
	if contains(problems, "ANEMIA") && f.famacha() == 5 {
		h.Acuity = AcuityICU
	}
	if contains(problems, "JAUNDICE") && (f.notEating() || f.Activity == "weak") {
		h.Acuity = AcuityICU
	}
	if contains(problems, "MASTITIS") && (f.down() || d.has("HYPOTHERMIA")) {
		h.Acuity = AcuityICU
	}
	if contains(problems, "PPR") && crashing {
		h.Acuity = AcuityICU
	}
	if contains(problems, IDUndifferentiated) && (f.down() || d.has("HYPOTHERMIA") || d.correctedTent == "gt4") {
		h.Acuity = AcuityICU
	}
	// Above 106 is ICU. A standing 103.5-106 fever is WARD: putting every 104
	// in ICU drowns the evening shift and is how alarm fatigue starts.
	if d.has("HIGH_FEVER") || contains(emergencies, EmergencyCool) || contains(emergencies, EmergencyFluids) {
		h.Acuity = AcuityICU
	}
	if contains(problems, "PROLAPSE") && contains(emergencies, EmergencyUterineProlapse) {
		h.Acuity = AcuityICU
	}

	if h.Acuity == AcuityHome && len(problems) > 0 {
		h.Acuity = AcuityWard
	}

	for _, id := range problems {
		if r.quarantine[id] {
			h.Containment = ContainmentQuarantine
		}
	}

	h.LowCompetition = contains(problems, "FRACTURE") || f.notEating() || contains(problems, IDUndifferentiated)
	return h
}

// applyShiftLists turns housing into the two working shifts, 06:00-15:00 and
// 15:00-24:00. The farm is empty 00:00-06:00 and nothing is ever due then.
func (r *Register) applyShiftLists(p *Proposal) {
	switch {
	case p.Housing.Acuity == AcuityICU:
		p.Housing.MorningWalk = true
		p.Housing.EveningWalk = true
	case p.Housing.Acuity == AcuityWard || p.Housing.Containment == ContainmentQuarantine:
		// Ward is once a day. A standing, eating quarantine animal is also once
		// a day; only ICU earns the evening round.
		p.Housing.MorningWalk = true
		p.Housing.EveningWalk = false
	}
	if p.Housing.Acuity == AcuityICU && p.Housing.Containment == ContainmentQuarantine {
		p.Housing.MorningWalk = true
		p.Housing.EveningWalk = true
	}
	p.Housing.NoDueOvernight = true

	if p.Housing.Acuity == AcuityWard {
		p.DirectorFlags = append(p.DirectorFlags, FlagNoEveningWard)
	}
	if p.Housing.MorningWalk && p.Housing.EveningWalk {
		p.DirectorFlags = append(p.DirectorFlags, FlagBothShifts)
	}
	if p.Housing.Containment == ContainmentQuarantine && p.Housing.Acuity != AcuityICU {
		p.DirectorFlags = append(p.DirectorFlags, FlagOnceDayQuarantine)
	}
}

// applyDrugRules covers the vetoes and the club decision. Millilitres are
// deliberately out of v1 — merging overlapping drugs is safe, inventing a dose
// is not.
func (r *Register) applyDrugRules(p *Proposal, f Findings, d derived, ctx Context) {
	// Meloxicam is vetoed on obstruction and on hypothermia. An NSAID on a
	// blocked bladder or a cold animal is the high-risk half of the
	// treatment_risk axis.
	if contains(p.Emergencies, EmergencyNoUrine) {
		p.DirectorFlags = append(p.DirectorFlags, FlagTonight)
		p.NoMeloxicam = true
	}
	if contains(p.Problems, "CALCULI") {
		p.NoMeloxicam = true
	}
	if d.has("HYPOTHERMIA") {
		p.NoMeloxicam = true
	}
	// A 20:30 tube is treated now; the next look is the 06:00 ICU round, never
	// 02:00.
	if contains(p.Emergencies, EmergencyTube) && ctx.hour() >= 20 {
		p.DirectorFlags = append(p.DirectorFlags, FlagNextLook6am)
	}

	realProblems := 0
	for _, id := range p.Problems {
		if id != IDUndifferentiated {
			realProblems++
		}
	}
	if realProblems >= 2 {
		p.Club = true
	}
	if contains(p.Problems, "BLOAT") && contains(p.Problems, "ACIDOSIS") {
		p.Club = true
	}
}

// applyCourseRefs resolves each problem to the SOP card the manager treats from.
// The manager never picks the disease; they work the card.
func (r *Register) applyCourseRefs(p *Proposal, s kidState) {
	for _, id := range p.Problems {
		rule := r.Rule(id)
		switch {
		case id == IDUndifferentiated:
			// Supportive is daily and ongoing, NOT the 3-day Fever recipe. A kid
			// that has stopped drinking is worked from the refusal ladder
			// instead, which has rungs Supportive does not.
			p.SOP[id] = kidUndifferentiatedSOP(s)
			p.CourseType[id] = "Supportive"
		case rule != nil && rule.ExitType == "Supportive":
			p.SOP[id] = "Supportive"
			p.CourseType[id] = "Supportive"
		case rule != nil:
			ref := rule.SOPRef
			if ref == "" {
				ref = id
			}
			p.SOP[id] = ref
			p.CourseType[id] = rule.ExitType
			if id == "ORF" {
				p.DirectorFlags = append(p.DirectorFlags, FlagGloves)
			}
		default:
			p.SOP[id] = id
		}
	}
	if contains(p.Problems, "LAMINITIS") {
		p.DirectorFlags = append(p.DirectorFlags, FlagPullConcentrate)
	}
	if contains(p.Emergencies, EmergencyAcidosisNow) {
		p.DirectorFlags = append(p.DirectorFlags, FlagConcentrateOff)
	}
}

// removeString returns a new slice without value. It allocates rather than
// filtering in place: the caller's slice is shared with the match result, and
// reusing its backing array would corrupt a list something else still holds.
func removeString(list []string, value string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != value {
			out = append(out, v)
		}
	}
	return out
}
