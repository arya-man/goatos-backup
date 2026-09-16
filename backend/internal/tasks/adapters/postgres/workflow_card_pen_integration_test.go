package postgres

import (
	"testing"

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
