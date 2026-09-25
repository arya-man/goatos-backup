package domain

import (
	"fmt"
	"sort"
	"strings"
)

// CAUSE OF DEATH. The operator answers one question on the death form: was this a NORMAL
// death, or was it DUE TO DISEASE?
//
//	normal   the written account, exactly as every death has carried since the workflow
//	         was built. No disease is claimed, because none was established.
//	disease  a coded cause, chosen from the diagnosis register or from the diseases authored
//	         in Health Config (2026-09-25), plus an optional note.
//
// This file owns the VOCABULARY and the shape; it performs no I/O and knows nothing about
// how a death is stored. The register itself lives in health/diagnosis as embedded YAML
// validated at process start, and is passed in — deliberately not duplicated here, because
// two copies of a clinical rule list drift and the register is already the one thing the
// engine diagnoses from.

const (
	// DeathCauseKindRegisterRule is the DIAGNOSIS rule the engine names: 'MASTITIS',
	// 'BLOAT'. 34 adult rules and 27 per kid class. This is the precise vocabulary, it is
	// what `health_cases.register_rule_id` stores, and it is what the Health Analytics
	// disease board counts — so a cause of death recorded this way can be read straight
	// against the incidence board with no translation.
	DeathCauseKindRegisterRule = "register_rule"
	// DeathCauseKindDiseaseKey is a disease authored in HEALTH CONFIG, by its stable
	// disease_key: 'dog_bite', 'horn_damage'. Two things carry it: a pre-engine,
	// direct-pick case's treatment card (resolved from the case itself), and -- since the
	// maintainer decision of 2026-09-25 -- a death the operator files under an ACTIVE Health
	// Config disease the register does not already name. It can be LOSSY where the card is
	// shared (PPR, POX and UNDIFFERENTIATED all route to 'supportive'), which is why a card a
	// register diagnosis opens is never offered under this kind and the kind travels with the
	// key everywhere.
	DeathCauseKindDiseaseKey = "disease_key"
)

// DeathCause is a coded cause of death. The zero value means a NORMAL death — no disease
// was established — which is a real, common and complete answer, not missing data.
type DeathCause struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
}

// IsZero reports a normal death.
func (c DeathCause) IsZero() bool { return strings.TrimSpace(c.Key) == "" }

// DeathCauseOption is one selectable disease in the death form's searchable dropdown.
type DeathCauseOption struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	// Label is what the operator reads. It is the register's own `sop_ref` — the farm's
	// word for the disease ('Foot rot', 'Udder edema') rather than the rule id, which is a
	// machine key and must never reach a screen.
	Label string `json:"label"`
	// AnimalClasses are the register classes that carry this rule: "adult", "kid_milk",
	// "kid_weaning", "kid_fattening". The client may narrow the list to the animal in
	// front of the operator; it is never used to REJECT a choice, because an animal can
	// change class between the diagnosis and the death.
	AnimalClasses []string `json:"animal_classes"`
}

// DeathCauseCatalog is the whole searchable vocabulary, one entry per distinct rule across
// every register, sorted by label so the dropdown reads alphabetically without the client
// re-sorting it into a different order on a different locale.
type DeathCauseCatalog struct {
	Options []DeathCauseOption `json:"options"`
	// RegisterVersions pins which rule tables produced this list, so an operator's
	// selection stays interpretable after the registers are edited.
	RegisterVersions []string `json:"register_versions"`

	// acceptedDiseaseKeys are the Health Config diseases a NEW death may name under the
	// disease_key kind. It is wider than the disease_key options on screen: a disease that is
	// folded into its register diagnosis (Mastitis the card is MASTITIS the rule) is not
	// listed twice, but a client naming the card key is still naming an active disease.
	// Never serialised -- the list the client reads is Options.
	acceptedDiseaseKeys map[string]struct{}
}

// AuthoredDisease is one disease authored on Health Config's treatment tab
// (`POST /health-config/diseases`), keyed by its stable disease_key.
//
// ACTIVE means it has a PUBLISHED treatment version today. A disease that was never published
// (a bare draft) or whose every version is retired is not active: a new death cannot be filed
// under it, but a death ALREADY filed under it keeps its name on every read.
type AuthoredDisease struct {
	Key    string
	Label  string
	Active bool
}

// DeathCauseTenantSources is what the tenant's own Health Config contributes to the death
// form: the problem rules of its PUBLISHED diagnosis registers (with the version label of
// each) and every disease authored on the treatment tab.
type DeathCauseTenantSources struct {
	RegisterRules    []RegisterRule
	RegisterVersions []string
	Diseases         []AuthoredDisease
}

// RegisterRule is the narrow view of a diagnosis rule this package needs. The diagnosis
// package's own Rule satisfies it structurally through BuildDeathCauseCatalog's caller,
// which keeps health/domain free of a dependency on the engine.
type RegisterRule struct {
	ID    string
	Label string
	Class string
	// Treats is the treatment protocol (a Health Config disease key) this diagnosis opens,
	// or "" for a field action. It is how an authored disease that is merely the COURSE of
	// a register diagnosis is recognised as that diagnosis rather than listed twice.
	Treats string
}

// BuildDeathCauseCatalog folds the loaded registers into one searchable list.
//
// A rule id appears ONCE even though it is carried by several registers — DIARRHEA is in
// all four — with its classes collected onto the single entry. Listing it four times would
// give the operator four identical rows to choose between.
//
// A rule with no label is SKIPPED rather than shown under its raw id: 'FOOT_ROT' is a
// machine key, and putting it in front of an operator is the copy-firewall break this
// product bans. Every rule in every shipped register carries one, so a skip means the
// register itself lost a label and the omission is the signal.
func BuildDeathCauseCatalog(rules []RegisterRule, registerVersions []string) DeathCauseCatalog {
	byID := make(map[string]*DeathCauseOption, len(rules))
	order := make([]string, 0, len(rules))
	for _, rule := range rules {
		id := strings.TrimSpace(rule.ID)
		label := strings.TrimSpace(rule.Label)
		if id == "" || label == "" {
			continue
		}
		existing, ok := byID[id]
		if !ok {
			byID[id] = &DeathCauseOption{
				Key:           id,
				Kind:          DeathCauseKindRegisterRule,
				Label:         label,
				AnimalClasses: appendClass(nil, rule.Class),
			}
			order = append(order, id)
			continue
		}
		existing.AnimalClasses = appendClass(existing.AnimalClasses, rule.Class)
	}

	options := make([]DeathCauseOption, 0, len(order))
	for _, id := range order {
		options = append(options, *byID[id])
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Label == options[j].Label {
			return options[i].Key < options[j].Key
		}
		return options[i].Label < options[j].Label
	})

	versions := append([]string(nil), registerVersions...)
	sort.Strings(versions)
	return DeathCauseCatalog{Options: options, RegisterVersions: versions}
}

func appendClass(classes []string, class string) []string {
	class = strings.TrimSpace(class)
	if class == "" {
		return classes
	}
	for _, existing := range classes {
		if existing == class {
			return classes
		}
	}
	return append(classes, class)
}

// WithAuthoredDiseases folds the tenant's Health Config diseases into a register-built catalog
// (maintainer decision 2026-09-25: the cause-of-death list is the built-in diseases PLUS every
// disease authored in Health Config).
//
// Only ACTIVE diseases are offered or accepted. An active disease is LISTED under its own
// disease_key unless it is already the same disease as a register diagnosis, in which case the
// register entry stands for both -- one disease, one row, and the precise register key is the
// one the incidence board counts. "The same disease" is decided three ways, each of which the
// shipped data needs:
//
//   - the same farm word ("Mastitis" and "Mastitis");
//   - the same key once case and separators are ignored (foot_rot and FOOT_ROT);
//   - the disease is the treatment course a register diagnosis opens (bloating is BLOAT's
//     course, pregnancy_toxemia is PREG_TOX's), which is how two names written by two people
//     for one illness are recognised as one.
//
// A folded disease is still ACCEPTED under its own key: it is an active disease, and refusing
// a client that names it would be refusing a true answer.
func WithAuthoredDiseases(catalog DeathCauseCatalog, rules []RegisterRule, diseases []AuthoredDisease) DeathCauseCatalog {
	labels := make(map[string]struct{}, len(catalog.Options))
	keys := make(map[string]struct{}, len(catalog.Options))
	for _, option := range catalog.Options {
		labels[foldDiseaseName(option.Label)] = struct{}{}
		keys[foldDiseaseName(option.Key)] = struct{}{}
	}
	courses := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if treats := strings.TrimSpace(rule.Treats); treats != "" {
			courses[treats] = struct{}{}
		}
	}

	out := DeathCauseCatalog{
		Options:             append([]DeathCauseOption(nil), catalog.Options...),
		RegisterVersions:    append([]string(nil), catalog.RegisterVersions...),
		acceptedDiseaseKeys: make(map[string]struct{}, len(diseases)),
	}
	seen := make(map[string]struct{}, len(diseases))
	for _, disease := range diseases {
		key := strings.TrimSpace(disease.Key)
		label := strings.TrimSpace(disease.Label)
		if !disease.Active || key == "" || label == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out.acceptedDiseaseKeys[key] = struct{}{}
		if _, same := labels[foldDiseaseName(label)]; same {
			continue
		}
		if _, same := keys[foldDiseaseName(key)]; same {
			continue
		}
		if _, same := courses[key]; same {
			continue
		}
		labels[foldDiseaseName(label)] = struct{}{}
		out.Options = append(out.Options, DeathCauseOption{
			Key:   key,
			Kind:  DeathCauseKindDiseaseKey,
			Label: label,
			// A treatment disease belongs to no diagnosis register class; empty means the
			// client must not narrow it away for any animal.
			AnimalClasses: []string{},
		})
	}
	sort.Slice(out.Options, func(i, j int) bool {
		if out.Options[i].Label == out.Options[j].Label {
			return out.Options[i].Key < out.Options[j].Key
		}
		return out.Options[i].Label < out.Options[j].Label
	})
	return out
}

// foldDiseaseName reduces a name or key to letters and digits, lower case, so "Foot rot",
// "foot_rot" and "FOOT_ROT" compare equal.
func foldDiseaseName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ValidateDeathCause checks a submitted cause against the catalog.
//
// AN UNKNOWN CAUSE IS REJECTED, never stored as typed. The whole value of a coded cause is
// that it groups: one death filed under 'MASTITIS' and another under 'Mastitus' are two
// diseases on the board and one in the barn. A client that cannot find the disease in the
// list has to record a normal death and say so in the note, which is honest, rather than
// invent a key nothing else in the product knows.
//
// An EMPTY cause is valid and means a normal death.
func ValidateDeathCause(cause DeathCause, catalog DeathCauseCatalog) error {
	key := strings.TrimSpace(cause.Key)
	kind := strings.TrimSpace(cause.Kind)
	if key == "" && kind == "" {
		return nil
	}
	// Both or neither, matching the database constraint. A kind alone names nothing; a key
	// alone cannot be read, because the same string can live in both vocabularies.
	if key == "" || kind == "" {
		return fmt.Errorf("death cause needs both a disease and its kind, or neither")
	}
	switch kind {
	case DeathCauseKindRegisterRule:
		for _, option := range catalog.Options {
			if option.Key == key {
				return nil
			}
		}
		return fmt.Errorf("%q is not a disease in the diagnosis register", key)
	case DeathCauseKindDiseaseKey:
		// A disease authored in Health Config (maintainer decision 2026-09-25). Only an
		// ACTIVE one -- published today -- may be named on a NEW death; a disease that was
		// retired stays readable on the deaths already filed under it, because nothing here
		// re-validates a stored cause.
		if _, ok := catalog.acceptedDiseaseKeys[key]; ok {
			return nil
		}
		return fmt.Errorf("%q is not an active disease in Health config", key)
	default:
		return fmt.Errorf("death cause kind must be %s or %s", DeathCauseKindRegisterRule, DeathCauseKindDiseaseKey)
	}
}

// DeathCauseFromCase resolves the coded cause a health case can supply.
//
// The register rule wins where the case has one. A pre-engine case carries only the
// treatment card it was opened against, so it falls back to that — with the kind saying
// so, because the card is many-to-one across diseases and a reader must be able to tell
// the precise answer from the approximate one.
//
// A case with neither can supply no coded cause at all; that death is recorded normally.
func DeathCauseFromCase(registerRuleID, diseaseKey string) DeathCause {
	if rule := strings.TrimSpace(registerRuleID); rule != "" {
		return DeathCause{Key: rule, Kind: DeathCauseKindRegisterRule}
	}
	if card := strings.TrimSpace(diseaseKey); card != "" {
		return DeathCause{Key: card, Kind: DeathCauseKindDiseaseKey}
	}
	return DeathCause{}
}

// deathCauseLabels is the farm's word for each diagnosis rule.
//
// WHY THIS MAP EXISTS AND `sop_ref` DOES NOT SERVE. The register's `sop_ref` names the
// TREATMENT SOP, not the disease: PPR, POX and UNDIFFERENTIATED all carry `sop_ref:
// Supportive`, so a dropdown built from it would offer the operator three different
// diseases under one identical label and give the mortality board no way to tell them
// apart. The rule ID is unique and precise but is a MACHINE KEY — 'FOOT_ROT' on a screen
// is the copy-firewall break this product bans — so the two are mapped here, in one
// reviewable place, exactly as `humanLabel` does for column keys.
//
// MAINTAINER NOTE: the expansions of the abbreviated rules (PREG_TOX, CALCULI, NEURO,
// SKIN) are this author's reading of standard veterinary shorthand, not text taken from a
// farm document. They are copy and are cheap to correct; nothing but the label changes.
var deathCauseLabels = map[string]string{
	"ACIDOSIS":    "Acidosis",
	"ANEMIA":      "Anemia",
	"ARTHRITIS":   "Arthritis",
	"BLOAT":       "Bloat",
	"BODY_EDEMA":  "Body edema",
	"CALCULI":     "Urinary calculi",
	"DIARRHEA":    "Diarrhea",
	"FEVER":       "Fever",
	"FLOPPY_KID":  "Floppy kid",
	"FLYSTRIKE":   "Flystrike",
	"FOOT_ROT":    "Foot rot",
	"FRACTURE":    "Fracture",
	"HEAT_STRESS": "Heat stress",
	"HYPOTHERMIA": "Hypothermia",
	"JAUNDICE":    "Jaundice",
	"LAMINITIS":   "Laminitis",
	"LUMPS":       "Lumps",
	"MASTITIS":    "Mastitis",
	"METRITIS":    "Metritis",
	"MILK_FEVER":  "Milk fever",
	"NAVEL_ILL":   "Navel ill",
	"NEURO":       "Neurological",
	"ORF":         "Orf",
	"PINKEYE":     "Pinkeye",
	"POX":         "Pox",
	"PPR":         "PPR",
	"PREG_TOX":    "Pregnancy toxaemia",
	"PROLAPSE":    "Prolapse",
	"RED_URINE":   "Red urine",
	"SKIN":        "Skin condition",
	"TETANUS":     "Tetanus",
	"UDDER_EDEMA": "Udder edema",
	"WOUNDS":      "Wounds",
}

// DeathCauseLabel returns the farm's word for a rule.
//
// A rule with no mapping falls back to a HUMANISED id rather than the raw key, so a rule
// added to the register tomorrow reaches the dropdown reading "Ring worm" instead of
// "RING_WORM" — and never disappears from it, which is what a fail-closed empty label
// would do. The map is still the right place to give it the farm's own wording.
func DeathCauseLabel(ruleID string) string {
	if label, ok := deathCauseLabels[ruleID]; ok {
		return label
	}
	humanised := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(ruleID), "_", " "))
	if humanised == "" {
		return ""
	}
	return strings.ToUpper(humanised[:1]) + humanised[1:]
}
