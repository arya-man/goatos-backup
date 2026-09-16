package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Work states -- the KERNEL dimension, PC Care's 000183 shape. 'completed' is reached on submit
// for a review 'none' routine and on the verifier's approval for a 'verifier' routine.
const (
	WorkStateScheduled = "scheduled"
	WorkStateDelayed   = "delayed"
	WorkStateCompleted = "completed"
	WorkStateCanceled  = "canceled"
)

// Statuses -- the GATE dimension. 'open' is the pre-submit working state; a submit under
// review locks the row 'pending_verification'; the verdict decides 'completed' or 'rework'.
const (
	StatusOpen                = "open"
	StatusPendingVerification = "pending_verification"
	StatusCompleted           = "completed"
	StatusRework              = "rework"
)

// Presence event types.
const (
	PresenceEnter = "enter"
	PresenceLeave = "leave"
)

// Proof kinds a submit carries.
const (
	ProofKindPhoto = "photo"
	ProofKindVideo = "video"
)

// ProofItem is one capture on a task: pen_routine_tasks.proof_refs.
type ProofItem struct {
	Ref  string `json:"ref"`
	Kind string `json:"kind"`
}

// PresenceLocation is what the phone captured when the punch was made. Every field is
// optional and recorded as given: V1 refuses nothing by distance.
type PresenceLocation struct {
	Latitude  *float64 `json:"latitude,omitempty"`
	Longitude *float64 `json:"longitude,omitempty"`
	AccuracyM *float64 `json:"accuracy_m,omitempty"`
	Status    string   `json:"status,omitempty"` // captured | permission_missing | unavailable
	Address   string   `json:"address,omitempty"`
}

// PresenceIntegrity is the honest-capture block the workforce clock also records.
type PresenceIntegrity struct {
	MockLocation *bool  `json:"mock_location,omitempty"`
	DeviceID     string `json:"device_id,omitempty"`
	AppVersion   string `json:"app_version,omitempty"`
	DeviceModel  string `json:"device_model,omitempty"`
	Offline      *bool  `json:"offline,omitempty"`
}

// PresenceEvent is one punch on a task.
type PresenceEvent struct {
	PresenceID string
	TaskID     string
	UserID     string
	EventType  string
	CapturedAt time.Time
	RecordedAt time.Time
	Location   PresenceLocation
	Integrity  PresenceIntegrity
}

// Task is one occurrence of a routine in one pen, plus the resolved labels every read carries
// and the pinned version's renderable fields.
type Task struct {
	TaskID         string
	TenantID       string
	RoutineID      string
	RoutineVersion int
	RoutineName    string
	Instruction    string
	Evidence       Evidence
	ReviewKind     string
	CadenceLine    string
	ParkID         string
	ParkName       string
	ShedID         string
	ShedName       string
	Partition      string
	// PenLabel is the oploc display of (shed, partition): "Castro 2", "Godel 1 - Part 3".
	PenLabel     string
	TriggerKinds []string
	SourceDate   string // YYYY-MM-DD
	PlannedDate  string // YYYY-MM-DD, immutable
	DueDate      string // YYYY-MM-DD, rolls forward only
	WorkState    string
	Status       string
	// AssigneeIDs are the routine's people; any one of them may work the task. AssigneeNames
	// are their display names in the same order, resolved by the read (a name the register
	// cannot resolve is "").
	AssigneeIDs   []string
	AssigneeNames []string
	Answers       map[string]any
	Proofs        []ProofItem
	EnteredAt     *time.Time
	EnteredBy     string
	LeftAt        *time.Time
	SubmittedBy   string
	SubmittedAt   *time.Time
	VerifiedBy    string
	VerifiedAt    *time.Time
	ReworkReason  string
	RolledFwd     int
	DelayedSince  *string
	RowVersion    int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Actor is who is looking at, or acting on, a task.
type Actor struct {
	UserID string
}

// IsAssignee reports whether the actor is one of the routine's people.
func (t Task) IsAssignee(a Actor) bool {
	if a.UserID == "" {
		return false
	}
	for _, id := range t.AssigneeIDs {
		if id == a.UserID {
			return true
		}
	}
	return false
}

// IsOpen reports whether the task is still owed on the kernel clock.
func (t Task) IsOpen() bool {
	return t.WorkState == WorkStateScheduled || t.WorkState == WorkStateDelayed
}

// AwaitsWork reports whether someone still has to go and do it: owed, and not with the verifier.
func (t Task) AwaitsWork() bool {
	return t.IsOpen() && (t.Status == StatusOpen || t.Status == StatusRework)
}

// IsVerified reports whether the gate closed (verifier approved, or review none submitted).
func (t Task) IsVerified() bool { return t.Status == StatusCompleted }

// CanSubmit: an assignee, while the task still awaits work.
func (t Task) CanSubmit(a Actor) bool { return t.IsAssignee(a) && t.AwaitsWork() }

// InPen reports whether the actor has checked in to this pen on this task and not left.
func (t Task) InPen(a Actor) bool {
	return t.EnteredAt != nil && t.LeftAt == nil && t.EnteredBy == a.UserID
}

// CanCheckIn: an assignee, task awaiting work, presence asked for, not already in the pen.
func (t Task) CanCheckIn(a Actor) bool {
	return t.CanSubmit(a) && t.Evidence.PresenceRequired() && !t.InPen(a)
}

// ProofCounts tallies the captures by kind.
func (t Task) ProofCounts() (photos, videos int) {
	for _, p := range t.Proofs {
		switch p.Kind {
		case ProofKindPhoto:
			photos++
		case ProofKindVideo:
			videos++
		}
	}
	return photos, videos
}

// checkOpenFor is the shared precondition of every write on a task.
func checkOpenFor(t Task, a Actor, rowVersion int) error {
	if !t.IsAssignee(a) {
		return ErrNotAssignee
	}
	switch t.WorkState {
	case WorkStateCompleted:
		return ErrAlreadyDone
	case WorkStateCanceled:
		return ErrCanceled
	}
	switch t.Status {
	case StatusPendingVerification:
		return ErrInReview
	case StatusCompleted:
		return ErrAlreadyDone
	}
	if rowVersion != 0 && rowVersion != t.RowVersion {
		return ErrVersionConflict
	}
	return nil
}

// CheckPresence is the rule a punch re-runs under the row lock: an enter while not in the pen,
// a leave while in it.
func CheckPresence(t Task, a Actor, eventType string, rowVersion int) error {
	if err := checkOpenFor(t, a, rowVersion); err != nil {
		return err
	}
	switch eventType {
	case PresenceEnter:
		if t.EnteredAt != nil && t.LeftAt == nil {
			return ErrPresenceState
		}
	case PresenceLeave:
		if !t.InPen(a) {
			return ErrPresenceState
		}
	default:
		return ErrPresenceState
	}
	return nil
}

// CheckProofs validates the captures against the pinned rule: counts inside min/max and every
// item a known kind with a ref.
func CheckProofs(e Evidence, proofs []ProofItem) error {
	photos, videos := 0, 0
	for _, p := range proofs {
		if strings.TrimSpace(p.Ref) == "" {
			return ErrInvalidProof
		}
		switch p.Kind {
		case ProofKindPhoto:
			photos++
		case ProofKindVideo:
			videos++
		default:
			return ErrInvalidProof
		}
	}
	if photos < e.Photo.Min || photos > e.Photo.Max || videos < e.Video.Min || videos > e.Video.Max {
		return ErrProofCount
	}
	return nil
}

// CheckSubmit is the rule the write re-runs under the row lock. Answers are validated
// separately by CheckAnswers (the caller passes the result in through the repository).
func CheckSubmit(t Task, a Actor, proofs []ProofItem, rowVersion int) error {
	if err := checkOpenFor(t, a, rowVersion); err != nil {
		return err
	}
	if err := CheckProofs(t.Evidence, proofs); err != nil {
		return err
	}
	if t.Evidence.PresenceRequired() && !(t.EnteredAt != nil && t.EnteredBy == a.UserID) {
		return ErrPresenceMissing
	}
	return nil
}

// SubmitOutcome is what a submit does to the two dimensions, from the routine's review kind.
func SubmitOutcome(reviewKind string) (workState, status string) {
	if reviewKind == ReviewNone {
		return WorkStateCompleted, StatusCompleted
	}
	return "", StatusPendingVerification // work state untouched
}

// Title is the card's first line: the routine, at the pen, at the park.
func Title(t Task) string {
	name := strings.TrimSpace(t.RoutineName)
	if name == "" {
		name = "Routine"
	}
	pen := strings.TrimSpace(t.PenLabel)
	if pen == "" {
		pen = "Pen"
	}
	if strings.TrimSpace(t.ParkName) == "" {
		return name + " · " + pen
	}
	return fmt.Sprintf("%s · %s · %s", name, pen, strings.TrimSpace(t.ParkName))
}

// ReasonLine is the card's second line: why today -- the cadence, or the work that raised it.
func ReasonLine(t Task, today string) string {
	kinds := SortWorkKinds(t.TriggerKinds)
	if len(kinds) == 0 {
		if strings.TrimSpace(t.CadenceLine) != "" {
			return t.CadenceLine
		}
		return "Routine check"
	}
	labels := make([]string, 0, len(kinds))
	for _, k := range kinds {
		labels = append(labels, strings.ToLower(WorkKindLabel(k)))
	}
	when := "on " + biztime.FarmDateFromBusinessDate(t.SourceDate)
	if t.SourceDate == today {
		when = "today"
	} else if yesterday, ok := dayBefore(today); ok && yesterday == t.SourceDate {
		when = "yesterday"
	}
	return "After " + strings.Join(labels, ", ") + " " + when
}

// StateChip is the card chip: where the task stands, in farm words. The gate speaks first and
// the kernel clock only while work is owed.
func StateChip(t Task, today string) string {
	switch t.Status {
	case StatusPendingVerification:
		return "In review"
	case StatusRework:
		return "Sent back"
	}
	switch t.WorkState {
	case WorkStateCompleted:
		if t.ReviewKind == ReviewVerifier {
			return "Verified"
		}
		return "Done"
	case WorkStateCanceled:
		return "Cancelled"
	case WorkStateDelayed:
		since := t.PlannedDate
		if t.DelayedSince != nil && *t.DelayedSince != "" {
			since = *t.DelayedSince
		}
		return "Delayed since " + biztime.FarmDateFromBusinessDate(since)
	}
	if t.DueDate == today {
		return "Due today"
	}
	return "Due " + biztime.FarmDateFromBusinessDate(t.DueDate)
}

// StateTone is the chip's colour token: the client maps it, never composes its own.
func StateTone(t Task) string {
	switch t.Status {
	case StatusPendingVerification:
		return "review"
	case StatusRework:
		return "danger"
	}
	switch t.WorkState {
	case WorkStateCompleted:
		return "success"
	case WorkStateCanceled:
		return "muted"
	case WorkStateDelayed:
		return "danger"
	}
	return "info"
}

// Instruction is the detail screen's sentence of what to do, from the routine's own words.
func Instruction(t Task, a Actor) string {
	switch t.Status {
	case StatusRework:
		reason := strings.TrimSpace(t.ReworkReason)
		if reason != "" {
			return "The verifier sent this back: " + reason + ". Do it again and submit."
		}
		return "The verifier sent this back. Do it again and submit."
	case StatusPendingVerification:
		return "Submitted. The verifier is reviewing it."
	case StatusCompleted:
		if t.ReviewKind == ReviewVerifier {
			return "The verifier approved this."
		}
		return "Done."
	}
	own := strings.TrimSpace(t.Instruction)
	if t.Evidence.PresenceRequired() && !t.InPen(a) {
		if own == "" {
			return "Check in when you reach the pen, then answer and submit."
		}
		return own + " Check in when you reach the pen."
	}
	if own == "" {
		return "Answer, capture what is asked for, and submit."
	}
	return own
}

// PresenceLine is the detail line for the check-in: "In pen since 9:12 AM", "Checked in 9:12 AM
// · left 9:40 AM", or the ask.
func PresenceLine(t Task) string {
	if !t.Evidence.PresenceRequired() {
		return ""
	}
	if t.EnteredAt == nil {
		return "Check in to the pen to start"
	}
	in := t.EnteredAt.In(biztime.DefaultLocation())
	if t.LeftAt == nil {
		return "In pen since " + clock(in)
	}
	out := t.LeftAt.In(biztime.DefaultLocation())
	return "Checked in " + clock(in) + " · left " + clock(out)
}

// DoneLine is the detail line once submitted.
func DoneLine(t Task) string {
	if t.SubmittedAt == nil {
		return ""
	}
	at := t.SubmittedAt.In(biztime.DefaultLocation())
	line := "Submitted " + biztime.FarmDate(at) + " · " + clock(at)
	if t.VerifiedAt != nil && t.Status == StatusCompleted && t.ReviewKind == ReviewVerifier {
		line += " · verified " + biztime.FarmDate(t.VerifiedAt.In(biztime.DefaultLocation()))
	}
	return line
}

func clock(at time.Time) string { return strings.ToLower(at.Format("3:04 PM")) }

// Filters. The client names a KEY; the backend owns which states that means.
const (
	FilterToDo = "todo"
	FilterDone = "done"
)

// FilterKeys is the chip order.
var FilterKeys = []string{FilterToDo, FilterDone}

// FilterKeyOrDefault normalizes a requested key, falling back to To do.
func FilterKeyOrDefault(key string) string {
	if strings.TrimSpace(key) == FilterDone {
		return FilterDone
	}
	return FilterToDo
}

// StatesForFilter resolves a chip key to the work states it lists. Cancelled tasks are never
// listed. A submitted task in review is still under To do (its kernel clock is open).
func StatesForFilter(key string) []string {
	if key == FilterDone {
		return []string{WorkStateCompleted}
	}
	return []string{WorkStateScheduled, WorkStateDelayed}
}

// FilterLabel is the chip text.
func FilterLabel(key string) string {
	if key == FilterDone {
		return "Done"
	}
	return "To do"
}

// FilterEmptyMessage is the line shown when a slice has nothing in it.
func FilterEmptyMessage(key string) string {
	if key == FilterDone {
		return "Nothing submitted yet."
	}
	return "No routine checks due. A check appears here on the days its routine names."
}

// FilterCount resolves a chip's count from whole-list state counts.
func FilterCount(key string, stateCounts map[string]int) int {
	total := 0
	for _, s := range StatesForFilter(key) {
		total += stateCounts[s]
	}
	return total
}

func dayBefore(businessDate string) (string, bool) {
	d, err := time.Parse("2006-01-02", businessDate)
	if err != nil {
		return "", false
	}
	return d.AddDate(0, 0, -1).Format("2006-01-02"), true
}

// AnswerRow is one rendered answer for a reader (the verifier's context rows, the web table).
type AnswerRow struct {
	QuestionID string `json:"question_id"`
	Title      string `json:"title"`
	Value      string `json:"value"`
}

// AnswerRows renders the stored answers in question order, skipping the unanswered.
func AnswerRows(t Task) []AnswerRow {
	out := make([]AnswerRow, 0, len(t.Evidence.Questions))
	for _, q := range t.Evidence.Questions {
		v, ok := t.Answers[q.ID]
		if !ok {
			continue
		}
		s := RenderAnswer(q, v)
		if s == "" {
			continue
		}
		out = append(out, AnswerRow{QuestionID: q.ID, Title: q.Title, Value: s})
	}
	return out
}

// Step is the task as every surface renders it -- the phone card and detail, the web Today
// table and the Work Board subtask. ONE wire shape, composed here, so no two screens disagree
// about where a pen's check stands. Every visible string is backend copy rendered verbatim.
type Step struct {
	TaskID         string   `json:"task_id"`
	RoutineID      string   `json:"routine_id"`
	RoutineVersion int      `json:"routine_version"`
	RoutineName    string   `json:"routine_name"`
	Title          string   `json:"title"`
	ParkID         string   `json:"park_id"`
	ParkName       string   `json:"park_name"`
	ShedID         string   `json:"shed_id"`
	ShedName       string   `json:"shed_name"`
	Partition      string   `json:"partition_label"`
	PenLabel       string   `json:"operational_location_display"`
	TriggerKinds   []string `json:"trigger_kinds"`
	ReasonLine     string   `json:"reason_line"`
	SourceDate     string   `json:"source_business_date"`
	PlannedDate    string   `json:"planned_business_date"`
	DueDate        string   `json:"due_business_date"`
	WorkState      string   `json:"work_state"`
	Status         string   `json:"status"`
	StateChip      string   `json:"state_chip"`
	StateTone      string   `json:"state_tone"`
	Instruction    string   `json:"instruction"`
	EvidenceLine   string   `json:"evidence_line"`
	ReviewKind     string   `json:"review_kind"`
	// Form is the pinned version's evidence: the questions and capture rules the phone renders.
	Form Evidence `json:"form"`
	// Answers are the stored answers keyed by question id (null when none); AnswerRows their
	// rendering in question order.
	Answers    map[string]any `json:"answers"`
	AnswerRows []AnswerRow    `json:"answer_rows"`
	Proofs     []ProofItem    `json:"proofs"`
	// Presence: the check-in state for THIS caller.
	PresenceRequired bool    `json:"presence_required"`
	PresenceLine     string  `json:"presence_line"`
	InPen            bool    `json:"in_pen"`
	CanCheckIn       bool    `json:"can_check_in"`
	EnteredAt        *string `json:"entered_at"`
	LeftAt           *string `json:"left_at"`
	// CanSubmit is THIS caller's answer: an assignee while work is owed.
	CanSubmit    bool    `json:"can_submit"`
	Verified     bool    `json:"verified"`
	DoneLine     string  `json:"done_line"`
	ReworkReason string  `json:"rework_reason,omitempty"`
	SubmittedAt  *string `json:"submitted_at"`
	VerifiedAt   *string `json:"verified_at"`
	RowVersion   int     `json:"row_version"`
}

// StepFor composes the wire shape for one caller on one business day.
func StepFor(t Task, actor Actor, today string) Step {
	form := t.Evidence
	if form.Questions == nil {
		form.Questions = []Question{}
	}
	if form.Presence == "" {
		form.Presence = PresenceOff
	}
	proofs := t.Proofs
	if proofs == nil {
		proofs = []ProofItem{}
	}
	kinds := SortWorkKinds(t.TriggerKinds)
	if kinds == nil {
		kinds = []string{}
	}
	return Step{
		TaskID:           t.TaskID,
		RoutineID:        t.RoutineID,
		RoutineVersion:   t.RoutineVersion,
		RoutineName:      t.RoutineName,
		Title:            Title(t),
		ParkID:           t.ParkID,
		ParkName:         t.ParkName,
		ShedID:           t.ShedID,
		ShedName:         t.ShedName,
		Partition:        t.Partition,
		PenLabel:         t.PenLabel,
		TriggerKinds:     kinds,
		ReasonLine:       ReasonLine(t, today),
		SourceDate:       t.SourceDate,
		PlannedDate:      t.PlannedDate,
		DueDate:          t.DueDate,
		WorkState:        t.WorkState,
		Status:           t.Status,
		StateChip:        StateChip(t, today),
		StateTone:        StateTone(t),
		Instruction:      Instruction(t, actor),
		EvidenceLine:     EvidenceLine(t.Evidence),
		ReviewKind:       t.ReviewKind,
		Form:             form,
		Answers:          t.Answers,
		AnswerRows:       AnswerRows(t),
		Proofs:           proofs,
		PresenceRequired: t.Evidence.PresenceRequired(),
		PresenceLine:     PresenceLine(t),
		InPen:            t.InPen(actor),
		CanCheckIn:       t.CanCheckIn(actor),
		EnteredAt:        wireInstant(t.EnteredAt),
		LeftAt:           wireInstant(t.LeftAt),
		CanSubmit:        t.CanSubmit(actor),
		Verified:         t.IsVerified(),
		DoneLine:         DoneLine(t),
		ReworkReason:     t.ReworkReason,
		SubmittedAt:      wireInstant(t.SubmittedAt),
		VerifiedAt:       wireInstant(t.VerifiedAt),
		RowVersion:       t.RowVersion,
	}
}

func wireInstant(at *time.Time) *string {
	if at == nil {
		return nil
	}
	s := at.UTC().Format("2006-01-02T15:04:05Z07:00")
	return &s
}

// PenKey is the map key a batched pen lookup answers under: shed id and NORMALIZED partition,
// the same key partition_key carries, so "Part 3", "part 3" and " Part 3 " meet.
func PenKey(shedID, partition string) string {
	return shedID + "|" + NormalizePartitionKey(partition)
}

// NormalizePartitionKey mirrors the generated partition_key column.
func NormalizePartitionKey(partition string) string {
	p := strings.ToLower(strings.TrimSpace(partition))
	if p == "" || p == "whole" {
		return "whole"
	}
	return p
}
