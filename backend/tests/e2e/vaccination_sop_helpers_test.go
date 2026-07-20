package e2e

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/eventbus"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	"github.com/vgoats/goatos/backend/internal/sopbridge"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationproofmedia "github.com/vgoats/goatos/backend/internal/verification/adapters/proofmedia"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

type vaccinationSOPHarness struct {
	t       *testing.T
	fx      *Fixture
	Service *sopapp.Service
	proofs  *proofapp.Service
}

// completeVaccinationDriveThroughSOP turns already-generated, unbatched work into an accepted
// vaccination only through the production sweeper, proof, SOP submission, and independent review
// path. It is the canonical setup helper for stories whose actual subject is a downstream rule.
func completeVaccinationDriveThroughSOP(t *testing.T, fx *Fixture, versionID, shedID string, goatIDs []string, administeredAt time.Time, key string) string {
	return completeVaccinationObligationThroughSOP(t, fx, versionID, "", shedID, goatIDs, administeredAt, key)
}

func completeVaccinationObligationThroughSOP(t *testing.T, fx *Fixture, versionID, obligationID, shedID string, goatIDs []string, administeredAt time.Time, key string) string {
	t.Helper()
	itemID, lotID := uuid.NewString(), uuid.NewString()
	operatorID, reviewerID := uuid.NewString(), uuid.NewString()
	fx.exec("SOP helper vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, $3, 'E2E SOP helper vaccine', 'vaccine', 'dose')`, itemID, fxTenant, "E2E-"+itemID[:8])
	fx.exec("SOP helper vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, $5, 0, 'dose', CURRENT_DATE + INTERVAL '10 years')`, lotID, fxTenant, itemID, fxPark, len(goatIDs)+10)
	h := newVaccinationSOPHarness(t, fx)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: h.Service, actorID: operatorID}, fx.Inv)
	if _, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{
		SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1,
	}, administeredAt.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("%s sweep generated vaccination work: %v", key, err)
	}
	taskID := ""
	batchID := ""
	if obligationID != "" {
		taskID = fx.scanText(`SELECT ob.sop_task_id::text FROM obligation_instances oi JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id WHERE oi.tenant_id=$1 AND oi.obligation_id=$2::uuid`, fxTenant, obligationID)
		batchID = fx.scanText(`SELECT batch_id::text FROM obligation_instances WHERE tenant_id=$1 AND obligation_id=$2::uuid`, fxTenant, obligationID)
	} else {
		taskID = fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2::uuid ORDER BY created_at DESC LIMIT 1`, fxTenant, versionID)
		batchID = fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2::uuid ORDER BY created_at DESC LIMIT 1`, fxTenant, versionID)
	}
	reservedLotID := reservedLotForBatch(t, fx, batchID)
	submitted, err := h.submit(taskID, shedID, operatorID, reservedLotID, goatIDs, administeredAt, key+":submit", "subcutaneous")
	if err != nil {
		t.Fatalf("%s submit generated vaccination work: %v", key, err)
	}
	accepted, err := h.Service.VerifyTask(fx.Ctx, sopports.ReviewTaskCommand{
		TenantID: fxTenant, ActorID: reviewerID, TaskID: taskID,
		Body: sopdomain.ReviewTaskRequest{Reason: "E2E downstream setup accepted", RowVersion: submitted.Task.RowVersion},
	}, "e2e-review-"+key)
	if err != nil || accepted.Task.State != "accepted" {
		t.Fatalf("%s review generated vaccination work: response=%+v err=%v", key, accepted, err)
	}
	return taskID
}

func reservedLotForBatch(t *testing.T, fx *Fixture, batchID string) string {
	t.Helper()
	return fx.scanText(`
SELECT lot_id::text
FROM inventory_stock_movements
WHERE tenant_id=$1
  AND batch_id=$2::uuid
  AND movement_type='reserve'
ORDER BY occurred_at, movement_id
LIMIT 1`, fxTenant, batchID)
}

func newVaccinationSOPHarness(t *testing.T, fx *Fixture) *vaccinationSOPHarness {
	return newVaccinationSOPHarnessWithBus(t, fx, nil)
}

func newVaccinationSOPHarnessWithBus(t *testing.T, fx *Fixture, bus eventbus.Bus) *vaccinationSOPHarness {
	t.Helper()
	sopService := sopapp.NewService(soppg.NewRepository(fx.Pool, 5*time.Second))
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "e2e-vaccination-proof-secret"))
	vaccinationService := vaccapp.NewService(fx.Vacc)

	// Use provided bus or create a new one if none is provided (for backward compatibility).
	// Production uses the fixture's domain bus (built via BuildDomainBus), which has all handlers already registered.
	if bus == nil {
		bus = eventbus.NewInProcessBus()
		// Register handlers for tests that don't use the fixture's domain bus
		vaccinationCompletion := vaccapp.NewCompletionService(vaccinationService, fx.Obl, fx.Inv)
		vaccapp.NewVerificationHandler(vaccinationCompletion).Register(bus)
		vaccinationBooster := vaccapp.NewBoosterService(fx.Proto, fx.Obl).
			WithGoatReader(fx.Vacc).
			WithCrossVaccineGapReader(fx.Vacc)
		vaccapp.NewVaccinationCompletedHandler(vaccinationService, fx.Obl, vaccinationBooster).Register(bus)
	}

	// Register the generic verification producer and category for vaccination submissions.
	// The submission bridge creates verification items when a SOP task is submitted (R50-013).
	verificationService := verificationapp.NewService(
		verificationpg.NewRepository(fx.Pool, 5*time.Second),
		verificationproofmedia.NewResolver(proofService),
	)
	if err := verificationService.RegisterCategory(verificationdomain.CategoryDefinition{
		Vertical:      "preventive_care",
		Module:        "vaccination",
		Category:      sopbridge.VaccinationVerificationCategory,
		ExpectedMedia: []string{"video"},
	}); err != nil {
		t.Fatalf("register verification category: %v", err)
	}
	sopService.WithProofValidator(proofService).
		WithSubmissionHook(sopbridge.NewVaccinationSubmissionBridge(vaccinationService).
			WithVerificationProducer(verificationService)).
		WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, bus))
	return &vaccinationSOPHarness{t: t, fx: fx, Service: sopService, proofs: proofService}
}

func (h *vaccinationSOPHarness) submit(taskID, shedID, actorID, lotID string, goatIDs []string, administeredAt time.Time, idempotencyKey, routeSite string) (*sopdomain.SubmissionResponse, error) {
	h.t.Helper()
	body, err := h.submissionRequest(taskID, shedID, actorID, lotID, goatIDs, administeredAt, idempotencyKey, routeSite)
	if err != nil {
		return nil, err
	}
	return h.Service.SubmitTask(h.fx.Ctx, sopports.SubmitTaskCommand{
		TenantID: fxTenant, ActorID: actorID, TaskID: taskID, Body: body,
	}, "e2e-submit-"+idempotencyKey)
}

func (h *vaccinationSOPHarness) submissionRequest(taskID, _ string, actorID, lotID string, goatIDs []string, administeredAt time.Time, idempotencyKey, routeSite string) (sopdomain.SubmitTaskRequest, error) {
	h.t.Helper()
	proofRefs := make([]sopdomain.ProofReference, 0, len(goatIDs))
	for _, goatID := range goatIDs {
		goatID := goatID
		target, err := h.proofs.CreateUpload(h.fx.Ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4", ScopeType: "task", ScopeID: taskID,
			SubjectType: "goat", SubjectID: &goatID, UploadedBy: storyAAPtrString(actorID),
			Metadata: map[string]any{
				"capture_source":      "in_app_camera",
				"captured_start_ms":   int64(1000),
				"captured_end_ms":     int64(5200),
				"e2e_idempotency_key": idempotencyKey,
			},
		})
		if err != nil {
			return sopdomain.SubmitTaskRequest{}, err
		}
		stored, err := h.proofs.StoreUpload(h.fx.Ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString(idempotencyKey+":goat:"+goatID))
		if err != nil {
			return sopdomain.SubmitTaskRequest{}, err
		}
		duration := int64(4200)
		if _, err := h.proofs.CompleteUpload(h.fx.Ctx, proofdomain.CompleteUpload{
			TenantID: fxTenant, ProofID: target.Proof.ProofID, ContentHash: stored.ContentHash,
			MimeType: stored.MimeType, SizeBytes: stored.SizeBytes, DurationMS: &duration,
		}); err != nil {
			return sopdomain.SubmitTaskRequest{}, err
		}
		proofRefs = append(proofRefs, sopdomain.ProofReference{ProofID: target.Proof.ProofID})
	}
	goats := make([]any, len(goatIDs))
	for i := range goatIDs {
		goats[i] = goatIDs[i]
	}
	// Vaccination shed completion is an acknowledgement: submit only with per-animal scans and proof.
	// Manual medical fields (vaccine_lot_id, cold_chain_verified, dose_ml_given, route_site,
	// administered_at, adverse_reaction) are removed; administered_at is derived server-side.
	return sopdomain.SubmitTaskRequest{
		SOPVersionID: canonicalVaccinationSOPVersion, IdempotencyKey: idempotencyKey, ProofRefs: proofRefs,
		Answers: map[string]any{
			"goat_ids": goats,
		},
	}, nil
}
