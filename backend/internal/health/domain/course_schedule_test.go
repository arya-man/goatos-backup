package domain

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

func step(day int, session, recordType string) ProtocolStep {
	return ProtocolStep{DayNo: day, Session: session, RecordType: recordType}
}

func sessionNames(in []ScheduledSession) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, s.Session)
	}
	return out
}

// TestSessionsForHousing pins maintainer decision F: the housing directive, not
// the authored grid, decides how often the animal is seen.
func TestSessionsForHousing(t *testing.T) {
	cases := []struct {
		name        string
		acuity      string
		containment string
		want        []string
	}{
		{"icu earns both shifts", diagnosis.AcuityICU, diagnosis.ContainmentHome,
			[]string{SessionMorning, SessionEvening}},
		{"icu and quarantine still both", diagnosis.AcuityICU, diagnosis.ContainmentQuarantine,
			[]string{SessionMorning, SessionEvening}},
		{"ward is once a day", diagnosis.AcuityWard, diagnosis.ContainmentHome,
			[]string{SessionMorning}},
		// A standing, eating quarantine animal is seen once. Quarantine is a
		// containment decision; it does not by itself mean the animal is crashing.
		{"quarantine without icu is once a day", diagnosis.AcuityHome, diagnosis.ContainmentQuarantine,
			[]string{SessionMorning}},
		{"field has no daily cycle", diagnosis.AcuityField, diagnosis.ContainmentHome, nil},
		{"home has no daily cycle", diagnosis.AcuityHome, diagnosis.ContainmentHome, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SessionsForHousing(tc.acuity, tc.containment)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SessionsForHousing(%q,%q) = %v, want %v", tc.acuity, tc.containment, got, tc.want)
			}
		})
	}
}

// THE CARD EARNS ITS OWN VISITS (maintainer decision 2026-09-23, superseding the roll-forward
// this test used to pin).
//
// A ward animal earns a morning visit from its housing. A card that also authors an AFTERNOON
// dose earns an afternoon visit too, and the operator goes twice -- the farm has the people, and
// the alternative was handing someone an evening dose at 07:00 with nothing saying it was meant
// for 19:00.
//
// `unscheduled` names no time, so it still rides the earliest visit: a medicine with no hour on
// it is given at the first chance, never dropped and never deferred.
func TestTheCardEarnsItsOwnVisits(t *testing.T) {
	steps := []ProtocolStep{
		step(1, SessionMorning, "action"),
		step(1, SessionAfternoon, "medication"),
		step(1, SessionUnscheduled, "action"),
	}
	got := ScheduleCourse(steps, 1, []string{SessionMorning})

	if len(got) != 2 {
		t.Fatalf("a card authoring an afternoon dose earns an afternoon visit; got %d visits", len(got))
	}
	if got[0].Session != SessionMorning || got[1].Session != SessionAfternoon {
		t.Fatalf("visits = %q/%q, want morning then afternoon in clock order", got[0].Session, got[1].Session)
	}
	// Morning keeps its own step AND the one with no hour on it.
	if len(got[0].Steps) != 2 {
		t.Fatalf("morning visit holds %d steps, want its own plus the unscheduled one", len(got[0].Steps))
	}
	if got[0].Steps[0].Session != SessionMorning || got[0].Steps[1].Session != SessionUnscheduled {
		t.Errorf("morning steps = %q/%q, want the authored one then the unscheduled one",
			got[0].Steps[0].Session, got[0].Steps[1].Session)
	}
	// The afternoon dose is given in the afternoon, which is the whole point.
	if len(got[1].Steps) != 1 || got[1].Steps[0].Session != SessionAfternoon {
		t.Fatalf("afternoon visit = %+v, want exactly the afternoon dose", got[1].Steps)
	}
	// NOTHING IS LOST in the division: every authored step still lands somewhere.
	total := 0
	for _, v := range got {
		total += len(v.Steps)
	}
	if total != len(steps) {
		t.Fatalf("%d of %d authored steps survived the split", total, len(steps))
	}
}

// HOUSING IS A FLOOR. An ICU animal is seen morning and evening whatever its card authors,
// because being in ICU is itself a reason to look at it.
func TestHousingIsAFloorTheCardCannotLower(t *testing.T) {
	got := ScheduleCourse([]ProtocolStep{step(1, SessionMorning, "medication")}, 1,
		[]string{SessionMorning, SessionEvening})

	if len(got) != 2 {
		t.Fatalf("ICU earns two visits even from a morning-only card; got %d", len(got))
	}
	if got[1].Session != SessionEvening || len(got[1].Steps) != 0 {
		t.Fatalf("the evening visit must still happen and carry no invented work: %+v", got[1])
	}
}

// An ICU animal is seen twice, so an evening-authored step stays in the evening
// rather than being pulled forward.
func TestScheduleCourseKeepsStepsAtAReachableSession(t *testing.T) {
	steps := []ProtocolStep{
		step(1, SessionMorning, "medication"),
		step(1, SessionEvening, "medication"),
	}
	got := ScheduleCourse(steps, 1, []string{SessionMorning, SessionEvening})

	if len(got) != 2 {
		t.Fatalf("want two visits, got %d", len(got))
	}
	for i, want := range []string{SessionMorning, SessionEvening} {
		if got[i].Session != want {
			t.Fatalf("visit %d = %q, want %q", i, got[i].Session, want)
		}
		if len(got[i].Steps) != 1 {
			t.Errorf("visit %q should hold exactly its own step, got %d", want, len(got[i].Steps))
		}
	}
}

// A day inside the course with no authored step still earns its visits: the
// animal is seen even when the card says nothing for that day.
func TestScheduleCourseVisitsEveryDayEvenWithoutSteps(t *testing.T) {
	got := ScheduleCourse([]ProtocolStep{step(1, SessionMorning, "action")}, 3, []string{SessionMorning})
	if len(got) != 3 {
		t.Fatalf("a 3-day ward course must yield 3 visits, got %d", len(got))
	}
	if len(got[1].Steps) != 0 || len(got[2].Steps) != 0 {
		t.Errorf("days 2 and 3 have no authored steps and must be empty visits")
	}
}

// Steps authored beyond the snapshot horizon are ignored rather than compressed
// into the last day, which would double up a day's medication.
func TestScheduleCourseIgnoresStepsBeyondTheHorizon(t *testing.T) {
	steps := []ProtocolStep{step(1, SessionMorning, "medication"), step(5, SessionMorning, "medication")}
	got := ScheduleCourse(steps, 2, []string{SessionMorning})
	total := 0
	for _, v := range got {
		total += len(v.Steps)
	}
	if total != 1 {
		t.Errorf("only the day-1 step is inside a 2-day horizon, got %d steps", total)
	}
}

// Field housing has no daily cycle, so it generates no visits at all. A field
// action is treated in place and once.
func TestScheduleCourseProducesNothingWithoutVisits(t *testing.T) {
	if got := ScheduleCourse([]ProtocolStep{step(1, SessionMorning, "action")}, 3, nil); got != nil {
		t.Errorf("no housing sessions must yield no visits, got %v", sessionNames(got))
	}
}

func TestClosureModel(t *testing.T) {
	for _, exit := range []string{ExitTypeFixed, ExitTypeTest, ExitTypeDirector, ExitTypeSupportive} {
		if !ValidExitType(exit) {
			t.Errorf("%q must be a valid exit type", exit)
		}
	}
	if ValidExitType("f") || ValidExitType("fixed") || ValidExitType("") {
		t.Error("exit type is validate-or-reject; near-misses must not pass")
	}
	if !ClosesOnDayCount(ExitTypeFixed) {
		t.Error("Type F closes on a day count")
	}
	for _, exit := range []string{ExitTypeTest, ExitTypeDirector, ExitTypeSupportive} {
		if ClosesOnDayCount(exit) {
			t.Errorf("%q must not close on a day count -- that invents an end date", exit)
		}
	}
}

// TestSOPRefResolvesForEveryRegisterRule is the cross-source check: every sop_ref
// the register can emit must resolve to a disease_key, and the four field actions
// must resolve to nothing at all.
//
// This reads the real register rather than a fixture, so a new rule with a
// sop_ref nobody mapped shows up here rather than at diagnosis time.
func TestSOPRefResolvesForEveryRegisterRule(t *testing.T) {
	raw, err := os.ReadFile("../diagnosis/registers/adult-1.yaml")
	if err != nil {
		t.Fatalf("read register: %v", err)
	}
	reg, err := diagnosis.Load(raw)
	if err != nil {
		t.Fatalf("load register: %v", err)
	}

	for i := range reg.Rules {
		rule := &reg.Rules[i]
		key := SOPRefToDiseaseKey(rule.SOPRef)
		if rule.Kind == diagnosis.KindField {
			if key != "" {
				t.Errorf("%s is a field action and must resolve to no card, got %q", rule.ID, key)
			}
			continue
		}
		if key == "" {
			t.Errorf("%s (sop_ref %q) resolved to no disease key", rule.ID, rule.SOPRef)
		}
	}
}

// TestSOPRefMatchesAuthoredCards is the honest inventory: it reports which
// register diagnoses currently have a published treatment card and which do not.
//
// It does NOT fail on a missing card. Nine of the thirty diagnoses point at cards
// the Health Director has not authored yet, and that is a known, accepted state
// handled by failing closed at diagnosis time. What this test DOES fail on is a
// resolution that lands on nothing while an obviously-matching card exists --
// i.e. the alias table silently going stale.
func TestSOPRefMatchesAuthoredCards(t *testing.T) {
	raw, err := os.ReadFile("../diagnosis/registers/adult-1.yaml")
	if err != nil {
		t.Fatalf("read register: %v", err)
	}
	reg, err := diagnosis.Load(raw)
	if err != nil {
		t.Fatalf("load register: %v", err)
	}

	snapshot, err := os.ReadFile("../../../../context/source-findings/health-sop-v1.json")
	if err != nil {
		t.Skipf("bootstrap snapshot unavailable: %v", err)
	}
	var parsed struct {
		Protocols []struct {
			DiseaseKey string `json:"disease_key"`
		} `json:"protocols"`
	}
	if err := json.Unmarshal(snapshot, &parsed); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	authored := map[string]bool{}
	for _, p := range parsed.Protocols {
		authored[p.DiseaseKey] = true
	}

	// The cards known to be missing, from the 2026-08-14 gap analysis. Listing
	// them by name means authoring one WITHOUT removing it here fails this test,
	// which is the reminder to update the list.
	knownMissing := map[string]bool{
		"supportive": true, "calculi": true, "laminitis": true,
		"metritis": true, "neuro": true, "prolapse": true, "tetanus": true,
	}

	for i := range reg.Rules {
		rule := &reg.Rules[i]
		if rule.Kind == diagnosis.KindField || rule.HumanSelectedOnly {
			continue
		}
		key := SOPRefToDiseaseKey(rule.SOPRef)
		switch {
		case authored[key] && knownMissing[key]:
			t.Errorf("%q is now authored -- remove it from knownMissing", key)
		case !authored[key] && !knownMissing[key]:
			t.Errorf("%s (sop_ref %q) resolves to %q, which no card matches and which is "+
				"not a known gap -- the alias table is stale", rule.ID, rule.SOPRef, key)
		}
	}
}
