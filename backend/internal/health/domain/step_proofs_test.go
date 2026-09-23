package domain

import "testing"

func str(s string) *string { return &s }

// Every step owes a video (maintainer, 2026-09-23), and the refusal NAMES the steps that are
// missing -- an operator told "3 steps missing" has to hunt for them on a twelve-step card.
func TestEveryStepOwesItsVideo(t *testing.T) {
	steps := []ProtocolStep{
		{StepID: "s1", Seq: 1, RecordType: "action", Instruction: str("Check temperature")},
		{StepID: "s2", Seq: 2, RecordType: "medication", MedicineName: str("Tylosin")},
		{StepID: "s3", Seq: 3, RecordType: "medication", MedicineName: str("Meloxicam")},
	}

	missing := MissingStepProofs(steps, nil)
	if len(missing) != 3 {
		t.Fatalf("a card with nothing filmed owes all three steps, got %d", len(missing))
	}

	partly := MissingStepProofs(steps, []StepProof{{StepID: "s2", ProofRef: "p-2"}})
	if len(partly) != 2 || partly[0].StepID != "s1" || partly[1].StepID != "s3" {
		t.Fatalf("missing steps = %+v, want s1 and s3 in working order", partly)
	}

	done := MissingStepProofs(steps, []StepProof{
		{StepID: "s1", ProofRef: "p-1"}, {StepID: "s2", ProofRef: "p-2"}, {StepID: "s3", ProofRef: "p-3"},
	})
	if len(done) != 0 {
		t.Fatalf("every step filmed, still owes %+v", done)
	}
}

// A BLANK proof ref is not a recording. It reaches here from a row whose upload never landed, and
// counting it would let a session submit with a step nobody filmed -- the exact failure the proof
// business-ack contract exists to stop.
func TestABlankProofRefIsNotARecording(t *testing.T) {
	steps := []ProtocolStep{{StepID: "s1", Seq: 1, RecordType: "medication", MedicineName: str("Tylosin")}}
	if got := MissingStepProofs(steps, []StepProof{{StepID: "s1", ProofRef: "   "}}); len(got) != 1 {
		t.Fatalf("a blank proof ref counted as a recording: %+v", got)
	}
}

// A session from BEFORE step proofs has steps carrying no id, and is proved by the session's own
// single video. Treating those as missing would make every in-flight card unsubmittable.
func TestAPreStepProofSessionIsNotHeldByThisRule(t *testing.T) {
	steps := []ProtocolStep{{Seq: 1, RecordType: "medication", MedicineName: str("Tylosin")}}
	if got := MissingStepProofs(steps, nil); len(got) != 0 {
		t.Fatalf("a step with no id was held by the per-step rule: %+v", got)
	}
}

// The verifier receives a LIST of clips. Without a name per clip she cannot tell which injection
// each one is, which is the whole reason the review stayed one item per session.
func TestAStepNamesItselfForTheVerifier(t *testing.T) {
	med := ProtocolStep{
		RecordType: "medication", MedicineName: str("Tylosin"),
		DosageText: str("0.1"), DosageDenominator: str("kg"), MedicineRoute: str("IM"),
	}
	if got := StepLabel(med); got != "Tylosin · 0.1 per kg · IM" {
		t.Fatalf("medicine label = %q, want the medicine, its dose and its route", got)
	}
	action := ProtocolStep{RecordType: "action", Instruction: str("Check temperature")}
	if got := StepLabel(action); got != "Check temperature" {
		t.Fatalf("action label = %q, want the instruction", got)
	}
	bare := ProtocolStep{RecordType: "observation"}
	if got := StepLabel(bare); got != "observation" {
		t.Fatalf("a step with neither medicine nor instruction = %q, want its record type", got)
	}
}

// A DOSE IS NOT PRESENTATION. Joining the raw columns with a separator is what the phone did, and
// all three of these reached a real card on 2026-09-23.
func TestTheDoseReadsAsADosePerson(t *testing.T) {
	kg, none, oral, im := "kg", "none", "Oral", "IM"
	five, third := "5", "0.033"

	// A denominator is a PER-unit. "0.033 · kg" reads as a weight of medicine; it means 0.033
	// PER kg, and a decimal point is all that stands between a dose and ten times a dose.
	if got := DoseLabel(&third, &kg, &im); got != "0.033 per kg · IM" {
		t.Errorf("per-kg dose = %q, want it read as a rate", got)
	}
	// "none" is the authoring tool's way of saying there is no per-unit. It is not a unit and an
	// operator must never see the word.
	if got := DoseLabel(&five, &none, &oral); got != "5 · Oral" {
		t.Errorf("no-per-unit dose = %q, want the word none gone", got)
	}
	// A missing denominator is the same thing said with a NULL.
	if got := DoseLabel(&five, nil, &oral); got != "5 · Oral" {
		t.Errorf("null-denominator dose = %q", got)
	}
	// Nothing authored at all composes nothing rather than a stray separator.
	if got := DoseLabel(nil, nil, nil); got != "" {
		t.Errorf("empty dose = %q, want blank", got)
	}
	if got := DoseLabel(nil, nil, &oral); got != "Oral" {
		t.Errorf("route-only dose = %q, want just the route", got)
	}
}

// The verifier judges the clip against the dose the OPERATOR was told to give, so both read the
// same words. They drifted once already: the card said "5 · Oral" while the verifier said
// "Tylosin 5".
func TestTheVerifierAndTheOperatorReadTheSameDose(t *testing.T) {
	kg, im := "kg", "IM"
	amount := "0.1"
	step := ProtocolStep{
		RecordType: "medication", MedicineName: str("Tylosin"),
		DosageText: &amount, DosageDenominator: &kg, MedicineRoute: &im,
	}
	dose := DoseLabel(step.DosageText, step.DosageDenominator, step.MedicineRoute)
	if got := StepLabel(step); got != "Tylosin · "+dose {
		t.Fatalf("verifier label %q does not carry the operator's dose %q", got, dose)
	}
}
