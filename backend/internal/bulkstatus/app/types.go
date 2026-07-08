package app

// PreviewInput is the request context for a preview call.
type PreviewInput struct {
	TenantID string
	TraceID  string
	RawBody  []byte
}

// CommitInput is the request context for a commit call.
type CommitInput struct {
	TenantID       string
	ActorID        string
	IdempotencyKey string
	TraceID        string
	RawBody        []byte
}

// PreviewRow is one requested per-goat change in the wire payload.
type PreviewRow struct {
	GoatID string `json:"goat_id"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
}

// PreviewRequest is the preview wire body.
type PreviewRequest struct {
	Axis string       `json:"axis"`
	Rows []PreviewRow `json:"rows"`
}

// PreviewDecision categorizes what a commit would do with a row.
const (
	DecisionApply          = "apply"           // will transition on apply
	DecisionNoop           = "noop"            // already at target; will skip
	DecisionNotFound       = "not_found"       // goat missing/exited/merged/out of scope
	DecisionBlocked        = "blocked"         // target needs the critical-action guardrail path
	DecisionRequiresReview = "requires_review" // invalid target / malformed row
)

// PreviewRowResult is the per-row decision surfaced back to the operator.
type PreviewRowResult struct {
	GoatID       string `json:"goat_id"`
	Decision     string `json:"decision"`
	CurrentState string `json:"current_state,omitempty"`
	Target       string `json:"target"`
	RowVersion   *int   `json:"row_version,omitempty"`
	Message      string `json:"message,omitempty"`
}

// PreviewSummary is the decision rollup.
type PreviewSummary struct {
	Total          int `json:"total"`
	Apply          int `json:"apply"`
	Noop           int `json:"noop"`
	NotFound       int `json:"not_found"`
	Blocked        int `json:"blocked"`
	RequiresReview int `json:"requires_review"`
}

// PreviewResponse is the preview result. It never mutates state. PreviewToken is
// a signed binding over (tenant, axis, row-set fingerprint, count) that commit
// must present; it does not embed the (potentially millions of) rows.
type PreviewResponse struct {
	Axis         string             `json:"axis"`
	Summary      PreviewSummary     `json:"summary"`
	Rows         []PreviewRowResult `json:"rows"`
	PreviewToken string             `json:"preview_token"`
	TraceID      string             `json:"trace_id"`
}

// CommitRequest is the commit wire body. Rows are re-sent so the server can
// re-derive the fingerprint the token was signed over and then bulk-enqueue them.
type CommitRequest struct {
	Axis         string       `json:"axis"`
	Rows         []PreviewRow `json:"rows"`
	PreviewToken string       `json:"preview_token"`
}

// CommitResponse reports the enqueued (or replayed) job.
type CommitResponse struct {
	JobID     string `json:"job_id"`
	Axis      string `json:"axis"`
	TotalRows int    `json:"total_rows"`
	State     string `json:"state"`
	Replayed  bool   `json:"replayed"`
	TraceID   string `json:"trace_id"`
}
