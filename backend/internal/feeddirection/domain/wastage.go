package domain

import (
	"time"

	"github.com/vgoats/goatos/backend/internal/platform/oploc"
)

// FEED WASTAGE (maintainer decision 2026-08-18). A daily task on EXPERIMENT pens only: every feed
// day, each pen on a hand-authored feed experiment owes ONE wastage video — the operator films the
// leftover feed and submits, the verifier reads the leftover weight off the clip and records it in
// kg, or rejects for a re-shoot when the value is not readable.
//
// Grain is the PEN-DAY. There is deliberately NO session: packing and distribution are per-bag work
// (a pen's morning and evening are two bags, two videos), but wastage is what is LEFT OVER after the
// day's feeding, measured once. One pen, one feed day, one video, one recorded value.
//
// The worklist is DERIVED from the day's frozen EXPERIMENT sheet — the same rows packing reads — so
// "which pens are on the experiment today" has exactly one source of truth and this file adds no
// second planner. A pen appears here if and only if the experiment sheet has a row for it.

// WastageQuery selects one park's wastage worklist for one feed day.
//
// There is no Workflow field: wastage is experiment-only by definition, and offering a workflow
// filter would imply a normal-workflow wastage list exists to select. There is no SessionNo for the
// same reason there is no session in the grain. There is no Draft: the worklist carries no
// quantities, so a config-authoring what-if has nothing to preview.
type WastageQuery struct {
	TenantID   string
	ParkID     string
	TargetDate time.Time
	// ShedID / PartitionLabel optionally narrow to a single operational location, mirroring
	// PackingQuery — used by live-status polling to guarantee the target row lands on page one.
	ShedID         string
	PartitionLabel string
	// Status optionally narrows to one verification-lifecycle bucket (SessionStatus* value). Empty
	// means every status. Applied over the WHOLE scope before paging, so a filtered page and its
	// summary describe the same status set.
	Status string
	Limit  int32
	Offset int32
	// AuthorizedParkIDs is the caller's own park scope; empty means unrestricted. Same contract as
	// PackingQuery.AuthorizedParkIDs.
	AuthorizedParkIDs []string
}

// WastageRow is ONE wastage line: one experiment pen's single wastage task for one feed day.
type WastageRow struct {
	ParkID    string `json:"park_id"`
	ParkLabel string `json:"park_label"`
	ShedID    string `json:"shed_id"`
	ShedLabel string `json:"shed_label"`
	// PartitionLabel is the pen ("1", "Part 3"), empty for an undivided shed. Part of the row's
	// IDENTITY for the same reason it is part of packing's: Castro 1 and Castro 2 are different
	// animals on different rations, and one pen's video must never close another pen's task.
	PartitionLabel string `json:"partition_label,omitempty"`
	// OperationalLocationDisplay is the backend-composed shed+pen label ("Castro - 2",
	// "Godel 1 - Part 3", bare "Yashoda" when undivided), built with platform/oploc so every
	// surface renders the pen the same way.
	OperationalLocationDisplay string `json:"operational_location_display"`
	// Workflow is always WorkflowExperiment — carried explicitly so the completion write and the
	// row agree byte-for-byte on the natural key, never inferred client-side.
	Workflow string `json:"workflow"`
	// ExperimentArm is the pen's authored trial group, so the operator knows which trial a
	// leftover measurement belongs to without opening the direction sheet.
	ExperimentArm string `json:"experiment_arm"`
	// HeadCount is the pen's projected head count — context for the verifier and the operator,
	// never a gate. It is the pen's population, identical across the day's sessions.
	HeadCount int64 `json:"head_count"`
	// LifecycleStatus is the verification-lifecycle bucket of this pen-day's wastage completion
	// (one of the SessionStatus* values). Completed collapses it for a coarse badge.
	LifecycleStatus string `json:"lifecycle_status"`
	Completed       bool   `json:"completed"`
	// ReworkReason is the backend-composed sentence telling the operator WHY this pen came back,
	// present only while the stored row is actually in rework. Rendered verbatim (golden frontend
	// rule).
	ReworkReason string `json:"rework_reason,omitempty"`
	// WastageKg is the verifier's recorded leftover weight in kg, present only once she has
	// recorded one ("0" is a real measurement — an empty trough). Read-only context on the
	// operator's list; the write rides the verifier's own route.
	WastageKg string `json:"wastage_kg,omitempty"`
}

// WastageSummary covers the WHOLE filtered worklist, never one page (operational read-model
// contract: summaries are whole-filter aggregates).
//
// projection-review: membership=every WastageRow built for the filtered scope (the service builds
// the worklist over the full shed scope before paging); group_key=lifecycle bucket per PEN row —
// one row per (shed_id, partition_key) by construction of BuildWastageRows, so no fan-out;
// join_cardinality=no joins — a pure in-memory fold over already-collapsed pen rows;
// pagination=INVARIANT to limit/offset, the fold runs over the whole filtered scope; buckets are
// DISJOINT (each row has exactly one LifecycleStatus) and TotalPens is their sum;
// scope=tenant + park + target_date, identical to the predicates that selected the rows.
type WastageSummary struct {
	// TotalPens is the number of experiment pens owing a wastage video on this feed day.
	TotalPens int32 `json:"total_pens"`
	// PendingPens have no submitted video yet, or were bounced back for a re-shoot (the rework
	// bucket normalizes to pending: "needs my action").
	PendingPens int32 `json:"pending_pens"`
	// InReviewPens have a submitted video awaiting the verifier.
	InReviewPens int32 `json:"in_review_pens"`
	// CompletedPens are verifier-approved.
	CompletedPens int32 `json:"completed_pens"`
}

// WastagePage is one page of the worklist. Paged by shed, mirroring PackingPage, so one shed's pens
// never straddle a page boundary.
type WastagePage struct {
	Items   []WastageRow   `json:"items"`
	Summary WastageSummary `json:"summary"`
	// Lifecycle carries the same issue-state metadata as PackingPage: the worklist exists once the
	// experiment sheet for the day is frozen, and before that the client is told when it arrives.
	Lifecycle Lifecycle `json:"lifecycle"`
	// Filters is the backend-owned park/shed filter vocabulary plus the served park id.
	Filters    FeedFilterOptions `json:"filters"`
	TargetDate string            `json:"target_date"`
	Limit      int32             `json:"limit"`
	Offset     int32             `json:"offset"`
	HasMore    bool              `json:"has_more"`
}

// BuildWastageRows collapses generated/frozen direction rows into the per-PEN wastage lines.
//
// EXPERIMENT ROWS ONLY — a normal-workflow row contributes nothing, which is the "everyday it
// triggers rows of only experiment pens" rule expressed once, here. A pen's several sessions and
// ration grains fold into ONE line, keyed by (shed, partitionKey) with the same PartitionMatchKey
// normalization packing uses so an authoring variant cannot split one pen into two tasks. Order is
// first-appearance, which preserves the shed scope's catalog order.
func BuildWastageRows(rows []DirectionRow) []WastageRow {
	type lineKey struct {
		shedID       string
		partitionKey string
	}
	type line struct {
		order int
		row   WastageRow
	}
	lines := map[lineKey]*line{}
	order := 0
	for _, row := range rows {
		if row.Workflow != WorkflowExperiment {
			continue
		}
		key := lineKey{shedID: row.ShedID, partitionKey: PartitionMatchKey(row.PartitionLabel)}
		if _, ok := lines[key]; ok {
			continue
		}
		lines[key] = &line{
			order: order,
			row: WastageRow{
				ParkID:         row.ParkID,
				ParkLabel:      row.ParkLabel,
				ShedID:         row.ShedID,
				ShedLabel:      row.ShedLabel,
				PartitionLabel: row.PartitionLabel,
				OperationalLocationDisplay: oploc.OperationalLocation{
					ShedName:       row.ShedLabel,
					PartitionLabel: row.PartitionLabel,
				}.Display(),
				Workflow: row.Workflow,
				// Safe to take from the first contributing row: the planner is selected per
				// pen, so every row of a pen shares one arm.
				ExperimentArm: row.ExperimentArm,
				// The pen's population, NOT a sum across sessions — the same animals eat morning
				// and evening, so the first row's head count is the pen's head count.
				HeadCount: row.HeadCount,
			},
		}
		order++
	}
	out := make([]WastageRow, len(lines))
	for _, l := range lines {
		out[l.order] = l.row
	}
	return out
}

// SummarizeWastage folds the WHOLE filtered scope's pen rows into the disjoint lifecycle buckets.
// See the projection-review marker on WastageSummary.
func SummarizeWastage(rows []WastageRow) WastageSummary {
	s := WastageSummary{TotalPens: int32(len(rows))}
	for _, row := range rows {
		switch row.LifecycleStatus {
		case SessionStatusAwaitingVerification:
			s.InReviewPens++
		case SessionStatusCompleted:
			s.CompletedPens++
		default:
			s.PendingPens++
		}
	}
	return s
}
