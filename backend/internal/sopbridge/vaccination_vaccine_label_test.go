package sopbridge

// A vaccination verification item must name the VACCINE.
//
// The verifier's job is deciding whether the clip shows this dose being given to these animals.
// Before this, a shed-grain item read "Sumathi 1 - Part 3 · 12 goats" and a per-animal item read
// "Gandhi 1 · G-006004": both said WHERE and HOW MANY, neither said WHAT. She was judging a video
// of an animal against nothing, and a PPR clip filed under an ET+TT drive was indistinguishable
// from a correct one.
//
// The label is the HUMAN one (vaccinationdomain.DoseDisplayLabel, resolved in the vaccination
// adapter). Raw dose codes -- "et_tt_adult_w2", "ppr_booster" -- are config tokens and are banned
// from user-facing copy (AGENTS.md, make ui-vaccine-labels-guard), so the tests below assert the
// rendered form AND assert the raw code never appears.

import (
	"context"
	"strings"
	"testing"
	"time"

	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	vaccinationdomain "github.com/vgoats/goatos/backend/internal/vaccination/domain"
)

func TestVaccinationShedSubjectLabelNamesTheVaccine(t *testing.T) {
	administeredAt := time.Date(2026, 8, 5, 12, 39, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 2,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "c-1", SubmissionID: "sub-1", GoatID: "goat-1", ShedID: "shed-1", ShedLabel: "Sumathi 1 - Part 3", ParkID: "park-1", VaccineLabel: "ET+TT", AdministeredAt: administeredAt},
			{CompletionID: "c-2", SubmissionID: "sub-1", GoatID: "goat-2", ShedID: "shed-1", ShedLabel: "Sumathi 1 - Part 3", ParkID: "park-1", VaccineLabel: "ET+TT", AdministeredAt: administeredAt.Add(time.Minute)},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	shedID := "shed-1"
	submission := sopdomain.SubmissionSummary{
		SubmissionID: "sub-1",
		SubmittedBy:  "operator-1",
		ProofRefs:    []sopdomain.ProofReference{{ProofID: "shed-video-1", SubjectType: "shed", SubjectID: &shedID}},
	}
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1", sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"}, submission); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	// WHERE (shed + partition) · WHAT (vaccine) · HOW MANY.
	if got := deref(producer.last.SubjectLabel); got != "Sumathi 1 - Part 3 · ET+TT · 2 goats" {
		t.Fatalf("shed subject label = %q, want shed+partition, vaccine, and count", got)
	}
}

// One shed can be visited for more than one vaccine in the same drive, so the label must not
// assume a single dose -- and must not shuffle between reads.
func TestVaccinationShedSubjectLabelSummarisesMultipleVaccines(t *testing.T) {
	administeredAt := time.Date(2026, 8, 5, 12, 39, 0, 0, time.UTC)
	completionsFor := func(labels ...string) []vaccinationdomain.SubmissionCompletion {
		out := make([]vaccinationdomain.SubmissionCompletion, 0, len(labels))
		for i, label := range labels {
			out = append(out, vaccinationdomain.SubmissionCompletion{
				CompletionID: "c-" + label, SubmissionID: "sub-1", GoatID: "goat-" + label,
				ShedID: "shed-1", ShedLabel: "Sumathi 1 - Part 3", ParkID: "park-1",
				VaccineLabel: label, AdministeredAt: administeredAt.Add(time.Duration(i) * time.Minute),
			})
		}
		return out
	}
	run := func(t *testing.T, completions []vaccinationdomain.SubmissionCompletion) string {
		t.Helper()
		rec := &captureVaccinationRecorder{count: len(completions), completions: completions}
		producer := &captureVerificationProducer{}
		bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
		shedID := "shed-1"
		if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1",
			sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"},
			sopdomain.SubmissionSummary{SubmissionID: "sub-1", SubmittedBy: "operator-1",
				ProofRefs: []sopdomain.ProofReference{{ProofID: "shed-video-1", SubjectType: "shed", SubjectID: &shedID}}},
		); err != nil {
			t.Fatalf("vaccination submit: %v", err)
		}
		return deref(producer.last.SubjectLabel)
	}

	if got := run(t, completionsFor("PPR", "ET+TT")); got != "Sumathi 1 - Part 3 · ET+TT + PPR · 2 goats" {
		t.Fatalf("two-vaccine subject label = %q, want both vaccines named in a stable order", got)
	}
	// Past two the list stops being scannable, so it collapses to a count rather than growing.
	if got := run(t, completionsFor("PPR", "ET+TT", "Blue Tongue", "Goat Pox")); !strings.Contains(got, "4 vaccines") {
		t.Fatalf("four-vaccine subject label = %q, want it collapsed to a count", got)
	}
}

func TestVaccinationPerAnimalSubjectLabelNamesTheVaccine(t *testing.T) {
	administeredAt := time.Date(2026, 8, 5, 12, 39, 0, 0, time.UTC)
	rec := &captureVaccinationRecorder{
		count: 1,
		completions: []vaccinationdomain.SubmissionCompletion{
			{CompletionID: "c-1", SubmissionID: "sub-1", GoatID: "goat-1", GoatLabel: "G-006004",
				ShedID: "shed-1", ShedLabel: "Gandhi 1 - Part 2", ParkID: "park-1",
				VaccineLabel: "PPR · Booster", ProofRefIDs: []string{"goat-video-1"}, AdministeredAt: administeredAt},
		},
	}
	producer := &captureVerificationProducer{}
	bridge := NewVaccinationSubmissionBridge(rec).WithVerificationProducer(producer)
	if err := bridge.OnTaskSubmitted(context.Background(), "tenant-1",
		sopdomain.TaskSummary{TaskID: "task-1", SOPCode: "vaccination.drive"},
		sopdomain.SubmissionSummary{SubmissionID: "sub-1", SubmittedBy: "operator-1"},
	); err != nil {
		t.Fatalf("vaccination submit: %v", err)
	}
	if got := deref(producer.last.SubjectLabel); got != "Gandhi 1 - Part 2 · G-006004 · PPR · Booster" {
		t.Fatalf("per-animal subject label = %q, want shed+partition, animal, and vaccine", got)
	}
}

// The formatter is the only thing standing between a config token and the verifier's screen.
func TestVaccinationSubjectLabelNeverCarriesARawDoseCode(t *testing.T) {
	for _, doseCode := range []string{"et_tt_adult_w2", "ppr_booster", "blue_tongue_first", "goat_pox"} {
		if got := vaccinationdomain.DoseDisplayLabel("Preventive Care Vaccination Matrix", doseCode); strings.Contains(got, "_") {
			t.Fatalf("DoseDisplayLabel(%q) = %q, want a human label with no raw token", doseCode, got)
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
