package app

// A weighing verification item MUST name the animal and its recorded weight.
//
// The verifier's whole job is deciding whether the video matches the weight. Before this,
// every individual weighing item carried the hardcoded literal "individual animal weight"
// as its subject: fifteen items from the same shed rendered byte-identical, and the weight
// under review was never shown to the person reviewing it. An operator who typed 120 kg
// instead of 12 kg produced a correct-looking video of a goat on a scale that no verifier
// could catch.
//
// The scanned tag and the weight are both already on the observation the enqueue site
// holds (repository.go:1601 / :1830 return them), so the subject is composed there and both
// clients simply render it -- backend owns the label.
//
// Free-flow rule: the scanned identifier IS the identity. Nothing here resolves it to a
// goat, and lump-sum carries no per-animal identity at all -- only its total and count.

import (
	"context"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

func TestRecordAnimalObservationVerificationSubjectNamesTagAndWeight(t *testing.T) {
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(&animalObservationRepo{}).WithVerificationEnqueuer(enqueuer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
		CampaignID:        "00000000-0000-4000-8000-000000000501",
		CampaignShedID:    "00000000-0000-4000-8000-000000000801",
		ScannedIdentifier: "901007000504407",
		WeightKg:          12,
		ProofArtifactID:   proofOne,
		IdempotencyKey:    "scan-subject-label-1",
	}); err != nil {
		t.Fatalf("record animal observation: %v", err)
	}
	got := enqueuer.received.SubjectLabel
	if !strings.Contains(got, "901007000504407") {
		t.Fatalf("verification subject = %q, want it to name the scanned tag 901007000504407", got)
	}
	if !strings.Contains(got, "12.0 kg") {
		t.Fatalf("verification subject = %q, want it to carry the recorded weight 12.0 kg", got)
	}
}

// Two animals weighed in the same shed must not render as the same row. This is the defect
// the verifier actually saw: a queue of identical lines with nothing to tell them apart.
func TestRecordAnimalObservationVerificationSubjectsAreDistinguishableWithinAShed(t *testing.T) {
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}
	capture := func(tag string, weight float64, key string) string {
		enqueuer := &captureVerificationEnqueuer{}
		service := NewService(&animalObservationRepo{}).WithVerificationEnqueuer(enqueuer)
		if _, err := service.RecordAnimalObservation(context.Background(), operator, domain.RecordAnimalObservation{
			CampaignID:        "00000000-0000-4000-8000-000000000501",
			CampaignShedID:    "00000000-0000-4000-8000-000000000801",
			ScannedIdentifier: tag,
			WeightKg:          weight,
			ProofArtifactID:   proofOne,
			IdempotencyKey:    key,
		}); err != nil {
			t.Fatalf("record animal observation: %v", err)
		}
		return enqueuer.received.SubjectLabel
	}

	first := capture("901007000504332", 15, "scan-subject-label-a")
	second := capture("901007000504392", 17, "scan-subject-label-b")
	if first == second {
		t.Fatalf("two animals in the same shed produced the same verification subject %q", first)
	}
}

func TestRecordShedObservationVerificationSubjectNamesTotalWeightAndCount(t *testing.T) {
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(&shedObservationRepo{}).WithVerificationEnqueuer(enqueuer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID:       "00000000-0000-4000-8000-000000000501",
		CampaignShedID:   "00000000-0000-4000-8000-000000000801",
		WeightKg:         250,
		AnimalCount:      10,
		ProofArtifactID:  proofOne,
		ProofArtifactIDs: []string{proofOne, proofTwo},
		IdempotencyKey:   "shed-subject-label-1",
	}); err != nil {
		t.Fatalf("record shed observation: %v", err)
	}
	got := enqueuer.received.SubjectLabel
	if !strings.Contains(got, "250.0 kg") {
		t.Fatalf("verification subject = %q, want it to carry the total weight 250.0 kg", got)
	}
	if !strings.Contains(got, "10 goats") {
		t.Fatalf("verification subject = %q, want it to carry the animal count 10 goats", got)
	}
}

// Lump-sum has no per-animal identity in free-flow. The subject must not invent one.
func TestRecordShedObservationVerificationSubjectInventsNoAnimalIdentity(t *testing.T) {
	enqueuer := &captureVerificationEnqueuer{}
	service := NewService(&shedObservationRepo{}).WithVerificationEnqueuer(enqueuer)
	operator := domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

	if _, err := service.RecordShedObservation(context.Background(), operator, domain.RecordShedObservation{
		CampaignID:       "00000000-0000-4000-8000-000000000501",
		CampaignShedID:   "00000000-0000-4000-8000-000000000801",
		WeightKg:         250,
		AnimalCount:      10,
		ProofArtifactID:  proofOne,
		ProofArtifactIDs: []string{proofOne, proofTwo},
		IdempotencyKey:   "shed-subject-label-2",
	}); err != nil {
		t.Fatalf("record shed observation: %v", err)
	}
	if got := strings.ToLower(enqueuer.received.SubjectLabel); strings.Contains(got, "tag") {
		t.Fatalf("lump-sum verification subject = %q, want no per-animal tag", enqueuer.received.SubjectLabel)
	}
}
