package ports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

var (
	ErrNotFound              = errors.New("identity record not found")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with different request")
	ErrIdempotencyPending    = errors.New("idempotency key is not completed")
	ErrWriteConflict         = errors.New("identity write conflict")
	ErrNoTemporaryIdentifier = errors.New("identity goat has no active temporary identifier to promote")
	ErrInvalidReference      = errors.New("identity referenced record is invalid")
	ErrInvalidCursor         = errors.New("invalid pagination cursor")
	ErrGuardrailRequired     = errors.New("identity critical transition requires guardrail")
	ErrInvalidChronology     = errors.New("identity dob must be on or before entry_date")
	// ErrPartitionRequired: Admin animal creation targeted a shed that has active catalog
	// partitions without naming one. A parent shed is not an animal's ground location once that
	// shed is subdivided, so the write must fail instead of silently placing the animal at shed
	// grain.
	ErrPartitionRequired = errors.New("identity partition_label is required for a partitioned shed")
	// ErrPartitionNotInShed: a create/placement named a partition_label that does not exist in
	// shed_partitions for that shed. Rejected rather than stored, so a typo can never put an
	// animal in a pen that is not real; a shed-level placement omits the field instead.
	ErrPartitionNotInShed             = errors.New("identity partition_label is not a real partition of this shed")
	ErrFutureAnchor                   = errors.New("identity dob/entry_date cannot be in the future")
	ErrCriticalDeathGuardrailRequired = fmt.Errorf("%w: death exit", ErrGuardrailRequired)
	// ErrCrossParkMove: goats never move between parks (maintainer decision 2026-07-19).
	// Shed-to-shed movement exists only WITHIN one park; leaving a park is a terminal
	// exit (transferred/sold), never a move. Initial placement (a goat with no prior
	// park) is not a move and is never blocked by this rule.
	ErrCrossParkMove = errors.New("identity cross-park goat movement does not exist")
	// ErrDestinationTagRequired: shifting into an EMPTY destination shed (no live animals to
	// derive an operational cohort from) requires the caller to supply the destination
	// management_stage explicitly. Silently keeping the moved animal's old tag would leave it
	// classified and fed under its previous cohort in the new shed (maintainer decision
	// 2026-07-19: a shed is homogeneous — every animal in it shares one management_stage).
	ErrDestinationTagRequired = errors.New("identity relocate: destination shed is empty and requires an explicit destination management_stage")
	// ErrDestinationTagConflict: the caller supplied a destination management_stage that disagrees
	// with the tag the OCCUPIED destination shed's existing animals already carry. A shed cannot
	// hold two cohorts, so the move is rejected rather than corrupting the shed's homogeneity.
	ErrDestinationTagConflict = errors.New("identity relocate: supplied destination management_stage disagrees with the occupied destination shed's cohort")
	// ErrDestinationStageAmbiguous: the destination shed's existing live animals carry more than one
	// distinct management_stage, so no single cohort tag can be derived. This is a data-integrity
	// violation of the homogeneous-shed invariant and must be reconciled before a move can adopt a tag.
	ErrDestinationStageAmbiguous = errors.New("identity relocate: destination shed holds more than one management_stage and is not homogeneous")
	// ErrClinicalDestinationTag: the resolved destination cohort is a CLINICAL state (sick,
	// under_treatment, recovering, quarantine, icu -- protocol/domain.MandatoryClinicalDeferStates).
	// A shed move must not FABRICATE a clinical fact: being sick/under treatment/in quarantine/in ICU
	// is established by a health event, never by walking an animal into a shed. Adopting a clinical
	// destination tag from a move would silently mark a healthy animal as clinical (and, via the
	// clinical defer rule, suppress its vaccination work). Fail closed: the animal's clinical state
	// must be set by the owning clinical flow first; the move then follows an already-diagnosed
	// animal. (Reproductive cohorts like Pregnant/Lactating are intentionally NOT rejected here — see
	// resolveDestinationTag's scope note; that distinction is part of the shed_profiles work.)
	ErrClinicalDestinationTag = errors.New("identity relocate: destination cohort is a clinical state that a shed move must not fabricate")
	// ErrDestinationProfileMissing: the destination shed has no ACTIVE configured operational profile
	// (an active shed_profiles row joined through animal_stage_lookup). The destination cohort is
	// authoritative CONFIGURATION -- read from shed_profiles, never inferred from resident goats
	// (domain-event-architecture shifting_completion_to_vaccination contract) -- so a shed that is not
	// configured has no authority to assign a cohort and the move FAILS CLOSED. This is exactly the
	// empty/spare-shed case: configure the shed's profile first, then move animals into it.
	ErrDestinationProfileMissing = errors.New("identity relocate: destination shed has no active configured operational profile")
	// ErrShedProfileStageRequired: a goat is being CREATED into a shed with no supplied
	// management_stage, but the shed has no ACTIVE configured operational profile stage to inherit
	// (an active shed_profiles row joined through animal_stage_lookup). "One shed, one tag": a
	// created animal adopts its shed's CONFIGURED stage — the same authority a shifting move uses
	// (resolveDestinationTag) — so the shed stays homogeneous from birth/intake onward and
	// stage-scoped work (feed ration resolution, vaccination eligibility) can classify the animal.
	// A blank stage previously persisted as NULL and blocked the whole shed's feed packing
	// (unknown_shed_tag). Fail closed rather than create a stage-less alive animal: configure the
	// shed's profile first, then create animals into it (or supply an explicit stage).
	ErrShedProfileStageRequired = errors.New("identity create goat: shed has no active configured profile stage to assign and no management_stage was supplied")
)

type SearchGoatsParams struct {
	TenantID       string
	Limit          int
	Cursor         *string
	Query          *string
	GoatID         *string
	IdentifierType *string
	ScopeKey       *string
	Breed          *string
	Sex            *string
	FarmID         *string
	ParkID         *string
	LocationID     *string
	Status         *string
}

type ListTemporaryTaggedGoatsParams struct {
	TenantID string
	Limit    int
	Cursor   *string
	// ParkID / ShedID optionally narrow the list to one location (the operator's "Awaiting RFID"
	// park -> shed filter). Empty means no filter on that dimension. A shed belongs to exactly one
	// park, so ShedID alone is sufficient, but both may be sent by the park -> shed cascade.
	ParkID string
	ShedID string
}

type ResolveIdentifierParams struct {
	TenantID        string
	IdentifierType  string
	NormalizedValue string
	ScopeKey        *string
	FarmID          *string
	ParkID          *string
	LocationID      *string
}

type GetGoatTimelineParams struct {
	TenantID string
	GoatID   string
	Limit    int
	Cursor   *string
}

type AddGoatIdentifierCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	IdentifierType       string
	IdentifierValue      string
	NormalizedValue      string
	ScopeKey             string
	IsPrimaryForGoat     bool
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
	Reason               string
}

type RetireGoatIdentifierCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	IdentifierID         string
	Reason               string
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
}

// PromoteTemporaryIdentifierCommand atomically retires a goat's active temporary_tag and attaches
// the supplied permanent animal_identifier_1 as its primary identity, in ONE transaction. The temp
// identifier is found server-side (the operator supplies only the goat and the permanent RFID).
type PromoteTemporaryIdentifierCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	// PermanentValue is the RFID to attach as animal_identifier_1 (primary). NormalizedValue is its
	// normalized form for the uniqueness check.
	PermanentValue  string
	NormalizedValue string
	// SecondaryValue is an OPTIONAL second RFID attached as animal_identifier_2 (non-primary), exactly
	// like the birth flow's optional animal_identifier_2. Empty means only the primary is attached.
	// SecondaryNormalized is its normalized form.
	SecondaryValue      string
	SecondaryNormalized string
	EvidenceRefs        []domain.EvidenceRef
	RowVersion          int
	Reason              string
}

type MoveGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	ToParkID             string
	ToShedID             string
	// ToPartitionLabel is the destination pen, or nil for a shed-level move.
	ToPartitionLabel  *string
	Reason            string
	OccurredAt        time.Time
	EvidenceRefs      []domain.EvidenceRef
	RowVersion        int
	GuardrailApproved bool
}

type ExitGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	LifecycleStatus      string
	ExitReason           string
	Reason               string
	OccurredAt           time.Time
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
	GuardrailApproved    bool
}

type StageGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	ManagementStage      string
	Reason               string
	OccurredAt           time.Time
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
}

type HealthGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	HealthStatus         string
	Reason               string
	OccurredAt           time.Time
	EvidenceRefs         []domain.EvidenceRef
	RowVersion           int
}

type ReproductiveGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	ReproductiveStatus   string
	// BreedingDate and LastDeliveryDate are optional pregnancy-timing facts set on the same
	// transition. Nil means "leave the stored value untouched"; a value overwrites it. These feed
	// vaccination pregnancy defer/catch-up windows (see internal/vaccination schedule_policy).
	BreedingDate     *time.Time
	LastDeliveryDate *time.Time
	Reason           string
	OccurredAt       time.Time
	EvidenceRefs     []domain.EvidenceRef
	RowVersion       int
}

// IdentityGoatCommand corrects a goat's DOB and/or entry_date (a later data-entry fix). At least
// one of DOB / EntryDate must be set; nil means "leave the stored value untouched". Correcting these
// anchors changes vaccination age-routing (schedulePathForGoat) and post_arrival/birth_age due dates,
// so this command durably emits goat.identity.changed for the vaccination recheck consumer.
type IdentityGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	GoatID               string
	DOB                  *time.Time
	EntryDate            *time.Time
	Reason               string
	// OccurredAt is the SERVER processing instant used for recomputation, decision, and updated_at.
	// EffectiveAt is the optional client-supplied business effective date, retained for audit only —
	// it never drives the generation as_of or persisted timestamps (VACC-REV-07).
	OccurredAt   time.Time
	EffectiveAt  *time.Time
	EvidenceRefs []domain.EvidenceRef
	RowVersion   int
}

type AdminGoatCreateIdentifier struct {
	IdentifierType  string
	IdentifierValue string
	NormalizedValue string
	ScopeKey        string
	IsPrimary       bool
}

type ValidateAdminGoatCreateCommand struct {
	TenantID             string
	StoredIdempotencyKey string
	RequestHash          string
	Identifiers          []AdminGoatCreateIdentifier
	FarmID               *string
	FarmCode             *string
	ParkID               *string
	ParkCode             *string
	ShedID               *string
	ShedCode             *string
	PartitionLabel       *string
	// RequirePartitionGrain is enabled by the Admin single-create and bulk-import routes. The same
	// identity constructor is also used by the Counts birth approval path, whose partition contract
	// is owned separately; keeping this flag explicit prevents an Admin-only repair from changing
	// that workflow accidentally.
	RequirePartitionGrain bool
	ManagementStage       *string
	// BirthDamRef is the operator-scanned mother RFID at submit time, or the already-resolved
	// canonical mother UUID when a pending approval is applied.
	BirthDamRef *string
	Species     string
}

type AdminGoatCreateValidation struct {
	CustodianPartyID string
	FarmID           *string
	ParkID           string
	ShedID           string
	PartitionLabel   *string
	DamGoatID        *string
	Conflicts        []domain.FieldError
	Warnings         []domain.Warning
}

type CreateAdminGoatCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	IdempotencyScope     string
	RequestHash          string
	TraceID              string
	Identifiers          []AdminGoatCreateIdentifier
	CustodianPartyID     string
	FarmID               *string
	ParkID               string
	ShedID               string
	// PartitionLabel is the canonical HUMAN catalog label for the pen within ShedID. Admin writes
	// set RequirePartitionGrain, making this mandatory whenever the shed has active partitions and
	// nil only for a genuinely non-partitioned shed. Other internal callers retain their existing
	// explicit contract until their owning module supplies the flag.
	PartitionLabel *string
	// RequirePartitionGrain rechecks the Admin placement rule inside the write transaction after
	// read-only validation, so a catalog change cannot turn a valid preview into a bare-shed write.
	RequirePartitionGrain bool
	Species               string
	Breed                 *string
	Sex                   string
	DOB                   *time.Time
	DOBEstimated          bool
	OriginType            string
	EntryDate             time.Time
	ManagementStage       *string
	HealthStatus          *string
	WeightKg              *float64
	// DamID is canonical after ValidateAdminGoatCreate; it is never a copied RFID in a persisted
	// birth relationship or emitted goat.created payload.
	DamID      *string
	LitterSize *int
	// BirthEventID and BirthChildOrdinal are supplied by the Counts birth-litter submit path.
	// Every sibling shares BirthEventID and gets a stable 1-based ordinal. BirthCountStatus is
	// pending until the separate web approval activates the children in herd-count projections.
	// Direct admin birth creates leave these blank and are treated as a one-child approved event.
	BirthEventID      string
	BirthChildOrdinal int
	BirthCountStatus  string
	// TimeOfBirth is the optional HH:MM (24h, IST) birth time stored as goats.time_of_birth and
	// carried on the goat.created payload for the birth workflow opener.
	TimeOfBirth    *string
	SireOrLot      *string
	PhotoURL       *string
	SourceRecordID *string
	EvidenceRefs   []domain.EvidenceRef
}

// ResolveReproductiveMatchCommand asks whether a bulk-import row's identifiers
// resolve to a single existing goat eligible for a reproductive update.
type ResolveReproductiveMatchCommand struct {
	TenantID    string
	Identifiers []AdminGoatCreateIdentifier
}

// ReproductiveMatchResult is the resolution outcome. Matched is true only for a
// single, active (non-merged, non-exited) goat. Blocked marks a matched-but-
// ineligible goat (merged or exited) so the caller can surface a clear reason.
type ReproductiveMatchResult struct {
	Matched                   bool
	Blocked                   bool
	BlockReason               string
	GoatID                    string
	CurrentReproductiveStatus string
	RowVersion                int
	MatchConfidence           float64
}

type AdminGoatMutationResult struct {
	Goat             domain.GoatSummary
	Identifiers      []domain.GoatIdentifier
	Decision         domain.DecisionRecordSummary
	Events           []domain.EventSummary
	GenerationStatus string
	Replayed         bool
	FirstResultID    *string
}

type Repository interface {
	GetGoatByID(ctx context.Context, tenantID, goatID string) (*domain.GoatPassport, error)
	GetGoatByDisplayID(ctx context.Context, tenantID, displayID string) (*domain.GoatPassport, error)
	SearchGoats(ctx context.Context, params SearchGoatsParams) ([]domain.GoatSummary, *string, error)
	ListTemporaryTaggedGoats(ctx context.Context, params ListTemporaryTaggedGoatsParams) ([]domain.TemporaryTaggedGoat, *string, error)
	FindIdentifierMatches(ctx context.Context, params ResolveIdentifierParams) ([]domain.IdentifierMatch, error)
	FindOpenConflictForIdentifier(ctx context.Context, tenantID, identifierType, normalizedValue, scopeKey string) (*string, error)
	GetGoatTimeline(ctx context.Context, params GetGoatTimelineParams) ([]domain.GoatTimelineEvent, *string, error)
	AddGoatIdentifier(ctx context.Context, cmd AddGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	RetireGoatIdentifier(ctx context.Context, cmd RetireGoatIdentifierCommand) (*AdminGoatMutationResult, error)
	PromoteTemporaryIdentifier(ctx context.Context, cmd PromoteTemporaryIdentifierCommand) (*AdminGoatMutationResult, error)
	MoveGoat(ctx context.Context, cmd MoveGoatCommand) (*AdminGoatMutationResult, error)
	ExitGoat(ctx context.Context, cmd ExitGoatCommand) (*AdminGoatMutationResult, error)
	StageGoat(ctx context.Context, cmd StageGoatCommand) (*AdminGoatMutationResult, error)
	HealthGoat(ctx context.Context, cmd HealthGoatCommand) (*AdminGoatMutationResult, error)
	ReproductiveGoat(ctx context.Context, cmd ReproductiveGoatCommand) (*AdminGoatMutationResult, error)
	IdentityGoat(ctx context.Context, cmd IdentityGoatCommand) (*AdminGoatMutationResult, error)
	PreviewReclassifyShedStage(ctx context.Context, cmd ReclassifyShedStageCommand) (*ReclassifyShedStagePreview, error)
	ReclassifyShedStage(ctx context.Context, cmd ReclassifyShedStageCommand) (*ReclassifyShedStageResult, error)

	// The census-slice correction: fix a wrongly recorded breed or sex on the animals of ONE Counts
	// Breakdown row. Scoped to the row, not the pen -- see ports/census_correction.go for why that
	// differs from the reclassification above.
	PreviewCorrectCensusSlice(ctx context.Context, cmd CorrectCensusSliceCommand) (*CensusSlicePreview, error)
	CorrectCensusSlice(ctx context.Context, cmd CorrectCensusSliceCommand) (*CensusSliceCorrectionResult, error)
	Ping(ctx context.Context) error
}
