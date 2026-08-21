// Package domain holds the pure PC Care business vocabulary (maintainer decision 2026-08-21).
//
// PC Care is an ASSIGNED-TASK module: the CEO plans one task per (category, pen, business date)
// and names one or more operators. Assigned operators scan RFIDs free-flow (the tag is stored
// VERBATIM, never resolved against the herd) and record mandatory live-camera videos per animal.
// The task is submitted whole and completed only when a verifier approves the video set.
package domain

import (
	"errors"
	"strings"
)

// PartitionMatchKey normalizes a pen label to the value the pc_care_tasks.partition_key
// generated column stores: lower(btrim(label)), with blank meaning the whole shed. It mirrors
// the SQL expression byte-for-byte so a Go-side comparison and the natural key can never
// disagree.
func PartitionMatchKey(label string) string {
	trimmed := strings.ToLower(strings.Trim(label, " "))
	if trimmed == "" {
		return "whole"
	}
	return trimmed
}

// The four work categories. These are STORAGE/CONTRACT tokens, never user-facing copy — clients
// render the backend-owned labels carried on the nav/worklist contracts.
const (
	CategoryDeworming    = "deworming"
	CategoryTicksRemoval = "ticks_removal"
	CategoryHoofTrimming = "hoof_trimming"
	CategoryHairTrimming = "hair_trimming"
)

// Categories lists every valid category, in display order.
var Categories = []string{CategoryDeworming, CategoryTicksRemoval, CategoryHoofTrimming, CategoryHairTrimming}

// IsValidCategory reports whether c names a real PC Care category.
func IsValidCategory(c string) bool {
	switch c {
	case CategoryDeworming, CategoryTicksRemoval, CategoryHoofTrimming, CategoryHairTrimming:
		return true
	}
	return false
}

// Slot field keys. The 1-video categories use SlotVideo; the trimming categories use the
// before/during/after triple. These keys ARE the wire contract (expected_slots[].field_key and
// the animal proof-registration path segment) and the Android capture slot identity — one
// definition, owned here.
const (
	SlotVideo  = "video"
	SlotBefore = "before_video"
	SlotDuring = "during_video"
	SlotAfter  = "after_video"
)

// Capture modes (maintainer decision 2026-08-21, second pass). The quick jobs — deworming and
// ticks removal — are SCAN-AND-RECORD: the operator scans a tag and the phone opens the video
// recorder immediately. The trimming jobs are ROSTER-PICK: the screen lists the RFIDs of the
// animals currently in the task's pen and the operator taps one to record. These are
// STORAGE/CONTRACT tokens; the client branches on the task contract's capture_mode verbatim.
const (
	CaptureModeScanRecord = "scan_record"
	CaptureModeRosterPick = "roster_pick"
)

// CaptureModeForCategory maps a work category to its capture mode.
func CaptureModeForCategory(category string) string {
	switch category {
	case CategoryHoofTrimming, CategoryHairTrimming:
		return CaptureModeRosterPick
	}
	return CaptureModeScanRecord
}

// Slot describes one expected proof slot for a category, as served to clients on the task
// detail contract. Label is backend-owned farm copy rendered verbatim.
type Slot struct {
	FieldKey string
	Label    string
	// MinDurationHintSeconds is recorder-chrome GUIDANCE (the ~10 s "during" clip), never a
	// client-enforced cap. Zero means no hint.
	MinDurationHintSeconds int
}

// SlotsForCategory returns the ordered mandatory proof slots for a category. Every scanned
// animal must carry every slot before the task can be submitted. The slot set is BACKEND-OWNED:
// clients iterate this list off the contract and never hardcode a category→slot map.
func SlotsForCategory(category string) []Slot {
	switch category {
	case CategoryDeworming:
		return []Slot{{FieldKey: SlotVideo, Label: "Deworming video"}}
	case CategoryTicksRemoval:
		return []Slot{{FieldKey: SlotVideo, Label: "Ticks removal video"}}
	// Maintainer decision 2026-08-21 (restated in the second pass): the trimming categories keep
	// THREE videos per animal — before, while (~10 s), and after the work. Only the capture FLOW
	// changed to roster_pick: the operator taps the animal's RFID off the pen roster and the
	// screen walks the three clips.
	case CategoryHoofTrimming:
		return []Slot{
			{FieldKey: SlotBefore, Label: "Before trimming"},
			{FieldKey: SlotDuring, Label: "While trimming", MinDurationHintSeconds: 10},
			{FieldKey: SlotAfter, Label: "After trimming"},
		}
	case CategoryHairTrimming:
		return []Slot{
			{FieldKey: SlotBefore, Label: "Before trimming"},
			{FieldKey: SlotDuring, Label: "While trimming", MinDurationHintSeconds: 10},
			{FieldKey: SlotAfter, Label: "After trimming"},
		}
	}
	return nil
}

// IsValidSlotForCategory reports whether fieldKey is one of category's expected slots.
func IsValidSlotForCategory(category, fieldKey string) bool {
	for _, s := range SlotsForCategory(category) {
		if s.FieldKey == fieldKey {
			return true
		}
	}
	return false
}

// CategoryLabel is the backend-owned English display name for a category. Locale-specific copy
// lives on the nav/bootstrap contract; this is the label used on verifier items and
// notifications composed server-side.
func CategoryLabel(category string) string {
	switch category {
	case CategoryDeworming:
		return "Deworming"
	case CategoryTicksRemoval:
		return "Ticks Removal"
	case CategoryHoofTrimming:
		return "Hoof Trimming"
	case CategoryHairTrimming:
		return "Hair Trimming"
	}
	return category
}

// SlotDisplayLabel is the per-media-ref suffix on the verifier's item ("<tag> · Before trimming").
func SlotDisplayLabel(category, fieldKey string) string {
	for _, s := range SlotsForCategory(category) {
		if s.FieldKey == fieldKey {
			return s.Label
		}
	}
	return fieldKey
}

// Verification coordinates. Module pc_care with FOUR categories, one per work category, all under
// ONE RefType — the verdict consumers filter on Module+RefType, and the verifier's queue splits
// by category as page filters (never one Verify tab per category).
const (
	VerificationVerticalPreventiveCare = "preventive_care"
	VerificationModulePCCare           = "pc_care"
	VerificationCategoryDeworming      = "pc_deworming"
	VerificationCategoryTicksRemoval   = "pc_ticks_removal"
	VerificationCategoryHoofTrimming   = "pc_hoof_trimming"
	VerificationCategoryHairTrimming   = "pc_hair_trimming"
	VerificationRefTypeTask            = "pc_care_task"
)

// VerificationCategoryFor maps a work category to its verification category.
func VerificationCategoryFor(category string) string {
	switch category {
	case CategoryDeworming:
		return VerificationCategoryDeworming
	case CategoryTicksRemoval:
		return VerificationCategoryTicksRemoval
	case CategoryHoofTrimming:
		return VerificationCategoryHoofTrimming
	case CategoryHairTrimming:
		return VerificationCategoryHairTrimming
	}
	return ""
}

// Task work_state values — the KERNEL dimension (weighing 000059 shape), orthogonal to the
// verification status below.
const (
	WorkStateScheduled = "scheduled"
	WorkStateDelayed   = "delayed"
	WorkStateCompleted = "completed"
	WorkStateClosed    = "closed"
	WorkStateCanceled  = "canceled"
)

// Task verification status values — the GATE dimension (feed 000176 shape). "open" is the
// pre-submit working state; the other three are the verifier-gated lifecycle.
const (
	StatusOpen                = "open"
	StatusPendingVerification = "pending_verification"
	StatusCompleted           = "completed"
	StatusRework              = "rework"
)

// Actor is the authenticated caller, resolved by the HTTP adapter.
type Actor struct {
	TenantID string
	UserID   string
	Roles    []string
}

// Typed errors, mapped to HTTP codes by the adapter.
var (
	// ErrDuplicateScan is returned when a tag already sits in this task (the ONE business rule
	// on scans). Surfaces as 409 duplicate_scan.
	ErrDuplicateScan = errors.New("pccare: this tag is already scanned in this task")
	// ErrTaskNotAssigned is returned when an execute-permission holder who is NOT an assignee
	// of the task attempts a write. Surfaces as 403 task_not_assigned.
	ErrTaskNotAssigned = errors.New("pccare: caller is not assigned to this task")
	// ErrProofIncomplete is returned when a submit arrives while some scanned animal is still
	// missing a mandatory slot. Surfaces as 422 proof_incomplete.
	ErrProofIncomplete = errors.New("pccare: some animals are missing required videos")
	// ErrNoAnimals is returned when a submit arrives on a task with zero scanned animals —
	// there is nothing for a verifier to review. Surfaces as 422 no_animals.
	ErrNoAnimals = errors.New("pccare: no animals scanned in this task")
	// ErrInvalidSlotForCategory is returned when the named slot is not one of the task
	// category's expected slots. Surfaces as 422 invalid_slot.
	ErrInvalidSlotForCategory = errors.New("pccare: slot is not valid for this task's category")
	// ErrInvalidCategory is returned for an unknown category token. Surfaces as 422.
	ErrInvalidCategory = errors.New("pccare: unknown category")
	// ErrTaskNotOpen is returned when a scan/slot write arrives while the task is locked
	// (pending_verification) or terminal. Surfaces as 409 task_locked.
	ErrTaskNotOpen = errors.New("pccare: task is not open for capture")
	// ErrTaskAlreadyPlanned is returned when the planner names a (category, pen, date) that
	// already carries a live task. Surfaces as 409 task_already_planned.
	ErrTaskAlreadyPlanned = errors.New("pccare: a task for this pen, category and date already exists")
	// ErrAssigneesRequired is returned when a planner create names no operators.
	ErrAssigneesRequired = errors.New("pccare: at least one assigned operator is required")
)
