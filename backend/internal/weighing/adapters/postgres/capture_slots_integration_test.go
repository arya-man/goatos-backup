package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// THE WEIGH CAPTURES ARE AUTHORED (maintainer decision 2026-09-16): the store half. The evidence
// row keeps {slot: ref} and the answers, judges each capture's KIND against its slot in the
// write transaction, keys a new evidence round on the whole slot map, and a whole-pen row keeps
// each capture's slot key at positions up to ten.

const (
	slotPhotoProof     = "00000000-0000-4000-8000-000000009701"
	slotPhotoProofTwo  = "00000000-0000-4000-8000-000000009702"
	slotPenPhotoProof  = "00000000-0000-4000-8000-000000009703"
	slotPenVideoFour   = "00000000-0000-4000-8000-000000009704"
	slotPenVideoFive   = "00000000-0000-4000-8000-000000009705"
	slotPenVideoSix    = "00000000-0000-4000-8000-000000009706"
	slotPenPhotoProof2 = "00000000-0000-4000-8000-000000009707"
)

func twoSlotRules() domain.Rules {
	r := domain.SeededRules()
	r.Version = 9
	r.Capture.Individual.Proofs = []domain.RemovalProofSlot{
		{Key: "animal_video", Title: "Weighing video", Kind: "video", Required: true},
		{Key: "scale_photo", Title: "Scale display", Kind: "photo", Required: true},
	}
	r.Capture.Individual.Questions = []domain.SOPQuestion{{ID: "limp", Kind: domain.SOPQuestionChoice, Title: "Limping?", Required: true, Options: []domain.SOPOption{{Value: "yes", Label: "Yes"}, {Value: "no", Label: "No"}}}}
	r.Capture.LumpSum.Proofs = []domain.CountedProofSlot{
		{Key: "pen_video", Title: "Weighing video", Kind: "video", Min: 1, Max: 5},
		{Key: "scale_photo", Title: "Scale display photo", Kind: "photo", Min: 1, Max: 2},
	}
	return r
}

// judgedAnimal fills the derived fields the service fills (app.applyIndividualCaptureRules).
func judgedAnimal(t *testing.T, rules domain.Rules, cmd domain.RecordAnimalObservation) domain.RecordAnimalObservation {
	t.Helper()
	ordered, err := rules.ValidateIndividualProofRefs(cmd.Proofs)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	cmd.NormalizedProofs = domain.NormalizeIndividualProofRefs(cmd.Proofs)
	cmd.NormalizedAnswers = rules.NormalizeIndividualAnswers(cmd.Answers)
	cmd.OrderedRefs, cmd.PrimaryProofRef, cmd.ProofArtifactID = ordered, ordered[0], ordered[0]
	cmd.SlotKinds = rules.IndividualSlotKinds()
	return cmd
}

func judgedShed(t *testing.T, rules domain.Rules, cmd domain.RecordShedObservation) domain.RecordShedObservation {
	t.Helper()
	ordered, err := rules.ValidateLumpSumProofRefs(cmd.Proofs)
	if err != nil {
		t.Fatalf("judge: %v", err)
	}
	cmd.NormalizedProofs = domain.NormalizeLumpSumProofRefs(cmd.Proofs)
	cmd.NormalizedAnswers = rules.NormalizeLumpSumAnswers(cmd.Answers)
	cmd.Ordered = ordered
	cmd.SlotKinds = rules.LumpSumSlotKinds()
	cmd.ProofArtifactIDs = nil
	for _, c := range ordered {
		cmd.ProofArtifactIDs = append(cmd.ProofArtifactIDs, c.Ref)
	}
	cmd.ProofArtifactID = cmd.ProofArtifactIDs[0]
	return cmd
}

func animalSlotCmd(idem string) domain.RecordAnimalObservation {
	return domain.RecordAnimalObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, ScannedIdentifier: "RFID-SLOT-1",
		WeightKg: 22.5, IdempotencyKey: idem, RecordedBy: repoOperator,
		Proofs:  domain.IndividualProofRefs{"animal_video": repoExpectedShedProof, "scale_photo": slotPhotoProof},
		Answers: domain.SOPAnswers{"limp": json.RawMessage(`"no"`)},
	}
}

func TestAnimalObservationStoresSlotProofsAndAnswers(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, slotPhotoProof, "photo", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)
	rules := twoSlotRules()

	obs, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, animalSlotCmd("slot:first")))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if obs.ProofArtifactID != repoExpectedShedProof || obs.Proofs["scale_photo"] != slotPhotoProof || string(obs.Answers["limp"]) != `"no"` {
		t.Fatalf("returned obs = %+v", obs)
	}
	if obs.ProofKinds[slotPhotoProof] != "photo" || obs.ProofKinds[repoExpectedShedProof] != "video" {
		t.Fatalf("proof kinds must come from the register: %v", obs.ProofKinds)
	}
	var proofs, answers string
	if err := pool.QueryRow(ctx, `SELECT sop_proofs::text, sop_answers::text FROM weighing_observations WHERE tenant_id=$1::uuid AND observation_id=$2::uuid`, repoTenant, obs.ObservationID).Scan(&proofs, &answers); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if !strings.Contains(proofs, `"scale_photo": "`+slotPhotoProof+`"`) || !strings.Contains(answers, `"limp": "no"`) {
		t.Fatalf("stored sop_proofs=%s sop_answers=%s", proofs, answers)
	}
	// The roster read carries the slot map, answers and kinds back to the phone.
	page, err := repo.ListScopeRoster(ctx, repoTenant, repoCampaign, repoAnimalScope, "", 10)
	if err != nil {
		t.Fatalf("roster: %v", err)
	}
	if len(page.Observations) != 1 || page.Observations[0].Proofs["scale_photo"] != slotPhotoProof || page.Observations[0].ProofKinds[slotPhotoProof] != "photo" || string(page.Observations[0].Answers["limp"]) != `"no"` {
		t.Fatalf("roster obs = %+v", page.Observations)
	}
}

func TestAnimalRepostWithUnchangedSlotsIsNotANewRound(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, slotPhotoProof, "photo", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)
	rules := twoSlotRules()
	first, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, animalSlotCmd("slot:a")))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, animalSlotCmd("slot:b")))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Superseded || second.ObservationID != first.ObservationID || !second.AcceptedAt.Equal(first.AcceptedAt) {
		t.Fatalf("an identical re-post under a new key must not open a round: %+v vs %+v", second, first)
	}
}

func TestAnimalRepostChangingOnlyASecondarySlotOpensANewRound(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, slotPhotoProof, "photo", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	insertProof(t, ctx, pool, slotPhotoProofTwo, "photo", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)
	rules := twoSlotRules()
	first, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, animalSlotCmd("slot:a")))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	cmd := animalSlotCmd("slot:b")
	cmd.Proofs["scale_photo"] = slotPhotoProofTwo // same weight, same primary video, new photo
	second, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, cmd))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if !second.Superseded || second.ObservationID != first.ObservationID || second.Proofs["scale_photo"] != slotPhotoProofTwo {
		t.Fatalf("a changed secondary slot is a new evidence round: %+v", second)
	}
	// And a changed ANSWER alone is too.
	cmd = animalSlotCmd("slot:c")
	cmd.Proofs["scale_photo"] = slotPhotoProofTwo
	cmd.Answers = domain.SOPAnswers{"limp": json.RawMessage(`"yes"`)}
	third, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, cmd))
	if err != nil {
		t.Fatalf("third: %v", err)
	}
	if !third.Superseded || string(third.Answers["limp"]) != `"yes"` {
		t.Fatalf("a changed answer is a new evidence round: %+v", third)
	}
}

func TestPhotoSlotAcceptedAndVideoSlotRefusesPhoto(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, slotPhotoProof, "photo", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)
	rules := twoSlotRules()
	// A photo in the VIDEO slot: the register says photo, the slot says video -> refused.
	cmd := animalSlotCmd("slot:wrongkind")
	cmd.Proofs = domain.IndividualProofRefs{"animal_video": slotPhotoProof, "scale_photo": repoExpectedShedProof}
	if _, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, cmd)); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("photo in a video slot err = %v, want ErrInvalidArgument", err)
	}
	// An `either` slot takes the photo, and the register's kind rides the row.
	rules.Capture.Individual.Proofs[1].Kind = "either"
	cmd = animalSlotCmd("slot:either")
	obs, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, cmd))
	if err != nil {
		t.Fatalf("either slot: %v", err)
	}
	if obs.ProofKinds[slotPhotoProof] != "photo" {
		t.Fatalf("kinds = %v", obs.ProofKinds)
	}
}

func TestShedObservationStoresSlotKeysAndSixProofs(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	for _, id := range []string{slotPenVideoFour, slotPenVideoFive} {
		insertProof(t, ctx, pool, id, "video", "completed", "shed", repoPerShed, "shed", repoPerShed)
	}
	insertProof(t, ctx, pool, slotPenPhotoProof, "photo", "completed", "shed", repoPerShed, "shed", repoPerShed)
	insertProof(t, ctx, pool, slotPenPhotoProof2, "photo", "completed", "shed", repoPerShed, "shed", repoPerShed)
	repo := NewRepository(pool, 5*time.Second)
	rules := twoSlotRules()
	cmd := domain.RecordShedObservation{
		TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 412, IdempotencyKey: "shed:six", RecordedBy: repoOperator,
		Proofs:  domain.LumpSumProofRefs{"pen_video": {repoShedProof, repoShedProofTwo, repoShedProofThree, slotPenVideoFour}, "scale_photo": {slotPenPhotoProof, slotPenPhotoProof2}},
		Answers: domain.SOPAnswers{},
	}
	// A photo in the VIDEO slot is refused in the transaction (checked FIRST: a submit that
	// lands completes the bucket, and a later one is refused as immutable whatever it carries).
	bad := cmd
	bad.IdempotencyKey = "shed:bad"
	bad.Proofs = domain.LumpSumProofRefs{"pen_video": {slotPenPhotoProof}, "scale_photo": {slotPenPhotoProof2}}
	if _, err := repo.RecordShedObservation(ctx, judgedShed(t, rules, bad)); !errors.Is(err, ports.ErrInvalidArgument) {
		t.Fatalf("photo in a video slot err = %v, want ErrInvalidArgument", err)
	}
	obs, err := repo.RecordShedObservation(ctx, judgedShed(t, rules, cmd))
	if err != nil {
		t.Fatalf("record six: %v", err)
	}
	if len(obs.ProofArtifactIDs) != 6 || obs.ProofArtifactID != repoShedProof || len(obs.ProofSlots["scale_photo"]) != 2 || obs.ProofKinds[slotPenPhotoProof] != "photo" {
		t.Fatalf("obs = %+v", obs)
	}
	rows, err := pool.Query(ctx, `SELECT proof_position, COALESCE(slot_key,'') FROM weighing_shed_observation_proofs WHERE tenant_id=$1::uuid AND shed_observation_id=$2::uuid ORDER BY proof_position`, repoTenant, obs.ObservationID)
	if err != nil {
		t.Fatalf("read children: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var pos int
		var key string
		if err := rows.Scan(&pos, &key); err != nil {
			t.Fatal(err)
		}
		got = append(got, key)
	}
	if strings.Join(got, ",") != "pen_video,pen_video,pen_video,pen_video,scale_photo,scale_photo" {
		t.Fatalf("slot keys by position = %v", got)
	}
	// The leadership read carries the slot map and kinds.
	videos, err := repo.GetLeadershipShedVideos(ctx, repoTenant, repoCampaign, repoShedScope, "", 10, ports.CampaignAccess{Unrestricted: true})
	if err != nil {
		t.Fatalf("leadership: %v", err)
	}
	if videos.LumpSum == nil || len(videos.LumpSum.ProofSlots["pen_video"]) != 4 || videos.LumpSum.ProofKinds[slotPenPhotoProof2] != "photo" {
		t.Fatalf("leadership lump = %+v", videos.LumpSum)
	}
}

func TestScopeSubmitAcceptsPhotoPrimary(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedWeighingObservationFixture(t, ctx, pool)
	insertProof(t, ctx, pool, slotPhotoProof, "photo", "completed", "shed", repoExpectedShed, "shed", repoExpectedShed)
	repo := NewRepository(pool, 5*time.Second)
	rules := twoSlotRules()
	// A document whose FIRST compulsory slot is a photo: the primary proof is a photo.
	rules.Capture.Individual.Proofs = []domain.RemovalProofSlot{
		{Key: "scale_photo", Title: "Scale display", Kind: "photo", Required: true},
		{Key: "animal_video", Title: "Weighing video", Kind: "video", Required: false},
	}
	cmd := animalSlotCmd("slot:photo-primary")
	cmd.Proofs = domain.IndividualProofRefs{"scale_photo": slotPhotoProof}
	obs, err := repo.RecordAnimalObservation(ctx, judgedAnimal(t, rules, cmd))
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if obs.ProofArtifactID != slotPhotoProof {
		t.Fatalf("primary = %s", obs.ProofArtifactID)
	}
	if err := repo.SubmitIndividualScope(ctx, repoTenant, repoCampaign, repoAnimalScope, repoOperator, "submit:photo-primary", []string{"RFID-SLOT-1"}); err != nil {
		t.Fatalf("submit with a photo primary must be accepted: %v", err)
	}
	assertScopeStatus(t, ctx, pool, repoAnimalScope, domain.StatusCompleted)
}

func TestLegacyAnimalReplayFingerprintUnchanged(t *testing.T) {
	// No DB: the fingerprint of a legacy-shaped command must not move when the service fills the
	// derived fields, or every installed phone's replay would 409 after deploy.
	legacy := domain.RecordAnimalObservation{TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoAnimalScope, ScannedIdentifier: "R", WeightKg: 1, ProofArtifactID: repoExpectedShedProof, IdempotencyKey: "k", RecordedBy: repoOperator}
	before := idempotencyFingerprint(legacy)
	judged := legacy
	judged.NormalizedProofs = domain.LegacyIndividualRefs(domain.SeededRules(), legacy.ProofArtifactID)
	judged.NormalizedAnswers = domain.SOPAnswers{}
	judged.OrderedRefs = []string{legacy.ProofArtifactID}
	judged.PrimaryProofRef = legacy.ProofArtifactID
	judged.SlotKinds = domain.SeededRules().IndividualSlotKinds()
	judged.LegacyShape = true
	if idempotencyFingerprint(judged) != before {
		t.Fatal("derived fields moved the legacy animal fingerprint")
	}
	shed := domain.RecordShedObservation{TenantID: repoTenant, CampaignID: repoCampaign, CampaignShedID: repoShedScope, WeightKg: 1, ProofArtifactID: repoShedProof, ProofArtifactIDs: []string{repoShedProof}, IdempotencyKey: "k", RecordedBy: repoOperator}
	sb := idempotencyFingerprint(shed)
	js := shed
	js.NormalizedProofs = domain.LegacyLumpSumRefs(domain.SeededRules(), shed.ProofArtifactIDs)
	js.Ordered = domain.LegacyLumpSumOrdered(js.NormalizedProofs)
	js.SlotKinds = domain.SeededRules().LumpSumSlotKinds()
	js.NormalizedAnswers = domain.SOPAnswers{}
	js.LegacyShape = true
	if idempotencyFingerprint(js) != sb {
		t.Fatal("derived fields moved the legacy shed fingerprint")
	}
}
