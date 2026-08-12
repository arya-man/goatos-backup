package e2e

import (
	"testing"
	"time"

	feeddirectionpg "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/postgres"
	feeddirectionbridge "github.com/vgoats/goatos/backend/internal/feeddirection/adapters/verificationbridge"
	feeddirectionapp "github.com/vgoats/goatos/backend/internal/feeddirection/app"
	feeddirectiondomain "github.com/vgoats/goatos/backend/internal/feeddirection/domain"
	feeddirectionports "github.com/vgoats/goatos/backend/internal/feeddirection/ports"
	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	verificationpg "github.com/vgoats/goatos/backend/internal/verification/adapters/postgres"
	verificationapp "github.com/vgoats/goatos/backend/internal/verification/app"
	verificationdomain "github.com/vgoats/goatos/backend/internal/verification/domain"
)

// TestKernelStory_FeedTransportIsOneTaskPerShed is the production-path proof that Feed Transport is
// ONE daily task per physical shed, whatever the shed's pen layout.
//
// The defect it closes: migration 000143 fanned the materializer out over shed_partitions, so a
// partitioned shed produced one transport task PER PEN. Coimbatore's Godel 1 has ten pens, so the
// operator's list asked for ten videos of a single load. Both AGENTS.md and
// docs/decisions/feed-transport-verification.md already said the grain is
// (tenant_id, business_date, shed_id); 000152 is the forward repair.
//
//	PRODUCER  feeddirection.MaterializeTransportTasks  (the same call the scheduler makes)
//	READ      feeddirection.ListTransportTasks         (the phone's list + filter vocabulary)
//	WRITE     feeddirection app.Service.SubmitTransport -> verificationbridge.NewTransport
//	          -> verification.CreateItem                (the verifier's queue)
//
// The payoff is the last hop: ONE verification item reaches the verifier for a ten-pen shed, because
// the whole chain carries one task. A per-pen grain would have queued ten clips of one trip.
func TestKernelStory_FeedTransportIsOneTaskPerShed(t *testing.T) {
	fx := NewFixture(t)
	story := NewStory(t, "story-feed-transport-shed-grain",
		"Feed Transport is one task per shed, never one per pen",
		"A shed's pens are packed as separate bags and fed separately, but the feed for all of them is "+
			"LOADED AND STAGED as one trip. Feed Transport is therefore one daily task, one video and one "+
			"verifier review per physical shed, no matter how many pens the shed is divided into.")
	defer story.Finish()
	story.Certify("backend kernel")

	ctx := fx.Ctx

	const (
		partitionedShed = "5f000000-0000-4000-8000-0000000f0201" // three pens, like Castro
		plainShed       = "5f000000-0000-4000-8000-0000000f0202" // undivided
		// One shared stage row: SeedShed always declares stage_code 'K1', which is unique per tenant,
		// so two ids would collide. Transport has no stage grain anyway.
		sharedStage = "5f000000-0000-4000-8000-0000000f02a1"
		operator    = "5f000000-0000-4000-8000-0000000f0301"
		parkHead    = "5f000000-0000-4000-8000-0000000f0302"
		verifier    = "5f000000-0000-4000-8000-0000000f0303"
	)

	story.Step("A park with one partitioned shed and one undivided shed",
		"The partitioned shed carries three ACTIVE pens in the shed_partitions catalog. Under the "+
			"per-pen grain this shed alone produced three transport tasks for one day.")
	fx.SeedShed(partitionedShed, "E2E-FT-PART", sharedStage)
	fx.SeedShed(plainShed, "E2E-FT-PLAIN", sharedStage)
	for _, pen := range []struct{ label, normalized string }{{"1", "1"}, {"2", "2"}, {"3", "3"}} {
		fx.exec("transport pen catalog",
			`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, status, source)
			 VALUES ($1, $2, $3, $4, 'active', 'manual')`,
			fxTenant, partitionedShed, pen.label, pen.normalized)
	}

	repo := feeddirectionpg.NewRepository(fx.Pool, 10*time.Second)
	day := time.Date(2026, 8, 12, 0, 0, 0, 0, biztime.DefaultLocation())
	afterCutoff := day.Add(15*time.Hour + 30*time.Minute)

	story.Step("The scheduler materializes the day's transport work",
		"MaterializeTransportTasks is the production call. It runs twice here on purpose: overlapping "+
			"workers and retries are normal, and the daily shed key must absorb them.")
	if _, err := repo.MaterializeTransportTasks(ctx, feeddirectionports.MaterializeTransportParams{
		TenantID: fxTenant, AsOf: afterCutoff,
	}); err != nil {
		t.Fatalf("materialize transport tasks: %v", err)
	}
	if _, err := repo.MaterializeTransportTasks(ctx, feeddirectionports.MaterializeTransportParams{
		TenantID: fxTenant, AsOf: afterCutoff.Add(30 * time.Minute),
	}); err != nil {
		t.Fatalf("re-materialize transport tasks: %v", err)
	}

	partitionedTasks := fx.countRows(
		`SELECT count(*) FROM feed_transport_tasks WHERE tenant_id=$1 AND shed_id=$2 AND business_date=$3::date AND status <> 'retired'`,
		fxTenant, partitionedShed, day.Format("2006-01-02"))
	story.Assert("the three-pen shed produced exactly ONE transport task", partitionedTasks == 1,
		"tasks=%d (a per-pen grain would produce 3)", partitionedTasks)

	plainTasks := fx.countRows(
		`SELECT count(*) FROM feed_transport_tasks WHERE tenant_id=$1 AND shed_id=$2 AND business_date=$3::date AND status <> 'retired'`,
		fxTenant, plainShed, day.Format("2006-01-02"))
	story.Assert("the undivided shed produced exactly one too", plainTasks == 1, "tasks=%d", plainTasks)

	storedPartition := fx.scanText(
		`SELECT COALESCE(partition_label,'') FROM feed_transport_tasks WHERE tenant_id=$1 AND shed_id=$2 AND business_date=$3::date`,
		fxTenant, partitionedShed, day.Format("2006-01-02"))
	story.Assert("the task names no pen", storedPartition == "", "partition_label=%q", storedPartition)

	story.Step("The operator's list and its shed filter agree with that grain",
		"ListTransportTasks serves the phone. The row must read as the bare shed, and the filter "+
			"vocabulary must offer the shed ONCE — it previously listed the same shed once per pen, keyed "+
			"by an opaque <shed>\\x1f<pen> composite the operator could not tell apart.")
	// Scoped to this shed: the materializer legitimately creates a task for EVERY active shed the
	// baseline fixture carries, and the page is a bounded ~20-row keyset ordered by task_id, so an
	// unscoped read is a lottery. The filter VOCABULARY below is unaffected -- it is whole-date
	// scoped by contract and never narrows to the selected shed, which is exactly what is asserted.
	page, err := repo.ListTransportTasks(ctx, feeddirectionports.ListTransportTasksParams{
		TenantID: fxTenant, Day: day, ShedID: partitionedShed, Limit: 20,
	})
	if err != nil {
		t.Fatalf("list transport tasks: %v", err)
	}
	rowsForPartitioned := 0
	display := ""
	taskID := ""
	for _, item := range page.Items {
		if item.ShedID == partitionedShed {
			rowsForPartitioned++
			display = item.OperationalLocationDisplay
			taskID = item.TaskID
		}
	}
	story.Assert("the list shows the partitioned shed once", rowsForPartitioned == 1, "rows=%d", rowsForPartitioned)
	story.Assert("and names it by the bare shed, with no pen suffix", display == "E2E-FT-PART", "display=%q", display)

	shedOptions := 0
	for _, option := range page.Filters.Sheds {
		if option.ID == partitionedShed {
			shedOptions++
			story.Assert("the filter option is keyed by the plain shed UUID, not a pen composite",
				option.PartitionLabel == "", "partition_label=%q", option.PartitionLabel)
		}
	}
	story.Assert("the shed filter offers that shed exactly once", shedOptions == 1, "options=%d", shedOptions)

	story.Step("One submitted video reaches the verifier as ONE review item",
		"The service is wired exactly as bootstrap/api.go wires it: SubmitTransport appends the proof "+
			"attempt and the transport bridge creates the verification item. This is the hop that made the "+
			"per-pen grain expensive — ten pens meant ten clips of one load sitting in the queue.")
	fx.SeedWorkforce(operator, parkHead, verifier, partitionedShed)

	verificationRepo := verificationpg.NewRepository(fx.Pool, 10*time.Second)
	verificationService := verificationapp.NewService(verificationRepo, nil)
	if err := verificationService.RegisterCategory(verificationdomain.CategoryDefinition{
		Vertical: feeddirectiondomain.VerificationVerticalFeed, Module: feeddirectiondomain.VerificationModuleFeed,
		Category: feeddirectiondomain.VerificationCategoryTransport, ExpectedMedia: []string{"video"},
		MediaLabels:      []string{"Feed transport video"},
		NavigationModule: "feed_direction", NavigationModuleLabel: "Feed",
		PageKey: "feed_transport", PageLabel: "Feed Transport", PageOrder: 3,
	}); err != nil {
		t.Fatalf("register feed transport verification category: %v", err)
	}
	service := feeddirectionapp.NewService(nil, nil).
		WithTransportStore(repo).
		WithTransportVerificationEnqueuer(feeddirectionbridge.NewTransport(verificationService))

	result, err := service.SubmitTransport(ctx, feeddirectionapp.SubmitTransportInput{
		TenantID: fxTenant, TaskID: taskID, ProofRef: "proof-ft-shed-grain",
		OperatorID: operator, IdempotencyKey: "ft-shed-grain-submit",
		ActorID: operator, ActorType: "operator", TraceID: "trace-ft-shed-grain",
	})
	story.Assert("the operator's submit succeeded", err == nil, "err=%v", err)
	story.Assert("the task is awaiting verification",
		result.Status == "verification_due", "status=%s", result.Status)

	queued := fx.countRows(`
SELECT count(*) FROM verification_items
WHERE tenant_id=$1 AND category=$2 AND shed_id=$3`,
		fxTenant, feeddirectiondomain.VerificationCategoryTransport, partitionedShed)
	story.Assert("the verifier receives exactly ONE feed-transport item for the three-pen shed",
		queued == 1, "verification_items=%d (a per-pen grain would queue 3 clips of one trip)", queued)

	itemPartition := fx.scanText(`
SELECT COALESCE(partition_label,'') FROM verification_items
WHERE tenant_id=$1 AND category=$2 AND shed_id=$3`,
		fxTenant, feeddirectiondomain.VerificationCategoryTransport, partitionedShed)
	story.Assert("and that item names no pen either", itemPartition == "", "partition_label=%q", itemPartition)
}
