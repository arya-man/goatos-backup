// Package domain is the Work Board's vocabulary: the one row shape every module
// normalises to, the lanes those rows sit in, and the query that bounds a read.
package domain

import (
	"errors"
	"sort"
	"strings"
	"time"

	pidomain "github.com/vgoats/goatos/backend/internal/processintegrity/domain"
)

// Module is the operational module a row belongs to. The ORDER of Modules() is the
// keyset order of the board, so it is declared once and never derived from a map.
type Module string

const (
	ModuleFeed         Module = "feed"
	ModuleHealth       Module = "health"
	ModuleVaccination  Module = "vaccination"
	ModuleWeighing     Module = "weighing"
	ModuleCounts       Module = "counts"
	ModuleMilk         Module = "milk"
	ModulePCCare       Module = "pc_care"
	ModuleToxin        Module = "toxin"
	ModuleProcurement  Module = "procurement"
	ModuleVerification Module = "verification"
)

// Modules returns every module in board order. New modules append; the order is part of
// the cursor contract, so it never changes for an existing module.
func Modules() []Module {
	return []Module{
		ModuleFeed, ModuleHealth, ModuleVaccination, ModuleWeighing, ModuleCounts,
		ModuleMilk, ModulePCCare, ModuleToxin, ModuleProcurement, ModuleVerification,
	}
}

// IsModule reports whether s names a known module.
func IsModule(s string) bool {
	for _, m := range Modules() {
		if string(m) == s {
			return true
		}
	}
	return false
}

// WorkState is REUSED VERBATIM from process integrity: the eleven states Action Center
// already renders with backend-owned copy. The board does not add a twelfth; that would
// be a maintainer decision, not an adapter's convenience.
type WorkState = pidomain.WorkState

const (
	WorkStateScheduled           = pidomain.WorkStateScheduled
	WorkStateDue                 = pidomain.WorkStateDue
	WorkStateOverdue             = pidomain.WorkStateOverdue
	WorkStateInProgress          = pidomain.WorkStateInProgress
	WorkStateProofPending        = pidomain.WorkStateProofPending
	WorkStateVerificationPending = pidomain.WorkStateVerificationPending
	WorkStateRejected            = pidomain.WorkStateRejected
	WorkStateDeferred            = pidomain.WorkStateDeferred
	WorkStateMissed              = pidomain.WorkStateMissed
	WorkStateBlocked             = pidomain.WorkStateBlocked
	WorkStateCompleted           = pidomain.WorkStateCompleted
)

// WorkStates returns the closed vocabulary in a stable order.
func WorkStates() []WorkState {
	return []WorkState{
		WorkStateScheduled, WorkStateDue, WorkStateOverdue, WorkStateInProgress,
		WorkStateProofPending, WorkStateVerificationPending, WorkStateRejected,
		WorkStateDeferred, WorkStateMissed, WorkStateBlocked, WorkStateCompleted,
	}
}

// IsWorkState reports whether s is one of the eleven.
func IsWorkState(s string) bool {
	for _, w := range WorkStates() {
		if string(w) == s {
			return true
		}
	}
	return false
}

// Severity is reused from process integrity for the same reason as WorkState.
type Severity = pidomain.Severity

const (
	SeverityOK     = pidomain.SeverityOK
	SeverityWatch  = pidomain.SeverityWatch
	SeverityAtRisk = pidomain.SeverityAtRisk
	SeverityBroken = pidomain.SeverityBroken
)

// Lane is a board column. The column is DERIVED from the work state by LaneFor and is
// never set by a client: nothing started sits in To do, anything started in In progress,
// work submitted and awaiting a verdict in In review, approved work in Done.
type Lane string

const (
	LaneToDo       Lane = "todo"
	LaneInProgress Lane = "in_progress"
	LaneInReview   Lane = "in_review"
	LaneDone       Lane = "done"
)

// Lanes returns the four columns in display order.
func Lanes() []Lane { return []Lane{LaneToDo, LaneInProgress, LaneInReview, LaneDone} }

// LaneFor maps a work state to its column. Overdue and missed work has not started, so it
// stays in To do and carries its severity; rejected work is back with the operator, so it
// is In progress. A state this function does not know lands in To do rather than
// disappearing, because a row nobody can see is worse than a row in the wrong column.
func LaneFor(state WorkState) Lane {
	switch state {
	case WorkStateInProgress, WorkStateProofPending, WorkStateRejected, WorkStateBlocked:
		return LaneInProgress
	case WorkStateVerificationPending:
		return LaneInReview
	case WorkStateCompleted:
		return LaneDone
	default:
		return LaneToDo
	}
}

// OwnerState says how a row came to have (or lack) an owner. It is the board's own
// vocabulary because process integrity's two values cannot say "this is a claim pool".
type OwnerState string

const (
	// OwnerStateAssigned: the module's own row names the person.
	OwnerStateAssigned OwnerState = "assigned"
	// OwnerStateMissing: the module expects an owner and the row has none. Reported as
	// such -- never filled with a fallback (the vaccination operator rule).
	OwnerStateMissing OwnerState = "missing"
	// OwnerStatePool: the module is a claim pool by design (health sessions, toxin steps,
	// verification). Whoever does the work is recorded after the fact.
	OwnerStatePool OwnerState = "pool"
)

// Owner is the person a row belongs to. Both identities are carried because the fleet
// keys work by user id in some modules and by workforce member in others; the board's
// self-scope filter matches on UserID, and the screens render Name.
type Owner struct {
	UserID            string `json:"user_id,omitempty"`
	WorkforceMemberID string `json:"workforce_member_id,omitempty"`
	Name              string `json:"name,omitempty"`
}

// Pen is the operational location of a row: park + physical shed + optional partition.
// Display is composed ONCE by the adapter through platform/oploc and rendered verbatim;
// clients never hand-roll it (operational-location convention, rule 5).
type Pen struct {
	ShedID         string `json:"shed_id,omitempty"`
	ShedName       string `json:"shed_name,omitempty"`
	PartitionLabel string `json:"partition_label,omitempty"`
	Display        string `json:"operational_location_display,omitempty"`
}

// Counts are the three tiles a card shows. They are the module's own arithmetic over its
// own subtasks (animals, bags, steps) and the board never recomputes them.
type Counts struct {
	Done           int `json:"done"`
	Pending        int `json:"pending"`
	NeedsAttention int `json:"needs_attention"`
}

// Row is THE contract. Every source emits exactly this.
type Row struct {
	Module     Module `json:"module"`
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	// RowKey is module|source_type|source_id, the stable identity a client keys on and
	// the value a cursor carries.
	RowKey string `json:"row_key"`

	ParkID   string `json:"park_id"`
	ParkName string `json:"park_name,omitempty"`
	Pen      Pen    `json:"pen"`

	// BusinessDate is YYYY-MM-DD in Asia/Kolkata. Each adapter maps its own spelling.
	BusinessDate string     `json:"business_date"`
	DueAt        *time.Time `json:"due_at,omitempty"`
	// ClockLabel is farm wording for the clock: "staged by 15:00", "13:00 session".
	ClockLabel string `json:"clock_label,omitempty"`

	WorkState  WorkState  `json:"work_state"`
	Lane       Lane       `json:"lane"`
	Severity   Severity   `json:"severity"`
	Owner      Owner      `json:"owner"`
	OwnerState OwnerState `json:"owner_state"`

	// Title and Subtitle are backend-owned copy in farm words. Pen, never shed.
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Counts   Counts `json:"counts"`

	// Href is where the module's own screen opens this row, on the surface that asked.
	Href string `json:"href,omitempty"`
}

// Finalize fills the derived fields a source may leave blank. Sources call it on every row
// they emit so RowKey and Lane are never composed twice.
func (r Row) Finalize() Row {
	r.RowKey = RowKey(r.Module, r.SourceType, r.SourceID)
	r.Lane = LaneFor(r.WorkState)
	if r.Severity == "" {
		r.Severity = SeverityOK
	}
	if r.OwnerState == "" {
		if r.Owner.UserID != "" || r.Owner.WorkforceMemberID != "" {
			r.OwnerState = OwnerStateAssigned
		} else {
			r.OwnerState = OwnerStateMissing
		}
	}
	return r
}

// RowKey composes the stable identity.
func RowKey(module Module, sourceType, sourceID string) string {
	return string(module) + "|" + sourceType + "|" + sourceID
}

// Cursor is where a page stopped: the last row's identity. Encoded as the row key.
type Cursor struct {
	Module     Module
	SourceType string
	SourceID   string
}

// ErrInvalidCursor is returned for a cursor the board cannot read.
var ErrInvalidCursor = errors.New("workboard: invalid cursor")

// ParseCursor decodes a cursor string; empty means the first page.
func ParseCursor(raw string) (Cursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Cursor{}, nil
	}
	parts := strings.SplitN(raw, "|", 3)
	if len(parts) != 3 || !IsModule(parts[0]) || parts[1] == "" || parts[2] == "" {
		return Cursor{}, ErrInvalidCursor
	}
	return Cursor{Module: Module(parts[0]), SourceType: parts[1], SourceID: parts[2]}, nil
}

// String encodes the cursor.
func (c Cursor) String() string {
	if c.Module == "" {
		return ""
	}
	return RowKey(c.Module, c.SourceType, c.SourceID)
}

// IsZero reports the first-page cursor.
func (c Cursor) IsZero() bool { return c.Module == "" }

// Query bounds one read. TenantID, ParkID and BusinessDate are required: they are what
// keeps every source on its own index.
type Query struct {
	TenantID     string
	ParkID       string
	BusinessDate string
	// Modules narrows the read; empty means every module the caller may see.
	Modules []Module
	// WorkStates narrows the read; empty means every state.
	WorkStates []WorkState
	// OwnerUserID narrows to one person's rows (the operator lens, or an assignee filter).
	OwnerUserID string
	Limit       int
	Cursor      Cursor
}

const (
	DefaultLimit = 25
	MaxLimit     = 100
)

// ErrInvalidQuery is returned when a required bound is missing.
var ErrInvalidQuery = errors.New("workboard: invalid query")

// Normalize validates and fills defaults.
func (q Query) Normalize() (Query, error) {
	q.TenantID = strings.TrimSpace(q.TenantID)
	q.ParkID = strings.TrimSpace(q.ParkID)
	q.BusinessDate = strings.TrimSpace(q.BusinessDate)
	if q.TenantID == "" || q.ParkID == "" {
		return q, ErrInvalidQuery
	}
	if _, err := time.Parse("2006-01-02", q.BusinessDate); err != nil {
		return q, ErrInvalidQuery
	}
	if q.Limit <= 0 {
		q.Limit = DefaultLimit
	}
	if q.Limit > MaxLimit {
		q.Limit = MaxLimit
	}
	q.Modules = dedupeModules(q.Modules)
	return q, nil
}

// WantsModule reports whether the query includes m.
func (q Query) WantsModule(m Module) bool {
	if len(q.Modules) == 0 {
		return true
	}
	for _, x := range q.Modules {
		if x == m {
			return true
		}
	}
	return false
}

// WantsState reports whether the query includes s.
func (q Query) WantsState(s WorkState) bool {
	if len(q.WorkStates) == 0 {
		return true
	}
	for _, x := range q.WorkStates {
		if x == s {
			return true
		}
	}
	return false
}

func dedupeModules(in []Module) []Module {
	if len(in) == 0 {
		return nil
	}
	seen := map[Module]struct{}{}
	out := make([]Module, 0, len(in))
	for _, m := range in {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return moduleIndex(out[i]) < moduleIndex(out[j]) })
	return out
}

func moduleIndex(m Module) int {
	for i, x := range Modules() {
		if x == m {
			return i
		}
	}
	return len(Modules())
}

// Page is one page of rows plus the cursor for the next.
type Page struct {
	Rows       []Row  `json:"rows"`
	NextCursor string `json:"next_cursor,omitempty"`
}

// Summary is the WHOLE-FILTER aggregate the lane headers and the KPI tiles render. It is
// never a page-local sum.
type Summary struct {
	Total     int               `json:"total"`
	ByLane    map[Lane]int      `json:"by_lane"`
	ByState   map[WorkState]int `json:"by_state"`
	ByModule  map[Module]int    `json:"by_module"`
	Attention int               `json:"needs_attention"`
	Modules   []Module          `json:"modules"`
	Lanes     []Lane            `json:"lanes"`
}

// NewSummary returns an empty summary with every lane and module present, so a client
// renders a zero rather than an absent key.
func NewSummary(modules []Module) Summary {
	s := Summary{ByLane: map[Lane]int{}, ByState: map[WorkState]int{}, ByModule: map[Module]int{}, Modules: modules, Lanes: Lanes()}
	for _, l := range Lanes() {
		s.ByLane[l] = 0
	}
	for _, m := range modules {
		s.ByModule[m] = 0
	}
	return s
}

// Add folds one source's per-state counts into the summary.
func (s *Summary) Add(module Module, byState map[WorkState]int) {
	for state, n := range byState {
		if n == 0 {
			continue
		}
		s.Total += n
		s.ByState[state] += n
		s.ByLane[LaneFor(state)] += n
		s.ByModule[module] += n
		if state == WorkStateOverdue || state == WorkStateMissed || state == WorkStateRejected || state == WorkStateBlocked {
			s.Attention += n
		}
	}
}
