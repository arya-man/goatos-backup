// Package domain holds the module-agnostic Verification vertical types (verification-module-design.md).
// The verification module knows nothing about vaccination/feed/diagnosis/etc. internals; producers
// speak to it only through the generic Item shape + a back-pointer SourceRef.
package domain

import (
	"errors"
	"time"
)

// Status values for a verification_item.
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusRejected = "rejected"
	// StatusWithdrawn is the verdict-FREE terminal status a producing module puts
	// on its own still-pending items when the source record they point at is
	// superseded (see Repository.WithdrawItemsBySource). It is not a decision: no
	// verifier, no verdict, no reason. It has always existed in the database;
	// naming it here lets the retraction event and its consumers branch on the
	// same constant the write path uses.
	StatusWithdrawn = "withdrawn"
)

// Decision values accepted by RecordVerdict.
const (
	DecisionApproved = "approved"
	DecisionRejected = "rejected"
)

// VerdictState is what the item is DOING right now, as opposed to Status, which
// is only what the verifier decided.
//
// The two are not the same thing and conflating them is what made W-18 dangerous.
// A verdict is applied asynchronously: RecordVerdict writes the decision and an
// outbox row, and the producing module's own record does not change until the
// durable event is consumed. Status flips the instant the verifier taps; the
// world does not. Between those two moments the item is neither awaiting review
// nor settled, and a surface that only knows Status has no way to say so -- it
// drops the item out of the pending queue and shows the verifier an empty list,
// which reads as "done" when nothing has happened yet.
//
// VerdictState is derived (see Item.VerdictState), never stored: it is a reading
// of applier_ack_expected + applied_at + status, so there is exactly one source
// of truth and no state machine to keep in sync.
const (
	// VerdictStateAwaitingReview: no verifier has decided this yet.
	VerdictStateAwaitingReview = "awaiting_review"
	// VerdictStateApplying: the verifier decided, and the producing module has
	// NOT yet confirmed it wrote that outcome onto its own record. This is a
	// normal, usually brief state -- but it is also exactly what a stopped
	// relay, a lagging consumer, or a dead-lettered event looks like, which is
	// why it has to be visible rather than inferred from an empty queue.
	VerdictStateApplying = "applying"
	// VerdictStateSettled: the decision has been applied by the producing
	// module, or the producing module does not participate in the ack protocol
	// (applier_ack_expected=false) so there is nothing to wait on here.
	VerdictStateSettled = "settled"
)

var (
	ErrInvalid         = errors.New("verification: invalid input")
	ErrReasonRequired  = errors.New("verification: reason is required to reject")
	ErrUnknownCategory = errors.New("verification: category is not registered")
)

// FieldError and ErrorEnvelope mirror the sop/proof modules' per-module error envelope shape
// (each module owns its own copy rather than sharing a cross-module type).
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	FieldErrors []FieldError `json:"field_errors"`
	TraceID     string       `json:"trace_id"`
	Retryable   bool         `json:"retryable"`
}

// SourceRef is the generic back-pointer to the producing module's own record. Verification stores
// only this reference tuple — never producer-specific columns (verification-module-design.md §2.2).
type SourceRef struct {
	Module       string  `json:"module"`
	TaskID       *string `json:"task_id,omitempty"`
	SubmissionID *string `json:"submission_id,omitempty"`
	RefType      string  `json:"ref_type"`
	RefID        string  `json:"ref_id"`
}

// Item is one unit of media awaiting (or having received) independent verification.
type Item struct {
	ItemID       string
	TenantID     string
	Vertical     string
	Module       string
	Category     string
	SubjectLabel *string
	// SubjectNote is optional free text from whoever RAISED the underlying work, shown to the
	// verifier during review. Kept separate from SubjectLabel on purpose: the label is
	// system-composed identity ("Shed move · 12 animals"), this is a human's words about it.
	SubjectNote    *string
	Source         SourceRef
	MediaRefs      []string // proof_artifact IDs; signed URLs resolved at read time.
	Status         string
	VerdictReason  *string
	OperatorID     *string
	OperatorName   *string // backend-owned display label for OperatorID
	ShedID         *string
	ShedLabel      *string // backend-owned display label for ShedID
	PartitionLabel *string // raw partition label ('1', 'Part 3') or nil for non-partitioned
	OperationalLocationDisplay *string // backend-composed operational location (shed + partition). Examples: "Castro 2", "Godel 1 - Part 3", "Yashoda"
	ParkID         *string
	ParkLabel      *string // backend-owned display label for ParkID
	CapturedAt     time.Time
	VerifiedBy     *string
	VerifiedByName *string // backend-owned display label for VerifiedBy
	VerifiedAt     *time.Time
	ClosedBy       *string
	ClosedAt       *time.Time
	// ApplierAckExpected is the producing module's declaration that it runs an
	// applier which acks back. Producers that have not wired an ack leave it
	// false and their items never enter VerdictStateApplying -- better silent
	// than falsely alarming on every decided item they own.
	ApplierAckExpected bool
	// AppliedAt / AppliedByModule are the producing module's receipt: it wrote
	// the verdict outcome onto its OWN record. Stamped by MarkVerdictApplied
	// from the applier, after that applier's transaction committed.
	AppliedAt       *time.Time
	AppliedByModule *string
	RowVersion      int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// VerdictState reads the item's real position between "a verifier decided" and
// "the farm's records changed". See the VerdictState* constants for why Status
// alone cannot answer that.
func (i Item) VerdictState() string {
	if i.Status == StatusPending {
		return VerdictStateAwaitingReview
	}
	// A withdrawn item carries no verdict at all -- the producing module retracted
	// the source record, so there is no outcome for an applier to apply and
	// nothing to wait on. Without this it would sit in "applying" forever.
	if i.Status == StatusWithdrawn {
		return VerdictStateSettled
	}
	if i.ApplierAckExpected && i.AppliedAt == nil {
		return VerdictStateApplying
	}
	return VerdictStateSettled
}

// CreateItem is the input a producer supplies to enqueue one verification item.
type CreateItem struct {
	TenantID       string
	Vertical       string
	Module         string
	Category       string
	SubjectLabel   *string
	SubjectNote    *string
	Source         SourceRef
	MediaRefs      []string
	OperatorID     *string
	ShedID         *string
	ParkID         *string
	PartitionLabel *string // raw partition label ('1', 'Part 3') or nil for non-partitioned
	CapturedAt     time.Time
	IdempotencyKey string
	// ApplierAckExpected: set true only if this producer actually runs an applier
	// that calls MarkVerdictApplied. Setting it true without wiring the ack would
	// park every decided item of yours in VerdictStateApplying permanently.
	ApplierAckExpected bool
}

// CreateItemResult reports whether CreateItem minted a new row (false = idempotent replay no-op,
// the existing item is still returned).
type CreateItemResult struct {
	Item    Item
	Created bool
}

// Verdict is the verifier's approve/reject decision on one item.
type Verdict struct {
	TenantID       string
	ItemID         string
	Decision       string
	Reason         string
	VerifierID     string
	RowVersion     int
	IdempotencyKey string
}

// CloseAction is the leadership authority transition applied only after verifier approval.
type CloseAction struct {
	TenantID       string
	ItemID         string
	ActorID        string
	RowVersion     int
	IdempotencyKey string
}

// CloseSubmissionAction closes one operator submission (the vaccination drive execution unit) only
// after every goat verification item in it has independent verifier approval.
type CloseSubmissionAction struct {
	TenantID       string
	SubmissionID   string
	ActorID        string
	IdempotencyKey string
}

// CloseVaccinationBatchAction closes one vaccination drive/batch after every obligation in that
// batch has an approved proof. The batch can contain one or more planned dates.
type CloseVaccinationBatchAction struct {
	TenantID       string
	BatchID        string
	ActorID        string
	IdempotencyKey string
}

// VaccinationBatchClosure is a leadership-ready drive close candidate.
type VaccinationBatchClosure struct {
	BatchID        string `json:"batch_id"`
	DriveKey       string `json:"drive_key,omitempty"`
	DriveLabel     string `json:"drive_label,omitempty"`
	BatchLabel     string `json:"batch_label,omitempty"`
	ParkID         string `json:"park_id,omitempty"`
	ParkLabel      string `json:"park_label,omitempty"`
	StartDate      string `json:"start_date,omitempty"`
	EndDate        string `json:"end_date,omitempty"`
	TotalCount     int    `json:"total_count"`
	ApprovedCount  int    `json:"approved_count"`
	RejectedCount  int    `json:"rejected_count"`
	PendingCount   int    `json:"pending_count"`
	VideoCount     int    `json:"video_count"`
	ApprovedVideos int    `json:"approved_videos"`
	RejectedVideos int    `json:"rejected_videos"`
	PendingVideos  int    `json:"pending_videos"`
	ShedCount      int    `json:"shed_count"`
	Ready          bool   `json:"ready"`
}

type LocationFilterOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// QueuePageOption is one backend-defined page tab within the selected verifier drawer module.
// Categories are disjoint queue filters over verification-item grain; the UI never adds tabs.
type QueuePageOption struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Category string `json:"category"`
}

// QueueStatusOption is one backend-defined secondary tab.
//
// The single-status tabs are disjoint at verification-item grain: pending (Due), approved, and
// rejected. The "All" tab carries an EMPTY Status, which is omitted from the payload — a renderer
// selecting it must send no status query parameter, and the queue then returns every status
// together (maintainer request 2026-07-30: the Actions page needs to see all actions at once).
type QueueStatusOption struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
}

// QueueActionTypeOption is one backend-registered verification action type exposed to
// cross-module renderers such as admin-web. Category is the disjoint queue predicate; the
// remaining fields are presentation and grouping metadata owned by the registry.
type QueueActionTypeOption struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	ModuleKey   string `json:"module_key"`
	ModuleLabel string `json:"module_label"`
}

type QueueFilterOptions struct {
	ModuleKey            string                  `json:"module_key,omitempty"`
	ModuleLabel          string                  `json:"module_label,omitempty"`
	ActionTypes          []QueueActionTypeOption `json:"action_types"`
	Pages                []QueuePageOption       `json:"pages"`
	Statuses             []QueueStatusOption     `json:"statuses"`
	Parks                []LocationFilterOption  `json:"parks"`
	Sheds                []LocationFilterOption  `json:"sheds"`
	Counts               QueueStatusCounts       `json:"counts"`
	SelectedBusinessDate string                  `json:"selected_business_date,omitempty"`
	BusinessTimezone     string                  `json:"business_timezone"`
	MissedOnly           bool                    `json:"missed_only"`
	HasMissed            bool                    `json:"has_missed"`
}

// QueueStatusCounts is a whole-filter aggregate row count per verification_item status
// (pending/approved/rejected), scoped identically to the paginated queue read — tenant,
// category/vertical/module, park, shed, business date — but computed by ONE indexed GROUP BY query
// in the repository. It must never be derived by fetching a page and grouping in app/service/
// frontend state (goatos-code-review scale anti-pattern: "capped read-time rollup presented as
// truth"), because that silently undercounts once the in-scope backlog exceeds the fetched page.
//
// projection-review: membership=verification_items rows with status IN (pending, approved,
// rejected), unique key (tenant_id, item_id) — one row per verification item;
// group_key=status; join_cardinality=none — the count query reads a single table, no joined side
// to fan out; pagination=whole-filter, no LIMIT/OFFSET/cursor, independent of the queue page's
// keyset window; scope=tenant_id + category + vertical + module + park_id + shed_id + captured_at
// window, identical to the queue page's own predicates minus status. Disjointness: pending,
// approved, and rejected are mutually exclusive values of the single `status` column (CHECK-
// constrained to pending/approved/rejected/withdrawn, withdrawn excluded by the count query) — a
// verification_items row is in exactly one bucket, so the three counts partition (never overlap)
// the in-scope backlog.
type QueueStatusCounts struct {
	Pending  int `json:"pending"`
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
}

// MediaItem is one resolved, streamable media reference for display.
type MediaItem struct {
	ProofID     string `json:"proof_id"`
	Label       string `json:"label,omitempty"`
	Answer      string `json:"answer,omitempty"`
	DownloadURL string `json:"download_url"`
	MimeType    string `json:"mime_type,omitempty"`
	DurationMS  *int64 `json:"duration_ms,omitempty"`
}

// QueueRow is one queue listing row: the item plus its resolved media.
type QueueRow struct {
	Item  Item
	Media []MediaItem
	// EvidenceLinkResolved reports that EVERY media_ref on the item resolved to a signed download
	// link at read time. It is a LINK-RESOLUTION claim, NOT a byte-retrievability guarantee: the
	// queue read deliberately does not stat the stored objects (see resolveMedia). Terminal
	// unavailability is discovered on the download path, which answers 410 proof_object_missing
	// with retryable=false. Serialized as `evidence_available` for wire compatibility.
	EvidenceLinkResolved bool
}
