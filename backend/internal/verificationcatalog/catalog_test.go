package verificationcatalog

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
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
