package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/health/domain"
)

// fakeDeathCauseSource stands in for the tenant's Health Config.
type fakeDeathCauseSource struct {
	sources domain.DeathCauseTenantSources
	err     error
	calls   int
}

func (f *fakeDeathCauseSource) DeathCauseSources(context.Context, string) (domain.DeathCauseTenantSources, error) {
	f.calls++
	return f.sources, f.err
}

// The treatment-tab diseases the farm's Health Config actually carries (read from the
// STG-like database on 2026-09-25), all published.
func shippedTreatmentDiseases() []domain.AuthoredDisease {
	names := map[string]string{
		"abscesses": "Abscesses", "acidosis": "Acidosis", "anemia": "Anemia", "arthritis": "Arthritis",
		"bloating": "Bloating", "body_edema": "Body Edema", "diarrhea": "Diarrhea", "dog_bite": "Dog Bite",
		"fever": "Fever", "fly_strike": "Fly strike", "foot_rot": "Foot rot", "fracture": "Fracture",
		"heat_stress": "Heat Stress", "horn_damage": "Horn Damage", "jaundice": "Jaundice", "lumps": "Lumps",
		"mastitis": "Mastitis", "milk_fever": "Milk fever", "not_eating": "Not Eating", "orf": "ORF",
		"pinkeye": "Pinkeye", "pregnancy_toxemia": "Pregnancy toxemia", "red_urine": "Red Urine",
		"skin_infections": "Skin Infections", "udder_edema": "Udder Edema", "wounds": "Wounds",
	}
	out := make([]domain.AuthoredDisease, 0, len(names))
	for key, label := range names {
		out = append(out, domain.AuthoredDisease{Key: key, Label: label, Active: true})
	}
	return out
}

func optionsByKey(catalog domain.DeathCauseCatalog) map[string]domain.DeathCauseOption {
	out := make(map[string]domain.DeathCauseOption, len(catalog.Options))
	for _, o := range catalog.Options {
		out[o.Key] = o
	}
	return out
}

// MAINTAINER DECISION 2026-09-25: the list is the built-in diseases PLUS every disease
// authored in Health Config -- one row per disease, never the same illness twice.
func TestDeathCausesAreTheBuiltInRegisterPlusHealthConfigDiseases(t *testing.T) {
	src := &fakeDeathCauseSource{sources: domain.DeathCauseTenantSources{Diseases: shippedTreatmentDiseases()}}
	svc := NewDeathCauseCatalogService().WithTenantSource(src)
	catalog, err := svc.Catalog(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	byKey := optionsByKey(catalog)

	// Diseases the register does not name arrive under their own stable disease key.
	for key, label := range map[string]string{
		"dog_bite": "Dog Bite", "horn_damage": "Horn Damage", "abscesses": "Abscesses", "not_eating": "Not Eating",
	} {
		option, ok := byKey[key]
		if !ok {
			t.Errorf("Health Config disease %s is not offered", key)
			continue
		}
		if option.Kind != domain.DeathCauseKindDiseaseKey || option.Label != label {
			t.Errorf("%s = %+v, want kind disease_key label %q", key, option, label)
		}
	}
	// The same illness is ONE row, the register's: same name (Mastitis), same key (foot_rot),
	// or the course a register diagnosis opens (bloating is BLOAT's, pregnancy_toxemia PREG_TOX's).
	for _, folded := range []string{"mastitis", "foot_rot", "bloating", "pregnancy_toxemia", "skin_infections", "fly_strike"} {
		if _, listed := byKey[folded]; listed {
			t.Errorf("%s is listed beside the register diagnosis it is", folded)
		}
	}
	for _, rule := range []string{"MASTITIS", "FOOT_ROT", "BLOAT", "PREG_TOX", "PPR"} {
		if _, ok := byKey[rule]; !ok {
			t.Errorf("built-in %s dropped from the list", rule)
		}
	}
	seen := map[string]string{}
	for _, o := range catalog.Options {
		if first, dup := seen[strings.ToLower(o.Label)]; dup {
			t.Errorf("%q labels both %s and %s", o.Label, first, o.Key)
		}
		seen[strings.ToLower(o.Label)] = o.Key
	}

	// Every active Health Config disease may be named on a new death -- a folded one too.
	for _, key := range []string{"dog_bite", "mastitis", "bloating"} {
		if err := svc.ValidateCause(context.Background(), "tenant-a", key, domain.DeathCauseKindDiseaseKey); err != nil {
			t.Errorf("active disease %s refused: %v", key, err)
		}
	}
}

// A published register the farm authored adds its own diagnoses to the list.
func TestAuthoredRegisterDiagnosesJoinTheList(t *testing.T) {
	src := &fakeDeathCauseSource{sources: domain.DeathCauseTenantSources{
		RegisterRules: []domain.RegisterRule{
			{ID: "SNAKE_BITE", Class: "adult"},
			{ID: "MASTITIS", Class: "mothers", Treats: "mastitis"},
		},
		RegisterVersions: []string{"adult-authored-v2"},
	}}
	catalog, err := NewDeathCauseCatalogService().WithTenantSource(src).Catalog(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	byKey := optionsByKey(catalog)
	if o := byKey["SNAKE_BITE"]; o.Kind != domain.DeathCauseKindRegisterRule || o.Label != "Snake bite" {
		t.Fatalf("authored diagnosis = %+v, want a register_rule labelled Snake bite", o)
	}
	if classes := byKey["MASTITIS"].AnimalClasses; !containsString(classes, "mothers") || !containsString(classes, "adult") {
		t.Errorf("MASTITIS classes = %v, want the authored type folded onto the one row", classes)
	}
	if !containsString(catalog.RegisterVersions, "adult-authored-v2") {
		t.Errorf("register versions = %v, want the authored version pinned", catalog.RegisterVersions)
	}
}

// RETIRED: refused on a NEW death, still named on the deaths already filed under it.
func TestARetiredDiseaseIsRefusedForANewDeathButKeepsItsNameOnHistory(t *testing.T) {
	src := &fakeDeathCauseSource{sources: domain.DeathCauseTenantSources{Diseases: []domain.AuthoredDisease{
		{Key: "dog_bite", Label: "Dog Bite", Active: true},
		{Key: "snake_bite", Label: "Snake bite", Active: false},
		{Key: "draft_only", Label: "Draft only", Active: false},
	}}}
	svc := NewDeathCauseCatalogService().WithTenantSource(src)
	ctx := context.Background()

	byKey := optionsByKey(mustCatalog(t, svc, "tenant-a"))
	if _, listed := byKey["snake_bite"]; listed {
		t.Errorf("a retired disease is offered to a new death")
	}
	if _, listed := byKey["draft_only"]; listed {
		t.Errorf("a never-published disease is offered to a new death")
	}
	if err := svc.ValidateCause(ctx, "tenant-a", "snake_bite", domain.DeathCauseKindDiseaseKey); err == nil {
		t.Errorf("a retired disease was accepted on a new death")
	}
	// The death already recorded under it reads by name on the mortality board.
	if got := svc.LabelDeathCause(ctx, "tenant-a", "snake_bite"); got != "Snake bite" {
		t.Errorf("history label = %q, want Snake bite", got)
	}
	// Unknown keys are still refused, under either kind.
	for _, cause := range []domain.DeathCause{
		{Key: "nope", Kind: domain.DeathCauseKindDiseaseKey},
		{Key: "NOPE", Kind: domain.DeathCauseKindRegisterRule},
		{Key: "dog_bite", Kind: "other"},
	} {
		if err := svc.ValidateCause(ctx, "tenant-a", cause.Key, cause.Kind); err == nil {
			t.Errorf("%+v was accepted", cause)
		}
	}
}

// The tenant half is cached briefly and dropped on a publish, never held forever.
func TestTenantDeathCausesAreCachedBrieflyAndDroppedOnPublish(t *testing.T) {
	src := &fakeDeathCauseSource{}
	svc := NewDeathCauseCatalogService().WithTenantSource(src)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	ctx := context.Background()

	mustCatalog(t, svc, "tenant-a")
	mustCatalog(t, svc, "tenant-a")
	if src.calls != 1 {
		t.Fatalf("reads = %d, want one read served from the cache", src.calls)
	}
	src.sources.Diseases = []domain.AuthoredDisease{{Key: "dog_bite", Label: "Dog Bite", Active: true}}
	svc.Invalidate("tenant-a")
	if _, ok := optionsByKey(mustCatalog(t, svc, "tenant-a"))["dog_bite"]; !ok {
		t.Fatalf("a disease published on this instance did not appear after Invalidate")
	}
	now = now.Add(tenantCatalogTTL + time.Second)
	mustCatalog(t, svc, "tenant-a")
	if src.calls != 3 {
		t.Fatalf("reads = %d, want a re-read once the entry expired", src.calls)
	}
	// Bounded: filling past the cap never grows the map past it.
	for i := 0; i < tenantCatalogMaxEntries+10; i++ {
		if _, err := svc.Catalog(ctx, "t-"+string(rune('a'+i%26))+strings.Repeat("x", i)); err != nil {
			t.Fatal(err)
		}
	}
	if len(svc.cache) > tenantCatalogMaxEntries {
		t.Fatalf("cache holds %d tenants, cap is %d", len(svc.cache), tenantCatalogMaxEntries)
	}
}

// A failed tenant read is an error, never a silently narrower list.
func TestTenantDeathCauseReadFailureIsAnError(t *testing.T) {
	svc := NewDeathCauseCatalogService().WithTenantSource(&fakeDeathCauseSource{err: errors.New("db down")})
	if _, err := svc.Catalog(context.Background(), "tenant-a"); err == nil {
		t.Fatal("a failed Health Config read served a list")
	}
}

func mustCatalog(t *testing.T, svc *DeathCauseCatalogService, tenantID string) domain.DeathCauseCatalog {
	t.Helper()
	catalog, err := svc.Catalog(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("catalog: %v", err)
	}
	return catalog
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
