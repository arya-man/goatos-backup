package verificationcatalog

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	weighingdomain "github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// TestEveryCategoryIsNamedOnceAndCarriesDisplayCopy: the catalog is read by the API's registry and
// by the kernel worker's randomization closeout, and a row with no navigation copy has no name a
// CEO or a verifier could read -- the copy firewall bans showing the raw token instead.
func TestEveryCategoryIsNamedOnceAndCarriesDisplayCopy(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range All() {
		if def.Category == "" {
			t.Fatalf("catalog entry with no category: %+v", def)
		}
		if seen[def.Category] {
			t.Fatalf("category %q appears twice; registration would fail at boot", def.Category)
		}
		seen[def.Category] = true
		if def.NavigationModule == "" || def.NavigationModuleLabel == "" || def.PageLabel == "" {
			t.Fatalf("category %q carries no display copy (module=%q label=%q page=%q)",
				def.Category, def.NavigationModule, def.NavigationModuleLabel, def.PageLabel)
		}
	}
	if len(seen) < 13 {
		t.Fatalf("catalog holds %d categories; it is meant to be the WHOLE registered set", len(seen))
	}
}

// TestBootstrapDeclaresNoCategoryOfItsOwn is the guard that keeps this package honest.
//
// The failure it prevents is silent, which is why it is worth a source check: a category declared
// INLINE in bootstrap/api.go registers fine in the API and is simply absent from the worker's
// registry, so the randomization closeout never settles it -- and its producer's records wait
// forever on a review the policy already decided nobody would do. Nothing errors; work just stops.
func TestBootstrapDeclaresNoCategoryOfItsOwn(t *testing.T) {
	path := filepath.Join("..", "bootstrap", "api.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	// Two shapes of inline declaration, and only those. A literal is recognised by a FIELD name
	// after the brace (`{Vertical:`), which is what separates it from
	// `[]verificationdomain.CategoryDefinition{ verificationcatalog.Weighing, ... }` -- a slice that
	// merely holds catalog references and is exactly what this file wants api.go to contain.
	for _, inline := range []*regexp.Regexp{
		// A named literal: verificationdomain.CategoryDefinition{Vertical: ...}
		regexp.MustCompile(`verificationdomain\.CategoryDefinition\{\s*[A-Za-z]\w*:`),
		// An anonymous element inside a slice of them: []verificationdomain.CategoryDefinition{{...
		regexp.MustCompile(`\[\]verificationdomain\.CategoryDefinition\{\s*\{`),
	} {
		loc := inline.FindIndex(source)
		if loc == nil {
			continue
		}
		line := 1 + strings.Count(string(source[:loc[0]]), "\n")
		t.Fatalf("bootstrap/api.go:%d declares a verification category inline. Add it to "+
			"verificationcatalog instead: a category the kernel worker's registry has never heard of "+
			"is never settled by the randomization closeout, and its producer's records wait forever.", line)
	}
}

// BLIND WEIGHING VERIFICATION (maintainer decision 2026-09-21).
//
// The verifier is not shown the operator's weight and must record her own, which becomes the
// recorded weight of that animal or that pen. These pin the two halves of that, because either one
// alone is worse than neither: a required field beside a visible operator weight is a rubber stamp
// with extra typing, and a hidden weight with an optional field is an approve that silently keeps
// a number nobody on the verifying side ever saw.
func TestWeighingApproveRequiresTheVerifiersOwnWeightReading(t *testing.T) {
	spec := Weighing.MeasurementCorrection
	if spec == nil {
		t.Fatal("weighing must declare a measurement the verifier records")
	}
	if !spec.RequiredForApprove {
		t.Fatal("weighing's approve must carry the verifier's weight reading; a blank approve would " +
			"record a weight she was never shown")
	}
	// The count stays absent: the lump-sum head count is frozen from the herd register at submit
	// (2026-08-24) and is nobody's to edit, the verifier included.
	if spec.CountLabel != "" {
		t.Fatalf("weighing must offer no head-count field, got %q", spec.CountLabel)
	}
}

// The other half, asserted on the label the verifier actually reads. weighingdomain composes it and
// both surfaces -- her admin-web drawer and her phone -- render it verbatim, so one assertion here
// covers both.
func TestWeighingSubjectLabelShowsTheVerifierNoOperatorWeight(t *testing.T) {
	for _, tc := range []struct {
		name, refType, shed, tag string
		count                    int
	}{
		{"individual", weighingdomain.VerificationRefTypeAnimal, "Castro 2", "9010123", 0},
		{"lump sum", weighingdomain.VerificationRefTypeShed, "Godel 1 - Part 3", "", 31},
	} {
		t.Run(tc.name, func(t *testing.T) {
			label := weighingdomain.CorrectedSubjectLabel(tc.refType, tc.shed, tc.tag, tc.count)
			if strings.Contains(label, "kg") {
				t.Fatalf("the verifier's label must carry no weight, got %q", label)
			}
		})
	}
}

// Requiring the measurement LOCKS weighing sampling at 100%, and that is derived from the spec
// rather than listed anywhere -- so it is asserted here, beside the spec that causes it.
//
// It matters because the two must move together. The closeout stops settling unsampled weighing
// items the moment weighing becomes non-waivable (it must: auto-approving one would complete a
// bucket with no verifier reading), so if the panel still let a CEO narrow the queue those items
// would sit pending forever against an unconditional close gate. Migration 000254 clears the rows
// already stored; this keeps the two halves from drifting apart later.
func TestWeighingSamplingIsLockedBecauseTheVerifierIsTheDataSource(t *testing.T) {
	if Weighing.SamplingWaivable() {
		t.Fatal("weighing must not be waivable: an unwatched video would complete a weighing " +
			"bucket with no verifier weight recorded")
	}
	// Vaccination stays samplable, which is what proves the lock is derived from the declared
	// measurement and is not a blanket change to every category.
	if !Vaccination.SamplingWaivable() {
		t.Fatal("vaccination declares no measurement and must stay samplable")
	}
}

// The 000254 migration clears weighing's stored sampling rows, and it can only do that by naming
// the category as a SQL literal. The first draft named "weighing" -- the NAVIGATION MODULE key --
// and would have deleted nothing while reading as done, leaving the exact stranding it exists to
// prevent. This pins the literal to the Go constant so the two cannot drift.
func TestSamplingLockMigrationNamesTheRealWeighingCategory(t *testing.T) {
	path := filepath.Join("..", "..", "migrations", "postgres", "000383_weighing_blind_verification.sql")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(body)
	want := "WHERE category = '" + weighingdomain.VerificationCategoryWeighing + "';"
	if !strings.Contains(sql, want) {
		t.Fatalf("migration must delete rows for %q; no line %q found",
			weighingdomain.VerificationCategoryWeighing, want)
	}
	// The module key is a different token and must never be the one deleted on.
	if strings.Contains(sql, "WHERE category = 'weighing';") {
		t.Fatal("migration deletes on the navigation module key 'weighing', which matches no row")
	}
}
