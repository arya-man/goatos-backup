package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	healthhttp "github.com/vgoats/goatos/backend/internal/health/adapters/http"
	healthpg "github.com/vgoats/goatos/backend/internal/health/adapters/postgres"
	healthapp "github.com/vgoats/goatos/backend/internal/health/app"
	healthdomain "github.com/vgoats/goatos/backend/internal/health/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// TestKernelStoryHC_HealthConfigAuthoring proves the authored-treatment-protocol chain end to end
// through the SAME HTTP routes admin-web calls: a disease is created, its course is authored,
// published, edited, and republished; a goat is diagnosed between the two publishes; and the animal
// mid-treatment keeps the dosages it started on.
//
// Nothing here is seeded that the production path can produce. The only inserted facts are the
// shed and the goat (input facts a story is allowed to seed). Every protocol version, every step,
// every version retirement, the treatment case, and its per-day session steps are produced by the
// real handlers, services and repositories the backend serves.
func TestKernelStoryHC_HealthConfigAuthoring(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-hc", "Health Config authoring changes the next diagnosis, never an animal mid-treatment",
		"A Health Director creates a disease, authors its day-by-day treatment course, and publishes it "+
			"through the /health-config/* routes admin-web calls. A sick goat is diagnosed under that "+
			"version. The dosage is then changed and republished. The story proves the new course is what "+
			"the NEXT diagnosis loads, while the goat already being treated finishes on the dosages it "+
			"started on -- which is the whole reason treatment protocols are versioned rather than edited "+
			"in place. It also proves the Google Sheet importer now fails closed rather than silently "+
			"discarding an authored edit.")
	story.Certify("authenticated /health-config/* HTTP + health authoring service/repository + real OpenCase diagnosis path")
	defer story.Finish()

	const (
		shedID  = "dc000000-0000-4000-8000-000000000001"
		stageID = "dc000000-0000-4000-8000-000000000002"
		goatA   = "dc000000-0000-4000-8000-000000000003"
		goatB   = "dc000000-0000-4000-8000-000000000004"
		actorID = "dc000000-0000-4000-8000-000000000005"
	)

	repo := healthpg.NewRepository(fx.Pool, 10*time.Second)
	mux := http.NewServeMux()
	healthhttp.RegisterConfig(mux, healthhttp.NewConfigHandler(healthapp.NewConfigService(repo), nil))

	// Every call below goes through this: a real request, with the tenant/actor context the auth
	// middleware installs in production.
	call := func(method, path, body, idempotencyKey string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		ctx := httpmiddleware.WithTenantID(req.Context(), fxTenant)
		ctx = httpmiddleware.WithActorID(ctx, actorID)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req.WithContext(ctx))
		var decoded map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
		return rec.Code, decoded
	}
	str := func(m map[string]any, key string) string {
		if v, ok := m[key].(string); ok {
			return v
		}
		return ""
	}

	// -----------------------------------------------------------------------------------------
	story.Step("Seed only the input facts: a shed and two goats",
		"A story may seed the external facts a scenario starts from. Everything downstream -- the "+
			"protocol, its versions, the treatment case and its steps -- is produced by production code.")
	fx.SeedShed(shedID, "E2E-HC", stageID)
	dob := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	fx.SeedGoat(GoatSpec{GoatID: goatA, ShedID: shedID, DOB: &dob, AgeBand: healthdomain.AgeBandAdult})
	fx.SeedGoat(GoatSpec{GoatID: goatB, ShedID: shedID, DOB: &dob, AgeBand: healthdomain.AgeBandAdult})
	story.Assert("two adult goats exist in the shed",
		fx.scanText(`SELECT count(*)::text FROM goats WHERE tenant_id=$1 AND shed_id=$2::uuid`, fxTenant, shedID) == "2",
		"shed goat count")

	// -----------------------------------------------------------------------------------------
	story.Step("Create a disease through POST /health-config/diseases",
		"Creating a disease opens a DRAFT for both the adult and the kid band. Both, because the phone "+
			"picks the protocol from the goat's own age band -- a disease authored for adults only would "+
			"fail at diagnosis for a kid with 'protocol not published', which reads as a bug rather than "+
			"as a deliberate gap. Nothing is live yet.")
	status, created := call(http.MethodPost, "/health-config/diseases",
		`{"display_name":"Foot rot","duration_days":2}`, "hc-create")
	story.Assert("the create returns 201", status == http.StatusCreated, "status=%d body=%v", status, created)
	story.Assert("the disease key is derived from the name", str(created, "disease_key") == "foot_rot",
		"disease_key=%q", str(created, "disease_key"))
	drafts, _ := created["draft_version_ids"].(map[string]any)
	adultDraft, _ := drafts[healthdomain.AgeBandAdult].(string)
	kidDraft, _ := drafts[healthdomain.AgeBandKid].(string)
	story.Assert("a draft was opened for BOTH age bands", adultDraft != "" && kidDraft != "",
		"adult=%q kid=%q", adultDraft, kidDraft)
	story.Assert("nothing is live yet",
		fx.scanText(`SELECT count(*)::text FROM health_protocol_versions WHERE tenant_id=$1 AND disease_key='foot_rot' AND status='published'`, fxTenant) == "0",
		"published version count before any publish")

	// -----------------------------------------------------------------------------------------
	story.Step("Author the day-by-day course through POST /health-config/drafts/save",
		"The whole document is saved at once -- name, number of days, and the ordered steps. The client "+
			"sends no sequence numbers: order is positional and the server assigns 1..N, which is what "+
			"makes a reorder a single replacement rather than a renumber that collides with itself.")
	saveBody := func(dose string) string {
		return fmt.Sprintf(`{
          "disease_key":"foot_rot","age_band":"adult","display_name":"Foot rot","duration_days":2,
          "steps":[
            {"day_no":1,"session":"morning","record_type":"medication","medicine_name":"Meloxicam Paracetamol","dosage_text":%q,"dosage_denominator":"ml","medicine_route":"IM"},
            {"day_no":1,"session":"evening","record_type":"action","instruction":"Clean and dress the hoof."},
            {"day_no":2,"session":"morning","record_type":"medication","medicine_name":"Meloxicam Paracetamol","dosage_text":%q,"dosage_denominator":"ml","medicine_route":"IM"}
          ]}`, dose, dose)
	}
	status, saved := call(http.MethodPost, "/health-config/drafts/save", saveBody("5"), "hc-save-1")
	story.Assert("the save succeeds", status == http.StatusOK, "status=%d body=%v", status, saved)
	story.Assert("the outcome is 'saved'", str(saved, "outcome") == healthdomain.OutcomeSaved,
		"outcome=%q", str(saved, "outcome"))
	story.Assert("the server assigned sequence numbers 1,2,3 in working order",
		fx.scanText(`SELECT string_agg(seq::text, ',' ORDER BY seq) FROM health_protocol_steps WHERE health_protocol_version_id=$1::uuid`, adultDraft) == "1,2,3",
		"stored step sequence")

	// Re-saving the SAME content is not a new version. Without this every Save click churns out a
	// version differing from its predecessor in nothing but its id.
	status, unchanged := call(http.MethodPost, "/health-config/drafts/save", saveBody("5"), "hc-save-1-again")
	story.Assert("re-saving identical content reports 'unchanged'",
		status == http.StatusOK && str(unchanged, "outcome") == healthdomain.OutcomeUnchanged,
		"status=%d outcome=%q", status, str(unchanged, "outcome"))

	// -----------------------------------------------------------------------------------------
	story.Step("Publish the draft through POST /health-config/protocols/{id}/publish",
		"Publishing promotes the draft to the live protocol. It is the first publish of this disease, "+
			"so it retires nothing.")
	status, published := call(http.MethodPost, "/health-config/protocols/"+adultDraft+"/publish", "", "hc-publish-1")
	story.Assert("the publish succeeds", status == http.StatusOK, "status=%d body=%v", status, published)
	story.Assert("the outcome is 'published'", str(published, "outcome") == healthdomain.OutcomePublished,
		"outcome=%q", str(published, "outcome"))
	story.Assert("the first publish of a disease retires nothing", str(published, "retired_version_id") == "",
		"retired=%q", str(published, "retired_version_id"))
	story.Assert("exactly one adult version is live",
		fx.scanText(`SELECT count(*)::text FROM health_protocol_versions WHERE tenant_id=$1 AND disease_key='foot_rot' AND age_band='adult' AND status='published'`, fxTenant) == "1",
		"live adult version count")

	// -----------------------------------------------------------------------------------------
	story.Step("Diagnose a goat under the live protocol, through the real OpenCase path",
		"This is the production diagnosis path the phone calls. It snapshots the currently published "+
			"version onto the case and materialises the case's own copy of the steps -- which is what "+
			"makes the next assertion meaningful.")
	loc, _ := time.LoadLocation("Asia/Kolkata")
	opened, err := repo.OpenCase(fx.Ctx, healthdomain.OpenCaseInput{
		TenantID: fxTenant, ActorID: actorID, GoatID: goatA,
		DiseaseKey: "foot_rot", AgeBand: healthdomain.AgeBandAdult,
		StartDate:      time.Now().In(loc),
		IdempotencyKey: "hc-open-a", RequestFingerprint: "hc-open-a-fp",
	})
	story.Assert("the case opened with the configured 2-day course",
		err == nil && opened.DurationDays == 2, "err=%v duration=%d", err, opened.DurationDays)
	if err != nil {
		return
	}
	story.Assert("the case pinned the version that was live at diagnosis",
		fx.scanText(`SELECT health_protocol_version_id::text FROM health_cases WHERE tenant_id=$1 AND health_case_id=$2::uuid`, fxTenant, opened.CaseID) == adultDraft,
		"pinned version")
	story.Assert("the operator's first dose reads 5 ml",
		firstCaseDose(fx, opened.CaseID) == "5", "first case dose")

	// -----------------------------------------------------------------------------------------
	story.Step("Change the dosage and publish a second version",
		"Editing copies the live version into a draft, so the live protocol keeps serving diagnoses "+
			"untouched while the author works. Publishing the draft retires the version it replaces.")
	status, draft2 := call(http.MethodPost, "/health-config/drafts",
		`{"disease_key":"foot_rot","age_band":"adult"}`, "")
	story.Assert("opening a draft succeeds", status == http.StatusOK, "status=%d", status)
	draft2ID := str(draft2, "protocol_version_id")
	story.Assert("the new draft is a distinct version from the live one",
		draft2ID != "" && draft2ID != adultDraft, "draft=%q live=%q", draft2ID, adultDraft)
	steps2, _ := draft2["steps"].([]any)
	story.Assert("the draft copied the live course rather than starting blank", len(steps2) == 3,
		"copied step count=%d", len(steps2))
	story.Assert("the live version is untouched while the draft exists",
		fx.scanText(`SELECT status FROM health_protocol_versions WHERE tenant_id=$1 AND health_protocol_version_id=$2::uuid`, fxTenant, adultDraft) == "published",
		"live version status during edit")

	status, _ = call(http.MethodPost, "/health-config/drafts/save", saveBody("3"), "hc-save-2")
	story.Assert("the dosage change saves", status == http.StatusOK, "status=%d", status)
	status, published2 := call(http.MethodPost, "/health-config/protocols/"+draft2ID+"/publish", "", "hc-publish-2")
	story.Assert("the second publish succeeds", status == http.StatusOK, "status=%d body=%v", status, published2)
	story.Assert("the second publish RETIRED the first version",
		str(published2, "retired_version_id") == adultDraft,
		"retired=%q want=%q", str(published2, "retired_version_id"), adultDraft)
	story.Assert("still exactly one live adult version",
		fx.scanText(`SELECT count(*)::text FROM health_protocol_versions WHERE tenant_id=$1 AND disease_key='foot_rot' AND age_band='adult' AND status='published'`, fxTenant) == "1",
		"live adult version count after the swap")
	story.Assert("the retired version is kept, not deleted, so its dosages stay readable",
		fx.scanText(`SELECT count(*)::text FROM health_protocol_steps WHERE health_protocol_version_id=$1::uuid`, adultDraft) == "3",
		"retired version step count")

	// -----------------------------------------------------------------------------------------
	story.Step("THE SAFETY PROPERTY: the animal mid-treatment is unaffected",
		"The goat diagnosed before the change is still being treated. Its case pinned the old version "+
			"and holds its own copy of the steps, so the operator's instruction is still 5 ml -- not the "+
			"3 ml that was just published. This is the whole reason protocols are versioned rather than "+
			"edited in place.")
	story.Assert("the in-flight case still points at the version it was diagnosed under",
		fx.scanText(`SELECT health_protocol_version_id::text FROM health_cases WHERE tenant_id=$1 AND health_case_id=$2::uuid`, fxTenant, opened.CaseID) == adultDraft,
		"pinned version after the republish")
	story.Assert("the operator mid-treatment still administers 5 ml, not the new 3 ml",
		firstCaseDose(fx, opened.CaseID) == "5", "in-flight case dose after the republish")

	// -----------------------------------------------------------------------------------------
	story.Step("The NEXT diagnosis loads the new course",
		"A second goat diagnosed after the publish gets the new dosage. Same disease, same age band, "+
			"different version -- which is exactly what publishing is for.")
	opened2, err := repo.OpenCase(fx.Ctx, healthdomain.OpenCaseInput{
		TenantID: fxTenant, ActorID: actorID, GoatID: goatB,
		DiseaseKey: "foot_rot", AgeBand: healthdomain.AgeBandAdult,
		StartDate:      time.Now().In(loc),
		IdempotencyKey: "hc-open-b", RequestFingerprint: "hc-open-b-fp",
	})
	story.Assert("the second case opened", err == nil, "err=%v", err)
	if err == nil {
		story.Assert("it pinned the NEW version",
			fx.scanText(`SELECT health_protocol_version_id::text FROM health_cases WHERE tenant_id=$1 AND health_case_id=$2::uuid`, fxTenant, opened2.CaseID) == draft2ID,
			"second case pinned version")
		story.Assert("its operator administers the new 3 ml",
			firstCaseDose(fx, opened2.CaseID) == "3", "second case dose")
	}

	// -----------------------------------------------------------------------------------------
	story.Step("The catalog read shows the live version and any open draft side by side",
		"One row per disease per age band. The counts belong to their own version rather than to a "+
			"joined row, so a 3-step protocol reads as one row carrying 3 steps, never as 3 rows.")
	status, catalog := call(http.MethodGet, "/health-config/protocols?age_band=adult&search=Foot", "", "")
	story.Assert("the catalog read succeeds", status == http.StatusOK, "status=%d", status)
	items, _ := catalog["items"].([]any)
	story.Assert("Foot rot appears exactly once for the adult band", len(items) == 1,
		"adult rows for 'Foot' = %d", len(items))
	if len(items) == 1 {
		row, _ := items[0].(map[string]any)
		story.Assert("the row reports the live version's own step count (3), not a fanned-out join",
			numOf(row, "step_count") == 3, "step_count=%v", row["step_count"])
		story.Assert("the row reports 2 medicine steps",
			numOf(row, "medication_count") == 2, "medication_count=%v", row["medication_count"])
		story.Assert("the row reports no open draft (both were published)",
			row["has_draft"] == false, "has_draft=%v", row["has_draft"])
		story.Assert("the catalog returns a keyset cursor and no total count",
			catalog["next_cursor"] == nil && catalog["total"] == nil,
			"next_cursor=%v total=%v", catalog["next_cursor"], catalog["total"])
	}

	// -----------------------------------------------------------------------------------------
	story.Step("Publishing is gated on a complete course",
		"A draft may be saved incomplete on purpose -- it is work in progress. Publishing applies the "+
			"strict rulebook against what is actually STORED, and reports every offending field at once "+
			"so a long protocol can be fixed in one pass.")
	status, _ = call(http.MethodPost, "/health-config/diseases", `{"display_name":"Acidosis","duration_days":2}`, "hc-create-2")
	story.Assert("a second disease was created", status == http.StatusCreated, "status=%d", status)
	acidosisDraft := fx.scanText(`SELECT health_protocol_version_id::text FROM health_protocol_versions
WHERE tenant_id=$1 AND disease_key='acidosis' AND age_band='adult' AND status='draft'`, fxTenant)
	status, rejected := call(http.MethodPost, "/health-config/protocols/"+acidosisDraft+"/publish", "", "hc-publish-empty")
	story.Assert("publishing a stepless draft is rejected with 422", status == http.StatusUnprocessableEntity,
		"status=%d body=%v", status, rejected)
	fieldErrors, _ := rejected["errors"].([]any)
	story.Assert("the rejection names the offending field", len(fieldErrors) > 0, "errors=%v", rejected["errors"])
	story.Assert("nothing was published",
		fx.scanText(`SELECT count(*)::text FROM health_protocol_versions WHERE tenant_id=$1 AND disease_key='acidosis' AND status='published'`, fxTenant) == "0",
		"acidosis published count")

	// -----------------------------------------------------------------------------------------
	story.Step("The Google Sheet importer now fails closed",
		"The sheet was the bootstrap. It retires every published protocol and republishes its own set, "+
			"so running it after an authored edit would silently discard that edit -- and, because it "+
			"publishes a new version rather than mutating one, it would do so with no constraint "+
			"violation to notice. It therefore refuses once the app has taken authorship.")
	sheet := []healthdomain.SourceProtocol{{
		DiseaseKey: "foot_rot", DisplayName: "Foot rot", AgeBand: healthdomain.AgeBandAdult,
		DurationDays: 1,
		Steps: []healthdomain.ProtocolStep{{
			DayNo: 1, Session: healthdomain.SessionMorning, Seq: 1,
			RecordType: healthdomain.RecordTypeAction, Instruction: ptr("Sheet instruction."),
		}},
	}}
	importErr := repo.ReplacePublishedProtocols(fx.Ctx, fxTenant, actorID, "google-sheet:bootstrap", "sheet-hash", sheet)
	story.Assert("the sheet import is refused once the app has authored", importErr != nil,
		"import err=%v", importErr)
	story.Assert("the live dosage is still the authored 3 ml, not the sheet's content",
		fx.scanText(`SELECT s.dosage_text FROM health_protocol_steps s
JOIN health_protocol_versions v ON v.health_protocol_version_id=s.health_protocol_version_id
WHERE v.tenant_id=$1 AND v.disease_key='foot_rot' AND v.age_band='adult' AND v.status='published' AND s.seq=1`, fxTenant) == "3",
		"live dosage after the refused import")

	// -----------------------------------------------------------------------------------------
	story.Step("Every authored write is recorded in the audit ledger",
		"A publish spans two version rows (one retired, one published) and a discard leaves none, so the "+
			"write's identity belongs to a ledger rather than to any row. The ledger is written in the "+
			"same transaction as the side effects: an entry that exists is proof the edit committed.")
	story.Assert("the ledger recorded both publishes with the version each retired",
		fx.scanText(`SELECT count(*)::text FROM health_config_write_log
WHERE tenant_id=$1 AND write_kind='draft_publish' AND outcome='published'`, fxTenant) == "2",
		"publish ledger entries")
	story.Assert("the second publish's ledger entry names the version it retired",
		fx.scanText(`SELECT count(*)::text FROM health_config_write_log
WHERE tenant_id=$1 AND retired_version_id=$2::uuid`, fxTenant, adultDraft) == "1",
		"retired-version ledger entry")
	story.Assert("every ledger entry carries its idempotency key and actor",
		fx.scanText(`SELECT count(*)::text FROM health_config_write_log
WHERE tenant_id=$1 AND (btrim(idempotency_key)='' OR btrim(actor_ref)='')`, fxTenant) == "0",
		"ledger entries missing a key or actor")
}

// firstCaseDose reads the dosage the operator will actually administer on this case's first
// medication step -- the case's OWN copy of the protocol, not the protocol table.
func firstCaseDose(fx *Fixture, caseID string) string {
	return fx.scanText(`
SELECT coalesce(ss.dosage_text,'') FROM health_session_steps ss
JOIN health_treatment_sessions hs ON hs.health_session_id=ss.health_session_id
WHERE hs.health_case_id=$1::uuid AND ss.record_type='medication'
ORDER BY hs.day_no, ss.seq LIMIT 1`, caseID)
}

func numOf(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return -1
}

func ptr(s string) *string { return &s }
