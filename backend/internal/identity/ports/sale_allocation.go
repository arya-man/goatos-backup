package ports

import (
	"context"
	"errors"
	"time"

	"github.com/vgoats/goatos/backend/internal/identity/domain"
)

// Sale allocation ports: pick real animals for a recorded sale, review them, confirm.
//
// See migration 000177_goat_sale_allocations.sql for why the mapping lives on the herd
// side rather than in the sales module, and identity/domain/sale_allocation.go for the
// fail-closed blocker gate every path here runs through.

// MaxSaleAllocationGoatsPerCommand bounds ONE confirm.
//
// TWO REASONS, AND THE SECOND IS THE BINDING ONE:
//
//  1. It keeps the confirm a bounded transactional write rather than an unbounded one, so
//     a payload cannot hold row locks across the herd.
//  2. The confirm applies the CANONICAL per-goat exit transition once per animal inside
//     one transaction, which is what buys the proper goat.exited event, decision record
//     and audit row for each. That is linear work, and the hot-API budget is sub-500ms.
//
// 100 is four times the largest deal in the imported ledger (23 animals), so it never
// blocks honest work. A genuinely larger sale is split into two confirms, each atomic on
// its own. Raising this without moving the exit off the request path would spend the
// latency budget.
const MaxSaleAllocationGoatsPerCommand = 100

// SaleCandidatePageSize is the picker's keyset page size. Mobile and web alike page the
// picker rather than loading a shed; a pen can hold far more animals than a viewport.
const SaleCandidatePageSize = 50

var (
	// ErrSaleAllocationScopeTooLarge is returned when one confirm names more than
	// MaxSaleAllocationGoatsPerCommand animals.
	ErrSaleAllocationScopeTooLarge = errors.New("identity: sale allocation scope too large")
	// ErrSaleAllocationBlocked is returned when a confirm names an animal the gate
	// refuses. It carries no override: see domain.SaleBlocker for why.
	ErrSaleAllocationBlocked = errors.New("identity: sale allocation blocked")
	// ErrSaleAllocationEmpty is returned when a confirm names no animals at all.
	ErrSaleAllocationEmpty = errors.New("identity: sale allocation names no animals")
	// ErrSaleAllocationCountChanged is returned when a serialized confirm finds the
	// sale's live allocation count no longer matches the preflight count.
	ErrSaleAllocationCountChanged = errors.New("identity: sale allocation count changed")
)

// ListSaleCandidatesParams filters the picker.
//
// ShedID and PartitionLabels narrow WITHIN a park. PartitionLabels is a list because a
// person picking for one sale routinely takes animals from several pens of one shed at
// once, and making them re-filter per pen would turn one selection into three.
type ListSaleCandidatesParams struct {
	TenantID string
	ParkID   string
	ShedID   string
	// PartitionLabels are HUMAN labels ('Part 3'), matched against the pen catalog.
	// Empty means every pen of the shed.
	PartitionLabels []string
	// Query filters by identifier or display id, so a person holding a tag in their hand
	// can find that one animal without scrolling.
	Query string
	// IncludeBlocked keeps blocked animals in the list, shown greyed with their reason,
	// rather than hiding them. Hiding is worse: a person who cannot find an animal they
	// can see in the pen assumes the system is broken, whereas "In quarantine" answers
	// the question. The picker always sends true; the parameter exists so a caller that
	// genuinely wants only sellable rows does not have to filter client-side.
	IncludeBlocked bool
	Limit          int
	Cursor         string
}

// SaleCandidateRow is ONE animal exactly as the database has it: identity, where it
// stands, and the raw facts the gate judges. It carries NO verdict.
//
// The split from SaleCandidate is deliberate. The gate is domain.ResolveSaleBlocker, a
// pure Go function with its own tests; if the adapter returned a verdict, that verdict
// would have been computed in SQL and there would be two gates -- one in Postgres for the
// picker and one in Go for the confirm -- which is exactly how a blocked animal ends up
// sellable on one path. The adapter fetches; the app layer judges, once.
type SaleCandidateRow struct {
	GoatID    string
	DisplayID string
	TagNumber string

	ParkID         string
	ParkName       string
	ShedID         string
	ShedName       string
	PartitionLabel string

	Breed string
	Sex   string

	// The gate's inputs, passed straight to domain.ResolveSaleBlocker.
	State domain.SaleCandidateState
}

// SaleCandidate is ONE animal as the picker shows it: a SaleCandidateRow with the gate's
// verdict attached.
type SaleCandidate struct {
	GoatID    string
	DisplayID string
	// TagNumber is the identifier a person reads off the animal -- the active RFID when
	// it has one, otherwise its visible tag. Composed by the adapter so every surface
	// shows the same string.
	TagNumber                  string
	ParkID                     string
	ParkName                   string
	ShedID                     string
	ShedName                   string
	PartitionLabel             string
	OperationalLocationDisplay string
	Breed                      string
	Sex                        string
	ManagementStage            string
	RowVersion                 int
	// Blocker/BlockedReason come from domain.ResolveSaleBlocker. Blocker is the machine
	// code; BlockedReason is the farm sentence, rendered verbatim.
	Blocker       string
	BlockedReason string
	Sellable      bool
}

// SaleAllocationRow is one animal named in a preview or confirm.
type SaleAllocationRow struct {
	GoatID     string
	RowVersion int
}

// RecordSaleAllocationsCommand confirms a picked set onto a sale.
//
// ExpectedRowVersions is what makes the confirm no-clobber: each animal's version was
// captured when the picker listed it, and an animal whose version has since moved is
// refused rather than overwritten. Between picking and confirming, a real animal can be
// moved, treated or put in quarantine by someone else, and a confirm that ignored the
// version would sell it anyway.
type RecordSaleAllocationsCommand struct {
	TenantID             string
	ActorID              string
	ClientIdempotencyKey string
	StoredIdempotencyKey string
	RequestHash          string
	TraceID              string

	SalesDealID string
	// DeclaredAnimalCount is re-checked inside the serialized write transaction. The
	// app-layer count gate is still the friendly preflight, but this is the race gate:
	// two tabs cannot both see remaining=N and commit disjoint goats beyond the sale.
	DeclaredAnimalCount int
	Rows                []SaleAllocationRow
	Reason              string
	OccurredAt          time.Time
}

// SaleAllocationShedGroup is the confirmation screen's shape: the picked animals of ONE
// operational shed. The person confirming reads the farm back to themselves shed by shed,
// because that is how they will physically gather the animals.
type SaleAllocationShedGroup struct {
	ParkName                   string
	ShedID                     string
	ShedName                   string
	PartitionLabel             string
	OperationalLocationDisplay string
	Animals                    int
	TagNumbers                 []string
}

// SaleAllocationPreview is the review step. It MUTATES NOTHING.
type SaleAllocationPreview struct {
	SalesDealID string
	// DeclaredAnimalCount is how many animals the SALE is for, and AlreadyTagged how many
	// of them are already mapped. The screen shows the target so a person can see how many
	// more to pick, and Complete says whether Confirm may be offered at all.
	DeclaredAnimalCount int
	AlreadyTagged       int
	Complete            bool
	// Sellable and Blocked are DISJOINT and together cover every named animal, so a
	// client can render "12 ready, 2 blocked" without a third count that overlaps.
	Sellable int
	Blocked  int
	// ShedGroups covers the SELLABLE animals only -- it is the gather list, and an
	// animal that will not be sold does not belong on it.
	ShedGroups []SaleAllocationShedGroup
	// BlockedAnimals carries each refused animal with its farm-worded reason.
	BlockedAnimals []SaleCandidate
}

// SaleAllocationResult is what a confirm applied.
type SaleAllocationResult struct {
	SalesDealID string
	Allocated   int
	ShedGroups  []SaleAllocationShedGroup
}

// SaleAllocationReader is the read side, used by the picker and by the Sales page's
// read-back of which animals a deal is made of.
type SaleAllocationReader interface {
	ListSaleCandidates(ctx context.Context, params ListSaleCandidatesParams) ([]SaleCandidateRow, *string, error)
	// ReadSaleCandidateRows reads a BOUNDED set of named animals in one set-based query.
	// excludeDealID lets an animal already tagged to THIS deal read as not-blocked, so a
	// repeated confirm is a no-op rather than a self-collision. An id with no row is
	// simply absent from the result; the caller reports it through the gate's
	// not-in-the-herd verdict rather than silently dropping it.
	ReadSaleCandidateRows(ctx context.Context, tenantID, excludeDealID string, goatIDs []string) (map[string]SaleCandidateRow, error)
	// ListSaleAllocations returns the animals tagged to one deal, shed-wise.
	ListSaleAllocations(ctx context.Context, tenantID, salesDealID string) ([]SaleAllocationShedGroup, error)
	// ListSaleLocations is the picker's park/shed/pen vocabulary, legacy alias shed rows
	// excluded. See the adapter for why offering them is the reported empty-picker bug.
	ListSaleLocations(ctx context.Context, tenantID string) (*SaleLocationCatalog, error)
}

// SaleAllocationWriter is the write side.
type SaleAllocationWriter interface {
	RecordSaleAllocations(ctx context.Context, cmd RecordSaleAllocationsCommand) (*SaleAllocationResult, error)
}

// SaleLocationCatalog is the picker's park/shed/pen vocabulary.
//
// It is a CATALOG read, not a census facet read. Facets answer "where animals currently
// are"; a write picker answers "where animals are ALLOWED to be", so a pen holding zero
// animals today must still be offered. AGENTS.md makes that distinction mandatory for
// write pickers.
type SaleLocationCatalog struct {
	Parks []SaleLocationPark `json:"parks"`
	// Locations are the selectable operational locations, pen-wise. Ordered park, then
	// label, so the list reads the way the farm is walked.
	Locations []SaleLocationEntry `json:"locations"`
}

type SaleLocationPark struct {
	ParkID string `json:"park_id"`
	// Label is the park SHORT CODE (CBE, CPT) when it has one, else the full name.
	Label string `json:"label"`
}

// SaleLocationEntry is ONE selectable operational location: a pen if the shed is
// subdivided, the shed itself if it is not.
//
// PEN-WISE, NOT SHED-WISE, and that is the product rule rather than a preference:
// "active shed" means active operational LOCATION. A parent-only dropdown -- offering
// "Castro" and making the operator work out which of its three pens they mean -- is the
// exact shape AGENTS.md records as forcing operators to guess. The farm's sheds are
// painted "Castro 1", "Castro 2", "Castro 3"; those are the names people use.
type SaleLocationEntry struct {
	ShedID string `json:"shed_id"`
	ParkID string `json:"park_id"`
	// PartitionLabel is the HUMAN pen label ('1', 'Part 3'), empty for an undivided shed.
	PartitionLabel string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is the composed display, built by the canonical helper so
	// a numeric pen joins with a space ("Castro 1") and a worded one with a dash
	// ("Godel 1 - Part 3"). Clients render it verbatim.
	//
	// It carries the CONVENTION'S OWN NAME rather than a local one like "label": that is
	// what marks it as the canonical composition to the next reader, and what lets the
	// operational-location guard verify that a shed-identity response cannot render bare.
	OperationalLocationDisplay string `json:"operational_location_display"`
}

var (
	// ErrSaleDealNotFound is returned when the named deal does not exist.
	ErrSaleDealNotFound = errors.New("identity: sales deal not found")
	// ErrSaleDealNoAnimalCount is returned when the deal declares no animal count at
	// all -- a manure sale, or a row imported without one. There is no target to map
	// against, so animals cannot be tagged to it.
	ErrSaleDealNoAnimalCount = errors.New("identity: sales deal declares no animal count")
)

// SaleDeal is the little the allocation gate needs to know about a sale.
type SaleDeal struct {
	SalesDealID string
	// DeclaredAnimalCount is how many animals the SALE says it is for. It is the target
	// the mapping must hit exactly.
	DeclaredAnimalCount int
	// AlreadyTagged is how many animals are already live-tagged to this deal.
	AlreadyTagged int
}

// Remaining is how many animals still need mapping before this sale is complete.
func (d SaleDeal) Remaining() int {
	remaining := d.DeclaredAnimalCount - d.AlreadyTagged
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SaleDealReader tells the allocation gate how many animals a sale is for.
//
// IT IS AN INTERFACE, NOT A QUERY, on purpose. The sales ledger is a separate module with
// its own tables, and migration 000177 deliberately keeps identity's SQL out of the sales
// schema -- sales_deal_id is not even a foreign key -- so that a sales migration can never
// block a herd write. The count still has to be enforced SERVER-SIDE, because a
// client-supplied target is not a gate: a crafted request would simply claim whatever
// number it wanted. An interface satisfies both: identity depends on the fact, an adapter
// outside identity's own package supplies it.
type SaleDealReader interface {
	ReadSaleDeal(ctx context.Context, tenantID, salesDealID string) (*SaleDeal, error)
}
