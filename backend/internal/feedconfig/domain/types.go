// Package domain holds the authored feed-configuration vocabulary, its read shapes, and the
// pure validation rules for editing it.
//
// SCOPE BOUNDARY -- this package owns AUTHORED CONFIGURATION, not execution.
//
// backend/internal/feed owns feed DIRECTION: taking today's herd, resolving each shed's ration,
// and issuing/packing the direction. This package owns the tables that direction READS FROM --
// the ration grid (migration 000003) and the dispatch clock (000004) -- and the surface an
// operator edits them through. Keeping them apart is the module-table ownership rule from
// backend/AGENTS.md: feed_* config tables have exactly one writer, and it is this module.
//
// # THE SAFETY RULE THIS PACKAGE INHERITS FROM MIGRATION 000003
//
// A rate of 0 is a REAL authored value (milk-fed K0/K1 kids are correctly fed 0 g of solids). A
// rate that was never authored is a DIFFERENT state, and the consequences are opposite: 0 means
// "feed nothing, proceed", absent means "we do not know what to feed this group, BLOCK". Nothing
// in this package may collapse the two. Concretely:
//
//   - a write path never invents rows to fill gaps in the grid;
//   - an absent grams_per_head in a request is REJECTED, never defaulted to 0;
//   - a read never COALESCEs a missing rate to 0.
//
// # EFFECTIVE DATING
//
// Rates, shed factors, and schedule clocks are effective-dated. An edit is never destructive: it
// closes the currently-open row (valid_to = the business date the change takes effect) and opens a
// new one, so "what were we feeding Osmanabadi/Pregnant at CBE last March" stays answerable. The
// one exception is a SAME-BUSINESS-DAY re-edit, which corrects the open row in place because a
// zero-length window cannot satisfy the schema's valid_to > valid_from. That three-way behaviour is
// exactly what seed-feed-ration's seedRates does, and the two must not diverge -- if they did, the
// UI and the seed would disagree about what an edit means.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Validation errors. These are all "the author sent something we will not silently repair"
// conditions -- per AGENTS.md, a PRESENT but out-of-range authored value fails the write; it is
// never rewritten to a default the author never entered.
var (
	ErrMissingField   = errors.New("feedconfig: missing required field")
	ErrInvalidDecimal = errors.New("feedconfig: value is not a valid decimal")
	ErrNegativeValue  = errors.New("feedconfig: value must not be negative")
	// ErrValueOutOfRange is for a value that parses as a decimal and is non-negative but falls
	// outside the column's own CHECK -- a dry-matter factor above 1, a wastage factor of 1 or more.
	// It is a SEPARATE error from ErrNegativeValue because the author needs to be told which bound
	// they crossed; both are rejections, and neither is ever repaired into range.
	ErrValueOutOfRange  = errors.New("feedconfig: value is outside the allowed range")
	ErrInvalidTime      = errors.New("feedconfig: value is not a valid local time (HH:MM or HH:MM:SS)")
	ErrTimeOrder        = errors.New("feedconfig: schedule times are out of order")
	ErrInvalidWorkflow  = errors.New("feedconfig: workflow must be 'normal' or 'experiment'")
	ErrInvalidDate      = errors.New("feedconfig: value is not a valid business date (YYYY-MM-DD)")
	ErrInvalidAppliesTo = errors.New("feedconfig: applies_to must be 'adult' or 'kid'")
	// ErrInvalidExperimentStatus guards the one field that decides WHICH WORKFLOW feeds a shed.
	// An unrecognised value is rejected rather than coerced: 'active' enrols the shed onto authored
	// absolute kg and 'retired' returns it to the per-head ration grid, so there is no safe default
	// to fall back to.
	ErrInvalidExperimentStatus = errors.New("feedconfig: status must be 'active' or 'retired'")
	// ErrDuplicateFeedItem guards a batch that names one feed item twice. The two cells carry two
	// authored quantities and collapse onto ONE row under the natural key, so keeping either one
	// silently stores a number the author did not choose. Rejected rather than de-duplicated.
	ErrDuplicateFeedItem = errors.New("feedconfig: feed item appears more than once in one write")
)

// Workflows recognised by feed_schedule_config. Mirrors the migration's CHECK constraint; a value
// outside it is rejected before it reaches SQL so the caller gets a field error rather than a
// constraint violation.
const (
	WorkflowNormal     = "normal"
	WorkflowExperiment = "experiment"
)

// Write outcomes. Same vocabulary as the migration's CHECK and as seed-feed-ration's counters.
const (
	OutcomeInserted   = "inserted"
	OutcomeSuperseded = "superseded"
	OutcomeCorrected  = "corrected"
	OutcomeUnchanged  = "unchanged"
)

// Write kinds, matching feed_config_write_log.write_kind.
const (
	WriteKindRationRate     = "ration_rate"
	WriteKindShedFactor     = "shed_factor"
	WriteKindScheduleConfig = "schedule_config"
	// WriteKindExperimentConfig covers BOTH experiment writes -- authoring one shed's absolute-kg
	// cell and switching a shed between the experiment workflow and the normal ration grid. They
	// are one authoring surface with one identity space; the ledger's outcome and result_row_id
	// already distinguish what an individual edit did. Added to the schema by migration 000006.
	WriteKindExperimentConfig = "experiment_config"
	// WriteKindFeedItem covers adding an entry to the feed-item catalog -- the tenant's feed
	// vocabulary. Its OWN kind rather than part of 'ration_rate' because adding an item authors no
	// quantity: a new item feeds nothing until a rate, a shed factor or an experiment cell names it.
	// Added to the schema by migration 000136.
	WriteKindFeedItem = "feed_item"
)

// Experiment row statuses, mirroring feed_experiment_config.status.
//
// STATUS IS THE WORKFLOW SWITCH, and this is the single most consequential fact about this table.
// Membership in feed_experiment_config with status='active' IS what makes a shed an experiment shed
// -- domain.ExperimentPlanner.Applies has no separate flag to consult, and the direction path reads
// only active rows. So retiring a shed's rows does not merely hide them: it moves that shed back
// onto the per-head ration grid and changes what its animals are fed. Conversely a shed that should
// be on absolute kg but has no active row here is fed head_count x grams_per_head, which for the 34
// live experiment sheds was measured at roughly 2.2x the authored quantity.
//
// Withdrawal is therefore a STATUS FLIP rather than a DELETE, so the authored quantities survive a
// withdraw-and-restore instead of having to be re-keyed from the workbook.
const (
	ExperimentStatusActive  = "active"
	ExperimentStatusRetired = "retired"
)

// ---------------------------------------------------------------------------
// Read shapes
// ---------------------------------------------------------------------------

// Page is the bounded paging window every list read accepts.
//
// Bounded limit+offset rather than keyset, deliberately. These are AUTHORED CONFIG tables, not
// growing event streams: the whole live grid is 1442 rates, 31 tags, 10 items, 7 groups, and a
// handful of session/schedule rows. The set is small, stable, and the operator pages a grid rather
// than draining a queue, so an offset walk is bounded by construction -- and the service rejects an
// offset past MaxOffset outright rather than letting it grow.
type Page struct {
	Limit  int32
	Offset int32
}

// RationRate is one authored cell of the editable grid.
//
// GramsPerHead is a DECIMAL STRING, never a float64. numeric(12,3) is exact and a float round-trip
// is not; a rate that reads back as 149.99999999 after an edit is a real, visible defect on a
// screen whose whole purpose is authoring exact numbers.
type RationRate struct {
	RationRateID     string  `json:"ration_rate_id"`
	ParkID           string  `json:"park_id"`
	RationGroupLabel string  `json:"ration_group"`
	ShedTagLabel     string  `json:"shed_tag"`
	FeedItemLabel    string  `json:"feed_item"`
	GramsPerHead     string  `json:"grams_per_head"`
	ValidFrom        string  `json:"valid_from"`
	ValidTo          *string `json:"valid_to,omitempty"`
	SourceSystem     string  `json:"source_system"`
}

type RationRateQuery struct {
	TenantID string
	ParkID   string
	// Optional narrowing filters. Empty means "no filter" -- never "match empty".
	RationGroup string
	ShedTag     string
	FeedItem    string
	Page        Page
}

type RationRatePage struct {
	Items  []RationRate `json:"items"`
	Limit  int32        `json:"limit"`
	Offset int32        `json:"offset"`
	// HasMore is derived by fetching Limit+1 rows and reporting whether the extra one existed. It is
	// NOT a total count: counting the whole filtered set on every page is the compute-on-read shape
	// the scale rules ban, and the grid UI needs "is there another page", not a total.
	HasMore bool `json:"has_more"`
}

// RationGroup is one breed -> ration-group mapping. Adult breeds only: kids resolve to the fixed
// 'Kid' group by age band and never consult this table (see migration 000003).
type RationGroup struct {
	RationGroupID    string `json:"ration_group_id"`
	BreedLabel       string `json:"breed"`
	RationGroupLabel string `json:"ration_group"`
}

type RationGroupPage struct {
	Items   []RationGroup `json:"items"`
	Limit   int32         `json:"limit"`
	Offset  int32         `json:"offset"`
	HasMore bool          `json:"has_more"`
}

// ShedTag is one entry of the authored tag vocabulary the grid is indexed by.
type ShedTag struct {
	ShedTagID    string `json:"shed_tag_id"`
	ShedTagLabel string `json:"shed_tag"`
	// AppliesTo splits the kid course from the adult course. The two sets are disjoint in the source
	// grid, which is why this is single-valued.
	AppliesTo    string `json:"applies_to"`
	DisplayOrder int32  `json:"display_order"`
	Status       string `json:"status"`
}

type ShedTagQuery struct {
	TenantID string
	// AppliesTo optionally narrows to 'adult' or 'kid'. Empty means both.
	AppliesTo string
	Page      Page
}

type ShedTagPage struct {
	Items   []ShedTag `json:"items"`
	Limit   int32     `json:"limit"`
	Offset  int32     `json:"offset"`
	HasMore bool      `json:"has_more"`
}

// FeedItem is one catalog entry. The nutritional attributes are pointers because they are genuinely
// nullable in the schema: a missing energy value blocks a rollup, never a feeding decision, so it
// is an honest gap rather than a value to invent.
type FeedItem struct {
	FeedItemID      string  `json:"feed_item_id"`
	FeedItemLabel   string  `json:"feed_item"`
	EnergyKcalPerKg *string `json:"energy_kcal_per_kg,omitempty"`
	DryMatterFactor *string `json:"dry_matter_factor,omitempty"`
	WastageFactor   *string `json:"wastage_factor,omitempty"`
	DisplayOrder    int32   `json:"display_order"`
	Status          string  `json:"status"`
}

type FeedItemPage struct {
	Items   []FeedItem `json:"items"`
	Limit   int32      `json:"limit"`
	Offset  int32      `json:"offset"`
	HasMore bool       `json:"has_more"`
}

// SessionTemplate is one feeding session and the fraction of the day's quantity it carries.
type SessionTemplate struct {
	SessionTemplateID string `json:"session_template_id"`
	ParkID            string `json:"park_id"`
	SessionNo         int32  `json:"session_no"`
	SessionLabel      string `json:"session_label"`
	SplitFraction     string `json:"split_fraction"`
	DisplayOrder      int32  `json:"display_order"`
	Status            string `json:"status"`
}

type SessionTemplateQuery struct {
	TenantID string
	ParkID   string
	Page     Page
}

type SessionTemplatePage struct {
	Items   []SessionTemplate `json:"items"`
	Limit   int32             `json:"limit"`
	Offset  int32             `json:"offset"`
	HasMore bool              `json:"has_more"`
}

// ScheduleConfig is one park/workflow dispatch clock.
//
// The three times are LOCAL Asia/Kolkata wall-clock strings ("07:00:00"), carrying no offset, and
// are rendered/edited as such. They are recurring business-calendar rules, not instants: the
// consumer combines (business date, this local time, Asia/Kolkata) to schedule. See migration
// 000004.
//
// TransportTime is a pointer because NULL is meaningful: the park has not declared a cutoff. It
// reads as UNKNOWN, never as "no deadline".
type ScheduleConfig struct {
	ScheduleConfigID string  `json:"schedule_config_id"`
	ParkID           string  `json:"park_id"`
	Workflow         string  `json:"workflow"`
	DirectionTime    string  `json:"direction_time"`
	CorrectionTime   string  `json:"correction_time"`
	TransportTime    *string `json:"transport_time,omitempty"`
	ValidFrom        string  `json:"valid_from"`
	ValidTo          *string `json:"valid_to,omitempty"`
}

type ScheduleConfigQuery struct {
	TenantID string
	ParkID   string
	// Workflow optionally narrows to one workflow. Empty means both.
	Workflow string
	Page     Page
}

type ScheduleConfigPage struct {
	Items   []ScheduleConfig `json:"items"`
	Limit   int32            `json:"limit"`
	Offset  int32            `json:"offset"`
	HasMore bool             `json:"has_more"`
}

// ShedFactor is one per-shed, per-item multiplier -- the third term of
// head_count x grams_per_head x shed_factor.
type ShedFactor struct {
	ShedFactorID  string  `json:"shed_factor_id"`
	ParkID        string  `json:"park_id"`
	ShedID        string  `json:"shed_id"`
	FeedItemLabel string  `json:"feed_item"`
	Multiplier    string  `json:"multiplier"`
	ValidFrom     string  `json:"valid_from"`
	ValidTo       *string `json:"valid_to,omitempty"`
}

type ShedFactorQuery struct {
	TenantID string
	ParkID   string
	// ShedID optionally narrows to one shed. Empty means every shed in the park.
	ShedID   string
	FeedItem string
	Page     Page
}

type ShedFactorPage struct {
	Items   []ShedFactor `json:"items"`
	Limit   int32        `json:"limit"`
	Offset  int32        `json:"offset"`
	HasMore bool         `json:"has_more"`
}

// ExperimentConfig is one authored cell of an EXPERIMENT shed: the absolute kg of one feed item that
// the whole shed is fed.
//
// ABSOLUTE KG IS A SHED TOTAL, NOT A PER-HEAD RATE. That is the one distinction between this type
// and RationRate that must never blur. HeadCount travels with it as INFORMATIONAL context -- the
// population the operator authored the figure against -- and multiplying the two would overfeed the
// shed by a factor of its entire population. Nothing in this module, the generator, or the UI may
// treat HeadCount as a multiplier; ExperimentPlanner ignores the projected count for quantity
// purposes entirely and flags the row so nothing downstream can scale by it.
//
// HeadCount is a POINTER because the column is nullable: a shed whose population was not recorded
// alongside the quantity is an honest gap, and rendering it as 0 would state that the shed is empty.
type ExperimentConfig struct {
	ExperimentConfigID string `json:"experiment_config_id"`
	ParkID             string `json:"park_id"`
	// ParkName is carried because this list may span BOTH parks when the caller asks for a
	// company-wide view. The shed name cannot stand in for it: Castro, Gandhi and Yashoda each exist
	// in both parks, so a cross-park row labelled by shed alone is ambiguous.
	ParkName string `json:"park_name"`
	ShedID   string `json:"shed_id"`
	// ShedName and PartitionLabel are the two halves of the ground location, and they must always
	// travel together. A partitioned shed authors ONE CELL PER PEN, so shed_id alone does not
	// identify a row: Mandela 1 holds ten pens, each with its own arm, head count and quantities.
	// Before these fields existed the screen rendered ten identical "Mandela 1 / Dry Masoor Bhusa"
	// rows differing only by a number, which no operator could tell apart.
	ShedName string `json:"shed_name"`
	// PartitionLabel is the HUMAN label ('Part 3', '2'), never the normalized matching key ('3').
	// Empty means an undivided shed -- 'whole' is a matching sentinel and never reaches a client.
	PartitionLabel string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is composed by the backend via oploc so every surface reads the
	// same string ("Mandela 1 - Part 3"); clients render it verbatim and never rejoin the halves.
	OperationalLocationDisplay string `json:"operational_location_display"`
	FeedItemLabel              string `json:"feed_item"`
	// AbsoluteKg is an exact decimal string for the same reason GramsPerHead is: numeric(12,3) is
	// exact and a float round-trip is not.
	AbsoluteKg string `json:"absolute_kg"`
	HeadCount  *int32 `json:"head_count,omitempty"`
	// ExperimentCategory is the experiment ARM. It stands in for the shed tag on the direction sheet,
	// because an experiment shed has no ration grain and therefore no authored tag to report.
	ExperimentCategory string `json:"experiment_category"`
	Status             string `json:"status"`
}

type ExperimentConfigQuery struct {
	TenantID string
	ParkID   string
	// ShedID optionally narrows to one shed. Empty means every experiment shed in the park.
	ShedID string
	// Status optionally narrows to 'active' or 'retired'. Empty means BOTH, which is what the config
	// screen wants: a withdrawn shed's authored quantities must stay visible so it can be restored
	// without re-keying them from the workbook.
	Status string
	Page   Page
}

type ExperimentConfigPage struct {
	Items   []ExperimentConfig `json:"items"`
	Limit   int32              `json:"limit"`
	Offset  int32              `json:"offset"`
	HasMore bool               `json:"has_more"`
}

// ExperimentBatchCell is one authored feed item inside a batch enrolment.
type ExperimentBatchCell struct {
	FeedItemLabel string
	// AbsoluteKg is an exact decimal string, already normalized. Same absent-vs-zero contract as the
	// single-cell write: a cell the author left blank is NOT in this slice at all, and a cell that IS
	// here carries a real authored number, which may legitimately be "0".
	AbsoluteKg string
}

// UpsertExperimentConfigBatchCommand authors EVERY feed item of one pen in a single transaction.
//
// WHY THIS IS ATOMIC AND NOT N SINGLE-CELL WRITES. Membership in feed_experiment_config IS what puts
// a pen on the experiment workflow, and ExperimentPlanner treats the pen's authored cells as the
// COMPLETE list of what it is fed -- it does not fall back to the ration grid for a missing item. So
// a partly-applied enrolment does not leave the pen unconfigured and loud; it leaves the pen ON the
// experiment, fed only the items that happened to commit, on a sheet that looks complete. That is a
// silent underfeed of live animals, which is why the whole set commits or none of it does.
//
// The pen's arm and head count are carried once, not per cell: they describe the PEN, and letting
// them vary per cell is how a pen ends up with two arms and the display picks whichever row sorted
// first.
type UpsertExperimentConfigBatchCommand struct {
	WriteIdentity
	ParkID string
	ShedID string
	// PartitionLabel names WHICH PEN. Empty is legitimate (an undivided shed) and authors the
	// shed-wide row; on a partitioned shed an empty label is the defect that quietly creates a
	// phantom whole-shed row beside the real pens.
	PartitionLabel     string
	ExperimentCategory string
	HeadCount          *int32
	// Cells is the authored set, at least one. Duplicate feed items are rejected before this point:
	// two cells normalizing to the same key would race each other inside one statement and the
	// survivor would be arbitrary.
	Cells []ExperimentBatchCell
}

// Pen is ONE operational location in a park: a physical shed plus, when the shed is subdivided, the
// pen within it. It is the catalog the experiment enroller offers, and it exists because the
// experiment table cannot supply that list itself.
//
// WHY A SEPARATE READ AND NOT A DISTINCT OVER feed_experiment_config. The enroller's whole job is to
// offer a location that has NO experiment rows yet, so deriving the list from the experiment table
// can only ever return locations that are already enrolled. Deriving it from the SHED list is the
// bug this replaces: CBE's Godel 1 holds ten pens of which seven were enrolled, and a shed-keyed
// candidate list saw "Godel 1 is already an experiment shed" and hid the other three -- Part 8 could
// not be enrolled from the screen at all, and the only route was a hand-written database write.
//
// A pen with ZERO animals is still a real pen and is still offered. The catalog is the locations /
// shed_partitions truth, never a per-goat table: deriving it from goat placement hides an empty pen,
// and an empty pen is exactly the one an operator is about to move animals into and wants configured
// first. This read touches no per-animal table.
type Pen struct {
	ParkID   string `json:"park_id"`
	ShedID   string `json:"shed_id"`
	ShedName string `json:"shed_name"`
	// PartitionLabel is the HUMAN label ('Part 3', '2'), never the normalized matching key ('3') and
	// never the 'whole' sentinel. Empty means the shed is undivided.
	PartitionLabel string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is composed by the backend via oploc, so this list reads identically
	// to the experiment table it feeds and to every other surface.
	OperationalLocationDisplay string `json:"operational_location_display"`
	// HasExperimentConfig reports whether this pen already has at least one authored experiment cell.
	// Computed here, next to the catalog, rather than left to the client to infer by matching names:
	// name-matching across a partitioned shed is exactly the keying mistake this whole area keeps
	// making, and the pen's identity is (shed_id, partition) which only the backend holds reliably.
	HasExperimentConfig bool `json:"has_experiment_config"`
}

type PenQuery struct {
	TenantID string
	ParkID   string
	Page     Page
}

type PenPage struct {
	Items   []Pen `json:"items"`
	Limit   int32 `json:"limit"`
	Offset  int32 `json:"offset"`
	HasMore bool  `json:"has_more"`
}

// ---------------------------------------------------------------------------
// Write shapes
// ---------------------------------------------------------------------------

// WriteResult describes what an authored edit actually did.
//
// It is deliberately explicit about the effective-dating outcome rather than returning a bare
// "ok": the operator who just changed a rate needs to know whether they opened a new window
// (superseded), corrected today's authoring (corrected), created the first value (inserted), or
// changed nothing (unchanged). Those are four different states of the audit trail.
type WriteResult struct {
	WriteID string `json:"write_id"`
	Kind    string `json:"kind"`
	Outcome string `json:"outcome"`
	// ResultRowID is the row now in force. Empty only for an 'unchanged' write.
	ResultRowID string `json:"result_row_id,omitempty"`
	// SupersededRowID is the row this write closed. Set only for 'superseded'.
	SupersededRowID string `json:"superseded_row_id,omitempty"`
	EffectiveFrom   string `json:"effective_from"`
	// Replayed reports that this response is the ORIGINAL result of an earlier identical request,
	// replayed without re-running any side effect.
	Replayed bool `json:"idempotent_replay"`
}

// WriteIdentity is the idempotency envelope every authored write carries. Embedded rather than
// repeated so a new write kind cannot accidentally ship without it.
type WriteIdentity struct {
	TenantID string
	// ActorRef identifies who authored the change, for the ledger's audit trail.
	ActorRef string
	// EffectiveFrom is the Asia/Kolkata business date the change takes effect. Server-derived from
	// the business calendar, never from the client and never from SQL now() -- a late-evening IST
	// write must not be dated to the previous UTC day.
	EffectiveFrom string
	// IdempotencyKey is the client's key. RequestFingerprint is a stable hash of the canonical
	// client request; the pair is what makes an exact replay return the original result and a
	// same-key/different-payload replay a conflict.
	IdempotencyKey     string
	RequestFingerprint string
}

// UpsertRationRateCommand authors one cell of the grid.
//
// GramsPerHead is a canonical decimal STRING and is REQUIRED. It is not a float64 and not a
// pointer-with-default: "absent" is not representable here on purpose, because the HTTP layer must
// have already rejected an absent value rather than defaulting it to 0. See the package comment.
type UpsertRationRateCommand struct {
	WriteIdentity
	ParkID           string
	RationGroupLabel string
	ShedTagLabel     string
	FeedItemLabel    string
	GramsPerHead     string
}

// UpsertShedFactorCommand authors one shed multiplier.
type UpsertShedFactorCommand struct {
	WriteIdentity
	ParkID        string
	ShedID        string
	FeedItemLabel string
	Multiplier    string
}

// UpsertScheduleConfigCommand authors one park/workflow dispatch clock. Times are local
// Asia/Kolkata wall-clock strings; TransportTime is optional and stays NULL when absent.
type UpsertScheduleConfigCommand struct {
	WriteIdentity
	ParkID         string
	Workflow       string
	DirectionTime  string
	CorrectionTime string
	TransportTime  *string
}

// UpsertExperimentConfigCommand authors one experiment shed's absolute kg of one feed item.
//
// AbsoluteKg is a canonical decimal STRING and is REQUIRED, exactly like GramsPerHead: "absent" is
// not representable, because the HTTP layer must already have rejected a cleared field rather than
// filling it with 0. An authored 0 IS legal (an arm that deliberately gets none of an item).
//
// HeadCount is a pointer so "not recorded" (NULL) stays distinct from an authored 0, which would
// state the shed is empty.
type UpsertExperimentConfigCommand struct {
	WriteIdentity
	ParkID string
	ShedID string
	// PartitionLabel names WHICH PEN of the shed this cell belongs to. It is part of the row's
	// identity, not decoration: the natural key is
	// (tenant_id, park_id, shed_id, partition_key, feed_item_key), so a write that omits it targets
	// the 'whole' sentinel and inserts a phantom shed-wide row instead of editing the pen the
	// author clicked. Empty is legitimate for an undivided shed and normalizes to 'whole'.
	PartitionLabel     string
	FeedItemLabel      string
	AbsoluteKg         string
	HeadCount          *int32
	ExperimentCategory string
}

// CreateFeedItemCommand adds one entry to the tenant's feed-item catalog.
//
// ADDING AN ITEM AUTHORS NO QUANTITY. That is the whole safety property of this command and the
// reason it is a plain create rather than an upsert of anything: the catalog is a VOCABULARY. A new
// item is fed to nothing until a ration rate, a shed factor or an experiment cell names it, so this
// write cannot change what any animal eats today. Nothing here may grow into a path that authors a
// rate on the author's behalf -- an invented rate would be exactly the "value nobody entered" the
// package comment bans, and an invented ZERO would read as "feed none of it" forever.
//
// The three nutritional attributes are POINTERS because NULL is the honest state for an item whose
// energy value nobody has measured: a missing energy figure blocks a rollup, never a feeding
// decision (see FeedItem and the column's own nullability). They are never defaulted to 0, which
// would state a measured zero.
//
// DisplayOrder is a pointer for a different reason: absent means "put it at the end", which the
// repository resolves from the catalog's current maximum inside the write transaction. That is a
// PRESENTATION position, not a business value, which is why deriving it is acceptable here while
// deriving a rate never is.
type CreateFeedItemCommand struct {
	WriteIdentity
	FeedItemLabel   string
	EnergyKcalPerKg *string
	DryMatterFactor *string
	WastageFactor   *string
	DisplayOrder    *int32
}

// SetExperimentShedStatusCommand switches ONE PEN between the experiment workflow and the normal
// per-head ration grid.
//
// This is a business-meaningful action, not a visibility toggle -- see the ExperimentStatus
// constants.
//
// WHOLE-PEN, NEVER PER-CELL. A pen half on absolute kg and half on the ration grid is not a state
// the generator can represent (a planner owns the pen, not the cell), so a per-cell status edit
// would let an author create a pen whose feed is undefined.
//
// PEN, NOT SHED (maintainer decision 2026-08-09). This was shed-scoped while the screen above it
// had already become pen-grouped, so a control captioned "Return Godel 1 - Part 3 to the standard
// ration" retired all ten Godel 1 pens. Because membership-with-status-active IS the workflow
// switch, the nine unnamed pens silently fell back to the per-head grid at roughly 2.2x their
// authored quantity -- a real change to what those animals are fed, applied by a button that named
// one pen. PartitionLabel is therefore required to identify the target, and is blank only for a
// genuinely undivided shed.
type SetExperimentShedStatusCommand struct {
	WriteIdentity
	ParkID string
	ShedID string
	// PartitionLabel is the raw authored pen ("2", "Part 3"), blank for an undivided shed. It is
	// normalized to partition_key in SQL, never in Go -- see the adapter's partitionKeyMatch.
	PartitionLabel string
	Status         string
}

// ---------------------------------------------------------------------------
// Pure validation
// ---------------------------------------------------------------------------

// FieldError names the offending field alongside the rule it broke, so the UI can attach the
// message to the right input instead of showing a generic failure.
type FieldError struct {
	Field  string
	Reason error
	Detail string
}

func (e *FieldError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Field, e.Reason.Error(), e.Detail)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Reason.Error())
}

func (e *FieldError) Unwrap() error { return e.Reason }

func fieldErr(field string, reason error, detail string) error {
	return &FieldError{Field: field, Reason: reason, Detail: detail}
}

// NormalizeDecimal validates an authored decimal and returns it in a canonical fixed-scale form
// suitable for binding as ::numeric.
//
// It REJECTS rather than repairs. A present-but-negative rate is a business error the author must
// see, not something to clamp to 0 -- clamping would write a real "feed nothing" instruction the
// author never gave. Scale beyond the column's is likewise rejected instead of rounded: silently
// turning 12.3456 into 12.346 changes the authored number.
//
// An empty string is a MISSING field, not a zero. The distinction is the whole safety rule of this
// module.
// NormalizeFeedItemKey is the Go twin of the Postgres `feed_config_norm` function: trim, casefold,
// collapse runs of whitespace/underscore/hyphen to a single underscore.
//
// It exists so a duplicate inside ONE batch is caught by the same rule the unique index would apply
// -- "RGS Concentrate" and "rgs  concentrate" are one cell, and rejecting them here is what keeps
// the write from racing two quantities onto one row. It must stay in step with the SQL function; the
// two are checked against each other in the repository's Postgres tests.
func NormalizeFeedItemKey(raw string) string {
	return feedItemKeySeparators.ReplaceAllString(strings.ToLower(strings.TrimSpace(raw)), "_")
}

var feedItemKeySeparators = regexp.MustCompile(`[\s_-]+`)

func NormalizeDecimal(field, raw string, scale int, allowZero bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fieldErr(field, ErrMissingField, "")
	}
	neg := false
	body := raw
	switch {
	case strings.HasPrefix(body, "-"):
		neg = true
		body = body[1:]
	case strings.HasPrefix(body, "+"):
		body = body[1:]
	}
	intPart, fracPart, hasFrac := strings.Cut(body, ".")
	if intPart == "" && fracPart == "" {
		return "", fieldErr(field, ErrInvalidDecimal, raw)
	}
	if !allDigits(intPart) || (hasFrac && !allDigits(fracPart)) {
		return "", fieldErr(field, ErrInvalidDecimal, raw)
	}
	if hasFrac && fracPart == "" {
		return "", fieldErr(field, ErrInvalidDecimal, raw)
	}
	if len(fracPart) > scale {
		return "", fieldErr(field, ErrInvalidDecimal,
			fmt.Sprintf("%s has more than %d decimal places", raw, scale))
	}
	// Reject before checking zero-ness so "-0.000" is treated as the zero it is rather than as a
	// negative value.
	zero := isAllZero(intPart) && isAllZero(fracPart)
	if neg && !zero {
		return "", fieldErr(field, ErrNegativeValue, raw)
	}
	if zero && !allowZero {
		return "", fieldErr(field, ErrNegativeValue, raw+" must be greater than zero")
	}
	if intPart == "" {
		intPart = "0"
	}
	intPart = strings.TrimLeft(intPart, "0")
	if intPart == "" {
		intPart = "0"
	}
	frac := fracPart + strings.Repeat("0", scale-len(fracPart))
	if scale == 0 {
		return intPart, nil
	}
	return intPart + "." + frac, nil
}

// Scales of the three nullable feed_item_catalog attributes, mirroring the migration's column
// types. Authored values are rejected rather than rounded to fit, so these must stay in step with
// the schema: energy_kcal_per_kg numeric(10,3), dry_matter_factor numeric(6,4),
// wastage_factor numeric(6,4).
const (
	energyScale     = 3
	dryMatterScale  = 4
	wastageScale    = 4
	dryMatterMaxRaw = "1"
	wastageMaxRaw   = "1"
)

// NormalizeEnergyKcalPerKg validates the optional energy attribute: >= 0, three decimal places.
//
// An authored 0 is accepted as a real measurement (an item that carries no metabolisable energy).
// It is the ABSENT case that must not be turned into one -- absent means nobody measured it, and
// that gap is reported as a gap rather than as a zero.
func NormalizeEnergyKcalPerKg(field, raw string) (string, error) {
	return NormalizeDecimal(field, raw, energyScale, true)
}

// NormalizeDryMatterFactor validates the optional dry-matter fraction: > 0 and <= 1.
//
// Both bounds mirror feed_item_catalog_dry_matter_check exactly, and both are rejections rather
// than clamps. Zero is EXCLUDED here (unlike energy) because the column excludes it: a dry-matter
// fraction of 0 says the item is entirely water, which is not a feed. A value above 1 says the
// item is more than 100% dry matter, which is not a quantity that exists.
func NormalizeDryMatterFactor(field, raw string) (string, error) {
	normalized, err := NormalizeDecimal(field, raw, dryMatterScale, false)
	if err != nil {
		return "", err
	}
	return normalized, requireAtMost(field, normalized, dryMatterMaxRaw, dryMatterScale, true)
}

// NormalizeWastageFactor validates the optional wastage fraction: >= 0 and < 1.
//
// Mirrors feed_item_catalog_wastage_check. An authored 0 IS legal (an item with no expected
// wastage); 1 is not, because a wastage fraction of 1 says the entire quantity is lost, leaving
// nothing fed.
func NormalizeWastageFactor(field, raw string) (string, error) {
	normalized, err := NormalizeDecimal(field, raw, wastageScale, true)
	if err != nil {
		return "", err
	}
	return normalized, requireAtMost(field, normalized, wastageMaxRaw, wastageScale, false)
}

// requireAtMost enforces an upper bound on an ALREADY-canonical decimal.
//
// The comparison is done on scaled INTEGER units rather than on float64: the bound cases here are
// exactly 1.0000, and a float round-trip is precisely where an equality check at a boundary stops
// being reliable. Both operands come from NormalizeDecimal, so they share a fixed scale and their
// digit strings compare as integers.
func requireAtMost(field, canonical, maxRaw string, scale int, inclusive bool) error {
	max, err := NormalizeDecimal(field, maxRaw, scale, true)
	if err != nil {
		return err
	}
	value, err := decimalUnits(canonical, scale)
	if err != nil {
		return fieldErr(field, ErrInvalidDecimal, canonical)
	}
	limit, err := decimalUnits(max, scale)
	if err != nil {
		return fieldErr(field, ErrInvalidDecimal, max)
	}
	if value > limit || (!inclusive && value == limit) {
		bound := "less than"
		if inclusive {
			bound = "at most"
		}
		return fieldErr(field, ErrValueOutOfRange,
			fmt.Sprintf("%s must be %s %s", canonical, bound, maxRaw))
	}
	return nil
}

// decimalUnits turns a canonical fixed-scale decimal ("0.8500") into its integer count of scaled
// units (8500). Exact by construction: NormalizeDecimal has already guaranteed the shape.
func decimalUnits(canonical string, scale int) (int64, error) {
	intPart, fracPart, _ := strings.Cut(canonical, ".")
	if scale > 0 && len(fracPart) != scale {
		return 0, fmt.Errorf("feedconfig: %q is not at scale %d", canonical, scale)
	}
	return strconv.ParseInt(intPart+fracPart, 10, 64)
}

// ValidateDisplayOrder checks the optional catalog sort position.
//
// nil is legal and means "put it at the end", resolved by the write path from the catalog's current
// maximum. A present negative value is rejected rather than clamped, for the same
// validate-or-reject reason as every other authored field -- though note what is NOT at stake here:
// display_order is a presentation position, so a wrong one misorders a dropdown and never misfeeds
// an animal. That is exactly why deriving an absent one is acceptable while deriving an absent rate
// is not.
func ValidateDisplayOrder(field string, raw *int32) (*int32, error) {
	if raw == nil {
		return nil, nil
	}
	if *raw < 0 {
		return nil, fieldErr(field, ErrNegativeValue, fmt.Sprintf("%d", *raw))
	}
	return raw, nil
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isAllZero(s string) bool {
	for _, r := range s {
		if r != '0' {
			return false
		}
	}
	return true
}

// NormalizeLocalTime validates an authored Asia/Kolkata wall-clock time and canonicalizes it to
// HH:MM:SS.
//
// It accepts HH:MM and HH:MM:SS and rejects anything else -- including an offset suffix. An offset
// is not merely unsupported here, it is WRONG: these values are recurring business-calendar rules,
// and accepting "07:00+05:30" would imply the stored value is tied to an instant. See migration
// 000004's time-semantics note.
func NormalizeLocalTime(field, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fieldErr(field, ErrMissingField, "")
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return "", fieldErr(field, ErrInvalidTime, raw)
	}
	bounds := []int{23, 59, 59}
	vals := make([]int, 3)
	for i, p := range parts {
		if len(p) != 2 || !allDigits(p) {
			return "", fieldErr(field, ErrInvalidTime, raw)
		}
		v := int(p[0]-'0')*10 + int(p[1]-'0')
		if v > bounds[i] {
			return "", fieldErr(field, ErrInvalidTime, raw)
		}
		vals[i] = v
	}
	return fmt.Sprintf("%02d:%02d:%02d", vals[0], vals[1], vals[2]), nil
}

// ValidateWorkflow rejects anything outside the migration's CHECK vocabulary, before SQL sees it.
func ValidateWorkflow(field, raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "", fieldErr(field, ErrMissingField, "")
	}
	if v != WorkflowNormal && v != WorkflowExperiment {
		return "", fieldErr(field, ErrInvalidWorkflow, raw)
	}
	return v, nil
}

// ValidateExperimentStatus rejects anything outside feed_experiment_config's status vocabulary.
//
// `required=false` allows the empty string as "no filter" for reads. On a WRITE the status is the
// whole point of the request, so an absent one is a missing field rather than a default: guessing
// 'active' would enrol a shed onto absolute kg, and guessing 'retired' would move it back onto the
// per-head grid. Both are changes to what animals are fed, and neither is a safe default.
func ValidateExperimentStatus(field, raw string, required bool) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		if required {
			return "", fieldErr(field, ErrMissingField, "")
		}
		return "", nil
	}
	if v != ExperimentStatusActive && v != ExperimentStatusRetired {
		return "", fieldErr(field, ErrInvalidExperimentStatus, raw)
	}
	return v, nil
}

// ValidateHeadCount checks the INFORMATIONAL population figure carried alongside an absolute
// quantity.
//
// It is validate-or-reject like every other authored value: a present-but-negative count fails
// rather than being clamped. nil is legal and means "not recorded" -- which is NOT the same as 0,
// and is why this returns the pointer through unchanged rather than defaulting it.
//
// Note what this function does NOT do: it never influences a quantity. head_count is not a
// multiplier here (see ExperimentConfig), so an out-of-range value cannot under- or over-feed a
// shed; it is rejected because a wrong number printed next to a feeding instruction misleads the
// operator reading it.
func ValidateHeadCount(field string, raw *int32) (*int32, error) {
	if raw == nil {
		return nil, nil
	}
	if *raw < 0 {
		return nil, fieldErr(field, ErrNegativeValue, fmt.Sprintf("%d", *raw))
	}
	return raw, nil
}

// ValidateAppliesTo rejects a shed-tag course filter outside the schema vocabulary.
func ValidateAppliesTo(field, raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "", nil
	}
	if v != "adult" && v != "kid" {
		return "", fieldErr(field, ErrInvalidAppliesTo, raw)
	}
	return v, nil
}

// ValidateScheduleOrder enforces the same ordering the schema CHECKs, one layer earlier so the
// author gets a field-level message instead of a constraint violation.
//
// A correction amends a direction that was already issued, so it cannot precede it (equality is
// legal -- that is the live experiment case). A transport cutoff before the correction batch would
// make every correction dead on arrival.
func ValidateScheduleOrder(direction, correction string, transport *string) error {
	if correction < direction {
		return fieldErr("correction_time", ErrTimeOrder,
			fmt.Sprintf("correction_time %s is before direction_time %s", correction, direction))
	}
	if transport != nil && *transport < correction {
		return fieldErr("transport_time", ErrTimeOrder,
			fmt.Sprintf("transport_time %s is before correction_time %s", *transport, correction))
	}
	return nil
}

// RequireNonBlank is the guard for authored labels and identifiers. A blank label is missing, not
// empty-valued: the schema's *_not_blank CHECKs say the same thing, and catching it here gives the
// author the field name.
func RequireNonBlank(field, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", fieldErr(field, ErrMissingField, "")
	}
	return v, nil
}

// ValidateBusinessDate checks a YYYY-MM-DD business date. It does not accept a timestamp: an
// effective date is a business-calendar day, and letting an instant through would reintroduce the
// UTC-vs-Asia/Kolkata day-boundary bug the date form exists to avoid.
func ValidateBusinessDate(field, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", fieldErr(field, ErrMissingField, "")
	}
	if len(v) != 10 || v[4] != '-' || v[7] != '-' ||
		!allDigits(v[0:4]) || !allDigits(v[5:7]) || !allDigits(v[8:10]) {
		return "", fieldErr(field, ErrInvalidDate, raw)
	}
	month := int(v[5]-'0')*10 + int(v[6]-'0')
	day := int(v[8]-'0')*10 + int(v[9]-'0')
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return "", fieldErr(field, ErrInvalidDate, raw)
	}
	return v, nil
}
