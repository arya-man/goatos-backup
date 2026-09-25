package boardsource

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
	"github.com/vgoats/goatos/backend/internal/workboard/domain"
	"github.com/vgoats/goatos/backend/internal/workboard/ports"
)

const mpShed = "00000000-0000-4000-8000-00000000a601"

func seedPrepGoat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, i int, park, stage string) {
	t.Helper()
	exec(t, ctx, pool, `
INSERT INTO goats (goat_id, tenant_id, display_id, species, breed, sex, lifecycle_status,
  age_band, custodian_party_id, park_id, shed_id, management_stage)
VALUES ($1::uuid, $2::uuid, $3, 'goat', 'Beetal', 'female', 'alive',
  'kid', '00000000-0000-4000-8000-000000001001'::uuid, $4::uuid, $5::uuid, $6)`,
		fmt.Sprintf("00000000-0000-4000-8000-0000000a%04d", i), mkTenant, fmt.Sprintf("G-8%05d", i), park, mpShed, stage)
}

func seedPrepCompletion(t *testing.T, ctx context.Context, pool *pgxpool.Pool, park, date, status, by string) {
	t.Helper()
	exec(t, ctx, pool, `
INSERT INTO milk_preparation_completions (tenant_id, park_id, preparation_date, feeding_date, status, submitted_by, rework_reason)
VALUES ($1::uuid, $2::uuid, $3::date, $3::date + 1, $4, $5::uuid, CASE WHEN $4 = 'rework' THEN 'Bottle not shown' END)`,
		mkTenant, park, date, status, by)
}

// TestMilkPreparationCard_OneToMany_ParkScope_StatusMatrix_Pagination: milk preparation is its
// own card (maintainer, 2026-09-25), one per park per preparation day, whatever the number of
// kids behind it. Owed from the page's own kid predicate before anyone submits; every
// completion status maps to its board state; a submitted preparation keeps its card even when
// the park has no kid; a park with no kid and no submission has no card; rows and counts agree;
// the owner lens and the keyset hold; the drill is the preparation itself.
func TestMilkPreparationCard_OneToMany_ParkScope_StatusMatrix_Pagination(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx := context.Background()
	pool := pgtest.StartPostgres(t, ctx)
	defer pool.Close()
	seedOrg(t, ctx, pool, mkTenant, mkPark, mkOtherPk, mkMember, mkOperator)
	exec(t, ctx, pool, `
INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status)
VALUES ($1::uuid, $2::uuid, 'shed', 'MP1', 'Castro', 'active') ON CONFLICT (location_id) DO NOTHING`, mpShed, mkTenant)
	// One-to-many: three milk kids and an adult at Coimbatore -> still ONE card.
	seedPrepGoat(t, ctx, pool, 1, mkPark, "K1")
	seedPrepGoat(t, ctx, pool, 2, mkPark, "K2")
	seedPrepGoat(t, ctx, pool, 3, mkPark, "K2")
	seedPrepGoat(t, ctx, pool, 4, mkPark, "F2")
	// A K3 with no weaning clock is never prepared for: Channapatna has no milk kid.
	seedPrepGoat(t, ctx, pool, 5, mkOtherPk, "K3")
	src := NewMilkPreparation(pool, 5*time.Second)
	q := func(park, date, owner string, states ...domain.WorkState) ports.SourceQuery {
		return ports.SourceQuery{TenantID: mkTenant, ParkID: park, BusinessDate: date, OwnerUserID: owner, WorkStates: states, Limit: 50}
	}

	// Owed, nothing submitted: one card, a crew pool, due.
	rows, err := src.ListRows(ctx, q(mkPark, mkDate, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("three kids are one preparation card: %+v", rows)
	}
	owed := rows[0]
	if owed.Title != "Milk preparation · Coimbatore" || owed.ClockLabel != "For feeding on 11/09/2026" || owed.WorkState != domain.WorkStateDue ||
		owed.OwnerState != domain.OwnerStatePool || owed.SourceID != mkPark || owed.Counts.Pending != 1 || owed.Module != domain.ModuleMilk {
		t.Fatalf("owed card %+v", owed)
	}
	// Park scope: Channapatna has no milk kid and no submission -> no card.
	rows, err = src.ListRows(ctx, q(mkOtherPk, mkDate, ""))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a park with no milk kid owes no preparation: %+v", rows)
	}

	// Status matrix, one preparation day each.
	matrix := []struct {
		park, date, status string
		want               domain.WorkState
		attention          int
	}{
		{mkPark, "2026-09-11", "pending_verification", domain.WorkStateVerificationPending, 0},
		{mkPark, "2026-09-12", "completed", domain.WorkStateCompleted, 0},
		// A submitted preparation keeps its card even at a park with no milk kid now.
		{mkOtherPk, "2026-09-11", "rework", domain.WorkStateRejected, 1},
	}
	for _, m := range matrix {
		seedPrepCompletion(t, ctx, pool, m.park, m.date, m.status, mkOperator)
		rows, err := src.ListRows(ctx, q(m.park, m.date, ""))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].WorkState != m.want || rows[0].Counts.NeedsAttention != m.attention ||
			rows[0].Owner.Name != "Dinakar" || rows[0].OwnerState != domain.OwnerStateAssigned {
			t.Fatalf("%s: %+v", m.status, rows)
		}
		counts, err := src.CountByState(ctx, q(m.park, m.date, ""))
		if err != nil {
			t.Fatal(err)
		}
		if len(counts) != 1 || counts[m.want] != 1 {
			t.Fatalf("%s: counts %v disagree with the row", m.status, counts)
		}
		filtered, err := src.ListRows(ctx, q(m.park, m.date, "", m.want))
		if err != nil || len(filtered) != 1 {
			t.Fatalf("%s: the state filter must find it: %+v %v", m.status, filtered, err)
		}
		other, err := src.ListRows(ctx, q(m.park, m.date, "", domain.WorkStateDue))
		if err != nil || len(other) != 0 {
			t.Fatalf("%s: a different state filter must not: %+v %v", m.status, other, err)
		}
	}

	// Owner lens: someone else's submitted preparation is not in my lens; an unclaimed one is.
	rows, err = src.ListRows(ctx, q(mkPark, "2026-09-11", mkOtherOp))
	if err != nil || len(rows) != 0 {
		t.Fatalf("another operator's preparation: %+v %v", rows, err)
	}
	rows, err = src.ListRows(ctx, q(mkPark, mkDate, mkOtherOp))
	if err != nil || len(rows) != 1 {
		t.Fatalf("an unclaimed preparation is anyone's: %+v %v", rows, err)
	}

	// Keyset: past the one card there is nothing.
	after := q(mkPark, mkDate, "")
	after.AfterSourceID = mkPark
	rows, err = src.ListRows(ctx, after)
	if err != nil || len(rows) != 0 {
		t.Fatalf("keyset after the card: %+v %v", rows, err)
	}

	// Drill: the preparation itself, prepare -> verify.
	sq := func(park, date, id string) ports.SubtaskQuery {
		return ports.SubtaskQuery{TenantID: mkTenant, ParkID: park, BusinessDate: date, SourceID: id, Limit: 10}
	}
	page, err := src.ListSubtasks(ctx, sq(mkPark, mkDate, mkPark))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Subtasks[0].Name != "Milk preparation" || page.Subtasks[0].Subtitle != "For feeding on 11/09/2026" ||
		states(page.Subtasks[0].Steps) != "todo locked " {
		t.Fatalf("owed drill %+v", page)
	}
	page, err = src.ListSubtasks(ctx, sq(mkOtherPk, "2026-09-11", mkOtherPk))
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || !page.Subtasks[0].NeedsAttention || states(page.Subtasks[0].Steps) != "done rework " || page.Subtasks[0].Steps[1].Detail != "Bottle not shown" {
		t.Fatalf("sent-back drill %+v", page)
	}
	page, err = src.ListSubtasks(ctx, sq(mkPark, mkDate, mkOtherPk))
	if err != nil || page.Total != 0 {
		t.Fatalf("a drill naming another park is not this card: %+v %v", page, err)
	}
}
