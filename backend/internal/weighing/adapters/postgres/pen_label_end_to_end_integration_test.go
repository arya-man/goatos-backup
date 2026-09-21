package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/weighing/domain"
	"github.com/vgoats/goatos/backend/internal/weighing/ports"
)

// TestPenLabelIsComposedOnceFromCatalogToVerifier follows ONE pen's label along the whole chain
// that produced "Mandela 1 - Part 1 - Part 1" on an operator's weighing schedule (2026-09-21):
//
//	stored bucket row -> task-detail read -> removal card read -> SUBMIT -> the row the submit
//	WRITES -> the verifier's subject line
//
// It is a production-path test on a real database: every hop is the same repository method the
// API calls, and the assertions are on the STRING each hop produces, not on a field being present.
// Field-presence and pure-formatter tests both passed while the real output was wrong, which is
// how this shipped.
//
// The seeded shape is the one the live register actually holds and the one the old code doubled:
// display_name already carries the pen ("Gandhi 1 - Part 1") AND partition_label separately says
// "Part 1". The three rows in the live weighing register that carry a non-empty partition_label
// are exactly this shape.
func TestPenLabelIsComposedOnceFromCatalogToVerifier(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()

	const weighDate = "2026-09-04"
	fastingID := seedFastingFixture(t, ctx, pool, weighDate)
	repo := NewRepository(pool, 5*time.Second)

	// THE DEFECT'S INPUT. Without this the bucket carries a NULL partition_label and the old code
	// looked correct -- which is why the doubling was intermittent and easy to miss.
	execWeighingTestSQL(t, ctx, pool,
		`UPDATE weighing_campaign_sheds SET partition_label='Part 1' WHERE campaign_shed_id=$1::uuid`,
		repoAnimalScope)

	const wantPen = "Gandhi 1 - Part 1"

	// HOP 1 -- the task detail the operator's schedule renders.
	campaign, err := repo.CampaignByID(ctx, repoTenant, repoCampaign, ports.CampaignAccess{Unrestricted: true})
	if err != nil {
		t.Fatalf("get campaign: %v", err)
	}
	var bucket domain.CampaignShed
	for _, s := range campaign.Sheds {
		if s.CampaignShedID == repoAnimalScope {
			bucket = s
		}
	}
	if bucket.CampaignShedID == "" {
		t.Fatal("seeded bucket missing from the task detail")
	}
	if bucket.OperationalLocationDisplay != wantPen {
		t.Errorf("task detail display = %q, want %q", bucket.OperationalLocationDisplay, wantPen)
	}
	// The parts must agree with the whole: a screen that composes from them must land on the same
	// string. This is the pair the phone receives and the pair the old client re-composed.
	if bucket.ParentShedName != "Gandhi 1" || bucket.PartitionLabel != "Part 1" {
		t.Errorf("task detail parts = (%q, %q), want (\"Gandhi 1\", \"Part 1\")", bucket.ParentShedName, bucket.PartitionLabel)
	}

	// HOP 2 -- the paged bucket list, a SEPARATE query. The two must agree; a task screen and its
	// own paged list disagreeing about a pen is the cross-surface defect the weighing rules ban.
	page, err := repo.ListCampaignSheds(ctx, repoTenant, repoCampaign, "", 50, ports.CampaignAccess{Unrestricted: true})
	if err != nil {
		t.Fatalf("list campaign sheds: %v", err)
	}
	var paged domain.CampaignShed
	for _, s := range page.Items {
		if s.CampaignShedID == repoAnimalScope {
			paged = s
		}
	}
	if paged.OperationalLocationDisplay != wantPen {
		t.Errorf("paged bucket display = %q, want %q", paged.OperationalLocationDisplay, wantPen)
	}

	// HOP 3 -- the removal card the operator taps, read through the live-shed path.
	task, err := repo.FastingTaskByID(ctx, repoTenant, fastingID, "")
	if err != nil {
		t.Fatalf("get fasting task: %v", err)
	}
	var card domain.FastingShedProof
	for _, s := range task.Sheds {
		if s.CampaignShedID == repoAnimalScope {
			card = s
		}
	}
	if card.ShedLabel != wantPen {
		t.Errorf("removal card label = %q, want %q", card.ShedLabel, wantPen)
	}

	// HOP 4 -- SUBMIT. This is the hop that WRITES the label down, so a double here is persisted
	// rather than merely rendered; it is what put "Godel 2 - Part 1 - Part 1" in the database.
	cmd := fastingSubmitShedA(fastingID, "pen-label-e2e")
	res, err := repo.SubmitFastingShed(ctx, cmd)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.Card.ShedLabel != wantPen {
		t.Errorf("submit response label = %q, want %q", res.Card.ShedLabel, wantPen)
	}

	// HOP 5 -- the row the submit actually wrote, read straight back from the table.
	var stored string
	if err := pool.QueryRow(ctx,
		`SELECT shed_label FROM weighing_fasting_shed_proofs WHERE tenant_id=$1::uuid AND fasting_task_id=$2::uuid AND campaign_shed_id=$3::uuid`,
		repoTenant, fastingID, repoAnimalScope).Scan(&stored); err != nil {
		t.Fatalf("read stored shed_label: %v", err)
	}
	if stored != wantPen {
		t.Errorf("STORED shed_label = %q, want %q", stored, wantPen)
	}

	// HOP 6 -- the verifier's subject line. The enqueue is an APP-LAYER seam (the service holds an
	// enqueuer registered at composition time), so no verification_items row exists at repository
	// grain; asserting one here would be asserting the fixture, not the code. What the verifier
	// reads is domain.FastingShedSubjectLabel over exactly the ShedLabel hops 4 and 5 just proved,
	// so that is what is checked -- on the value read back FROM THE DATABASE, not a local literal.
	subject := domain.FastingShedSubjectLabel(stored)
	if !strings.HasSuffix(subject, wantPen) {
		t.Errorf("verifier subject = %q, want it to end in %q", subject, wantPen)
	}
	// The pen must appear ONCE in that sentence, COUNTED rather than eyeballed: the defect's
	// signature is the SECOND occurrence, and a HasSuffix check alone still passes on
	// "... Gandhi 1 - Part 1 - Part 1".
	if n := strings.Count(subject, "Part 1"); n != 1 {
		t.Errorf("verifier subject %q names the pen %d times, want exactly 1", subject, n)
	}
}
