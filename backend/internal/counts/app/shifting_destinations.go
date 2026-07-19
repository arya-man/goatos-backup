package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/counts/domain"
	"github.com/vgoats/goatos/backend/internal/counts/ports"
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

// DeriveShiftingSource reads the CURRENT park/shed of a single named animal so a shifting event
// that arrived without an explicit source can still record where the movement started.
//
// WHY THIS EXISTS. The simplified Shifting screen stopped asking the operator to re-enter the source
// park/shed: the animal is picked by RFID and the server already knows where it stands, so retyping
// it is both wasted keystrokes and a chance to enter a location that contradicts the record. But
// nothing backfilled the field afterwards, so the stored shifting_events row kept a blank source and
// the movement lost its "from" half -- a movement you cannot read backwards is not an audit trail.
//
// WHY ONE ANIMAL ONLY. Same boundary as DeriveShiftingImpacts: a source is a single origin, and two
// animals standing in different sheds have no single truthful answer. Rather than pick one and
// silently mislabel the rest, a multi-animal movement must state its own source, and this returns
// ErrImpactNotDerivable.
//
// DEGRADES, DOES NOT INVENT. An animal with no recorded placement yields (nil, nil): the caller
// leaves the source absent, exactly as it is today. Writing an empty string instead would turn
// "unknown origin" into a stored fact, and it would violate the non-blank CHECKs on the column.
func (s *Service) DeriveShiftingSource(
	ctx context.Context,
	tenantID string,
	goatIDs []string,
) (parkID *string, shedID *string, err error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, nil, ErrMissingRequiredField
	}
	if len(goatIDs) != 1 {
		return nil, nil, ErrImpactNotDerivable
	}

	facts, err := s.repo.GoatShiftingFacts(ctx, tenantID, goatIDs)
	if err != nil {
		return nil, nil, err
	}
	if len(facts) != len(goatIDs) {
		return nil, nil, fmt.Errorf("%w: %d of %d goat ids resolved", ports.ErrGoatNotFound, len(facts), len(goatIDs))
	}

	return nonBlank(facts[0].ParkID), nonBlank(facts[0].ShedID), nil
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
// WHY IT IS LIMITED TO ONE ANIMAL. With two or more animals the server would have to decide how to
// split them into cohorts and how to distribute pregnancy/lactation/warm-up counts across those
// cohorts. Those are business facts, not arithmetic; inventing them would put numbers the operator
// never entered into a projection the business reads as truth. So a multi-animal movement must
// still state its impacts, and this returns ErrImpactNotDerivable.
//
// FAILS CLOSED. If the named animal does not resolve to a live, non-merged animal in this tenant,
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
	if len(goatIDs) != 1 {
		return nil, ErrImpactNotDerivable
	}

	facts, err := s.repo.GoatShiftingFacts(ctx, tenantID, goatIDs)
	if err != nil {
		return nil, err
	}
	if len(facts) != len(goatIDs) {
		return nil, fmt.Errorf("%w: %d of %d goat ids resolved", ports.ErrGoatNotFound, len(facts), len(goatIDs))
	}

	impacts := make([]domain.ShiftingEventImpact, 0, len(facts))
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
		impacts = append(impacts, domain.ShiftingEventImpact{
			// Same grain key the explicit-impact path builds, so a derived impact lands on the
			// exact projection row an equivalent typed-in impact would have.
			GrainKey:   strings.ToLower(strings.TrimSpace(destinationShedID)) + ":" + breedKey,
			BreedID:    fact.BreedID,
			BreedKey:   breedKey,
			BreedLabel: breedLabel,
			StageTag:   fact.StageTag,
			AgeClass:   fact.AgeClass,
			Sex:        fact.Sex,
			HeadCount:  1,
			// Pregnancy / lactation / warm-up are deliberately left at zero rather than being
			// inferred from the animal's reproductive_status. Those three counts drive feed
			// direction, and quietly asserting "this moved animal is pregnant" from a status field
			// that the operator did not confirm at the shed would put an unreviewed business claim
			// into a projection the business reads as truth. An operator who needs to record them
			// still can, by supplying explicit impacts.
			PregnantCount:  0,
			LactatingCount: 0,
			WarmupCount:    0,
			RiskFlagsJSON:  []byte("{}"),
		})
	}
	return impacts, nil
}
