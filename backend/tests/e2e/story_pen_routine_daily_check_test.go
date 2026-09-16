package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/notificationbridge"
	penroutinesboard "github.com/vgoats/goatos/backend/internal/penroutines/adapters/boardsource"
	penroutinespg "github.com/vgoats/goatos/backend/internal/penroutines/adapters/postgres"
	penroutinesproof "github.com/vgoats/goatos/backend/internal/penroutines/adapters/proof"
	penroutinesbridge "github.com/vgoats/goatos/backend/internal/penroutines/adapters/verificationbridge"
	penroutinesapp "github.com/vgoats/goatos/backend/internal/penroutines/app"
	penroutinesdomain "github.com/vgoats/goatos/backend/internal/penroutines/domain"
	penroutinesports "github.com/vgoats/goatos/backend/internal/penroutines/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	prooflocal "github.com/vgoats/goatos/backend/internal/proof/adapters/storage/local"
	proofapp "github.com/vgoats/goatos/backend/internal/proof/app"
	proofdomain "github.com/vgoats/goatos/backend/internal/proof/domain"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
	"github.com/vgoats/goatos/backend/internal/verificationcatalog"
	workboarddomain "github.com/vgoats/goatos/backend/internal/workboard/domain"
	workboardports "github.com/vgoats/goatos/backend/internal/workboard/ports"
	workforcepg "github.com/vgoats/goatos/backend/internal/workforce/adapters/postgres"
	workforceapp "github.com/vgoats/goatos/backend/internal/workforce/app"
)

// TestKernelStory_PenRoutineDailyCheck is the production-path proof of the maintainer's
// 2026-09-16 instruction (docs/decisions/pen-routines.md): a routine is a rule the CEO writes
// once per park -- "every day, for every occupied pen, was the pen cleaned? one photo, check in
// first" -- and the kernel turns it into one task per pen per day, worked by any of the routine's
// people, reviewed by the verifier, rolled forward as delayed when nobody does it, and raised
// the day after care work when the routine says so.
//
// Every state transition below is produced by the same code production runs:
//
//	THE RULE          penroutines app.AuthoringService (the CEO's write, eligibility refused there)
//	THE TASKS         penroutines postgres.Materialize (the kernel stage's write, the stage's date math)
//	THE PUSH          notificationbridge.PenRoutineDueNotifier over the real roster + calendar queue
//	THE WORK          penroutines app.Service: RecordPresence, Submit (proofs through proof app.Service)
//	THE VERIFIER      pen_routine.submitted -> PendingVerificationHandler -> verification.CreateItem
//	THE VERDICT       verification app.Service.RecordVerdict -> outbox -> the durable domain bus
//	                  -> PenRoutineVerificationHandler (eventwiring.RegisterVerificationAppliers)
//	THE CLOCK         penroutines postgres.SweepRollForward (the kernel stage's roll-forward)
//	THE BOARD         the pen-routine Work Board source
//	AFTER WORK        a pc_deworming verification item raised through verification.CreateItem,
//	                  read back by the after_work materializer
func TestKernelStory_PenRoutineDailyCheck(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-pen-routine-daily-check",
		"A daily pen routine: authored once, raised per occupied pen, checked in, answered, photographed, verified, rolled forward",
		"The CEO writes one rule for Coimbatore: every day, every occupied pen, the park head answers "+
			"'Was the pen cleaned?', counts the sick animals, takes one photo, and must check in to the pen "+
			"first. The kernel raises one card per occupied pen (an empty pen is skipped), tells the two "+
			"assignees once the routine's notify time has passed, refuses a submit before the check-in, and "+
			"hands the photo and the answers to the verifier as ONE item. A rejected check is sent back and "+
			"redone; an approved one completes. A pen nobody worked rolls forward as delayed with the date it "+
			"was owed and shows overdue on the Work Board. A routine with no review completes on the spot, and "+
			"an after-work routine raises the day after deworming.")
	defer story.Finish()
	story.Certify("backend kernel + verification + pen routines + notifications + work board")

	ctx := fx.Ctx
	const (
		shedGodelID  = "7d000000-0000-4000-8000-0000000d0201" // partitioned: Godel 1 - Part 3
		shedYashID   = "7d000000-0000-4000-8000-0000000d0202" // undivided: Yashoda
		shedEmptyID  = "7d000000-0000-4000-8000-0000000d0203" // undivided, no animals: Nehru
		goatGodelID  = "7d000000-0000-4000-8000-0000000d0101"
		goatYashID   = "7d000000-0000-4000-8000-0000000d0102"
		parkHeadID   = "7d000000-0000-4000-8000-0000000d0301"
		secondID     = "7d000000-0000-4000-8000-0000000d0302"
		strangerID   = "7d000000-0000-4000-8000-0000000d0303"
		verifierID   = "7d000000-0000-4000-8000-0000000d0304"
		ceoID        = "7d000000-0000-4000-8000-0000000d0305"
		partitionLbl = "Part 3"
	)

	// ---------------------------------------------------------------------------
	story.Step("A park with two occupied pens (one partitioned, one undivided), an empty third pen, and the people",
		"Pens are the partition catalog (shed_partitions), the same source the herd-register pickers use: "+
			"'Godel 1' holds one catalogued pen 'Part 3', 'Yashoda' and 'Nehru' are undivided. Live goats sit in "+
			"the first two; Nehru is empty. The park head holds a park_head grant on the park, the second "+
			"assignee a director grant, the stranger no grant at all -- eligibility is decided from those rows.")
	for _, s := range []struct {
		id, code, name string
		order          int
	}{
		{shedGodelID, "E2E-GODEL1", "Godel 1", 1}, {shedYashID, "E2E-YASH", "Yashoda", 2}, {shedEmptyID, "E2E-NEHRU", "Nehru", 3},
	} {
		fx.exec("shed "+s.name,
			`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, parent_location_id, status, display_order)
			 VALUES ($1, $2, 'shed', $3, $4, $5, 'active', $6)`, s.id, fxTenant, s.code, s.name, fxPark, s.order)
	}
	fx.exec("godel partition catalog",
		`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order)
		 VALUES ($1, $2, $3, '3', 'manual', 1)`, fxTenant, shedGodelID, partitionLbl)
	fx.SeedGoat(GoatSpec{GoatID: goatGodelID, ShedID: shedGodelID})
	fx.exec("godel goat placement",
		`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label) VALUES ($1, $2, $3, 'seed', $4)`,
		fxTenant, goatGodelID, shedGodelID, partitionLbl)
	fx.SeedGoat(GoatSpec{GoatID: goatYashID, ShedID: shedYashID})
	for _, p := range []struct{ id, code, name, hint string }{
		{parkHeadID, "PH-PR", "E2E Park Head", "park_head"}, {secondID, "SEC-PR", "Second Assignee", "pc_director"},
		{strangerID, "STR-PR", "Stranger", "operator"}, {verifierID, "VER-PR", "E2E Verifier", "verifier"}, {ceoID, "CEO-PR", "E2E CEO", "cxo"},
	} {
		fx.exec("person "+p.name,
			`INSERT INTO workforce_members (workforce_member_id, tenant_id, user_id, display_code, display_name, status, primary_role_hint, primary_location_id)
			 VALUES ($1, $2, $1, $3, $4, 'active', $5, $6)`, p.id, fxTenant, p.code, p.name, p.hint, fxPark)
	}
	fx.exec("park head grant",
		`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
		 VALUES ($1, $2, 'park_head', 'park', $3, 'active', now() - interval '1 day')`, fxTenant, parkHeadID, fxPark)
	fx.exec("second assignee grant",
		`INSERT INTO user_scope_grants (tenant_id, user_id, role, scope_type, scope_id, status, valid_from)
		 VALUES ($1, $2, 'pc_director', 'tenant', $1, 'active', now() - interval '1 day')`, fxTenant, secondID)
	for _, id := range []string{parkHeadID, secondID} {
		fx.exec("device "+id,
			`INSERT INTO workforce_member_devices (device_id, tenant_id, workforce_member_id, platform, app_install_id, fcm_token, app_version, os_version, status, last_seen_at, registered_by)
			 VALUES (gen_random_uuid(), $1, $2::uuid, 'android', 'pr-install-' || $3::text, 'pr-token-' || $3::text, '1.0.0', '14', 'active', now(), $2::uuid)`, fxTenant, id, id)
	}

	// Services, composed the way bootstrap/api.go and kernelstages compose them.
	verification := verificationapp.NewService(fx.VerifRepo, fx.VerifMedia)
	if err := verification.RegisterCategory(verificationcatalog.PenRoutine); err != nil {
		t.Fatalf("register pen routine category: %v", err)
	}
	for _, def := range verificationcatalog.PCCare() {
		if err := verification.RegisterCategory(def); err != nil {
			t.Fatalf("register pc care category: %v", err)
		}
	}
	proofService := proofapp.NewService(fx.Proof, prooflocal.New(t.TempDir(), "story-pr-proof-secret"))
	repo := penroutinespg.NewRepository(fx.Pool, 10*time.Second)
	authoring := penroutinesapp.NewAuthoringService(repo)
	routines := penroutinesapp.NewService(repo).WithProofValidator(penroutinesproof.NewValidator(fx.Proof))
	// The submit -> verifier-item enqueue is registered on every durable bus in production
	// (kernelstages/bus.go); the verdict applier is already on fx.Bus through
	// eventwiring.RegisterVerificationAppliers (domainconsumer wiring).
	penroutinesapp.NewPendingVerificationHandler(penroutinesbridge.New(verification), nil).WithTaskReader(repo).Register(fx.Bus)
	workforceRepo := workforcepg.NewRepository(fx.Pool, 10*time.Second)
	roster := workforceapp.NewRosterService(workforceRepo, workforceRepo)

	now := time.Now()
	today := biztime.BusinessDate(now)
	todayStart := biztime.BusinessDayStart(now)
	tomorrowInstant := todayStart.AddDate(0, 0, 1).Add(9 * time.Hour)
	tomorrow := biztime.BusinessDate(tomorrowInstant)

	// The outbox stamps next_attempt_at with the DATABASE clock and the relay claims with the
	// test process's; against a remote Postgres the two can differ by a second. Two rounds,
	// because a hop can write the NEXT hop's row during the drain (the verdict applier emits
	// pen_routine.verified) and that row sits behind the same skew. Harness truth, not product.
	relay := func() {
		for i := 0; i < 2; i++ {
			time.Sleep(1500 * time.Millisecond)
			fx.RelayOutboxEvents()
		}
	}
	capture := func(kind, mime, by, key string) string {
		t.Helper()
		meta := map[string]any{"capture_source": "in_app_camera", "story": "PR:" + key}
		if kind == "video" {
			meta["captured_start_ms"] = int64(1000)
			meta["captured_end_ms"] = int64(6000)
		}
		target, err := proofService.CreateUpload(ctx, proofdomain.CreateUpload{
			TenantID: fxTenant, ProofType: kind, MimeType: mime, ScopeType: "park", ScopeID: fxPark,
			SubjectType: "other", UploadedBy: storyAAPtrString(by), Metadata: meta,
		})
		if err != nil {
			t.Fatalf("create upload %s: %v", key, err)
		}
		stored, err := proofService.StoreUpload(ctx, fxTenant, target.Proof.ProofID, mime, bytes.NewBufferString("story-pr-"+key))
		if err != nil {
			t.Fatalf("store upload %s: %v", key, err)
		}
		complete := proofdomain.CompleteUpload{TenantID: fxTenant, ProofID: target.Proof.ProofID, ContentHash: stored.ContentHash, MimeType: stored.MimeType, SizeBytes: stored.SizeBytes}
		if kind == "video" {
			duration := int64(5000)
			complete.DurationMS = &duration
		}
		if _, err := proofService.CompleteUpload(ctx, complete); err != nil {
			t.Fatalf("complete upload %s: %v", key, err)
		}
		return target.Proof.ProofID
	}
	photo := func(by, key string) string { return capture("photo", "image/jpeg", by, key) }
	answers := func(m map[string]any) map[string]json.RawMessage {
		t.Helper()
		out := map[string]json.RawMessage{}
		for k, v := range m {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			out[k] = b
		}
		return out
	}
	itemFor := func(taskID string) verificationdomain.Item {
		t.Helper()
		var itemID string
		if err := fx.Pool.QueryRow(ctx, `SELECT item_id::text FROM verification_items WHERE tenant_id=$1 AND source_module=$2 AND source_ref_type=$3 AND source_ref_id=$4::uuid ORDER BY created_at DESC LIMIT 1`,
			fxTenant, penroutinesapp.VerificationModule, penroutinesapp.VerificationRefType, taskID).Scan(&itemID); err != nil {
			t.Fatalf("verification item for task %s: %v", taskID, err)
		}
		item, err := verification.GetItem(ctx, fxTenant, itemID)
		if err != nil {
			t.Fatalf("get item %s: %v", itemID, err)
		}
		return item
	}
	verdict := func(item verificationdomain.Item, decision, reason, key string) {
		t.Helper()
		if _, err := verification.RecordVerdict(ctx, verificationdomain.Verdict{TenantID: fxTenant, ItemID: item.ItemID, Decision: decision, Reason: reason, VerifierID: verifierID, RowVersion: item.RowVersion, IdempotencyKey: "pr-verdict-" + key}); err != nil {
			t.Fatalf("verdict %s: %v", key, err)
		}
		relay()
	}
	contextValue := func(item verificationdomain.Item, label string) string {
		for _, row := range item.ContextRows {
			if row.Label == label {
				return row.Value
			}
		}
		return ""
	}
	head := penroutinesdomain.Actor{UserID: parkHeadID}
	write := func(key string) penroutinesports.WriteParams {
		return penroutinesports.WriteParams{TenantID: fxTenant, ActorID: ceoID, IdempotencyKey: "pr-" + key, TraceID: "pr-" + key}
	}
	boardQuery := func(date string) workboardports.SourceQuery {
		return workboardports.SourceQuery{TenantID: fxTenant, ParkID: fxPark, BusinessDate: date, Limit: 50}
	}
	board := penroutinesboard.New(fx.Pool, 10*time.Second)

	// ---------------------------------------------------------------------------
	story.Step("The CEO authors ONE daily routine for the park; a stranger cannot be assigned",
		"The rule is the whole configuration the maintainer asked for: scope all pens, occupied only, "+
			"reviewed by the verifier, a yes/no and a number question, one to two photos, up to one video, "+
			"presence required, two assignees. It is written as version 1. An assignee who holds no Routines "+
			"access for the park is refused with the person named -- there is never a fallback person.")
	zero, five := 0.0, 500.0
	evidence := penroutinesdomain.Evidence{
		Questions: []penroutinesdomain.Question{
			{ID: "cleaned", Kind: penroutinesdomain.QuestionYesNo, Title: "Was the pen cleaned?", Required: true},
			{ID: "sick", Kind: penroutinesdomain.QuestionNumber, Title: "Sick animals", Min: &zero, Max: &five},
		},
		Photo:    penroutinesdomain.ProofRule{Min: 1, Max: 2},
		Video:    penroutinesdomain.ProofRule{Min: 0, Max: 1},
		Presence: penroutinesdomain.PresenceRequired,
	}
	definition := penroutinesdomain.Definition{
		ParkID: fxPark, Name: "Pen cleaning", Instruction: "Sweep the pen and check the water trough.",
		ScopeKind: penroutinesdomain.ScopeAllPens, OccupiedOnly: true, CadenceKind: penroutinesdomain.CadenceDaily,
		NotifyTime: "07:00", ReviewKind: penroutinesdomain.ReviewVerifier, Evidence: evidence,
		AssigneeIDs: []string{parkHeadID, secondID},
	}
	withStranger := definition
	withStranger.AssigneeIDs = []string{parkHeadID, strangerID}
	_, err := authoring.Create(ctx, write("create-stranger"), withStranger)
	story.Assert("an assignee with no Routines access for the park is refused", errors.Is(err, penroutinesports.ErrAssigneeNotEligible), "err=%v", err)
	daily, err := authoring.Create(ctx, write("create-daily"), definition)
	story.Assert("the daily routine is written as version 1 with both people", err == nil && daily.CurrentVersion == 1 && daily.Status == penroutinesdomain.StatusActive && len(daily.Assignees) == 2 && daily.ParkName != "", "err=%v routine=%+v", err, daily)
	if err != nil {
		return
	}
	versions := fx.countRows(`SELECT count(*) FROM pen_routine_versions WHERE tenant_id=$1 AND routine_id=$2::uuid AND version=1`, fxTenant, daily.RoutineID)
	story.Assert("exactly one version row exists for the new routine", versions == 1, "versions=%d", versions)

	// ---------------------------------------------------------------------------
	story.Step("The kernel raises today's tasks: one per OCCUPIED pen, labelled through oploc; a second tick raises nothing",
		"Materialize is the kernel stage's write for one business date. The empty pen is skipped "+
			"(occupied_only), the partitioned pen reads 'Godel 1 - Part 3' and the undivided one its bare "+
			"name. The natural key (routine, shed, pen, planned date) makes the replay insert nothing.")
	result, err := repo.Materialize(ctx, fxTenant, today, today, now)
	story.Assert("exactly two tasks were raised (the empty pen skipped)", err == nil && result.Created == 2 && result.Widened == 0 && len(result.RoutinesWithoutAssignee) == 0, "err=%v result=%+v", err, result)
	replay, err := repo.Materialize(ctx, fxTenant, today, today, now)
	story.Assert("a second tick inserts nothing (natural key)", err == nil && replay.Created == 0 && replay.Widened == 0, "err=%v replay=%+v", err, replay)
	headList, err := routines.ListMine(ctx, fxTenant, parkHeadID, "todo", 20, "")
	story.Assert("the park head lists both tasks", err == nil && len(headList.Rows) == 2, "err=%v rows=%d", err, len(headList.Rows))
	var godelTask, yashTask penroutinesdomain.Task
	for _, row := range headList.Rows {
		switch row.ShedID {
		case shedGodelID:
			godelTask = row
		case shedYashID:
			yashTask = row
		}
	}
	story.Assert("the partitioned pen is labelled 'Godel 1 - Part 3' through oploc", godelTask.TaskID != "" && godelTask.PenLabel == "Godel 1 - Part 3" && godelTask.Partition == partitionLbl, "task=%+v", godelTask)
	story.Assert("the undivided pen is labelled by its bare shed name", yashTask.TaskID != "" && yashTask.PenLabel == "Yashoda" && yashTask.Partition == "", "task=%+v", yashTask)
	story.Assert("both are due today, open on both dimensions, pinned to version 1", godelTask.DueDate == today && yashTask.DueDate == today && godelTask.WorkState == penroutinesdomain.WorkStateScheduled && godelTask.Status == penroutinesdomain.StatusOpen && godelTask.RoutineVersion == 1, "godel=%+v yash=%+v", godelTask, yashTask)
	if godelTask.TaskID == "" || yashTask.TaskID == "" {
		return
	}

	// ---------------------------------------------------------------------------
	story.Step("The second assignee sees both, the stranger sees none; the morning push goes out once the notify time has passed",
		"The list is 'the routines I am assigned to'. The due-push notifier is the kernel stage's second "+
			"act: ONE digest per routine naming the pens still owed, to every assignee's devices, keyed on the "+
			"business date so a later tick queues nothing again. Before 07:00 IST it stays silent.")
	secondList, err := routines.ListMine(ctx, fxTenant, secondID, "todo", 20, "")
	strangerList, err2 := routines.ListMine(ctx, fxTenant, strangerID, "todo", 20, "")
	story.Assert("the second assignee lists both tasks; the stranger lists nothing", err == nil && err2 == nil && len(secondList.Rows) == 2 && len(strangerList.Rows) == 0, "err=%v/%v rows=%d/%d", err, err2, len(secondList.Rows), len(strangerList.Rows))
	digests, err := repo.DueDigests(ctx, fxTenant, today)
	story.Assert("the kernel reads ONE digest for the routine carrying both pens and both assignees", err == nil && len(digests) == 1 && len(digests[0].Tasks) == 2 && len(digests[0].AssigneeIDs) == 2 && digests[0].NotifyTime == "07:00", "err=%v digests=%+v", err, digests)
	pushCount := func() (rows, recipients, events int) {
		t.Helper()
		if err := fx.Pool.QueryRow(ctx, `SELECT count(*), count(DISTINCT recipient_ref), count(DISTINCT calendar_event_id) FROM notification_requests WHERE tenant_id=$1 AND notification_type=$2`,
			fxTenant, notificationbridge.NotificationTypePenRoutineDue).Scan(&rows, &recipients, &events); err != nil {
			t.Fatalf("count pushes: %v", err)
		}
		return rows, recipients, events
	}
	early := notificationbridge.NewPenRoutineDueNotifier(roster, fx.Calendar, nil).WithClock(func() time.Time { return todayStart.Add(6 * time.Hour) })
	err = early.NotifyDue(ctx, fxTenant, digests)
	rows, _, _ := pushCount()
	story.Assert("at 06:00 IST nothing is queued -- the routine's notify time has not passed", err == nil && rows == 0, "err=%v rows=%d", err, rows)
	notifier := notificationbridge.NewPenRoutineDueNotifier(roster, fx.Calendar, nil).WithClock(func() time.Time { return todayStart.Add(8 * time.Hour) })
	err = notifier.NotifyDue(ctx, fxTenant, digests)
	rows, recipients, events := pushCount()
	story.Assert("at 08:00 IST ONE push for the routine reaches the two assignees' devices", err == nil && events == 1 && recipients == 2 && rows == 2, "err=%v rows=%d recipients=%d events=%d", err, rows, recipients, events)
	var pushTitle, pushBody string
	if err := fx.Pool.QueryRow(ctx, `SELECT title, body FROM notification_requests WHERE tenant_id=$1 AND notification_type=$2 ORDER BY created_at LIMIT 1`, fxTenant, notificationbridge.NotificationTypePenRoutineDue).Scan(&pushTitle, &pushBody); err != nil {
		t.Fatalf("read push: %v", err)
	}
	story.Assert("the push names the routine, the park and both pens", pushTitle == "Pen cleaning: 2 pens at Coimbatore" && contains(pushBody, "Godel 1 - Part 3") && contains(pushBody, "Yashoda"), "title=%q body=%q", pushTitle, pushBody)
	err = notifier.NotifyDue(ctx, fxTenant, digests)
	rows, _, _ = pushCount()
	story.Assert("a later tick the same day queues nothing again (business-date event key)", err == nil && rows == 2, "err=%v rows=%d", err, rows)

	// ---------------------------------------------------------------------------
	story.Step("The park head must check in first; then the answers and the photo go to the verifier as ONE item",
		"Submit before the check-in is refused (presence_missing). RecordPresence 'enter' stamps the "+
			"check-in; the submit records the leave in the same transaction, locks the task pending "+
			"verification, and announces pen_routine.submitted, which the production consumer turns into "+
			"ONE verifier item carrying the photo and the answers as context rows.")
	goodAnswers := answers(map[string]any{"cleaned": "yes", "sick": 0})
	_, err = routines.Submit(ctx, penroutinesports.SubmitParams{TenantID: fxTenant, Actor: head, TaskID: godelTask.TaskID, Answers: goodAnswers, Proofs: []penroutinesdomain.ProofItem{{Ref: photo(parkHeadID, "early"), Kind: penroutinesdomain.ProofKindPhoto}}, RowVersion: godelTask.RowVersion, IdempotencyKey: "pr-submit-early"})
	story.Assert("submit before the check-in is refused with presence_missing", errors.Is(err, penroutinesdomain.ErrPresenceMissing) && penroutinesapp.HTTPError(err).Code == "presence_missing", "err=%v", err)
	entered, err := routines.RecordPresence(ctx, penroutinesports.PresenceParams{TenantID: fxTenant, Actor: head, TaskID: godelTask.TaskID, EventType: penroutinesdomain.PresenceEnter, CapturedAt: now, RowVersion: godelTask.RowVersion, IdempotencyKey: "pr-enter-1"})
	story.Assert("the park head checks in to the pen", err == nil && entered.EnteredAt != nil && entered.EnteredBy == parkHeadID && entered.LeftAt == nil, "err=%v task=%+v", err, entered)
	if err != nil {
		return
	}
	submitted, err := routines.Submit(ctx, penroutinesports.SubmitParams{TenantID: fxTenant, Actor: head, TaskID: godelTask.TaskID, Answers: goodAnswers, Proofs: []penroutinesdomain.ProofItem{{Ref: photo(parkHeadID, "first"), Kind: penroutinesdomain.ProofKindPhoto}}, RowVersion: entered.RowVersion, CapturedAt: now.Add(10 * time.Minute), IdempotencyKey: "pr-submit-1", TraceID: "pr-submit-1"})
	story.Assert("the submit lands pending verification with the leave stamped", err == nil && submitted.Status == penroutinesdomain.StatusPendingVerification && submitted.WorkState == penroutinesdomain.WorkStateScheduled && submitted.LeftAt != nil && submitted.SubmittedBy == parkHeadID && submitted.Answers["cleaned"] == "yes", "err=%v task=%+v", err, submitted)
	if err != nil {
		return
	}
	punches := fx.countRows(`SELECT count(*) FROM pen_routine_task_presence WHERE tenant_id=$1 AND task_id=$2::uuid`, fxTenant, godelTask.TaskID)
	enterLeave := fx.countRows(`SELECT count(DISTINCT event_type) FROM pen_routine_task_presence WHERE tenant_id=$1 AND task_id=$2::uuid AND event_type IN ('enter','leave')`, fxTenant, godelTask.TaskID)
	story.Assert("the presence rows are the enter and the leave", punches == 2 && enterLeave == 2, "rows=%d kinds=%d", punches, enterLeave)
	relay()
	items := fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_module=$2 AND source_ref_id=$3::uuid`, fxTenant, penroutinesapp.VerificationModule, godelTask.TaskID)
	story.Assert("the production consumer minted exactly ONE verifier item", items == 1, "items=%d", items)
	item := itemFor(godelTask.TaskID)
	story.Assert("the item is category pen_routine and carries the photo", item.Category == penroutinesapp.VerificationCategory && len(item.MediaRefs) == 1, "item=%+v", item)
	story.Assert("the answers ride as context rows in farm words", contextValue(item, "Was the pen cleaned?") == "Yes" && contextValue(item, "Sick animals") == "0" && contextValue(item, "Pen") == "Godel 1 - Part 3", "rows=%+v", item.ContextRows)

	// ---------------------------------------------------------------------------
	story.Step("The verifier REJECTS: the check is sent back and the check-in cleared; a redo is APPROVED and completes",
		"Reject flips the task to rework with the verifier's words and clears the check-in stamps so the "+
			"redo starts with a fresh pen check-in. Approve completes BOTH dimensions with the verifier's "+
			"stamps and announces pen_routine.verified in the same transaction.")
	verdict(item, verificationdomain.DecisionRejected, "Water trough not in frame", "reject-1")
	bounced, err := repo.GetTask(ctx, fxTenant, godelTask.TaskID)
	story.Assert("the task is sent back with the reason, check-in cleared, clock still open", err == nil && bounced.Status == penroutinesdomain.StatusRework && bounced.ReworkReason == "Water trough not in frame" && bounced.EnteredAt == nil && bounced.WorkState == penroutinesdomain.WorkStateScheduled, "err=%v task=%+v", err, bounced)
	_, err = routines.Submit(ctx, penroutinesports.SubmitParams{TenantID: fxTenant, Actor: head, TaskID: godelTask.TaskID, Answers: goodAnswers, Proofs: []penroutinesdomain.ProofItem{{Ref: photo(parkHeadID, "redo-early"), Kind: penroutinesdomain.ProofKindPhoto}}, RowVersion: bounced.RowVersion, IdempotencyKey: "pr-submit-redo-early"})
	story.Assert("the redo also needs a fresh check-in first", errors.Is(err, penroutinesdomain.ErrPresenceMissing), "err=%v", err)
	reentered, err := routines.RecordPresence(ctx, penroutinesports.PresenceParams{TenantID: fxTenant, Actor: head, TaskID: godelTask.TaskID, EventType: penroutinesdomain.PresenceEnter, CapturedAt: now.Add(time.Hour), RowVersion: bounced.RowVersion, IdempotencyKey: "pr-enter-2"})
	story.Assert("the park head checks in again", err == nil && reentered.EnteredAt != nil, "err=%v", err)
	if err != nil {
		return
	}
	redone, err := routines.Submit(ctx, penroutinesports.SubmitParams{TenantID: fxTenant, Actor: head, TaskID: godelTask.TaskID, Answers: goodAnswers, Proofs: []penroutinesdomain.ProofItem{{Ref: photo(parkHeadID, "redo"), Kind: penroutinesdomain.ProofKindPhoto}}, RowVersion: reentered.RowVersion, CapturedAt: now.Add(70 * time.Minute), IdempotencyKey: "pr-submit-2", TraceID: "pr-submit-2"})
	story.Assert("the redo is accepted for review with the old reason gone", err == nil && redone.Status == penroutinesdomain.StatusPendingVerification && redone.ReworkReason == "" && redone.RowVersion > bounced.RowVersion, "err=%v task=%+v", err, redone)
	relay()
	items = fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_module=$2 AND source_ref_id=$3::uuid`, fxTenant, penroutinesapp.VerificationModule, godelTask.TaskID)
	story.Assert("the redo minted a SECOND verifier item (a retry would have collapsed)", items == 2, "items=%d", items)
	verdict(itemFor(godelTask.TaskID), verificationdomain.DecisionApproved, "", "approve-1")
	verified, err := repo.GetTask(ctx, fxTenant, godelTask.TaskID)
	story.Assert("approve completes the task on both dimensions with the verifier's stamps", err == nil && verified.Status == penroutinesdomain.StatusCompleted && verified.WorkState == penroutinesdomain.WorkStateCompleted && verified.VerifiedBy == verifierID && verified.VerifiedAt != nil, "err=%v task=%+v", err, verified)
	verifiedEvents := fx.countRows(`SELECT count(*) FROM outbox_messages WHERE tenant_id=$1 AND event_type='pen_routine.verified' AND aggregate_id=$2::uuid`, fxTenant, godelTask.TaskID)
	story.Assert("pen_routine.verified rode the approve transaction, once", verifiedEvents == 1, "events=%d", verifiedEvents)
	doneList, err := routines.ListMine(ctx, fxTenant, parkHeadID, "done", 20, "")
	story.Assert("the park head's Done chip lists the verified check", err == nil && len(doneList.Rows) == 1 && doneList.Rows[0].TaskID == godelTask.TaskID, "err=%v rows=%d", err, len(doneList.Rows))

	// ---------------------------------------------------------------------------
	story.Step("The other pen is never worked: the next day's roll-forward marks it delayed with the date it was owed; the Work Board reads overdue",
		"SweepRollForward is the kernel stage's third act. A task past its due date moves to today as "+
			"delayed, delayed_since = the day it was owed, and never disappears. The Work Board rows it under "+
			"Tasks as overdue on the day it is now due; the verified pen reads done on its own day.")
	sweep, err := repo.SweepRollForward(ctx, fxTenant, tomorrowInstant, 200, 50)
	story.Assert("exactly one task rolled forward", err == nil && sweep.RolledForward == 1 && !sweep.Truncated, "err=%v sweep=%+v", err, sweep)
	delayed, err := repo.GetTask(ctx, fxTenant, yashTask.TaskID)
	story.Assert("the unworked pen is delayed since today, due tomorrow, planned date immutable", err == nil && delayed.WorkState == penroutinesdomain.WorkStateDelayed && delayed.DelayedSince != nil && *delayed.DelayedSince == today && delayed.DueDate == tomorrow && delayed.PlannedDate == today && delayed.RolledFwd == 1, "err=%v task=%+v", err, delayed)
	story.Assert("its chip says delayed since today", penroutinesdomain.StateChip(delayed, tomorrow) == "Delayed since "+biztime.FarmDateFromBusinessDate(today), "chip=%q", penroutinesdomain.StateChip(delayed, tomorrow))
	rowsTomorrow, err := board.ListRows(ctx, boardQuery(tomorrow))
	story.Assert("the Work Board rows the delayed pen under Tasks as OVERDUE on the day it is now due", err == nil && boardState(rowsTomorrow, yashTask.TaskID) == workboarddomain.WorkStateOverdue && boardRowModule(rowsTomorrow, yashTask.TaskID) == workboarddomain.ModuleTasks, "err=%v rows=%+v", err, rowsTomorrow)
	rowsToday, err := board.ListRows(ctx, boardQuery(today))
	story.Assert("the verified pen reads DONE on the board for today", err == nil && boardState(rowsToday, godelTask.TaskID) == workboarddomain.WorkStateCompleted && boardTitle(rowsToday, godelTask.TaskID) == "Pen cleaning · Godel 1 - Part 3", "err=%v state=%q title=%q", err, boardState(rowsToday, godelTask.TaskID), boardTitle(rowsToday, godelTask.TaskID))

	// ---------------------------------------------------------------------------
	story.Step("A routine with NO review and no presence completes on the spot",
		"review 'none' means the submit is the completion: both dimensions close in the submit's own "+
			"transaction and no verifier item is ever minted for it.")
	quick, err := authoring.Create(ctx, write("create-quick"), penroutinesdomain.Definition{
		ParkID: fxPark, Name: "Water check", Instruction: "Is the trough full?",
		ScopeKind: penroutinesdomain.ScopeAllPens, OccupiedOnly: true, CadenceKind: penroutinesdomain.CadenceDaily,
		NotifyTime: "07:00", ReviewKind: penroutinesdomain.ReviewNone,
		Evidence: penroutinesdomain.Evidence{
			Questions: []penroutinesdomain.Question{{ID: "full", Kind: penroutinesdomain.QuestionYesNo, Title: "Is the trough full?", Required: true}},
			Presence:  penroutinesdomain.PresenceOff,
		},
		AssigneeIDs: []string{parkHeadID},
	})
	story.Assert("the no-review routine is written", err == nil && quick.ReviewKind == penroutinesdomain.ReviewNone, "err=%v", err)
	if err != nil {
		return
	}
	quickResult, err := repo.Materialize(ctx, fxTenant, today, today, now)
	story.Assert("the tick raises its two pens and nothing else (the daily routine's are already there)", err == nil && quickResult.Created == 2, "err=%v result=%+v", err, quickResult)
	var quickTaskID string
	var quickRowVersion int
	if err := fx.Pool.QueryRow(ctx, `SELECT task_id::text, row_version FROM pen_routine_tasks WHERE tenant_id=$1 AND routine_id=$2::uuid AND shed_id=$3::uuid`, fxTenant, quick.RoutineID, shedYashID).Scan(&quickTaskID, &quickRowVersion); err != nil {
		t.Fatalf("read quick task: %v", err)
	}
	quickDone, err := routines.Submit(ctx, penroutinesports.SubmitParams{TenantID: fxTenant, Actor: head, TaskID: quickTaskID, Answers: answers(map[string]any{"full": "yes"}), RowVersion: quickRowVersion, IdempotencyKey: "pr-submit-quick"})
	story.Assert("the submit completes the task on the spot, no check-in asked", err == nil && quickDone.Status == penroutinesdomain.StatusCompleted && quickDone.WorkState == penroutinesdomain.WorkStateCompleted && quickDone.LeftAt == nil, "err=%v task=%+v", err, quickDone)
	relay()
	quickItems := fx.countRows(`SELECT count(*) FROM verification_items WHERE tenant_id=$1 AND source_module=$2 AND source_ref_id=$3::uuid`, fxTenant, penroutinesapp.VerificationModule, quickTaskID)
	story.Assert("no verifier item was minted for it", quickItems == 0, "items=%d", quickItems)

	// ---------------------------------------------------------------------------
	story.Step("An AFTER-WORK routine raises the day after deworming, in the dewormed pen only",
		"'Trigger it after any task' is a cadence: the routine lists work kinds, and the pens where work of "+
			"that kind was SUBMITTED today (its verification item, the pen-visit read) owe the check tomorrow. "+
			"The deworming's item is raised through the verification service exactly as the PC Care submit "+
			"bridge raises it; the materializer for tomorrow reads it back and raises one task with the reason "+
			"'After deworming yesterday'.")
	afterWork, err := authoring.Create(ctx, write("create-after"), penroutinesdomain.Definition{
		ParkID: fxPark, Name: "After deworming check", Instruction: "Look for animals still off feed.",
		ScopeKind: penroutinesdomain.ScopeAllPens, OccupiedOnly: true, CadenceKind: penroutinesdomain.CadenceAfterWork,
		AfterWorkKinds: []string{penroutinesdomain.WorkDeworming}, DueOffsetDays: 1, NotifyTime: "07:00", ReviewKind: penroutinesdomain.ReviewNone,
		Evidence: penroutinesdomain.Evidence{
			Questions: []penroutinesdomain.Question{{ID: "off_feed", Kind: penroutinesdomain.QuestionNumber, Title: "Animals off feed", Min: &zero, Max: &five}},
			Presence:  penroutinesdomain.PresenceOff,
		},
		AssigneeIDs: []string{parkHeadID},
	})
	story.Assert("the after-work routine is written for deworming with a one-day offset", err == nil && afterWork.CadenceKind == penroutinesdomain.CadenceAfterWork && afterWork.DueOffsetDays == 1, "err=%v", err)
	if err != nil {
		return
	}
	dewormClip := capture("video", "video/mp4", parkHeadID, "deworm")
	shedRef, partitionRef, parkRef, operatorRef := shedGodelID, partitionLbl, fxPark, parkHeadID
	subject := "Deworming · Godel 1 - Part 3"
	created, err := verification.CreateItem(ctx, verificationdomain.CreateItem{
		TenantID: fxTenant, Vertical: "preventive_care", Module: "pc_care", Category: penroutinesdomain.WorkKindCategory(penroutinesdomain.WorkDeworming),
		SubjectLabel: &subject, Source: verificationdomain.SourceRef{Module: "pc_care", RefType: "pc_care_task", RefID: "7d000000-0000-4000-8000-0000000d0901"},
		MediaRefs: []string{dewormClip}, OperatorID: &operatorRef, ShedID: &shedRef, PartitionLabel: &partitionRef, ParkID: &parkRef,
		CapturedAt: now, IdempotencyKey: "pr-deworm-item",
	})
	story.Assert("today's deworming in Godel 1 - Part 3 sits in the verifier queue (the fact the after-work read keys on)", err == nil && created.Created, "err=%v created=%+v", err, created)
	awResult, err := repo.Materialize(ctx, fxTenant, today, tomorrow, tomorrowInstant)
	story.Assert("tomorrow's tick raises exactly one after-work task", err == nil && awResult.Created == 1, "err=%v result=%+v", err, awResult)
	var awTaskID string
	if err := fx.Pool.QueryRow(ctx, `SELECT task_id::text FROM pen_routine_tasks WHERE tenant_id=$1 AND routine_id=$2::uuid`, fxTenant, afterWork.RoutineID).Scan(&awTaskID); err != nil {
		t.Fatalf("read after-work task: %v", err)
	}
	awTask, err := repo.GetTask(ctx, fxTenant, awTaskID)
	story.Assert("it is the dewormed pen, due tomorrow, triggered by deworming", err == nil && awTask.PenLabel == "Godel 1 - Part 3" && awTask.PlannedDate == tomorrow && awTask.DueDate == tomorrow && awTask.SourceDate == today && len(awTask.TriggerKinds) == 1 && awTask.TriggerKinds[0] == penroutinesdomain.WorkDeworming && awTask.WorkState == penroutinesdomain.WorkStateScheduled, "err=%v task=%+v", err, awTask)
	story.Assert("its reason line reads 'After deworming yesterday'", penroutinesdomain.ReasonLine(awTask, tomorrow) == "After deworming yesterday", "reason=%q", penroutinesdomain.ReasonLine(awTask, tomorrow))
	awReplay, err := repo.Materialize(ctx, fxTenant, today, tomorrow, tomorrowInstant)
	story.Assert("a replay of tomorrow's tick raises nothing new", err == nil && awReplay.Created == 0, "err=%v result=%+v", err, awReplay)
}

func boardRowModule(rows []workboarddomain.Row, sourceID string) workboarddomain.Module {
	for _, r := range rows {
		if r.SourceID == sourceID {
			return r.Module
		}
	}
	return ""
}

func boardTitle(rows []workboarddomain.Row, sourceID string) string {
	for _, r := range rows {
		if r.SourceID == sourceID {
			return r.Title
		}
	}
	return ""
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
