package ports

import (
	"errors"
	"time"
)

// CorrectCensusSlice fixes a WRONGLY RECORDED breed or sex on the animals of one Counts Breakdown
// row. It is a DATA CORRECTION, not a husbandry event: nothing about the animal changed, only what
// the register says about it.
//
// SCOPE IS THE ROW, NOT THE PEN, and that is the whole difference from ReclassifyShedStage next to
// it. A cohort tag is a property of the PEN, so retagging moves everything standing in it. Breed
// and sex are properties of the ANIMAL, and one pen legitimately holds several of each -- CBE
// Gandhi - 3 holds Beetal, Sojat and Sirohi females together. So this command matches the exact
// census slice the operator is looking at (park + stage + breed + sex + pen) and touches nothing
// else in that pen.
//
// WHY SEX IS EDITABLE AT ALL. Sex is a biological fact, not a setting, so the only honest reason to
// change it in bulk is that intake recorded it wrongly for a batch. That is why every commit
// carries a reason into the audit row: the record is being corrected, and the correction has to say
// who decided it and why.
//
// WHAT THIS DOES NOT DO, and it matters. It writes the corrected value and its audit row. It does
// NOT re-evaluate anything downstream that keyed on the old value -- a vaccination schedule derived
// from the wrong sex is not recomputed here. That cascade is a separate piece of work; until it
// exists, a correction fixes the register and a human still has to look at what the wrong value
// produced.
type CorrectCensusSliceCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string

	// The slice, exactly as the Counts Breakdown row names it. Every field participates in the
	// predicate: dropping one would silently widen the correction to animals the operator never
	// saw.
	ShedID string
	// PartitionLabel is the pen within ShedID, or nil for a shed with no pens. Matched through the
	// canonical normalizer, so '1' and 'Part 1' resolve to the same pen.
	PartitionLabel *string
	// ManagementStage, Breed and Sex identify the row. Breed may be "" -- the census shows a blank
	// breed as its own row, and correcting exactly those animals is the most common reason to
	// reach for this.
	ManagementStage string
	Breed           string
	Sex             string

	// Field is "breed" or "sex": which column this command corrects. One field per command, so an
	// audit row always states one decision.
	Field string
	// Value is the corrected value. Validated against the tenant's live breed vocabulary for
	// "breed", and against the goats_sex_check domain for "sex".
	Value string

	Reason     string
	OccurredAt time.Time
}

// MaxCensusCorrectionGoats bounds one correction. A census row is tens to low hundreds of animals;
// capping it keeps the statement's lock footprint bounded and stops a malformed slice from taking
// row locks across the herd. Same reasoning and same order of magnitude as
// MaxReclassifyGoatsPerCommand.
const MaxCensusCorrectionGoats = 1000

var (
	// ErrCensusCorrectionEmptyScope: the named slice holds no live animals. Reported rather than
	// treated as a successful no-op, because it means the row the operator clicked no longer
	// describes anything -- usually because someone else already corrected it.
	ErrCensusCorrectionEmptyScope = errors.New("identity correct census slice: no live animals match that row")
	// ErrCensusCorrectionScopeTooLarge: more animals than one command may correct.
	ErrCensusCorrectionScopeTooLarge = errors.New("identity correct census slice: row holds more live animals than one command may correct")
	// ErrCensusCorrectionField: Field was neither "breed" nor "sex".
	ErrCensusCorrectionField = errors.New("identity correct census slice: correctable field must be breed or sex")
	// ErrCensusCorrectionValue: the corrected value is not in the tenant's vocabulary for that
	// field. Fails closed rather than writing free text into a column other screens group by.
	ErrCensusCorrectionValue = errors.New("identity correct census slice: value is not a known value for that field")
	// ErrCensusCorrectionNoChange: the corrected value equals what the slice already carries.
	// Rejected rather than written, because an audit row claiming a correction that changed nothing
	// is noise in the one place that must stay readable.
	ErrCensusCorrectionNoChange = errors.New("identity correct census slice: the value is already what this row carries")
)

// CensusSlicePreview answers "what would this change" before anything is written. TotalLive is the
// whole slice, computed over the same predicate the commit uses.
type CensusSlicePreview struct {
	ShedID                     string `json:"shed_id"`
	ShedName                   string `json:"shed_name"`
	PartitionLabel             string `json:"partition_label"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	Field                      string `json:"field"`
	CurrentValue               string `json:"current_value"`
	Value                      string `json:"value"`
	TotalLive                  int    `json:"total_live"`
}

// CensusSliceCorrectionResult is what actually happened. Corrected is the number of animals whose
// row changed; it equals TotalLive unless another writer moved animals out of the slice between
// the preview and the commit.
// The json tags are load-bearing rather than decoration: this struct IS the audit row's
// after_state, and it is read back verbatim on an idempotent replay. Without them the audit wrote
// Go field names (`ShedName`, `Corrected`) into a column every other row spells in snake_case.
type CensusSliceCorrectionResult struct {
	ShedID                     string `json:"shed_id"`
	ShedName                   string `json:"shed_name"`
	PartitionLabel             string `json:"partition_label"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	Field                      string `json:"field"`
	CurrentValue               string `json:"current_value"`
	Value                      string `json:"value"`
	TotalLive                  int    `json:"total_live"`
	Corrected                  int    `json:"corrected"`
}
