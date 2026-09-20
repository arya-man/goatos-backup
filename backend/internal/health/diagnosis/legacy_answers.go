package diagnosis

import "strconv"

// LegacyAnswers maps an observation recorded on the PRE-AUTHORED form onto the
// authored form's answers.
//
// It exists for two readers. One is the acceptance catalogs, which are the proof that
// authoring the form changed no clinical behaviour. The other is every phone still
// running an APK built against the old wire: a farm does not update all its handsets
// the day a register is published, and an observation already taken must not become
// unreadable because the vet edited the form.
//
// THE ONE RULE THAT MAKES IT FAITHFUL: a field the old form did not SET is left
// ABSENT here, never defaulted. The old form's zero values were not answers -- an
// omitted `leg` emitted nothing while `leg: normal` emitted `leg:normal`, and an
// omitted temperature read as 102degF emitted nothing because no band covers 102. Any
// default invented here would put a token into an old observation that the manager
// never recorded, and the unexplained-findings channel reads EVERY token in the
// evidence set. A single invented one surfaces to the Director as a finding nobody saw.
func LegacyAnswers(animal Animal, f Findings) Answers {
	ans := Answers{}

	put := func(id string, values ...string) {
		if len(values) == 0 {
			return
		}
		ans[id] = Answer{Values: values}
	}
	flag := func(id string, set bool) {
		if set {
			ans[id] = Answer{Values: []string{"yes"}}
		}
	}

	if f.Temp != nil {
		ans["temp"] = Answer{Number: f.Temp}
	}

	put("eating", f.Eating...)
	put("activity", nonEmpty(f.Activity)...)
	put("breathing", f.Breathing...)
	flag("nasal", f.Nasal)

	put("left_stomach", f.LeftStomach...)
	flag("frothy_mouth", f.FrothyMouth)
	put("rumen_movement", nonEmpty(f.RumenMovement)...)
	flag("diarrhea", f.Diarrhea.Set)

	// The old wire spelled this band three ways, including one with an en dash.
	switch f.SkinTent {
	case "gt4":
		put("skin_tent", "gt4")
	case "2-4", "2-4s", "2–4s":
		put("skin_tent", "s2_4s")
	case "lt2":
		put("skin_tent", "lt2")
	}

	put("cmt", nonEmpty(f.CMT)...)
	put("lactation", nonEmpty(f.Lactation)...)
	put("udder", nonEmpty(f.Udder)...)
	put("vulva", nonEmpty(f.Vulva)...)

	if f.Famacha != nil {
		put("famacha", "f"+strconv.Itoa(*f.Famacha))
	}
	flag("yellow", f.Yellow)

	put("straining", nonEmpty(f.Straining)...)
	flag("red_urine", f.RedUrine)
	flag("body_edema", f.BodyEdema)
	flag("competition", f.Competition)
	flag("stomach_inside", f.StomachInside)

	put("mouth", nonEmpty(f.Mouth)...)
	put("eyes", f.Eyes...)
	flag("locked_jaw", f.LockedJaw)
	put("neuro", f.Neuro...)
	put("rash_character", nonEmpty(f.RashCharacter)...)
	// The old form had ONE hairloss boolean and emitted body only. The authored form
	// asks where, so an old tick becomes exactly the answer it used to mean -- and
	// neck and legs, which the SKIN rule has always read and no form could record,
	// become answerable for the first time.
	if f.Hairloss {
		put("hairloss", "body")
	}

	put("leg", nonEmpty(f.Leg)...)
	put("lumps", nonEmpty(f.Lumps)...)
	put("wounds", f.Wounds...)

	flag("flystrike", f.Flystrike)
	flag("eartag_flystrike", f.EartagFlystrike)
	flag("eartag_wound", f.EartagWound)
	flag("ticks", f.Ticks)

	if animal.isKid() {
		put("suckle", nonEmpty(f.Suckle)...)
		put("responsiveness", nonEmpty(f.Responsiveness)...)
		put("navel", nonEmpty(f.Navel)...)
		if f.Landing != "" && f.Landing != NotApplicable {
			put("landing", f.Landing)
		}
		// The bar reading only. What it MEANS is the refusal ladder's answer, and
		// that runs from the animal's history rather than from this tick.
		put("milk_intake", f.MilkIntake...)
	}

	return ans
}

func nonEmpty(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}
