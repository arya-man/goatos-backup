package postgres

import (
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
	"github.com/vgoats/goatos/backend/internal/tasks/domain"
	"github.com/vgoats/goatos/backend/internal/tasks/ports"
)

// TestWorkflowCardShowsTheAnimalsPenWithItsPartition pins the operational-location rule on the
// birth/death card: a kid in shed "Castro" partition "1" reads "Castro 1" on the list and the detail
// header, never the bare building name (found on the Realme, 2026-09-17: "Born … · Castro · Beetal").
// A goat whose partition row points at ANOTHER shed must not borrow that label.
func TestWorkflowCardShowsTheAnimalsPenWithItsPartition(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const (
		park  = "aaaaaaa1-0000-0000-0000-00000000aa01"
		shed  = "aaaaaaa1-0000-0000-0000-00000000aa02"
		other = "aaaaaaa1-0000-0000-0000-00000000aa03"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'WF-PARK', 'Channapatna', 'active'),
       ($3::uuid, $1::uuid, 'shed', 'WF-CASTRO', 'Castro', 'active'),
       ($4::uuid, $1::uuid, 'shed', 'WF-GANDHI', 'Gandhi', 'active')
ON CONFLICT (location_id) DO NOTHING`, wfTenant, park, shed, other); err != nil {
		t.Fatalf("seed locations: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET shed_id = $2::uuid WHERE tenant_id = $1::uuid AND goat_id IN ($3::uuid, $4::uuid)`,
		wfTenant, shed, wfKid, wfDam); err != nil {
		t.Fatalf("place goats: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $3::uuid, '1', 'seed'),
       ($1::uuid, $4::uuid, $5::uuid, '2', 'seed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		wfTenant, wfKid, shed, wfDam, other); err != nil {
		t.Fatalf("seed partitions: %v", err)
	}
	parkID, shedID := park, shed
	for _, subject := range []struct{ template, goat string }{{domain.TemplateKeyBirthKid, wfKid}, {domain.TemplateKeyBirthMother, wfDam}} {
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
			TenantID: wfTenant, TemplateKey: subject.template, SubjectGoatID: subject.goat, EventAt: wfEventAt,
			ParkID: &parkID, ShedID: &shedID,
		}); err != nil {
			t.Fatalf("open %s: %v", subject.template, err)
		}
	}
	page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{
		TenantID: wfTenant, Module: domain.ModuleBirth, EventDate: biztime.BusinessDate(wfEventAt),
		Filter: domain.FilterAll, PageSize: 20, Now: wfEventAt,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	labels := map[string]string{}
	for _, card := range page.Items {
		labels[card.TemplateKey] = card.ShedLabel
	}
	if labels[domain.TemplateKeyBirthKid] != "Castro 1" {
		t.Fatalf("kid card pen = %q, want %q", labels[domain.TemplateKeyBirthKid], "Castro 1")
	}
	if labels[domain.TemplateKeyBirthMother] != "Castro" {
		t.Fatalf("mother card pen = %q, want the bare shed (her partition row is for another shed)", labels[domain.TemplateKeyBirthMother])
	}
	kidID := findWorkflowID(t, repo, ctx, domain.TemplateKeyBirthKid, wfKid)
	detail, err := repo.GetWorkflow(ctx, wfTenant, kidID, wfEventAt)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if detail.Card.ShedLabel != "Castro 1" {
		t.Fatalf("detail header pen = %q, want %q", detail.Card.ShedLabel, "Castro 1")
	}
}

// TestWorkflowCardPenOneToManyPaginationScheduledDateParkScopeStatusBuckets is the aggregate proof
// for the partition join on the card read: goat_shed_partitions is keyed (tenant_id, goat_id), so
// the join is 1:{0,1} per card and can never multiply a card or a chip.
//   - one-to-many: a partitioned kid AND a partitioned mother on the same day stay two cards and
//     chips.All == 2, whatever partition rows exist;
//   - pagination: a page size of 1 visits each card exactly once, each with its own pen;
//   - scheduled date: a card on ANOTHER business date is not counted on this date;
//   - scope: a partition row that belongs to ANOTHER shed never labels this card's pen (the goat
//     table cannot hold another tenant's row for this goat: goat_shed_partitions carries its FK);
//   - status buckets: Due + Overdue + Completed chips still sum to All with the join present.
func TestWorkflowCardPenOneToManyPaginationScheduledDateParkScopeStatusBuckets(t *testing.T) {
	repo, pool, ctx := newWorkflowRepo(t)
	const (
		park      = "aaaaaaa1-0000-0000-0000-00000000ab01"
		shed      = "aaaaaaa1-0000-0000-0000-00000000ab02"
		otherShed = "aaaaaaa1-0000-0000-0000-00000000ab03"
	)
	if _, err := pool.Exec(ctx, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($2::uuid, $1::uuid, 'park', 'WF-PARK-B', 'Channapatna', 'active'),
       ($3::uuid, $1::uuid, 'shed', 'WF-GODEL', 'Godel 1', 'active'),
       ($4::uuid, $1::uuid, 'shed', 'WF-MANDELA', 'Mandela 1', 'active')
ON CONFLICT (location_id) DO NOTHING`, wfTenant, park, shed, otherShed); err != nil {
		t.Fatalf("seed locations: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE goats SET shed_id = $2::uuid WHERE tenant_id = $1::uuid AND goat_id IN ($3::uuid, $4::uuid, $5::uuid)`,
		wfTenant, shed, wfKid, wfDam, wfDead); err != nil {
		t.Fatalf("place goats: %v", err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, partition_label, source_shed_name)
VALUES ($1::uuid, $2::uuid, $4::uuid, 'Part 3', 'seed'),
       ($1::uuid, $3::uuid, $5::uuid, 'Part 7', 'seed')
ON CONFLICT (tenant_id, goat_id) DO UPDATE SET shed_id = EXCLUDED.shed_id, partition_label = EXCLUDED.partition_label`,
		wfTenant, wfKid, wfDam, shed, otherShed); err != nil {
		t.Fatalf("seed partitions: %v", err)
	}
	parkID, shedID := park, shed
	open := func(template, goat string, at time.Time) {
		t.Helper()
		if _, err := repo.OpenWorkflow(ctx, ports.OpenWorkflowCommand{
			TenantID: wfTenant, TemplateKey: template, SubjectGoatID: goat, EventAt: at, ParkID: &parkID, ShedID: &shedID,
		}); err != nil {
			t.Fatalf("open %s: %v", template, err)
		}
	}
	open(domain.TemplateKeyBirthKid, wfKid, wfEventAt)
	open(domain.TemplateKeyBirthMother, wfDam, wfEventAt)
	open(domain.TemplateKeyBirthMother, wfDead, wfEventAt.AddDate(0, 0, 1)) // scheduled date: another day

	seen := map[string]string{}
	cursor := ""
	for i := 0; i < 5; i++ {
		decoded, err := domain.DecodeWorkflowCursor(cursor)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		page, err := repo.ListWorkflows(ctx, domain.WorkflowListQuery{
			TenantID: wfTenant, Module: domain.ModuleBirth, EventDate: biztime.BusinessDate(wfEventAt),
			Filter: domain.FilterAll, PageSize: 1, Cursor: decoded, Now: wfEventAt,
		})
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		if page.Chips.All != 2 {
			t.Fatalf("chips.All = %d on page %d, want 2 (join must not fan out, other date excluded)", page.Chips.All, i)
		}
		if page.Chips.Due+page.Chips.Overdue+page.Chips.Completed+page.Chips.AwaitingVideo != page.Chips.All {
			t.Fatalf("status buckets %+v do not cover All", page.Chips)
		}
		for _, card := range page.Items {
			if _, dup := seen[card.WorkflowID]; dup {
				t.Fatalf("card %s served twice", card.WorkflowID)
			}
			seen[card.WorkflowID] = card.TemplateKey + "=" + card.ShedLabel
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	want := map[string]bool{
		domain.TemplateKeyBirthKid + "=Godel 1 - Part 3": true,
		domain.TemplateKeyBirthMother + "=Godel 1":       true, // 'whole' and the other tenant's row never label
	}
	if len(seen) != 2 {
		t.Fatalf("visited %d cards, want 2: %v", len(seen), seen)
	}
	for _, got := range seen {
		if !want[got] {
			t.Fatalf("card pen %q not expected; seen %v", got, seen)
		}
	}
}
