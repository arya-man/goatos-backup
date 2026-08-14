package app

import (
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/adminui/domain"
)

func shedDirectoryTable(t *testing.T, pages []domain.PageContract) domain.TableContract {
	t.Helper()
	for _, page := range pages {
		if page.RouteID != "counts-sheds" {
			continue
		}
		for _, table := range page.Tables {
			if table.ID == "shed-directory" {
				return table
			}
		}
		t.Fatal("counts-sheds page has no shed-directory table")
	}
	t.Fatal("no counts-sheds page contract")
	return domain.TableContract{}
}

// TestShedDirectoryColumnsComeFromTheLiveParkFamily is the rule this compile step exists for: the
// park half of every header is tenant data. A third park opening must produce its column pair with
// no code change, and no park code may appear as a literal in contract code.
func TestShedDirectoryColumnsComeFromTheLiveParkFamily(t *testing.T) {
	compiled := compileShedDirectoryColumns(
		[]domain.TableContract{{ID: "shed-directory", Columns: []domain.Column{{Key: "shed", Label: "Shed", Visible: true}}}},
		[]ReferenceOption{
			{Key: "park-cbe", Label: "Coimbatore"},
			{Key: "park-cpt", Label: "Channapatna"},
			{Key: "park-new", Label: "Hosur"},
		},
	)

	var keys, labels []string
	for _, column := range compiled[0].Columns {
		keys = append(keys, column.Key)
		labels = append(labels, column.Label)
	}

	want := []string{"shed", "tag:park-cbe", "capacity:park-cbe", "tag:park-cpt", "capacity:park-cpt", "tag:park-new", "capacity:park-new"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Fatalf("columns not one pair per live park:\n got %v\nwant %v", keys, want)
	}
	// The key carries the park ID, never the code or the name -- a park rename must not
	// re-associate a shed with another park's column.
	for _, label := range labels {
		if label == "Hosur tag" {
			return
		}
	}
	t.Fatalf("a newly opened park produced no header of its own: %v", labels)
}

// TestShedDirectoryColumnsLeaveOtherTablesAlone pins that the compile step is scoped to its own
// table id. It runs over every table on the page, and appending park columns to a neighbour would
// break that table's renderer, which requires a cell for each declared column.
func TestShedDirectoryColumnsLeaveOtherTablesAlone(t *testing.T) {
	compiled := compileShedDirectoryColumns(
		[]domain.TableContract{
			{ID: "some-other-table", Columns: []domain.Column{{Key: "a", Label: "A", Visible: true}}},
			{ID: "shed-directory", Columns: []domain.Column{{Key: "shed", Label: "Shed", Visible: true}}},
		},
		[]ReferenceOption{{Key: "park-cbe", Label: "Coimbatore"}},
	)

	if len(compiled[0].Columns) != 1 {
		t.Fatalf("a neighbouring table gained park columns: %+v", compiled[0].Columns)
	}
	if len(compiled[1].Columns) != 3 {
		t.Fatalf("shed-directory did not gain its park pair: %+v", compiled[1].Columns)
	}
}

// TestShedDirectoryPageIsAContractedCountsLeaf keeps the route, its crumb and its nav leaf moving
// together: a page contract with no nav entry is unreachable, and a nav entry with no contract
// fails closed at requireAdminWebPageContract.
func TestShedDirectoryPageIsAContractedCountsLeaf(t *testing.T) {
	table := shedDirectoryTable(t, pages())

	if table.DataSource != "/counts/sheds" {
		t.Fatalf("table reads %q, want /counts/sheds", table.DataSource)
	}

	var leafFound bool
	for _, group := range navigation().Groups {
		if group.ID != "counts" {
			continue
		}
		for _, leaf := range group.Leaves {
			if leaf.ID == "counts-sheds" && leaf.Href == "/counts/sheds" {
				leafFound = true
			}
		}
	}
	if !leafFound {
		t.Fatal("Sheds is not a leaf of the Counts nav group")
	}
}
