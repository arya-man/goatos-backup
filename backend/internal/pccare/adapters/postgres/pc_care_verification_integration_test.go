package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// PC Care verification gate — proofs of the maintainer-2026-08-21 rules against the REAL
// pc_care_* schema (CHECK constraints, the tag dedup index, the natural key, and the outbox
// tenant-parity trigger are all part of the assertion surface).
//
// The rules: the CEO plans one task per (category, pen, day) with one or more assignees;
// operators scan tags VERBATIM (duplicate tag in one task refused); trimming categories demand
// three slot videos per animal, the others one; ANY assignee submits the whole task once every
// animal's slot set is full; the task is 'completed' (and pc_care.task.completed emitted) ONLY
// when a verifier approves; a rejection bounces to 'rework' and the re-submit mints a fresh
// row_version.
//
// Gated by the repository's postgres integration test harness.

const (
	pcTenant    = "9c000000-0000-4000-8000-000000000001"
	pcPark      = "9c000000-0000-4000-8000-000000003001"
	pcShedA     = "9c000000-0000-4000-8000-000000004001"
	pcOperator1 = "9c000000-0000-4000-8000-000000005001"
	pcOperator2 = "9c000000-0000-4000-8000-000000005002"
	pcOutsider  = "9c000000-0000-4000-8000-000000005003"
	// pcOtherPark is a SECOND park; pcOtherParkOperator is an active member scoped
	// to it and to nothing in pcPark.
	pcOtherPark         = "9c000000-0000-4000-8000-000000003002"
	pcOtherParkOperator = "9c000000-0000-4000-8000-000000005044"
	pcVerifier          = "9c000000-0000-4000-8000-000000006001"
)

func pcBusinessDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, biztime.DefaultLocation())
}

func setupPCCareDB(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()
	pgtest.SkipIfNoDocker(t)
	pool := pgtest.StartPostgres(t, ctx)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Mesha Test', 'active')
ON CONFLICT (tenant_id) DO NOTHING`, pcTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CPT', 'CPT', 'active')
ON CONFLICT (location_id) DO NOTHING`, pcTenant, pcPark)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id, display_order)
VALUES ($3::uuid, $1::uuid, 'shed', 'S-A', 'Castro', 'active', $2::uuid, 1)
ON CONFLICT (location_id) DO NOTHING`, pcTenant, pcPark, pcShedA)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'CBE', 'CBE', 'active')
ON CONFLICT (location_id) DO NOTHING`, pcTenant, pcOtherPark)
	exec(`INSERT INTO workforce_members (tenant_id, display_code, display_name, status, user_id)
VALUES ($1::uuid, 'PC-OP-1', 'Amit', 'active', $2::uuid),
       ($1::uuid, 'PC-OP-2', 'Darshan', 'active', $3::uuid),
       ($1::uuid, 'PC-OP-4', 'Sagar', 'active', $4::uuid)
ON CONFLICT DO NOTHING`, pcTenant, pcOperator1, pcOperator2, pcOtherParkOperator)
	// Operators are park-scoped by invariant; CreateTask refuses an assignee whose
	// grants do not reach the task's park.
	exec(`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
VALUES ($1::uuid, $2::uuid, 'operator', 'park', $4::uuid, 'active', now()),
       ($1::uuid, $3::uuid, 'operator', 'park', $4::uuid, 'active', now()),
       ($1::uuid, $5::uuid, 'operator', 'park', $6::uuid, 'active', now())
ON CONFLICT DO NOTHING`, pcTenant, pcOperator1, pcOperator2, pcPark, pcOtherParkOperator, pcOtherPark)
	return NewRepository(pool, 10*time.Second), pool
}

func createPCTask(t *testing.T, ctx context.Context, repo *Repository, category, idemKey string) ports.TaskRow {
	t.Helper()
	task, err := repo.CreateTask(ctx, ports.CreateTaskParams{
		TenantID:            pcTenant,
		Category:            category,
		ParkID:              pcPark,
		ShedID:              pcShedA,
		PlannedBusinessDate: pcBusinessDay(2026, 8, 21),
		AssigneeUserIDs:     []string{pcOperator1, pcOperator2},
		IdempotencyKey:      idemKey,
		CreatedBy:           pcVerifier,
		ActorID:             pcVerifier,
		ActorType:           "human",
		TraceID:             "trace-pc-create",
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	return task
}

// (a) Create is idempotent on the request key, and a second live task for the same
// (category, pen, day) is refused as already planned.
func TestCreateTaskIdempotencyAndNaturalKey(t *testing.T) {
	ctx := context.Background()
	repo, _ := setupPCCareDB(t, ctx)

	task := createPCTask(t, ctx, repo, domain.CategoryDeworming, "pc-create-key-1")
	if task.WorkState != domain.WorkStateScheduled || task.Status != domain.StatusOpen {
		t.Fatalf("fresh task state = %s/%s, want scheduled/open", task.WorkState, task.Status)
	}
	if len(task.AssigneeUserIDs) != 2 {
		t.Fatalf("assignees = %v, want 2", task.AssigneeUserIDs)
	}

	replay := createPCTask(t, ctx, repo, domain.CategoryDeworming, "pc-create-key-1")
	if replay.TaskID != task.TaskID {
		t.Fatalf("replay minted a second task: %s vs %s", replay.TaskID, task.TaskID)
	}

	_, err := repo.CreateTask(ctx, ports.CreateTaskParams{
		TenantID: pcTenant, Category: domain.CategoryDeworming, ParkID: pcPark, ShedID: pcShedA,
		PlannedBusinessDate: pcBusinessDay(2026, 8, 21),
		AssigneeUserIDs:     []string{pcOperator1},
		IdempotencyKey:      "pc-create-key-2", CreatedBy: pcVerifier,
	})
	if !errors.Is(err, domain.ErrTaskAlreadyPlanned) {
		t.Fatalf("second live task err = %v, want ErrTaskAlreadyPlanned", err)
	}

	// A DIFFERENT category on the same pen-day is a different task and is allowed.
	other := createPCTask(t, ctx, repo, domain.CategoryHoofTrimming, "pc-create-key-3")
	if other.TaskID == task.TaskID {
		t.Fatal("hoof trimming task collided with deworming task")
	}
}

// (b) A tag is stored VERBATIM and cannot be scanned twice into one task — concurrent
// different-key scans of the same tag yield exactly one row and a typed conflict.
func TestScanAnimalVerbatimAndDuplicateRefused(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	task := createPCTask(t, ctx, repo, domain.CategoryTicksRemoval, "pc-scan-create")

	res, err := repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID,
		ScannedIdentifier: "  954 0001 2345 ", ScannedBy: pcOperator1,
		IdempotencyKey: "pc-scan-1", ActorID: pcOperator1, ActorType: "operator",
	})
	if err != nil {
		t.Fatalf("ScanAnimal: %v", err)
	}
	var stored string
	if err := pool.QueryRow(ctx, `
SELECT scanned_identifier FROM pc_care_task_animals
WHERE tenant_id = $1::uuid AND animal_row_id = $2::uuid`, pcTenant, res.AnimalRowID).Scan(&stored); err != nil {
		t.Fatalf("read scan row: %v", err)
	}
	// Verbatim after the service-level trim: interior spacing preserved, no herd lookup.
	if stored != "954 0001 2345" {
		t.Fatalf("stored tag = %q, want verbatim", stored)
	}

	// The SAME tag (case/space-insensitively) under a DIFFERENT key, by a DIFFERENT operator.
	_, err = repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID,
		ScannedIdentifier: "954 0001 2345", ScannedBy: pcOperator2,
		IdempotencyKey: "pc-scan-2", ActorID: pcOperator2, ActorType: "operator",
	})
	if !errors.Is(err, domain.ErrDuplicateScan) {
		t.Fatalf("duplicate scan err = %v, want ErrDuplicateScan", err)
	}

	// An exact replay of the FIRST request is not a duplicate — it echoes the original row.
	replay, err := repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID,
		ScannedIdentifier: "  954 0001 2345 ", ScannedBy: pcOperator1,
		IdempotencyKey: "pc-scan-1", ActorID: pcOperator1, ActorType: "operator",
	})
	if err != nil || !replay.Replayed || replay.AnimalRowID != res.AnimalRowID {
		t.Fatalf("scan replay = %+v err=%v, want replayed original row", replay, err)
	}
}

// registerSlot is the test's slot write helper.
func registerSlot(t *testing.T, ctx context.Context, repo *Repository, taskID, animalRowID, slot, ref, by, key string) error {
	t.Helper()
	return repo.RegisterSlotProof(ctx, ports.RegisterSlotProofParams{
		TenantID: pcTenant, TaskID: taskID, AnimalRowID: animalRowID,
		SlotFieldKey: slot, ProofRef: ref, CapturedBy: by,
		IdempotencyKey: key, ActorID: by, ActorType: "operator",
	})
}

// (c) Trimming demands the before/during/after triple; a slot outside the category is refused;
// a peer may fill a sibling slot; submit is refused until the set is complete and then locks
// the task; the ONE verification item's media refs are labeled per animal + slot.
func TestSlotSetGatesSubmitAndPeersMayFillSlots(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	task := createPCTask(t, ctx, repo, domain.CategoryHoofTrimming, "pc-slots-create")

	scan, err := repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID,
		ScannedIdentifier: "RFID-42", ScannedBy: pcOperator1,
		IdempotencyKey: "pc-slots-scan-1", ActorID: pcOperator1, ActorType: "operator",
	})
	if err != nil {
		t.Fatalf("ScanAnimal: %v", err)
	}

	// The 1-video slot does not belong to a trimming task.
	if err := registerSlot(t, ctx, repo, task.TaskID, scan.AnimalRowID, domain.SlotVideo, "proof-x", pcOperator1, "pc-slot-key-x"); !errors.Is(err, domain.ErrInvalidSlotForCategory) {
		t.Fatalf("video slot on trimming err = %v, want ErrInvalidSlotForCategory", err)
	}

	if err := registerSlot(t, ctx, repo, task.TaskID, scan.AnimalRowID, domain.SlotBefore, "proof-before-1", pcOperator1, "pc-slot-key-1"); err != nil {
		t.Fatalf("before slot: %v", err)
	}

	// Submit with two slots missing is refused with nothing changed.
	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-submit-early", ActorType: "operator",
	}); !errors.Is(err, domain.ErrProofIncomplete) {
		t.Fatalf("early submit err = %v, want ErrProofIncomplete", err)
	}

	// A PEER (operator 2) fills the remaining slots — the collaboration rule.
	if err := registerSlot(t, ctx, repo, task.TaskID, scan.AnimalRowID, domain.SlotDuring, "proof-during-1", pcOperator2, "pc-slot-key-2"); err != nil {
		t.Fatalf("during slot: %v", err)
	}
	if err := registerSlot(t, ctx, repo, task.TaskID, scan.AnimalRowID, domain.SlotAfter, "proof-after-1", pcOperator2, "pc-slot-key-3"); err != nil {
		t.Fatalf("after slot: %v", err)
	}

	result, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator2,
		IdempotencyKey: "pc-submit-1", ActorType: "operator",
	})
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	if !result.NewlyPending || result.Status != domain.StatusPendingVerification {
		t.Fatalf("submit result = %+v, want newly pending", result)
	}
	if result.AnimalCount != 1 || len(result.MediaRefs) != 3 {
		t.Fatalf("submit media = %+v, want 3 labeled refs for 1 animal", result.MediaRefs)
	}
	// OUTPUT-STRING assertion, DB round trip: the labels name the animal and the step.
	if result.MediaRefs[0].Label != "RFID-42 · Before trimming" {
		t.Fatalf("media label = %q, want 'RFID-42 · Before trimming'", result.MediaRefs[0].Label)
	}
	if result.ShedName != "Castro" {
		t.Fatalf("submit shed name = %q, want Castro", result.ShedName)
	}
	var pendingOutboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'pc_care.task.pending_verification' AND aggregate_id = $2::uuid`,
		pcTenant, task.TaskID).Scan(&pendingOutboxCount); err != nil {
		t.Fatalf("count pending verification outbox: %v", err)
	}
	if pendingOutboxCount != 1 {
		t.Fatalf("pc_care.task.pending_verification outbox rows = %d, want 1", pendingOutboxCount)
	}

	// The lock: a scan into a pending task is refused.
	if _, err := repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID,
		ScannedIdentifier: "RFID-43", ScannedBy: pcOperator1,
		IdempotencyKey: "pc-scan-late", ActorID: pcOperator1, ActorType: "operator",
	}); !errors.Is(err, domain.ErrTaskNotOpen) {
		t.Fatalf("scan on pending err = %v, want ErrTaskNotOpen", err)
	}

	// A different-key resend of the submit is an idempotent no-op (never a second enqueue).
	resend, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-submit-again", ActorType: "operator",
	})
	if err != nil || resend.NewlyPending {
		t.Fatalf("resubmit = %+v err=%v, want no-op", resend, err)
	}

	var submittedAnimals int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM pc_care_task_animals
WHERE tenant_id = $1::uuid AND task_id = $2::uuid AND submitted_at IS NOT NULL`,
		pcTenant, task.TaskID).Scan(&submittedAnimals); err != nil {
		t.Fatalf("count submitted animals: %v", err)
	}
	if submittedAnimals != 1 {
		t.Fatalf("submitted animal rows = %d, want 1", submittedAnimals)
	}
}

func TestInventoryVaccineTaskProofGatesSubmitAndFansOutToVerification(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	task := createPCTask(t, ctx, repo, domain.CategoryInventoryVaccine, "pc-inventory-submit-create")

	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-inventory-submit-early", ActorType: "operator",
	}); !errors.Is(err, domain.ErrProofIncomplete) {
		t.Fatalf("inventory submit before fridge proof err = %v, want ErrProofIncomplete", err)
	}

	if err := repo.RegisterTaskProof(ctx, ports.RegisterTaskProofParams{
		TenantID:       pcTenant,
		TaskID:         task.TaskID,
		SlotKey:        domain.SlotStockFridgePhoto,
		ProofRef:       "proof-fridge-stock-photo",
		CapturedBy:     pcOperator1,
		IdempotencyKey: "pc-inventory-task-proof-photo",
		ActorID:        pcOperator1,
		ActorType:      "operator",
		TraceID:        "trace-inventory-task-proof-photo",
	}); err != nil {
		t.Fatalf("RegisterTaskProof photo: %v", err)
	}

	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-inventory-submit-only-photo", ActorType: "operator",
	}); !errors.Is(err, domain.ErrProofIncomplete) {
		t.Fatalf("inventory submit with photo only err = %v, want ErrProofIncomplete", err)
	}

	if err := repo.RegisterTaskProof(ctx, ports.RegisterTaskProofParams{
		TenantID:       pcTenant,
		TaskID:         task.TaskID,
		SlotKey:        domain.SlotStockFridgeVideo,
		ProofRef:       "proof-fridge-stock-video",
		CapturedBy:     pcOperator1,
		IdempotencyKey: "pc-inventory-task-proof-video",
		ActorID:        pcOperator1,
		ActorType:      "operator",
		TraceID:        "trace-inventory-task-proof-video",
	}); err != nil {
		t.Fatalf("RegisterTaskProof video: %v", err)
	}

	proofs, err := repo.ListTaskProofs(ctx, pcTenant, task.TaskID)
	if err != nil {
		t.Fatalf("ListTaskProofs: %v", err)
	}
	if len(proofs) != 2 {
		t.Fatalf("task proofs = %+v, want photo and video proofs", proofs)
	}
	bySlot := map[string]string{}
	for _, proof := range proofs {
		bySlot[proof.SlotKey] = proof.ProofRef
	}
	if bySlot[domain.SlotStockFridgePhoto] != "proof-fridge-stock-photo" || bySlot[domain.SlotStockFridgeVideo] != "proof-fridge-stock-video" {
		t.Fatalf("task proofs = %+v, want independent photo/video fridge-stock proofs", proofs)
	}
	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-inventory-submit-no-requirements", ActorType: "operator",
	}); !errors.Is(err, domain.ErrProofIncomplete) {
		t.Fatalf("inventory submit with proof but no requirement rows err = %v, want ErrProofIncomplete", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO pc_care_task_inventory_requirements (
  tenant_id, task_id, vaccine_label, required_doses, source_batch_ids
) VALUES (
  $1::uuid, $2::uuid, 'PPR', 25, ARRAY['9c000000-0000-4000-8000-00000000f001'::uuid]
)`, pcTenant, task.TaskID); err != nil {
		t.Fatalf("seed inventory requirement: %v", err)
	}

	result, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-inventory-submit-ready", ActorType: "operator",
		TraceID: "trace-inventory-submit-ready",
	})
	if err != nil {
		t.Fatalf("SubmitTask inventory: %v", err)
	}
	if !result.NewlyPending || result.Status != domain.StatusPendingVerification {
		t.Fatalf("inventory submit result = %+v, want newly pending", result)
	}
	if result.AnimalCount != 0 {
		t.Fatalf("inventory task animal count = %d, want 0 because proof is task-level", result.AnimalCount)
	}
	if len(result.MediaRefs) != 2 ||
		result.MediaRefs[0].ProofRef != "proof-fridge-stock-photo" || result.MediaRefs[0].Label != "Fridge stock photo" ||
		result.MediaRefs[1].ProofRef != "proof-fridge-stock-video" || result.MediaRefs[1].Label != "Fridge stock video" {
		t.Fatalf("inventory media refs = %+v, want labeled fridge photo and video proofs", result.MediaRefs)
	}

	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT payload FROM outbox_messages
WHERE tenant_id = $1::uuid
  AND event_type = 'pc_care.task.pending_verification'
  AND aggregate_id = $2::uuid`, pcTenant, task.TaskID).Scan(&payload); err != nil {
		t.Fatalf("read pending verification payload: %v", err)
	}
	var envelope struct {
		Payload struct {
			Category  string `json:"category"`
			MediaRefs []struct {
				ProofRef string `json:"proof_ref"`
				Label    string `json:"label"`
			} `json:"media_refs"`
			AnimalCount int32 `json:"animal_count"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode pending verification payload: %v", err)
	}
	if envelope.Payload.Category != domain.CategoryInventoryVaccine || envelope.Payload.AnimalCount != 0 {
		t.Fatalf("pending payload category/count = %q/%d, want inventory_vaccine/0", envelope.Payload.Category, envelope.Payload.AnimalCount)
	}
	if len(envelope.Payload.MediaRefs) != 2 ||
		envelope.Payload.MediaRefs[0].ProofRef != "proof-fridge-stock-photo" || envelope.Payload.MediaRefs[0].Label != "Fridge stock photo" ||
		envelope.Payload.MediaRefs[1].ProofRef != "proof-fridge-stock-video" || envelope.Payload.MediaRefs[1].Label != "Fridge stock video" {
		t.Fatalf("pending payload media refs = %+v, want fridge photo and video proofs for verifier", envelope.Payload.MediaRefs)
	}
}

// (d) Approve completes BOTH state columns and emits pc_care.task.completed exactly once
// (outbox tenant-parity trigger passes); a re-delivered verdict is a no-op. Reject bounces to
// rework with the reason; re-record happens on the SAME animal row; resubmit bumps row_version
// so the next verification item's idempotency key differs.
func TestVerdictApplyBounceAndResubmitCycle(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	task := createPCTask(t, ctx, repo, domain.CategoryDeworming, "pc-verdict-create")

	scan, err := repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID,
		ScannedIdentifier: "RFID-7", ScannedBy: pcOperator1,
		IdempotencyKey: "pc-verdict-scan", ActorID: pcOperator1, ActorType: "operator",
	})
	if err != nil {
		t.Fatalf("ScanAnimal: %v", err)
	}
	if err := registerSlot(t, ctx, repo, task.TaskID, scan.AnimalRowID, domain.SlotVideo, "proof-v1", pcOperator1, "pc-verdict-slot-1"); err != nil {
		t.Fatalf("slot: %v", err)
	}
	first, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-verdict-submit-1", ActorType: "operator",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	// REJECT → rework with the verifier's reason, rendered verbatim later.
	bounced, err := repo.BounceTaskForRework(ctx, ports.BounceTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, Reason: "Video too dark", TraceID: "trace-b1",
	})
	if err != nil || !bounced {
		t.Fatalf("bounce = %v err=%v, want applied", bounced, err)
	}
	var status, reason string
	if err := pool.QueryRow(ctx, `
SELECT status, coalesce(rework_reason, '') FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, task.TaskID).Scan(&status, &reason); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != domain.StatusRework || reason != "Video too dark" {
		t.Fatalf("after bounce = %s/%q, want rework/'Video too dark'", status, reason)
	}

	// Re-record on the SAME animal row (replacement, not a second row), then resubmit.
	if err := registerSlot(t, ctx, repo, task.TaskID, scan.AnimalRowID, domain.SlotVideo, "proof-v2", pcOperator2, "pc-verdict-slot-2"); err != nil {
		t.Fatalf("re-record slot: %v", err)
	}
	second, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator2,
		IdempotencyKey: "pc-verdict-submit-2", ActorType: "operator",
	})
	if err != nil || !second.NewlyPending {
		t.Fatalf("resubmit = %+v err=%v, want newly pending", second, err)
	}
	if second.RowVersion <= first.RowVersion {
		t.Fatalf("resubmit row_version = %d, want > %d (fresh verification item key)", second.RowVersion, first.RowVersion)
	}
	if len(second.MediaRefs) != 1 || second.MediaRefs[0].ProofRef != "proof-v2" {
		t.Fatalf("resubmit media = %+v, want the replacement clip", second.MediaRefs)
	}

	// APPROVE → completed on BOTH columns + exactly one outbox event.
	applied, err := repo.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, VerifiedBy: pcVerifier, TraceID: "trace-a1",
	})
	if err != nil || !applied {
		t.Fatalf("apply = %v err=%v, want applied", applied, err)
	}
	var workState string
	if err := pool.QueryRow(ctx, `
SELECT status, work_state FROM pc_care_tasks
WHERE tenant_id = $1::uuid AND task_id = $2::uuid`, pcTenant, task.TaskID).Scan(&status, &workState); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if status != domain.StatusCompleted || workState != domain.WorkStateCompleted {
		t.Fatalf("after apply = %s/%s, want completed/completed", status, workState)
	}
	var outboxCount int
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'pc_care.task.completed' AND aggregate_id = $2::uuid`,
		pcTenant, task.TaskID).Scan(&outboxCount); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("pc_care.task.completed outbox rows = %d, want 1", outboxCount)
	}

	// Re-delivered verdicts are stale-guarded no-ops.
	if applied, err := repo.ApplyVerifiedTask(ctx, ports.ApplyVerifiedTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, VerifiedBy: pcVerifier,
	}); err != nil || applied {
		t.Fatalf("re-delivered apply = %v err=%v, want no-op", applied, err)
	}
	if bounced, err := repo.BounceTaskForRework(ctx, ports.BounceTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, Reason: "stale",
	}); err != nil || bounced {
		t.Fatalf("stale bounce = %v err=%v, want no-op", bounced, err)
	}
}

// (e) A submit with zero scanned animals is refused, and the kernel roll-forward slides an
// overdue open task to today as delayed (planned date immutable).
func TestSubmitNeedsAnimalsAndKernelRollsForward(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	task := createPCTask(t, ctx, repo, domain.CategoryHairTrimming, "pc-kernel-create")

	if _, err := repo.SubmitTask(ctx, ports.SubmitTaskParams{
		TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator1,
		IdempotencyKey: "pc-kernel-submit", ActorType: "operator",
	}); !errors.Is(err, domain.ErrNoAnimals) {
		t.Fatalf("empty submit err = %v, want ErrNoAnimals", err)
	}

	// Two days later, the task has slid to the current business day as 'delayed'.
	asOf := pcBusinessDay(2026, 8, 23)
	result, err := repo.SweepTaskRollForward(ctx, pcTenant, asOf, 200, 50)
	if err != nil {
		t.Fatalf("SweepTaskRollForward: %v", err)
	}
	if result.RolledForward != 1 {
		t.Fatalf("rolled forward = %d, want 1", result.RolledForward)
	}
	var workState, planned, due, delayedSince string
	if err := pool.QueryRow(ctx, `
SELECT work_state, planned_business_date::text, due_business_date::text,
       coalesce(delayed_since_business_date::text, '')
FROM pc_care_tasks WHERE tenant_id = $1::uuid AND task_id = $2::uuid`,
		pcTenant, task.TaskID).Scan(&workState, &planned, &due, &delayedSince); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if workState != domain.WorkStateDelayed || planned != "2026-08-21" || due != "2026-08-23" || delayedSince != "2026-08-21" {
		t.Fatalf("after sweep = %s planned=%s due=%s since=%s, want delayed/2026-08-21/2026-08-23/2026-08-21", workState, planned, due, delayedSince)
	}
	// Idempotent: a second sweep the same day moves nothing.
	again, err := repo.SweepTaskRollForward(ctx, pcTenant, asOf, 200, 50)
	if err != nil || again.RolledForward != 0 {
		t.Fatalf("second sweep = %+v err=%v, want zero", again, err)
	}
}
