// Package domain is the rulebook of the pen visit STEP (maintainer decision 2026-09-07, reshaped
// 2026-09-12).
//
// The day after any preventive-care work is SUBMITTED in a pen -- a vaccination shed proof or a
// PC Care task (deworming, anti protozoan, ticks removal, hoof trimming, hair trimming) -- one of
// the park's configured visitors goes to that pen, looks at the animals, records ONE live-camera
// video and submits it. The video then goes to the VERIFIER like every other clip in the chain
// (feed & water removal, the work itself), and the parent care task closes only once the visit is
// approved. The visit is NOT a task of its own: it is raised by the kernel as the last step of the
// work that happened in the pen, it is reached from that work's own card, and it may be recorded by
// ANY person the park's HRMS config names -- "if multiple people are there, if anyone does then
// enough".
//
// Every string a screen shows -- the reason line, the state chip, the due label -- is composed
// HERE and rendered verbatim by the phone. The pen's own name comes from oploc, never from this
// package.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
)

// Work states -- the KERNEL dimension, PC Care's 000183 shape. 'completed' is reached ONLY when
// the verifier approves the visit video (migration 000297); a submitted visit awaiting review
// keeps its scheduled/delayed clock.
const (
	WorkStateScheduled = "scheduled"
	WorkStateDelayed   = "delayed"
	WorkStateCompleted = "completed"
	WorkStateCanceled  = "canceled"
)

// Verification statuses -- the GATE dimension (PC Care's shape). 'open' is the pre-submit
// working state; a submit locks the row 'pending_verification'; the verdict decides
// 'completed' or 'rework' (record again).
const (
	StatusOpen                = "open"
	StatusPendingVerification = "pending_verification"
	StatusCompleted           = "completed"
	StatusRework              = "rework"
)

// Source kinds: which parent a visit closes. The materializer takes both from the verification
// item the parent's submit raised.
const (
	SourceKindPCCareTask            = "pc_care_task"
	SourceKindVaccinationSubmission = "sop_submission"
)

// Source is one parent of a visit.
type Source struct {
	Kind  string
	RefID string
}

// Reasons: WHY a pen is visited. The closed vocabulary the materializer writes and the card
// labels. 'vaccination' is the vaccination shed proof; the rest are PC Care's pen categories.
const (
	ReasonVaccination   = "vaccination"
	ReasonDeworming     = "deworming"
	ReasonAntiProtozoan = "anti_protozoan"
	ReasonTicksRemoval  = "ticks_removal"
	ReasonHoofTrimming  = "hoof_trimming"
	ReasonHairTrimming  = "hair_trimming"
)

// reasonOrder is the display order when a pen carries several reasons on one day.
var reasonOrder = []string{ReasonVaccination, ReasonDeworming, ReasonAntiProtozoan, ReasonTicksRemoval, ReasonHoofTrimming, ReasonHairTrimming}

// ReasonLabel is the farm word for a reason. Unknown keys render as themselves so a future
// vocabulary widening never blanks a card.
func ReasonLabel(reason string) string {
	switch reason {
	case ReasonVaccination:
		return "Vaccination"
	case ReasonDeworming:
		return "Deworming"
	case ReasonAntiProtozoan:
		return "Anti protozoan"
	case ReasonTicksRemoval:
		return "Ticks removal"
	case ReasonHoofTrimming:
		return "Hoof trimming"
	case ReasonHairTrimming:
		return "Hair trimming"
	}
	return strings.ReplaceAll(reason, "_", " ")
}

// IsKnownReason reports whether r is in the closed vocabulary.
func IsKnownReason(r string) bool {
	for _, known := range reasonOrder {
		if known == r {
			return true
		}
	}
	return false
}

// SortReasons orders reasons for display and drops duplicates and unknowns.
func SortReasons(reasons []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(reasons))
	for _, r := range reasons {
		r = strings.TrimSpace(r)
		if !IsKnownReason(r) || seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return reasonRank(out[i]) < reasonRank(out[j]) })
	return out
}

func reasonRank(r string) int {
	for i, known := range reasonOrder {
		if known == r {
			return i
		}
	}
	return len(reasonOrder)
}

// Sentinel errors. The transport maps each to a stable code and a farm-worded message.
var (
	ErrNotAssignee     = errors.New("pen visit: caller is not a configured visitor for this park")
	ErrAlreadyDone     = errors.New("pen visit: already submitted")
	ErrInReview        = errors.New("pen visit: the video is with the verifier")
	ErrCanceled        = errors.New("pen visit: task is canceled")
	ErrProofRequired   = errors.New("pen visit: a live video (proof_ref) is required")
	ErrInvalidProof    = errors.New("pen visit: the video could not be verified")
	ErrVersionConflict = errors.New("pen visit: the task changed since it was loaded")
)

// Task is one pen visit as stored, plus the two resolved labels every read carries.
type Task struct {
	TaskID    string
	TenantID  string
	ParkID    string
	ParkName  string
	ShedID    string
	ShedName  string
	Partition string
	// PenLabel is the oploc display of (shed, partition): "Castro 2", "Godel 1 - Part 3".
	PenLabel    string
	Reasons     []string
	SourceDate  string // YYYY-MM-DD, the IST day the work was submitted
	PlannedDate string // YYYY-MM-DD, immutable
	DueDate     string // YYYY-MM-DD, rolls forward only
	WorkState   string
	Status      string
	// VisitorIDs are the people the park's HRMS config names; any one of them may record.
	VisitorIDs   []string
	Sources      []Source
	ProofRef     *string
	SubmittedBy  *string
	SubmittedAt  *time.Time
	VerifiedBy   *string
	VerifiedAt   *time.Time
	ReworkReason string
	RolledFwd    int
	DelayedSince *string
	RowVersion   int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Actor is who is looking at, or acting on, a task.
type Actor struct {
	UserID string
}

// IsAssignee reports whether the actor is one of the park's configured visitors. The name is
// kept from the one-person era; the answer is now a membership test.
func (t Task) IsAssignee(a Actor) bool {
	if a.UserID == "" {
		return false
	}
	for _, id := range t.VisitorIDs {
		if id == a.UserID {
			return true
		}
	}
	return false
}

// IsOpen reports whether the visit is still owed on the kernel clock.
func (t Task) IsOpen() bool {
	return t.WorkState == WorkStateScheduled || t.WorkState == WorkStateDelayed
}

// AwaitsRecording reports whether someone still has to go and film: the visit is owed and is
// not sitting with the verifier.
func (t Task) AwaitsRecording() bool {
	return t.IsOpen() && (t.Status == StatusOpen || t.Status == StatusRework)
}

// IsVerified reports whether the verifier approved the visit; the parent closes on this.
func (t Task) IsVerified() bool { return t.Status == StatusCompleted }

// CanSubmit: a configured visitor, while the visit still awaits a recording.
func (t Task) CanSubmit(a Actor) bool { return t.IsAssignee(a) && t.AwaitsRecording() }

// CheckSubmit is the rule the write re-runs under the row lock.
func CheckSubmit(t Task, a Actor, proofRef string, rowVersion int) error {
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
	if strings.TrimSpace(proofRef) == "" {
		return ErrProofRequired
	}
	if rowVersion != 0 && rowVersion != t.RowVersion {
		return ErrVersionConflict
	}
	return nil
}

// ReasonLine is the card's second line: what happened in the pen and when, from the visit day's
// point of view -- "Vaccination yesterday", "Deworming, ticks removal on 5 Sep".
func ReasonLine(t Task, today string) string {
	labels := make([]string, 0, len(t.Reasons))
	for i, r := range SortReasons(t.Reasons) {
		l := ReasonLabel(r)
		if i > 0 {
			l = strings.ToLower(l)
		}
		labels = append(labels, l)
	}
	if len(labels) == 0 {
		labels = []string{"Preventive care"}
	}
	when := "on " + biztime.FarmDateFromBusinessDate(t.SourceDate)
	if yesterday, ok := dayBefore(today); ok && yesterday == t.SourceDate {
		when = "yesterday"
	}
	return strings.Join(labels, ", ") + " " + when
}

// StateChip is the card chip: where the visit stands, in farm words. The gate speaks first
// (a clip with the verifier, or sent back) and the kernel clock only while a recording is owed.
func StateChip(t Task, today string) string {
	switch t.Status {
	case StatusPendingVerification:
		return "Visit in review"
	case StatusRework:
		return "Visit needs another video"
	}
	switch t.WorkState {
	case WorkStateCompleted:
		return "Visit verified"
	case WorkStateCanceled:
		return "Cancelled"
	case WorkStateDelayed:
		since := t.PlannedDate
		if t.DelayedSince != nil && *t.DelayedSince != "" {
			since = *t.DelayedSince
		}
		return "Visit delayed since " + biztime.FarmDateFromBusinessDate(since)
	}
	if t.DueDate == today {
		return "Visit pen today"
	}
	return "Visit pen " + biztime.FarmDateFromBusinessDate(t.DueDate)
}

// StateTone is the chip's colour token: the phone maps it, never composes its own.
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

// Title is the card's first line: the pen, at the park.
func Title(t Task) string {
	pen := strings.TrimSpace(t.PenLabel)
	if pen == "" {
		pen = "Pen"
	}
	if strings.TrimSpace(t.ParkName) == "" {
		return "Visit " + pen
	}
	return fmt.Sprintf("Visit %s · %s", pen, strings.TrimSpace(t.ParkName))
}

// Instruction is the detail screen's one sentence of what to do.
func Instruction(t Task) string {
	switch t.Status {
	case StatusRework:
		reason := strings.TrimSpace(t.ReworkReason)
		if reason != "" {
			return "The verifier sent the visit video back: " + reason + ". Go to the pen again and record a new video."
		}
		return "The verifier sent the visit video back. Go to the pen again and record a new video."
	case StatusPendingVerification:
		return "The visit video is with the verifier. The pen's work closes once it is approved."
	case StatusCompleted:
		return "The verifier approved the visit. This pen's work is closed."
	}
	return "Go to the pen, look at the animals and record one video. It will submit automatically."
}

// DoneLine is the detail line once the visit is submitted.
func DoneLine(t Task) string {
	if t.SubmittedAt == nil {
		return ""
	}
	at := t.SubmittedAt.In(biztime.DefaultLocation())
	line := "Visited " + biztime.FarmDate(at) + " · " + strings.ToLower(at.Format("3:04 PM"))
	if t.VerifiedAt != nil && t.Status == StatusCompleted {
		line += " · verified " + biztime.FarmDate(t.VerifiedAt.In(biztime.DefaultLocation()))
	}
	return line
}

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
// listed: a cancelled visit is noise on a park head's day. A submitted visit awaiting review is
// still under To do (its kernel clock is open) and wears the "in review" chip.
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
		return "No pen visits submitted yet."
	}
	return "No pens to visit today. A pen appears here the day after vaccination or preventive care work in it."
}

// FilterCount resolves a chip's count from whole-list state counts.
func FilterCount(key string, stateCounts map[string]int) int {
	total := 0
	for _, s := range StatesForFilter(key) {
		total += stateCounts[s]
	}
	return total
}

// dayBefore returns the YYYY-MM-DD one day before the given business date.
func dayBefore(businessDate string) (string, bool) {
	d, err := time.Parse("2006-01-02", businessDate)
	if err != nil {
		return "", false
	}
	return d.AddDate(0, 0, -1).Format("2006-01-02"), true
}

// Step is the visit as every parent surface renders it -- the PC Care task card and detail,
// the vaccination shed card and drilldown, and the visit's own detail. ONE wire shape, composed
// here, so the four screens cannot disagree about where a pen's visit stands. Every visible
// string is backend copy rendered verbatim.
type Step struct {
	TaskID       string   `json:"task_id"`
	Title        string   `json:"title"`
	ParkID       string   `json:"park_id"`
	ParkName     string   `json:"park_name"`
	ShedID       string   `json:"shed_id"`
	ShedName     string   `json:"shed_name"`
	Partition    string   `json:"partition_label"`
	PenLabel     string   `json:"operational_location_display"`
	Reasons      []string `json:"reasons"`
	ReasonLabels []string `json:"reason_labels"`
	ReasonLine   string   `json:"reason_line"`
	SourceDate   string   `json:"source_business_date"`
	PlannedDate  string   `json:"planned_business_date"`
	DueDate      string   `json:"due_business_date"`
	WorkState    string   `json:"work_state"`
	Status       string   `json:"status"`
	StateChip    string   `json:"state_chip"`
	StateTone    string   `json:"state_tone"`
	Instruction  string   `json:"instruction"`
	DoneLine     string   `json:"done_line"`
	ReworkReason string   `json:"rework_reason,omitempty"`
	// CanSubmit is THIS caller's answer: a configured visitor while a recording is owed.
	CanSubmit bool `json:"can_submit"`
	// Verified is the parent's closure signal: the verifier approved the visit.
	Verified    bool    `json:"verified"`
	ProofRef    *string `json:"proof_ref"`
	SubmittedAt *string `json:"submitted_at"`
	VerifiedAt  *string `json:"verified_at"`
	RowVersion  int     `json:"row_version"`
}

// StepFor composes the wire shape for one caller on one business day.
func StepFor(t Task, actor Actor, today string) Step {
	reasons := SortReasons(t.Reasons)
	labels := make([]string, 0, len(reasons))
	for _, r := range reasons {
		labels = append(labels, ReasonLabel(r))
	}
	return Step{
		TaskID:       t.TaskID,
		Title:        Title(t),
		ParkID:       t.ParkID,
		ParkName:     t.ParkName,
		ShedID:       t.ShedID,
		ShedName:     t.ShedName,
		Partition:    t.Partition,
		PenLabel:     t.PenLabel,
		Reasons:      reasons,
		ReasonLabels: labels,
		ReasonLine:   ReasonLine(t, today),
		SourceDate:   t.SourceDate,
		PlannedDate:  t.PlannedDate,
		DueDate:      t.DueDate,
		WorkState:    t.WorkState,
		Status:       t.Status,
		StateChip:    StateChip(t, today),
		StateTone:    StateTone(t),
		Instruction:  Instruction(t),
		DoneLine:     DoneLine(t),
		ReworkReason: t.ReworkReason,
		CanSubmit:    t.CanSubmit(actor),
		Verified:     t.IsVerified(),
		ProofRef:     t.ProofRef,
		SubmittedAt:  wireInstant(t.SubmittedAt),
		VerifiedAt:   wireInstant(t.VerifiedAt),
		RowVersion:   t.RowVersion,
	}
}

func wireInstant(at *time.Time) *string {
	if at == nil {
		return nil
	}
	s := at.UTC().Format("2006-01-02T15:04:05Z07:00")
	return &s
}

// PenRef names a pen for a batched visit lookup.
type PenRef struct {
	ShedID    string
	Partition string
}

// PenKey is the map key ForPens answers under: the shed id and the NORMALIZED partition, the
// same key pen_visit_tasks.partition_key carries, so "Part 3", "part 3" and " Part 3 " meet.
func PenKey(shedID, partition string) string {
	return shedID + "|" + normalizePartitionKey(partition)
}

func normalizePartitionKey(partition string) string {
	p := strings.ToLower(strings.TrimSpace(partition))
	if p == "" || p == "whole" {
		return "whole"
	}
	return p
}
