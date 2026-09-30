package postgres

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/configuration/domain"
	"github.com/vgoats/goatos/backend/internal/platform/pgtest"
)

// TestConfigurationWorkbookLoad is the volume proof for the onboarding workbook (maintainer
// instruction 2026-09-19: "test with two lakh, three lakh rows"). It is opt-in --
// GOATOS_RUN_POSTGRES_TESTS=1 plus GOATOS_WORKBOOK_LOAD_ROWS=<n> -- because a run of a lakh rows
// takes minutes and the numbers it prints are the point: stage, validate and apply wall time,
// rows per second, and the process's peak heap while a workbook of that size moves through.
//
// The workbook is the farm's real shape scaled up: two parks, 200 pens (100 per park, the SAME
// names in both), a lists tab, an items tab carrying most of the rows, and an animals tab of
// GOATOS_WORKBOOK_LOAD_ANIMALS rows (default 1,000; the herd register validates each one with
// its own queries, so over a tunnel it is the slow tab) with a duplicate tag placed far apart so
// the whole-sheet index has to catch it across identity chunks. A row in every 1,000 is bad on
// purpose, so the error paths run at volume too.
func TestConfigurationWorkbookLoad(t *testing.T) {
	pgtest.SkipIfNoDocker(t)
	rows, _ := strconv.Atoi(os.Getenv("GOATOS_WORKBOOK_LOAD_ROWS"))
	if rows <= 0 {
		t.Skip("set GOATOS_WORKBOOK_LOAD_ROWS to run the volume proof")
	}
	animalsN, _ := strconv.Atoi(os.Getenv("GOATOS_WORKBOOK_LOAD_ANIMALS"))
	if animalsN <= 0 {
		animalsN = 1000
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	pool := pgtest.StartPostgres(t, ctx)
	seedConfigurationFixture(t, ctx, pool)
	h := newWorkbookHarness(t, ctx, pool, "wb-load")

	// Peak heap sampler.
	var peak atomic.Uint64
	stop := make(chan struct{})
	go func() {
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(250 * time.Millisecond):
				runtime.ReadMemStats(&m)
				if m.HeapInuse > peak.Load() {
					peak.Store(m.HeapInuse)
				}
			}
		}
	}()
	defer close(stop)

	const parksN, pensPerPark = 2, 100
	parks := [][]string{{"name", "code", "capacity"}, {"Load park A", "LPA", "5000"}, {"Load park B", "LPB", "5000"}}
	pens := [][]string{{"park_id", "name"}}
	for n := 1; n <= pensPerPark; n++ {
		pens = append(pens, []string{"LPA", "Load pen " + strconv.Itoa(n)})
		pens = append(pens, []string{"Load park B", "Load pen " + strconv.Itoa(n)})
	}
	lists := [][]string{{"name", "parent_id", "kind"}, {"Load medicines", "", "medicine"}, {"Load antibiotics", "Load medicines", ""}, {"Load feeds", "", "feed"}}
	items := [][]string{{"name", "category_id", "unit", "route", "strength", "withdrawal_days"}}
	badItems := 0
	for n := 1; n <= rows; n++ {
		category := "Load antibiotics"
		if n%3 == 0 {
			category = "Load feeds"
		}
		row := []string{"Load item " + strconv.Itoa(n), category, "ml", "im", "10 mg/ml", "7"}
		if category == "Load feeds" {
			row = []string{"Load item " + strconv.Itoa(n), category, "kg", "", "", ""}
		}
		if n%1000 == 0 {
			row[2] = "" // unit is required: invalid
			badItems++
		}
		items = append(items, row)
	}
	animals := [][]string{{"animal_identifier_1", "species", "sex", "park", "pen_name", "management_stage", "dob", "origin"}}
	for n := 1; n <= animalsN; n++ {
		park, pen := "LPA", "Load pen "+strconv.Itoa(1+n%pensPerPark)
		if n%2 == 0 {
			park = "Load park B"
		}
		tag := "LOAD-" + strconv.Itoa(n)
		if n == animalsN-1 {
			tag = "LOAD-2" // the same tag as row 3, far apart: caught by the sheet-wide index
		}
		animals = append(animals, []string{tag, "goat", "female", park, pen, "F2-Female", "2025-01-01", "procured"})
	}
	file := (&workbook{}).tab("Parks", parks...).tab("Pens", pens...).tab("Lists", lists...).tab("Items & categories", items...).tab("Animals", animals...).bytes(t)
	t.Logf("workbook: %d item rows, %d animal rows, %.1f MB xlsx", rows, animalsN, float64(len(file))/1e6)

	started := time.Now()
	bundle := h.stage("load.xlsx", file)
	stageTook := time.Since(started)
	t.Logf("STAGE   %d rows in %s (%.0f rows/s)", rows+animalsN+2*pensPerPark+5, stageTook, float64(rows+animalsN)/stageTook.Seconds())

	validateStart := time.Now()
	bundle = h.waitBundle(bundle.ID, 5*time.Hour, domain.ImportPreviewed, domain.ImportFailed)
	validateTook := time.Since(validateStart)
	if bundle.Status != domain.ImportPreviewed {
		t.Fatalf("preview: %s (%s) %s", bundle.Status, bundle.Error, describeTabs(bundle))
	}
	itemsTab := h.tabByRegister(bundle, domain.RegItems)
	animalsTab := h.tabByRegister(bundle, domain.RegAnimals)
	t.Logf("VALIDATE all tabs in %s (%.0f rows/s); items v=%d i=%d; animals v=%d i=%d", validateTook, float64(rows+animalsN)/validateTook.Seconds(), itemsTab.ValidRows, itemsTab.InvalidRows, animalsTab.ValidRows, animalsTab.InvalidRows)
	if itemsTab.ValidRows != rows-badItems || itemsTab.InvalidRows != badItems {
		t.Fatalf("items preview v=%d i=%d, want v=%d i=%d", itemsTab.ValidRows, itemsTab.InvalidRows, rows-badItems, badItems)
	}
	if animalsTab.ValidRows != animalsN-1 || animalsTab.InvalidRows != 1 {
		t.Fatalf("animals preview v=%d i=%d, want the far-apart duplicate tag caught", animalsTab.ValidRows, animalsTab.InvalidRows)
	}
	dup := h.rowsIn(animalsTab.ID, domain.ImportRowInvalid)
	if len(dup) != 1 || dup[0].RowNo != animalsN || dup[0].Errors[0].Code != "duplicate" {
		t.Fatalf("duplicate animal = %+v", dup)
	}
	if h.tabByRegister(bundle, domain.RegPens).ValidRows != 2*pensPerPark {
		t.Fatalf("pens preview: %s", describeTabs(bundle))
	}

	// Cross-check the error workbook at volume.
	var errBuf bytes.Buffer
	errStart := time.Now()
	if err := h.svc.BundleErrorSheet(ctx, h.repo, cfgTenant, bundle.ID, domain.FormatXLSX, &errBuf); err != nil {
		t.Fatalf("error workbook: %v", err)
	}
	t.Logf("ERRORS  workbook of %d rows in %s (%.1f KB)", badItems+1, time.Since(errStart), float64(errBuf.Len())/1e3)

	if _, ok, err := h.repo.RequestImportBundleApply(ctx, cfgTenant, bundle.ID, cfgActor); err != nil || !ok {
		t.Fatalf("request apply: %v %v", ok, err)
	}
	applyStart := time.Now()
	// Drive the apply the way production does: kicks and sweeps, each bounded by the job
	// timeout, until the bundle finishes.
	for {
		if err := h.importer.ProcessBundle(ctx, cfgTenant, bundle.ID); err != nil {
			t.Fatalf("process: %v", err)
		}
		bundle, _ = h.repo.GetImportBundle(ctx, cfgTenant, bundle.ID)
		if bundle.Status != domain.ImportApplying {
			break
		}
	}
	applyTook := time.Since(applyStart)
	if bundle.Status != domain.ImportApplied {
		t.Fatalf("apply: %s (%s) %s", bundle.Status, bundle.Error, describeTabs(bundle))
	}
	itemsTab = h.tabByRegister(bundle, domain.RegItems)
	animalsTab = h.tabByRegister(bundle, domain.RegAnimals)
	t.Logf("APPLY   all tabs in %s (%.0f rows/s); items a=%d f=%d; animals a=%d f=%d", applyTook, float64(rows-badItems+animalsN-1)/applyTook.Seconds(), itemsTab.AppliedRows, itemsTab.FailedRows, animalsTab.AppliedRows, animalsTab.FailedRows)
	if itemsTab.AppliedRows != rows-badItems || itemsTab.FailedRows != 0 {
		t.Fatalf("items apply a=%d f=%d", itemsTab.AppliedRows, itemsTab.FailedRows)
	}
	if animalsTab.AppliedRows != animalsN-1 || animalsTab.FailedRows != 0 {
		t.Fatalf("animals apply a=%d f=%d", animalsTab.AppliedRows, animalsTab.FailedRows)
	}
	var writtenItems, writtenAnimals, writtenPens int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory_items WHERE tenant_id = $1::uuid AND name LIKE 'Load item %'`, cfgTenant).Scan(&writtenItems); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM goat_identifiers gi JOIN goats g ON g.goat_id = gi.goat_id WHERE g.tenant_id = $1::uuid AND gi.identifier_value LIKE 'LOAD-%'`, cfgTenant).Scan(&writtenAnimals); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM locations WHERE tenant_id = $1::uuid AND location_type = 'shed' AND name LIKE 'Load pen %'`, cfgTenant).Scan(&writtenPens); err != nil {
		t.Fatal(err)
	}
	if writtenItems != rows-badItems || writtenAnimals != animalsN-1 || writtenPens != 2*pensPerPark {
		t.Fatalf("written items=%d animals=%d pens=%d, want %d / %d / %d", writtenItems, writtenAnimals, writtenPens, rows-badItems, animalsN-1, 2*pensPerPark)
	}
	t.Logf("PEAK HEAP %.1f MB across stage + validate + apply of %d rows", float64(peak.Load())/1e6, rows+animalsN)
	t.Logf("TOTAL   %s", time.Since(started))
}
