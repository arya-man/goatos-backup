package e2e

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsproof "github.com/vgoats/goatos/backend/internal/counts/adapters/proof"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	countsdomain "github.com/vgoats/goatos/backend/internal/counts/domain"
	countsbridge "github.com/vgoats/goatos/backend/internal/countsbridge"
	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	verificationcatalog "github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

// TestKernelStory_UhtMilkStockDepletesOnce is the end-to-end proof that a litre of UHT milk leaves
// the store EXACTLY ONCE, driven through the production path the operator and the verifier actually
// use.
//
// The rule (maintainer decision 2026-08-27, migration 000216): UHT stock depletes from the Milk
// Preparation workflow ON SUBMIT. The operator's entered quantity IS the measurement, so holding it
// behind review would make every stock card lag the verification queue. The verifier's approve
// decides whether the VIDEO was good; it does not decide whether the milk was drunk.
//
// The defect this story exists to keep dead (found 2026-09-02): the earlier 2026-08-22 design also
// forwarded the accepted litres into the feed_external_consumption LEDGER on approve, keyed at
// preparation_date, while the view books the same milk at feeding_date (= preparation_date + 1, a
// DB CHECK). The ledger row was therefore never suppressed by its own preparation and the milk was
// deducted TWICE. It looked fine only because a farm preparing milk EVERY day has yesterday's
// preparation masking today's ledger date by coincidence. The recorder is deleted, and migration
// 000241 additionally suppresses a ledger row on EITHER of a preparation's two dates, so a second
// writer -- the legacy sheet importer on the crossover day, or a reintroduced fan-out -- still
// cannot double-deplete.
//
// The chain it walks:
//
//	operator  countsapp.SubmitMilkPreparation   real proof artifacts -> real attempt row
//	                                            -> countsbridge enqueue -> a real verification item
//	stock     feeddirection.StockAnalytics      the card the Feed Analytics screen renders
//	verifier  verification.RecordVerdict        -> outbox -> domain consumer -> the registered
//	                                            appliers (the SAME eventwiring list production uses)
//
// Nothing asserted is hand-seeded: the fixture inserts only external input facts (tenant, park,
// the feed catalog row, the purchased load, and -- for the fallback cases -- legacy sheet ledger
// rows, which is exactly what cmd/import-feed-external-consumption writes).
func TestKernelStory_UhtMilkStockDepletesOnce(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-uht-milk-stock-depletes-once",
		"UHT milk leaves the store once: on submit, and the approve changes nothing",
		"The operator types how many litres of UHT he opened, films it, and submits. That number is the "+
			"measurement, so the stock card moves immediately -- the farm should not have to wait for a "+
			"verifier to know how much milk is left. When the verifier then approves the video, the balance "+
			"must not move again. An earlier design copied the litres into the old consumption ledger at that "+
			"moment, dated a day apart from the preparation itself, and quietly took the same milk out of the "+
			"store twice. This walks submit, approve, a legacy sheet row on the preparation date, and a genuine "+
			"pre-workflow day, and holds the balance to the litre at every step.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	const (
		park     = "ee000000-0000-4000-8000-000000003001"
		operator = "ee000000-0000-4000-8000-000000005001"
		verifier = "ee000000-0000-4000-8000-000000005002"
	)

	seedUhtStockScope(t, ctx, fx.Pool, park)

	countsRepo := countspg.NewRepository(fx.Pool, 10*time.Second)
	feedRepo := feeddirectionpg.NewRepository(fx.Pool, 10*time.Second)
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	// The category comes from the SHARED catalog the API registers from, so the item this story
	// raises is byte-for-byte the one a real verifier's queue is composed from.
	if err := verification.RegisterCategory(verificationcatalog.MilkPreparation); err != nil {
		t.Fatalf("register milk preparation category: %v", err)
	}
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-uht-proof-secret"))
	counts := countsapp.NewHerdRegisterService(countsRepo).
		WithMilkPreparationProofValidator(countsproof.NewValidator(fx.Proof)).
		WithMilkPreparationVerificationEnqueuer(countsbridge.NewMilkPreparationVerificationEnqueuer(verification))

	// balance reads the UHT card off the real Feed Analytics stock read -- the same function the
	// /feed-analytics/stock endpoint serves, so this story asserts what the screen shows.
	balance := func() string {
		t.Helper()
		res, err := feedRepo.StockAnalytics(ctx, fxTenant, feeddirectiondomain.DirectedAnalyticsQuery{})
		if err != nil {
			t.Fatalf("StockAnalytics: %v", err)
		}
		for i := range res.Items {
			if res.Items[i].FeedItemLabel == "UHT Milk" {
				return res.Items[i].BalanceKg
			}
		}
		return ""
	}

	// captureStepVideo shoots one in-app-camera clip bound to this park and this preparation step,
	// exactly as the phone does; the counts proof validator refuses anything else.
	captureStepVideo := func(step string) string {
		t.Helper()
		subject := park
		uploader := operator
		target, err := proofService.CreateUpload(ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4",
			ScopeType: "park", ScopeID: park,
			SubjectType: "other", SubjectID: &subject, UploadedBy: &uploader,
			Metadata: map[string]any{
				"capture_source":    "in_app_camera",
				"field_key":         "milk_preparation_" + step,
				"captured_start_ms": int64(1000),
				"captured_end_ms":   int64(6000),
			},
		})
		if err != nil {
			t.Fatalf("create %s upload: %v", step, err)
		}
		stored, err := proofService.StoreUpload(ctx, fxTenant, target.Proof.ProofID, "video/mp4",
			bytes.NewBufferString("story-uht-"+step+"-"+target.Proof.ProofID))
		if err != nil {
			t.Fatalf("store %s upload: %v", step, err)
		}
		duration := int64(5000)
		if _, err := proofService.CompleteUpload(ctx, proofdomain.CompleteUpload{
			TenantID: fxTenant, ProofID: target.Proof.ProofID, ContentHash: stored.ContentHash,
			MimeType: stored.MimeType, SizeBytes: stored.SizeBytes, DurationMS: &duration,
		}); err != nil {
			t.Fatalf("complete %s upload: %v", step, err)
		}
		return target.Proof.ProofID
	}

	prepDay := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)

	// -----------------------------------------------------------------------
	story.Step("The operator opens 28 litres of UHT, films it, and submits",
		"Milk prepared on the 24th is fed on the 25th (a DB check enforces that pairing). The submit "+
			"stores one immutable attempt and raises one verification item carrying both step videos.")

	result, err := counts.SubmitMilkPreparation(ctx, countsdomain.MilkPreparationSubmission{
		TenantID: fxTenant, ParkID: park, PreparationDate: prepDay, GoatMilkUsed: false,
		Answers: countsdomain.MilkPreparationAnswers{
			MorningMilkCollectedLitres: 2, EveningMilkCollectedLitres: 2,
			UHTMilkQuantityLitres: 28, CitricAcidGrams: 120,
		},
		Proofs: countsdomain.MilkPreparationProofs{
			UHTMilkQuantityProofRef:  captureStepVideo(countsdomain.MilkPreparationStepUHTMilkQuantity),
			CitricAcidMixingProofRef: captureStepVideo(countsdomain.MilkPreparationStepCitricAcidMixing),
		},
		SubmittedBy: operator, SubmittedAt: time.Date(2026, 8, 24, 6, 30, 0, 0, time.UTC),
		IdempotencyKey: "story-uht-prep-1", TraceID: "story-uht-trace-1",
	})
	story.Assert("the submit was accepted and is awaiting review", err == nil && result.Status == "pending_verification",
		"status=%q err=%v", result.Status, err)
	if err != nil {
		return
	}

	story.Assert("the store moves on SUBMIT: 600 kg bought, 28 litres opened, 572.0 left",
		balance() == "572.0",
		"balance=%q -- a card that waits for the verifier lags the whole review queue", balance())

	// -----------------------------------------------------------------------
	story.Step("The verifier approves the video",
		"The verdict travels the real outbox -> domain-consumer -> registered-appliers path. It decides "+
			"whether the CLIP was good. The milk was already drunk, so the balance must not move.")

	itemID := fx.scanText(`SELECT item_id::text FROM verification_items
WHERE tenant_id=$1 AND source_ref_type='milk_preparation_completion' AND source_ref_id=$2`,
		fxTenant, result.CompletionID)
	story.Assert("the submit raised exactly one verification item for the preparation", itemID != "",
		"item_id=%q", itemID)
	if itemID == "" {
		return
	}

	if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{
		TenantID: fxTenant, ItemID: itemID, Decision: verificationdomain.DecisionApproved,
		VerifierID: verifier, Reason: "all steps visible",
		RowVersion:     verificationItemRowVersion(t, ctx, fx.Pool, itemID),
		IdempotencyKey: "story-uht-verdict-1",
	}); err != nil {
		t.Fatalf("RecordVerdict(approved): %v", err)
	}
	fx.RelayOutboxEvents()

	story.Assert("the approve completed the preparation",
		fx.scanText(`SELECT status FROM milk_preparation_completions WHERE tenant_id=$1 AND completion_id=$2::uuid`,
			fxTenant, result.CompletionID) == "completed",
		"status=%q", fx.scanText(`SELECT status FROM milk_preparation_completions WHERE tenant_id=$1 AND completion_id=$2::uuid`,
			fxTenant, result.CompletionID))

	story.Assert("the balance did NOT move again: still 572.0 after the approve",
		balance() == "572.0",
		"balance=%q -- 544.0 is the retired recorder deducting the same 28 litres a second time", balance())

	story.Assert("the approve wrote no consumption-ledger row of its own",
		fx.countRows(`SELECT count(*) FROM feed_external_consumption WHERE tenant_id=$1`, fxTenant) == 0,
		"ledger rows=%d -- the workflow owns this fact; a second writer can only disagree with it",
		fx.countRows(`SELECT count(*) FROM feed_external_consumption WHERE tenant_id=$1`, fxTenant))

	// A redelivered verdict is the at-least-once case the bus really produces.
	fx.RelayOutboxEvents()
	story.Assert("a redelivered verdict still leaves the balance at 572.0", balance() == "572.0",
		"balance=%q", balance())

	// -----------------------------------------------------------------------
	story.Step("A legacy sheet row lands on the PREPARATION date",
		"The Feed DB sheet books UHT on the day it is prepared; the workflow books it on the day it is "+
			"fed. On the handover day both records describe the SAME milk, one day apart, and the old "+
			"precedence rule counted both. Migration 000241 suppresses the ledger row on either date.")

	seedUhtLedgerRow(t, ctx, fx.Pool, park, "2026-08-24", "28.000")
	story.Assert("the same milk from the sheet is superseded, not added: still 572.0",
		balance() == "572.0",
		"balance=%q -- 544.0 means the preparation and its own sheet row were both deducted", balance())

	// -----------------------------------------------------------------------
	story.Step("A day before the workflow existed still depletes from the ledger",
		"The ledger is kept precisely for the history the module predates. A day no preparation touches "+
			"on either date must still leave the store, or the balance silently overstates the milk on hand.")

	seedUhtLedgerRow(t, ctx, fx.Pool, park, "2026-08-20", "40.000")
	story.Assert("the pre-workflow day depletes: 572.0 - 40 = 532.0", balance() == "532.0",
		"balance=%q -- suppression must be per covered day, never a blanket ignore of the ledger", balance())
}

func seedUhtStockScope(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park string) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed uht stock scope: %v\nsql: %s", err, sql)
		}
	}
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'UHT-CBE', 'CBE', 'active')
ON CONFLICT (location_id) DO NOTHING`, fxTenant, park)
	exec(`INSERT INTO feed_item_catalog (tenant_id, feed_item_label, display_order, status)
VALUES ($1::uuid, 'UHT Milk', 1, 'active')
ON CONFLICT DO NOTHING`, fxTenant)
	// One purchased load: 600 kg landed on the farm and depleting from the 18th.
	exec(`INSERT INTO feed_purchases (tenant_id, park_id, farm_label, feed_item_label, batch_no,
                            purchase_date, quantity_kg, per_kg_cost, total_cost,
                            consumed_at_import_kg, depletes_from, vendor, payment_status)
VALUES ($1::uuid, $2::uuid, 'CBE', 'UHT Milk', 326, DATE '2026-08-07', 600, 63.64, 38184,
        0, DATE '2026-08-18', 'Balamurugan Enterprises', 'Pending')`, fxTenant, park)
}

// seedUhtLedgerRow writes the legacy sheet ledger exactly as cmd/import-feed-external-consumption
// does -- an external input fact, never a derived one.
func seedUhtLedgerRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park, day, qty string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
INSERT INTO feed_external_consumption (tenant_id, park_id, farm_label, feed_item_label, feed_day, quantity_kg, batch_no, source_ref)
VALUES ($1::uuid, $2::uuid, 'CBE', 'UHT Milk', $3::date, $4::numeric, 326, 'feed-db-sheet:story')`,
		fxTenant, park, day, qty); err != nil {
		t.Fatalf("seed uht ledger row %s: %v", day, err)
	}
}
