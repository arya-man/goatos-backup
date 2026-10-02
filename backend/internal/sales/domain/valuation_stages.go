package domain

import (
	"regexp"
	"strings"
)

// THE STAGES THE HERD IS VALUED IN ARE AUTHORED (maintainer instruction 2026-09-24: "even new
// stage should also be configurable don't hardcode ... we have warmup with animals i need to add
// them ... once i add it and define price, farm values should change and card should come").
//
// Until now the valuation knew six stages because six stages were written into a SQL CASE, and a
// stage the farm actually keeps animals in -- Warmup, 58 live kids on the day this was written --
// could not be valued without a deploy. It was not valued at nothing by decision; it was valued at
// nothing because nobody could tell the software it existed.
//
// A stage is now a row the farm writes on Sales Config: what it is called, which entries of the
// herd's own stage register it covers, and (through its two bucket rows) what a female and a male
// in it are carried at. Adding Warmup is picking it from that register and typing two prices; the
// card appears because the cards ARE these rows.
//
// WHAT IT MATCHES ON IS THE REGISTER, NOT A GUESS. A stage row names management-stage values as
// `animal_stage_lookup` spells them, and an animal is matched on its own management stage or, when
// the register lost it, its milk cohort -- the same two columns the CASE read, compared through the
// same normalizer (upper, strip non-alphanumerics) the herd's import has needed since 000166,
// because this farm genuinely carries both `ICU- kid` and `ICU-Kid`.
//
// AN ANIMAL IN NO NAMED STAGE IS NOT VALUED, AND IS SHOWN. That was already true and is the
// property that makes this safe to author: a stage nobody has placed does not quietly fall into
// the nearest bucket and move the farm's value by an amount nobody decided -- it stands in the
// not-valued list with its own name and its head count, which is exactly how Warmup asked to be
// added. Do not add a fallback bucket.
//
// TWO STAGES MAY NOT CLAIM ONE REGISTER ENTRY. Overlapping matches are refused at the write rather
// than resolved by display order, because an animal silently valued by whichever row sorts first is
// a farm value that changes when someone reorders the screen.

// ValuationStage is one authored stage: the words for it, the register entries it covers, and its
// place on the screen. Its four rates live in Buckets, keyed `<stage>_<species>_<gender>`.
type ValuationStage struct {
	// Stage is the stable key. It is derived from the label ONCE, when the row is added, and never
	// again: the bucket keys, the stored figures and every audit row are keyed on it, so a rename
	// must move none of them.
	Stage        string   `json:"stage"`
	Label        string   `json:"label"`
	DisplayOrder int      `json:"display_order"`
	Matches      []string `json:"matches"`
}

// A key derived from a label is lower case; the seeded K0..K3 keys are the register's own capitals
// and the stored rows are keyed on them, so the pattern admits both rather than renaming what is
// already written down.
var valuationStageKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_]{0,39}$`)

// NormalizeStageMatch is the ONE comparison both sides use: this file when it refuses two stages
// that claim one register entry, and the valuation SQL when it files an animal into a stage.
// Changing it here without changing `stageMatchNormalizedSQL` would let a write pass that the read
// then files somewhere else.
func NormalizeStageMatch(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValuationStageKeyFromLabel derives the key for a stage being ADDED.
func ValuationStageKeyFromLabel(label string) string {
	var b strings.Builder
	last := byte('_')
	for _, r := range strings.ToLower(strings.TrimSpace(label)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			last = byte(r)
		case last != '_' && b.Len() > 0:
			b.WriteByte('_')
			last = '_'
		}
	}
	return strings.Trim(b.String(), "_")
}

// BucketKeysForStages is the closed set of bucket keys an authored stage list implies: every stage
// against every species and gender, in screen order. The SET is closed and checked; the farm
// decides how long it is.
func BucketKeysForStages(stages []ValuationStage) []string {
	out := make([]string, 0, len(stages)*len(ValuationSpecies)*len(ValuationGenders))
	for _, s := range stages {
		for _, sp := range ValuationSpecies {
			for _, g := range ValuationGenders {
				out = append(out, ValuationBucketKey(s.Stage, sp.Key, g.Key))
			}
		}
	}
	return out
}

// BucketLabel is the words on the card, composed from the stage's own label, the species and the
// gender, so a stage renamed on the screen renames all its cards and nothing else is kept in step.
func BucketLabel(stageLabel, speciesLabel, genderLabel string) string {
	return stageLabel + " · " + speciesLabel + " · " + genderLabel
}

// NormalizeValuationAssumptions assigns a key to every stage being added, orders both lists the way
// the screen sent them, and composes each bucket's label from its stage. It runs before
// ValidateValuationAssumptions and is the only place a key is derived.
func NormalizeValuationAssumptions(v *ValuationAssumptions) {
	byStage := map[string]ValuationStage{}
	for i := range v.Stages {
		s := &v.Stages[i]
		s.Label = strings.TrimSpace(s.Label)
		s.Stage = strings.TrimSpace(s.Stage)
		if s.Stage == "" {
			s.Stage = ValuationStageKeyFromLabel(s.Label)
		}
		s.DisplayOrder = i + 1
		cleaned := make([]string, 0, len(s.Matches))
		for _, m := range s.Matches {
			if m = strings.TrimSpace(m); m != "" {
				cleaned = append(cleaned, m)
			}
		}
		s.Matches = cleaned
		byStage[s.Stage] = *s
	}
	for i := range v.Buckets {
		b := &v.Buckets[i]
		b.Bucket = strings.TrimSpace(b.Bucket)
		b.DisplayOrder = i + 1
		stage, species, gender, ok := splitBucketKey(b.Bucket)
		if !ok {
			continue
		}
		if s, found := byStage[stage]; found {
			b.Label = BucketLabel(s.Label, species.Label, gender.Label)
		}
	}
}

func splitBucketKey(key string) (string, struct{ Key, Label string }, struct{ Key, Label string }, bool) {
	none := struct{ Key, Label string }{}
	for _, sp := range ValuationSpecies {
		for _, g := range ValuationGenders {
			if suffix := "_" + sp.Key + "_" + g.Key; strings.HasSuffix(key, suffix) {
				return strings.TrimSuffix(key, suffix), sp, g, true
			}
		}
	}
	return "", none, none, false
}

// validateValuationStages is the stage half of ValidateValuationAssumptions.
func validateValuationStages(v ValuationAssumptions, bad func(string, ...any) error) error {
	if len(v.Stages) == 0 {
		return bad("stages: at least one stage is needed, or nothing in the herd can be valued")
	}
	seenKey := map[string]int{}
	seenMatch := map[string]string{}
	for i, s := range v.Stages {
		if s.Label == "" {
			return bad("stages[%d].label: required", i)
		}
		if len(s.Label) > 60 {
			return bad("stages[%d].label: keep it under 60 characters", i)
		}
		if !valuationStageKeyPattern.MatchString(s.Stage) {
			return bad("stages[%d].stage: %q cannot be used as a stage name", i, s.Label)
		}
		if prev, dup := seenKey[s.Stage]; dup {
			return bad("stages[%d].label: %q is already stage %d", i, s.Label, prev+1)
		}
		seenKey[s.Stage] = i
		if len(s.Matches) == 0 {
			return bad("stages[%d].matches: pick at least one stage from the register, or no animal can land in %q", i, s.Label)
		}
		for j, m := range s.Matches {
			norm := NormalizeStageMatch(m)
			if norm == "" {
				return bad("stages[%d].matches[%d]: blank", i, j)
			}
			if owner, taken := seenMatch[norm]; taken {
				return bad("stages[%d].matches[%d]: %q is already valued as %q; one register stage belongs to one valuation stage", i, j, m, owner)
			}
			seenMatch[norm] = s.Label
		}
	}
	return nil
}

// StageRegisterEntry is one row of the herd's OWN stage register (`animal_stage_lookup`) offered to
// the screen to pick from, with the animals standing in it right now. The head count is what makes
// the screen answer the question the farm actually asks -- which stages have animals and no price
// on them -- rather than presenting nineteen equal-looking names.
type StageRegisterEntry struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	LiveAnimals int    `json:"live_animals"`
}

// UpgradeLegacyValuationBuckets rewrites a bucket list stored before the species split
// (`<stage>_<gender>`, pre-000470) into species keys, copying each figure to both species. A list
// already keyed by species is left untouched. Labels are recomposed from the stages.
func UpgradeLegacyValuationBuckets(v *ValuationAssumptions) {
	legacy := false
	for _, b := range v.Buckets {
		if _, _, _, ok := splitBucketKey(b.Bucket); !ok {
			legacy = true
			break
		}
	}
	if !legacy {
		return
	}
	stageLabel := map[string]string{}
	for _, s := range v.Stages {
		stageLabel[s.Stage] = s.Label
	}
	out := make([]ValuationBucketRate, 0, len(v.Buckets)*len(ValuationSpecies))
	for _, s := range v.Stages {
		for _, sp := range ValuationSpecies {
			for _, g := range ValuationGenders {
				for _, b := range v.Buckets {
					if b.Bucket != s.Stage+"_"+g.Key && b.Bucket != ValuationBucketKey(s.Stage, sp.Key, g.Key) {
						continue
					}
					out = append(out, ValuationBucketRate{
						Bucket:        ValuationBucketKey(s.Stage, sp.Key, g.Key),
						Label:         BucketLabel(stageLabel[s.Stage], sp.Label, g.Label),
						FixedWeightKg: b.FixedWeightKg,
						PricePerKg:    b.PricePerKg,
						DisplayOrder:  len(out) + 1,
					})
					break
				}
			}
		}
	}
	v.Buckets = out
}
