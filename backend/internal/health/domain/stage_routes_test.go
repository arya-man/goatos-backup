package domain

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

func routedDoe(band, stage string) GoatFacts {
	f := adultDoe()
	f.AgeBand = band
	f.ManagementStage = stage
	return f
}

// The rule that makes a farm able to peel ONE stage off a broad one: an exact stage beats the
// band wildcard. Without it, adding `Mother -> mothers` beside the adult wildcard would be a
// coin toss, and the farm could not split a cohort without deleting the rule that serves
// everybody else.
func TestExactStageBeatsTheBandWildcard(t *testing.T) {
	routing := NewStageRouting([]StageRoute{
		{AgeBand: AgeBandAdult, StageCode: StageWildcard, TypeKey: diagnosis.ClassAdult},
		{AgeBand: AgeBandAdult, StageCode: "mother", TypeKey: "mothers"},
	})

	got, err := ResolveAnimal(routedDoe(AgeBandAdult, "Mother"), routing)
	if err != nil {
		t.Fatalf("a mother must route: %v", err)
	}
	if got.Class != "mothers" {
		t.Errorf("class = %q, want the specific route to win with %q", got.Class, "mothers")
	}

	// Every OTHER adult stays on the wildcard. Splitting one cohort must not move the rest.
	for _, stage := range []string{"Non-Pregnant", "Buck", "ICU", ""} {
		other, err := ResolveAnimal(routedDoe(AgeBandAdult, stage), routing)
		if err != nil {
			t.Fatalf("stage %q must still route through the wildcard: %v", stage, err)
		}
		if other.Class != diagnosis.ClassAdult {
			t.Errorf("stage %q = %q, want it left on %q", stage, other.Class, diagnosis.ClassAdult)
		}
	}
}

// The whole point of the feature, as the maintainer put it: create a type, point a stage at it,
// no deploy. Warmup rides the fattening register today; here it moves to its own.
func TestAStageCanBeMovedOntoANewTypeWithoutCode(t *testing.T) {
	before := NewStageRouting(BuiltinStageRoutes())
	got, err := ResolveAnimal(routedDoe(AgeBandKid, "Warmup"), before)
	if err != nil || got.Class != diagnosis.ClassKidFattening {
		t.Fatalf("Warmup starts on fattening; got %q err=%v", got.Class, err)
	}

	after := NewStageRouting(append(BuiltinStageRoutes(),
		StageRoute{AgeBand: AgeBandKid, StageCode: "warmup", TypeKey: "kid_warmup"}))
	moved, err := ResolveAnimal(routedDoe(AgeBandKid, "Warmup"), after)
	if err != nil {
		t.Fatalf("after the edit Warmup must route: %v", err)
	}
	if moved.Class != "kid_warmup" {
		t.Errorf("class = %q, want %q", moved.Class, "kid_warmup")
	}

	// And the cohort it left keeps its own register.
	sibling, err := ResolveAnimal(routedDoe(AgeBandKid, "F2-Male"), after)
	if err != nil || sibling.Class != diagnosis.ClassKidFattening {
		t.Errorf("fattening kids must be untouched; got %q err=%v", sibling.Class, err)
	}
}

// THE SAFETY PROPERTY. An animal nothing routes is refused, and the refusal names the stage so
// a director can act on it. This is the 2026-08-17 decision, restated on authored data.
func TestAnUnroutedAnimalIsRefusedAndTheStageIsNamed(t *testing.T) {
	routing := NewStageRouting(BuiltinStageRoutes())

	got, err := ResolveAnimal(routedDoe(AgeBandKid, "K9-Experimental"), routing)
	if err == nil {
		t.Fatalf("an unrouted kid must be refused, got class %q", got.Class)
	}
	if !errors.Is(err, ErrGoatNotDiagnosable) {
		t.Errorf("error = %v, want it to wrap ErrGoatNotDiagnosable", err)
	}
	if !strings.Contains(err.Error(), "K9-Experimental") {
		t.Errorf("the refusal must name the stage a director can map; got %q", err)
	}
	if got.Class != "" {
		t.Errorf("a refused animal must carry no class, got %q", got.Class)
	}

	// A blank stage cannot be named, so it says the fact that IS actionable instead.
	_, blankErr := ResolveAnimal(routedDoe(AgeBandKid, "   "), routing)
	if blankErr == nil || !strings.Contains(blankErr.Error(), "no management stage recorded") {
		t.Errorf("a blank stage must say so plainly; got %v", blankErr)
	}
}

// An empty routing must NOT silently fall back to the shipped map. A farm whose routing was
// never seeded has to say so out loud -- the alternative is every animal judged against a table
// nobody chose.
func TestEmptyRoutingRefusesRatherThanFallingBack(t *testing.T) {
	empty := NewStageRouting(nil)
	if !empty.Empty() {
		t.Fatal("NewStageRouting(nil) must report itself empty")
	}
	for _, band := range []string{AgeBandAdult, AgeBandKid} {
		if _, err := ResolveAnimal(routedDoe(band, "K2"), empty); err == nil {
			t.Errorf("band %q resolved against an empty routing instead of refusing", band)
		}
	}
}

// Retiring a type is expressed by the ROUTE disappearing (the repository joins on active types),
// so the domain need not know retirement exists. This pins the consequence: an animal pointed at
// nothing behaves exactly like an unmapped stage rather than erroring differently.
func TestARouteToANowRetiredTypeReadsAsUnmapped(t *testing.T) {
	// The repository's join drops routes whose type is retired, so the domain simply sees fewer
	// rows -- here, every kid route gone.
	adultOnly := NewStageRouting([]StageRoute{
		{AgeBand: AgeBandAdult, StageCode: StageWildcard, TypeKey: diagnosis.ClassAdult},
	})
	_, err := ResolveAnimal(routedDoe(AgeBandKid, "K2"), adultOnly)
	if !errors.Is(err, ErrGoatNotDiagnosable) {
		t.Errorf("error = %v, want the ordinary unmapped refusal", err)
	}
}

// The catalog holds the same codes in more than one casing across import runs, which is why the
// shipped map compared case-insensitively. Authored rows must keep that.
func TestRoutingIsCaseAndSpaceInsensitive(t *testing.T) {
	routing := NewStageRouting([]StageRoute{
		{AgeBand: "Kid", StageCode: "  WARMUP ", TypeKey: "kid_warmup"},
	})
	for _, stage := range []string{"warmup", "Warmup", "  WarmUp  "} {
		got, err := ResolveAnimal(routedDoe(AgeBandKid, stage), routing)
		if err != nil || got.Class != "kid_warmup" {
			t.Errorf("stage %q = %q err=%v, want kid_warmup", stage, got.Class, err)
		}
	}
}

// THE SEED IS THE SHIPPED MAP, ROW FOR ROW.
//
// Migration 000395 moves routing out of Go, and the one thing that must not change on deploy is
// which register an animal reaches. This reads the migration's own VALUES block and compares it
// to the oracle, so a hand edit to either side that drifts from the other fails here rather than
// in a shed.
func TestMigrationSeedReproducesTheShippedRouting(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "postgres",
		"000395_health_diagnosis_types_and_stage_routes.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)

	block := sql[strings.Index(sql, "INSERT INTO public.health_diagnosis_stage_routes"):]
	block = block[:strings.Index(block, "AS v(age_band")]

	row := regexp.MustCompile(`\('(\w+)',\s*'([^']+)',\s*'(\w+)',\s*'(\w*)'\)`)
	seeded := map[string]StageRoute{}
	for _, m := range row.FindAllStringSubmatch(block, -1) {
		seeded[m[1]+"/"+m[2]] = StageRoute{AgeBand: m[1], StageCode: m[2], TypeKey: m[3], SubStage: m[4]}
	}

	want := BuiltinStageRoutes()
	if len(seeded) != len(want) {
		t.Fatalf("migration seeds %d routes, the oracle has %d", len(seeded), len(want))
	}
	for _, w := range want {
		got, ok := seeded[w.AgeBand+"/"+w.StageCode]
		if !ok {
			t.Errorf("migration does not seed %s/%s", w.AgeBand, w.StageCode)
			continue
		}
		if got.TypeKey != w.TypeKey || got.SubStage != w.SubStage {
			t.Errorf("%s/%s seeds (%s,%s), oracle says (%s,%s)",
				w.AgeBand, w.StageCode, got.TypeKey, got.SubStage, w.TypeKey, w.SubStage)
		}
	}
}

// A ROUTE ON A STAGE THE FARM DOES NOT HAVE IS A DEAD RULE.
//
// It matches nothing, for ever, and reports that as a zero in a column. On 2026-09-23 the
// maintainer typed `mothers` where the farm's stage is `Mother`; the screen accepted it, showed
// "0 animals", and five does stayed on the adult wildcard behind a rule that looked authored.
//
// The refusal itself is enforced in the repository, where the stage catalog is readable. What is
// pinned here is the CONSEQUENCE, so the reason the refusal exists cannot be argued away: a route
// whose stage no animal is on changes nothing about which register they reach.
func TestARouteOnAStageNoAnimalIsOnChangesNothing(t *testing.T) {
	routing := NewStageRouting([]StageRoute{
		{AgeBand: AgeBandAdult, StageCode: StageWildcard, TypeKey: diagnosis.ClassAdult},
		// The typo: the farm's stage is "Mother".
		{AgeBand: AgeBandAdult, StageCode: "mothers", TypeKey: "mothers"},
	})

	got, err := ResolveAnimal(routedDoe(AgeBandAdult, "Mother"), routing)
	if err != nil {
		t.Fatalf("a real mother must still route: %v", err)
	}
	if got.Class != diagnosis.ClassAdult {
		t.Fatalf("class = %q; a dead route must not divert her, but it must not carry her either", got.Class)
	}
	// The whole point: the author believed they had routed mothers, and had not.
	if got.Class == "mothers" {
		t.Error("the typo must not be treated as the real stage")
	}
}
