package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
)

const (
	destTenantID = "11111111-1111-4111-8111-111111111111"
	destShedID   = "55555555-5555-4555-8555-555555555555"
	destGoatID   = "33333333-3333-4333-8333-333333333333"
	destGoatIDB  = "66666666-6666-4666-8666-666666666666"
)

// TestDeriveShiftingImpactsBuildsOneImpactFromTheAnimal is the core of the simplification: an
// operator who moved ONE animal supplies no impacts, and the server produces the single breed-grain
// impact row the count projection needs from that animal's own canonical facts.
//
// It asserts the derived row FIELD BY FIELD rather than just "one impact came back", because every
// one of these fields lands in shifting_event_impacts and is read back as business truth:
// head_count must be exactly 1 (not 0, which the DB CHECK rejects, and not a guess), the breed
// key/label must come from the animal, stage/age/sex must be carried through, and the grain key must
// match what the explicit-impact path would have built for the same shed+breed so the derived row
// merges onto the same projection row instead of forking a near-duplicate.
func TestDeriveShiftingImpactsBuildsOneImpactFromTheAnimal(t *testing.T) {
	breedID := "77777777-7777-4777-8777-777777777777"
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{{
		GoatID:     destGoatID,
		BreedID:    &breedID,
		BreedKey:   "boer_cross",
		BreedLabel: "Boer Cross",
		StageTag:   strPtr("K2"),
		AgeClass:   strPtr("kid"),
		Sex:        strPtr("female"),
	}}}
	svc := NewService(repo)

	impacts, err := svc.DeriveShiftingImpacts(context.Background(), destTenantID, destShedID, []string{destGoatID})
	if err != nil {
		t.Fatalf("DeriveShiftingImpacts: %v", err)
	}
	if len(impacts) != 1 {
		t.Fatalf("len(impacts)=%d, want 1", len(impacts))
	}
	got := impacts[0]

	// The grain key is the impact row's identity within the event. It must be the full cohort key
	// domain.ShiftingImpactGrainKey builds (shed:breed:stage:age:sex) -- the identical shape the
	// explicit-impact handler path builds -- so the two paths can never fork key shapes, and so a
	// mixed group's distinct cohorts each get their own row under the per-event unique index.
	wantGrain := "55555555-5555-4555-8555-555555555555:boer_cross:k2:kid:female"
	if got.GrainKey != wantGrain {
		t.Fatalf("GrainKey=%q, want %q", got.GrainKey, wantGrain)
	}
	if got.HeadCount != 1 {
		t.Fatalf("HeadCount=%d, want 1 (exactly the one animal named)", got.HeadCount)
	}
	if got.BreedKey != "boer_cross" || got.BreedLabel != "Boer Cross" {
		t.Fatalf("breed=(%q,%q), want (boer_cross, Boer Cross)", got.BreedKey, got.BreedLabel)
	}
	if got.BreedID == nil || *got.BreedID != breedID {
		t.Fatalf("BreedID=%v, want %q -- the canonical FK must be carried, not dropped", got.BreedID, breedID)
	}
	if got.StageTag == nil || *got.StageTag != "K2" {
		t.Fatalf("StageTag=%v, want K2", got.StageTag)
	}
	if got.AgeClass == nil || *got.AgeClass != "kid" {
		t.Fatalf("AgeClass=%v, want kid", got.AgeClass)
	}
	if got.Sex == nil || *got.Sex != "female" {
		t.Fatalf("Sex=%v, want female", got.Sex)
	}
	// Pregnancy/lactation/warm-up drive feed direction. Deriving them from a status field the
	// operator did not confirm at the shed would inject an unreviewed claim into a projection the
	// business reads as truth, so the derived row leaves them at zero on purpose.
	if got.PregnantCount != 0 || got.LactatingCount != 0 || got.WarmupCount != 0 {
		t.Fatalf("pregnant/lactating/warmup=(%d,%d,%d), want all 0 -- these must never be inferred",
			got.PregnantCount, got.LactatingCount, got.WarmupCount)
	}
	if string(got.RiskFlagsJSON) != "{}" {
		t.Fatalf("RiskFlagsJSON=%q, want {} (the column requires a JSON object)", got.RiskFlagsJSON)
	}
	// The service must ask for exactly the ids it was given -- no widening, no re-ordering into a
	// different animal.
	if len(repo.goatFactsReq) != 1 || repo.goatFactsReq[0] != destGoatID {
		t.Fatalf("repo asked for %v, want [%s]", repo.goatFactsReq, destGoatID)
	}
}

// TestDeriveShiftingImpactsRejectsEmptyInput pins the one remaining hard boundary: with no animals
// named there is nothing to describe, so the derivation rejects BEFORE touching the database -- a
// request that can never be derived should not cost a goat read.
func TestDeriveShiftingImpactsRejectsEmptyInput(t *testing.T) {
	for name, goatIDs := range map[string][]string{
		"zero animals": {},
		"nil animals":  nil,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &fakeRepo{}
			svc := NewService(repo)

			_, err := svc.DeriveShiftingImpacts(context.Background(), destTenantID, destShedID, goatIDs)
			if !errors.Is(err, ErrImpactNotDerivable) {
				t.Fatalf("err=%v, want ErrImpactNotDerivable", err)
			}
			if repo.goatFactsReq != nil {
				t.Fatalf("repo was queried with %v, want no query at all", repo.goatFactsReq)
			}
		})
	}
}

// TestDeriveShiftingImpactsMergesAHomogeneousGroup is the common multi-animal case: several animals
// of the SAME cohort moving into one shed collapse into a single impact row whose head_count is the
// group size. That is the exact aggregate shifting_event_impacts_grain_unique permits (one row per
// (event, shed:breed) grain) and the count projection sums -- not one row per animal.
func TestDeriveShiftingImpactsMergesAHomogeneousGroup(t *testing.T) {
	breedID := "77777777-7777-4777-8777-777777777777"
	fact := func(id string) domain.GoatShiftingFact {
		return domain.GoatShiftingFact{
			GoatID: id, BreedID: &breedID, BreedKey: "boer_cross", BreedLabel: "Boer Cross",
			StageTag: strPtr("K2"), AgeClass: strPtr("kid"), Sex: strPtr("female"),
		}
	}
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{fact(destGoatID), fact(destGoatIDB)}}

	impacts, err := NewService(repo).DeriveShiftingImpacts(
		context.Background(), destTenantID, destShedID, []string{destGoatID, destGoatIDB})
	if err != nil {
		t.Fatalf("DeriveShiftingImpacts: %v", err)
	}
	if len(impacts) != 1 {
		t.Fatalf("len(impacts)=%d, want 1 merged cohort row", len(impacts))
	}
	if impacts[0].HeadCount != 2 {
		t.Fatalf("HeadCount=%d, want 2 (both animals summed into the one cohort)", impacts[0].HeadCount)
	}
}

// TestDeriveShiftingImpactsKeepsDistinctBreedsSeparate: different breeds are different cohorts with
// different grain keys, so they stay two rows -- never summed into one.
func TestDeriveShiftingImpactsKeepsDistinctBreedsSeparate(t *testing.T) {
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{
		{GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer", StageTag: strPtr("K2"), Sex: strPtr("female")},
		{GoatID: destGoatIDB, BreedKey: "sirohi", BreedLabel: "Sirohi", StageTag: strPtr("K2"), Sex: strPtr("female")},
	}}

	impacts, err := NewService(repo).DeriveShiftingImpacts(
		context.Background(), destTenantID, destShedID, []string{destGoatID, destGoatIDB})
	if err != nil {
		t.Fatalf("DeriveShiftingImpacts: %v", err)
	}
	if len(impacts) != 2 {
		t.Fatalf("len(impacts)=%d, want 2 distinct breed cohorts", len(impacts))
	}
	for _, im := range impacts {
		if im.HeadCount != 1 {
			t.Fatalf("HeadCount=%d, want 1 per distinct cohort", im.HeadCount)
		}
	}
}

// TestDeriveShiftingImpactsSplitsAMixedCohort (maintainer decision 2026-08-18): animals sharing a
// (shed, breed) grain but disagreeing on stage/age/sex are a genuine cohort split, and the server
// states it as SEPARATE truthful rows -- one per distinct cohort, each carrying that animal's own
// descriptors and its own grain key -- never a manufactured aggregate averaging the two, and no
// longer a rejection forcing the operator to raise one movement per animal.
func TestDeriveShiftingImpactsSplitsAMixedCohort(t *testing.T) {
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{
		{GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer", StageTag: strPtr("K2"), Sex: strPtr("female")},
		{GoatID: destGoatIDB, BreedKey: "boer", BreedLabel: "Boer", StageTag: strPtr("K2"), Sex: strPtr("male")},
	}}

	impacts, err := NewService(repo).DeriveShiftingImpacts(
		context.Background(), destTenantID, destShedID, []string{destGoatID, destGoatIDB})
	if err != nil {
		t.Fatalf("DeriveShiftingImpacts: %v", err)
	}
	if len(impacts) != 2 {
		t.Fatalf("len(impacts)=%d, want 2 (one row per distinct cohort, never an averaged aggregate)", len(impacts))
	}
	if impacts[0].GrainKey == impacts[1].GrainKey {
		t.Fatalf("both rows share grain key %q -- distinct cohorts must have distinct keys or the per-event unique index rejects the insert", impacts[0].GrainKey)
	}
	for _, im := range impacts {
		if im.HeadCount != 1 {
			t.Fatalf("HeadCount=%d, want 1 per cohort row", im.HeadCount)
		}
	}
	if sexOf := func(i int) string {
		if impacts[i].Sex == nil {
			return ""
		}
		return *impacts[i].Sex
	}; sexOf(0) != "female" || sexOf(1) != "male" {
		t.Fatalf("sex=(%q,%q), want (female, male) -- each row keeps its own animal's descriptor, in input order", sexOf(0), sexOf(1))
	}
}

// TestDeriveShiftingImpactsFailsClosedWhenTheAnimalDoesNotResolve is the safety half.
//
// A goat id that resolves to nothing means a wrong RFID, an already-exited animal, a merged animal,
// or another tenant's animal. Falling back to a placeholder impact would record an authorized
// movement describing an animal nobody can read -- exactly the drift that made goat_ids mandatory.
// It must surface as a specific error the operator can act on.
func TestDeriveShiftingImpactsFailsClosedWhenTheAnimalDoesNotResolve(t *testing.T) {
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{}}
	svc := NewService(repo)

	impacts, err := svc.DeriveShiftingImpacts(context.Background(), destTenantID, destShedID, []string{destGoatID})
	if !errors.Is(err, ports.ErrGoatNotFound) {
		t.Fatalf("err=%v, want ports.ErrGoatNotFound", err)
	}
	if impacts != nil {
		t.Fatalf("impacts=%v, want nil -- a failed derivation must not yield a partial impact set", impacts)
	}
}

// TestDeriveShiftingImpactsRejectsAnExitedAnimalAsNotShiftable reproduces the real CBE failure:
// CBE-ASSUMED-RFID-00001 resolves to an existing goat whose lifecycle is dead and whose exited_at
// is set. That is not a missing goat (404); it is an ineligible movement target that must fail
// before a shifting event or approval request can be created.
func TestDeriveShiftingImpactsRejectsAnExitedAnimalAsNotShiftable(t *testing.T) {
	exitedAt := time.Date(2026, 7, 28, 4, 28, 14, 0, time.UTC)
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{{
		GoatID: destGoatID, LifecycleStatus: "dead", ExitedAt: &exitedAt,
		BreedKey: "sirohi", BreedLabel: "Sirohi",
	}}}

	impacts, err := NewService(repo).DeriveShiftingImpacts(
		context.Background(), destTenantID, destShedID, []string{destGoatID})
	if !errors.Is(err, ports.ErrGoatNotShiftable) {
		t.Fatalf("err=%v, want ports.ErrGoatNotShiftable for an existing dead/exited goat", err)
	}
	if impacts != nil {
		t.Fatalf("impacts=%v, want nil for an ineligible animal", impacts)
	}
}

// TestDeriveShiftingImpactsRejectsAnUnresolvableBreed guards the DB CHECK from the Go side.
//
// shifting_event_impacts requires a non-blank breed_key. The repository's COALESCE chain should
// make an empty key impossible (it terminates at goats.species, which is NOT NULL), but if that
// chain is ever weakened this must fail with a named error rather than reaching Postgres and
// surfacing as an opaque 500 on an operator's phone.
func TestDeriveShiftingImpactsRejectsAnUnresolvableBreed(t *testing.T) {
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{{
		GoatID:     destGoatID,
		BreedKey:   "",
		BreedLabel: "",
	}}}
	svc := NewService(repo)

	if _, err := svc.DeriveShiftingImpacts(context.Background(), destTenantID, destShedID, []string{destGoatID}); !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("err=%v, want ErrMissingRequiredField", err)
	}
}

// TestShiftingDestinationsRequiresTenantScope pins that the catalog is never read tenant-wide. A
// missing tenant must fail rather than fall through to a query that would leak another tenant's
// parks into an operator's dropdown.
func TestShiftingDestinationsRequiresTenantScope(t *testing.T) {
	svc := NewService(&fakeRepo{})
	if _, err := svc.ShiftingDestinations(context.Background(), "  "); !errors.Is(err, ErrMissingRequiredField) {
		t.Fatalf("err=%v, want ErrMissingRequiredField", err)
	}
}

// TestShiftingDestinationsReturnsTheCatalogUnreordered pins that the service is a pass-through.
//
// Ordering is settled in SQL (park name, then shed name, with an id tiebreak). If the service
// re-sorted or de-duplicated here, two same-named sheds in different parks could collapse or swap
// between calls, and the dropdown would reorder under the operator's finger mid-selection.
func TestShiftingDestinationsReturnsTheCatalogUnreordered(t *testing.T) {
	catalog := domain.ShiftingDestinationCatalog{Parks: []domain.ShiftingDestinationPark{
		{ParkID: "park-cbe", Name: "Coimbatore", Sheds: []domain.ShiftingDestinationShed{
			{ShedID: "shed-cbe-castro1", Name: "Castro 1"},
			{ShedID: "shed-cbe-castro2", Name: "Castro 2"},
		}},
		{ParkID: "park-cpt", Name: "Channapatna", Sheds: []domain.ShiftingDestinationShed{
			{ShedID: "shed-cpt-castro1", Name: "Castro 1"},
		}},
	}}
	svc := NewService(&fakeRepo{destinations: catalog})

	got, err := svc.ShiftingDestinations(context.Background(), destTenantID)
	if err != nil {
		t.Fatalf("ShiftingDestinations: %v", err)
	}
	if len(got.Parks) != 2 {
		t.Fatalf("len(parks)=%d, want 2", len(got.Parks))
	}
	if got.Parks[0].ParkID != "park-cbe" || got.Parks[1].ParkID != "park-cpt" {
		t.Fatalf("park order=%q,%q, want park-cbe,park-cpt (SQL order preserved)",
			got.Parks[0].ParkID, got.Parks[1].ParkID)
	}
	// The disambiguation guarantee: the SAME shed name under two different parks stays two
	// distinct options with distinct ids.
	if got.Parks[0].Sheds[0].Name != got.Parks[1].Sheds[0].Name {
		t.Fatal("test premise broken: this case exists to cover a repeated shed name")
	}
	if got.Parks[0].Sheds[0].ShedID == got.Parks[1].Sheds[0].ShedID {
		t.Fatal("two sheds sharing a name must keep distinct ids")
	}
}

// TestDeriveShiftingSourceReadsTheAnimalsCurrentPlacement is the service-level proof for the
// blank-source fix: the origin of a movement is read off the animal, not retyped by the operator.
func TestDeriveShiftingSourceReadsTheAnimalsCurrentPlacement(t *testing.T) {
	const srcParkID = "88888888-8888-4888-8888-888888888888"
	const srcShedID = "99999999-9999-4999-8999-999999999999"
	repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{{
		GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer",
		ParkID: strPtr(srcParkID), ShedID: strPtr(srcShedID),
	}}}
	svc := NewService(repo)

	parkID, shedID, _, err := svc.DeriveShiftingSource(context.Background(), destTenantID, []string{destGoatID})
	if err != nil {
		t.Fatalf("DeriveShiftingSource: %v", err)
	}
	if parkID == nil || *parkID != srcParkID {
		t.Fatalf("park = %v, want %q", parkID, srcParkID)
	}
	if shedID == nil || *shedID != srcShedID {
		t.Fatalf("shed = %v, want %q", shedID, srcShedID)
	}
}

// TestDeriveShiftingSourceDegradesInsteadOfInventing covers the three non-answers. None of them may
// produce a blank or guessed origin: an empty string would become a stored fact claiming the animal
// came from nowhere, and would violate the non-blank CHECKs on the column.
func TestDeriveShiftingSourceDegradesInsteadOfInventing(t *testing.T) {
	t.Run("animal has no recorded placement", func(t *testing.T) {
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{{
			GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer",
		}}}
		parkID, shedID, _, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID})
		if err != nil {
			t.Fatalf("DeriveShiftingSource: %v", err)
		}
		if parkID != nil || shedID != nil {
			t.Fatalf("got %v/%v, want nil/nil", parkID, shedID)
		}
	})

	t.Run("placement is present but blank", func(t *testing.T) {
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{{
			GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer",
			ParkID: strPtr("   "), ShedID: strPtr(""),
		}}}
		parkID, shedID, _, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID})
		if err != nil {
			t.Fatalf("DeriveShiftingSource: %v", err)
		}
		if parkID != nil || shedID != nil {
			t.Fatalf("got %v/%v, want nil/nil -- a blank placement must not become a stored fact", parkID, shedID)
		}
	})

	t.Run("multiple animals sharing one origin resolve to that source", func(t *testing.T) {
		const p = "88888888-8888-4888-8888-888888888888"
		const s = "99999999-9999-4999-8999-999999999999"
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{
			{GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer", ParkID: strPtr(p), ShedID: strPtr(s)},
			{GoatID: destGoatIDB, BreedKey: "boer", BreedLabel: "Boer", ParkID: strPtr(p), ShedID: strPtr(s)},
		}}
		parkID, shedID, _, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID, destGoatIDB})
		if err != nil {
			t.Fatalf("DeriveShiftingSource: %v", err)
		}
		if parkID == nil || *parkID != p || shedID == nil || *shedID != s {
			t.Fatalf("got %v/%v, want %s/%s (a shared origin is a truthful single source)", parkID, shedID, p, s)
		}
	})

	t.Run("animals in different sheds keep the shared farm and drop the shed", func(t *testing.T) {
		// Maintainer decision 2026-08-20: mixed source sheds are a valid movement. The farm is
		// still one truthful fact and is kept; the shed (and partition) degrade to absent.
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{
			{GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer", ParkID: strPtr("p1"), ShedID: strPtr("s1"), ShedPartitionLabel: strPtr("1")},
			{GoatID: destGoatIDB, BreedKey: "boer", BreedLabel: "Boer", ParkID: strPtr("p1"), ShedID: strPtr("s2"), ShedPartitionLabel: strPtr("1")},
		}}
		parkID, shedID, partition, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID, destGoatIDB})
		if err != nil {
			t.Fatalf("DeriveShiftingSource: %v", err)
		}
		if parkID == nil || *parkID != "p1" {
			t.Fatalf("park = %v, want p1 (the farm is still shared and truthful)", parkID)
		}
		if shedID != nil || partition != nil {
			t.Fatalf("shed/partition = %v/%v, want nil/nil -- mixed sheds have no single source to store", shedID, partition)
		}
	})

	t.Run("animals on different farms are rejected outright", func(t *testing.T) {
		// Goats never move between parks (movement lock 2026-07-19): a cross-farm basket is not
		// a movement with a degraded source, it is not a movement at all.
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{
			{GoatID: destGoatID, BreedKey: "boer", BreedLabel: "Boer", ParkID: strPtr("p1"), ShedID: strPtr("s1")},
			{GoatID: destGoatIDB, BreedKey: "boer", BreedLabel: "Boer", ParkID: strPtr("p2"), ShedID: strPtr("s1")},
		}}
		if _, _, _, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID, destGoatIDB}); !errors.Is(err, ErrMixedSourceParks) {
			t.Fatalf("err=%v, want ErrMixedSourceParks", err)
		}
	})

	t.Run("empty input rejects before the database", func(t *testing.T) {
		repo := &fakeRepo{}
		if _, _, _, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, nil); !errors.Is(err, ErrImpactNotDerivable) {
			t.Fatalf("err=%v, want ErrImpactNotDerivable", err)
		}
		if repo.goatFactsReq != nil {
			t.Fatalf("repo was queried with %v, want no query at all", repo.goatFactsReq)
		}
	})
}

// TestDeriveShiftingSourceFailsClosedWhenTheAnimalDoesNotResolve mirrors the impacts path: an
// animal nobody can read must not silently yield a source.
func TestDeriveShiftingSourceFailsClosedWhenTheAnimalDoesNotResolve(t *testing.T) {
	svc := NewService(&fakeRepo{})
	if _, _, _, err := svc.DeriveShiftingSource(
		context.Background(), destTenantID, []string{destGoatID}); !errors.Is(err, ports.ErrGoatNotFound) {
		t.Fatalf("err = %v, want ports.ErrGoatNotFound", err)
	}
}

// TestDeriveShiftingSourceCarriesTheOriginPartition pins the FROM half of an OPERATIONAL location.
//
// The origin is park + shed + optional partition. An earlier revision accepted
// source_partition_label on the request, trimmed it, carried it on the domain struct -- and then
// hardcoded nil at the write, so a movement out of "Castro 2" was stored as leaving "Castro". The
// move still applied correctly; only the audit trail lost which pen the animals actually left,
// which is exactly the kind of silently-wrong history nobody notices until they need it.
func TestDeriveShiftingSourceCarriesTheOriginPartition(t *testing.T) {
	const srcParkID = "88888888-8888-4888-8888-888888888888"
	const srcShedID = "99999999-9999-4999-8999-999999999999"
	fact := func(goatID, partition string) domain.GoatShiftingFact {
		f := domain.GoatShiftingFact{
			GoatID: goatID, BreedKey: "boer", BreedLabel: "Boer",
			ParkID: strPtr(srcParkID), ShedID: strPtr(srcShedID),
		}
		if partition != "" {
			f.ShedPartitionLabel = strPtr(partition)
		}
		return f
	}

	t.Run("shared partition is the source partition", func(t *testing.T) {
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{fact(destGoatID, "2"), fact(destGoatIDB, "2")}}
		_, _, partition, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID, destGoatIDB})
		if err != nil {
			t.Fatalf("DeriveShiftingSource: %v", err)
		}
		if partition == nil || *partition != "2" {
			t.Fatalf("partition = %v, want %q", partition, "2")
		}
	})

	t.Run("both label conventions are one partition", func(t *testing.T) {
		// 'Part 3' and '3' name the same pen; treating them as different origins would wrongly
		// drop the partition from a group that in fact shares one.
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{fact(destGoatID, "Part 3"), fact(destGoatIDB, "3")}}
		_, _, partition, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID, destGoatIDB})
		if err != nil {
			t.Fatalf("DeriveShiftingSource: %v", err)
		}
		if partition == nil {
			t.Fatal("partition = nil, want the shared partition ('Part 3' and '3' are the same pen)")
		}
	})

	t.Run("mixed partitions keep the shared shed and drop the partition", func(t *testing.T) {
		// The shed is still one truthful fact ("they left Castro"); which pen is not, so the
		// partition degrades to absent rather than one pen's label mislabeling the group.
		repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{fact(destGoatID, "1"), fact(destGoatIDB, "2")}}
		parkID, shedID, partition, err := NewService(repo).DeriveShiftingSource(
			context.Background(), destTenantID, []string{destGoatID, destGoatIDB})
		if err != nil {
			t.Fatalf("DeriveShiftingSource: %v", err)
		}
		if parkID == nil || *parkID != srcParkID || shedID == nil || *shedID != srcShedID {
			t.Fatalf("park/shed = %v/%v, want %s/%s", parkID, shedID, srcParkID, srcShedID)
		}
		if partition != nil {
			t.Fatalf("partition = %v, want nil -- mixed pens have no single source partition", *partition)
		}
	})

	t.Run("non-partitioned shed never yields the whole sentinel", func(t *testing.T) {
		for _, raw := range []string{"", "whole", "  "} {
			repo := &fakeRepo{goatFacts: []domain.GoatShiftingFact{fact(destGoatID, raw)}}
			_, _, partition, err := NewService(repo).DeriveShiftingSource(
				context.Background(), destTenantID, []string{destGoatID})
			if err != nil {
				t.Fatalf("DeriveShiftingSource(%q): %v", raw, err)
			}
			if partition != nil {
				t.Fatalf("partition = %q for raw %q, want nil", *partition, raw)
			}
		}
	})
}
