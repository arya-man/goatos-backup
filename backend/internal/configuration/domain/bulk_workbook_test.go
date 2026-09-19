package domain

import (
	"reflect"
	"strings"
	"testing"
)

// TestWorkbookOrderRespectsEveryReference pins the dependency order the onboarding workbook
// is worked in: a register comes after every register its ref columns (and its declared
// text dependencies) point at, and every importable static register except the hidden
// register OF reference lists has a tab. A register added with a new ref that is listed
// ahead of its target fails here, before a pens tab can validate against a parks tab that
// has not run.
func TestWorkbookOrderRespectsEveryReference(t *testing.T) {
	position := map[string]int{}
	for n, key := range WorkbookOrder {
		reg, ok := RegisterByKey(key)
		if !ok {
			t.Fatalf("WorkbookOrder names unknown register %q", key)
		}
		if !reg.Importable {
			t.Fatalf("WorkbookOrder names %q, which takes no upload", key)
		}
		if _, dup := position[key]; dup {
			t.Fatalf("WorkbookOrder names %q twice", key)
		}
		position[key] = n
	}
	for _, key := range WorkbookOrder {
		reg, _ := RegisterByKey(key)
		for _, dep := range WorkbookDependenciesOf(reg) {
			if dep == key {
				continue // a self reference (a list under a list) is resolved within the tab
			}
			at, ok := position[dep]
			if !ok {
				t.Fatalf("%s depends on %s, which has no tab", key, dep)
			}
			if at >= position[key] {
				t.Fatalf("%s (tab %d) depends on %s (tab %d): the dependency must come first", key, position[key], dep, at)
			}
		}
	}
	var missing []string
	for _, reg := range Registers {
		if _, ok := position[reg.Key]; ok || !reg.Importable {
			continue
		}
		missing = append(missing, reg.Key)
	}
	if want := []string{RegReferenceLists}; !reflect.DeepEqual(missing, want) {
		t.Fatalf("importable registers without a workbook tab = %v, want exactly %v (the hidden register of lists)", missing, want)
	}
}

func TestWorkbookDependenciesOfAnimalsNamesTheTextResolvedRegisters(t *testing.T) {
	reg, _ := RegisterByKey(RegAnimals)
	deps := WorkbookDependenciesOf(reg)
	for _, want := range []string{RegParks, RegPens, RegSpecies, RegSexes, RegStages} {
		found := false
		for _, d := range deps {
			if d == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("animals dependencies %v lack %s", deps, want)
		}
	}
}

// TestMatchSheetNameReadsTheWordsAPersonTypes pins that a tab named by key, label or singular
// noun -- in any case, with or without spaces -- lands on the register, and that a name no
// register carries lands nowhere.
func TestMatchSheetNameReadsTheWordsAPersonTypes(t *testing.T) {
	cases := map[string]string{
		"Parks":              RegParks,
		"parks":              RegParks,
		"Park":               RegParks,
		"Pens":               RegPens,
		"Lifecycle stages":   RegStages,
		"lifecycle_stages":   RegStages,
		"STAGES":             RegStages,
		"Items & categories": RegItems,
		"items":              RegItems,
		"Lists":              RegCategories,
		"categories":         RegCategories,
		"Gender":             RegSexes,
		"sexes":              RegSexes,
		"Animals":            RegAnimals,
		"Species":            RegSpecies,
		"Breeds":             "", // read-only: no tab
		"Task types":         RegTaskTypes,
		"Notes":              "",
		"Sheet1":             "",
		"":                   "",
		"Roles":              "", // read-only: no tab
		"Feed items":         "", // read-only: no tab
	}
	for name, want := range cases {
		if got := MatchSheetName(name, Registers); got != want {
			t.Fatalf("MatchSheetName(%q) = %q, want %q", name, got, want)
		}
	}
	// A tenant's own reference list matches by its name or its key.
	lists := append([]Register{}, Registers...)
	lists = append(lists, ReferenceRegister(ReferenceList{Key: "cull_reasons", Name: "Cull reasons"}))
	for _, name := range []string{"Cull reasons", "cull_reasons", "CULL REASONS"} {
		if got := MatchSheetName(name, lists); got != RefPrefix+"cull_reasons" {
			t.Fatalf("MatchSheetName(%q) = %q, want the reference list", name, got)
		}
	}
}

func TestBundleRowTokenRoundTrips(t *testing.T) {
	token := BundleRowToken("22222222-2222-2222-2222-222222222222", 41)
	if !strings.HasPrefix(token, BundleRowTokenPrefix) {
		t.Fatalf("token %q lacks the prefix", token)
	}
	job, row, ok := ParseBundleRowToken(token)
	if !ok || job != "22222222-2222-2222-2222-222222222222" || row != 41 {
		t.Fatalf("parse = %q %d %v", job, row, ok)
	}
	for _, bad := range []string{"", "22222222-2222-2222-2222-222222222222", "bundle-row:", "bundle-row:x", "bundle-row:x:0", "bundle-row:x:-1", "bundle-row::3"} {
		if _, _, ok := ParseBundleRowToken(bad); ok {
			t.Fatalf("%q parsed as a token", bad)
		}
	}
}

// TestScopedRefIndexResolvesWithinTheParent pins the pen-by-park behaviour: "Castro" exists in
// both parks, so unscoped it is ambiguous, scoped to a park it is that park's pen, and a
// pending (sibling-tab) pen under a park is found only within that park while its token is
// reachable from anywhere.
func TestScopedRefIndexResolvesWithinTheParent(t *testing.T) {
	idx := NewScopedRefIndex(
		[]string{"pen-cbe-castro", "pen-cpt-castro", "pen-cbe-gandhi"},
		[]string{"Castro", "Castro", "Gandhi"},
		[]string{"park-cbe", "park-cpt", "park-cbe"},
	)
	if _, _, ok := idx.Resolve("", "Castro"); ok {
		t.Fatal("unscoped Castro resolved although two parks have one")
	}
	if id, _, ok := idx.Resolve("park-cbe", "castro"); !ok || id != "pen-cbe-castro" {
		t.Fatalf("scoped Castro = %q %v", id, ok)
	}
	if id, _, ok := idx.Resolve("park-cpt", "Castro"); !ok || id != "pen-cpt-castro" {
		t.Fatalf("scoped Castro (CPT) = %q %v", id, ok)
	}
	if _, msg, ok := idx.Resolve("park-cpt", "Gandhi"); ok || !strings.Contains(msg, "no active row") {
		t.Fatalf("Gandhi under CPT = ok=%v msg=%q", ok, msg)
	}
	if id, _, ok := idx.Resolve("park-cpt", "pen-cbe-gandhi"); !ok || id != "pen-cbe-gandhi" {
		t.Fatalf("an id is accepted from anywhere: %q %v", id, ok)
	}
	// A pending pen "Nehru" on the parks tab's token park.
	idx.AddPending("bundle-row:job:7", "Nehru", "bundle-row:parks:2")
	if id, _, ok := idx.Resolve("bundle-row:parks:2", "nehru"); !ok || id != "bundle-row:job:7" {
		t.Fatalf("pending pen under pending park = %q %v", id, ok)
	}
	if _, _, ok := idx.Resolve("park-cbe", "Nehru"); ok {
		t.Fatal("pending pen resolved under another park")
	}
	if id, _, ok := idx.Resolve("", "bundle-row:job:7"); !ok || id != "bundle-row:job:7" {
		t.Fatal("pending token not reachable by id")
	}
	// A pending row whose name a stored row already carries under that parent is left to the
	// stored row.
	idx.AddPending("bundle-row:job:8", "Castro", "park-cbe")
	if id, _, ok := idx.Resolve("park-cbe", "Castro"); !ok || id != "pen-cbe-castro" {
		t.Fatalf("stored row lost to a pending duplicate: %q", id)
	}
}

func TestParentColumnFindsTheSharedScope(t *testing.T) {
	partitions, _ := RegisterByKey(RegPartitions)
	pens, _ := RegisterByKey(RegPens)
	parks, _ := RegisterByKey(RegParks)
	if got := ParentColumn(partitions, pens, "pen_id"); got != "park_id" {
		t.Fatalf("partitions.pen_id is scoped by %q, want park_id", got)
	}
	if got := ParentColumn(partitions, parks, "park_id"); got != "" {
		t.Fatalf("partitions.park_id is scoped by %q, want nothing", got)
	}
	items, _ := RegisterByKey(RegItems)
	categories, _ := RegisterByKey(RegCategories)
	if got := ParentColumn(items, categories, "category_id"); got != "" {
		t.Fatalf("items.category_id is scoped by %q, want nothing", got)
	}
}
