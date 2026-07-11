package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	oblapp "github.com/vgoats/goatos/backend/internal/obligation/app"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	vacchttp "github.com/vgoats/goatos/backend/internal/vaccination/adapters/http"
	vaccapp "github.com/vgoats/goatos/backend/internal/vaccination/app"
)

// TestKernelStoryAH_VerificationQueuePagesPastFormerCap proves that the real generation,
// sweeper-created SOP task, canonical proof/submission bridge, HTTP handler, opaque cursor and
// database ordering expose every item beyond the old 200-row frontend preload. One real 251-goat
// batch submission gives every completion the same administered timestamp and exercises the UUID
// tie-breaker across every page boundary.
func TestKernelStoryAH_VerificationQueuePagesPastFormerCap(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-ah", "Verification queue reaches every row beyond the former 200 cap",
		"Two hundred and fifty-one real generated obligations are recorded for review. Operators traverse the production HTTP queue at 10, 25 and 50 rows per page without skips, duplicates, false totals or a hidden client cap.")
	defer story.Finish()

	const (
		shedID   = "82000000-0000-4000-8000-000000000001"
		stageID  = "82000000-0000-4000-8000-000000000002"
		operator = "82000000-0000-4000-8000-000000000003"
		parkHead = "82000000-0000-4000-8000-000000000004"
		verifier = "82000000-0000-4000-8000-000000000005"
		itemID   = "82000000-0000-4000-8000-000000000006"
		stockLot = "82000000-0000-4000-8000-000000000007"
	)
	fx.SeedShed(shedID, "E2E-AH", stageID)
	fx.SeedWorkforce(operator, parkHead, verifier, shedID)
	asOf := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	dob := asOf.AddDate(0, 0, -21)
	goatIDs := make([]string, 0, 251)
	for i := 0; i < 251; i++ {
		goatID := fmt.Sprintf("82000000-0000-4000-8000-%012x", i+100)
		goatIDs = append(goatIDs, goatID)
		fx.SeedGoat(GoatSpec{GoatID: goatID, ShedID: shedID, DOB: &dob})
	}
	versionID, ruleID := fx.PublishSimpleProtocol("vaccination.e2e.story_ah", 21, 7, nil)
	generated, err := vaccapp.NewGenerationService(fx.Proto, fx.Vacc, fx.Obl).GenerateForVersion(fx.Ctx, fxTenant, versionID, asOf)
	story.Assert("production generation created all queue candidates", err == nil && generated.Generated >= 251, "generated=%d err=%v", generated.Generated, err)

	generatedCount := fx.countRows(`SELECT count(*) FROM obligation_instances WHERE tenant_id=$1 AND protocol_version_id=$2::uuid AND rule_id=$3::uuid`, fxTenant, versionID, ruleID)
	story.Assert("exactly 251 authored goats have generated work", generatedCount == 251, "obligations=%d", generatedCount)

	story.Step("Create one real drive and submit all 251 goats through the canonical SOP bridge",
		"The story reserves real stock, requires a sweeper-created task, uploads canonical proof, and sends one per-goat SOP payload. The VaccinationSubmissionBridge—not test SQL or RecordCompletion—materializes the verification queue.")
	fx.exec("vaccine item", `INSERT INTO inventory_items (item_id, tenant_id, item_code, name, category, base_unit)
		VALUES ($1, $2, 'VAC-E2E-AH', 'E2E Story AH vaccine', 'vaccine', 'dose')`, itemID, fxTenant)
	fx.exec("vaccine stock", `INSERT INTO inventory_stock (stock_id, tenant_id, item_id, location_id, quantity_in_stock, quantity_reserved, quantity_unit, expiry_date)
		VALUES ($1, $2, $3, $4, 300, 0, 'dose', CURRENT_DATE + INTERVAL '180 days')`, stockLot, fxTenant, itemID, shedID)
	sopHarness := newVaccinationSOPHarness(t, fx)
	sweeper := oblapp.NewSweeperService(fx.Obl, storyAATaskCreator{service: sopHarness.Service, actorID: operator}, fx.Inv)
	sweep, err := sweeper.SweepVersion(fx.Ctx, fxTenant, versionID, oblapp.SweepConfig{
		SOPVersionID: canonicalVaccinationSOPVersion, VaccineItemID: itemID, DosesPerGoat: 1,
	}, asOf.AddDate(0, 0, 1))
	story.Assert("real sweep formed one 251-goat drive", err == nil && sweep.Batches == 1 && sweep.Obligations == 251,
		"batches=%d obligations=%d err=%v", sweep.Batches, sweep.Obligations, err)
	if err != nil {
		return
	}
	batchID := fx.scanText(`SELECT batch_id::text FROM obligation_batches WHERE tenant_id=$1 AND protocol_version_id=$2::uuid`, fxTenant, versionID)
	taskID := fx.scanText(`SELECT sop_task_id::text FROM obligation_batches WHERE tenant_id=$1 AND batch_id=$2::uuid`, fxTenant, batchID)
	story.Assert("sweeper created the executable SOP task", taskID != "", "task_id=%q", taskID)
	administeredAt := asOf.Add(12 * time.Hour)
	submitted, err := sopHarness.submit(taskID, shedID, operator, stockLot, goatIDs, administeredAt, "story-ah-submit-251", "subcutaneous")
	submittedState := ""
	if submitted != nil {
		submittedState = submitted.Task.State
	}
	story.Assert("canonical 251-goat SOP submission succeeded", err == nil && submittedState == "needs_review", "state=%q err=%v", submittedState, err)
	if err != nil {
		return
	}
	recordedCount := fx.countRows(`SELECT count(*) FROM vaccination_completions WHERE tenant_id=$1 AND batch_id=$2::uuid AND status='recorded'`, fxTenant, batchID)
	story.Assert("submission fanout materialized all 251 review rows", recordedCount == 251, "recorded=%d", recordedCount)

	story.Step("Traverse the production HTTP endpoint at every supported operator page size",
		"Each response contains only the requested visible page, an exact total, and an opaque next cursor. Every completion must appear exactly once.")
	mux := http.NewServeMux()
	vacchttp.Register(mux, vacchttp.NewHandler(vaccapp.NewService(fx.Vacc), nil))
	for _, pageSize := range []int{10, 25, 50} {
		seen := map[string]struct{}{}
		cursor := ""
		for page := 1; ; page++ {
			path := fmt.Sprintf("/vaccination/verification-queue?limit=%d", pageSize)
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req = req.WithContext(httpmiddleware.WithTenantID(req.Context(), fxTenant))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("size %d page %d HTTP=%d body=%s", pageSize, page, rec.Code, rec.Body.String())
			}
			var response struct {
				Items []struct {
					CompletionID string `json:"completion_id"`
				} `json:"items"`
				TotalCount int64   `json:"total_count"`
				NextCursor *string `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("size %d page %d decode: %v", pageSize, page, err)
			}
			if response.TotalCount != 251 || len(response.Items) > pageSize {
				t.Fatalf("size %d page %d total=%d items=%d", pageSize, page, response.TotalCount, len(response.Items))
			}
			for _, item := range response.Items {
				if _, duplicate := seen[item.CompletionID]; duplicate {
					t.Fatalf("size %d page %d duplicated %s", pageSize, page, item.CompletionID)
				}
				seen[item.CompletionID] = struct{}{}
			}
			if response.NextCursor == nil {
				break
			}
			cursor = *response.NextCursor
		}
		story.Assert(fmt.Sprintf("page size %d reaches all rows exactly once", pageSize), len(seen) == 251, "seen=%d", len(seen))
	}
}
