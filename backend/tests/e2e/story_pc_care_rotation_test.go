package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/kernelstages"
	pccarepg "github.com/vgoats/goatos/backend/internal/pccare/adapters/postgres"
	pccareproof "github.com/vgoats/goatos/backend/internal/pccare/adapters/proof"
	pccarebridge "github.com/vgoats/goatos/backend/internal/pccare/adapters/verificationbridge"
	pccareapp "github.com/vgoats/goatos/backend/internal/pccare/app"
	pccaredomain "github.com/vgoats/goatos/backend/internal/pccare/domain"
	pccareports "github.com/vgoats/goatos/backend/internal/pccare/ports"
	pccaresopapp "github.com/vgoats/goatos/backend/internal/pccaresop/app"
	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	platformpg "github.com/vgoats/goatos/backend/internal/platform/postgres"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	soppg "github.com/vgoats/goatos/backend/internal/sop/adapters/postgres"
	sopapp "github.com/vgoats/goatos/backend/internal/sop/app"
	sopdomain "github.com/vgoats/goatos/backend/internal/sop/domain"
	sopports "github.com/vgoats/goatos/backend/internal/sop/ports"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
)

// TestKernelStory_PCCareRotationRoundRobin is the production-path proof of the maintainer's
// 2026-10-02 instruction (docs/decisions/pc-care-rotation.md): fumigation goes ROUND ROBIN -- one
// pen after another, and when all are done, round again. Choices made that day: a pen counts as
// done when the operator SUBMITS it; only pens with animals in them; the park's pen order; one pen
// per day; the per-pen "every N days" repeat stays beside it.
//
// Every state change runs through the code production runs:
//
//	PUBLISH        sop app.Service CreateVersion/PublishVersion with the PC Care contract the API
//	               registers (pccaresop.PCCareSOPContract) -- a bad document is refused HERE
//	PLAN / CLOSE   pccare app.Service CreateRound / CloseRound (the planner's routes)
//	RECORD/SUBMIT  pccare app.Service RegisterTaskProof / SubmitTask, real proof uploads
//	VERIFIER ITEM  pc_care.task.pending_verification -> durable consumer -> verification.CreateItem
//	VERDICT        verification app.Service.RecordVerdict -> outbox -> the PC Care applier
//	HERD MOVE      identity MoveGoat (the production move command)
//	NEXT PEN       kernelstages.PcCareRepeatStage -- the REAL stage the kernel worker registers,
//	               reading the PUBLISHED SOP from the database; only its clock is pinned
//	ALERT          the stage's own notifier -> notification_requests
//	OPERATOR LIST  pccare app.Service Worklist
//
// Submit stamps submitted_at with the DATABASE clock (today); only the stage's clock moves forward,
// so "today + k" below is the day the kernel worker ticks on.
func TestKernelStory_PCCareRotationRoundRobin(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-pc-care-rotation",
		"Fumigation rotates through the pens, one a day, and round again",
		"The Preventive Care SOP card for fumigation is set to 'Rotate through the pens'. A planner "+
			"plans the first pen; from then on, each time the operator submits a pen, the next pen with "+
			"animals in it -- in the park's pen order -- is planned for the next day, with the same "+
			"operators. After the last pen it starts again from the first, after the card's wait. "+
			"Before anyone sets this, deploying changes nothing.")
	defer story.Finish()
	story.Certify("backend kernel + SOP publish + verification + real kernel stage")

	ctx := fx.Ctx
	const (
		stageID   = "7e000000-0000-4000-8000-0000000f01a1"
		castroID  = "7e000000-0000-4000-8000-0000000f0101" // undivided
		godelID   = "7e000000-0000-4000-8000-0000000f0102" // Part 1, Part 2, Part 3 (EMPTY), Part 10
		otherPark = "7e000000-0000-4000-8000-0000000f0201"
		yashodaID = "7e000000-0000-4000-8000-0000000f0202" // the other park's only pen
		ceoID     = "7e000000-0000-4000-8000-0000000f0301"
		op1ID     = "7e000000-0000-4000-8000-0000000f0302"
		op2ID     = "7e000000-0000-4000-8000-0000000f0303"
		op3ID     = "7e000000-0000-4000-8000-0000000f0304" // other park
		verifier  = "7e000000-0000-4000-8000-0000000f0305"
		goatPart  = "7e000000-0000-4000-8000-0000000f04"
	)

	// ---------------------------------------------------------------------------
	story.Step("The farm: two parks, pens with and without animals, two operators, a CEO",
		"CBE holds Castro (undivided) and Godel 1 with Part 1, Part 2, Part 3 and Part 10. Part 3 has "+
			"NO animals. A second park holds one pen, Yashoda, with its own operator. The CEO plans; "+
			"the CEO's phone is registered so the 'rotation stopped' alert has somewhere to land.")
	fx.SeedShed(castroID, "Castro", stageID)
	fx.SeedShed(godelID, "Godel 1", stageID)
	fx.exec("godel pens", `INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
		VALUES ($1, $2, 'Part 1', '1', 'active', 'manual'), ($1, $2, 'Part 2', '2', 'active', 'manual'),
		       ($1, $2, 'Part 3', '3', 'active', 'manual'), ($1, $2, 'Part 10', '10', 'active', 'manual')`, fxTenant, godelID)
	fx.exec("other park", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($1, $2, 'park', 'E2E-P2', 'Second Park', 'active')`, otherPark, fxTenant)
	fx.exec("yashoda", `INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status) VALUES ($1, $2, 'shed', 'Yashoda', 'Yashoda', $3, 'active')`, yashodaID, fxTenant, otherPark)
	goats := map[string]string{} // pen -> goat
	addGoat := func(n int, shed, partition, park string) string {
		id := fmt.Sprintf("%s%02d", goatPart, n)
		fx.exec("goat", `INSERT INTO goats (goat_id, tenant_id, lifecycle_status, health_status, species, custodian_party_id, sex, current_location_id, park_id, shed_id, management_stage)
			VALUES ($1, $2, 'alive', 'healthy', 'goat', $3, 'female', $4, $5, $4, 'K1')`, id, fxTenant, fxParty, shed, park)
		if partition != "" {
			fx.exec("goat pen", `INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name) VALUES ($1, $2, $3, $4, 'e2e')`, fxTenant, id, shed, partition)
		}
		return id
	}
	addGoat(1, castroID, "", fxPark)
	addGoat(2, castroID, "", fxPark)
	goats["Part 1"] = addGoat(3, godelID, "Part 1", fxPark)
	goats["Part 2"] = addGoat(4, godelID, "Part 2", fxPark)
	goats["Part 10"] = addGoat(5, godelID, "Part 10", fxPark)
	addGoat(6, yashodaID, "", otherPark)

	for _, p := range []struct{ id, code, name, role, park string }{
		{ceoID, "CEO-ROT", "Rotation CEO", "ceo_internal", fxTenant},
		{op1ID, "OP1-ROT", "Amit", "operator", fxPark},
		{op2ID, "OP2-ROT", "Darshan", "operator", fxPark},
		{op3ID, "OP3-ROT", "Sagar", "operator", otherPark},
		{verifier, "VER-ROT", "Verifier", "verifier", fxTenant},
	} {
		fx.exec("member "+p.name, `INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
			VALUES ($1, $2, $1, $3, $4, 'active', $5, $6)`, p.id, fxTenant, p.code, p.name, roleHint(p.role), fxPark)
		scope := "park"
		if p.park == fxTenant {
			scope = "tenant"
		}
		fx.exec("grant "+p.name, `INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
			VALUES ($1, $2, $3, $4, $5, 'active', now() - interval '1 day')`, fxTenant, p.id, p.role, scope, p.park)
	}
	fx.exec("ceo device", `INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
		VALUES (gen_random_uuid(), $1, $2::uuid, 'android', 'rot-install-ceo', 'rot-token-ceo', '1.0.0', '14', 'active', now(), $2::uuid)`, fxTenant, ceoID)

	// Services, composed the way bootstrap/api.go and the kernel worker compose them.
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	for _, def := range verificationcatalog.PCCare() {
		if err := verification.RegisterCategory(def); err != nil {
			t.Fatalf("register pc care category: %v", err)
		}
	}
	pccareapp.NewPCCarePendingVerificationHandler(pccarebridge.New(verification), nil).Register(fx.Bus)
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-rot-proof-secret"))
	pcRepo := pccarepg.NewRepository(fx.Pool, 10*time.Second)
	pcCare := pccareapp.NewService(pcRepo).
		WithRoundStore(pcRepo).
		WithProofValidator(pccareproof.NewValidator(fx.Proof)).
		WithVerificationEnqueuer(pccarebridge.New(verification))
	sopRepo := soppg.NewRepository(fx.Pool, 5*time.Second)
	sops := sopapp.NewService(sopRepo).WithFormDSLContract(pccaresopapp.PCCareSOPContract)
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	deps := kernelstages.Deps{Pool: fx.Pool, PgCfg: platformpg.Config{QueryTimeout: 10 * time.Second}, Logger: logger}

	ceo := pccaredomain.Actor{TenantID: fxTenant, UserID: ceoID, Roles: []string{permissions.RoleCEOInternal}}
	opActor := func(id string) pccaredomain.Actor {
		return pccaredomain.Actor{TenantID: fxTenant, UserID: id, Roles: []string{permissions.RoleOperator}}
	}
	today := biztime.BusinessDayStart(time.Now())
	day := func(k int) string { return today.AddDate(0, 0, k).Format("2006-01-02") }
	relay := func() {
		for i := 0; i < 2; i++ {
			time.Sleep(1500 * time.Millisecond)
			fx.RelayOutboxEvents()
		}
	}

	// --- helpers --------------------------------------------------------------------------
	plan := func(key, park string, pens []pccareapp.RoundPenInput, date string, ops ...string) pccareports.RoundRow {
		t.Helper()
		round, err := pcCare.CreateRound(ctx, ceo, pccareapp.CreateRoundInput{
			Category: pccaredomain.CategoryFumigation, ParkID: park, Pens: pens, PlannedBusinessDate: date,
			AssigneeUserIDs: ops, IdempotencyKey: "rot-plan-" + key, ActorID: ceoID, ActorType: "human", TraceID: "rot-plan-" + key,
		})
		if err != nil {
			t.Fatalf("plan %s: %v", key, err)
		}
		return round
	}
	video := func(taskID, by, key string) string {
		t.Helper()
		target, err := proofService.CreateUpload(ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: "video", MimeType: "video/mp4", ScopeType: "task", ScopeID: taskID,
			SubjectType: "other", UploadedBy: storyAAPtrString(by),
			Metadata: map[string]any{"capture_source": "in_app_camera", "captured_start_ms": int64(1000), "captured_end_ms": int64(6000), "story": "ROT:" + key},
		})
		if err != nil {
			t.Fatalf("create upload %s: %v", key, err)
		}
		stored, err := proofService.StoreUpload(ctx, fxTenant, target.Proof.ProofID, "video/mp4", bytes.NewBufferString("story-rot-"+key))
		if err != nil {
			t.Fatalf("store upload %s: %v", key, err)
		}
		duration := int64(5000)
		if _, err := proofService.CompleteUpload(ctx, proofdomain.CompleteUpload{
			TenantID: fxTenant, ProofID: target.Proof.ProofID, ContentHash: stored.ContentHash,
			MimeType: stored.MimeType, SizeBytes: stored.SizeBytes, DurationMS: &duration,
		}); err != nil {
			t.Fatalf("complete upload %s: %v", key, err)
		}
		return target.Proof.ProofID
	}
	submit := func(taskID, by, key string) {
		t.Helper()
		for _, slot := range []string{"mixing_video", "spraying_video"} {
			if err := pcCare.RegisterTaskProof(ctx, opActor(by), pccareapp.RegisterTaskProofInput{TaskID: taskID, SlotKey: slot,
				ProofRef: video(taskID, by, key+"-"+slot), IdempotencyKey: "rot-proof-" + key + "-" + slot, ActorID: by, ActorType: "operator"}); err != nil {
				t.Fatalf("record %s/%s: %v", key, slot, err)
			}
		}
		if _, err := pcCare.SubmitTask(ctx, opActor(by), pccareapp.SubmitTaskInput{TaskID: taskID, IdempotencyKey: "rot-submit-" + key,
			ActorID: by, ActorType: "operator", TraceID: "rot-submit-" + key}); err != nil {
			t.Fatalf("submit %s: %v", key, err)
		}
		relay()
	}
	tick := func(k int) {
		t.Helper()
		at := today.AddDate(0, 0, k).Add(10 * time.Hour)
		stage := kernelstages.NewPcCareRepeatStage(deps, fxTenant, logger).WithClock(func() time.Time { return at })
		if err := stage.Run(ctx); err != nil {
			t.Fatalf("stage on day+%d: %v", k, err)
		}
	}
	type pen struct {
		TaskID, Label, Date, RepeatOf, State string
		Operators                            []string
		Version                              int
	}
	readPens := func(where string, args ...any) []pen {
		t.Helper()
		rows, err := fx.Pool.Query(ctx, `
SELECT t.task_id::text, s.name || coalesce(' - ' || nullif(t.partition_label, ''), ''), t.planned_business_date::text,
       coalesce(t.repeat_of_task_id::text, ''), t.work_state, coalesce(t.sop_version, 0),
       coalesce((SELECT array_agg(a.operator_user_id::text ORDER BY a.operator_user_id::text) FROM pc_care_task_assignees a WHERE a.task_id = t.task_id), ARRAY[]::text[])
FROM pc_care_tasks t JOIN locations s ON s.location_id = t.shed_id
WHERE t.tenant_id = $1 AND t.category = 'fumigation' AND `+where+`
ORDER BY t.planned_business_date, t.created_at`, append([]any{fxTenant}, args...)...)
		if err != nil {
			t.Fatalf("read pens: %v", err)
		}
		defer rows.Close()
		out := []pen{}
		for rows.Next() {
			var p pen
			if err := rows.Scan(&p.TaskID, &p.Label, &p.Date, &p.RepeatOf, &p.State, &p.Version, &p.Operators); err != nil {
				t.Fatalf("scan pen: %v", err)
			}
			out = append(out, p)
		}
		return out
	}
	allFumigation := func() int { return len(readPens("true")) }
	nextOf := func(src string) []pen { return readPens("t.repeat_of_task_id = $2::uuid", src) }
	sopID := fx.scanText(`SELECT sop_id::text FROM sop_definitions WHERE tenant_id = $1 AND code = 'pc_care.tasks'`, fxTenant)
	publishedVersion := func() int {
		return fx.countRows(`SELECT coalesce(max(v.version), 0) FROM sop_versions v WHERE v.tenant_id = $1 AND v.sop_id = $2::uuid AND v.status = 'published'`, fxTenant, sopID)
	}
	fumigationCard := func() map[string]any {
		raw := fx.scanText(`SELECT (v.form_dsl->'pc_care'->'categories'->'fumigation')::text FROM sop_versions v WHERE v.tenant_id = $1 AND v.sop_id = $2::uuid AND v.status = 'published' ORDER BY v.version DESC LIMIT 1`, fxTenant, sopID)
		card := map[string]any{}
		_ = json.Unmarshal([]byte(raw), &card)
		return card
	}
	publish := func(label string, edit func(categories map[string]any)) error {
		t.Helper()
		raw := fx.scanText(`SELECT v.form_dsl::text FROM sop_versions v WHERE v.tenant_id = $1 AND v.sop_id = $2::uuid AND v.status = 'published' ORDER BY v.version DESC LIMIT 1`, fxTenant, sopID)
		var formDSL map[string]any
		if err := json.Unmarshal([]byte(raw), &formDSL); err != nil {
			t.Fatalf("published form_dsl: %v", err)
		}
		edit(formDSL["pc_care"].(map[string]any)["categories"].(map[string]any))
		// The editor saves the next version on top of the one in force: proof policy and
		// compatibility travel with it unchanged.
		var proofPolicy, compatibility map[string]any
		_ = json.Unmarshal([]byte(fx.scanText(`SELECT coalesce(v.proof_policy, '{}'::jsonb)::text FROM sop_versions v WHERE v.tenant_id = $1 AND v.sop_id = $2::uuid AND v.status = 'published' ORDER BY v.version DESC LIMIT 1`, fxTenant, sopID)), &proofPolicy)
		_ = json.Unmarshal([]byte(fx.scanText(`SELECT coalesce(v.compatibility, '{}'::jsonb)::text FROM sop_versions v WHERE v.tenant_id = $1 AND v.sop_id = $2::uuid AND v.status = 'published' ORDER BY v.version DESC LIMIT 1`, fxTenant, sopID)), &compatibility)
		created, err := sops.CreateVersion(ctx, sopports.CreateVersionCommand{TenantID: fxTenant, ActorID: ceoID, SOPID: sopID,
			Body: sopdomain.CreateSOPVersionRequest{VersionLabel: label, FormDSL: formDSL, ProofPolicy: proofPolicy, Compatibility: compatibility}}, "rot-"+label)
		if err != nil {
			return err
		}
		_, err = sops.PublishVersion(ctx, sopports.VersionCommand{TenantID: fxTenant, ActorID: ceoID, SOPID: sopID,
			SOPVersionID: created.Version.SOPVersionID, RowVersion: created.Version.RowVersion}, "rot-pub-"+label)
		return err
	}
	card := func(categories map[string]any, name string) map[string]any { return categories[name].(map[string]any) }

	// ---------------------------------------------------------------------------
	story.Step("DEPLOY AS-IS: with the SOP as it ships, nothing new is ever planned",
		"The tenant's published PC Care SOP is the one the migrations seeded -- exactly what production "+
			"runs on deploy day. A fumigation is planned, filmed and submitted, and the real kernel stage "+
			"ticks today, tomorrow and three days on. No task is created by the new code.")
	seededVersion := publishedVersion()
	seededCard := fumigationCard()
	_, hasMode := seededCard["repeat_mode"]
	_, hasGap := seededCard["rotation_gap_days"]
	story.Assert("the shipped SOP carries no rotation and no interval", !hasMode && !hasGap && seededCard["repeat_every_days"] == nil,
		"version=%d card keys=%v", seededVersion, keysOf(seededCard))
	first := plan("deploy", fxPark, []pccareapp.RoundPenInput{{ShedID: castroID}}, day(0), op1ID, op2ID)
	submit(first.Pens[0].TaskID, op1ID, "deploy")
	for _, k := range []int{0, 1, 3} {
		tick(k)
	}
	story.Assert("no task created by the stage on the shipped SOP", allFumigation() == 1, "fumigation tasks=%d", allFumigation())
	story.Assert("the submitted pen still reached the verifier exactly as before",
		fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id = $1 AND source_ref_id = $2::uuid`, fxTenant, first.Pens[0].TaskID) == 1,
		"items=%d", fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id = $1 AND source_ref_id = $2::uuid`, fxTenant, first.Pens[0].TaskID))

	// ---------------------------------------------------------------------------
	story.Step("The 'every N days' repeat still works exactly as it did",
		"Publish 'repeat every 30 days' on the fumigation card. The stage plans Castro again for the "+
			"same operators 30 days after its planned date, two days ahead. Then the planner closes that "+
			"repeat so the rotation below starts clean.")
	if err := publish("interval-30", func(c map[string]any) { card(c, "fumigation")["repeat_every_days"] = 30 }); err != nil {
		t.Fatalf("publish interval: %v", err)
	}
	tick(27)
	story.Assert("not yet on day 27 (the horizon is day 29)", len(nextOf(first.Pens[0].TaskID)) == 0, "repeats=%d", len(nextOf(first.Pens[0].TaskID)))
	tick(28)
	interval := nextOf(first.Pens[0].TaskID)
	story.Assert("day 28: Castro planned for day 30 with both operators", len(interval) == 1 && interval[0].Label == "Castro" && interval[0].Date == day(30) && len(interval[0].Operators) == 2,
		"%+v", interval)
	if len(interval) == 1 {
		roundID := fx.scanText(`SELECT round_id::text FROM pc_care_tasks WHERE task_id = $1::uuid`, interval[0].TaskID)
		if err := pcCare.CloseRound(ctx, ceo, roundID, "Switching to a rotation", "rot-close-interval"); err != nil {
			t.Fatalf("close interval repeat: %v", err)
		}
	}

	// ---------------------------------------------------------------------------
	story.Step("A bad card is refused when it is published, by the server",
		"The same check the web editor runs is enforced by the SOP service: a rotation AND an interval "+
			"on one card, a wait outside 0..365, a wait without a rotation, and a rotating deworming "+
			"(its feed & water removal needs the evening before) are each refused, and nothing is published.")
	before := publishedVersion()
	for name, edit := range map[string]func(map[string]any){
		"both": func(c map[string]any) {
			f := card(c, "fumigation")
			f["repeat_mode"] = "rotation"
			f["repeat_every_days"] = 30
		},
		"gap 400": func(c map[string]any) {
			f := card(c, "fumigation")
			f["repeat_mode"] = "rotation"
			f["rotation_gap_days"] = 400
		},
		"gap, no rotation":   func(c map[string]any) { card(c, "fumigation")["rotation_gap_days"] = 3 },
		"unknown mode":       func(c map[string]any) { card(c, "fumigation")["repeat_mode"] = "weekly" },
		"rotating deworming": func(c map[string]any) { card(c, "deworming")["repeat_mode"] = "rotation" },
	} {
		err := publish("bad-"+strings.ReplaceAll(name, " ", "-"), edit)
		story.Assert("refused: "+name, err != nil && strings.Contains(err.Error(), "invalid_sop_dsl"), "err=%v", err)
	}
	story.Assert("nothing was published by the refused cards", publishedVersion() == before, "published=%d want %d", publishedVersion(), before)

	// ---------------------------------------------------------------------------
	story.Step("Switch fumigation to 'Rotate through the pens', wait 2 days between rounds",
		"Published through the real SOP service. The old chain was closed, so nothing moves until a planner starts the rotation.")
	if err := publish("rotation", func(c map[string]any) {
		f := card(c, "fumigation")
		delete(f, "repeat_every_days")
		f["repeat_mode"] = "rotation"
		f["rotation_gap_days"] = 2
	}); err != nil {
		t.Fatalf("publish rotation: %v", err)
	}
	rotationVersion := publishedVersion()
	tick(0)
	tick(1)
	story.Assert("no pen planned before a planner starts it", allFumigation() == 2, "fumigation tasks=%d", allFumigation())

	// ---------------------------------------------------------------------------
	story.Step("Start: the CEO plans Godel 1 - Part 1 for today; nothing moves until it is SUBMITTED",
		"The rotation waits for the operator's submit, however many times the stage ticks.")
	p1 := plan("part1", fxPark, []pccareapp.RoundPenInput{{ShedID: godelID, PartitionLabel: "Part 1"}}, day(0), op1ID, op2ID).Pens[0].TaskID
	tick(0)
	tick(0)
	story.Assert("not submitted: no next pen", len(nextOf(p1)) == 0, "next=%v", nextOf(p1))

	// ---------------------------------------------------------------------------
	story.Step("Submit Part 1: Part 2 is planned for TOMORROW, same two operators, on the published card",
		"And it is on the operator's own list for tomorrow. A second tick the same day adds nothing.")
	submit(p1, op2ID, "part1")
	tick(0)
	tick(0)
	n := nextOf(p1)
	story.Assert("exactly one next pen: Godel 1 - Part 2, tomorrow", len(n) == 1 && n[0].Label == "Godel 1 - Part 2" && n[0].Date == day(1), "%+v", n)
	story.Assert("same operators, pinned to the rotation's SOP version", len(n) == 1 && len(n[0].Operators) == 2 && n[0].Version == rotationVersion,
		"%+v (version want %d)", n, rotationVersion)
	p2 := ""
	if len(n) == 1 {
		p2 = n[0].TaskID
	}
	page, err := pcCare.Worklist(ctx, opActor(op1ID), pccaredomain.CategoryFumigation, day(1), "", 20)
	listed := false
	for _, it := range page.Items {
		listed = listed || it.TaskID == p2
	}
	story.Assert("Part 2 is on operator Amit's fumigation list for tomorrow", err == nil && listed, "err=%v items=%d", err, len(page.Items))

	// ---------------------------------------------------------------------------
	story.Step("The verifier REJECTS Part 1 after the next pen was planned",
		"'Done' means the operator submitted. The verifier's rework sends Part 1 back to be re-shot, "+
			"but it neither pulls back Part 2 nor plans a second one.")
	itemID := fx.scanText(`SELECT item_id::text FROM verification_items WHERE tenant_id = $1 AND source_ref_id = $2::uuid ORDER BY created_at DESC LIMIT 1`, fxTenant, p1)
	item, err := verification.GetItem(ctx, fxTenant, itemID)
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{TenantID: fxTenant, ItemID: item.ItemID, Decision: verificationdomain.DecisionRejected,
		Reason: "Spraying not visible", VerifierID: verifier, RowVersion: item.RowVersion, IdempotencyKey: "rot-verdict-p1"}); err != nil {
		t.Fatalf("reject part 1: %v", err)
	}
	relay()
	tick(0)
	tick(1)
	status := fx.scanText(`SELECT status FROM pc_care_tasks WHERE task_id = $1::uuid`, p1)
	story.Assert("Part 1 is back with the operator for a re-shoot", status == pccaredomain.StatusRework, "status=%s", status)
	story.Assert("still exactly one Part 2", len(nextOf(p1)) == 1, "next=%d", len(nextOf(p1)))

	// ---------------------------------------------------------------------------
	story.Step("Part 10's animal is moved out; Part 3 never had any. After Part 2 the rotation wraps to Castro after the 2-day wait",
		"The move goes through the production identity move. Pens with no live animal drop out of the "+
			"rotation; the next pen after Part 2 is therefore the first pen again, Castro. Darshan's access "+
			"is revoked before the stage runs, so the next pen carries Amit alone.")
	fx.MoveGoat(goats["Part 10"], castroID, "rot-move-part10", time.Now())
	submit(p2, op1ID, "part2")
	fx.exec("revoke darshan", `UPDATE user_scope_grants SET status = 'revoked' WHERE tenant_id = $1 AND user_id = $2`, fxTenant, op2ID)
	tick(1)
	story.Assert("day+1: wrap date (Part 2's day+1, +1, +2 wait = day+4) is past the horizon -- nothing yet", len(nextOf(p2)) == 0, "next=%v", nextOf(p2))
	tick(2)
	n = nextOf(p2)
	story.Assert("day+2: Castro planned for day+4 (empty Part 3 and emptied Part 10 skipped)", len(n) == 1 && n[0].Label == "Castro" && n[0].Date == day(4), "%+v", n)
	story.Assert("only the operator who can still be assigned", len(n) == 1 && len(n[0].Operators) == 1 && n[0].Operators[0] == op1ID, "%+v", n)
	castro := ""
	if len(n) == 1 {
		castro = n[0].TaskID
	}

	// ---------------------------------------------------------------------------
	story.Step("Nobody left: the rotation stops and the CEO is alerted ONCE; nobody is substituted",
		"Amit's access is revoked too. Castro is submitted (before Amit left). The next pen, Godel 1 - "+
			"Part 1, cannot be given to anyone: no task is made, and the planner's phone gets one "+
			"'rotation stopped' push naming the pen, however many times the stage ticks.")
	submit(castro, op1ID, "castro")
	fx.exec("revoke amit", `UPDATE user_scope_grants SET status = 'revoked' WHERE tenant_id = $1 AND user_id = $2`, fxTenant, op1ID)
	tick(4)
	tick(4)
	tick(5)
	story.Assert("no next pen", len(nextOf(castro)) == 0, "next=%v", nextOf(castro))
	alerts := fx.countRows(`SELECT count(*) FROM notification_requests WHERE tenant_id = $1 AND notification_type = 'pc_care_repeat_skipped'`, fxTenant)
	title := fx.scanText(`SELECT coalesce(max(title), '') FROM notification_requests WHERE tenant_id = $1 AND notification_type = 'pc_care_repeat_skipped'`, fxTenant)
	story.Assert("exactly one alert, saying the rotation stopped at Godel 1 - Part 1", alerts == 1 && title == "Fumigation rotation stopped · Godel 1 - Part 1",
		"alerts=%d title=%q", alerts, title)

	// ---------------------------------------------------------------------------
	story.Step("The CEO restarts it by planning Part 1 by hand with Darshan (access restored)",
		"The hand-planned pen carries the rotation on.")
	fx.exec("restore darshan", `UPDATE user_scope_grants SET status = 'active' WHERE tenant_id = $1 AND user_id = $2`, fxTenant, op2ID)
	restart := plan("restart", fxPark, []pccareapp.RoundPenInput{{ShedID: godelID, PartitionLabel: "Part 1"}}, day(5), op2ID).Pens[0].TaskID
	submit(restart, op2ID, "restart")
	tick(5)
	n = nextOf(restart)
	story.Assert("Part 2 planned for day+6 with Darshan", len(n) == 1 && n[0].Label == "Godel 1 - Part 2" && n[0].Date == day(6) && len(n[0].Operators) == 1 && n[0].Operators[0] == op2ID, "%+v", n)
	p2b := ""
	if len(n) == 1 {
		p2b = n[0].TaskID
	}

	// ---------------------------------------------------------------------------
	story.Step("The worker was DOWN for days: the next pen is planned for the day it wakes, never a past day",
		"Part 2 is submitted; the stage next runs on day+11. The wrap date would be day+9; it plans day+11.")
	submit(p2b, op2ID, "part2b")
	tick(11)
	n = nextOf(p2b)
	story.Assert("Castro planned for day+11 (today for the late worker)", len(n) == 1 && n[0].Label == "Castro" && n[0].Date == day(11), "%+v", n)

	// ---------------------------------------------------------------------------
	story.Step("Closing the open pen STOPS the rotation",
		"The CEO closes the day+11 Castro pen. The stage keeps ticking; nothing more is planned.")
	if len(n) == 1 {
		roundID := fx.scanText(`SELECT round_id::text FROM pc_care_tasks WHERE task_id = $1::uuid`, n[0].TaskID)
		if err := pcCare.CloseRound(ctx, ceo, roundID, "Pausing the rotation", "rot-close"); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	countBefore := allFumigation()
	tick(12)
	tick(20)
	story.Assert("no new fumigation task after the close", allFumigation() == countBefore, "before=%d after=%d", countBefore, allFumigation())

	// ---------------------------------------------------------------------------
	story.Step("Another park rotates on its own: a one-pen park repeats that pen and never borrows CBE's pens",
		"Second Park holds only Yashoda. Its rotation goes Yashoda -> (wrap, 2-day wait) -> Yashoda, and "+
			"CBE's stopped rotation stays stopped.")
	y := plan("yashoda", otherPark, []pccareapp.RoundPenInput{{ShedID: yashodaID}}, day(20), op3ID).Pens[0].TaskID
	submit(y, op3ID, "yashoda")
	tick(20)
	story.Assert("day+20: the wrap (day+23) is past the two-day horizon -- nothing yet", len(nextOf(y)) == 0, "next=%v", nextOf(y))
	tick(21)
	n = nextOf(y)
	story.Assert("Yashoda again on day+23 (day+20 +1 +2 wait) with its own operator", len(n) == 1 && n[0].Label == "Yashoda" && n[0].Date == day(23) && n[0].Operators[0] == op3ID, "%+v", n)
	story.Assert("CBE did not move", allFumigation() == countBefore+2, "tasks=%d want %d", allFumigation(), countBefore+2)

	// ---------------------------------------------------------------------------
	story.Step("Switch the card back to 'No repeat': the rotation stops for everyone",
		"Second Park's pending Yashoda is submitted after the switch; nothing follows it.")
	if err := publish("no-repeat", func(c map[string]any) {
		f := card(c, "fumigation")
		delete(f, "repeat_mode")
		delete(f, "rotation_gap_days")
	}); err != nil {
		t.Fatalf("publish no repeat: %v", err)
	}
	if len(n) == 1 {
		submit(n[0].TaskID, op3ID, "yashoda2")
		tick(23)
		tick(30)
		story.Assert("nothing follows the submitted Yashoda", len(nextOf(n[0].TaskID)) == 0, "next=%v", nextOf(n[0].TaskID))
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// roleHint maps a grant role to the workforce_members.primary_role_hint vocabulary.
func roleHint(role string) string {
	if role == "ceo_internal" {
		return "cxo"
	}
	return role
}
