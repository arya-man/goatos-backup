package domain

import (
	"fmt"
	"strings"

	"github.com/vgoats/goatos/backend/internal/health/diagnosis"
)

// StageRoute is one authored answer to "which diagnosis type does this animal reach".
//
// It is the row `health_diagnosis_stage_routes` holds (migration 000395), carried into the
// domain so the resolution rule below stays pure and testable without a database.
type StageRoute struct {
	// AgeBand is `adult` or `kid`, matching the goat's own band.
	AgeBand string
	// StageCode is a management stage from `animal_stage_lookup`, lower-cased, or the band
	// wildcard `*`.
	StageCode string
	// TypeKey is the diagnosis type this animal is judged against -- the value that lands in
	// `health_diagnosis_register_versions.animal_class`.
	TypeKey string
	// SubStage is the cohort the register reads INSIDE the type (K0/K1/K2 for kids on milk),
	// blank for a type with one cohort.
	SubStage string
}

// StageWildcard stands for "every stage in this age band".
//
// It exists because adults are NOT routed by stage today: the shipped code reads
// `age_band == "adult"` and never looks at the stage, so a doe on Mother, a buck, and an
// adult sitting in ICU all reach the adult register. A stage-keyed table alone would refuse
// every adult whose stage nobody thought to seed -- including the clinical placements, which
// carry no age band of their own and so would never be seeded from the stage catalog.
const StageWildcard = "*"

// StageRouting resolves an animal's age band and management stage to a diagnosis type.
//
// THE FAIL-CLOSED PROPERTY IS THE WHOLE POINT, and it is the 2026-08-17 decision restated:
// a stage with no route is REFUSED, never defaulted. The three kid registers disagree about
// the things most likely to kill an animal -- a fattening kid diagnosed off the milk register
// is never checked for acidosis, a milk kid off the weaning register never gets the drop test
// -- so picking the wrong one is worse than picking none. Moving the map out of Go does not
// soften that; it only moves who may write it.
//
// A WILDCARD IS NOT A DEFAULT. It is an authored row a person can see and delete, scoped to
// one age band, and the exact stage always beats it. That is what lets a farm peel `Mother`
// off the adult wildcard onto its own type without touching bucks or dry does -- and what
// lets it delete the wildcard entirely to make adults fail-closed like kids. The difference
// from the old Go behaviour is that the broad rule is now visible and editable rather than
// implied by an `if` nobody outside the repo could read.
type StageRouting struct {
	byStage    map[string]StageRoute
	byWildcard map[string]StageRoute
}

// NewStageRouting indexes the authored rows for resolution.
//
// Rows are keyed on (band, stage) which the table already makes unique, so a duplicate here
// can only come from a caller assembling rows by hand; the last one wins rather than erroring,
// because refusing to route the whole farm over a duplicated row would be a worse failure than
// honouring one of two identical intents.
func NewStageRouting(rows []StageRoute) StageRouting {
	r := StageRouting{
		byStage:    make(map[string]StageRoute, len(rows)),
		byWildcard: make(map[string]StageRoute, 2),
	}
	for _, row := range rows {
		band := normalizeRouteKey(row.AgeBand)
		stage := normalizeRouteKey(row.StageCode)
		if band == "" || stage == "" || strings.TrimSpace(row.TypeKey) == "" {
			continue
		}
		row.AgeBand = band
		row.StageCode = stage
		row.TypeKey = strings.TrimSpace(row.TypeKey)
		row.SubStage = strings.TrimSpace(row.SubStage)
		if stage == StageWildcard {
			r.byWildcard[band] = row
			continue
		}
		r.byStage[band+"\x00"+stage] = row
	}
	return r
}

// Empty reports whether nothing was authored at all.
//
// The caller uses this to tell "this farm's routing is not set up" from "this animal's stage
// is not mapped", because those are different sentences to put in front of a manager and the
// second one names a stage they can act on.
func (r StageRouting) Empty() bool { return len(r.byStage) == 0 && len(r.byWildcard) == 0 }

// Resolve returns the type and sub-stage for one animal. The bool is false when nothing
// routes it, and the caller must refuse rather than default.
//
// Exact stage first, then the band wildcard. The order is the rule: specific beats general,
// so adding one stage row overrides a broad band rule for that stage alone.
func (r StageRouting) Resolve(ageBand, managementStage string) (typeKey string, subStage string, ok bool) {
	band := normalizeRouteKey(ageBand)
	if band == "" {
		return "", "", false
	}
	if stage := normalizeRouteKey(managementStage); stage != "" && stage != StageWildcard {
		if row, found := r.byStage[band+"\x00"+stage]; found {
			return row.TypeKey, row.SubStage, true
		}
	}
	if row, found := r.byWildcard[band]; found {
		return row.TypeKey, row.SubStage, true
	}
	return "", "", false
}

// normalizeRouteKey lower-cases and trims, because `animal_stage_lookup` holds the same codes
// in more than one casing across import runs -- the reason the shipped Go map compared
// case-insensitively, and a property the table's own lower-case CHECK preserves on the write
// side.
func normalizeRouteKey(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// BuiltinStageRoutes is the routing the engine SHIPPED with, and it is the golden ORACLE for
// migration 000395's seed -- nothing on the runtime path may read it.
//
// It is kept so a test can prove the seeded table reproduces the shipped behaviour row for
// row. Once routing is authored, this list is history: it says how a farm's routing was BORN,
// not how it is maintained. The same shape `toxin/domain.Steps()` and `tasks/domain/templates.go`
// already use for exactly this reason.
func BuiltinStageRoutes() []StageRoute {
	return []StageRoute{
		{AgeBand: AgeBandKid, StageCode: "k0", TypeKey: diagnosis.ClassKidMilk, SubStage: "K0"},
		{AgeBand: AgeBandKid, StageCode: "k1", TypeKey: diagnosis.ClassKidMilk, SubStage: "K1"},
		{AgeBand: AgeBandKid, StageCode: "k2", TypeKey: diagnosis.ClassKidMilk, SubStage: "K2"},
		{AgeBand: AgeBandKid, StageCode: "k3", TypeKey: diagnosis.ClassKidWeaning, SubStage: "K3"},
		{AgeBand: AgeBandKid, StageCode: "f2", TypeKey: diagnosis.ClassKidFattening},
		{AgeBand: AgeBandKid, StageCode: "f2-male", TypeKey: diagnosis.ClassKidFattening},
		{AgeBand: AgeBandKid, StageCode: "f2-female", TypeKey: diagnosis.ClassKidFattening},
		{AgeBand: AgeBandKid, StageCode: "warmup", TypeKey: diagnosis.ClassKidFattening},
		{AgeBand: AgeBandAdult, StageCode: StageWildcard, TypeKey: diagnosis.ClassAdult},
	}
}

// RouteRefusal is the sentence a manager reads when nothing routes their animal.
//
// It names the STAGE, because that is the fact someone can act on: a director reads it and
// adds the route on Health Config. The old Go version could only say "milk, weaning or
// fattening" -- the three types it knew about -- which stopped being true the moment a farm
// could author a fourth.
func RouteRefusal(managementStage string) error {
	stage := strings.TrimSpace(managementStage)
	if stage == "" {
		return fmt.Errorf("%w: it has no management stage recorded, so nothing can say which diagnosis type it belongs to", ErrGoatNotDiagnosable)
	}
	return fmt.Errorf("%w: its stage %q is not mapped to a diagnosis type yet", ErrGoatNotDiagnosable, stage)
}
