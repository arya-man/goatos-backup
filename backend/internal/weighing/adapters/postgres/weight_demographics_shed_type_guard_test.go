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
// Pen type is now CONFIGURED per pen (maintainer instruction 2026-09-22, migration 000385) and
// read from shed_profiles.shed_type. This test is the old one inverted, and it is the more
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

	if !strings.Contains(query, "COALESCE(sp.shed_type, parent_sp.shed_type)") {
		t.Fatal("the pen-type CTE must read the configured shed_profiles.shed_type, falling back only to the parent pen's")
	}

	// The building names, lowercased as the retired regex had them. Any of them reappearing in
	// a classification position means the guess is back.
	for _, name := range []string{"gandhi", "castro", "ho chi minh", "mandela", "godel", "sumathi", "yashoda"} {
		if strings.Contains(strings.ToLower(query), "'\\m("+name) || strings.Contains(strings.ToLower(query), "|"+name+"|") {
			t.Fatalf("pen type is guessed from the pen name %q again; it is configured on Configuration -> Items and settings -> Pens", name)
		}
	}
	if strings.Contains(query, "'crown|crowned|ground'") || strings.Contains(query, "(crown|crowned|ground)") {
		t.Fatal("pen type is inferred from free-text notes again; it is configured per pen")
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
