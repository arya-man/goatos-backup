package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/pccare/domain"
)

// An animal that has left the herd is not on the pen roster the operator works from.
//
// TaskShedRoster is the list of RFIDs a PC Care task expects in its pen -- what the operator is
// sent to treat. It is a QUERY over the live herd (g.lifecycle_status = 'alive'), not a domain
// event, so nothing about it can be registered as a producer/consumer chain and no consumer test
// can cover it. Before this test TaskShedRoster had NO test anywhere in the repository, so the
// predicate could be deleted in silence.
//
// The failure it guards is not a blank screen: the roster would simply be longer, and the extra
// animals would look like ordinary work. An operator sent to deworm an animal that is dead or
// sold cannot find it, and the pen reads as incomplete for as long as the register remembers
// where that animal last stood.
//
// The fixtures insert only INPUT facts (the herd register's own rows). The task is created
// through the production CreateTask path and the roster comes from the production query.
func TestTaskShedRosterListsTheLiveHerdOnly(t *testing.T) {
	ctx := context.Background()
	repo, pool := setupPCCareDB(t, ctx)
	task := createPCTask(t, ctx, repo, domain.CategoryDeworming, "pc-roster-lifecycle-create")

	// Three animals stand in the pen and are due the treatment.
	live := []string{"RFID-ROSTER-A", "RFID-ROSTER-B", "RFID-ROSTER-C"}
	for i, rfid := range live {
		seedRosterGoat(t, ctx, pool, 10+i, rfid, "alive", "")
	}
	// Three more last stood in that same pen and have left the herd. Their pen is still on record
	// -- an exit does not erase where the animal was -- which is exactly why lifecycle, and not
	// location, has to be what keeps them off the roster.
	seedRosterGoat(t, ctx, pool, 20, "RFID-ROSTER-DEAD", "dead", "died")
	seedRosterGoat(t, ctx, pool, 21, "RFID-ROSTER-SOLD", "sold", "sold")
	seedRosterGoat(t, ctx, pool, 22, "RFID-ROSTER-CULLED", "culled", "culled")

	page, err := repo.TaskShedRoster(ctx, pcTenant, task.TaskID, "", 50)
	if err != nil {
		t.Fatalf("TaskShedRoster: %v", err)
	}

	got := map[string]bool{}
	for _, id := range page.Identifiers {
		got[id] = true
	}
	for _, rfid := range live {
		if !got[rfid] {
			t.Errorf("roster is missing %s, an animal alive in the pen", rfid)
		}
	}
	for _, gone := range []string{"RFID-ROSTER-DEAD", "RFID-ROSTER-SOLD", "RFID-ROSTER-CULLED"} {
		if got[gone] {
			t.Errorf("roster lists %s, an animal that has left the herd -- the operator would be "+
				"sent to treat an animal that is not there", gone)
		}
	}
	if len(page.Identifiers) != len(live) {
		t.Fatalf("roster has %d animals (%v), want exactly the %d alive in the pen",
			len(page.Identifiers), page.Identifiers, len(live))
	}
}

// seedRosterGoat seeds one animal in the task's pen with one active primary RFID, at the given
// lifecycle. A terminal lifecycle carries the coded reason and the exit stamp
// goats_exited_lifecycle_check requires.
func seedRosterGoat(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	n int,
	rfid, lifecycleStatus, exitReason string,
) {
	t.Helper()
	goatID := fmt.Sprintf("9c000000-0000-4000-8000-0000000%05d", 81000+n)

	if _, err := pool.Exec(ctx,
		`INSERT INTO parties (party_id, party_type, display_name, status)
		 VALUES ('9c000000-0000-4000-8000-000000002001'::uuid, 'org', 'Mesha CPT', 'active')
		 ON CONFLICT (party_id) DO NOTHING`); err != nil {
		t.Fatalf("seed party: %v", err)
	}

	var reason any
	if exitReason != "" {
		reason = exitReason
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO goats (
  goat_id, tenant_id, lifecycle_status, species, custodian_party_id, sex,
  current_location_id, park_id, shed_id, management_stage, dob, origin_type,
  exit_reason, exited_at
) VALUES (
  $1::uuid, $2::uuid, $5, 'goat', '9c000000-0000-4000-8000-000000002001'::uuid, 'female',
  $3::uuid, $4::uuid, $3::uuid, 'adult', DATE '2025-01-01', 'birth',
  $6, CASE WHEN $6::text IS NULL THEN NULL ELSE now() END
)`, goatID, pcTenant, pcShedA, pcPark, lifecycleStatus, reason); err != nil {
		t.Fatalf("seed roster goat %s (%s): %v", rfid, lifecycleStatus, err)
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO goat_identifiers (
  tenant_id, goat_id, identifier_type, identifier_value, normalized_value,
  scope_key, is_primary_for_goat, status, valid_from, normalizer_version
) VALUES (
  $1::uuid, $2::uuid, 'animal_identifier_1', $3, lower($3), $3, true, 'active', now(), 'v1'
)`,
		pcTenant, goatID, rfid); err != nil {
		t.Fatalf("seed roster identifier %s: %v", rfid, err)
	}
}
