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
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/biztime"
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
	CategoryDeworming        = "deworming"
	CategoryTicksRemoval     = "ticks_removal"
	CategoryHoofTrimming     = "hoof_trimming"
	CategoryHairTrimming     = "hair_trimming"
	CategoryInventoryVaccine = "inventory_vaccine"
	// CategoryFeedWaterRemoval is the evening-before precondition for a tablet-in-feed deworming
	// (maintainer decision 2026-09-03): feed and water are removed from the pen the evening
	// before, each act proved by its own live-camera video. The row is created by the deworming
	// planner create in the SAME transaction — never planned on its own — and it appears in the
	// operator's worklist only from 20:00 IST on its due day.
	CategoryFeedWaterRemoval = "feed_water_removal"
)

// Categories lists every valid category, in display order.
var Categories = []string{CategoryDeworming, CategoryTicksRemoval, CategoryHoofTrimming, CategoryHairTrimming, CategoryInventoryVaccine, CategoryFeedWaterRemoval}

// PlannerCategories lists categories humans may plan through the PC Care create wizard.
// Kernel-owned categories stay readable/listable, but are created by reconciliation stages.
var PlannerCategories = []string{CategoryDeworming, CategoryTicksRemoval, CategoryHoofTrimming, CategoryHairTrimming}

// IsValidCategory reports whether c names a real PC Care category.
func IsValidCategory(c string) bool {
	switch c {
	case CategoryDeworming, CategoryTicksRemoval, CategoryHoofTrimming, CategoryHairTrimming, CategoryInventoryVaccine, CategoryFeedWaterRemoval:
		return true
	}
	return false
}

// IsKernelOwnedCategory reports categories that are created by the system, not by direct
// planner/API writes: inventory_vaccine rows come from kernel reconciliation, and
// feed_water_removal rows are born inside the deworming create transaction.
func IsKernelOwnedCategory(c string) bool {
	return c == CategoryInventoryVaccine || c == CategoryFeedWaterRemoval
}

// VerifierReviewedCategories are the categories whose submitted proof travels to the tenant
// VERIFIER's queue. inventory_vaccine is deliberately absent (maintainer decision 2026-09-02):
// the vaccine-stock check is recorded by park operators and approved by the PC DIRECTOR on the
// module's own stock-verdict route — the toxin-module approval-gate shape — so the verifier
// never sees stock work and no verification item is enqueued for it.
var VerifierReviewedCategories = []string{CategoryDeworming, CategoryTicksRemoval, CategoryHoofTrimming, CategoryHairTrimming, CategoryFeedWaterRemoval}

// IsDirectorApprovedCategory reports whether a category's submitted proof is judged by the PC
// Director instead of the tenant verifier.
func IsDirectorApprovedCategory(c string) bool {
	return c == CategoryInventoryVaccine
}

// Stock verdict tokens carried on POST /app/pc-care/tasks/{task_id}/stock-verdict. These are
// CONTRACT tokens, never user-facing copy.
const (
	StockVerdictApprove = "approve"
	StockVerdictReject  = "reject"
)

// Slot field keys. The 1-video categories use SlotVideo; the trimming categories use the
// before/during/after triple. These keys ARE the wire contract (expected_slots[].field_key and
// the animal proof-registration path segment) and the Android capture slot identity — one
// definition, owned here.
const (
	SlotVideo            = "video"
	SlotBefore           = "before_video"
	SlotDuring           = "during_video"
	SlotAfter            = "after_video"
	SlotStockFridgePhoto = "stock_fridge_photo"
	SlotStockFridgeVideo = "stock_fridge_video"
	SlotFeedVideo        = "feed_video"
	SlotWaterVideo       = "water_video"
)

// Capture modes (maintainer decision 2026-08-21, second pass). The quick jobs — deworming and
// ticks removal — are SCAN-AND-RECORD: the operator scans a tag and the phone opens the video
// recorder immediately. The trimming jobs are ROSTER-PICK: the screen lists the RFIDs of the
// animals currently in the task's pen and the operator taps one to record. These are
// STORAGE/CONTRACT tokens; the client branches on the task contract's capture_mode verbatim.
const (
	CaptureModeScanRecord = "scan_record"
	CaptureModeRosterPick = "roster_pick"
	CaptureModeTaskProof  = "task_proof"
)

// CaptureModeForCategory maps a work category to its capture mode.
func CaptureModeForCategory(category string) string {
	switch category {
	case CategoryHoofTrimming, CategoryHairTrimming:
		return CaptureModeRosterPick
	case CategoryInventoryVaccine, CategoryFeedWaterRemoval:
		return CaptureModeTaskProof
	}
	return CaptureModeScanRecord
}

// Slot describes one expected proof slot for a category, as served to clients on the task
// detail contract. Label is backend-owned farm copy rendered verbatim.
type Slot struct {
	FieldKey string
	Label    string
	// Description is backend-owned farm copy saying what this video must show, rendered
	// verbatim on the capture card.
	Description string
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
		return []Slot{{
			FieldKey: SlotVideo, Label: "Deworming video",
			Description: "Show the dose being given to this animal",
		}}
	case CategoryTicksRemoval:
		return []Slot{{
			FieldKey: SlotVideo, Label: "Ticks removal video",
			Description: "Show the ticks being removed from this animal",
		}}
	// Maintainer decision 2026-08-21 (restated in the second pass): the trimming categories keep
	// THREE videos per animal — before, while (~10 s), and after the work. Only the capture FLOW
	// changed to roster_pick: the operator taps the animal's RFID off the pen roster and the
	// screen walks the three clips.
	case CategoryHoofTrimming:
		return []Slot{
			{FieldKey: SlotBefore, Label: "Before trimming", Description: "Show the animal's hooves before the work"},
			{FieldKey: SlotDuring, Label: "While trimming", Description: "Record the hooves being trimmed", MinDurationHintSeconds: 10},
			{FieldKey: SlotAfter, Label: "After trimming", Description: "Show the trimmed hooves after the work"},
		}
	case CategoryHairTrimming:
		return []Slot{
			{FieldKey: SlotBefore, Label: "Before trimming", Description: "Show the animal's coat before the work"},
			{FieldKey: SlotDuring, Label: "While trimming", Description: "Record the hair being trimmed", MinDurationHintSeconds: 10},
			{FieldKey: SlotAfter, Label: "After trimming", Description: "Show the trimmed coat after the work"},
		}
	case CategoryInventoryVaccine:
		return []Slot{
			{
				FieldKey: SlotStockFridgePhoto, Label: "Fridge stock photo",
				Description: "Take a clear photo of the vaccine stock available in the fridge",
			},
			{
				FieldKey: SlotStockFridgeVideo, Label: "Fridge stock video",
				Description: "Record the vaccine stock available in the fridge for the scheduled vaccination",
			},
		}
	case CategoryFeedWaterRemoval:
		return []Slot{
			{
				FieldKey: SlotFeedVideo, Label: "Feed removal video",
				Description: "Show the feed being taken out of this pen",
			},
			{
				FieldKey: SlotWaterVideo, Label: "Water removal video",
				Description: "Show the water being taken out of this pen",
			},
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
	case CategoryInventoryVaccine:
		return "Vaccine Inventory"
	case CategoryFeedWaterRemoval:
		return "Feed & water removal"
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
	VerificationVerticalPreventiveCare   = "preventive_care"
	VerificationModulePCCare             = "pc_care"
	VerificationCategoryDeworming        = "pc_deworming"
	VerificationCategoryTicksRemoval     = "pc_ticks_removal"
	VerificationCategoryHoofTrimming     = "pc_hoof_trimming"
	VerificationCategoryHairTrimming     = "pc_hair_trimming"
	VerificationCategoryInventoryVaccine = "inventory_vaccine"
	VerificationCategoryFeedWaterRemoval = "pc_feed_water_removal"
	VerificationRefTypeTask              = "pc_care_task"
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
	case CategoryInventoryVaccine:
		return VerificationCategoryInventoryVaccine
	case CategoryFeedWaterRemoval:
		return VerificationCategoryFeedWaterRemoval
	}
	return ""
}

// ---------------------------------------------------------------------------
// Feed & water removal precondition (maintainer decision 2026-09-03)
// ---------------------------------------------------------------------------

// FeedRemovalEveningHourIST is the IST wall-clock hour that closes the planning window and
// opens the removal card: a deworming that needs feed & water removal can be planned for
// tomorrow only until 20:00 IST (the crew removing feed tonight must still have tonight), and
// the removal card surfaces on the operator's worklist from 20:00 IST of its due day.
const FeedRemovalEveningHourIST = 20

// EarliestFeedRemovalDewormingDate returns the earliest planned business date (00:00 IST) a
// deworming that requires feed & water removal may take, given the caller's clock: TOMORROW
// while the IST wall clock is before 20:00, the DAY AFTER TOMORROW from 20:00 on — because the
// removal happens the evening before, and by 20:00 tonight's removal can no longer be staffed.
// The clock is the CALLER's (counts/domain.ShiftingActionsDueFrom shape), never read here, so
// the rule is deterministic in tests.
func EarliestFeedRemovalDewormingDate(now time.Time) time.Time {
	local := now.In(biztime.DefaultLocation())
	leadDays := 1
	if local.Hour() >= FeedRemovalEveningHourIST {
		leadDays = 2
	}
	return biztime.BusinessDayStart(local).AddDate(0, 0, leadDays)
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
	// ErrKernelOwnedCategory is returned when a planner/API write tries to create work whose
	// source of truth is a kernel reconciliation path.
	ErrKernelOwnedCategory = errors.New("pccare: category is created by the kernel")
	// ErrNotStockTask is returned when a stock verdict names a task that is not an
	// inventory_vaccine stock task. Surfaces as 422 not_stock_task.
	ErrNotStockTask = errors.New("pccare: this task is not a vaccine stock task")
	// ErrStockVerdictNotPending is returned when a stock verdict arrives while the task is not
	// awaiting the director's decision. Surfaces as 409 verdict_not_pending.
	ErrStockVerdictNotPending = errors.New("pccare: this task is not awaiting approval")
	// ErrInvalidStockVerdict is returned for an unknown verdict token. Surfaces as 422.
	ErrInvalidStockVerdict = errors.New("pccare: unknown verdict")
	// ErrStockRejectReasonRequired is returned when a reject carries no reason — the operators
	// re-recording the fridge are owed a sentence saying why. Surfaces as 422 reason_required.
	ErrStockRejectReasonRequired = errors.New("pccare: a reason is required to reject")
	// ErrFastingWindowClosed is returned when a deworming that requires feed & water removal is
	// planned for a date whose evening-before removal can no longer be staffed (before 20:00 IST
	// the earliest date is tomorrow; from 20:00 IST it is the day after tomorrow). Surfaces as
	// 422 fasting_window_closed.
	ErrFastingWindowClosed = errors.New("pccare: too late to remove feed and water the evening before this date")
	// ErrRemovalOperatorsRequired is returned when feed & water removal is requested with no
	// operators named for the removal task. Surfaces as 422 removal_operators_required.
	ErrRemovalOperatorsRequired = errors.New("pccare: at least one operator is required for the feed and water removal")
	// ErrFeedRemovalNotApplicable is returned when feed & water removal fields ride a create for
	// a category other than deworming — rejected loudly, never silently dropped. Surfaces as 422
	// feed_removal_not_applicable.
	ErrFeedRemovalNotApplicable = errors.New("pccare: feed and water removal applies to deworming only")
)
