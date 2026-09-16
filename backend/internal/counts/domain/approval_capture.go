package domain

// CountsApprovalCapture is what the approver sees of a raise's SOP capture form before deciding:
// the pinned version label, every answer in farm words (grouped), every capture under its slot
// title with its kind, and a note naming what an older app did not send.
//
// SHARED CONTRACT (program decision 2026-09-16): this shape is owned by the herd-operations
// workstream (birth / death capture cards) and is reproduced here EXACTLY -- same names, same JSON
// -- so shifting's raise snapshot (shifting_events.raise_capture_evidence) rides
// CountsApprovalListItem.capture in the one shape both renderers already read.
type CountsApprovalCapture struct {
	VersionLabel string                       `json:"version_label"`
	Rows         []CountsApprovalCaptureRow   `json:"rows"`
	Media        []CountsApprovalCaptureMedia `json:"media"`
	MissingNote  string                       `json:"missing_note,omitempty"`
}

// CountsApprovalCaptureRow is one answer in farm words.
type CountsApprovalCaptureRow struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Group string `json:"group,omitempty"`
}

// CountsApprovalCaptureMedia is one capture the approver can open.
type CountsApprovalCaptureMedia struct {
	ProofID string `json:"proof_id"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
}
