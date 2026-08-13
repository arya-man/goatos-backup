package domain

import (
	"strings"
	"testing"
)

// The authoring rulebook. Every case here is a clinical or structural rule that, if it stopped
// holding, would let an unfollowable instruction reach an operator's phone.

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	if err == nil {
		return map[string]string{}
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T: %v", err, err)
	}
	out := map[string]string{}
	for _, fe := range ve.Errors {
		out[fe.Field] = fe.Message
	}
	return out
}

func medicineStep(day int, session string) AuthoredStep {
	return AuthoredStep{
		DayNo:             day,
		Session:           session,
		RecordType:        RecordTypeMedication,
		MedicineName:      "Meloxicam Paracetamol",
		DosageText:        "5",
		DosageDenominator: "ml",
		MedicineRoute:     "IM",
	}
}

func TestValidateAuthoredProtocolAcceptsACompleteCourse(t *testing.T) {
	p := AuthoredProtocol{
		DisplayName:  "Foot rot",
		DurationDays: 2,
		Steps: []AuthoredStep{
			medicineStep(1, SessionMorning),
			{DayNo: 1, Session: SessionEvening, RecordType: RecordTypeAction, Instruction: "Clean and dress the hoof."},
			medicineStep(2, SessionMorning),
		},
	}
	if err := ValidateAuthoredProtocol(p, true); err != nil {
		t.Fatalf("expected a complete course to publish, got %v", err)
	}
}

// A draft is work-in-progress an author saves and returns to; publish is the gate. If these two
// ever apply the same rules, either drafts become unsavable or unpublishable protocols go live.
func TestDraftMayBeIncompleteButPublishMayNot(t *testing.T) {
	empty := AuthoredProtocol{DisplayName: "New disease", DurationDays: 3}

	if err := ValidateAuthoredProtocol(empty, false); err != nil {
		t.Fatalf("a draft with no steps must save, got %v", err)
	}
	fields := fieldsOf(t, ValidateAuthoredProtocol(empty, true))
	if _, ok := fields["steps"]; !ok {
		t.Fatalf("publishing a stepless protocol must be rejected; got %v", fields)
	}
}

// The failure this prevents: an author shortens a 7-day course to 3 days. Steps on days 4-7 would
// never be scheduled, and nothing would tell them. Auto-deleting those steps, or auto-extending the
// duration, are each a guess about a medical document -- so publish reports and refuses.
func TestPublishRejectsStepsBeyondTheCourseLength(t *testing.T) {
	p := AuthoredProtocol{
		DisplayName:  "Anemia",
		DurationDays: 2,
		Steps:        []AuthoredStep{medicineStep(1, SessionMorning), medicineStep(2, SessionMorning), medicineStep(5, SessionMorning)},
	}
	if err := ValidateAuthoredProtocol(p, false); err != nil {
		t.Fatalf("a draft may hold a step past the duration, got %v", err)
	}
	fields := fieldsOf(t, ValidateAuthoredProtocol(p, true))
	msg, ok := fields["steps[2].day_no"]
	if !ok {
		t.Fatalf("expected the out-of-range step to be named; got %v", fields)
	}
	if !strings.Contains(msg, "Day 5") || !strings.Contains(msg, "2 day") {
		t.Fatalf("the message must name the day and the course length, got %q", msg)
	}
}

// A day inside the course with no step at all is a gap in the treatment, not an empty row.
func TestPublishReportsDaysWithNoSteps(t *testing.T) {
	p := AuthoredProtocol{
		DisplayName:  "Fever",
		DurationDays: 4,
		Steps:        []AuthoredStep{medicineStep(1, SessionMorning), medicineStep(4, SessionMorning)},
	}
	fields := fieldsOf(t, ValidateAuthoredProtocol(p, true))
	msg, ok := fields["steps"]
	if !ok || !strings.Contains(msg, "2, 3") {
		t.Fatalf("expected days 2 and 3 to be reported as empty, got %v", fields)
	}
}

// Half an instruction is not safe to complete by inference. "5" with no unit and "ml" with no
// number are both incomplete, and each is reported against the field the author must fill.
func TestPublishRequiresACompleteDosageAndARoute(t *testing.T) {
	cases := []struct {
		name      string
		step      AuthoredStep
		wantField string
	}{
		{
			name: "amount without a unit",
			step: AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeMedication,
				MedicineName: "OTC 200mg", DosageText: "5", MedicineRoute: "IM"},
			wantField: "steps[0].dosage_denominator",
		},
		{
			name: "unit without an amount",
			step: AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeMedication,
				MedicineName: "OTC 200mg", DosageDenominator: "ml", MedicineRoute: "IM"},
			wantField: "steps[0].dosage_text",
		},
		{
			name: "no route at all",
			step: AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeMedication,
				MedicineName: "OTC 200mg", DosageText: "5", DosageDenominator: "ml"},
			wantField: "steps[0].medicine_route",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := AuthoredProtocol{DisplayName: "Fever", DurationDays: 1, Steps: []AuthoredStep{tc.step}}
			fields := fieldsOf(t, ValidateAuthoredProtocol(p, true))
			if _, ok := fields[tc.wantField]; !ok {
				t.Fatalf("expected %s to be reported, got %v", tc.wantField, fields)
			}
			// A draft must still hold it: this is exactly the half-filled state an author saves.
			if err := ValidateAuthoredProtocol(p, false); err != nil {
				t.Fatalf("a draft may hold an incomplete dosage, got %v", err)
			}
		})
	}
}

// The route is a CLINICAL instruction. A stored typo renders on the phone as something nobody can
// follow, so the vocabulary is closed on both draft and publish.
func TestRouteAndUnitAreClosedVocabularies(t *testing.T) {
	p := AuthoredProtocol{
		DisplayName:  "Fever",
		DurationDays: 1,
		Steps: []AuthoredStep{{
			DayNo: 1, Session: SessionMorning, RecordType: RecordTypeMedication,
			MedicineName: "OTC 200mg", DosageText: "5", DosageDenominator: "litres", MedicineRoute: "1M",
		}},
	}
	fields := fieldsOf(t, ValidateAuthoredProtocol(p, false))
	if _, ok := fields["steps[0].medicine_route"]; !ok {
		t.Fatalf("a route outside the vocabulary must be rejected on a DRAFT too, got %v", fields)
	}
	if _, ok := fields["steps[0].dosage_denominator"]; !ok {
		t.Fatalf("a unit outside the vocabulary must be rejected on a DRAFT too, got %v", fields)
	}
}

// The database CHECK constraints say (record_type = 'critical_action') = (critical_action_type IS
// NOT NULL), and that a medication needs a name while everything else needs an instruction.
// Validation must reject these shapes with a field error rather than letting the insert fail with
// a constraint violation the author cannot interpret.
func TestStepShapeMatchesItsType(t *testing.T) {
	cases := []struct {
		name      string
		step      AuthoredStep
		wantField string
	}{
		{
			name:      "medicine with no name",
			step:      AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeMedication},
			wantField: "steps[0].medicine_name",
		},
		{
			name:      "action with no instruction",
			step:      AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeAction},
			wantField: "steps[0].instruction",
		},
		{
			name: "medicine carrying a critical handoff",
			step: AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeMedication,
				MedicineName: "OTC 200mg", CriticalActionType: CriticalActionLifecycleExit},
			wantField: "steps[0].critical_action_type",
		},
		{
			name: "critical action with no handoff type",
			step: AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeCriticalAction,
				Instruction: "Shift it to quarantine."},
			wantField: "steps[0].critical_action_type",
		},
		{
			name: "critical action also giving a medicine",
			step: AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeCriticalAction,
				Instruction: "Shift it to quarantine.", CriticalActionType: CriticalActionQuarantineOrMove,
				MedicineName: "OTC 200mg"},
			wantField: "steps[0].medicine_name",
		},
		{
			name:      "unknown step type",
			step:      AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: "note"},
			wantField: "steps[0].record_type",
		},
		{
			name:      "unknown session",
			step:      AuthoredStep{DayNo: 1, Session: "night", RecordType: RecordTypeAction, Instruction: "Check."},
			wantField: "steps[0].session",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := AuthoredProtocol{DisplayName: "Test", DurationDays: 1, Steps: []AuthoredStep{tc.step}}
			fields := fieldsOf(t, ValidateAuthoredProtocol(p, false))
			if _, ok := fields[tc.wantField]; !ok {
				t.Fatalf("expected %s, got %v", tc.wantField, fields)
			}
		})
	}
}

// Validate-or-reject: a present but out-of-range duration is an error, never rewritten to the
// default. Defaults apply only to genuinely-absent fields, and that substitution happens at the API
// edge where absent is still distinguishable from zero.
func TestOutOfRangeDurationIsRejectedNotDefaulted(t *testing.T) {
	for _, days := range []int{0, -1, MaxDurationDays + 1} {
		fields := fieldsOf(t, ValidateAuthoredProtocol(AuthoredProtocol{DisplayName: "Test", DurationDays: days}, false))
		if _, ok := fields["duration_days"]; !ok {
			t.Fatalf("duration %d must be rejected, got %v", days, fields)
		}
	}
}

// Every field error from one pass comes back together. One-at-a-time would make a 28-step protocol
// unauthorable.
func TestValidationReportsEveryFieldAtOnce(t *testing.T) {
	p := AuthoredProtocol{
		DisplayName:  "",
		DurationDays: 0,
		Steps: []AuthoredStep{
			{DayNo: 1, Session: "night", RecordType: RecordTypeMedication},
			{DayNo: 2, Session: SessionMorning, RecordType: RecordTypeAction},
		},
	}
	fields := fieldsOf(t, ValidateAuthoredProtocol(p, false))
	for _, want := range []string{"display_name", "duration_days", "steps[0].session", "steps[0].medicine_name", "steps[1].instruction"} {
		if _, ok := fields[want]; !ok {
			t.Fatalf("expected %s in one pass, got %v", want, fields)
		}
	}
}

// A blank row is what an "Add step" click leaves behind. Dropping it is what makes that button safe;
// keeping a HALF-filled row is what makes the mistake visible instead of silently discarded.
func TestNormalizeDropsFullyBlankStepsButKeepsPartialOnes(t *testing.T) {
	p := NormalizeAuthoredProtocol(AuthoredProtocol{
		DisplayName: "  Foot   rot  ",
		Steps: []AuthoredStep{
			{},                              // untouched "Add step" row
			{MedicineName: "  Tonoboost  "}, // half-filled: must survive and be reported
			{DayNo: 1, Session: " MORNING ", RecordType: " Medication ", MedicineName: "OTC 200mg"},
		},
	})
	if p.DisplayName != "Foot rot" {
		t.Fatalf("display name should be whitespace-normalized, got %q", p.DisplayName)
	}
	if len(p.Steps) != 2 {
		t.Fatalf("expected the blank row dropped and the half-filled one kept, got %d steps", len(p.Steps))
	}
	if p.Steps[0].MedicineName != "Tonoboost" {
		t.Fatalf("expected the half-filled row preserved and trimmed, got %+v", p.Steps[0])
	}
	if p.Steps[1].Session != SessionMorning || p.Steps[1].RecordType != RecordTypeMedication {
		t.Fatalf("session and record type should be lowercased and trimmed, got %+v", p.Steps[1])
	}
}

// Session order is a fact about a working day, not an alphabet. Sorting alphabetically would put
// afternoon before evening before MORNING -- the wrong order for the operator to work through.
func TestSortAuthoredStepsUsesDayThenWorkingSessionOrder(t *testing.T) {
	sorted := SortAuthoredSteps([]AuthoredStep{
		{DayNo: 2, Session: SessionMorning},
		{DayNo: 1, Session: SessionUnscheduled},
		{DayNo: 1, Session: SessionEvening},
		{DayNo: 1, Session: SessionMorning},
		{DayNo: 1, Session: SessionAfternoon},
	})
	want := []struct {
		day     int
		session string
	}{
		{1, SessionMorning}, {1, SessionAfternoon}, {1, SessionEvening}, {1, SessionUnscheduled}, {2, SessionMorning},
	}
	for i, w := range want {
		if sorted[i].DayNo != w.day || sorted[i].Session != w.session {
			t.Fatalf("position %d: want day %d %s, got day %d %s", i, w.day, w.session, sorted[i].DayNo, sorted[i].Session)
		}
	}
}

// Seq is server-assigned 1..N in the given order. The client never sends it, because
// health_protocol_steps is UNIQUE on (version, seq) and a client renumber collides mid-reorder.
func TestToProtocolStepsAssignsSequentialSeqAndNullsEmptyStrings(t *testing.T) {
	steps := ToProtocolSteps([]AuthoredStep{
		{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeAction, Instruction: "Check the animal."},
		medicineStep(1, SessionEvening),
	})
	if steps[0].Seq != 1 || steps[1].Seq != 2 {
		t.Fatalf("expected seq 1,2 got %d,%d", steps[0].Seq, steps[1].Seq)
	}
	// The empty-to-NULL mapping is what keeps health_protocol_steps_critical_check satisfied:
	// storing '' for a non-critical step's handoff type would violate the constraint.
	if steps[0].CriticalActionType != nil {
		t.Fatalf("an absent critical handoff must be NULL, not empty string")
	}
	if steps[0].MedicineName != nil {
		t.Fatalf("an action's medicine name must be NULL, not empty string")
	}
	if steps[1].MedicineRoute == nil || *steps[1].MedicineRoute != "IM" {
		t.Fatalf("a present route must survive, got %v", steps[1].MedicineRoute)
	}
}

func TestFromProtocolStepsRoundTrips(t *testing.T) {
	original := []AuthoredStep{
		medicineStep(1, SessionMorning),
		{DayNo: 2, Session: SessionUnscheduled, RecordType: RecordTypeCriticalAction,
			Instruction: "Shift to quarantine.", CriticalActionType: CriticalActionQuarantineOrMove},
	}
	back := FromProtocolSteps(ToProtocolSteps(original))
	if len(back) != len(original) {
		t.Fatalf("expected %d steps back, got %d", len(original), len(back))
	}
	for i := range original {
		if back[i] != original[i] {
			t.Fatalf("step %d did not round-trip: %+v vs %+v", i, back[i], original[i])
		}
	}
}

// The hash is what lets an identical re-save answer "unchanged" instead of writing a version that
// differs from its predecessor in nothing but its id. It must cover everything an author can change
// and nothing they cannot.
func TestContentHashCoversEveryAuthoredFieldAndOrder(t *testing.T) {
	base := AuthoredProtocol{
		DisplayName:  "Foot rot",
		DurationDays: 2,
		Steps:        []AuthoredStep{medicineStep(1, SessionMorning), medicineStep(2, SessionEvening)},
	}
	if ContentHash(base) != ContentHash(base) {
		t.Fatal("hash must be deterministic")
	}

	mutations := map[string]func(AuthoredProtocol) AuthoredProtocol{
		"renamed":          func(p AuthoredProtocol) AuthoredProtocol { p.DisplayName = "Footrot"; return p },
		"duration changed": func(p AuthoredProtocol) AuthoredProtocol { p.DurationDays = 3; return p },
		"dosage changed":   func(p AuthoredProtocol) AuthoredProtocol { p.Steps[0].DosageText = "3"; return p },
		"route changed":    func(p AuthoredProtocol) AuthoredProtocol { p.Steps[0].MedicineRoute = "SQ"; return p },
		"medicine changed": func(p AuthoredProtocol) AuthoredProtocol { p.Steps[0].MedicineName = "Tylosin"; return p },
		"unit changed":     func(p AuthoredProtocol) AuthoredProtocol { p.Steps[0].DosageDenominator = "kg"; return p },
		"session changed":  func(p AuthoredProtocol) AuthoredProtocol { p.Steps[0].Session = SessionEvening; return p },
		"day changed":      func(p AuthoredProtocol) AuthoredProtocol { p.Steps[0].DayNo = 2; return p },
		"step removed":     func(p AuthoredProtocol) AuthoredProtocol { p.Steps = p.Steps[:1]; return p },
		"steps reordered":  func(p AuthoredProtocol) AuthoredProtocol { p.Steps[0], p.Steps[1] = p.Steps[1], p.Steps[0]; return p },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			// Deep-copy so one mutation cannot leak into the next.
			mutated := base
			mutated.Steps = append([]AuthoredStep(nil), base.Steps...)
			if ContentHash(mutate(mutated)) == ContentHash(base) {
				t.Fatalf("%s must change the content hash", name)
			}
		})
	}
}

// Content that a naive encoding could confuse: delimiters and newlines typed into the free-text
// fields, and content moved across a field boundary.
//
// The format is collision-free by construction (fixed separator count per line, step count in the
// header), so these cases pass under either a quoted or a raw encoding. They are here as a
// REGRESSION FENCE around that property: if someone later adds an optional field, or drops the step
// count from the header, a shifted-field encoding stops being unambiguous and these go red.
func TestContentHashSeparatesFieldContentFromFieldBoundaries(t *testing.T) {
	protocol := func(steps ...AuthoredStep) AuthoredProtocol {
		return AuthoredProtocol{DisplayName: "Test", DurationDays: 1, Steps: steps}
	}
	medicine := func(name, dose string) AuthoredStep {
		return AuthoredStep{
			DayNo: 1, Session: SessionMorning, RecordType: RecordTypeMedication,
			MedicineName: name, DosageText: dose, DosageDenominator: "ml", MedicineRoute: "IM",
		}
	}
	action := func(instruction string) AuthoredStep {
		return AuthoredStep{DayNo: 1, Session: SessionMorning, RecordType: RecordTypeAction, Instruction: instruction}
	}

	cases := []struct {
		name string
		a, b AuthoredProtocol
	}{
		{
			// Content moved ACROSS a field boundary. Clinically these differ: one records no
			// dosage at all, the other records 5 ml.
			name: "delimiter typed into a medicine name vs a real dosage field",
			a:    protocol(medicine("OTC 200mg|5", "")),
			b:    protocol(medicine("OTC 200mg", "5")),
		},
		{
			name: "an instruction with an embedded newline vs without",
			a:    protocol(action("Give 5 ml\nthen wait")),
			b:    protocol(action("Give 5 ml then wait")),
		},
		{
			// A forged step boundary inside one instruction vs two genuine steps.
			name: "one instruction carrying a forged step line vs two real steps",
			a:    protocol(action("Clean|\n1|1|morning|action|||||Second step|")),
			b:    protocol(action("Clean"), action("Second step")),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if ContentHash(tc.a) == ContentHash(tc.b) {
				t.Fatal("these are different documents and must not share a content hash")
			}
		})
	}
}

func TestNormalizeDiseaseKey(t *testing.T) {
	cases := map[string]string{
		"Foot rot":          "foot_rot",
		"ET+TT":             "et_tt",
		"  Blue   Tongue  ": "blue_tongue",
		"ORF":               "orf",
		"Ear tag cleaning":  "ear_tag_cleaning",
		// The column CHECK requires a leading letter; a name starting with a digit would otherwise
		// produce a key the database refuses with an opaque error at insert time.
		"3 day scours": "d_3_day_scours",
		"!!!":          "",
	}
	for input, want := range cases {
		if got := NormalizeDiseaseKey(input); got != want {
			t.Fatalf("NormalizeDiseaseKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidDiseaseKeyMirrorsTheColumnCheck(t *testing.T) {
	valid := []string{"foot_rot", "orf", "et_tt", "d_3_day_scours", "a"}
	invalid := []string{"", "3_day", "_leading", "Foot_rot", "foot-rot", "foot rot"}
	for _, key := range valid {
		if !ValidDiseaseKey(key) {
			t.Fatalf("%q should be a valid disease key", key)
		}
	}
	for _, key := range invalid {
		if ValidDiseaseKey(key) {
			t.Fatalf("%q should NOT be a valid disease key", key)
		}
	}
}
