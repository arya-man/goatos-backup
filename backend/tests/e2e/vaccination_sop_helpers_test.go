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
		 VALUES ($1, $2, $3, $4, $5, 0, 'dose', CURRENT_DATE + INTERVAL '10 years')`, lotID, fxTenant, itemID, shedID, len(goatIDs)+10)
	h := newVaccinationSOPHarness(t, fx)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: h.Service, actorID: operatorID}, fx.Inv)
	if _, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{
		SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1,
	}, administeredAt.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("%s sweep generated vaccination work: %v", key, err)
	}
	taskID := ""
	if obligationID != "" {
		taskID = fx.scanText(`SELECT ob.sop_task_id::text FROM obligation_instances oi JOIN obligation_batches ob ON ob.tenant_id=oi.tenant_id AND ob.batch_id=oi.batch_id WHERE oi.tenant_id=$1 AND oi.obligation_id=$2::uuid`, fxTenant, obligationID)
	} else {
		taskID = fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2::uuid ORDER BY created_at DESC LIMIT 1`, fxTenant, versionID)
	}
	submitted, err := h.submit(taskID, shedID, operatorID, lotID, goatIDs, administeredAt, key+":submit", "subcutaneous")
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

func newVaccinationSOPHarness(t *testing.T, fx *Fixture) *vaccinationSOPHarness {
	t.Helper()
	sopService := sopapp.NewService(soppg.NewRepository(fx.Pool, 5*time.Second))
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "e2e-vaccination-proof-secret"))
	vaccinationService := vaccapp.NewService(fx.Vacc)
	verificationBus := eventbus.NewInProcessBus()
	vaccapp.NewVerificationHandler(vaccapp.NewCompletionService(vaccinationService, fx.Obl, fx.Inv)).Register(verificationBus)
	sopService.WithProofValidator(proofService).
		WithSubmissionHook(sopbridge.NewVaccinationSubmissionBridge(vaccinationService)).
		WithTaskReviewFanout(sopbridge.NewVerifyFanout(vaccinationService, verificationBus))
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

func (h *vaccinationSOPHarness) submissionRequest(taskID, shedID, actorID, lotID string, goatIDs []string, administeredAt time.Time, idempotencyKey, routeSite string) (sopdomain.SubmitTaskRequest, error) {
	h.t.Helper()
	proofRefs := make([]sopdomain.ProofReference, 0, 3)
	proofIDs := make(map[string]string, 3)
	for _, subject := range []string{"shed", "vial_lot", "administration"} {
		var subjectID *string
		if subject == "shed" {
			subjectID = storyAAPtrString(shedID)
		}
		target, err := h.proofs.CreateUpload(h.fx.Ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4", ScopeType: "task", ScopeID: taskID,
			SubjectType: subject, SubjectID: subjectID, UploadedBy: storyAAPtrString(actorID),
			Metadata: map[string]any{"e2e_idempotency_key": idempotencyKey},
		})
		if err != nil {
			return sopdomain.SubmitTaskRequest{}, err
		}
		if _, err := h.proofs.StoreUpload(h.fx.Ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString(idempotencyKey+":"+subject)); err != nil {
			return sopdomain.SubmitTaskRequest{}, err
		}
		proofRefs = append(proofRefs, sopdomain.ProofReference{ProofID: target.Proof.ProofID})
		proofIDs[subject] = target.Proof.ProofID
	}
	goats := make([]any, len(goatIDs))
	for i := range goatIDs {
		goats[i] = goatIDs[i]
	}
	return sopdomain.SubmitTaskRequest{
		SOPVersionID: canonicalVaccinationSOPVersion, IdempotencyKey: idempotencyKey, ProofRefs: proofRefs,
		Answers: map[string]any{
			"vaccine_lot_id": lotID, "cold_chain_verified": true, "goat_ids": goats,
			"dose_ml_given": 1.0, "doses": 1, "route_site": routeSite, "administered_at": administeredAt.Format(time.RFC3339),
			"adverse_reaction": false, "shed_video": proofIDs["shed"], "vial_lot_video": proofIDs["vial_lot"],
			"administration_video": proofIDs["administration"],
		},
	}, nil
}
