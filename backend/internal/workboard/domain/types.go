// Package domain is the Work Board's vocabulary: the one row shape every module
// normalises to, the lanes those rows sit in, and the query that bounds a read.
package domain

import (
	"errors"
	"sort"
	"strconv"
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
	// ModuleTasks is the Tasks module's half of the board (maintainer decision 2026-09-14): the
	// work the system owes a person -- today the next-day pen visits -- rows here, never under
	// the module whose work raised it. Appended last: the order is the cursor contract.
	ModuleTasks Module = "tasks"
)

// Modules returns every module in board order. New modules append; the order is part of
// the cursor contract, so it never changes for an existing module.
func Modules() []Module {
	return []Module{
		ModuleFeed, ModuleHealth, ModuleVaccination, ModuleWeighing, ModuleCounts,
		ModuleMilk, ModulePCCare, ModuleToxin, ModuleProcurement, ModuleVerification,
		ModuleTasks,
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

// StatesInLane is the inverse of LaneFor: every work state a lane holds, so a column can be
// read on its own (`lane=done` is the same read as `state=completed`, spelled by the lane).
func StatesInLane(lane Lane) []WorkState {
	out := []WorkState{}
	for _, s := range WorkStates() {
		if LaneFor(s) == lane {
			out = append(out, s)
		}
	}
	return out
}

// WorkStateNone is the state no row holds: a filter that must match nothing (a state asked
// for outside the lane asked for) binds it, so "no states" never reads as "every state".
const WorkStateNone WorkState = "none"

// IsLane reports whether raw names a lane.
func IsLane(raw string) bool {
	for _, l := range Lanes() {
		if string(l) == raw {
			return true
		}
	}
	return false
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
	// InReview and NotStarted split Pending for a source that knows them (the feed cards,
	// maintainer instruction 2026-09-25: "I need to see how everything is going"): units handed
	// in and waiting for a verdict, and units nobody has started. The rest of Pending, less the
	// units needing attention, is work started and not handed in. A source that does not know
	// them leaves both zero and the card reads as before.
	InReview   int `json:"in_review,omitempty"`
	NotStarted int `json:"not_started,omitempty"`
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
	// A REJECTED row is live work someone was told to redo: it is never severity ok, it is
	// always at least one attention, and it always carries a pending unit. Stated ONCE here
	// so no source can answer differently -- three did (live E2E 2026-09-11: counts said
	// watch/1 pending, feed transport said ok/1 pending, verification said ok/0 pending).
	if r.WorkState == WorkStateRejected {
		if r.Severity == SeverityOK {
			r.Severity = SeverityWatch
		}
		if r.Counts.NeedsAttention == 0 {
			r.Counts.NeedsAttention = 1
		}
		if r.Counts.Done+r.Counts.Pending == 0 {
			r.Counts.Pending = 1
		}
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

// ErrInvalidRowKey is a row key that is not module|source_type|source_id.
var ErrInvalidRowKey = errors.New("workboard: invalid row key")

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
	// NoModules means the caller may see nothing at all: an empty board, never an error,
	// and never a sentinel module on the wire. Distinct from an empty Modules, which means
	// every module.
	NoModules bool
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
	if q.NoModules {
		return false
	}
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
	// Degraded names the modules whose read failed on THIS request. The board never blanks for
	// one slow source: it serves every module that answered and lists here the ones that did
	// not, so a client shows the rest and a retry note for these (never an empty board).
	Degraded []Module `json:"degraded,omitempty"`
}

// Summary is the WHOLE-FILTER aggregate the lane headers and the KPI tiles render. It is
// never a page-local sum.
type Summary struct {
	Total        int                     `json:"total"`
	ByLane       map[Lane]int            `json:"by_lane"`
	ByState      map[WorkState]int       `json:"by_state"`
	ByModule     map[Module]int          `json:"by_module"`
	ByModuleLane map[Module]map[Lane]int `json:"by_module_lane"`
	// ByModuleState is every module's per-state count, so a client filtered to a module (and a
	// lane) can say how many of THOSE cards are done, pending and need attention without a second
	// read (maintainer review 2026-09-25: the phone's tiles ignored its own chips).
	ByModuleState map[Module]map[WorkState]int `json:"by_module_state"`
	Attention     int                          `json:"needs_attention"`
	Modules       []Module                     `json:"modules"`
	Lanes         []Lane                       `json:"lanes"`
	// Degraded names the modules whose aggregate read failed on THIS request; their counts are
	// absent from the totals above rather than blanking the whole summary.
	Degraded []Module `json:"degraded,omitempty"`
}

// NewSummary returns an empty summary with every lane and module present, so a client
// renders a zero rather than an absent key.
func NewSummary(modules []Module) Summary {
	s := Summary{
		ByLane:        map[Lane]int{},
		ByState:       map[WorkState]int{},
		ByModule:      map[Module]int{},
		ByModuleLane:  map[Module]map[Lane]int{},
		ByModuleState: map[Module]map[WorkState]int{},
		Modules:       modules,
		Lanes:         Lanes(),
	}
	for _, l := range Lanes() {
		s.ByLane[l] = 0
	}
	for _, m := range modules {
		s.ByModule[m] = 0
		s.ByModuleLane[m] = map[Lane]int{}
		for _, l := range Lanes() {
			s.ByModuleLane[m][l] = 0
		}
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
		lane := LaneFor(state)
		s.ByLane[lane] += n
		s.ByModule[module] += n
		if s.ByModuleLane[module] == nil {
			s.ByModuleLane[module] = map[Lane]int{}
		}
		s.ByModuleLane[module][lane] += n
		if s.ByModuleState == nil {
			s.ByModuleState = map[Module]map[WorkState]int{}
		}
		if s.ByModuleState[module] == nil {
			s.ByModuleState[module] = map[WorkState]int{}
		}
		s.ByModuleState[module][state] += n
		if state == WorkStateOverdue || state == WorkStateMissed || state == WorkStateRejected || state == WorkStateBlocked {
			s.Attention += n
		}
	}
}

// ---- subtasks ------------------------------------------------------------------------

// StepState is the state of ONE step in a subtask's chain. Seven values, closed: a client
// renders each with backend-owned copy and never derives one.
type StepState string

const (
	StepTodo           StepState = "todo"
	StepInProgress     StepState = "in_progress"
	StepInReview       StepState = "in_review"
	StepDone           StepState = "done"
	StepRework         StepState = "rework"
	StepNeedsAttention StepState = "needs_attention"
	// StepLocked: the step cannot start yet because an earlier one has not landed (a verify
	// before a submit), or a hold keeps it closed (a deferred dose, a guarded action).
	StepLocked StepState = "locked"
)

// StepStates returns the closed vocabulary in a stable order.
func StepStates() []StepState {
	return []StepState{StepTodo, StepInProgress, StepInReview, StepDone, StepRework, StepNeedsAttention, StepLocked}
}

// IsStepState reports whether s is one of the seven.
func IsStepState(s string) bool {
	for _, x := range StepStates() {
		if string(x) == s {
			return true
		}
	}
	return false
}

// Step is one link of a subtask's chain: "Scan", "Submit", "Verify", "Close".
type Step struct {
	Name  string    `json:"name"`
	State StepState `json:"state"`
	// Detail is optional farm wording beside the state: "12.5 kg", "Sent back: blurry".
	Detail string `json:"detail,omitempty"`
}

// Subtask is one unit of a row's work: an animal, a bag, a session, a step, a proof. Its
// Key is the keyset value; it is OPAQUE to a client and sorts worst-first (see SubtaskKey).
type Subtask struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Subtitle string `json:"subtitle,omitempty"`
	// WorkState reuses the board vocabulary so a subtask lands in a lane like a row does.
	WorkState      WorkState `json:"work_state"`
	Lane           Lane      `json:"lane"`
	Owner          Owner     `json:"owner"`
	NeedsAttention bool      `json:"needs_attention"`
	Steps          []Step    `json:"steps"`
	Href           string    `json:"href,omitempty"`
}

// Finalize fills the derived lane. Sources call it on every subtask they emit.
func (s Subtask) Finalize() Subtask {
	s.Lane = LaneFor(s.WorkState)
	if s.Steps == nil {
		s.Steps = []Step{}
	}
	return s
}

// SubtaskPage is one page of a row's subtasks. Total is the WHOLE count for the row,
// never the page length.
type SubtaskPage struct {
	Subtasks   []Subtask `json:"subtasks"`
	NextCursor string    `json:"next_cursor,omitempty"`
	Total      int       `json:"total"`
}

// Subtask ranks. The issue view lists subtasks WORST FIRST; a keyset cannot follow a sort it
// does not key on, so the rank is the leading segment of the subtask key and the sort is a
// plain ascending sort on the key. Lower is worse.
const (
	RankNeedsAttention = 0
	RankToDo           = 1
	RankInProgress     = 2
	RankInReview       = 3
	RankDone           = 4
)

// RankFor derives the rank from the subtask's lane and attention flag, the same way a
// source's SQL must derive it (a source states the SQL twin of this table once).
func RankFor(state WorkState, needsAttention bool) int {
	if needsAttention {
		return RankNeedsAttention
	}
	switch LaneFor(state) {
	case LaneInProgress:
		return RankInProgress
	case LaneInReview:
		return RankInReview
	case LaneDone:
		return RankDone
	default:
		return RankToDo
	}
}

// SubtaskKey composes the keyset value: "<rank>:<id>". The id is the source's own stable
// identity for the unit (an observation id, a step id, a proof ordinal).
func SubtaskKey(rank int, id string) string {
	return strconv.Itoa(rank) + ":" + id
}

// ErrInvalidSubtaskCursor is returned for a subtask cursor the board cannot read.
var ErrInvalidSubtaskCursor = errors.New("workboard: invalid subtask cursor")

// ParseSubtaskKey splits a key into its rank and id; empty means the first page.
func ParseSubtaskKey(raw string) (rank int, id string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, "", nil
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", ErrInvalidSubtaskCursor
	}
	rank, convErr := strconv.Atoi(parts[0])
	if convErr != nil || rank < RankNeedsAttention || rank > RankDone {
		return 0, "", ErrInvalidSubtaskCursor
	}
	return rank, parts[1], nil
}

const (
	// DefaultSubtaskLimit is the mock's page: ten subtasks.
	DefaultSubtaskLimit = 10
	MaxSubtaskLimit     = 50
)

// BoundSubtaskLimit clamps a requested page size into [DefaultSubtaskLimit, MaxSubtaskLimit];
// zero or less means the default.
func BoundSubtaskLimit(n int) int {
	if n <= 0 {
		return DefaultSubtaskLimit
	}
	if n < DefaultSubtaskLimit {
		return DefaultSubtaskLimit
	}
	if n > MaxSubtaskLimit {
		return MaxSubtaskLimit
	}
	return n
}
