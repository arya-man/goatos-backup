package postgres

import (
	"os"
	"strings"
	"testing"
)

// The Pen-wise comparison used to decide a pen's class INSIDE this query: it looked for the
// words elevated / crown / ground in the pen's free-text notes and, failing that, matched the
// pen's NAME against a list of buildings hardcoded in Go. A test used to pin that list.
//
// Pen type is now CONFIGURED per PARTITION (maintainer instructions 2026-09-22, migrations 000385
// then 000391) and read from shed_partitions.shed_type. This test is the old one inverted, and it
// is the more
// valuable of the two: the name list was the thing that made a pen built after it was written
// silently unclassifiable, and a future edit "restoring" a fallback would reintroduce exactly
// that. It would also make CLEARING a pen's type on screen do nothing, because the guess would
// immediately re-assert the answer the farm just removed.
func TestWeightDemographicsReadsTheConfiguredPenTypeAndNeverGuesses(t *testing.T) {
	text, err := os.ReadFile("weight_demographics.go")
	if err != nil {
		t.Fatalf("read weight_demographics.go: %v", err)
	}
	query := string(text)

	if !strings.Contains(query, "alias_pen.shed_type") || !strings.Contains(query, "own_pen.shed_type") {
		t.Fatal("the pen-type CTE must read the configured shed_partitions.shed_type, resolving a pen by its alias row or by its parent plus label")
	}
	if strings.Contains(query, "shed_profiles") {
		t.Fatal("the pen type moved to the partition in 000391; reading shed_profiles here would resurrect the retired column")
	}

	// The building names, lowercased as the retired regex had them. Any of them reappearing in
	// a classification position means the guess is back.
	for _, name := range []string{"gandhi", "castro", "ho chi minh", "mandela", "godel", "sumathi", "yashoda"} {
		if strings.Contains(strings.ToLower(query), "'\\m("+name) || strings.Contains(strings.ToLower(query), "|"+name+"|") {
			t.Fatalf("pen type is guessed from the pen name %q again; it is configured on Configuration -> Items and settings -> Partitions", name)
		}
	}
	if strings.Contains(query, "'crown|crowned|ground'") || strings.Contains(query, "(crown|crowned|ground)") {
		t.Fatal("pen type is inferred from free-text notes again; it is configured per partition")
	}

	// The retired second class was keyed 'ground' and labelled Crown/Ground. One farm concept,
	// one name: it is non_elevated everywhere now.
	if strings.Contains(query, "'ground'") {
		t.Fatal("the retired 'ground' pen-type key is back; the key is 'non_elevated'")
	}
}

// The names the retired inference used are not lost: migration 000385 wrote its answers into
// the new column ONCE, so no chart moved on the day the column landed. This pins that the
// one-time backfill carries them, which is the only place they still belong.
func TestPenTypeBackfillCarriesTheRetiredNameList(t *testing.T) {
	text, err := os.ReadFile("../../../../migrations/postgres/000385_shed_profiles_shed_type.sql")
	if err != nil {
		t.Fatalf("read migration 000385: %v", err)
	}
	migration := string(text)
	for _, name := range []string{"gandhi", "castro", "ho chi minh", "old yashoda", "yashoda old"} {
		if !strings.Contains(migration, name) {
			t.Fatalf("the non-elevated backfill is missing %q, so that pen lands unclassified and drops off both bars", name)
		}
	}
	for _, name := range []string{"mandela", "godel", "sumathi", "new yashoda", "yashoda new"} {
		if !strings.Contains(migration, name) {
			t.Fatalf("the elevated backfill is missing %q, so that pen lands unclassified and drops off both bars", name)
		}
	}
	// The word "ground" still appears in the backfill, as one of the NOTE words it reads a pen's
	// old classification out of. What must never appear is it being WRITTEN as a class.
	if !strings.Contains(migration, "THEN 'non_elevated'") {
		t.Fatal("the backfill must write the non_elevated key")
	}
	if strings.Contains(migration, "THEN 'ground'") {
		t.Fatal("the backfill writes the retired 'ground' key; the key is 'non_elevated'")
	}
}

// PEN TYPES ARE THE FARM'S OWN LIST (migration 000428, maintainer instruction 2026-09-25). The
// decoders used to drop any bucket whose code was not elevated or non_elevated, so a third kind of
// pen authored on Configuration -> Pen types would have been weighed, grouped by the SQL, and then
// silently thrown away here -- a missing bar with no error anywhere. Any non-empty code is kept;
// the chart takes each code's name from the page contract.
func TestShedTypeDecodersKeepAPenTypeTheFarmAdded(t *testing.T) {
	buckets, err := decodeWeightGainShedTypeBuckets([]byte(`[["Beetal","slatted_floor",4,151.5],["Beetal","elevated",6,140],["Beetal","",3,90]]`))
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, b := range buckets {
		types = append(types, b.ShedType)
	}
	if strings.Join(types, ",") != "slatted_floor,elevated" {
		t.Fatalf("decoded bucket types = %v, want the farm-added type kept and only the blank one dropped", types)
	}
	members, err := decodeShedTypeMembers([]byte(`[["Beetal","slatted_floor","aaaaaaaa-0000-4000-8000-000000000001",null,"Godel 3","p-cbe","Coimbatore"]]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].ShedType != "slatted_floor" {
		t.Fatalf("decoded members = %+v, want the farm-added type's pen listed", members)
	}
}

// Weighing names no pen type of its own: the code is the bucket key and nothing more.
func TestWeighingNamesNoPenTypeOfItsOwn(t *testing.T) {
	for _, file := range []string{"weight_demographics.go", "../../domain/weight_demographics.go"} {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(text), `"elevated"`) || strings.Contains(string(text), `"non_elevated"`) {
			t.Fatalf("%s names a pen type literally; pen types come from the Pen types register", file)
		}
	}
}
