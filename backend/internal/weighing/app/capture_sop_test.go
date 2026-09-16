package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16). Service half: the submit is
// judged against the PINNED version's slots and questions, an older app is accepted on the
// seeded slot with the rest reported as not captured, and the verifier item names every
// capture, says which KIND of weigh it is, and groups the answers by section.

const (
	captureCampaign = "00000000-0000-4000-8000-000000000501"
	captureBucket   = "00000000-0000-4000-8000-000000000801"
	proofFive       = "00000000-0000-4000-8000-000000000705"
	proofSix        = "00000000-0000-4000-8000-000000000706"
)

// slotAnimalRepo echoes what the service hands the store (the real write stores and returns
// the same maps) and records the refs the reuse guard was asked about.
type slotAnimalRepo struct {
	fakeRepo
	recorded      domain.RecordAnimalObservation
	rejectedAsked []string
	proofKinds    map[string]string
	calls         int
}

func (r *slotAnimalRepo) AnimalProofWasRejected(_ context.Context, _, _ string, refs []string, _ string) (bool, error) {
	r.rejectedAsked = append([]string(nil), refs...)
	return false, nil
}

func (r *slotAnimalRepo) RecordAnimalObservation(_ context.Context, cmd domain.RecordAnimalObservation) (domain.Observation, error) {
	r.recorded = cmd
	r.calls++
	return domain.Observation{
		ObservationID: "00000000-0000-4000-8000-000000000901", CampaignID: cmd.CampaignID, CampaignShedID: cmd.CampaignShedID,
		ScannedIdentifier: cmd.ScannedIdentifier, WeightKg: cmd.WeightKg, ProofArtifactID: cmd.PrimaryProofRef,
		Proofs: cmd.NormalizedProofs, Answers: cmd.NormalizedAnswers, ProofKinds: r.proofKinds,
		ExpectedLocationID: testShed, AcceptedAt: time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC),
	}, nil
}

type slotShedRepo struct {
	fakeRepo
	recorded domain.RecordShedObservation
}

func (r *slotShedRepo) RecordShedObservation(_ context.Context, cmd domain.RecordShedObservation) (domain.Observation, error) {
	r.recorded = cmd
	return domain.Observation{
		ObservationID: "00000000-0000-4000-8000-000000000902", CampaignID: cmd.CampaignID, CampaignShedID: cmd.CampaignShedID,
		WeightKg: cmd.WeightKg, AnimalCount: 12, ProofArtifactID: cmd.ProofArtifactID, ProofArtifactIDs: append([]string(nil), cmd.ProofArtifactIDs...),
		ProofSlots: cmd.NormalizedProofs, Answers: cmd.NormalizedAnswers, AcceptedAt: time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC),
	}, nil
}

func twoSlotIndividualRules(version int) ports.StaticRules {
	rules := rulesWithMode(version, domain.RemovalModeRequired)
	rules.Rules.Capture.Individual.Proofs = []domain.RemovalProofSlot{
		{Key: "animal_video", Title: "Weighing video", Kind: "video", Required: true},
		{Key: "scale_photo", Title: "Scale display", Kind: "photo", Required: true},
		{Key: "ear_tag", Title: "Ear tag close-up", Kind: "either", Required: false},
	}
	rules.Rules.Capture.Individual.Questions = []domain.SOPQuestion{
		{ID: "limp", Kind: domain.SOPQuestionChoice, Title: "Limping?", Required: true, Options: []domain.SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}},
	}
	return rules
}

func animalCmd() domain.RecordAnimalObservation {
	return domain.RecordAnimalObservation{
		CampaignID: captureCampaign, CampaignShedID: captureBucket, ScannedIdentifier: "RFID-1", WeightKg: 21.5, IdempotencyKey: "scan-slot-1",
	}
}

var operatorActor = domain.Actor{TenantID: testTenant, UserID: testOp, Roles: []string{permissions.RoleOperator}}

func TestRecordAnimalObservationEnqueuesOneAnimalItemForTwoProofs(t *testing.T) {
	repo := &slotAnimalRepo{}
	enq := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enq).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(2))
	cmd := animalCmd()
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": proofOne, "scale_photo": proofTwo}
	cmd.Answers = domain.SOPAnswers{"limp": json.RawMessage(`"no"`)}
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("err = %v", err)
	}
	if enq.calls != 1 || enq.received.Category != domain.VerificationRefTypeAnimal {
		t.Fatalf("calls=%d category=%q, want ONE animal item (two proofs is not a pen)", enq.calls, enq.received.Category)
	}
	if got := strings.Join(enq.received.MediaRefs, ","); got != proofOne+","+proofTwo {
		t.Fatalf("media refs = %s, want primary first then the photo", got)
	}
	if repo.recorded.PrimaryProofRef != proofOne || repo.recorded.ProofArtifactID != proofOne {
		t.Fatalf("primary = %q / %q, want %s stored on proof_artifact_id", repo.recorded.PrimaryProofRef, repo.recorded.ProofArtifactID, proofOne)
	}
	if repo.recorded.SlotKinds["scale_photo"] != "photo" || repo.recorded.SlotKinds["ear_tag"] != "either" {
		t.Fatalf("slot kinds = %v", repo.recorded.SlotKinds)
	}
	if !strings.HasPrefix(enq.received.IdempotencyKey, "weighing:"+domain.VerificationRefTypeAnimal+":") {
		t.Fatalf("idempotency key = %q", enq.received.IdempotencyKey)
	}
}

func TestRecordAnimalObservationCarriesSlotMediaMetaAndAnswerRows(t *testing.T) {
	repo := &slotAnimalRepo{proofKinds: map[string]string{proofOne: "video", proofTwo: "photo", proofThree: "photo"}}
	enq := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enq).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(2))
	cmd := animalCmd()
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": proofOne, "scale_photo": proofTwo, "ear_tag": proofThree}
	cmd.Answers = domain.SOPAnswers{"limp": json.RawMessage(`"yes"`)}
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("err = %v", err)
	}
	meta := enq.received.MediaMeta
	if len(meta) != 3 || meta[0].Label != "Weighing video" || meta[0].Kind != "video" || meta[1].Label != "Scale display" || meta[1].Kind != "photo" || meta[2].Label != "Ear tag close-up" || meta[2].Kind != "photo" {
		t.Fatalf("media meta = %+v, want the slot titles with the register's kind (either -> photo)", meta)
	}
	rows := enq.received.ContextRows
	if len(rows) != 2 || rows[0].Group != captureGroupWeighing || rows[0].Label != captureKindLabel || rows[0].Value != captureKindPerAnimal {
		t.Fatalf("rows = %+v, want a leading 'Weighed as: Per animal' row in group Weighing", rows)
	}
	if rows[1].Group != captureGroupPerAnimalAnswers || rows[1].Label != "Limping?" || rows[1].Value != "Yes" {
		t.Fatalf("answer row = %+v, want grouped under %q", rows[1], captureGroupPerAnimalAnswers)
	}
}

func TestRecordAnimalObservationLegacyProofMapsOntoSeededSlot(t *testing.T) {
	repo := &slotAnimalRepo{}
	enq := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enq) // unwired: the seed
	cmd := animalCmd()
	cmd.ProofArtifactID = proofOne
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !repo.recorded.LegacyShape || repo.recorded.NormalizedProofs[domain.IndividualProofAnimalVideo] != proofOne || repo.recorded.PrimaryProofRef != proofOne {
		t.Fatalf("recorded = %+v, want the legacy proof on the seeded slot", repo.recorded)
	}
	if len(repo.recorded.Proofs) != 0 {
		t.Fatal("the CLIENT map must stay empty so the legacy fingerprint is unchanged")
	}
	if meta := enq.received.MediaMeta; len(meta) != 1 || meta[0].Label != "Weighing video" || meta[0].Kind != "video" {
		t.Fatalf("meta = %+v", meta)
	}
	if rows := enq.received.ContextRows; len(rows) != 1 || rows[0].Value != captureKindPerAnimal {
		t.Fatalf("rows = %+v, want only the capture-kind row (nothing missing on the seed)", rows)
	}
}

func TestRecordAnimalObservationOlderAppIsAcceptedWithNotCapturedRows(t *testing.T) {
	repo := &slotAnimalRepo{}
	enq := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enq).WithSOPRules(twoSlotIndividualRules(3), pinnedVersion(3))
	cmd := animalCmd()
	cmd.ProofArtifactID = proofOne // an older app: no slot map, no answers
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("older app must be accepted, err = %v", err)
	}
	rows := enq.received.ContextRows
	want := []string{"Weighed as=Per animal", "Scale display=" + domain.NotCapturedOlderApp, "Limping?=" + domain.NotCapturedOlderApp}
	got := []string{}
	for _, r := range rows {
		got = append(got, r.Label+"="+r.Value)
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	// A version whose slots cannot take a video at all STILL accepts the older app (program decision
	// 7: never force an update): its video is stored under the reserved older-app key, reaches the
	// verifier as "Recorded on an older app", and the photo it could not send reads not captured.
	photoOnly := rulesWithMode(4, domain.RemovalModeRequired)
	photoOnly.Rules.Capture.Individual.Proofs = []domain.RemovalProofSlot{{Key: "scale_photo", Title: "Scale display", Kind: "photo", Required: true}}
	photoRepo := &slotAnimalRepo{}
	enq = &captureVerificationEnqueuer{}
	service = NewService(photoRepo).WithVerificationEnqueuer(enq).WithSOPRules(photoOnly, pinnedVersion(4))
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("photo-only version must accept the older app, err = %v", err)
	}
	if photoRepo.recorded.NormalizedProofs[domain.OlderAppVideoKey] != proofOne || photoRepo.recorded.PrimaryProofRef != proofOne || photoRepo.recorded.SlotKinds[domain.OlderAppVideoKey] != domain.RemovalProofKindVideo {
		t.Fatalf("recorded = %+v, want the video under %s judged as a video", photoRepo.recorded, domain.OlderAppVideoKey)
	}
	if m := enq.received.MediaMeta; len(m) != 1 || m[0].Label != domain.OlderAppVideoLabel || m[0].Kind != "video" || strings.Join(enq.received.MediaRefs, ",") != proofOne {
		t.Fatalf("meta = %+v refs = %v", m, enq.received.MediaRefs)
	}
	got = got[:0]
	for _, r := range enq.received.ContextRows {
		got = append(got, r.Label+"="+r.Value)
	}
	if strings.Join(got, "|") != "Weighed as=Per animal|Scale display="+domain.NotCapturedOlderApp {
		t.Fatalf("photo-only rows = %v", got)
	}
}

func TestRecordAnimalObservationJudgesAgainstThePinnedVersion(t *testing.T) {
	repo := &slotAnimalRepo{}
	service := NewService(repo).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(2))
	cmd := animalCmd()
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": proofOne}
	cmd.Answers = domain.SOPAnswers{"limp": json.RawMessage(`"no"`)}
	var pe *domain.ProofError
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); !errors.Is(err, domain.ErrCaptureProofInvalid) || !errors.As(err, &pe) || pe.SlotKey != "scale_photo" {
		t.Fatalf("new-shaped request missing the compulsory photo err = %v, want strict refusal naming scale_photo", err)
	}
	if repo.calls != 0 {
		t.Fatal("store must not be reached")
	}
	cmd.Proofs["scale_photo"] = proofTwo
	delete(cmd.Answers, "limp")
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); !errors.Is(err, domain.ErrCaptureAnswerInvalid) {
		t.Fatalf("required question unanswered err = %v, want ErrCaptureAnswerInvalid", err)
	}
	// Pinned to the SEED: scale_photo is not a slot there, so the same request is refused.
	service = NewService(repo).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(0))
	cmd.Answers = domain.SOPAnswers{}
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); !errors.As(err, &pe) || pe.SlotKey != "scale_photo" {
		t.Fatalf("under the seed err = %v, want unknown slot scale_photo", err)
	}
	// A bad ref is still a plain invalid argument.
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": "not-a-uuid"}
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("malformed ref err = %v", err)
	}
}

func lumpRules(version int) ports.StaticRules {
	rules := rulesWithMode(version, domain.RemovalModeRequired)
	rules.Rules.Capture.LumpSum.Proofs = []domain.CountedProofSlot{
		{Key: "pen_video", Title: "Weighing video", Kind: "video", Min: 1, Max: 3},
		{Key: "scale_photo", Title: "Scale display photo", Kind: "photo", Min: 1, Max: 1},
	}
	rules.Rules.Capture.LumpSum.Questions = []domain.SOPQuestion{
		{ID: "all_on", Kind: domain.SOPQuestionChoice, Title: "Every animal on the scale?", Required: true, Options: []domain.SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}},
	}
	return rules
}

func shedCmd() domain.RecordShedObservation {
	return domain.RecordShedObservation{CampaignID: captureCampaign, CampaignShedID: captureBucket, WeightKg: 250, IdempotencyKey: "shed-slot-1"}
}

func TestRecordShedObservationMediaMetaNamesEveryVideo(t *testing.T) {
	repo := &slotShedRepo{}
	enq := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enq).WithSOPRules(lumpRules(5), pinnedVersion(5))
	cmd := shedCmd()
	cmd.Proofs = domain.LumpSumProofRefs{"pen_video": {proofOne, proofTwo, proofThree}, "scale_photo": {proofFive}}
	cmd.Answers = domain.SOPAnswers{"all_on": json.RawMessage(`"yes"`)}
	if _, err := service.RecordShedObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("err = %v", err)
	}
	if enq.received.Category != domain.VerificationRefTypeShed {
		t.Fatalf("category = %q", enq.received.Category)
	}
	if got := strings.Join(enq.received.MediaRefs, ","); got != strings.Join([]string{proofOne, proofTwo, proofThree, proofFive}, ",") {
		t.Fatalf("media refs = %s", got)
	}
	labels := []string{}
	for _, m := range enq.received.MediaMeta {
		labels = append(labels, m.Label+"/"+m.Kind)
	}
	if strings.Join(labels, ",") != "Weighing video 1 of 3/video,Weighing video 2 of 3/video,Weighing video 3 of 3/video,Scale display photo/photo" {
		t.Fatalf("meta = %v", labels)
	}
	rows := enq.received.ContextRows
	if len(rows) != 2 || rows[0].Group != captureGroupWeighing || rows[0].Value != captureKindWholePen || rows[1].Group != captureGroupWholePenAnswers || rows[1].Value != "Yes" {
		t.Fatalf("rows = %+v", rows)
	}
	if strings.Join(repo.recorded.ProofArtifactIDs, ",") != strings.Join(enq.received.MediaRefs, ",") || repo.recorded.ProofArtifactID != proofOne {
		t.Fatalf("stored flat list / primary = %v / %q must follow slot order", repo.recorded.ProofArtifactIDs, repo.recorded.ProofArtifactID)
	}
	if repo.recorded.Ordered[3].SlotKey != "scale_photo" || repo.recorded.SlotKinds["scale_photo"] != "photo" {
		t.Fatalf("ordered = %+v kinds = %v", repo.recorded.Ordered, repo.recorded.SlotKinds)
	}
}

func TestRecordShedObservationSlotCountOutsideWindowIsNamed(t *testing.T) {
	repo := &slotShedRepo{}
	service := NewService(repo).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(lumpRules(5), pinnedVersion(5))
	cmd := shedCmd()
	cmd.Proofs = domain.LumpSumProofRefs{"pen_video": {proofOne, proofTwo, proofThree, proofFive}, "scale_photo": {proofSix}}
	cmd.Answers = domain.SOPAnswers{"all_on": json.RawMessage(`"yes"`)}
	var pe *domain.ProofError
	if _, err := service.RecordShedObservation(context.Background(), operatorActor, cmd); !errors.Is(err, domain.ErrCaptureProofInvalid) || !errors.As(err, &pe) || pe.SlotKey != "pen_video" {
		t.Fatalf("four in a max-3 slot err = %v, want ErrCaptureProofInvalid naming pen_video", err)
	}
	cmd.Proofs = domain.LumpSumProofRefs{"pen_video": {proofOne}}
	if _, err := service.RecordShedObservation(context.Background(), operatorActor, cmd); !errors.As(err, &pe) || pe.SlotKey != "scale_photo" {
		t.Fatalf("missing compulsory photo err = %v", err)
	}
	cmd.Proofs = domain.LumpSumProofRefs{"pen_video": {proofOne}, "scale_photo": {proofSix}}
	cmd.Answers = nil
	if _, err := service.RecordShedObservation(context.Background(), operatorActor, cmd); !errors.Is(err, domain.ErrCaptureAnswerInvalid) {
		t.Fatalf("unanswered whole-pen question err = %v", err)
	}
	// An OLDER app's flat list is accepted on the pen_video slot; the photo reads not captured.
	enq := &captureVerificationEnqueuer{}
	service = NewService(repo).WithVerificationEnqueuer(enq).WithSOPRules(lumpRules(5), pinnedVersion(5))
	legacy := shedCmd()
	legacy.ProofArtifactIDs = []string{proofOne, proofTwo}
	if _, err := service.RecordShedObservation(context.Background(), operatorActor, legacy); err != nil {
		t.Fatalf("older app err = %v", err)
	}
	if !repo.recorded.LegacyShape || len(repo.recorded.Proofs) != 0 || strings.Join(repo.recorded.NormalizedProofs["pen_video"], ",") != proofOne+","+proofTwo {
		t.Fatalf("legacy recorded = %+v", repo.recorded)
	}
	got := []string{}
	for _, r := range enq.received.ContextRows {
		got = append(got, r.Label+"="+r.Value)
	}
	if strings.Join(got, "|") != "Weighed as=Whole pen|Scale display photo="+domain.NotCapturedOlderApp+"|Every animal on the scale?="+domain.NotCapturedOlderApp {
		t.Fatalf("legacy rows = %v", got)
	}
	if m := enq.received.MediaMeta; len(m) != 2 || m[0].Label != "Weighing video 1 of 2" {
		t.Fatalf("legacy meta = %+v", m)
	}
}

func TestAnimalReplayReenqueuesWithTheSameMeta(t *testing.T) {
	repo := &slotAnimalRepo{proofKinds: map[string]string{proofOne: "video", proofThree: "photo"}}
	enq := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enq).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(2))
	cmd := animalCmd()
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": proofOne, "scale_photo": proofThree, "ear_tag": proofThree}
	cmd.Answers = domain.SOPAnswers{"limp": json.RawMessage(`"no"`)}
	// same ref twice is refused first; fix and submit twice
	cmd.Proofs["scale_photo"] = proofTwo
	repo.proofKinds[proofTwo] = "photo"
	first, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	firstMeta := enq.received.MediaMeta
	firstKey := enq.received.IdempotencyKey
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("replay err = %v", err)
	}
	if enq.received.IdempotencyKey != firstKey || len(enq.received.MediaMeta) != len(firstMeta) || enq.received.MediaMeta[2].Kind != "photo" {
		t.Fatalf("replay meta/key drifted: %+v vs %+v", enq.received.MediaMeta, firstMeta)
	}
	if first.ProofKinds[proofThree] != "photo" {
		t.Fatalf("observation must carry the register's kinds: %v", first.ProofKinds)
	}
}

func TestRejectedProofReuseCoversEverySlot(t *testing.T) {
	repo := &slotAnimalRepo{}
	service := NewService(repo).WithVerificationEnqueuer(&captureVerificationEnqueuer{}).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(2))
	cmd := animalCmd()
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": proofOne, "scale_photo": proofTwo}
	cmd.Answers = domain.SOPAnswers{"limp": json.RawMessage(`"no"`)}
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("err = %v", err)
	}
	if strings.Join(repo.rejectedAsked, ",") != proofOne+","+proofTwo {
		t.Fatalf("reuse guard asked about %v, want every slot's ref", repo.rejectedAsked)
	}
}

// Maintainer clarification 2026-09-16: the two capture kinds are told apart on the verifier
// item itself -- the leading context row and the answer group differ per kind, so /verify and
// the Android verify detail can never render a per-animal item as a whole-pen one.
func TestAnimalAndPenVerifierItemsAreToldApart(t *testing.T) {
	animalEnq := &captureVerificationEnqueuer{}
	service := NewService(&slotAnimalRepo{}).WithVerificationEnqueuer(animalEnq).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(2))
	cmd := animalCmd()
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": proofOne, "scale_photo": proofTwo}
	cmd.Answers = domain.SOPAnswers{"limp": json.RawMessage(`"no"`)}
	if _, err := service.RecordAnimalObservation(context.Background(), operatorActor, cmd); err != nil {
		t.Fatalf("err = %v", err)
	}
	penEnq := &captureVerificationEnqueuer{}
	service = NewService(&slotShedRepo{}).WithVerificationEnqueuer(penEnq).WithSOPRules(lumpRules(5), pinnedVersion(5))
	scmd := shedCmd()
	scmd.Proofs = domain.LumpSumProofRefs{"pen_video": {proofOne}, "scale_photo": {proofSix}}
	scmd.Answers = domain.SOPAnswers{"all_on": json.RawMessage(`"no"`)}
	if _, err := service.RecordShedObservation(context.Background(), operatorActor, scmd); err != nil {
		t.Fatalf("err = %v", err)
	}
	a, p := animalEnq.received.ContextRows, penEnq.received.ContextRows
	if a[0].Value == p[0].Value || a[0].Value != captureKindPerAnimal || p[0].Value != captureKindWholePen {
		t.Fatalf("capture-kind rows must differ: %q vs %q", a[0].Value, p[0].Value)
	}
	if a[1].Group == p[1].Group || a[1].Group != captureGroupPerAnimalAnswers || p[1].Group != captureGroupWholePenAnswers {
		t.Fatalf("answer groups must differ: %q vs %q", a[1].Group, p[1].Group)
	}
	if animalEnq.received.Category == penEnq.received.Category {
		t.Fatal("source ref types must differ")
	}
}

type leadershipEvidenceRepo struct {
	fakeRepo
	result domain.LeadershipShedVideos
}

func (r *leadershipEvidenceRepo) GetLeadershipShedVideos(context.Context, string, string, string, string, int, ports.CampaignAccess) (domain.LeadershipShedVideos, error) {
	return r.result, nil
}

// Leadership's "N of M" and the capture titles come from the task's PINNED rules, not the fixed
// proof-policy 5 / the registry's numbered video.
func TestLeadershipEvidenceUsesThePinnedWholePenCeilingAndTitles(t *testing.T) {
	director := domain.Actor{TenantID: testTenant, UserID: testActor, Roles: []string{permissions.RoleGrowthDirector}}
	repo := &leadershipEvidenceRepo{result: domain.LeadershipShedVideos{
		CampaignID: captureCampaign, CampaignShedID: captureBucket, WeighingCategory: domain.CategoryPerShedPartition, SOPVersion: 5, MaxShedVideos: 5,
		LumpSum: &domain.Observation{ObservationID: "o", ProofArtifactID: proofOne, ProofArtifactIDs: []string{proofOne, proofTwo, proofSix},
			ProofSlots: domain.LumpSumProofRefs{"pen_video": {proofOne, proofTwo}, "scale_photo": {proofSix}}, ProofKinds: map[string]string{proofOne: "video", proofTwo: "video", proofSix: "photo"}},
	}}
	service := NewService(repo).WithSOPRules(lumpRules(5), pinnedVersion(5))
	got, err := service.GetLeadershipShedVideos(context.Background(), director, captureCampaign, captureBucket, "", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.MaxShedVideos != 4 { // pen_video max 3 + scale_photo max 1
		t.Fatalf("MaxShedVideos = %d, want the pinned Σmax 4", got.MaxShedVideos)
	}
	m := got.LumpSum.Media
	if len(m) != 3 || m[0].Label != "Weighing video 1 of 2" || m[1].Label != "Weighing video 2 of 2" || m[2].Label != "Scale display photo" || m[2].MimeType != "image/jpeg" {
		t.Fatalf("media = %+v", m)
	}
	// A pre-slot row (no slot map) reads as the seeded slot, numbered, under the seed.
	repo.result.SOPVersion = 0
	repo.result.LumpSum = &domain.Observation{ObservationID: "o", ProofArtifactID: proofOne, ProofArtifactIDs: []string{proofOne, proofTwo}}
	got, err = service.GetLeadershipShedVideos(context.Background(), director, captureCampaign, captureBucket, "", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.MaxShedVideos != 5 || got.LumpSum.Media[1].Label != "Weighing video 2 of 2" {
		t.Fatalf("seed: max=%d media=%+v", got.MaxShedVideos, got.LumpSum.Media)
	}
	// Per animal: primary first, titled by slot.
	repo.result = domain.LeadershipShedVideos{CampaignID: captureCampaign, WeighingCategory: domain.CategoryIndividualAnimal, SOPVersion: 2,
		Individual: []domain.Observation{{ObservationID: "a", ProofArtifactID: proofOne, ProofArtifactIDs: []string{proofOne, proofTwo}, Proofs: domain.IndividualProofRefs{"animal_video": proofOne, "scale_photo": proofTwo}, ProofKinds: map[string]string{proofTwo: "photo"}}}}
	service = NewService(repo).WithSOPRules(twoSlotIndividualRules(2), pinnedVersion(2))
	got, err = service.GetLeadershipShedVideos(context.Background(), director, captureCampaign, captureBucket, "", 10)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if im := got.Individual[0].Media; len(im) != 2 || im[0].Label != "Weighing video" || im[1].Label != "Scale display" || im[1].MimeType != "image/jpeg" {
		t.Fatalf("individual media = %+v", im)
	}
}

// A whole-pen document with only photo slots still accepts an older app's video (program decision 7).
func TestRecordShedObservationOlderAppVideoOnAPhotoOnlyVersion(t *testing.T) {
	rules := rulesWithMode(6, domain.RemovalModeRequired)
	rules.Rules.Capture.LumpSum.Proofs = []domain.CountedProofSlot{{Key: "scale_photo", Title: "Scale display photo", Kind: "photo", Min: 1, Max: 1}}
	repo := &slotShedRepo{}
	enq := &captureVerificationEnqueuer{}
	service := NewService(repo).WithVerificationEnqueuer(enq).WithSOPRules(rules, pinnedVersion(6))
	legacy := shedCmd()
	legacy.ProofArtifactIDs = []string{proofOne}
	if _, err := service.RecordShedObservation(context.Background(), operatorActor, legacy); err != nil {
		t.Fatalf("older app on a photo-only version err = %v", err)
	}
	if strings.Join(repo.recorded.NormalizedProofs[domain.OlderAppVideoKey], ",") != proofOne || repo.recorded.SlotKinds[domain.OlderAppVideoKey] != domain.RemovalProofKindVideo {
		t.Fatalf("recorded = %+v", repo.recorded)
	}
	if m := enq.received.MediaMeta; len(m) != 1 || m[0].Label != domain.OlderAppVideoLabel || m[0].Kind != "video" {
		t.Fatalf("meta = %+v", m)
	}
	got := []string{}
	for _, r := range enq.received.ContextRows {
		got = append(got, r.Label+"="+r.Value)
	}
	if strings.Join(got, "|") != "Weighed as=Whole pen|Scale display photo="+domain.NotCapturedOlderApp {
		t.Fatalf("rows = %v", got)
	}
}
