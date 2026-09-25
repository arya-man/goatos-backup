package postgres

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/app"
	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// PEN TYPE IS SET ON THE PARTITION -- THE PEN THE FARM ACTUALLY WORKS -- AND NOWHERE ELSE
// (maintainer instructions 2026-09-22: configured, then "assignment will be per partition only not
// pen"), and the KINDS of pen are the farm's own Pen types register (2026-09-25, migration 000437).
//
// The things this pins, in the order they would break:
//
//  1. a pen type is authored on the register, and a partition takes it -- including a THIRD kind
//     the farm adds, which is the whole point of the register;
//  2. it round-trips: what the screen writes is what the reports read, and the partition list
//     shows the type's NAME, so a rename shows at once;
//  3. CLEARING it writes NULL rather than leaving the last answer standing;
//  4. a code nobody authored, or one the farm archived, is refused on the Pen type field;
//  5. a type still given to a pen cannot be removed (archive it instead).
func TestPenTypeIsConfiguredOnThePartitionFromThePenTypesRegister(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)

	repo := NewRepository(pool, 15*time.Second)
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}

	for _, pt := range []map[string]any{
		{"name": "Elevated", "code": "elevated", "sort_order": 10},
		{"name": "Non-elevated", "code": "non_elevated", "sort_order": 20},
		{"name": "Slatted floor", "sort_order": 30}, // code left blank: made from the name
	} {
		if _, err := repo.Create(ctx, write("pen-type-"+pt["name"].(string)), domain.RegPenTypes, pt); err != nil {
			t.Fatalf("author pen type %v: %v", pt["name"], err)
		}
	}
	options, err := repo.Options(ctx, cfgTenant, domain.RegPenTypes)
	if err != nil {
		t.Fatalf("pen type options: %v", err)
	}
	var offered []string
	for _, option := range options {
		offered = append(offered, option.ID)
	}
	if strings.Join(offered, ",") != "elevated,non_elevated,slatted_floor" {
		t.Fatalf("the Partitions dropdown offers %v, want the register in its order", offered)
	}

	created, err := repo.Create(ctx, write("pen-type-create"), domain.RegPartitions, map[string]any{
		"park_id": cfgParkCBE, "pen_id": cfgShed, "label": "Part 9", "shed_type": "slatted_floor",
	})
	if err != nil {
		t.Fatalf("create a pen of the farm-added type: %v", err)
	}
	if created.Fields["shed_type"] != "slatted_floor" || created.Labels["shed_type"] != "Slatted floor" {
		t.Fatalf("created pen reads back %v / %q, want slatted_floor shown as Slatted floor", created.Fields["shed_type"], created.Labels["shed_type"])
	}
	stored := func() *string {
		var v *string
		if err := pool.QueryRow(ctx, `SELECT shed_type FROM shed_partitions WHERE tenant_id = $1::uuid AND shed_id = split_part($2, ':', 1)::uuid AND normalized_label = split_part($2, ':', 2)`, cfgTenant, created.ID).Scan(&v); err != nil {
			t.Fatalf("read the stored pen type: %v", err)
		}
		return v
	}
	if v := stored(); v == nil || *v != "slatted_floor" {
		t.Fatalf("stored pen type = %v, want slatted_floor", v)
	}

	// A rename changes the NAME everywhere and never the key the reports group by.
	renamed, err := repo.Update(ctx, write("pen-type-rename"), domain.RegPenTypes, "slatted_floor", map[string]any{"name": "Slatted"}, 0)
	if err != nil {
		t.Fatalf("rename the pen type: %v", err)
	}
	if renamed.ID != "slatted_floor" {
		t.Fatalf("a rename moved the key to %q", renamed.ID)
	}
	again, err := repo.Get(ctx, cfgTenant, domain.RegPartitions, created.ID)
	if err != nil {
		t.Fatalf("re-read the pen: %v", err)
	}
	if again.Labels["shed_type"] != "Slatted" {
		t.Fatalf("the pen shows %q after the rename, want Slatted", again.Labels["shed_type"])
	}

	// A type still given to a pen cannot be removed.
	if err := repo.Delete(ctx, write("pen-type-delete-used"), domain.RegPenTypes, "slatted_floor", 0); err == nil {
		t.Fatal("a pen type still given to a pen was removed")
	}

	flipped, err := repo.Update(ctx, write("pen-type-flip"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "non_elevated"}, again.RowVersion)
	if err != nil {
		t.Fatalf("flip the pen type: %v", err)
	}
	if flipped.Fields["shed_type"] != "non_elevated" {
		t.Fatalf("flipped pen reads back %v, want non_elevated", flipped.Fields["shed_type"])
	}

	// CLEARED, not left standing.
	cleared, err := repo.Update(ctx, write("pen-type-clear"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": nil}, flipped.RowVersion)
	if err != nil {
		t.Fatalf("clear the pen type: %v", err)
	}
	if got, ok := cleared.Fields["shed_type"]; ok && got != nil {
		t.Fatalf("cleared pen still reads %v; a cleared type must be absent", got)
	}
	if v := stored(); v != nil {
		t.Fatalf("stored pen type = %v after clearing, want NULL", *v)
	}

	// A code nobody authored is refused ON THE FIELD, not as a database error.
	_, err = repo.Update(ctx, write("pen-type-bogus"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "raised"}, cleared.RowVersion)
	var refErr *ports.RefError
	if !errors.As(err, &refErr) || refErr.Field != "shed_type" {
		t.Fatalf("an unauthored pen type = %v, want a RefError on shed_type", err)
	}

	// An ARCHIVED type cannot be given to a pen that does not already carry it.
	if _, err := repo.SetStatus(ctx, write("pen-type-archive"), domain.RegPenTypes, "slatted_floor", domain.StatusArchived, 0); err != nil {
		t.Fatalf("archive the pen type: %v", err)
	}
	_, err = repo.Update(ctx, write("pen-type-archived"), domain.RegPartitions, created.ID,
		map[string]any{"shed_type": "slatted_floor"}, cleared.RowVersion)
	if !errors.As(err, &refErr) || refErr.Field != "shed_type" {
		t.Fatalf("an archived pen type = %v, want a RefError on shed_type", err)
	}
}

// The register DEFINITION is what the screen renders. Pen types sit after Parks and before Pens
// (maintainer instruction 2026-09-25), and the Partitions pen-type column is a REFERENCE to that
// register -- never a closed list typed in Go, which is what made adding a kind of pen a code change.
func TestPartitionsTakeTheirPenTypeFromThePenTypesRegister(t *testing.T) {
	var order []string
	var column *domain.Column
	for _, register := range domain.Registers {
		if register.Group == domain.GroupFarmPlaces {
			order = append(order, register.Key)
		}
		if register.Key != domain.RegPartitions {
			continue
		}
		for i := range register.Columns {
			if register.Columns[i].Key == "shed_type" {
				column = &register.Columns[i]
			}
		}
	}
	if got := strings.Join(order, ","); got != "parks,pen_types,pens,partitions" {
		t.Fatalf("farm places rail = %s, want parks,pen_types,pens,partitions", got)
	}
	if column == nil {
		t.Fatal("the Partitions register has no pen-type column; there is then nowhere on the farm to set it")
	}
	if column.Type != domain.TypeRef || column.Ref != domain.RegPenTypes {
		t.Fatalf("pen type is a %q column referencing %q, want a reference to the Pen types register", column.Type, column.Ref)
	}
	if len(column.Options) != 0 {
		t.Fatalf("pen type carries a typed option list %v; the choices are the register's rows", column.Options)
	}
	if column.Required {
		t.Fatal("pen type must stay optional: not knowing yet is a real answer, and it is reported as unclassified")
	}
}

// A FARM MAPS ITS PENS TO PEN TYPES IN BULK the way it maps everything else: download Partitions,
// fill the Pen type column by NAME, upload. Two things used to stop that. shed_partitions keeps no
// row version, so the sheet carries 0 -- and the upload refused 0 for every register, so a
// downloaded Partitions sheet could never go back up. And the column was a closed list; it is now
// a reference to the Pen types register, resolved by name or code, with an unknown name refused on
// its row rather than stored.
func TestPartitionsSheetMapsPensToPenTypesByName(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)

	repo := NewRepository(pool, 15*time.Second)
	svc := app.NewService(repo)
	importer := app.NewImporter(svc, repo, nil, "test-worker", nil)
	w := ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, TraceID: "trace-pen-sheet"}
	write := func(key string) ports.WriteParams {
		return ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, IdempotencyKey: key, TraceID: "trace-" + key}
	}
	for _, pt := range []map[string]any{{"name": "Elevated", "code": "elevated"}, {"name": "Slatted floor"}} {
		if _, err := repo.Create(ctx, write("sheet-type-"+pt["name"].(string)), domain.RegPenTypes, pt); err != nil {
			t.Fatalf("author pen type: %v", err)
		}
	}
	created, err := repo.Create(ctx, write("sheet-pen"), domain.RegPartitions, map[string]any{"park_id": cfgParkCBE, "pen_id": cfgShed, "label": "Part 8"})
	if err != nil {
		t.Fatalf("create pen: %v", err)
	}

	var buf bytes.Buffer
	if err := svc.Export(ctx, cfgTenant, domain.RegPartitions, "all", domain.FormatCSV, &buf); err != nil {
		t.Fatalf("export: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.Contains(lines[0], ",shed_type,") {
		t.Fatalf("partitions sheet header = %q, want a shed_type column", lines[0])
	}
	var mine string
	for _, line := range lines[1:] {
		if strings.HasPrefix(line, created.ID+",") {
			mine = line
		}
	}
	if mine == "" || !strings.HasPrefix(mine, created.ID+",0,") {
		t.Fatalf("downloaded row = %q, want %s with row version 0", mine, created.ID)
	}
	cols := strings.Split(mine, ",")
	header := strings.Split(lines[0], ",")
	bogus := make([]string, len(cols))
	copy(bogus, cols)
	for i, h := range header {
		if h == "shed_type" {
			cols[i] = "Slatted floor" // by NAME, as the farm types it
			bogus[i] = "Raised"
		}
	}
	body := lines[0] + "\n" + strings.Join(cols, ",") + "\n"

	job, err := importer.Stage(ctx, w, domain.RegPartitions, "partitions.csv", strings.NewReader(body))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	job = waitForImport(t, ctx, repo, job.ID, domain.ImportPreviewed, domain.ImportFailed)
	if job.Status != domain.ImportPreviewed || job.ValidRows != 1 || job.InvalidRows != 0 {
		t.Fatalf("a downloaded Partitions row with a pen type by name must validate: %+v", job)
	}
	if _, ok, err := repo.RequestImportApply(ctx, cfgTenant, job.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request apply: %v %v", err, ok)
	}
	if err := importer.Process(ctx, cfgTenant, job.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if job = waitForImport(t, ctx, repo, job.ID, domain.ImportApplied, domain.ImportFailed); job.Status != domain.ImportApplied || job.AppliedRows != 1 {
		t.Fatalf("applied = %+v", job)
	}
	var stored *string
	if err := pool.QueryRow(ctx, `SELECT shed_type FROM shed_partitions WHERE tenant_id = $1::uuid AND shed_id = split_part($2, ':', 1)::uuid AND normalized_label = split_part($2, ':', 2)`, cfgTenant, created.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored pen type: %v", err)
	}
	if stored == nil || *stored != "slatted_floor" {
		t.Fatalf("stored pen type = %v, want slatted_floor", stored)
	}

	// An unknown name is refused on its row, never stored.
	bad, err := importer.Stage(ctx, w, domain.RegPartitions, "partitions-bad.csv", strings.NewReader(lines[0]+"\n"+strings.Join(bogus, ",")+"\n"))
	if err != nil {
		t.Fatalf("stage bad: %v", err)
	}
	if bad = waitForImport(t, ctx, repo, bad.ID, domain.ImportPreviewed, domain.ImportFailed); bad.InvalidRows != 1 {
		t.Fatalf("an unknown pen type must be refused on its row: %+v", bad)
	}
	rows, err := repo.ImportRows(ctx, cfgTenant, bad.ID, ports.ImportRowsParams{State: domain.ImportRowInvalid, Limit: 5})
	if err != nil || len(rows) != 1 || len(rows[0].Errors) == 0 || rows[0].Errors[0].Field != "shed_type" {
		t.Fatalf("invalid rows = %+v (%v), want the error on shed_type", rows, err)
	}
}

func waitForImport(t *testing.T, ctx context.Context, repo *Repository, jobID string, statuses ...string) domain.ImportJob {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		job, err := repo.GetImportJob(ctx, cfgTenant, jobID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		for _, s := range statuses {
			if job.Status == s {
				return job
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("job %s never reached %v", jobID, statuses)
	return domain.ImportJob{}
}
