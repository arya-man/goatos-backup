package ports

import (
	"errors"
	"time"
)

// ReclassifyShedStage is the DIRECT, whole-pen cohort reclassification behind the Counts screen's
// "Change stage" action. It answers one operational question: the farm has decided that everything
// standing in this pen is now a different cohort (K2 became F2-Male, Non-Pregnant became Mother),
// and the tag on every animal in it must follow.
//
// WHY THIS IS NOT A SHIFTING EVENT. A shifting moves animals BETWEEN sheds and adopts the
// destination's cohort as a side effect (counts/domain.ResolveShiftingDestinationStage); it is
// gated on Park Head approval and a mandatory operator video because the claim being proved is
// "these animals physically walked". Here nothing walks. The animals stay exactly where they are
// and only their classification changes, so there is no physical act for a video to prove and no
// second party who can see something the raiser cannot. Maintainer decision 2026-08-12: this
// applies immediately, CEO/leadership only (permissions.GoatReclassifyShedStage, held by
// ceo_internal alone), with the audit row and the per-animal goat.stage_changed event doing the
// accountability work that approval does on the movement path.
//
// WHY THE TAG CARRIES kid/adult. age_band is a property OF the cohort tag, not of the animal's
// birthday -- migration 000109 records the farm data proving it (F2-Male animals up to 67 weeks
// old that the farm still calls kids). So this command reads BOTH stage_code and age_band from the
// same animal_stage_lookup row and writes them together; they can never disagree about one tag.
//
// SCOPE IS ONE PEN, NOT ONE BUILDING. Castro - 1 and Castro - 2 are different pens holding
// different cohorts. ShedID always names the PARENT physical shed and PartitionLabel names the pen
// inside it, per the operational-location convention. A nil PartitionLabel means the shed is
// genuinely undivided ('whole'), never "every partition".
type ReclassifyShedStageCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string

	// ShedID is the parent physical shed location id. Never a partition-bearing alias row.
	ShedID string

	// PartitionLabel is the raw pen label within ShedID ('1', 'Part 3'), or nil for an undivided
	// shed. Matched against goat_shed_partitions using the canonical normalizer, so the caller may
	// send either the display label or the normalized key.
	PartitionLabel *string

	// ManagementStage is the target cohort tag. Validated against the tenant's active
	// animal_stage_lookup vocabulary and REJECTED when it names a clinical state.
	ManagementStage string

	Reason     string
	OccurredAt time.Time

	// ConfigureEmpty allows the command to succeed against a location holding NO live animals,
	// writing only the configured cohort.
	//
	// It exists because two callers want two different things from one write. The Counts Breakdown
	// drawer says "retag the animals in this pen", and an empty pen there is almost always the
	// wrong pen -- so it leaves this false and gets ErrReclassifyEmptyScope. The Sheds directory
	// says "this pen's tag is now X", which is a perfectly ordinary thing to record for a pen that
	// is standing empty before animals arrive -- 12 of the tenant's 116 pens are in that state
	// today -- so it sets this true and gets a success with Reclassified=0.
	//
	// It is part of the request hash, so the two intents cannot replay onto each other.
	ConfigureEmpty bool
}

// MaxReclassifyGoatsPerCommand bounds one reclassification. A pen is a physical enclosure holding
// tens to low hundreds of animals; capping it keeps the statement's lock and memory footprint
// bounded and stops a malformed shed id from taking row locks across the whole herd.
const MaxReclassifyGoatsPerCommand = 1000

// ErrReclassifyScopeTooLarge: the requested pen holds more live animals than one command may
// reclassify. This fails closed rather than silently reclassifying a subset -- a partial cohort
// flip would leave the pen holding two cohorts, which is precisely the state the action exists to
// remove.
var ErrReclassifyScopeTooLarge = errors.New("identity reclassify shed stage: pen holds more live animals than one command may reclassify")

// ErrReclassifyEmptyScope: the requested shed+partition holds no live animals, and the caller did
// not set ConfigureEmpty. Reported rather than treated as a successful no-op, because for a caller
// whose intent is "retag these animals" it almost always means the operator picked the wrong pen.
var ErrReclassifyEmptyScope = errors.New("identity reclassify shed stage: no live animals in the selected shed and partition")

// ReclassifyShedStagePreview is the whole-scope answer to "what would this button do", shown before
// anything is written. Counts are whole-filter aggregates over the pen, never a page of rows.
type ReclassifyShedStagePreview struct {
	ShedID string
	// ShedName and PartitionLabel are echoed so the caller renders the pen it actually resolved,
	// not the raw ids it sent. OperationalLocationDisplay is composed by oploc.Display -- clients
	// render it verbatim and never recompose it.
	ShedName                   string
	PartitionLabel             string
	OperationalLocationDisplay string

	// ManagementStage and AgeBand are the CANONICAL resolved target, in animal_stage_lookup's
	// casing, with the kid/adult band that tag carries. AgeBand is "" for a deliberately
	// unclassified tag, in which case the animals keep the band they already have.
	ManagementStage string
	AgeBand         string

	// TotalLive is every live animal in the pen. Changing is the subset whose stage actually
	// differs from the target; Unchanged is the remainder. Changing + Unchanged == TotalLive.
	TotalLive int
	Changing  int
	Unchanged int

	// CurrentStages is the pen's present cohort composition, so a mixed pen is visible BEFORE the
	// flip rather than discovered after it. Ordered by descending count then stage code.
	CurrentStages []ReclassifyStageBucket
}

// ReclassifyStageBucket is one current-cohort row of the preview. Grain: live animal.
type ReclassifyStageBucket struct {
	ManagementStage string
	AgeBand         string
	Count           int
}

// ReclassifyShedStageResult reports what the commit actually wrote. Reclassified counts only the
// animals whose stage genuinely changed -- the ones that already carried the target tag are left
// alone and, deliberately, emit no event, so a re-run of the same button does not republish
// stage-change events for animals nothing happened to.
type ReclassifyShedStageResult struct {
	ShedID                     string
	ShedName                   string
	PartitionLabel             string
	OperationalLocationDisplay string
	ManagementStage            string
	AgeBand                    string
	TotalLive                  int
	Reclassified               int
	Unchanged                  int
}
