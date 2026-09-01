package diagnosis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// The acceptance catalog is the contract. ENGINEERING.md is explicit: "Done =
// every story passes in your code. A miss is a rule bug or an algorithm bug, NOT
// a skip." There is deliberately no skip mechanism in this file.

const (
	catalogPath = "testdata/catalog.json"
	// expectedStories guards against a truncated or partially-loaded catalog
	// silently reporting green.
	expectedStories = 180
)

type story struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Animal Animal   `json:"animal"`
	Find   Findings `json:"findings"`
	Ctx    Context  `json:"ctx"`
	Expect expect   `json:"expect"`

	Pair            *story `json:"pair"`
	SameHits        *bool  `json:"same_hits"`
	SameContainment bool   `json:"same_containment"`
	PairDiff        bool   `json:"pair_diff"`
}

// expect mirrors the catalog's assertion vocabulary. A key that is absent is not
// asserted, so every field is a pointer or a slice whose nil-ness is meaningful.
type expect struct {
	Valid  *bool   `json:"valid"`
	Reject *string `json:"reject"`
	Scope  *string `json:"scope"`

	Hits    []string `json:"hits"`
	HitsHas []string `json:"hits_has"`
	NotHits []string `json:"not_hits"`

	Covered    []string `json:"covered"`
	CoveredHas []string `json:"covered_has"`

	Emergencies    []string `json:"emergencies"`
	EmergenciesHas []string `json:"emergencies_has"`
	NotEmergencies []string `json:"not_emergencies"`

	Field    []string `json:"field"`
	NotField []string `json:"not_field"`
	Rechecks []string `json:"rechecks"`

	Acuity         *string `json:"acuity"`
	Containment    *string `json:"containment"`
	LowComp        *bool   `json:"low_comp"`
	MorningWalk    *bool   `json:"morning_walk"`
	EveningWalk    *bool   `json:"evening_walk"`
	NoDueOvernight *bool   `json:"no_due_overnight"`

	NoMeloxicam *bool `json:"no_meloxicam"`
	Club        *bool `json:"club"`

	SOP        map[string]string `json:"sop"`
	CourseType map[string]string `json:"course_type"`
	Tier       map[string]string `json:"tier"`

	HDHas []string `json:"hd_has"`
	HDNot []string `json:"hd_not"`

	HintsHas       []string `json:"hints_has"`
	UnexplainedHas []string `json:"unexplained_has"`

	New     []string `json:"new"`
	NewHas  []string `json:"new_has"`
	Ongoing []string `json:"ongoing"`

	OngoingHas      []string `json:"ongoing_has"`
	ProposeClose    []string `json:"propose_close"`
	ProposeExtend   []string `json:"propose_extend"`
	NotProposeClose []string `json:"not_propose_close"`
}

type catalog struct {
	Version string  `json:"catalog_version"`
	Count   int     `json:"count"`
	Stories []story `json:"stories"`
}

func loadRegister(t *testing.T) *Register {
	t.Helper()
	reg, err := AdultRegister()
	if err != nil {
		t.Fatalf("load embedded register: %v", err)
	}
	return reg
}

func loadCatalog(t *testing.T) catalog { return loadCatalogAt(t, catalogPath) }

func loadCatalogAt(t *testing.T, path string) catalog {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	// Unknown fields are rejected so a catalog key this harness does not assert
	// cannot pass silently — an unread expectation is a story that is not
	// actually being checked.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c catalog
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	return c
}

// TestRegisterStructuralValidators is Test 1: vocabulary, anchoring and
// reachability across all three tier pairs. It runs before the stories because a
// register that fails these cannot produce a trustworthy story result.
func TestRegisterStructuralValidators(t *testing.T) {
	reg := loadRegister(t)
	if errs := reg.Validate(); len(errs) > 0 {
		for _, err := range errs {
			t.Errorf("register validator: %v", err)
		}
	}
	if reg.Version != "adult-1" {
		t.Errorf("register version = %q, want adult-1", reg.Version)
	}
}

// TestAcceptanceCatalog runs S001-S180.
func TestAcceptanceCatalog(t *testing.T) {
	reg := loadRegister(t)
	c := loadCatalog(t)

	if len(c.Stories) != expectedStories {
		t.Fatalf("catalog has %d stories, want %d", len(c.Stories), expectedStories)
	}
	if c.Count != expectedStories {
		t.Fatalf("catalog count field = %d, want %d", c.Count, expectedStories)
	}
	seen := map[string]bool{}
	for _, s := range c.Stories {
		if seen[s.ID] {
			t.Fatalf("duplicate story id %s", s.ID)
		}
		seen[s.ID] = true
	}

	for _, s := range c.Stories {
		t.Run(s.ID, func(t *testing.T) {
			got := reg.Evaluate(s.Animal, s.Find, s.Ctx)
			for _, msg := range checkStory(reg, s, got) {
				t.Errorf("%s (%s): %s", s.ID, s.Title, msg)
			}
		})
	}
}

func checkStory(reg *Register, s story, got Proposal) []string {
	var errs []string
	e := s.Expect

	if e.Valid != nil && got.Valid != *e.Valid {
		errs = append(errs, fmt.Sprintf("valid = %v, want %v", got.Valid, *e.Valid))
	}
	if e.Reject != nil && got.RejectReason != *e.Reject {
		errs = append(errs, fmt.Sprintf("reject = %q, want %q", got.RejectReason, *e.Reject))
	}
	if !got.Valid {
		return errs
	}

	if e.Scope != nil && got.Scope != *e.Scope {
		errs = append(errs, fmt.Sprintf("scope = %q, want %q", got.Scope, *e.Scope))
	}

	errs = append(errs, eqSet("hits", got.Problems, e.Hits)...)
	errs = append(errs, subset("hits", got.Problems, e.HitsHas)...)
	errs = append(errs, disjoint("hits", got.Problems, e.NotHits)...)

	errs = append(errs, eqSet("covered", got.Covered, e.Covered)...)
	errs = append(errs, subset("covered", got.Covered, e.CoveredHas)...)

	errs = append(errs, eqSet("emergencies", got.Emergencies, e.Emergencies)...)
	errs = append(errs, subset("emergencies", got.Emergencies, e.EmergenciesHas)...)
	errs = append(errs, disjoint("emergencies", got.Emergencies, e.NotEmergencies)...)

	errs = append(errs, eqSet("field", got.FieldActions, e.Field)...)
	errs = append(errs, disjoint("field", got.FieldActions, e.NotField)...)
	errs = append(errs, eqSet("rechecks", got.Rechecks, e.Rechecks)...)

	errs = append(errs, eqStr("acuity", got.Housing.Acuity, e.Acuity)...)
	errs = append(errs, eqStr("containment", got.Housing.Containment, e.Containment)...)
	errs = append(errs, eqBool("low_comp", got.Housing.LowCompetition, e.LowComp)...)
	errs = append(errs, eqBool("morning_walk", got.Housing.MorningWalk, e.MorningWalk)...)
	errs = append(errs, eqBool("evening_walk", got.Housing.EveningWalk, e.EveningWalk)...)
	errs = append(errs, eqBool("no_due_overnight", got.Housing.NoDueOvernight, e.NoDueOvernight)...)

	errs = append(errs, eqBool("no_meloxicam", got.NoMeloxicam, e.NoMeloxicam)...)
	errs = append(errs, eqBool("club", got.Club, e.Club)...)

	for k, want := range e.SOP {
		if got.SOP[k] != want {
			errs = append(errs, fmt.Sprintf("sop[%s] = %q, want %q", k, got.SOP[k], want))
		}
	}
	for k, want := range e.CourseType {
		if got.CourseType[k] != want {
			errs = append(errs, fmt.Sprintf("course_type[%s] = %q, want %q", k, got.CourseType[k], want))
		}
	}
	for k, want := range e.Tier {
		if string(got.Tiers[k]) != want {
			errs = append(errs, fmt.Sprintf("tier[%s] = %q, want %q", k, got.Tiers[k], want))
		}
	}

	errs = append(errs, subset("hd_flags", got.DirectorFlags, e.HDHas)...)
	errs = append(errs, disjoint("hd_flags", got.DirectorFlags, e.HDNot)...)
	errs = append(errs, subset("hints", got.Hints, e.HintsHas)...)
	errs = append(errs, subset("unexplained", got.Unexplained, e.UnexplainedHas)...)

	errs = append(errs, eqSet("new", got.New, e.New)...)
	errs = append(errs, subset("new", got.New, e.NewHas)...)
	errs = append(errs, eqSet("ongoing", got.Ongoing, e.Ongoing)...)
	errs = append(errs, subset("ongoing", got.Ongoing, e.OngoingHas)...)

	errs = append(errs, eqSet("propose_close", got.ProposeClose, e.ProposeClose)...)
	errs = append(errs, eqSet("propose_extend", got.ProposeExtend, e.ProposeExtend)...)
	errs = append(errs, disjoint("propose_close", got.ProposeClose, e.NotProposeClose)...)

	if s.Pair != nil {
		errs = append(errs, checkPair(reg, s, got)...)
	}
	return errs
}

// checkPair covers the paired stories: two presentations that must agree (the
// goat/sheep pox pair, which is one rule for both species) or must differ.
func checkPair(reg *Register, s story, got Proposal) []string {
	var errs []string
	other := reg.Evaluate(s.Pair.Animal, s.Pair.Find, s.Pair.Ctx)

	sameHits := s.SameHits == nil || *s.SameHits
	if sameHits && !sameSet(got.Problems, other.Problems) {
		errs = append(errs, fmt.Sprintf("pair hits %v != %v", got.Problems, other.Problems))
	}
	if s.SameContainment && got.Housing.Containment != other.Housing.Containment {
		errs = append(errs, fmt.Sprintf("pair containment %q != %q",
			got.Housing.Containment, other.Housing.Containment))
	}
	if s.PairDiff {
		if sameSet(got.Problems, other.Problems) && got.Housing.Containment == other.Housing.Containment {
			errs = append(errs, fmt.Sprintf("pair expected to differ, both %v / %q",
				got.Problems, got.Housing.Containment))
		}
		pe := s.Pair.Expect
		errs = append(errs, eqSet("pair2 hits", other.Problems, pe.Hits)...)
		errs = append(errs, eqStr("pair2 containment", other.Housing.Containment, pe.Containment)...)
		for k, want := range pe.SOP {
			if other.SOP[k] != want {
				errs = append(errs, fmt.Sprintf("pair2 sop[%s] = %q, want %q", k, other.SOP[k], want))
			}
		}
	}
	return errs
}

// --- set helpers. The catalog compares as sets unless a key asks for an exact
// list, and none of them do; ordering inside a bucket carries no meaning.

func eqSet(name string, got, want []string) []string {
	if want == nil {
		return nil
	}
	if !sameSet(got, want) {
		return []string{fmt.Sprintf("%s = %v, want %v", name, sortedCopy(got), sortedCopy(want))}
	}
	return nil
}

func subset(name string, got, want []string) []string {
	if want == nil {
		return nil
	}
	have := toSet(got)
	var missing []string
	for _, w := range want {
		if !have[w] {
			missing = append(missing, w)
		}
	}
	if len(missing) > 0 {
		return []string{fmt.Sprintf("%s missing %v (have %v)", name, missing, sortedCopy(got))}
	}
	return nil
}

func disjoint(name string, got, forbidden []string) []string {
	if forbidden == nil {
		return nil
	}
	have := toSet(got)
	var present []string
	for _, f := range forbidden {
		if have[f] {
			present = append(present, f)
		}
	}
	if len(present) > 0 {
		return []string{fmt.Sprintf("%s unexpectedly contains %v", name, present)}
	}
	return nil
}

func eqStr(name, got string, want *string) []string {
	if want == nil || got == *want {
		return nil
	}
	return []string{fmt.Sprintf("%s = %q, want %q", name, got, *want)}
}

func eqBool(name string, got bool, want *bool) []string {
	if want == nil || got == *want {
		return nil
	}
	return []string{fmt.Sprintf("%s = %v, want %v", name, got, *want)}
}

func sameSet(a, b []string) bool {
	sa, sb := toSet(a), toSet(b)
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}
