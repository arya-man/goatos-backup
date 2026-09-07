// Package domain is the rulebook of the Pen Visit module (maintainer decision 2026-09-07).
//
// The day after any preventive-care work is SUBMITTED in a pen -- a vaccination shed proof or a
// PC Care task (deworming, anti protozoan, ticks removal, hoof trimming, hair trimming) -- the
// park's head visits that pen, records ONE live-camera video and submits it. Nothing else: no
// per-animal scan, no roster, no verifier queue. The task is raised by the kernel, never typed by
// a person, and it belongs to the ONE person the park's config row names (CBE -> Dinakar,
// CPT -> Chandrakant).
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

// Work states. PC Care's kernel shape (000183) minus the verification gate: a visit has no
// verifier, so submit IS completion.
const (
	WorkStateScheduled = "scheduled"
	WorkStateDelayed   = "delayed"
	WorkStateCompleted = "completed"
	WorkStateCanceled  = "canceled"
)

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
	ErrNotAssignee     = errors.New("pen visit: caller is not the assignee")
	ErrAlreadyDone     = errors.New("pen visit: already submitted")
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
	PenLabel     string
	Reasons      []string
	SourceDate   string // YYYY-MM-DD, the IST day the work was submitted
	PlannedDate  string // YYYY-MM-DD, immutable
	DueDate      string // YYYY-MM-DD, rolls forward only
	WorkState    string
	AssigneeID   string
	ProofRef     *string
	SubmittedBy  *string
	SubmittedAt  *time.Time
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

// IsAssignee reports whether the task belongs to the actor.
func (t Task) IsAssignee(a Actor) bool { return a.UserID != "" && a.UserID == t.AssigneeID }

// IsOpen reports whether the visit is still owed.
func (t Task) IsOpen() bool {
	return t.WorkState == WorkStateScheduled || t.WorkState == WorkStateDelayed
}

// CanSubmit: the assignee, while the visit is still owed.
func (t Task) CanSubmit(a Actor) bool { return t.IsAssignee(a) && t.IsOpen() }

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

// StateChip is the card chip: where the visit stands, in farm words.
func StateChip(t Task, today string) string {
	switch t.WorkState {
	case WorkStateCompleted:
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

// StateTone is the chip's colour token: the phone maps it, never composes its own.
func StateTone(t Task) string {
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
	return "Go to the pen, look at the animals and record one video. Submit it here."
}

// DoneLine is the detail line once the visit is submitted.
func DoneLine(t Task) string {
	if t.SubmittedAt == nil {
		return ""
	}
	return "Visited " + biztime.FarmDate(*t.SubmittedAt)
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
// listed: a cancelled visit is noise on a park head's day.
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
