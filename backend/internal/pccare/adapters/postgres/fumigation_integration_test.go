package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
)

// FUMIGATION (maintainer instruction 2026-09-30) against the REAL schema: a pen task with no
// animals, proved by the pen's own two videos. The category CHECK admits it (000457), a scan or a
// per-animal slot write is refused, the submit waits for BOTH pen videos, and the verifier's
// payload carries them in card order under the fumigation category.
func TestFumigationIsProvedByThePensTwoVideos(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	task := createPCTask(t, ctx, repo, domain.CategoryFumigation, "pc-fumigation-create")

	var slotKeys, requiredKeys []string
	if err := pool.QueryRow(ctx, `SELECT slot_keys, required_slot_keys FROM pc_care_tasks WHERE task_id = $1::uuid`, task.TaskID).
		Scan(&slotKeys, &requiredKeys); err != nil {
		t.Fatalf("read slot snapshot: %v", err)
	}
	if len(requiredKeys) != 2 || requiredKeys[0] != domain.SlotMixingVideo || requiredKeys[1] != domain.SlotSprayingVideo {
		t.Fatalf("required slots = %v, want [mixing_video spraying_video]", requiredKeys)
	}

	// No animal is scanned on pen work.
	if _, err := repo.ScanAnimal(ctx, ports.ScanAnimalParams{
		TenantID: pcTenant, TaskID: task.TaskID, ScannedIdentifier: "TAG-1", ScannedBy: pcOperator1,
		IdempotencyKey: "pc-fumigation-scan", ActorType: "operator",
	}); !errors.Is(err, domain.ErrNotAnimalTask) {
		t.Fatalf("scan on a fumigation task err = %v, want ErrNotAnimalTask", err)
	}

	register := func(slot, ref, key string) error {
		return repo.RegisterTaskProof(ctx, ports.RegisterTaskProofParams{
			TenantID: pcTenant, TaskID: task.TaskID, SlotKey: slot, ProofRef: ref,
			CapturedBy: pcOperator1, IdempotencyKey: key, ActorID: pcOperator1, ActorType: "operator",
		})
	}
	submit := func(key string) (ports.SubmitTaskResult, error) {
		return repo.SubmitTask(ctx, ports.SubmitTaskParams{
			TenantID: pcTenant, TaskID: task.TaskID, SubmittedBy: pcOperator2,
			IdempotencyKey: key, ActorType: "operator",
		})
	}

	// A slot the card does not ask for is refused.
	if err := register(domain.SlotVideo, "proof-x", "pc-fumigation-wrong-slot"); !errors.Is(err, domain.ErrInvalidSlotForCategory) {
		t.Fatalf("register a per-animal slot on pen work err = %v, want ErrInvalidSlotForCategory", err)
	}
	if _, err := submit("pc-fumigation-submit-empty"); !errors.Is(err, domain.ErrProofIncomplete) {
		t.Fatalf("submit with no videos err = %v, want ErrProofIncomplete", err)
	}
	if err := register(domain.SlotMixingVideo, "proof-mixing", "pc-fumigation-mixing"); err != nil {
		t.Fatalf("register mixing: %v", err)
	}
	if _, err := submit("pc-fumigation-submit-half"); !errors.Is(err, domain.ErrProofIncomplete) {
		t.Fatalf("submit with the mixing video only err = %v, want ErrProofIncomplete", err)
	}
	// Any assignee may record either video.
	if err := repo.RegisterTaskProof(ctx, ports.RegisterTaskProofParams{
		TenantID: pcTenant, TaskID: task.TaskID, SlotKey: domain.SlotSprayingVideo, ProofRef: "proof-spraying",
		CapturedBy: pcOperator2, IdempotencyKey: "pc-fumigation-spraying", ActorID: pcOperator2, ActorType: "operator",
	}); err != nil {
		t.Fatalf("register spraying: %v", err)
	}

	result, err := submit("pc-fumigation-submit-ready")
	if err != nil {
		t.Fatalf("submit fumigation: %v", err)
	}
	if !result.NewlyPending || result.Status != domain.StatusPendingVerification || result.AnimalCount != 0 {
		t.Fatalf("submit result = %+v, want newly pending with no animals", result)
	}
	if len(result.MediaRefs) != 2 ||
		result.MediaRefs[0].ProofRef != "proof-mixing" || result.MediaRefs[0].Label != "Mixing video" ||
		result.MediaRefs[1].ProofRef != "proof-spraying" || result.MediaRefs[1].Label != "Spraying video" {
		t.Fatalf("media refs = %+v, want mixing then spraying, labelled by the card", result.MediaRefs)
	}

	var payload []byte
	if err := pool.QueryRow(ctx, `
SELECT payload FROM outbox_messages
WHERE tenant_id = $1::uuid AND event_type = 'pc_care.task.pending_verification' AND aggregate_id = $2::uuid`,
		pcTenant, task.TaskID).Scan(&payload); err != nil {
		t.Fatalf("read pending verification payload: %v", err)
	}
	var envelope struct {
		Payload struct {
			Category  string `json:"category"`
			MediaRefs []struct {
				ProofRef string `json:"proof_ref"`
			} `json:"media_refs"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if envelope.Payload.Category != domain.CategoryFumigation || len(envelope.Payload.MediaRefs) != 2 {
		t.Fatalf("payload = %+v, want fumigation with both pen videos for the verifier", envelope.Payload)
	}

	// Locked for review: no more captures until the verdict.
	if err := register(domain.SlotMixingVideo, "proof-mixing-2", "pc-fumigation-mixing-late"); !errors.Is(err, domain.ErrTaskNotOpen) {
		t.Fatalf("capture after submit err = %v, want ErrTaskNotOpen", err)
	}
}
