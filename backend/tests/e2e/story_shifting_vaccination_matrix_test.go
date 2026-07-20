package e2e

import (
	"errors"
	"strconv"
	"testing"
	"time"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityports "github.com/vgoats/goatos/backend/internal/identity/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// This file extends the shifting->vaccination handoff proof
// (story_shifting_vaccination_handoff_test.go) with the reviewer's required scenario matrix. Every
// test here drives the SAME production producer as the main story
// (counts.CompleteShiftingEvent -> identity.RelocateGoatsToShedInTx, wired exactly as
// internal/bootstrap/api.go wires it via WithIdentityTxWriter) so the schema's triggers/constraints
// and the shed_profiles cohort authority are part of the assertion surface. The two helpers
// recordAndApproveShifting and completeShiftingE2E are reused verbatim from the main story file.
//
// Each Test is self-contained: its own Fixture (throwaway Postgres) and its own disjoint location/
// goat UUIDs, so nothing collides across tests even though they run in one package.

// newShiftRepo builds the counts shifting producer wired to the real identity relocation adapter,
// identically to the main story.
func newShiftRepo(fx *Fixture) *countspg.Repository {
	return countspg.NewRepository(fx.Pool, 10*time.Second).
		WithIdentityTxWriter(identitypg.NewRepository(fx.Pool, 10*time.Second))
}

// seedShedReusingStage seeds a shed (location + shed_profiles + operational attributes) that REUSES
// an already-seeded animal_stage_lookup row. It exists because animal_stage_lookup carries a UNIQUE
// (tenant_id, stage_code) constraint, so two sheds that must share the SAME cohort code (scenario 2)
// cannot each insert their own stage row -- they point their profile at one shared stage id.
func seedShedReusingStage(f *Fixture, shedID, shedCode, stageID string) {
	f.T.Helper()
	f.exec("reuse-stage shed location",
		`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status)
		 VALUES ($1, $2, 'shed', $3, $3, $4, 'active')`,
		shedID, fxTenant, shedCode, fxPark)
	f.exec("reuse-stage shed profile",
		`INSERT INTO shed_profiles (location_id, tenant_id, animal_stage_id, sex, capacity)
		 VALUES ($1, $2, $3, 'mixed', 500)`,
		shedID, fxTenant, stageID)
	f.exec("reuse-stage shed operational attributes",
		`INSERT INTO location_operational_attributes (tenant_id, location_id, usable_for_vaccination, is_quarantine, is_icu)
		 VALUES ($1, $2, true, false, false)`,
		fxTenant, shedID)
}

// TestKernelStory_ShiftingSnapshotPersistence proves that a successful cross-cohort completion
// persists the destination profile SNAPSHOT (its id + row_version) onto the goat.stage_changed
// outbox payload, so the cohort a moved animal adopted is provably the profile that authorised it.
func TestKernelStory_ShiftingSnapshotPersistence(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-snapshot-persistence",
		"Cross-cohort shifting persists the destination profile snapshot on goat.stage_changed",
		"A K1 goat is moved into an empty K2 shed. The completion's goat.stage_changed outbox payload "+
			"must carry destination_profile_id = the destination shed and destination_profile_row_version "+
			">= 1 -- the snapshot of the shed_profiles row that authorised the K2 cohort.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx
	shiftRepo := newShiftRepo(fx)

	const (
		sourceShed = "51000000-0000-4000-8000-000000000001"
		destShed   = "51000000-0000-4000-8000-000000000002"
		stageSrc   = "51000000-0000-4000-8000-00000000000a"
		stageDst   = "51000000-0000-4000-8000-00000000000b"
		mover      = "51000000-0000-4000-8000-000000000010"
	)
	fx.SeedShed(sourceShed, "E2E-SNAP-SRC", stageSrc)          // K1
	fx.SeedAdultShed(destShed, "E2E-SNAP-DST", stageDst, "K2") // K2, empty

	completedAt := time.Date(2026, 7, 19, 9, 0, 0, 0, biztime.DefaultLocation())
	dob := completedAt.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: mover, ShedID: sourceShed, Stage: "K1", DOB: &dob})

	story.Step("Complete an approved K1->K2 shifting",
		"The real producer moves the animal and adopts the destination shed's configured K2 cohort.")
	eventID := recordAndApproveShifting(t, ctx, shiftRepo, "snap-1", []string{mover}, sourceShed, destShed, completedAt)
	result, replayed, err := completeShiftingE2E(shiftRepo, ctx, "snap-1", eventID, "", completedAt)
	story.Assert("completion succeeded", err == nil, "err=%v", err)
	story.Assert("completion is a fresh execution", !replayed, "replayed=%v", replayed)
	if err == nil {
		story.Assert("completion applied the movement", result.EventStatus == countsdomain.ShiftingEventStatusApplied, "status=%s", result.EventStatus)
	}

	story.Step("The goat.stage_changed payload carries the destination profile snapshot",
		"destination_profile_id is the destination shed's profile (its location_id), and "+
			"destination_profile_row_version snapshots that shed_profiles row version.")
	snapID := fx.scanText(`SELECT payload->'payload'->>'destination_profile_id' FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)
	story.Assert("destination_profile_id is the destination shed", snapID == destShed, "destination_profile_id=%s", snapID)
	snapRV := fx.scanText(`SELECT payload->'payload'->>'destination_profile_row_version' FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)
	rv, convErr := strconv.Atoi(snapRV)
	story.Assert("destination_profile_row_version parses to an int", convErr == nil, "raw=%q err=%v", snapRV, convErr)
	story.Assert("destination_profile_row_version >= 1", rv >= 1, "row_version=%d", rv)
}

// TestKernelStory_ShiftingSameProfileNoStageEvent proves that a move BETWEEN two sheds configured
// with the SAME cohort emits the location event but NOT a stage event: management_stage does not
// change, so movement must not fabricate a spurious reclassification.
func TestKernelStory_ShiftingSameProfileNoStageEvent(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-same-profile-no-stage-event",
		"A same-cohort shed move emits location but not stage",
		"Two DIFFERENT sheds are both configured K2. A K2 goat moving between them relocates (one "+
			"goat.location.changed) but its management_stage is unchanged, so ZERO goat.stage_changed "+
			"is written.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx
	shiftRepo := newShiftRepo(fx)

	const (
		shedA   = "52000000-0000-4000-8000-000000000001"
		shedB   = "52000000-0000-4000-8000-000000000002"
		stageK2 = "52000000-0000-4000-8000-00000000000a"
		mover   = "52000000-0000-4000-8000-000000000010"
	)
	// Both sheds K2. Shed A seeds the shared K2 stage row; shed B reuses it (UNIQUE stage_code).
	fx.SeedAdultShed(shedA, "E2E-SAME-A", stageK2, "K2")
	seedShedReusingStage(fx, shedB, "E2E-SAME-B", stageK2)

	completedAt := time.Date(2026, 7, 19, 9, 0, 0, 0, biztime.DefaultLocation())
	dob := completedAt.AddDate(0, 0, -60)
	fx.SeedGoat(GoatSpec{GoatID: mover, ShedID: shedA, Stage: "K2", DOB: &dob})

	story.Step("Complete an approved K2->K2 shifting",
		"The animal relocates from shed A to shed B; both are the K2 cohort, so no stage transition.")
	eventID := recordAndApproveShifting(t, ctx, shiftRepo, "same-1", []string{mover}, shedA, shedB, completedAt)
	result, _, err := completeShiftingE2E(shiftRepo, ctx, "same-1", eventID, "", completedAt)
	story.Assert("completion succeeded", err == nil, "err=%v", err)
	if err == nil {
		story.Assert("completion applied the movement", result.EventStatus == countsdomain.ShiftingEventStatusApplied, "status=%s", result.EventStatus)
	}

	story.Step("Exactly one location event and zero stage events",
		"management_stage is unchanged (K2->K2), so the stage-change writer emits nothing.")
	shedAfter := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("animal is now in shed B", shedAfter == shedB, "shed_id=%s", shedAfter)
	stageAfter := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("management_stage is still K2", stageAfter == "K2", "management_stage=%s", stageAfter)

	locOutbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.location.changed'`, fxTenant, mover)
	story.Assert("exactly one goat.location.changed outbox message", locOutbox == 1, "count=%d", locOutbox)
	stageOutbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)
	story.Assert("zero goat.stage_changed outbox messages", stageOutbox == 0, "count=%d", stageOutbox)
	stageIdentity := fx.countRows(`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type='goat.stage_changed'`, fxTenant, mover)
	story.Assert("zero goat.stage_changed identity events", stageIdentity == 0, "count=%d", stageIdentity)
}

// TestKernelStory_ShiftingClinicalDestinationFailsClosed proves that a move into a shed whose
// configured cohort is a CLINICAL state fails closed -- movement never fabricates clinical truth --
// and nothing moves, with zero events written.
func TestKernelStory_ShiftingClinicalDestinationFailsClosed(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-clinical-destination-fails-closed",
		"A move into a clinical-cohort shed fails closed",
		"The destination shed is configured 'icu'. Movement must never invent clinical truth, so the "+
			"completion returns ErrClinicalDestinationTag, the animal stays in the source shed, and no "+
			"location/stage events are written.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx
	shiftRepo := newShiftRepo(fx)

	const (
		sourceShed = "53000000-0000-4000-8000-000000000001"
		icuShed    = "53000000-0000-4000-8000-000000000002"
		stageSrc   = "53000000-0000-4000-8000-00000000000a"
		icuStage   = "53000000-0000-4000-8000-00000000000b"
		mover      = "53000000-0000-4000-8000-000000000010"
	)
	fx.SeedShed(sourceShed, "E2E-CLIN-SRC", stageSrc)          // K1
	fx.SeedAdultShed(icuShed, "E2E-CLIN-ICU", icuStage, "icu") // clinical cohort

	completedAt := time.Date(2026, 7, 19, 9, 0, 0, 0, biztime.DefaultLocation())
	dob := completedAt.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: mover, ShedID: sourceShed, Stage: "K1", DOB: &dob})

	story.Step("Complete an approved move into the icu-configured shed",
		"The resolved destination cohort is clinical, so the relocation aborts.")
	eventID := recordAndApproveShifting(t, ctx, shiftRepo, "clin-1", []string{mover}, sourceShed, icuShed, completedAt)
	_, _, err := completeShiftingE2E(shiftRepo, ctx, "clin-1", eventID, "", completedAt)
	story.Assert("completion fails closed with ErrClinicalDestinationTag", errors.Is(err, identityports.ErrClinicalDestinationTag), "err=%v", err)

	story.Step("Nothing moved and no events were written",
		"The animal stays in the source shed; the transaction rolled back with no side effects.")
	shedAfter := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("animal stays in the source shed", shedAfter == sourceShed, "shed_id=%s", shedAfter)
	events := fx.countRows(`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type IN ('goat.location.changed','goat.stage_changed')`, fxTenant, mover)
	story.Assert("no location/stage identity events written", events == 0, "count=%d", events)
	outbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id=$2 AND event_type IN ('goat.location.changed','goat.stage_changed')`, fxTenant, mover)
	story.Assert("no location/stage outbox messages written", outbox == 0, "count=%d", outbox)
}

// TestKernelStory_ShiftingDestinationTagConflictFailsClosed proves that an operator-supplied
// DestinationTag that DISAGREES with the configured cohort fails closed and moves nothing.
func TestKernelStory_ShiftingDestinationTagConflictFailsClosed(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-destination-tag-conflict-fails-closed",
		"A destination-tag conflict fails closed",
		"The destination shed is configured K2, but completion supplies tag=K3. A shed cannot hold two "+
			"cohorts, so the completion returns ErrDestinationTagConflict and nothing moves.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx
	shiftRepo := newShiftRepo(fx)

	const (
		sourceShed = "54000000-0000-4000-8000-000000000001"
		destShed   = "54000000-0000-4000-8000-000000000002"
		stageSrc   = "54000000-0000-4000-8000-00000000000a"
		stageDst   = "54000000-0000-4000-8000-00000000000b"
		mover      = "54000000-0000-4000-8000-000000000010"
	)
	fx.SeedShed(sourceShed, "E2E-CONF-SRC", stageSrc)          // K1
	fx.SeedAdultShed(destShed, "E2E-CONF-DST", stageDst, "K2") // configured K2

	completedAt := time.Date(2026, 7, 19, 9, 0, 0, 0, biztime.DefaultLocation())
	dob := completedAt.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: mover, ShedID: sourceShed, Stage: "K1", DOB: &dob})

	story.Step("Complete the move with a conflicting operator tag (K3 vs configured K2)",
		"The supplied tag is a request that must AGREE with the configured profile; it can never override it.")
	eventID := recordAndApproveShifting(t, ctx, shiftRepo, "conf-1", []string{mover}, sourceShed, destShed, completedAt)
	_, _, err := completeShiftingE2E(shiftRepo, ctx, "conf-1", eventID, "K3", completedAt)
	story.Assert("completion fails closed with ErrDestinationTagConflict", errors.Is(err, identityports.ErrDestinationTagConflict), "err=%v", err)

	story.Step("Nothing moved",
		"The animal stays in the source shed with its original cohort.")
	shedAfter := fx.scanText(`SELECT shed_id::text FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("animal stays in the source shed", shedAfter == sourceShed, "shed_id=%s", shedAfter)
	stageAfter := fx.scanText(`SELECT COALESCE(management_stage,'') FROM goats WHERE tenant_id=$1 AND goat_id=$2`, fxTenant, mover)
	story.Assert("management_stage unchanged (still K1)", stageAfter == "K1", "management_stage=%s", stageAfter)
	events := fx.countRows(`SELECT count(*) FROM goat_identity_events WHERE tenant_id=$1 AND goat_id=$2 AND event_type IN ('goat.location.changed','goat.stage_changed')`, fxTenant, mover)
	story.Assert("no location/stage identity events written", events == 0, "count=%d", events)
}

// TestKernelStory_ShiftingMultiAnimalHomogeneousMove proves that ONE approved shifting carrying TWO
// same-cohort movers relocates BOTH atomically into the destination shed, reclassifies both, and
// emits exactly one location + one stage event PER animal.
func TestKernelStory_ShiftingMultiAnimalHomogeneousMove(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-multi-animal-homogeneous-move",
		"One shifting moves two same-cohort animals into the destination shed",
		"Two K1 movers are relocated to one K2 shed in a single approved shifting. Both end up in the "+
			"destination shed at management_stage K2, with exactly 2 goat.location.changed and 2 "+
			"goat.stage_changed outbox rows (one per animal), and result.MovedGoatIDs has 2.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx
	shiftRepo := newShiftRepo(fx)

	const (
		sourceShed = "55000000-0000-4000-8000-000000000001"
		destShed   = "55000000-0000-4000-8000-000000000002"
		stageSrc   = "55000000-0000-4000-8000-00000000000a"
		stageDst   = "55000000-0000-4000-8000-00000000000b"
		moverA     = "55000000-0000-4000-8000-000000000010"
		moverB     = "55000000-0000-4000-8000-000000000011"
	)
	fx.SeedShed(sourceShed, "E2E-MULTI-SRC", stageSrc)          // K1
	fx.SeedAdultShed(destShed, "E2E-MULTI-DST", stageDst, "K2") // K2, empty

	completedAt := time.Date(2026, 7, 19, 9, 0, 0, 0, biztime.DefaultLocation())
	dob := completedAt.AddDate(0, 0, -21)
	fx.SeedGoat(GoatSpec{GoatID: moverA, ShedID: sourceShed, Stage: "K1", DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: moverB, ShedID: sourceShed, Stage: "K1", DOB: &dob})

	story.Step("Complete one approved shifting carrying both animals",
		"recordAndApproveShifting sets head_count = len(goatIDs) = 2; the completion moves both in one transaction.")
	eventID := recordAndApproveShifting(t, ctx, shiftRepo, "multi-1", []string{moverA, moverB}, sourceShed, destShed, completedAt)
	result, _, err := completeShiftingE2E(shiftRepo, ctx, "multi-1", eventID, "", completedAt)
	story.Assert("completion succeeded", err == nil, "err=%v", err)
	if err == nil {
		story.Assert("completion applied the movement", result.EventStatus == countsdomain.ShiftingEventStatusApplied, "status=%s", result.EventStatus)
		story.Assert("both animals relocated (MovedGoatIDs has 2)", len(result.MovedGoatIDs) == 2, "moved=%d", len(result.MovedGoatIDs))
	}

	story.Step("Both animals are in the destination shed at K2",
		"Each mover adopted the destination shed's configured cohort.")
	inDest := fx.countRows(`SELECT count(*) FROM goats WHERE tenant_id=$1 AND goat_id = ANY($2) AND shed_id=$3 AND management_stage='K2'`, fxTenant, []string{moverA, moverB}, destShed)
	story.Assert("both animals are in the destination shed at management_stage K2", inDest == 2, "count=%d", inDest)

	story.Step("Exactly two location events and two stage events (one per animal)",
		"The set-based relocation emits a per-animal event pair, not a single batch event.")
	locOutbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id = ANY($2) AND event_type='goat.location.changed'`, fxTenant, []string{moverA, moverB})
	story.Assert("exactly 2 goat.location.changed outbox rows", locOutbox == 2, "count=%d", locOutbox)
	stageOutbox := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND aggregate_id = ANY($2) AND event_type='goat.stage_changed'`, fxTenant, []string{moverA, moverB})
	story.Assert("exactly 2 goat.stage_changed outbox rows", stageOutbox == 2, "count=%d", stageOutbox)
}
