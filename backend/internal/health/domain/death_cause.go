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
//	disease  a coded cause, chosen from the diagnosis register, plus an optional note.
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
	// DeathCauseKindDiseaseKey is the TREATMENT CARD a pre-engine, direct-pick case was
	// opened against: 'mastitis', 'supportive'. It is used ONLY where the case carries no
	// register rule, and it is LOSSY on purpose to record rather than hide — PPR, POX and
	// UNDIFFERENTIATED all route to the 'supportive' card, so a cause stored this way
	// cannot say which of the three it was. A reader is owed that difference, which is why
	// the kind travels with the key everywhere.
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
}

// RegisterRule is the narrow view of a diagnosis rule this package needs. The diagnosis
// package's own Rule satisfies it structurally through BuildDeathCauseCatalog's caller,
// which keeps health/domain free of a dependency on the engine.
type RegisterRule struct {
	ID    string
	Label string
	Class string
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
		// The treatment-card vocabulary is NOT offered in the dropdown and cannot be
		// hand-typed by a client: it exists only where a pre-engine case already carries
		// that key, and the server resolves it from the case itself. Accepting one here
		// would let a caller file a death under a card key the animal was never treated
		// on, which is the same fabrication the register check above prevents.
		return fmt.Errorf("a treatment-card cause is resolved from the animal's own case, never submitted")
	default:
		return fmt.Errorf("death cause kind must be %s", DeathCauseKindRegisterRule)
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
