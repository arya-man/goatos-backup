package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	protopg "github.com/vgoats/goatos/backend/internal/protocol/adapters/postgres"
	protoapp "github.com/vgoats/goatos/backend/internal/protocol/app"
	protodomain "github.com/vgoats/goatos/backend/internal/protocol/domain"
)

const clinicalDeferHTTPTenant = "00000000-0000-4000-8000-000000000001"

// matrixDSLWithDefer returns an otherwise-valid vaccination matrix rule_dsl whose
// eligibility.defer_states array is deferJSON.
func matrixDSLWithDefer(deferJSON string) string {
	return fmt.Sprintf(`{"vaccine":{"code":"ET+TT","name":"ET+TT","type":"killed","pathogen_class":"bacterial","course_type":"booster","inventory_item_id":"item-et","manufacturer":"tracked-matrix","disease":"Enterotoxaemia + Tetanus","compatibility_group":"ET+TT"},"eligibility":{"animal_stage":"K1","sex":"all","breed":"all","lifecycle":"alive","health":"any","reproductive":"any","exclude_reproductive_states":["pregnant","lactating"],"defer_states":%s},"missed_dose_policy":"pc_approval","compatibility_policy":{"live_to_killed_gap_days":14,"killed_to_killed_gap_days":14,"live_to_live_gap_days":28,"kid_booster_min_gap_days":21,"bacterial_viral_same_day_allowed":true,"live_killed_viral_same_day_allowed":true},"procurement_policy":{"warmup_no_vaccination_days":7,"kids_normal_schedule_until_weeks":16,"adult_prior_vaccination_allowed":true,"first_wave":["ET+TT","PPR"],"second_wave_after_days":28,"goat_second_wave":["Goat Pox","ET+TT booster"],"sheep_second_wave":["ET+TT booster","Sheep Pox"]},"pregnancy_policy":{"allow_until_pregnancy_month":3,"skip_from_pregnancy_month":4,"skip_through_pregnancy_month":5,"post_delivery_catch_up_days":14},"schedule":[{"dose_code":"et_tt_4w","sequence":1,"trigger_type":"birth_age","offset_days":28,"due_window_days":7,"dose_amount":2,"dose_unit":"ml","route_site":"subcutaneous","max_delay_days":7,"course_lapse_policy":"pc_review","repeat":"none","catch_up":"pc_approval"}]}`, deferJSON)
}

// C35-010 production-shaped proof: a partial clinical defer_states authored on a
// vaccination matrix must be rejected through the REAL public publish flow —
// HTTP route -> protocol Service -> Postgres -> ValidateExecutionContract — not
// just the validator unit. Full/absent lists get past the clinical gate.
func TestPublishRouteRejectsPartialClinicalDeferOverPostgres(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := httpmiddleware.WithTenantID(context.Background(), clinicalDeferHTTPTenant)
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	repo := protopg.NewRepository(pool, 5*time.Second)
	svc := protoapp.NewService(repo)
	handler := NewHandler(svc)
	sop := "b0000000-0000-4000-8000-000000000002"

	// createDraft creates a definition + draft vaccination matrix version with the
	// given defer_states and returns its version id.
	createDraft := func(t *testing.T, code, deferJSON string) string {
		t.Helper()
		protoID, err := repo.CreateDefinition(ctx, protodomain.NewDefinition{
			TenantID: clinicalDeferHTTPTenant, Code: code, Name: code, Category: "vaccination", Status: "draft",
		})
		if err != nil {
			t.Fatalf("definition: %v", err)
		}
		versionID, err := repo.CreateVersion(ctx, protodomain.NewVersion{
			TenantID: clinicalDeferHTTPTenant, ProtocolID: protoID, ScopeType: "tenant", Version: 1, Status: "draft",
			EffectiveFrom: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			RuleDsl:       []byte(matrixDSLWithDefer(deferJSON)),
			ProofPolicy:   []byte(`{"required":true,"types":["video"]}`),
			SopVersionID:  &sop,
		})
		if err != nil {
			t.Fatalf("version: %v", err)
		}
		return versionID
	}

	publish := func(versionID string) *httptest.ResponseRecorder {
		mux := http.NewServeMux()
		Register(mux, handler)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/protocols/versions/"+versionID+"/publish", strings.NewReader(""))
		req.Header.Set("Idempotency-Key", "clinical-defer-"+versionID)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	// Partial authored defer_states -> the public publish route rejects it.
	partialID := createDraft(t, "vaccination.partialdefer.http", `["icu","quarantine"]`)
	rec := publish(partialID)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("partial defer publish: want 422, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "not_publishable") ||
		!strings.Contains(body, "defer_states omits mandatory clinical safety states") {
		t.Fatalf("partial defer publish body did not name the clinical safety rejection: %s", body)
	}

	// Full mandatory set -> the clinical defer gate passes (any later gate may
	// still apply, but the clinical rejection must NOT be the reason).
	fullID := createDraft(t, "vaccination.fulldefer.http", `["sick","under_treatment","icu","quarantine"]`)
	rec = publish(fullID)
	if strings.Contains(rec.Body.String(), "defer_states omits mandatory clinical safety states") {
		t.Fatalf("full defer set was wrongly rejected by the clinical gate: code=%d body=%s", rec.Code, rec.Body.String())
	}
}
