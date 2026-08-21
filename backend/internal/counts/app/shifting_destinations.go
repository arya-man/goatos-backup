package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// Operator-facing support for the simplified single-animal shifting flow.
//
// The field flow this serves is: an operator searches ONE animal by RFID, picks a destination from
// a park -> shed cascade, and submits. Everything else the aggregate shifting model needs -- the
// breed-grain impact rows that feed the count projection -- is derived here from the animal itself
// rather than being typed in on a phone.

// ShiftingDestinations returns the bounded park -> shed catalog the operator picks a destination
// from. Ordering is settled in SQL so the dropdown is stable between calls.
func (s *Service) ShiftingDestinations(ctx context.Context, tenantID string) (domain.ShiftingDestinationCatalog, error) {
	if strings.TrimSpace(tenantID) == "" {
		return domain.ShiftingDestinationCatalog{}, ErrMissingRequiredField
	}
	return s.repo.ShiftingDestinationCatalog(ctx, tenantID)
}

// ActiveBreeds returns the breeds present on the tenant's live herd for the operator birth form's
// breed picker. It is the same vocabulary the Counts Breakdown breed facet shows, but reachable on
// the operator (CountsWrite) surface -- the read-only Counts Breakdown screen is CountsRead, which
// a field operator does not hold, so the birth form must not source its breed options from there.
func (s *Service) ActiveBreeds(ctx context.Context, tenantID string) ([]domain.CountsBreakdownSeriesPoint, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrMissingRequiredField
	}
	return s.repo.ActiveBreeds(ctx, tenantID)
}

// ShiftingGoatFacts reads the named animals' narrow canonical facts (stage, sex, placement) for
// the typed-raise rulebook (domain.ResolveShiftTypeDecision). Same fail-closed contract as the
// derivations below: every id must resolve to a live, non-merged animal in this tenant.
func (s *Service) ShiftingGoatFacts(ctx context.Context, tenantID string, goatIDs []string) ([]domain.GoatShiftingFact, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, ErrMissingRequiredField
	}
	if len(goatIDs) == 0 {
		return nil, ErrImpactNotDerivable
	}
	facts, err := s.repo.GoatShiftingFacts(ctx, tenantID, goatIDs)
	if err != nil {
		return nil, err
	}
	if len(facts) != len(goatIDs) {
		return nil, fmt.Errorf("%w: %d of %d goat ids resolved", ports.ErrGoatNotFound, len(facts), len(goatIDs))
	}
	if err := validateShiftableGoatFacts(facts); err != nil {
		return nil, err
	}
	return facts, nil
}

// DeriveShiftingSource reads the CURRENT park/shed of a single named animal so a shifting event
// that arrived without an explicit source can still record where the movement started.
//
// WHY THIS EXISTS. The simplified Shifting screen stopped asking the operator to re-enter the source
// park/shed: the animal is picked by RFID and the server already knows where it stands, so retyping
// it is both wasted keystrokes and a chance to enter a location that contradicts the record. But
// nothing backfilled the field afterwards, so the stored shifting_events row kept a blank source and
// the movement lost its "from" half -- a movement you cannot read backwards is not an audit trail.
//
// MULTI-ANIMAL. A source is a single origin. When every named animal stands in the SAME park/shed
// (the common "move this whole group out of one shed" case) that shared origin is the truthful
// source and is returned. When the animals stand in DIFFERENT sheds there is no single truthful
// answer, so rather than pick one and silently mislabel the rest this returns ErrImpactNotDerivable
// and the caller leaves the source absent -- the per-goat "from" is still preserved on each animal's
// location history by the relocation path.
//
// DEGRADES, DOES NOT INVENT. An animal with no recorded placement yields (nil, nil): the caller
// leaves the source absent, exactly as it is today. Writing an empty string instead would turn
// "unknown origin" into a stored fact, and it would violate the non-blank CHECKs on the column.
// PARTITION. The origin is an OPERATIONAL location, so the source is park + shed + optional
// partition. The partition is derived under exactly the same "one truthful origin" rule as the
// shed: it is returned only when every named animal shares it. A group drawn from Castro 1 AND
// Castro 2 has no single source partition, so the movement is rejected instead of being weakened
// into a parent-shed source. Comparison goes through oploc.SamePartition so 'Part 3' and '3' are
// one partition, and a non-partitioned shed (NULL/”/'whole') yields nil, never the 'whole' sentinel.
func (s *Service) DeriveShiftingSource(
	ctx context.Context,
	tenantID string,
	goatIDs []string,
) (parkID *string, shedID *string, partitionLabel *string, err error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, nil, nil, ErrMissingRequiredField
	}
	if len(goatIDs) == 0 {
		return nil, nil, nil, ErrImpactNotDerivable
	}

	facts, err := s.repo.GoatShiftingFacts(ctx, tenantID, goatIDs)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(facts) != len(goatIDs) {
		return nil, nil, nil, fmt.Errorf("%w: %d of %d goat ids resolved", ports.ErrGoatNotFound, len(facts), len(goatIDs))
	}
	if err := validateShiftableGoatFacts(facts); err != nil {
		return nil, nil, nil, err
	}

	// All animals must share one origin park AND shed for it to be a truthful single source.
	park := nonBlank(facts[0].ParkID)
	shed := nonBlank(facts[0].ShedID)
	for _, fact := range facts[1:] {
		if !eqOptional(park, nonBlank(fact.ParkID)) || !eqOptional(shed, nonBlank(fact.ShedID)) {
			return nil, nil, nil, ErrImpactNotDerivable
		}
	}

	partition := partitionOrNil(facts[0].ShedPartitionLabel)
	for _, fact := range facts[1:] {
		if !oploc.SamePartition(derefOrBlank(partition), derefOrBlank(partitionOrNil(fact.ShedPartitionLabel))) {
			return nil, nil, nil, ErrImpactNotDerivable
		}
	}
	return park, shed, partition, nil
}

// partitionOrNil collapses every "not partitioned" encoding (NULL, "", and the 'whole' matching
// sentinel) to nil, so a non-partitioned shed never stores 'whole' as if it were a place.
func partitionOrNil(v *string) *string {
	trimmed := nonBlank(v)
	if trimmed == nil || !oploc.IsPartitioned(*trimmed) {
		return nil
	}
	return trimmed
}

func derefOrBlank(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// nonBlank collapses a present-but-empty/whitespace value to nil, so a source that cannot be
// resolved stays genuinely absent instead of becoming a blank stored fact.
func nonBlank(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	return v
}

// DeriveShiftingImpacts builds the impact rows for a movement whose caller supplied none.
//
// WHY THIS EXISTS. shifting_event_impacts is an AGGREGATE model: it records "N head of breed X at
// stage Y moved into shed Z", which is what the count projection consumes. That grain is the right
// one for a bulk movement, but it is a terrible thing to ask a field operator to type when they are
// moving a single animal that the system already knows everything about. So when the request names
// exactly one animal and no impacts, the server reads that animal's canonical facts and writes the
// one-row cohort that describes it.
//
// MULTI-ANIMAL: ONE SOURCE-CORRECT COHORT ROW PER DISTINCT COHORT, NEVER AN INVENTED AGGREGATE.
// The movement itself is applied and emitted one source-correct transition per goat by the
// relocation path (identity.RelocateGoatsToShedInTx is set-based over the whole group). Here we
// build the aggregate impact rows the count projection consumes. Each animal contributes exactly one
// head to the cohort it belongs to (destination shed x breed), and animals that share that cohort
// SUM their head_count into the one row shifting_event_impacts_grain_unique permits. What we never do
// is collapse a MIXED set into an aggregate the operator never entered: if two animals land on the
// same (shed, breed) grain but disagree on stage/age/sex, that is a genuine cohort split the server
// cannot invent a single stage/sex for, so it FAILS CLOSED with ErrImpactNotDerivable and the
// operator supplies explicit per-cohort impacts (the same fallback the reviewer sanctioned).
// Pregnancy / lactation / warm-up stay at zero for every derived row -- those are confirmed clinical
// facts, never inferred from a move.
//
// FAILS CLOSED. If any named animal does not resolve to a live, non-merged animal in this tenant,
// this returns ports.ErrGoatNotFound rather than falling back to a placeholder impact. An
// authorized movement that describes an animal nobody can read is exactly the drift the goat_ids
// requirement was added to prevent.
func (s *Service) DeriveShiftingImpacts(
	ctx context.Context,
	tenantID string,
	destinationShedID string,
	goatIDs []string,
) ([]domain.ShiftingEventImpact, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(destinationShedID) == "" {
		return nil, ErrMissingRequiredField
	}
	if len(goatIDs) == 0 {
		return nil, ErrImpactNotDerivable
	}

	facts, err := s.repo.GoatShiftingFacts(ctx, tenantID, goatIDs)
	if err != nil {
		return nil, err
	}
	if len(facts) != len(goatIDs) {
		return nil, fmt.Errorf("%w: %d of %d goat ids resolved", ports.ErrGoatNotFound, len(facts), len(goatIDs))
	}
	if err := validateShiftableGoatFacts(facts); err != nil {
		return nil, err
	}

	// Merge per-goat legs into cohort rows keyed by grain_key (destination shed x breed), preserving
	// insertion order so the derived set is deterministic for the idempotency-neutral persist below.
	byGrain := make(map[string]*domain.ShiftingEventImpact, len(facts))
	order := make([]string, 0, len(facts))
	for _, fact := range facts {
		breedKey := strings.TrimSpace(fact.BreedKey)
		breedLabel := strings.TrimSpace(fact.BreedLabel)
		// The repository's COALESCE chain already guarantees a non-empty label (it ends at
		// goats.species, which is NOT NULL). This check is the belt to that braces: an empty key
		// would violate shifting_event_impacts' btrim(breed_key) <> '' CHECK, and failing here with
		// a named error beats a raw constraint violation surfacing as a 500.
		if breedKey == "" || breedLabel == "" {
			return nil, fmt.Errorf("%w: goat %s has no resolvable breed", ErrMissingRequiredField, fact.GoatID)
		}
		leg := domain.ShiftingEventImpact{
			// Same grain key the explicit-impact path builds, so a derived impact lands on the
			// exact projection row an equivalent typed-in impact would have.
			GrainKey:       strings.ToLower(strings.TrimSpace(destinationShedID)) + ":" + breedKey,
			BreedID:        fact.BreedID,
			BreedKey:       breedKey,
			BreedLabel:     breedLabel,
			StageTag:       fact.StageTag,
			AgeClass:       fact.AgeClass,
			Sex:            fact.Sex,
			HeadCount:      1,
			PregnantCount:  0,
			LactatingCount: 0,
			WarmupCount:    0,
			RiskFlagsJSON:  []byte("{}"),
		}
		existing, ok := byGrain[leg.GrainKey]
		if !ok {
			cp := leg
			byGrain[leg.GrainKey] = &cp
			order = append(order, leg.GrainKey)
			continue
		}
		// Same cohort grain: only merge when the descriptive cohort identity agrees. A disagreement
		// is a mixed set we must not average into one invented stage/sex.
		if !sameCohortDescriptor(existing, &leg) {
			return nil, fmt.Errorf("%w: animals of breed %q moving to this shed differ in stage/age/sex; supply explicit impacts to state the cohort split",
				ErrImpactNotDerivable, breedLabel)
		}
		existing.HeadCount += leg.HeadCount
	}

	impacts := make([]domain.ShiftingEventImpact, 0, len(order))
	for _, grain := range order {
		impacts = append(impacts, *byGrain[grain])
	}
	return impacts, nil
}

// validateShiftableGoatFacts separates lifecycle membership from health. A sick, treated,
// quarantine, or ICU goat still has lifecycle_status='alive' and may need a health-category shed
// move. Dead/sold/transferred/exited goats are terminal and must be rejected before any shifting
// event or approval request is written.
func validateShiftableGoatFacts(facts []domain.GoatShiftingFact) error {
	for _, fact := range facts {
		lifecycle := strings.ToLower(strings.TrimSpace(fact.LifecycleStatus))
		if fact.ExitedAt != nil || (lifecycle != "" && lifecycle != "alive") {
			return fmt.Errorf("%w: goat %s has lifecycle_status=%q", ports.ErrGoatNotShiftable, fact.GoatID, fact.LifecycleStatus)
		}
	}
	return nil
}

// sameCohortDescriptor reports whether two same-grain impact legs describe the identical cohort on
// the fields that are not part of the grain key -- stage, age class, and sex. Two animals may share
// a (shed, breed) grain yet belong to different cohorts (a buck and a doe, a kid and an adult); those
// must stay distinguishable rather than be summed under one animal's descriptor.
func sameCohortDescriptor(a, b *domain.ShiftingEventImpact) bool {
	return eqOptional(a.StageTag, b.StageTag) &&
		eqOptional(a.AgeClass, b.AgeClass) &&
		eqOptional(a.Sex, b.Sex)
}

// eqOptional compares two optional descriptor values treating nil and blank as the same absent value.
func eqOptional(a, b *string) bool {
	av, bv := "", ""
	if a != nil {
		av = strings.TrimSpace(*a)
	}
	if b != nil {
		bv = strings.TrimSpace(*b)
	}
	return strings.EqualFold(av, bv)
}
