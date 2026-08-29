package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	countshttp "github.com/vgoats/goatos/backend/internal/counts/adapters/http"
	countspg "github.com/vgoats/goatos/backend/internal/counts/adapters/postgres"
	countsapp "github.com/vgoats/goatos/backend/internal/counts/app"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
)

// TestKernelStory_HealthRaiseAdoptsTheHomogeneousPensOwnTag is the production-path proof for the
// pen GRAIN of the typed-raise tag resolution (maintainer decision 2026-08-20: THE SHIFT TYPE
// DECIDES THE TAG; a health movement stamps the DESTINATION's tag).
//
// The live incident it closes (STG, CBE, 2026-08-29): a health raise into "Godel 1 - Part 1" -- a
// pen holding ONLY F2-Female animals -- was refused destination_tag_mixed ("This destination holds
// a mix of tags"). The pen was homogeneous; the SHED was not. The destination catalog aggregated
// resident stages PER SHED, so every pen row of Godel 1 carried the whole building's stage set
// {Buck, F2-Female, ...} and a legitimate health movement had no way forward.
//
// Why the existing tests missed it: the typed-raise handler tests drive the real rulebook against a
// FAKE repository, and a fake returns whatever catalog a test stocked it with -- the SQL grain was
// invisible. This story drives the REAL chain end to end:
//
//	READ   GET /app/counts/shifting/destinations  (real handler -> real catalog SQL)
//	RAISE  POST /app/counts/shifting-events        (real handler -> catalog + goat facts ->
//	       domain.ResolveShiftTypeDecision -> stored shifting event + approval request)
//
// Three assertions, and the adversarial one is what pins the grain rather than the happy path:
//
//	homogeneous pen inside a mixed shed  -> raise ACCEPTED, event stamps the pen's own tag
//	genuinely mixed pen (same shed)      -> raise refused destination_tag_mixed (the refusal is
//	                                        for pens that ARE mixed, not pens whose shed is)
//	destinations read                    -> per-pen destination_stage / farm-worded reason agree
//	                                        with what the raise then does (same resolver, one grain)
func TestKernelStory_HealthRaiseAdoptsTheHomogeneousPensOwnTag(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-shifting-health-raise-pen-grain",
		"A health raise into a homogeneous pen adopts that pen's own tag, not its shed's mix",
		"A health shifting stamps the destination's tag, so the destination catalog must offer each "+
			"PEN its own residents' cohort. A shed-grain aggregate made a homogeneous pen inside a mixed "+
			"shed read as 'a mix of tags' and refused legitimate health movements (live CBE incident, "+
			"Godel 1 - Part 1, 2026-08-29). The raise and the destinations screen resolve through the "+
			"same catalog, so this story asserts both surfaces on the same fixture.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	const (
		sourceShed  = "5f000000-0000-4000-8000-0000000d6101"
		godelShed   = "5f000000-0000-4000-8000-0000000d6102" // partitioned: Part 1 homogeneous, Part 2 mixed
		stageShared = "5f000000-0000-4000-8000-0000000d61a1"
		sickGoat    = "5f000000-0000-4000-8000-0000000d6201" // raised into Part 1 (must succeed)
		sickGoat2   = "5f000000-0000-4000-8000-0000000d6202" // raised into Part 2 (must refuse)
		residentF1  = "5f000000-0000-4000-8000-0000000d6301" // Part 1: F2-Female
		residentF2  = "5f000000-0000-4000-8000-0000000d6302" // Part 1: F2-Female
		residentF3  = "5f000000-0000-4000-8000-0000000d6303" // Part 2: F2-Female
		residentB1  = "5f000000-0000-4000-8000-0000000d6304" // Part 2: Buck -- the mix
	)

	story.Step("A partitioned shed whose pens hold different cohorts, and no authored pen tags",
		"External facts only: the shed, its two catalog pens (shed_partitions, animal_stage_id NULL -- "+
			"the resident fallback is the only tag source, the exact incident condition), the residents "+
			"with their per-pen placement, and the active stage vocabulary the relocation validates "+
			"against. Part 1 holds only F2-Female; Part 2 holds F2-Female AND Buck.")
	fx.SeedShed(sourceShed, "E2E-HG-SRC", stageShared)
	fx.SeedShed(godelShed, "E2E-HG-GODEL", stageShared)
	fx.exec("stage vocabulary",
		`INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, age_band, sort_order, status)
		 VALUES ($1, 'F2-Female', 'F2 Female', 'adult', 10, 'active'),
		        ($1, 'Buck', 'Buck', 'adult', 11, 'active')
		 ON CONFLICT (tenant_id, stage_code) DO NOTHING`, fxTenant)
	fx.exec("pen catalog",
		`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
		 VALUES ($1, $2, 'Part 1', '1', 'active', 'manual'),
		        ($1, $2, 'Part 2', '2', 'active', 'manual')`, fxTenant, godelShed)
	dob := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, g := range []struct {
		id, stage, pen string
	}{
		{residentF1, "F2-Female", "Part 1"},
		{residentF2, "F2-Female", "Part 1"},
		{residentF3, "F2-Female", "Part 2"},
		{residentB1, "Buck", "Part 2"},
	} {
		fx.SeedGoat(GoatSpec{GoatID: g.id, ShedID: godelShed, Stage: g.stage, Breed: "Sirohi", DOB: &dob, AgeBand: "adult"})
		fx.exec("resident pen placement",
			`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
			 VALUES ($1, $2, $3, $4, 'E2E-HG-GODEL')`, fxTenant, g.id, godelShed, g.pen)
	}
	fx.SeedGoat(GoatSpec{GoatID: sickGoat, ShedID: sourceShed, Stage: "F2-Female", Breed: "Sirohi", DOB: &dob, AgeBand: "adult"})
	fx.SeedGoat(GoatSpec{GoatID: sickGoat2, ShedID: sourceShed, Stage: "F2-Female", Breed: "Sirohi", DOB: &dob, AgeBand: "adult"})

	// The REAL production assembly, exactly as internal/bootstrap/api.go wires it: postgres
	// repository -> counts service -> HTTP handler, with the real approval workflow so the raise
	// records the pending request a park head decides. Only the lifecycle validator is omitted --
	// it serves the birth/death routes, which this story never touches.
	repo := countspg.NewRepository(fx.Pool, 10*time.Second)
	service := countsapp.NewService(repo)
	approvals := countsapp.NewApprovalService(repo, nil, nil)
	mux := http.NewServeMux()
	countshttp.RegisterAppWrites(mux, countshttp.NewAppWriteHandler(service, nil).WithApprovalWorkflow(approvals, nil))

	serve := func(method, path string, idempotencyKey string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(payload)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		reqCtx := httpmiddleware.WithActorID(httpmiddleware.WithTenantID(ctx, fxTenant), "5f000000-0000-4000-8000-0000000d6901")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req.WithContext(reqCtx))
		return rec
	}

	story.Step("The destinations screen offers each pen ITS OWN tag",
		"GET /app/counts/shifting/destinations is the read the raise form renders. Part 1 must offer "+
			"F2-Female with no reason; Part 2 must offer nothing, with the farm-worded mixed reason. Both "+
			"come from the same catalog the raise below resolves through.")
	destRec := serve(http.MethodGet, "/app/counts/shifting/destinations", "", nil)
	if destRec.Code != http.StatusOK {
		t.Fatalf("destinations status=%d body=%s", destRec.Code, destRec.Body.String())
	}
	var destResp struct {
		Parks []struct {
			Sheds []struct {
				ShedID                 string  `json:"shed_id"`
				PartitionLabel         *string `json:"partition_label"`
				DestinationStage       string  `json:"destination_stage"`
				DestinationStageReason string  `json:"destination_stage_reason"`
			} `json:"sheds"`
		} `json:"parks"`
	}
	if err := json.Unmarshal(destRec.Body.Bytes(), &destResp); err != nil {
		t.Fatalf("decode destinations: %v", err)
	}
	stageByPen := map[string]string{}
	reasonByPen := map[string]string{}
	for _, park := range destResp.Parks {
		for _, shed := range park.Sheds {
			if shed.ShedID == godelShed && shed.PartitionLabel != nil {
				stageByPen[*shed.PartitionLabel] = shed.DestinationStage
				reasonByPen[*shed.PartitionLabel] = shed.DestinationStageReason
			}
		}
	}
	story.Assert("the homogeneous pen offers its residents' one tag",
		stageByPen["Part 1"] == "F2-Female" && reasonByPen["Part 1"] == "",
		"Part 1 stage=%q reason=%q", stageByPen["Part 1"], reasonByPen["Part 1"])
	story.Assert("the genuinely mixed pen offers nothing, with the farm-worded reason",
		stageByPen["Part 2"] == "" && reasonByPen["Part 2"] == "This destination holds a mix of tags",
		"Part 2 stage=%q reason=%q", stageByPen["Part 2"], reasonByPen["Part 2"])

	story.Step("A HEALTH raise into the homogeneous pen is ACCEPTED and stamps that pen's tag",
		"POST /app/counts/shifting-events with category=health. This is the exact request the live "+
			"incident refused. The stored movement must carry the pen's own tag as the target the park "+
			"head approves and the completion transaction will stamp.")
	raiseRec := serve(http.MethodPost, "/app/counts/shifting-events", "e2e-hg-raise-1", map[string]any{
		"destination_park_id":         fxPark,
		"destination_shed_id":         godelShed,
		"destination_partition_label": "Part 1",
		"category":                    "health",
		"goat_ids":                    []string{sickGoat},
	})
	story.Assert("the raise succeeds", raiseRec.Code == http.StatusOK,
		"status=%d body=%s", raiseRec.Code, raiseRec.Body.String())
	var raiseResp struct {
		ShiftingEventID string `json:"shifting_event_id"`
	}
	if err := json.Unmarshal(raiseRec.Body.Bytes(), &raiseResp); err != nil {
		t.Fatalf("decode raise: %v", err)
	}
	storedTarget := fx.scanText(
		`SELECT COALESCE(target_management_stage,'') FROM shifting_events WHERE tenant_id=$1 AND shifting_event_id=$2`,
		fxTenant, raiseResp.ShiftingEventID)
	storedMode := fx.scanText(
		`SELECT COALESCE(management_stage_mode,'') FROM shifting_events WHERE tenant_id=$1 AND shifting_event_id=$2`,
		fxTenant, raiseResp.ShiftingEventID)
	story.Assert("the movement carries the pen's own tag for the park head to approve",
		storedTarget == "F2-Female" && storedMode == "destination_stage",
		"target_management_stage=%q management_stage_mode=%q", storedTarget, storedMode)

	story.Step("A HEALTH raise into the genuinely mixed pen is still refused",
		"The mixed refusal exists for pens that ARE mixed. Fixing the grain must not have widened "+
			"acceptance: Part 2 really holds two cohorts, so there is no single tag a health movement "+
			"could stamp, and the raise is rejected at raise time with the same farm copy the "+
			"destinations screen showed.")
	mixedRec := serve(http.MethodPost, "/app/counts/shifting-events", "e2e-hg-raise-2", map[string]any{
		"destination_park_id":         fxPark,
		"destination_shed_id":         godelShed,
		"destination_partition_label": "Part 2",
		"category":                    "health",
		"goat_ids":                    []string{sickGoat2},
	})
	story.Assert("the mixed-pen raise is rejected with destination_tag_mixed",
		mixedRec.Code == http.StatusBadRequest && bytes.Contains(mixedRec.Body.Bytes(), []byte("destination_tag_mixed")),
		"status=%d body=%s", mixedRec.Code, mixedRec.Body.String())
	movementCount := fx.countRows(
		`SELECT count(*) FROM shifting_events WHERE tenant_id=$1 AND destination_shed_id=$2`,
		fxTenant, godelShed)
	story.Assert("the refused raise wrote no movement row: the shed holds exactly the one accepted movement",
		movementCount == 1, "rows=%d", movementCount)
}
