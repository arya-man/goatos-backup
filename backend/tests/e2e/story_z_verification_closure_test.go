package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
	verifhttp "github.com/vgoats/goatos/backend/internal/verification/adapters/http"
	verifapp "github.com/vgoats/goatos/backend/internal/verification/app"
)

// TestKernelStoryZ_VerificationClosureRealEndpoints verifies the production verification closure path
// for vaccination submissions: record verdicts and close items via the real generic verification API
// endpoints, not the legacy SOP admin-verify route. Asserts that leadership close emits the correct
// domain event and that a new successor obligation is created post-completion.
func TestKernelStoryZ_VerificationClosureRealEndpoints(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-z", "Verification closure via real generic verification API",
		"A vaccination submission creates verified items. The verifier records verdicts and "+
			"leadership closes items via the real /verification/items/{id}/verdict and /close endpoints. "+
			"Domain events are emitted, obligationinstances complete, and successor obligations are created.")
	story.Certify("backend kernel + real verification API + generic verification domain events")
	defer story.Finish()

	const (
		shedID     = "e4000000-0000-4000-8000-000000000001"
		stageID    = "e4000000-0000-4000-8000-000000000002"
		operatorID = "e4000000-0000-4000-8000-000000000003"
		parkHeadID = "e4000000-0000-4000-8000-000000000004"
		verifierID = "e4000000-0000-4000-8000-000000000005"
		goat1      = "e4000000-0000-4000-8000-000000000006"
		goat2      = "e4000000-0000-4000-8000-000000000007"
		itemID     = "e4000000-0000-4000-8000-000000000008"
		lotID      = "e4000000-0000-4000-8000-000000000009"
	)

	story.Step("Seed the shed, workforce, protocol, and two goats sharing a shed",
		"One park/shed/stage topology with operator, park head, and verifier. One published PC "+
			"vaccination rule. Two goats born the same day in the same shed.")
	fx.SeedShed(shedID, "E2E-Z", stageID)
	fx.SeedWorkforce(operatorID, parkHeadID, verifierID, shedID)

	// Two-dose protocol: a birth-age primary dose plus a booster dose triggered
	// after_previous_completion. Completing the primary via verification closure must let SM-7
	// (VaccinationCompletedHandler -> BoosterService) schedule the booster successor obligation.
	versionID, ruleID, boosterRuleID := fx.PublishBoosterProtocol("vaccination.e2e.story_z", 21, 0, 21)

	now := time.Now().UTC()
	dob := now.AddDate(0, 0, -53)
	fx.SeedGoat(GoatSpec{GoatID: goat1, ShedID: shedID, DOB: &dob})
	fx.SeedGoat(GoatSpec{GoatID: goat2, ShedID: shedID, DOB: &dob})

	fx.exec("vaccine item",
		`INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		 VALUES ($1, $2, 'VAC-E2E-Z', 'E2E Story Z vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock",
		`INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		 VALUES ($1, $2, $3, $4, 10, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, lotID, fxTenant, itemID, fxPark)

	story.Step("Generate + batch: the sweeper combines both goats into one park drive",
		"Run generation and the real obligation sweeper (SM-4) to create one drive with both goats.")

	gen := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl)
	genRes, err := gen.GenerateForVersion(fx.Ctx, fxTenant, versionID, now)
	story.Assert("generation ran without error", err == nil, "err=%v", err)
	story.Assert("both goats' doses were generated", genRes.Generated == 2, "generated=%d", genRes.Generated)

	sopHarness := newVaccinationSOPHarnessWithBus(t, fx, fx.Bus)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: sopHarness.Service, actorID: operatorID}, fx.Inv)
	sweepCfg := oblapp.SweepConfig{SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1}
	dueBefore := now.AddDate(0, 0, 1)
	sweepRes, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, sweepCfg, dueBefore)
	story.Assert("sweep ran without error", err == nil, "err=%v", err)
	story.Assert("both obligations combined into a single park drive", sweepRes.Batches == 1, "batches=%d", sweepRes.Batches)

	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2`, fxTenant, versionID)
	taskID := fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("sweeper created the executable SOP task", taskID != "", "task_id=%q", taskID)
	reservedLot := reservedLotForBatch(t, fx, batchID)

	story.Step("Capture canonical proof and submit both administrations",
		"The operator submits a real SOP task submission for both goats. Submission fanout records completions "+
			"and creates verification items in the generic verification module.")
	administeredAt := now.Add(12 * time.Hour)
	submitted, err := sopHarness.submit(taskID, shedID, operatorID, reservedLot, []string{goat1, goat2}, administeredAt, "story-z-submit", "subcutaneous")
	story.Assert("canonical two-goat submission succeeded", err == nil, "err=%v", err)
	if err != nil {
		return
	}
	submissionID := submitted.Submission.SubmissionID

	recordedCount := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND batch_id=$2::uuid AND status='recorded'`, fxTenant, batchID)
	story.Assert("submission fanout recorded both doses", recordedCount == 2, "recorded=%d", recordedCount)

	// Assert that verification items were created for this submission
	itemCount := fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_submission_id=$2::uuid`, fxTenant, submissionID)
	story.Assert("submission created verification items in generic module", itemCount == 2, "items=%d", itemCount)

	story.Step("Verifier records verdict for each verification item via real API",
		"For each verification item, POST /verification/items/{item_id}/verdict to record an approval decision.")
	// Get all verification item IDs for this submission
	itemRows, _ := fx.Pool.Query(fx.Ctx,
		`SELECT item_id::text FROM verification_items WHERE tenant_id=$1 AND source_submission_id=$2::uuid ORDER BY item_id`,
		fxTenant, submissionID)
	itemIDs := []string{}
	for itemRows.Next() {
		var itemID string
		itemRows.Scan(&itemID)
		itemIDs = append(itemIDs, itemID)
	}
	itemRows.Close()
	story.Assert("found verification items to verdict", len(itemIDs) > 0, "items=%d", len(itemIDs))

	verifService := verifapp.NewService(fx.VerifRepo, fx.VerifMedia)
	verifHandler := verifhttp.NewHandler(verifService)
	mux := http.NewServeMux()
	verifhttp.Register(mux, verifHandler)

	// Set up auth for two roles: verifier (for verdict) and park head (for close).
	// RoleVerifier has VerificationReview; RoleParkHead has VerificationAct.
	grantSource := storyAAGrantSource{byActor: map[string][]permissions.ActiveGrant{
		verifierID: {{Role: permissions.RoleVerifier, ScopeType: "tenant", ScopeID: fxTenant}},
		parkHeadID: {{Role: permissions.RoleParkHead, ScopeType: "tenant", ScopeID: fxTenant}},
	}}
	auth, authErr := httpmiddleware.NewAuthMiddleware(httpmiddleware.AuthConfig{
		Mode: httpmiddleware.AuthModeDevHeaders, DevHeadersAllowed: true, Environment: "test",
	}, nil, grantSource, nil)
	story.Assert("built auth middleware", authErr == nil, "err=%v", authErr)

	app := auth.Wrap(mux)
	verdictedCount := 0
	for _, itemID := range itemIDs {
		var rowVersion int
		fx.Pool.QueryRow(fx.Ctx, `SELECT row_version FROM verification_items WHERE tenant_id=$1 AND item_id=$2::uuid`, fxTenant, itemID).Scan(&rowVersion)
		body := fmt.Sprintf(`{"decision":"approved","reason":"proof accepted","row_version":%d}`, rowVersion)

		idemKey := fmt.Sprintf("verdict-%s", itemID)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/verification/items/%s/verdict", itemID), bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", idemKey)
		req.Header.Set(httpmiddleware.TenantContextHeader, fxTenant)
		req.Header.Set("X-GoatOS-Actor-ID", verifierID)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			var respBody struct {
				Item struct {
					Status string `json:"status"`
				} `json:"item"`
			}
			if json.Unmarshal(rec.Body.Bytes(), &respBody) == nil && respBody.Item.Status == "approved" {
				verdictedCount++
			}
		}
		story.Assert(fmt.Sprintf("verdict recorded for item %s", itemID[:8]), rec.Code == http.StatusOK, "HTTP=%d body=%s", rec.Code, rec.Body.String())
		// Relay outbox events from verdict so handlers can process them
		fx.RelayOutboxEvents()
	}
	story.Assert("all verdicts recorded", verdictedCount == 2, "verdicted=%d", verdictedCount)

	story.Step("Leadership closes the submitted verification items via real API",
		"Items created by an SOP submission must be closed as a batch via POST /verification/submissions/{submission_id}/close, not individually. This closes all verdicted items in the submission atomically.")

	// Close the whole submission (all verification items in it) via the batch close endpoint,
	// which is the correct production path for SOP-submitted items with source_submission_id set.
	body := `{}`
	idemKey := fmt.Sprintf("close-submission-%s", submissionID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/verification/submissions/%s/close", submissionID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idemKey)
	req.Header.Set(httpmiddleware.TenantContextHeader, fxTenant)
	req.Header.Set("X-GoatOS-Actor-ID", parkHeadID) // Leadership uses park head role for closure
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		story.Assert("submission closed successfully", false, "HTTP=%d body=%s", rec.Code, rec.Body.String())
		return
	}
	var closeResp struct {
		Items []struct {
			Status   string  `json:"status"`
			ClosedAt *string `json:"closed_at"`
		} `json:"items"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &closeResp) == nil {
		closedCount := 0
		for _, item := range closeResp.Items {
			if item.Status == "approved" && item.ClosedAt != nil {
				closedCount++
			}
		}
		story.Assert("all items closed in batch", closedCount == len(itemIDs), "closed=%d expected=%d", closedCount, len(itemIDs))
	} else {
		story.Assert("submission close returned valid response", false, "body=%s", rec.Body.String())
		return
	}
	// Relay outbox events from close so handlers process the closure and completion
	fx.RelayOutboxEvents()

	story.Step("Verify domain event was emitted and completion/obligation states updated",
		"After leadership close, the verification.item.closed event is emitted, completions accepted, "+
			"and obligations completed.")

	// Check that all verification items are now closed
	// Note: status remains 'approved' after closing; the closed_at timestamp indicates closure.
	allClosedCount := fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_submission_id=$2::uuid AND status='approved' AND closed_at IS NOT NULL`,
		fxTenant, submissionID)
	story.Assert("all verification items are closed (approved with closed_at set)", allClosedCount == 2, "closed=%d", allClosedCount)

	// Check that the vaccination completion is accepted and the obligation is completed
	completionCount := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND batch_id=$2::uuid AND status='accepted'`,
		fxTenant, batchID)
	story.Assert("completions accepted after verification closure", completionCount == 2, "accepted=%d", completionCount)

	obligationCompleteCount := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND batch_id=$2::uuid AND status='completed'`,
		fxTenant, batchID)
	story.Assert("obligations completed after verification closure", obligationCompleteCount == 2, "completed=%d", obligationCompleteCount)

	story.Step("Verify new successor obligation was created post-completion",
		"After the primary obligation is completed, the vaccination.completed event triggers generation "+
			"of the booster/next-dose obligation. Assert a NEW obligation exists with a different obligation_id.")
	// Get the primary obligation count
	primaryOblCount := fx.countRows(
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND batch_id=$2::uuid AND rule_id=$3::uuid AND status='completed'`,
		fxTenant, batchID, ruleID)
	story.Assert("found primary obligations completed", primaryOblCount >= 2, "obls=%d", primaryOblCount)

	// The successor (booster) obligation is created by SM-7 (VaccinationCompletedHandler ->
	// BoosterService.ScheduleNextDose) off the vaccination.completed event drained above, never by
	// test SQL. It carries the booster rule id (distinct from the completed primary rule id) and a
	// distinct obligation_id per goat.
	observedBoosterRuleID := fx.scanText(
		`SELECT DISTINCT rule_id::text FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2 AND rule_id<>$3 LIMIT 1`,
		fxTenant, versionID, ruleID)
	story.Assert("successor obligation carries the booster rule id", observedBoosterRuleID == boosterRuleID, "observed=%q expected=%q", observedBoosterRuleID, boosterRuleID)
	if observedBoosterRuleID != "" {
		boosterCount := fx.countRows(
			`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND ((target_id=$2::uuid OR target_id=$3::uuid)) AND rule_id=$4::uuid AND status IN ('scheduled','due')`,
			fxTenant, goat1, goat2, boosterRuleID)
		story.Assert("new booster obligations created", boosterCount == 2, "boosters=%d", boosterCount)
	}

	// Verify outbox event for verification.item.closed exists
	closedEventCount := fx.countRows(
		`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='verification.item.closed' AND status='published'`,
		fxTenant)
	story.Assert("verification.item.closed events published", closedEventCount >= 1, "events=%d", closedEventCount)

	story.Step("Idempotency: replay submission close is a no-op",
		"A second submission-close request with the same Idempotency-Key returns the same result "+
			"without duplicate state changes.")
	// Replay the exact submission-close request issued above (same endpoint, body, and
	// Idempotency-Key). The generic verification module must return the already-closed items and
	// perform no new state change.
	replayReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/verification/submissions/%s/close", submissionID), bytes.NewBufferString(body))
	replayReq.Header.Set("Content-Type", "application/json")
	replayReq.Header.Set("Idempotency-Key", idemKey)
	replayReq.Header.Set(httpmiddleware.TenantContextHeader, fxTenant)
	replayReq.Header.Set("X-GoatOS-Actor-ID", parkHeadID) // Same actor who closed it the first time
	replayRec := httptest.NewRecorder()
	app.ServeHTTP(replayRec, replayReq)

	story.Assert("idempotent replay returns OK", replayRec.Code == http.StatusOK, "HTTP=%d body=%s", replayRec.Code, replayRec.Body.String())

	var replayResp struct {
		Items []struct {
			Status   string  `json:"status"`
			ClosedAt *string `json:"closed_at"`
		} `json:"items"`
	}
	json.Unmarshal(replayRec.Body.Bytes(), &replayResp)
	replayClosed := 0
	for _, item := range replayResp.Items {
		if item.Status == "approved" && item.ClosedAt != nil {
			replayClosed++
		}
	}
	story.Assert("idempotent replay returns already-closed items", replayClosed == len(itemIDs), "closed=%d expected=%d", replayClosed, len(itemIDs))

	// The replay must not have created any additional booster obligations (no duplicate side effects).
	fx.RelayOutboxEvents()
	boosterAfterReplay := fx.countRows(
		`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND ((target_id=$2::uuid OR target_id=$3::uuid)) AND rule_id=$4::uuid AND status IN ('scheduled','due')`,
		fxTenant, goat1, goat2, boosterRuleID)
	story.Assert("no duplicate booster obligations after idempotent replay", boosterAfterReplay == 2, "boosters=%d", boosterAfterReplay)
}
