package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"

	"github.com/vgoats/goatos/backend/internal/configuration/app"
	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/configuration/ports"
	identitypg "github.com/vgoats/goatos/backend/internal/identity/adapters/postgres"
	identityapp "github.com/vgoats/goatos/backend/internal/identity/app"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// workbook builds an .xlsx in memory: one tab per name, each with a header and rows.
type workbook struct {
	tabs [][2]any // name, [][]string
}

func (w *workbook) tab(name string, rows ...[]string) *workbook {
	w.tabs = append(w.tabs, [2]any{name, rows})
	return w
}

func (w *workbook) bytes(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	for n, tab := range w.tabs {
		name := tab[0].(string)
		rows := tab[1].([][]string)
		if n == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				t.Fatal(err)
			}
		} else if _, err := f.NewSheet(name); err != nil {
			t.Fatal(err)
		}
		sw, err := f.NewStreamWriter(name)
		if err != nil {
			t.Fatal(err)
		}
		for r, row := range rows {
			cells := make([]any, len(row))
			for i, c := range row {
				cells[i] = c
			}
			cell, _ := excelize.CoordinatesToCellName(1, r+1)
			if err := sw.SetRow(cell, cells); err != nil {
				t.Fatal(err)
			}
		}
		if err := sw.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type workbookHarness struct {
	t        *testing.T
	ctx      context.Context
	pool     *pgxpool.Pool
	repo     *Repository
	svc      *app.Service
	importer *app.Importer
	w        ports.WriteParams
}

func newWorkbookHarness(t *testing.T, ctx context.Context, pool *pgxpool.Pool, worker string) *workbookHarness {
	t.Helper()
	repo := NewRepository(pool, 15*time.Second)
	svc := app.NewService(repo)
	animals := identityapp.NewService(identitypg.NewRepository(pool, 15*time.Second)).WithBulkPreviewSigningKey("workbook-test-signing-key")
	return &workbookHarness{
		t: t, ctx: ctx, pool: pool, repo: repo, svc: svc,
		importer: app.NewImporter(svc, repo, animals, worker, nil),
		w:        ports.WriteParams{TenantID: cfgTenant, ActorID: cfgActor, TraceID: "trace-workbook"},
	}
}

func (h *workbookHarness) stage(name string, data []byte) domain.ImportBundle {
	h.t.Helper()
	bundle, err := h.importer.StageWorkbook(h.ctx, h.w, name, bytes.NewReader(data))
	if err != nil {
		h.t.Fatalf("stage workbook %s: %v", name, err)
	}
	return bundle
}

func (h *workbookHarness) waitBundle(id string, timeout time.Duration, statuses ...string) domain.ImportBundle {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		bundle, err := h.repo.GetImportBundle(h.ctx, cfgTenant, id)
		if err != nil {
			h.t.Fatalf("get bundle: %v", err)
		}
		for _, s := range statuses {
			if bundle.Status == s {
				return bundle
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	bundle, _ := h.repo.GetImportBundle(h.ctx, cfgTenant, id)
	h.t.Fatalf("bundle %s never reached %v; now %s (%s) tabs=%s", id, statuses, bundle.Status, bundle.Error, describeTabs(bundle))
	return domain.ImportBundle{}
}

func describeTabs(b domain.ImportBundle) string {
	parts := make([]string, 0, len(b.Jobs))
	for _, j := range b.Jobs {
		parts = append(parts, fmt.Sprintf("%s[%s t=%d v=%d i=%d a=%d f=%d %s]", j.SheetName, j.Status, j.TotalRows, j.ValidRows, j.InvalidRows, j.AppliedRows, j.FailedRows, j.Error))
	}
	return strings.Join(parts, " ")
}

func (h *workbookHarness) tabByRegister(b domain.ImportBundle, register string) domain.ImportJob {
	h.t.Helper()
	for _, j := range b.Jobs {
		if j.Register == register {
			return j
		}
	}
	h.t.Fatalf("bundle has no %s tab: %s", register, describeTabs(b))
	return domain.ImportJob{}
}

func (h *workbookHarness) rowsIn(jobID, state string) []domain.ImportRow {
	h.t.Helper()
	rows, err := h.repo.ImportRows(h.ctx, cfgTenant, jobID, ports.ImportRowsParams{State: state, Limit: 200})
	if err != nil {
		h.t.Fatalf("rows %s: %v", state, err)
	}
	return rows
}

func (h *workbookHarness) countRows(register, query string) int {
	h.t.Helper()
	page, err := h.repo.List(h.ctx, cfgTenant, register, ports.ListParams{Status: "all", Query: query, Limit: 200})
	if err != nil {
		h.t.Fatalf("list %s: %v", register, err)
	}
	return page.Total
}

func (h *workbookHarness) findRow(register, query string) domain.Row {
	h.t.Helper()
	page, err := h.repo.List(h.ctx, cfgTenant, register, ports.ListParams{Status: "all", Query: query, Limit: 50})
	if err != nil {
		h.t.Fatalf("list %s: %v", register, err)
	}
	for _, row := range page.Rows {
		if strings.EqualFold(row.Display, query) || strings.EqualFold(domain.FieldString(row.Fields, "name"), query) || strings.EqualFold(domain.FieldString(row.Fields, "label"), query) {
			return row
		}
	}
	h.t.Fatalf("no %s row called %q (got %d)", register, query, len(page.Rows))
	return domain.Row{}
}

// TestConfigurationWorkbookPostgresPaths drives one onboarding workbook through the real
// pipeline end to end: nine tabs plus an unknown one, every cross-tab shape the farm's own
// file will carry -- a pen in a park that only exists on the Parks tab, kept for a stage that
// only exists on the Stages tab; a partition of that pen; a list under a list with the item
// inheriting the kind through both; an animal in that new park, pen and partition, of a new
// species -- beside the ways a row can be wrong (an unknown park, a stage-less list, a vaccine
// with a route, a made-up species) and the ways a row can fail only at apply (a pen the park
// already has). It pins that NOTHING is written before Apply, that every tab is previewed with
// its own counts, that a child of a failed parent is not written and says which sheet and row
// failed it, and that the rows-to-fix workbook has one tab per sheet with problems.
func TestConfigurationWorkbookPostgresPaths(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	h := newWorkbookHarness(t, ctx, pool, "wb-worker")

	file := (&workbook{}).
		tab("Notes", []string{"anything"}, []string{"free text"}).
		tab("Species", []string{"name", "code", "sort_order"}, []string{"Camel", "camel", "30"}).
		tab("Gender", []string{"name", "code"}). // header only: skipped
		// The range is wide on purpose: the Animals rows below carry a FIXED date of birth, and since
		// 2026-09-26 an animal is refused when it falls outside its stage's range -- a 0-90 day K1
		// made those rows fail once the calendar passed 90 days after it.
		tab("Lifecycle stages", []string{"name", "code", "min_age_days", "max_age_days"}, []string{"Kid one", "K1", "0", "3650"}).
		tab("Parks", []string{"name", "code", "capacity"},
			[]string{"Chennai", "CHN", "500"},
			[]string{"", "NON", "10"}). // name missing: invalid
		// A pen is a building in a park: stage and gender left the register on 2026-09-22.
		// Capacity is per partition since 2026-09-30, so it rides the Partitions tab.
		tab("Pens", []string{"park_id", "name"},
			[]string{"Chennai", "Nehru"},  // pending park
			[]string{"CBE", "Castro"},     // the park already has Castro: fails at apply
			[]string{"Nowhere", "Orphan"}, // unknown park: invalid
			[]string{"NON", "Ghost"}).     // names the INVALID parks row: invalid
		tab("Partitions", []string{"park_id", "pen_id", "label", "sort_order", "capacity"},
			[]string{"Chennai", "Nehru", "Part 1", "1", "40"}, // pending park + pending pen
			[]string{"Chennai", "Nehru", "Part 2", "2", "25"},
			[]string{"CBE", "Castro", "1", "1", "20"}). // resolves to the STORED Castro; label 1 exists: fails at apply
		tab("Lists", []string{"name", "parent_id", "kind", "sort_order"},
			[]string{"Dewormers", "", "dewormer", "50"},
			[]string{"Oral dewormers", "Dewormers", "", "1"}, // parent on this very tab
			[]string{"Kindless", "", "", "60"}).              // top-level with no kind: invalid
		tab("Items & categories", []string{"name", "category_id", "unit", "route", "strength", "withdrawal_days"},
			[]string{"Ivermectin", "Oral dewormers", "ml", "oral", "10 mg/ml", "28"}, // kind through two pending lists
			[]string{"PPR", "Vaccines", "dose", "oral", "", "21"}).                   // a vaccine with a route: invalid
		tab("Animals", []string{"animal_identifier_1", "species", "sex", "park", "pen_name", "partition_label", "management_stage", "dob", "origin"},
			[]string{"WB-CAMEL-1", "Camel", "female", "Chennai", "Nehru", "Part 1", "K1", "2026-06-01", "procured"},    // everything pending
			[]string{"WB-GOAT-2", "goat", "female", "CBE", "Castro", "1", "F2-Female", "2025-01-01", "procured"},       // everything stored
			[]string{"WB-DRAGON-3", "Dragon", "female", "Chennai", "Nehru", "Part 1", "K1", "2026-06-01", "procured"}). // species nobody has: invalid
		bytes(t)

	bundle := h.stage("farm-setup.xlsx", file)
	if got := bundle.UnknownSheets; len(got) != 1 || got[0] != "Notes" {
		t.Fatalf("unknown sheets = %v, want [Notes]", got)
	}
	wantOrder := []string{domain.RegSpecies, domain.RegStages, domain.RegParks, domain.RegPens, domain.RegPartitions, domain.RegCategories, domain.RegItems, domain.RegAnimals}
	if len(bundle.Jobs) != len(wantOrder) {
		t.Fatalf("tabs = %s, want %d (the empty Gender tab skipped)", describeTabs(bundle), len(wantOrder))
	}
	for n, job := range bundle.Jobs {
		if job.Register != wantOrder[n] || job.BundleOrder != n {
			t.Fatalf("tab %d = %s/%d, want %s", n, job.Register, job.BundleOrder, wantOrder[n])
		}
		wantStatus := domain.ImportJobQueued
		if n == 0 {
			wantStatus = domain.ImportValidating
		}
		if job.Status != wantStatus {
			t.Fatalf("tab %s status = %s, want %s", job.SheetName, job.Status, wantStatus)
		}
	}

	// --- preview: every tab validated, in order, nothing written ---
	bundle = h.waitBundle(bundle.ID, 3*time.Minute, domain.ImportPreviewed, domain.ImportFailed)
	if bundle.Status != domain.ImportPreviewed {
		t.Fatalf("bundle = %s (%s): %s", bundle.Status, bundle.Error, describeTabs(bundle))
	}
	type counts struct{ total, valid, invalid int }
	want := map[string]counts{
		domain.RegSpecies:    {1, 1, 0},
		domain.RegStages:     {1, 1, 0},
		domain.RegParks:      {2, 1, 1},
		domain.RegPens:       {4, 2, 2},
		domain.RegPartitions: {3, 3, 0},
		domain.RegCategories: {3, 3, 0},
		domain.RegItems:      {2, 1, 1},
		domain.RegAnimals:    {3, 2, 1},
	}
	for reg, c := range want {
		job := h.tabByRegister(bundle, reg)
		if job.Status != domain.ImportPreviewed || job.TotalRows != c.total || job.ValidRows != c.valid || job.InvalidRows != c.invalid {
			t.Fatalf("%s preview = %s t=%d v=%d i=%d, want %+v", reg, job.Status, job.TotalRows, job.ValidRows, job.InvalidRows, c)
		}
	}
	for _, q := range []string{"Chennai", "Nehru", "Camel", "Dewormers", "Ivermectin"} {
		for _, reg := range []string{domain.RegParks, domain.RegPens, domain.RegSpecies, domain.RegCategories, domain.RegItems} {
			if n := h.countRows(reg, q); n != 0 {
				t.Fatalf("%s %q written before apply (%d)", reg, q, n)
			}
		}
	}
	// The pending pen names its park by TOKEN, never by a guessed id.
	pens := h.tabByRegister(bundle, domain.RegPens)
	parks := h.tabByRegister(bundle, domain.RegParks)
	partitionsTab := h.tabByRegister(bundle, domain.RegPartitions)
	for _, row := range h.rowsIn(pens.ID, domain.ImportRowValid) {
		if domain.FieldString(row.Fields, "name") == "Nehru" {
			if got := domain.FieldString(row.Fields, "park_id"); got != domain.BundleRowToken(parks.ID, 2) {
				t.Fatalf("Nehru park_id = %q, want the Parks row 2 token", got)
			}
		}
	}
	// A ref to a row that does not exist YET, on another tab of the same workbook, resolves to that
	// tab's row token and becomes its id at apply. The pen's stage used to carry this proof; a pen
	// no longer has one, so the partition's pen does -- the same shape, one tab further down.
	for _, row := range h.rowsIn(partitionsTab.ID, domain.ImportRowValid) {
		if domain.FieldString(row.Fields, "label") == "Part 1" {
			if got := domain.FieldString(row.Fields, "pen_id"); got != domain.BundleRowToken(pens.ID, 2) {
				t.Fatalf("Part 1 pen_id = %q, want the Pens row 2 token", got)
			}
		}
	}
	invalidPens := h.rowsIn(pens.ID, domain.ImportRowInvalid)
	if len(invalidPens) != 2 {
		t.Fatalf("invalid pens = %d", len(invalidPens))
	}
	for _, row := range invalidPens {
		if len(row.Errors) == 0 || row.Errors[0].Field != "park_id" {
			t.Fatalf("invalid pen row %d errors = %+v, want park_id", row.RowNo, row.Errors)
		}
	}
	items := h.tabByRegister(bundle, domain.RegItems)
	if rows := h.rowsIn(items.ID, domain.ImportRowInvalid); len(rows) != 1 || rows[0].Errors[0].Field != "route" {
		t.Fatalf("invalid items = %+v, want the vaccine's route refused", rows)
	}
	animals := h.tabByRegister(bundle, domain.RegAnimals)
	if rows := h.rowsIn(animals.ID, domain.ImportRowInvalid); len(rows) != 1 || rows[0].Errors[0].Field != "species" {
		t.Fatalf("invalid animals = %+v, want the dragon's species refused", rows)
	}
	// The pending animal kept the park / pen tokens for apply.
	for _, row := range h.rowsIn(animals.ID, domain.ImportRowValid) {
		if domain.FieldString(row.Fields, "animal_identifier_1") == "WB-CAMEL-1" {
			if got := domain.FieldString(row.Fields, "park"); got != domain.BundleRowToken(parks.ID, 2) {
				t.Fatalf("camel park = %q, want the Parks token", got)
			}
			if got := domain.FieldString(row.Fields, "pen_name"); got != domain.BundleRowToken(pens.ID, 2) {
				t.Fatalf("camel pen = %q, want the Pens token", got)
			}
			if got := domain.FieldString(row.Fields, "species"); got != "camel" {
				t.Fatalf("camel species = %q, want the code", got)
			}
		}
	}

	// --- rows to fix: one tab per sheet with problems ---
	var errBuf bytes.Buffer
	if err := h.svc.BundleErrorSheet(ctx, h.repo, cfgTenant, bundle.ID, domain.FormatXLSX, &errBuf); err != nil {
		t.Fatalf("bundle error sheet: %v", err)
	}
	errWb, err := excelize.OpenReader(bytes.NewReader(errBuf.Bytes()))
	if err != nil {
		t.Fatalf("open error workbook: %v", err)
	}
	if got := errWb.GetSheetList(); strings.Join(got, ",") != "Parks,Pens,Items & categories,Animals" {
		t.Fatalf("error workbook tabs = %v", got)
	}
	penRows, _ := errWb.GetRows("Pens")
	if len(penRows) != 3 || penRows[0][0] != "row" || penRows[0][1] != "problem" {
		t.Fatalf("Pens rows to fix = %v", penRows)
	}
	var csvBuf bytes.Buffer
	if err := h.svc.BundleErrorSheet(ctx, h.repo, cfgTenant, bundle.ID, domain.FormatCSV, &csvBuf); err != nil {
		t.Fatalf("bundle error csv: %v", err)
	}
	if !strings.HasPrefix(csvBuf.String(), "sheet,row,problem,") || !strings.Contains(csvBuf.String(), "\nPens,") {
		t.Fatalf("bundle error csv = %q", csvBuf.String()[:120])
	}

	// --- apply: tab after tab, tokens become the ids the earlier tabs wrote ---
	if _, ok, err := h.repo.RequestImportBundleApply(ctx, cfgTenant, bundle.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request apply: %v %v", ok, err)
	}
	if err := h.importer.ProcessBundle(ctx, cfgTenant, bundle.ID); err != nil {
		t.Fatalf("process bundle: %v", err)
	}
	bundle = h.waitBundle(bundle.ID, 4*time.Minute, domain.ImportApplied, domain.ImportFailed)
	if bundle.Status != domain.ImportApplied {
		t.Fatalf("bundle = %s (%s): %s", bundle.Status, bundle.Error, describeTabs(bundle))
	}
	type applied struct{ applied, failed int }
	wantApplied := map[string]applied{
		domain.RegSpecies:    {1, 0},
		domain.RegStages:     {1, 0},
		domain.RegParks:      {1, 0},
		domain.RegPens:       {1, 1}, // Castro in CBE already exists
		domain.RegPartitions: {2, 1}, // partition 1 of the stored Castro already exists
		domain.RegCategories: {2, 1}, // Kindless: the store wants a kind on a top-level list
		domain.RegItems:      {1, 0},
		domain.RegAnimals:    {2, 0},
	}
	for reg, c := range wantApplied {
		job := h.tabByRegister(bundle, reg)
		if job.Status != domain.ImportApplied || job.AppliedRows != c.applied || job.FailedRows != c.failed {
			t.Fatalf("%s apply = %s a=%d f=%d (%s), want %+v", reg, job.Status, job.AppliedRows, job.FailedRows, job.Error, c)
		}
	}
	chennai := h.findRow(domain.RegParks, "Chennai")
	nehru := h.findRow(domain.RegPens, "Nehru")
	if domain.FieldString(nehru.Fields, "park_id") != chennai.ID {
		t.Fatalf("Nehru park = %q, want Chennai %s", domain.FieldString(nehru.Fields, "park_id"), chennai.ID)
	}
	var partitions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM shed_partitions WHERE tenant_id = $1::uuid AND shed_id = $2::uuid`, cfgTenant, nehru.ID).Scan(&partitions); err != nil || partitions != 2 {
		t.Fatalf("Nehru partitions = %d (%v), want 2", partitions, err)
	}
	dewormers := h.findRow(domain.RegCategories, "Dewormers")
	oral := h.findRow(domain.RegCategories, "Oral dewormers")
	if domain.FieldString(oral.Fields, "parent_id") != dewormers.ID || domain.FieldString(oral.Fields, "kind") != "dewormer" {
		t.Fatalf("Oral dewormers = %+v, want under Dewormers with kind dewormer", oral.Fields)
	}
	ivermectin := h.findRow(domain.RegItems, "Ivermectin")
	if domain.FieldString(ivermectin.Fields, "category_id") != oral.ID || domain.FieldString(ivermectin.Fields, "route") != "oral" {
		t.Fatalf("Ivermectin = %+v", ivermectin.Fields)
	}
	var camelShed, camelSpecies, camelPartition string
	if err := pool.QueryRow(ctx, `SELECT g.shed_id::text, g.species, COALESCE(p.partition_label, '') FROM goats g
		JOIN goat_identifiers gi ON gi.goat_id = g.goat_id AND gi.identifier_value = 'WB-CAMEL-1'
		LEFT JOIN goat_shed_partitions p ON p.tenant_id = g.tenant_id AND p.goat_id = g.goat_id
		WHERE g.tenant_id = $1::uuid`, cfgTenant).Scan(&camelShed, &camelSpecies, &camelPartition); err != nil {
		t.Fatalf("camel: %v", err)
	}
	if camelShed != nehru.ID || camelSpecies != "camel" || camelPartition != "Part 1" {
		t.Fatalf("camel = shed %s species %s partition %q, want Nehru / camel / Part 1", camelShed, camelSpecies, camelPartition)
	}
	// Each partition keeps its own capacity (per partition since 2026-09-30).
	var cap1, cap2 int
	if err := pool.QueryRow(ctx, `SELECT
		  (SELECT capacity FROM shed_partitions WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = '1'),
		  (SELECT capacity FROM shed_partitions WHERE tenant_id = $1 AND shed_id = $2::uuid AND normalized_label = '2')`,
		cfgTenant, nehru.ID).Scan(&cap1, &cap2); err != nil || cap1 != 40 || cap2 != 25 {
		t.Fatalf("Nehru partition capacities = %d / %d (%v), want 40 / 25", cap1, cap2, err)
	}
	if n := h.countRows(domain.RegPens, "Castro"); n != 1 {
		t.Fatalf("Castro pens = %d, want the stored one only", n)
	}
	failedPens := h.rowsIn(pens.ID, domain.ImportRowFailed)
	if len(failedPens) != 1 || domain.FieldString(failedPens[0].Fields, "name") != "Castro" {
		t.Fatalf("failed pens = %+v", failedPens)
	}

	// --- a second workbook: a pen whose park row FAILS at apply is not written, and says why ---
	second := (&workbook{}).
		tab("Parks", []string{"name", "code"}, []string{"Coimbatore Two", "CBE"}).      // the code exists: fails at apply
		tab("Pens", []string{"park_id", "name"}, []string{"Coimbatore Two", "Lonely"}). // names that very row by its new name
		bytes(t)
	b2 := h.stage("second.xlsx", second)
	b2 = h.waitBundle(b2.ID, 2*time.Minute, domain.ImportPreviewed, domain.ImportFailed)
	if b2.Status != domain.ImportPreviewed || h.tabByRegister(b2, domain.RegPens).ValidRows != 1 {
		t.Fatalf("second preview: %s %s", b2.Status, describeTabs(b2))
	}
	if _, ok, err := h.repo.RequestImportBundleApply(ctx, cfgTenant, b2.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request apply 2: %v %v", ok, err)
	}
	if err := h.importer.ProcessBundle(ctx, cfgTenant, b2.ID); err != nil {
		t.Fatalf("process bundle 2: %v", err)
	}
	b2 = h.waitBundle(b2.ID, 2*time.Minute, domain.ImportApplied, domain.ImportFailed)
	if b2.Status != domain.ImportApplied || h.tabByRegister(b2, domain.RegParks).FailedRows != 1 || h.tabByRegister(b2, domain.RegPens).FailedRows != 1 {
		t.Fatalf("second apply: %s %s", b2.Status, describeTabs(b2))
	}
	lonely := h.rowsIn(h.tabByRegister(b2, domain.RegPens).ID, domain.ImportRowFailed)
	if len(lonely) != 1 || lonely[0].Errors[0].Code != "parent_not_added" || !strings.Contains(lonely[0].Errors[0].Message, "row 2 of the Parks tab") {
		t.Fatalf("lonely pen errors = %+v, want parent_not_added naming Parks row 2", lonely)
	}
	if n := h.countRows(domain.RegPens, "Lonely"); n != 0 {
		t.Fatal("a pen was written without its park")
	}

	// --- the single-sheet path still works beside the workbook, including an UPDATE by id ---
	var exported bytes.Buffer
	if err := h.svc.Export(ctx, cfgTenant, domain.RegParks, "all", domain.FormatCSV, &exported); err != nil {
		t.Fatalf("export: %v", err)
	}
	var updateLine string
	for _, line := range strings.Split(exported.String(), "\n") {
		if strings.Contains(line, "Chennai") {
			updateLine = line
		}
	}
	if updateLine == "" {
		t.Fatal("Chennai missing from the parks export")
	}
	updateLine = strings.Replace(updateLine, "Chennai", "Chennai North", 1)
	header := strings.Split(exported.String(), "\n")[0]
	job, err := h.importer.Stage(ctx, h.w, domain.RegParks, "parks-update.csv", strings.NewReader(header+"\n"+updateLine+"\n"))
	if err != nil {
		t.Fatalf("stage single sheet: %v", err)
	}
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if job, _ = h.repo.GetImportJob(ctx, cfgTenant, job.ID); job.Status == domain.ImportPreviewed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Status != domain.ImportPreviewed || job.ValidRows != 1 {
		t.Fatalf("single-sheet preview = %+v", job)
	}
	if _, ok, err := h.repo.RequestImportApply(ctx, cfgTenant, job.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request single apply: %v %v", ok, err)
	}
	if err := h.importer.Process(ctx, cfgTenant, job.ID); err != nil {
		t.Fatalf("process single: %v", err)
	}
	if got := h.findRow(domain.RegParks, "Chennai North"); got.ID != chennai.ID {
		t.Fatalf("update by id wrote a new row %s, want %s renamed", got.ID, chennai.ID)
	}

	// --- refusals up front: two tabs for one list; a tab missing a required column ---
	dup := (&workbook{}).tab("Parks", []string{"name", "code"}, []string{"A", "A"}).tab("Park", []string{"name", "code"}, []string{"B", "B"}).bytes(t)
	if _, err := h.importer.StageWorkbook(ctx, h.w, "dup.xlsx", bytes.NewReader(dup)); err == nil || app.HTTPError(err).Code != "duplicate_sheet" {
		t.Fatalf("duplicate tabs: %v", err)
	}
	bad := (&workbook{}).tab("Parks", []string{"name", "code"}, []string{"Ok", "OK"}).tab("Pens", []string{"notes"}, []string{"3"}).bytes(t)
	if _, err := h.importer.StageWorkbook(ctx, h.w, "bad.xlsx", bytes.NewReader(bad)); err == nil || app.HTTPError(err).Code != "missing_columns" || !strings.HasPrefix(app.HTTPError(err).Message, "Pens: ") {
		t.Fatalf("missing columns: %v", err)
	}
	empty := (&workbook{}).tab("Parks", []string{"name", "code"}).tab("Notes", []string{"x"}, []string{"y"}).bytes(t)
	if _, err := h.importer.StageWorkbook(ctx, h.w, "empty.xlsx", bytes.NewReader(empty)); err == nil || app.HTTPError(err).Code != "invalid_file" {
		t.Fatalf("empty workbook: %v", err)
	}
	if _, err := h.importer.StageWorkbook(ctx, h.w, "parks.csv", strings.NewReader("name,code\nA,A\n")); err == nil || app.HTTPError(err).Code != "invalid_format" {
		t.Fatalf("csv workbook: %v", err)
	}
	// Nothing above left a bundle behind.
	bundles, err := h.repo.ListImportBundles(ctx, cfgTenant, 10)
	if err != nil || len(bundles) != 2 {
		t.Fatalf("bundles = %d (%v), want the two real ones", len(bundles), err)
	}
}

// failingRepo wraps the store and fails one method after N calls with a context error -- the
// shape a process death leaves behind: no failed status, a live claim, a cursor mid-phase.
type failingRepo struct {
	ports.ImportRepository
	method string
	after  int
	calls  int
	err    error
}

func (f *failingRepo) UpdateImportRows(ctx context.Context, tenantID, jobID, fromState string, updates []ports.ImportRowUpdate) (int, error) {
	if f.method == "UpdateImportRows" {
		f.calls++
		if f.calls > f.after {
			return 0, f.err
		}
	}
	return f.ImportRepository.UpdateImportRows(ctx, tenantID, jobID, fromState, updates)
}

func (f *failingRepo) ClaimImportRows(ctx context.Context, tenantID, jobID string, rowNos []int) ([]int, error) {
	if f.method == "ClaimImportRows" {
		f.calls++
		if f.calls > f.after {
			return nil, f.err
		}
	}
	return f.ImportRepository.ClaimImportRows(ctx, tenantID, jobID, rowNos)
}

// TestConfigurationWorkbookResumesAfterAWorkerDies kills the worker twice -- once mid-validate
// on the Pens tab, once mid-apply on it -- and lets the recovery sweep (a different worker,
// after the claim lapsed) finish the workbook. Every row is written exactly once: the apply
// resumes after the last chunk it recorded, and a chunk it had written but not recorded
// replays through the per-row idempotency key.
func TestConfigurationWorkbookResumesAfterAWorkerDies(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	h := newWorkbookHarness(t, ctx, pool, "wb-first")

	const pensPerPark = 450 // more than two apply chunks
	parks := [][]string{{"name", "code"}, {"Alpha", "ALP"}, {"Beta", "BET"}}
	pens := [][]string{{"park_id", "name"}}
	for n := 1; n <= pensPerPark; n++ {
		pens = append(pens, []string{"ALP", "Pen " + strconv.Itoa(n)})
		pens = append(pens, []string{"Beta", "Pen " + strconv.Itoa(n)}) // the same name in the other park
	}
	file := (&workbook{}).tab("Parks", parks...).tab("Pens", pens...).bytes(t)

	// A worker that dies during the Pens validation (after two chunks of outcomes).
	dying := &failingRepo{ImportRepository: h.repo, method: "UpdateImportRows", after: 2 + 2, err: context.DeadlineExceeded} // parks: 2 calls (valid+invalid), then pens
	dyingImporter := app.NewImporter(h.svc, dying, nil, "wb-first", nil)
	bundle, err := dyingImporter.StageWorkbook(ctx, h.w, "big.xlsx", bytes.NewReader(file))
	if err != nil {
		t.Fatalf("stage: %v", err)
	}
	// StageWorkbook kicked a goroutine on the dying importer; let it die.
	dyingImporter.Wait()
	mid, err := h.repo.GetImportBundle(ctx, cfgTenant, bundle.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	pensTab := h.tabByRegister(mid, domain.RegPens)
	if mid.Status != domain.ImportValidating || pensTab.Status != domain.ImportValidating || pensTab.ProgressRowNo == 0 || pensTab.ProgressRowNo >= pensTab.TotalRows {
		t.Fatalf("expected the worker to die mid-validate: bundle %s, %s", mid.Status, describeTabs(mid))
	}
	// The claim is live, so a sweep by another worker must NOT pick it up yet.
	fresh := newWorkbookHarness(t, ctx, pool, "wb-second")
	if _, err := fresh.importer.ProcessDue(ctx, cfgTenant, 10); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if again, _ := h.repo.GetImportBundle(ctx, cfgTenant, bundle.ID); h.tabByRegister(again, domain.RegPens).ProgressRowNo != pensTab.ProgressRowNo {
		t.Fatal("a live claim was worked by another worker")
	}
	// The claim lapses; the sweep finishes validation and previews the workbook.
	if _, err := pool.Exec(ctx, `UPDATE configuration_import_jobs SET claimed_at = now() - interval '10 minutes' WHERE job_id = $1::uuid`, pensTab.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.importer.ProcessDue(ctx, cfgTenant, 10); err != nil {
		t.Fatalf("sweep 2: %v", err)
	}
	bundle = h.waitBundle(bundle.ID, 3*time.Minute, domain.ImportPreviewed, domain.ImportFailed)
	pensTab = h.tabByRegister(bundle, domain.RegPens)
	if bundle.Status != domain.ImportPreviewed || pensTab.ValidRows != 2*pensPerPark || pensTab.InvalidRows != 0 {
		t.Fatalf("after resume: %s %s", bundle.Status, describeTabs(bundle))
	}

	// Apply with a worker that dies after the second chunk claim of the Pens tab.
	if _, ok, err := h.repo.RequestImportBundleApply(ctx, cfgTenant, bundle.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request apply: %v %v", ok, err)
	}
	dying2 := &failingRepo{ImportRepository: h.repo, method: "ClaimImportRows", after: 1 + 1, err: context.DeadlineExceeded} // parks: 1 claim; pens: 1 claim, then die
	dyingImporter2 := app.NewImporter(h.svc, dying2, nil, "wb-third", nil)
	err = dyingImporter2.ProcessBundle(ctx, cfgTenant, bundle.ID)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected the apply to die: %v", err)
	}
	mid, _ = h.repo.GetImportBundle(ctx, cfgTenant, bundle.ID)
	pensTab = h.tabByRegister(mid, domain.RegPens)
	if mid.Status != domain.ImportApplying || pensTab.Status != domain.ImportApplying || pensTab.AppliedRows == 0 || pensTab.AppliedRows >= pensTab.ValidRows {
		t.Fatalf("expected the worker to die mid-apply: %s %s", mid.Status, describeTabs(mid))
	}
	if _, err := pool.Exec(ctx, `UPDATE configuration_import_jobs SET claimed_at = now() - interval '10 minutes' WHERE job_id = $1::uuid`, pensTab.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.importer.ProcessDue(ctx, cfgTenant, 10); err != nil {
		t.Fatalf("sweep 3: %v", err)
	}
	bundle = h.waitBundle(bundle.ID, 4*time.Minute, domain.ImportApplied, domain.ImportFailed)
	pensTab = h.tabByRegister(bundle, domain.RegPens)
	if bundle.Status != domain.ImportApplied || pensTab.AppliedRows != 2*pensPerPark || pensTab.FailedRows != 0 {
		t.Fatalf("after apply resume: %s %s", bundle.Status, describeTabs(bundle))
	}
	var written int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM locations WHERE tenant_id = $1::uuid AND location_type = 'shed' AND name LIKE 'Pen %'`, cfgTenant).Scan(&written); err != nil {
		t.Fatal(err)
	}
	if written != 2*pensPerPark {
		t.Fatalf("pens written = %d, want exactly %d (once each, across the restart)", written, 2*pensPerPark)
	}
	var perPark int
	if err := pool.QueryRow(ctx, `SELECT count(DISTINCT parent_location_id) FROM locations WHERE tenant_id = $1::uuid AND location_type = 'shed' AND name LIKE 'Pen %'`, cfgTenant).Scan(&perPark); err != nil || perPark != 2 {
		t.Fatalf("pens spread over %d parks (%v), want 2 -- the same pen name in each park", perPark, err)
	}

	// Cancel mid-apply of a third workbook: rows already written stay, the rest are skipped.
	third := (&workbook{}).tab("Parks", []string{"name", "code"}, []string{"Gamma", "GAM"}).tab("Pens", pens[:201]...).bytes(t)
	b3 := h.stage("third.xlsx", third)
	b3 = h.waitBundle(b3.ID, 2*time.Minute, domain.ImportPreviewed, domain.ImportFailed)
	if b3.Status != domain.ImportPreviewed {
		t.Fatalf("third: %s %s", b3.Status, describeTabs(b3))
	}
	if _, ok, err := h.repo.RequestImportBundleApply(ctx, cfgTenant, b3.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request apply 3: %v %v", ok, err)
	}
	cancelling := &failingRepo{ImportRepository: h.repo, method: "ClaimImportRows", after: 1, err: context.Canceled}
	if err := app.NewImporter(h.svc, cancelling, nil, "wb-fourth", nil).ProcessBundle(ctx, cfgTenant, b3.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the third apply to stop: %v", err)
	}
	if _, ok, err := h.repo.CancelImportBundle(ctx, cfgTenant, b3.ID); err != nil || !ok {
		t.Fatalf("cancel: %v %v", ok, err)
	}
	b3, _ = h.repo.GetImportBundle(ctx, cfgTenant, b3.ID)
	if b3.Status != domain.ImportCancelled {
		t.Fatalf("third after cancel = %s", b3.Status)
	}
	p3 := h.tabByRegister(b3, domain.RegPens)
	if p3.Status != domain.ImportCancelled {
		t.Fatalf("third pens tab = %s", p3.Status)
	}
	var skipped int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM configuration_import_rows WHERE job_id = $1::uuid AND state = 'skipped'`, p3.ID).Scan(&skipped); err != nil || skipped != 200 {
		t.Fatalf("skipped = %d (%v), want the 200 unwritten pens", skipped, err)
	}
	if n := h.countRows(domain.RegParks, "Gamma"); n != 1 {
		t.Fatalf("Gamma parks = %d, want the written tab kept", n)
	}
}
