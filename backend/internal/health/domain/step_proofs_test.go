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
	if got := StepLabel(med); got != "Tylosin 0.1/kg IM" {
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
