package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

const (
	cfgTenant  = "0c0c0c0c-0000-4000-8000-000000000001"
	cfgParkCBE = "0c0c0c0c-0000-4000-8000-000000000010"
	cfgShed    = "0c0c0c0c-0000-4000-8000-000000000020"
	cfgAlias   = "0c0c0c0c-0000-4000-8000-000000000021"
	cfgGoat    = "0c0c0c0c-0000-4000-8000-000000000030"
	cfgActor   = "0c0c0c0c-0000-4000-8000-000000000099"
)

// seedConfigurationFixture: one tenant (so migration 000346's per-tenant built-ins exist for
// it), one park, one partitioned shed ("Castro" with partition 1) plus the legacy "Castro 1"
// alias row the farm still carries, and one live goat in Castro 1.
func seedConfigurationFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed: %v\n%s", err, sql)
		}
	}
	exec(`INSERT INTO tenants (tenant_id, name, status) VALUES ($1::uuid, 'Configuration Test', 'active') ON CONFLICT DO NOTHING`, cfgTenant)
	// The built-ins are written by the migration for tenants that existed then; a tenant created
	// afterwards gets them the same way a seed would. (The FK to tenants is why this is here.)
	exec(`INSERT INTO species_lookup (tenant_id, species_code, name, sort_order, is_builtin) VALUES ($1::uuid,'goat','Goat',10,true), ($1::uuid,'sheep','Sheep',20,true) ON CONFLICT DO NOTHING`, cfgTenant)
	exec(`INSERT INTO sex_lookup (tenant_id, sex_code, name, sort_order, is_builtin) VALUES ($1::uuid,'female','Female',10,true), ($1::uuid,'male','Male',20,true) ON CONFLICT DO NOTHING`, cfgTenant)
	// The tenant is created here, AFTER the migrations ran, so migration 000348's per-tenant seed
	// of the reference lists never covered it -- exactly as it never covered species or gender,
	// which this fixture already writes by hand. Without this the exit-reasons register has no
	// list to hold an entry and the lifecycle test fails on an unknown record; it was red on main
	// for that reason and nothing to do with the register under test.
	exec(`INSERT INTO reference_lists (tenant_id, list_key, name, description, sort_order, is_builtin)
VALUES ($1::uuid, 'exit_reasons', 'Exit reasons', 'Why an animal leaves the herd register.', 10, true) ON CONFLICT DO NOTHING`, cfgTenant)
	exec(`INSERT INTO reference_list_entries (tenant_id, list_key, entry_code, name, sort_order, is_builtin)
VALUES ($1::uuid,'exit_reasons','sold','Sold',10,true), ($1::uuid,'exit_reasons','died','Died',20,true),
       ($1::uuid,'exit_reasons','culled','Culled',30,true), ($1::uuid,'exit_reasons','transferred','Transferred',40,true),
       ($1::uuid,'exit_reasons','lost','Lost',50,true) ON CONFLICT DO NOTHING`, cfgTenant)
	exec(`INSERT INTO item_categories (tenant_id, name, normalized_name, item_kind, sort_order, is_builtin)
VALUES ($1::uuid,'Medicines','medicines','medicine',10,true), ($1::uuid,'Vaccines','vaccines','vaccine',20,true), ($1::uuid,'Feed','feed','feed',40,true) ON CONFLICT DO NOTHING`, cfgTenant)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status) VALUES ($2::uuid, $1::uuid, 'park', 'CBE', 'Coimbatore', 'active') ON CONFLICT DO NOTHING`, cfgTenant, cfgParkCBE)
	exec(`INSERT INTO park_profiles (location_id, tenant_id, park_code) VALUES ($2::uuid, $1::uuid, 'CBE') ON CONFLICT DO NOTHING`, cfgTenant, cfgParkCBE)
	exec(`INSERT INTO locations (location_id, tenant_id, location_type, location_code, name, status, parent_location_id)
VALUES ($2::uuid, $1::uuid, 'shed', 'CBE_SHED_CASTRO', 'Castro', 'active', $4::uuid),
       ($3::uuid, $1::uuid, 'shed', 'CBE_SHED_CASTRO_1', 'Castro 1', 'active', $4::uuid) ON CONFLICT DO NOTHING`, cfgTenant, cfgShed, cfgAlias, cfgParkCBE)
	exec(`INSERT INTO shed_partitions (tenant_id, shed_id, partition_label, normalized_label, source, display_order) VALUES ($1::uuid, $2::uuid, '1', '1', 'manual', 1) ON CONFLICT DO NOTHING`, cfgTenant, cfgShed)
	exec(`INSERT INTO parties (party_id, party_type, display_name, status) VALUES ($1::uuid, 'org', 'Configuration Custodian', 'active') ON CONFLICT DO NOTHING`, cfgTenant)
	exec(`INSERT INTO goats (goat_id, tenant_id, species, breed, sex, lifecycle_status, age_band, custodian_party_id, park_id, shed_id, management_stage)
VALUES ($1::uuid, $2::uuid, 'goat', 'Boer', 'female', 'alive', 'adult', $2::uuid, $3::uuid, $4::uuid, 'F2-Female')`, cfgGoat, cfgTenant, cfgParkCBE, cfgShed)
	exec(`INSERT INTO goat_shed_partitions (tenant_id, goat_id, shed_id, source_shed_name, partition_label) VALUES ($1::uuid, $2::uuid, $3::uuid, 'seed', '1')`, cfgTenant, cfgGoat, cfgShed)
	exec(`INSERT INTO animal_stage_lookup (tenant_id, stage_code, name, sort_order) VALUES ($1::uuid, 'F2-Female', 'Fattening female', 10) ON CONFLICT DO NOTHING`, cfgTenant)
}

// TestConfigurationRegistersLifecyclePostgresPaths drives every register write path through the
// real repository: create / update / archive / delete with the row_version fence, the usage
// checks that refuse an archive or delete while animals, partitions or items still name a row,
// the pen alias exclusion, the partition display composed through oploc, the category tree's
// kind inheritance, the item kind-specific facts mirrored onto vaccines, the idempotent replay
// and the same-key different-payload refusal, and the built-in rows that never leave.
func TestConfigurationRegistersLifecyclePostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	repo := NewRepository(pool, 15*time.Second)
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	list := func(register string, p ports.ListParams) ports.Page {
		t.Helper()
		if p.Limit == 0 {
			p.Limit = 50
		}
		page, err := repo.List(ctx, cfgTenant, register, p)
		if err != nil {
			t.Fatalf("list %s: %v", register, err)
		}
		return page
	}

	// --- counts: the rail; the legacy "Castro 1" alias row is NOT a pen ---
	counts, err := repo.Counts(ctx, cfgTenant)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if counts[domain.RegParks] != 1 || counts[domain.RegPens] != 1 || counts[domain.RegPartitions] != 1 || counts[domain.RegSpecies] != 2 || counts[domain.RegSexes] != 2 || counts[domain.RegCategories] != 3 {
		t.Fatalf("counts = %v", counts)
	}

	// --- pens: the alias row is excluded, the park label and the holds are composed ---
	pens := list(domain.RegPens, ports.ListParams{})
	if len(pens.Rows) != 1 || pens.Rows[0].Display != "Castro" || pens.Rows[0].Labels["park_id"] != "Coimbatore" || pens.Rows[0].Counts["animals"] != 1 || pens.Rows[0].Counts["partitions"] != 1 || pens.Total != 1 {
		t.Fatalf("pens = %+v", pens)
	}
	// --- partitions: display through oploc, never SQL ---
	parts := list(domain.RegPartitions, ports.ListParams{Filters: map[string]string{"park_id": cfgParkCBE}})
	if len(parts.Rows) != 1 || parts.Rows[0].Display != "Castro 1" || parts.Rows[0].ID != cfgShed+":1" || parts.Rows[0].Counts["animals"] != 1 {
		t.Fatalf("partitions = %+v", parts.Rows)
	}
	if _, has := parts.Rows[0].Fields["shed_name"]; has {
		t.Fatalf("shed_name is a composition input, never a field on the wire")
	}

	// --- create a pen under the park; its code follows the park's; a duplicate name is refused ---
	pen, err := repo.Create(ctx, write("pen-1"), domain.RegPens, map[string]any{"park_id": cfgParkCBE, "name": "Gandhi", "capacity": int64(120)})
	if err != nil {
		t.Fatalf("create pen: %v", err)
	}
	if pen.Display != "Gandhi" || pen.Fields["capacity"] != float64(120) || pen.RowVersion != 1 {
		t.Fatalf("pen = %+v", pen)
	}
	var code string
	if err := pool.QueryRow(ctx, `SELECT location_code FROM locations WHERE location_id = $1::uuid`, pen.ID).Scan(&code); err != nil || code != "CBE_SHED_GANDHI" {
		t.Fatalf("pen code = %q %v", code, err)
	}
	// Exact replay returns the same row without a second insert; a different payload is refused.
	again, err := repo.Create(ctx, write("pen-1"), domain.RegPens, map[string]any{"park_id": cfgParkCBE, "name": "Gandhi", "capacity": int64(120)})
	if err != nil || again.ID != pen.ID {
		t.Fatalf("replay: %v %+v", err, again)
	}
	if _, err := repo.Create(ctx, write("pen-1"), domain.RegPens, map[string]any{"park_id": cfgParkCBE, "name": "Other"}); !errors.Is(err, ports.ErrIdempotencyConflict) {
		t.Fatalf("same key different payload: %v", err)
	}
	var dup *ports.DuplicateError
	if _, err := repo.Create(ctx, write("pen-dup"), domain.RegPens, map[string]any{"park_id": cfgParkCBE, "name": "Gandhi"}); !errors.As(err, &dup) || dup.Field != "name" {
		t.Fatalf("duplicate pen name: %v", err)
	}
	// A ref to a park that does not exist names its field.
	var ref *ports.RefError
	if _, err := repo.Create(ctx, write("pen-badpark"), domain.RegPens, map[string]any{"park_id": cfgGoat, "name": "Nowhere"}); !errors.As(err, &ref) || ref.Field != "park_id" {
		t.Fatalf("unknown park: %v", err)
	}

	// --- update, fenced on row_version ---
	if _, err := repo.Update(ctx, write("pen-up-stale"), domain.RegPens, pen.ID, map[string]any{"capacity": int64(90)}, 7); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale fence: %v", err)
	}
	pen2, err := repo.Update(ctx, write("pen-up"), domain.RegPens, pen.ID, map[string]any{"name": "Gandhi North", "capacity": int64(90), "notes": nil}, pen.RowVersion)
	if err != nil || pen2.Display != "Gandhi North" || pen2.Fields["capacity"] != float64(90) || pen2.RowVersion != 2 {
		t.Fatalf("update pen: %v %+v", err, pen2)
	}
	if err := pool.QueryRow(ctx, `SELECT location_code FROM locations WHERE location_id = $1::uuid`, pen.ID).Scan(&code); err != nil || code != "CBE_SHED_GANDHI_NORTH" {
		t.Fatalf("pen code after rename = %q", code)
	}

	// --- partitions on the new pen: add, rename within the same key, refuse a duplicate ---
	part, err := repo.Create(ctx, write("part-1"), domain.RegPartitions, map[string]any{"park_id": cfgParkCBE, "pen_id": pen.ID, "label": "Part 3", "sort_order": int64(3)})
	if err != nil || part.Display != "Gandhi North - Part 3" || part.ID != pen.ID+":3" {
		t.Fatalf("create partition: %v %+v", err, part)
	}
	if _, err := repo.Create(ctx, write("part-dup"), domain.RegPartitions, map[string]any{"park_id": cfgParkCBE, "pen_id": pen.ID, "label": "3"}); !errors.As(err, &dup) || dup.Field != "label" {
		t.Fatalf("'3' and 'Part 3' are one partition: %v", err)
	}
	if _, err := repo.Update(ctx, write("part-up"), domain.RegPartitions, part.ID, map[string]any{"label": "Part 4"}, 0); err != nil {
		t.Fatalf("rename empty partition: %v", err)
	}
	if got, err := repo.Get(ctx, cfgTenant, domain.RegPartitions, pen.ID+":4"); err != nil || got.Display != "Gandhi North - Part 4" {
		t.Fatalf("renamed partition: %v %+v", err, got)
	}
	// The occupied partition cannot be renamed past its key, archived, or deleted.
	var inUse *ports.InUseError
	if _, err := repo.Update(ctx, write("part-occ"), domain.RegPartitions, cfgShed+":1", map[string]any{"label": "2"}, 0); !errors.As(err, &inUse) {
		t.Fatalf("renaming an occupied partition: %v", err)
	}
	if _, err := repo.SetStatus(ctx, write("part-arch"), domain.RegPartitions, cfgShed+":1", domain.StatusArchived, 0); !errors.As(err, &inUse) {
		t.Fatalf("archiving an occupied partition: %v", err)
	}
	if err := repo.Delete(ctx, write("part-del"), domain.RegPartitions, cfgShed+":1", 0); !errors.As(err, &inUse) || inUse.Usage.Sentence() != "In use by 1 animals" {
		t.Fatalf("deleting an occupied partition: %v", err)
	}
	usage, err := repo.Usage(ctx, cfgTenant, domain.RegPens, cfgShed)
	if err != nil || !usage.Blocked || usage.Sentence() != "In use by 1 animals, 1 partitions" {
		t.Fatalf("pen usage: %v %+v", err, usage)
	}
	// The empty pen can be archived and restored; the occupied one cannot be archived.
	if _, err := repo.SetStatus(ctx, write("pen-arch"), domain.RegPens, cfgShed, domain.StatusArchived, 0); !errors.As(err, &inUse) {
		t.Fatalf("archiving the occupied pen: %v", err)
	}
	// The new pen holds a partition now, so it is archive-blocked too until the partition goes.
	if err := repo.Delete(ctx, write("part-del-2"), domain.RegPartitions, pen.ID+":4", 0); err != nil {
		t.Fatalf("delete empty partition: %v", err)
	}
	archived, err := repo.SetStatus(ctx, write("pen-arch-2"), domain.RegPens, pen.ID, domain.StatusArchived, 0)
	if err != nil || archived.Status != domain.StatusArchived {
		t.Fatalf("archive pen: %v %+v", err, archived)
	}
	if got := list(domain.RegPens, ports.ListParams{}); len(got.Rows) != 1 {
		t.Fatalf("an archived pen leaves the active list: %+v", got.Rows)
	}
	if got := list(domain.RegPens, ports.ListParams{Status: "archived"}); len(got.Rows) != 1 || got.Rows[0].ID != pen.ID {
		t.Fatalf("archived list: %+v", got.Rows)
	}
	restored, err := repo.SetStatus(ctx, write("pen-rest"), domain.RegPens, pen.ID, domain.StatusActive, 0)
	if err != nil || restored.Status != domain.StatusActive {
		t.Fatalf("restore pen: %v", err)
	}
	if err := repo.Delete(ctx, write("pen-del"), domain.RegPens, pen.ID, 0); err != nil {
		t.Fatalf("delete empty pen: %v", err)
	}
	if _, err := repo.Get(ctx, cfgTenant, domain.RegPens, pen.ID); !errors.Is(err, ports.ErrNotFound) {
		t.Fatalf("deleted pen must be gone: %v", err)
	}

	// --- species: a new one is accepted, renamed, and removed; a built-in never leaves ---
	cow, err := repo.Create(ctx, write("sp-cow"), domain.RegSpecies, map[string]any{"name": "Cow", "code": "cow", "sort_order": int64(30)})
	if err != nil || cow.ID != "cow" || cow.IsBuiltin {
		t.Fatalf("create species: %v %+v", err, cow)
	}
	if _, err := repo.Create(ctx, write("sp-cow-2"), domain.RegSpecies, map[string]any{"name": "cow", "code": "cattle"}); !errors.As(err, &dup) {
		t.Fatalf("a species name is unique regardless of case: %v", err)
	}
	if _, err := repo.Update(ctx, write("sp-goat"), domain.RegSpecies, "goat", map[string]any{"name": "Goats"}, 1); err != nil {
		t.Fatalf("renaming a built-in is allowed: %v", err)
	}
	if err := repo.Delete(ctx, write("sp-goat-del"), domain.RegSpecies, "goat", 0); !errors.As(err, &inUse) {
		t.Fatalf("goat is in use by an animal: %v", err)
	}
	if err := repo.Delete(ctx, write("sp-sheep-del"), domain.RegSpecies, "sheep", 0); !errors.Is(err, ports.ErrVersionConflict) && !errors.Is(err, ports.ErrNotFound) {
		// The store refuses the row-level delete of a built-in (NOT is_builtin); the service
		// refuses earlier with ErrBuiltin. Either way sheep stays.
		t.Fatalf("a built-in must not be deletable at the store: %v", err)
	}
	if err := repo.Delete(ctx, write("sp-cow-del"), domain.RegSpecies, "cow", 0); err != nil {
		t.Fatalf("delete unused species: %v", err)
	}
	species := list(domain.RegSpecies, ports.ListParams{})
	if len(species.Rows) != 2 || species.Rows[0].Display != "Goats" || !species.Rows[0].IsBuiltin || species.Rows[0].Counts["animals"] != 1 {
		t.Fatalf("species = %+v", species.Rows)
	}

	// --- categories: a child inherits its root's kind; the tree cannot loop; items follow ---
	var medicines string
	if err := pool.QueryRow(ctx, `SELECT category_id::text FROM item_categories WHERE tenant_id = $1::uuid AND normalized_name = 'medicines'`, cfgTenant).Scan(&medicines); err != nil {
		t.Fatal(err)
	}
	antibiotics, err := repo.Create(ctx, write("cat-ab"), domain.RegCategories, map[string]any{"name": "Antibiotics", "parent_id": medicines})
	if err != nil || antibiotics.Display != "Medicines › Antibiotics" || antibiotics.Fields["kind"] != "medicine" || antibiotics.Labels["parent_id"] != "Medicines" {
		t.Fatalf("child category: %v %+v", err, antibiotics)
	}
	var vErr *domain.ValidationError
	if _, err := repo.Create(ctx, write("cat-root-nokind"), domain.RegCategories, map[string]any{"name": "Loose"}); !errors.As(err, &vErr) {
		t.Fatalf("a root needs a kind: %v", err)
	}
	if _, err := repo.Update(ctx, write("cat-loop"), domain.RegCategories, medicines, map[string]any{"parent_id": antibiotics.ID}, 0); !errors.As(err, &vErr) {
		t.Fatalf("moving a root under its own child must be refused: %v", err)
	}
	opts, err := repo.Options(ctx, cfgTenant, domain.RegCategories)
	if err != nil || len(opts) != 4 {
		t.Fatalf("category options: %v %+v", err, opts)
	}
	for _, o := range opts {
		if o.ID == antibiotics.ID && (o.Kind != "medicine" || o.ParentID != medicines) {
			t.Fatalf("option carries kind and parent: %+v", o)
		}
	}

	// --- items: a medicine under Antibiotics with its facts in context; a vaccine mirrors onto vaccines ---
	oxy, err := repo.Create(ctx, write("item-oxy"), domain.RegItems, map[string]any{"name": "Oxytetracycline", "category_id": antibiotics.ID, "unit": "ml", "route": "im", "strength": "10 mg/ml", "withdrawal_days": int64(14)})
	if err != nil || oxy.Fields["kind"] != "medicine" || oxy.Fields["route"] != "im" || oxy.Fields["withdrawal_days"] != float64(14) || oxy.Fields["code"] != "ITM-OXYTETRACYCLINE" || oxy.Labels["category_id"] != "Medicines › Antibiotics" {
		t.Fatalf("create medicine: %v %+v", err, oxy)
	}
	var vaccines string
	if err := pool.QueryRow(ctx, `SELECT category_id::text FROM item_categories WHERE tenant_id = $1::uuid AND normalized_name = 'vaccines'`, cfgTenant).Scan(&vaccines); err != nil {
		t.Fatal(err)
	}
	ppr, err := repo.Create(ctx, write("item-ppr"), domain.RegItems, map[string]any{"name": "PPR vaccine", "category_id": vaccines, "unit": "dose", "disease": "PPR", "doses_per_vial": int64(100), "vaccine_type": "live", "code": "VAC-PPR-2"})
	if err != nil || ppr.Fields["kind"] != "vaccine" {
		t.Fatalf("create vaccine: %v %+v", err, ppr)
	}
	var disease string
	var doses int
	if err := pool.QueryRow(ctx, `SELECT disease, doses_per_vial FROM vaccines WHERE tenant_id = $1::uuid AND item_id = $2::uuid`, cfgTenant, ppr.ID).Scan(&disease, &doses); err != nil || disease != "PPR" || doses != 100 {
		t.Fatalf("vaccine detail row: %v %q %d", err, disease, doses)
	}
	// The category now holds an item: it cannot be archived, and the item's kind cannot change
	// through a category of another kind while stock exists (none here, so the move is allowed).
	if _, err := repo.SetStatus(ctx, write("cat-ab-arch"), domain.RegCategories, antibiotics.ID, domain.StatusArchived, 0); !errors.As(err, &inUse) {
		t.Fatalf("archiving a category with items: %v", err)
	}
	moved, err := repo.Update(ctx, write("item-oxy-move"), domain.RegItems, oxy.ID, map[string]any{"category_id": medicines, "route": nil}, oxy.RowVersion)
	if err != nil || moved.Labels["category_id"] != "Medicines" {
		t.Fatalf("move item: %v %+v", err, moved)
	}
	if _, has := moved.Fields["route"]; has {
		t.Fatalf("a nil field clears the context key: %+v", moved.Fields)
	}
	items := list(domain.RegItems, ports.ListParams{Filters: map[string]string{"category_id": medicines}, Query: "oxy"})
	if len(items.Rows) != 1 || items.Rows[0].ID != oxy.ID || items.Total != 1 {
		t.Fatalf("items filtered + searched: %+v", items)
	}
	if err := repo.Delete(ctx, write("item-oxy-del"), domain.RegItems, oxy.ID, 0); err != nil {
		t.Fatalf("delete item: %v", err)
	}
	if err := repo.Delete(ctx, write("cat-ab-del"), domain.RegCategories, antibiotics.ID, 0); err != nil {
		t.Fatalf("delete empty category: %v", err)
	}

	// --- reference-list entries are keyed stores too: delete must honor the row_version fence ---
	entry, err := repo.Create(ctx, write("exit-extra"), domain.RefPrefix+"exit_reasons", map[string]any{"name": "Research Transfer", "code": "research_transfer"})
	if err != nil || entry.ID != "research_transfer" || entry.RowVersion != 1 {
		t.Fatalf("create reference-list entry: %v %+v", err, entry)
	}
	edited, err := repo.Update(ctx, write("exit-extra-up"), domain.RefPrefix+"exit_reasons", entry.ID, map[string]any{"description": "Moved for a trial"}, entry.RowVersion)
	if err != nil || edited.RowVersion != 2 {
		t.Fatalf("update reference-list entry: %v %+v", err, edited)
	}
	if err := repo.Delete(ctx, write("exit-extra-stale-del"), domain.RefPrefix+"exit_reasons", entry.ID, entry.RowVersion); !errors.Is(err, ports.ErrVersionConflict) {
		t.Fatalf("stale reference-list delete must conflict: %v", err)
	}
	if err := repo.Delete(ctx, write("exit-extra-del"), domain.RefPrefix+"exit_reasons", entry.ID, edited.RowVersion); err != nil {
		t.Fatalf("delete reference-list entry: %v", err)
	}

	// --- keyset paging over species (2 rows, page of 1) ---
	page1 := list(domain.RegSpecies, ports.ListParams{Limit: 1})
	if len(page1.Rows) != 1 || page1.NextCursor == "" || page1.Total != 2 {
		t.Fatalf("page 1 = %+v", page1)
	}
	page2 := list(domain.RegSpecies, ports.ListParams{Limit: 1, Cursor: page1.NextCursor})
	if len(page2.Rows) != 1 || page2.NextCursor != "" || page2.Rows[0].ID == page1.Rows[0].ID {
		t.Fatalf("page 2 = %+v", page2)
	}

	// --- every write left an audit row ---
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1::uuid AND action LIKE 'configuration.%'`, cfgTenant).Scan(&audits); err != nil || audits < 15 {
		t.Fatalf("audit rows = %d %v", audits, err)
	}
}
