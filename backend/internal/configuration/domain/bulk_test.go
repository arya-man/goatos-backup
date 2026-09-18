package domain

import (
	"strings"
	"testing"
)

func TestSheetColumnsCarryIdFirstAndStatusLast(t *testing.T) {
	cols := SheetColumns(mustRegister(t, RegPens))
	if cols[0].Key != SheetColumnID || cols[1].Key != SheetColumnRowVersion || cols[len(cols)-1].Key != SheetColumnStatus {
		t.Fatalf("columns = %v", cols)
	}
	animals := SheetColumns(mustRegister(t, RegAnimals))
	if animals[0].Key == SheetColumnID {
		t.Fatalf("a create-only register offers no id column")
	}
	if !mustRegister(t, RegAnimals).Importable || mustRegister(t, RegFeedItems).Importable || mustRegister(t, RegRoles).Importable || !mustRegister(t, RegPens).Importable {
		t.Fatalf("importable: animals yes, feed items no, roles no, pens yes")
	}
	if !ReferenceRegister(ReferenceList{Key: "x", Name: "X"}).Importable {
		t.Fatalf("a reference list takes a sheet")
	}
}

func TestProductWideRegistersStayReadOnly(t *testing.T) {
	for _, key := range []string{RegRoles, RegBreeds, RegStatusDefinitions} {
		reg := mustRegister(t, key)
		if !reg.ReadOnly || reg.Importable {
			t.Fatalf("%s read_only/importable = %v/%v, want read-only and not importable", key, reg.ReadOnly, reg.Importable)
		}
	}
}

func TestMatchHeaderAcceptsKeysAndLabelsAndReportsGaps(t *testing.T) {
	reg := mustRegister(t, RegPens)
	keys, unknown, missing := MatchHeader(reg, []string{"Park", "name", " Capacity ", "Gender", "colour", ""})
	if strings.Join(keys, ",") != "park_id,name,capacity,sex,," {
		t.Fatalf("keys = %v", keys)
	}
	if len(unknown) != 1 || unknown[0] != "colour" || len(missing) != 0 {
		t.Fatalf("unknown = %v missing = %v", unknown, missing)
	}
	_, _, missing = MatchHeader(reg, []string{"name"})
	if strings.Join(missing, ",") != "park_id" {
		t.Fatalf("a create sheet without the park must report it: %v", missing)
	}
	_, _, missing = MatchHeader(reg, []string{"id", "capacity"})
	if len(missing) != 0 {
		t.Fatalf("an update sheet names only what it changes: %v", missing)
	}
	_, _, missing = MatchHeader(mustRegister(t, RegAnimals), []string{"id", "animal_identifier_1"})
	if len(missing) == 0 {
		t.Fatalf("a create-only register still needs its required columns")
	}
	keys, _, _ = MatchHeader(reg, []string{"name", "Name"})
	if keys[1] != "" {
		t.Fatalf("a repeated header is matched once: %v", keys)
	}
}

func TestSheetRowDropsBlankCells(t *testing.T) {
	row := SheetRow([]string{"park_id", "name", "", "capacity"}, []string{"CBE", " Castro ", "ignored", ""})
	if len(row) != 2 || row["name"] != "Castro" || row["park_id"] != "CBE" {
		t.Fatalf("row = %v", row)
	}
	if !IsBlankRecord([]string{"", "  "}) || IsBlankRecord([]string{"", "x"}) {
		t.Fatalf("blank record detection")
	}
}

func TestSheetCellRendersLabelsBoolsAndNumbers(t *testing.T) {
	row := Row{ID: "r1", Status: "active", Fields: map[string]any{"park_id": "p1", "has_icu": true, "capacity": float64(40), "name": "Castro"}, Labels: map[string]string{"park_id": "Coimbatore"}}
	reg := mustRegister(t, RegPens)
	get := func(key string) string {
		for _, c := range SheetColumns(reg) {
			if c.Key == key {
				return SheetCell(c, row)
			}
		}
		t.Fatalf("no column %s", key)
		return ""
	}
	row.RowVersion = 7
	if get("id") != "r1" || get("row_version") != "7" || get("status") != "active" || get("park_id") != "Coimbatore" || get("has_icu") != "yes" || get("capacity") != "40" || get("name") != "Castro" || get("notes") != "" {
		t.Fatalf("cells: %s %s %s %s %s", get("park_id"), get("has_icu"), get("capacity"), get("name"), get("notes"))
	}
}

func TestRefIndexResolvesLabelIdAndRefusesAmbiguity(t *testing.T) {
	idx := NewRefIndex([]string{"a", "b", "c", "a"}, []string{"Castro", "Castro", "Gandhi", "CBE_CASTRO"})
	if id, _, ok := idx.Resolve("gandhi"); !ok || id != "c" {
		t.Fatalf("label match: %s %v", id, ok)
	}
	if id, _, ok := idx.Resolve("b"); !ok || id != "b" {
		t.Fatalf("id match: %s %v", id, ok)
	}
	if id, _, ok := idx.Resolve("cbe_castro"); !ok || id != "a" {
		t.Fatalf("alias match: %s %v", id, ok)
	}
	if _, msg, ok := idx.Resolve("Castro"); ok || !strings.Contains(msg, "more than one") {
		t.Fatalf("ambiguous label must refuse: %v %q", ok, msg)
	}
	if _, msg, ok := idx.Resolve("Nowhere"); ok || !strings.Contains(msg, "no active row") {
		t.Fatalf("unknown label must refuse: %v %q", ok, msg)
	}
}
