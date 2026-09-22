package diagnosis

// The v1 observation form, transcribed from the Go form that preceded it.
//
// Every question, answer and emitted token here is a line-for-line reading of
// buildEvidence / deriveTokens / addKidEvidence and the option lists the Android form
// rendered. It is a SEED, not a specification: from the first publish the farm owns
// it, and the four registers this produces are simply what the farm starts from.
//
// Its fidelity is not a matter of care -- it is PROVEN. The 180-story acceptance
// catalogs ran against the Go form; they now run against this, story for story,
// through the authored path. A single divergence means the transcription is wrong,
// and the transcription is what every farm inherits.
//
// THE PIECES THAT ARE NOT HERE, and where they went instead:
//
//   - The temperature cuts are BANDS on the temp question, per class, because the
//     fever line differs between an adult (above 103.5degF) and a kid (103.5 and up)
//     by one tenth of a degree, and that tenth decides whether a kid is treated.
//   - The skin-tent correction is a CORRECTION, because written in Go it was keyed
//     on two question ids and a rename would have disarmed it silently.
//   - The refusal ladder is neither: it reads the animal's recent history, which no
//     answer carries. The form emits `milk:refused_this_feed` and the engine decides
//     what that means for this kid at this stage.

func yesNo(id, title string, emits ...string) Question {
	return Question{
		ID: id, Kind: QuestionChoice, Title: title,
		Options: []Option{
			{Value: "no", Label: "No"},
			{Value: "yes", Label: "Yes", Emits: emits},
		},
	}
}

func opt(value, label string, emits ...string) Option {
	return Option{Value: value, Label: label, Emits: emits}
}

// tempQuestion bands the reading for one class. Most severe FIRST: bands are
// first-match-wins, and Validate refuses any band an earlier one already covers.
func tempQuestion(kid bool) Question {
	fever := Band{Gt: f(103.5), Emits: []string{"FEVER"}}
	if kid {
		// A kid at exactly 103.5degF IS febrile; an adult at 103.5 is not.
		fever = Band{Gte: f(103.5), Emits: []string{"FEVER"}}
	}
	return Question{
		ID: "temp", Kind: QuestionNumber, Title: "Temperature", Unit: "degF",
		Section: "Vitals", Min: f(90), Max: f(112),
		Bands: []Band{
			{Gt: f(106), Emits: []string{"HIGH_FEVER", "FEVER"}},
			fever,
			{Lt: f(100), Emits: []string{"HYPOTHERMIA"}},
		},
	}
}

func f(v float64) *float64 { return &v }

// baseQuestions is the head-to-toe pass every animal gets, adult and kid alike.
// The Go form ran all of these for kids too; the kid slices ADD rows, they do not
// replace them.
func baseQuestions(kid bool) []Question {
	return []Question{
		tempQuestion(kid),

		{
			ID: "eating", Kind: QuestionMulti, Title: "Eating", Section: "Vitals",
			Options: []Option{
				opt("normal", "Eating normally"),
				// Off feed excludes eating, but NOT dry feed: a goat that has
				// stopped on concentrate and green feed may still pick at dry
				// fodder, and the farm records exactly that.
				{Value: "not_eating", Label: "Not eating", Emits: []string{"eating:not_eating"},
					ConflictsWith: []string{"normal", "concentrate", "green_feed"}},
				opt("concentrate", "Concentrate"),
				opt("green_feed", "Green feed"),
				opt("dry_feed", "Dry feed"),
			},
		},
		{
			ID: "activity", Kind: QuestionChoice, Title: "Activity", Section: "Vitals",
			Options: []Option{
				opt("standing", "Standing"),
				opt("down", "Down / cannot stand", "activity:not_able_to_stand"),
				opt("limping", "Limping", "activity:limping"),
				opt("back_leg_drag", "Dragging a back leg", "activity:back_leg_drag"),
				opt("front_knees", "Walking on front knees", "activity:front_leg_knees"),
				opt("weak", "Weak", "activity:weak"),
			},
		},

		{
			ID: "breathing", Kind: QuestionMulti, Title: "Breathing", Section: "Chest",
			Options: []Option{
				{Value: "normal", Label: "Normal",
					ConflictsWith: []string{"fast", "labored", "cough", "pant"}},
				opt("fast", "Fast", "breathing:fast"),
				opt("labored", "Laboured", "breathing:labored"),
				opt("cough", "Coughing", "breathing:cough"),
				opt("pant", "Panting", "breathing:pant"),
			},
		},
		yesNo("nasal", "Nasal discharge", "nasal_discharge"),

		{
			ID: "left_stomach", Kind: QuestionMulti, Title: "Left stomach", Section: "Rumen",
			Options: []Option{
				{Value: "normal", Label: "Normal", ConflictsWith: []string{"bloating", "acidosis"}},
				opt("bloating", "Bloated", "left_stomach:bloating"),
				opt("acidosis", "Water sound", "left_stomach:acidosis"),
			},
		},
		yesNo("frothy_mouth", "Froth at the mouth", "frothy_mouth"),
		{
			ID: "rumen_movement", Kind: QuestionChoice, Title: "Rumen movement", Section: "Rumen",
			Options: []Option{
				opt("felt", "Felt"),
				opt("not_felt", "Not felt", "rumen_movement:not_felt"),
			},
		},
		yesNo("diarrhea", "Diarrhoea", "diarrhea"),

		{
			ID: "skin_tent", Kind: QuestionChoice, Title: "Skin tent", Section: "Rumen",
			Hint: "Pinch the skin and count how long it takes to fall back.",
			Options: []Option{
				opt("lt2", "Under 2 seconds"),
				opt("s2_4s", "2 to 4 seconds", "skin_tent:2-4s"),
				opt("gt4", "Over 4 seconds", "skin_tent:>4s", "TENT_GT4"),
			},
		},

		{
			ID: "lactation", Kind: QuestionChoice, Title: "Lactation", Section: "Udder",
			OnlyIfSex: "F",
			Options: []Option{
				opt("no", "Not lactating", "lactation:no"),
				opt("milk", "Milk", "has_milk"),
				opt("colostrum", "Colostrum", "has_milk"),
				opt("water", "Watery", "has_milk"),
				opt("pus", "Pus", "lactation:pus", "has_milk"),
			},
		},
		{
			// Asked only where there is milk to test. This was a submit-time
			// rejection in the Go form; as a condition it simply never appears.
			ID: "cmt", Kind: QuestionChoice, Title: "CMT", Section: "Udder",
			OnlyIfSex: "F",
			OnlyIf:    &Condition{QuestionID: "lactation", In: []string{"milk", "colostrum", "water", "pus"}},
			Options: []Option{
				opt("pos", "Positive", "cmt:positive"),
				opt("neg", "Negative", "cmt:negative"),
			},
		},
		{
			ID: "udder", Kind: QuestionChoice, Title: "Udder", Section: "Udder",
			OnlyIfSex: "F",
			Options: []Option{
				opt("normal", "Normal"),
				opt("swollen_hard", "Swollen and hard", "udder:swollen_hard"),
				opt("rashes", "Rashes", "udder:rashes"),
				// An udder wound is a wound, even though it is recorded here.
				opt("wound", "Wound", "udder:wound", "wounds:present"),
				opt("lumps", "Lumps", "udder:lumps"),
			},
		},
		{
			ID: "vulva", Kind: QuestionChoice, Title: "Vulva", Section: "Udder",
			OnlyIfSex: "F",
			Options: []Option{
				opt("none", "Nothing abnormal"),
				opt("lochia_normal", "Normal lochia"),
				opt("discharge_bad_smell", "Foul-smelling discharge", "vulva:foul_smelling"),
				opt("pus", "Pus", "vulva:pus"),
				opt("prolapse", "Tissue protruding", "vulva:tissue_protruding"),
			},
		},

		{
			ID: "famacha", Kind: QuestionChoice, Title: "FAMACHA", Section: "Eyes",
			Options: []Option{
				opt("f1", "1"),
				opt("f2", "2"),
				opt("f3", "3", "famacha:3"),
				opt("f4", "4", "famacha:4"),
				opt("f5", "5", "famacha:5"),
			},
		},
		yesNo("yellow", "Yellow membranes", "famacha:yellow"),

		{
			ID: "straining", Kind: QuestionChoice, Title: "Passing urine", Section: "Urine",
			OnlyIfSex: "M",
			Options: []Option{
				opt("no", "Normal"),
				opt("straining", "Straining", "straining_urine:straining"),
				opt("no_urine", "No urine passed", "straining_urine:no_urine_passed"),
			},
		},
		yesNo("red_urine", "Red urine", "misc:red_urine"),
		yesNo("body_edema", "Swelling under the jaw or belly", "misc:body_edema"),

		yesNo("competition", "Pushed off feed by others", "misc:competition"),
		yesNo("stomach_inside", "Stomach drawn in", "misc:stomach_inside"),

		{
			ID: "mouth", Kind: QuestionChoice, Title: "Mouth", Section: "Head",
			Options: []Option{
				opt("normal", "Normal"),
				opt("orf_scabs", "Scabs on the lips", "mouth:orf_scabs"),
			},
		},
		{
			ID: "eyes", Kind: QuestionMulti, Title: "Eyes", Section: "Eyes",
			Options: []Option{
				{Value: "normal", Label: "Normal", ConflictsWith: []string{"red", "cloudy", "discharge"}},
				opt("red", "Red", "eyes:red"),
				opt("cloudy", "Cloudy", "eyes:cloudy"),
				opt("discharge", "Discharge", "eyes:discharge"),
			},
		},
		yesNo("locked_jaw", "Locked jaw", "locked_jaw"),
		{
			ID: "neuro", Kind: QuestionMulti, Title: "Nervous signs", Section: "Head",
			Options: []Option{
				{Value: "none", Label: "None",
					ConflictsWith: []string{"circling", "head_tilt", "star_gazing", "blind", "tremors", "ataxia", "seizure"}},
				opt("circling", "Circling", "neuro:circling"),
				opt("head_tilt", "Head tilt", "neuro:head_tilt"),
				opt("star_gazing", "Star gazing", "neuro:star_gazing"),
				opt("blind", "Blind", "neuro:blind"),
				opt("tremors", "Tremors", "neuro:tremors"),
				opt("ataxia", "Unsteady", "neuro:ataxia"),
				// The SECOND repair the authored form surfaced. Every kid register
				// lists `neuro:seizure` in its vocabulary and NEURO matches on it
				// pathognomonically -- but the form offered no way to tick it, so a
				// fitting kid could only ever be recorded as something else. Adults
				// do not read it and will report it as declared-but-unread, which is
				// the honest state rather than a hidden asymmetry.
				opt("seizure", "Fitting", "neuro:seizure"),
			},
		},

		{
			ID: "rash_character", Kind: QuestionChoice, Title: "Rashes", Section: "Skin",
			Options: []Option{
				opt("none", "None"),
				opt("flat_itchy", "Flat and itchy", "rash_character:flat_itchy", "rashes:present"),
				opt("nodular", "Raised nodules", "rash_character:nodular"),
			},
		},
		{
			// THREE locations, not a yes/no, and this is a REPAIR rather than a
			// transcription. The Go form carried one boolean that emitted
			// `skin_coat:hairloss_body`, while the SKIN rule has always listed
			// hairloss on the body, the NECK and the LEGS as three separate probable
			// clauses -- so two of them could never fire, and hair loss on a goat's
			// neck could not be recorded at all. The authored form's own validation
			// is what surfaced it: a rule naming a token no answer emits is a
			// refusal, and these two had been sitting unreachable in a committed
			// file. A story that ticked the old boolean still emits exactly
			// `skin_coat:hairloss_body`, so nothing already diagnosed moves.
			ID: "hairloss", Kind: QuestionMulti, Title: "Hair loss", Section: "Skin",
			Options: []Option{
				{Value: "no", Label: "None", ConflictsWith: []string{"body", "neck", "legs"}},
				opt("body", "On the body", "skin_coat:hairloss_body"),
				opt("neck", "On the neck", "skin_coat:hairloss_neck"),
				opt("legs", "On the legs", "skin_coat:hairloss_legs"),
			},
		},

		{
			ID: "leg", Kind: QuestionChoice, Title: "Legs and feet", Section: "Legs",
			Options: []Option{
				opt("normal", "Normal", "leg:normal"),
				opt("arthritis", "Swollen joints", "leg:arthritis"),
				opt("fracture", "Fracture", "leg:fracture"),
				opt("foot_rot", "Foot rot", "leg:foot_rot"),
			},
		},
		{
			ID: "lumps", Kind: QuestionChoice, Title: "Lumps", Section: "Skin",
			Options: []Option{
				opt("no", "None"),
				opt("neck", "On the neck", "lumps:neck"),
				opt("body", "On the body", "lumps:body"),
			},
		},
		{
			ID: "wounds", Kind: QuestionMulti, Title: "Wounds", Section: "Skin",
			Options: []Option{
				{Value: "no", Label: "None",
					ConflictsWith: []string{"horn", "neck", "body", "legs"}},
				opt("horn", "Horn", "wounds:horn", "wounds:present"),
				opt("neck", "Neck", "wounds:neck", "wounds:present"),
				opt("body", "Body", "wounds:body", "wounds:present"),
				opt("legs", "Legs", "wounds:legs", "wounds:present"),
			},
		},

		yesNo("flystrike", "Maggots in a wound", "flystrike"),
		yesNo("eartag_flystrike", "Maggots at the ear tag", "eartag:flystrike"),
		yesNo("eartag_wound", "Ear tag wound", "eartag:wound", "wounds:present"),
		yesNo("ticks", "Ticks", "skin_coat:ticks"),
	}
}

// kidQuestions are the rows only a kid is asked. They ADD to the base pass.
func kidQuestions(class string) []Question {
	qs := []Question{
		{
			// A treatment gate rather than a symptom: a kid that sucks can be given
			// milk by mouth, and one that cannot must never be.
			ID: "suckle", Kind: QuestionChoice, Title: "Suckle reflex", Section: "Kid",
			Hint: "Put a clean finger in the mouth.",
			Options: []Option{
				opt("present", "Sucks", "suckle:present"),
				opt("absent", "Does not suck", "suckle:absent"),
			},
		},
		{
			// Deliberately non-specific: dull alone names no disease.
			ID: "responsiveness", Kind: QuestionChoice, Title: "Responsiveness", Section: "Kid",
			Options: []Option{
				opt("alert", "Alert", "responsiveness:alert"),
				opt("dull", "Dull", "responsiveness:dull"),
				opt("unresponsive", "Unresponsive", "responsiveness:unresponsive"),
			},
		},
	}

	if class == ClassKidMilk {
		qs = append(qs,
			Question{
				// Milk kids only: the navel has closed and healed by weaning.
				ID: "navel", Kind: QuestionChoice, Title: "Navel", Section: "Kid",
				Options: []Option{
					opt("normal", "Dry and closed"),
					opt("wet", "Wet", "navel:wet"),
					opt("swollen", "Swollen", "navel:swollen"),
					opt("painful", "Painful", "navel:painful"),
				},
			},
			Question{
				// The drop test, and the whole point of the milk-kid form: from about
				// 20 cm a well kid lands like Spider-Man. It is the only way floppy
				// kid is caught while the animal is still standing and cheap to treat.
				//
				// Asked only while the kid is ON ITS FEET. Dropping a recumbent kid to
				// grade its landing is the one thing this test must never cause, and
				// as a condition that is now unaskable rather than merely rejected.
				ID: "landing", Kind: QuestionChoice, Title: "Drop test", Section: "Kid",
				Hint:   "Hold about 20 cm above the bedding and let go.",
				OnlyIf: &Condition{QuestionID: "activity", In: []string{"standing", "weak", "limping"}},
				Options: []Option{
					opt("spiderman", "Lands square", "landing:spiderman"),
					opt("barely", "Barely stays up", "landing:barely"),
					opt("falls", "Falls", "landing:falls"),
				},
			},
		)
	}

	if class == ClassKidMilk || class == ClassKidWeaning {
		qs = append(qs, Question{
			// What the manager SAW at this feed. What it MEANS depends on the kid's
			// recent refusals and its stage, which is the engine's job -- see
			// milkProblem. The token, not this question's id, is what the engine
			// reads, so the question may be renamed freely.
			ID: "milk_intake", Kind: QuestionMulti, Title: "Milk taken", Section: "Kid",
			Options: []Option{
				{Value: "normal", Label: "Drank normally",
					ConflictsWith: []string{"not_drinking"}},
				opt("reduced", "Drank less", "milk:reduced"),
				opt("not_drinking", "Did not drink", "milk:refused_this_feed"),
			},
		})
	}

	return qs
}

// seedCorrections is the one rule that reads a finding in light of another.
//
// A goat with its stomach drawn in reads a skin tent one band worse than it is, so a
// tent over 4 seconds beside a drawn-in stomach is recorded as 2-4 seconds. It is
// data rather than Go because in Go it was keyed on two question ids, which made
// those ids load-bearing: renaming `skin_tent` would have silently under-read
// dehydration with no error anywhere.
func seedCorrections() []Correction {
	return []Correction{{
		ID:     "tent_read_with_stomach_drawn_in",
		When:   []string{"misc:stomach_inside", "skin_tent:>4s"},
		Remove: []string{"skin_tent:>4s", "TENT_GT4"},
		Add:    []string{"skin_tent:2-4s"},
		Note:   "A drawn-in stomach makes the skin tent read one band worse than the animal is.",
	}}
}

// SeedQuestions is the v1 form for one animal class.
func SeedQuestions(class string) []Question {
	kid := class != ClassAdult
	qs := baseQuestions(kid)
	if kid {
		qs = append(qs, kidQuestions(class)...)
	}
	return qs
}
