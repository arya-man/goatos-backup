package domain

// CorrectCensusSliceRequest corrects a wrongly recorded breed or sex on one Counts Breakdown row.
//
// The slice fields are the row's own identity and are all required in the predicate sense: the
// write matches animals carrying exactly this stage, breed and sex in exactly this pen. Breed may
// be empty, because the census renders a blank breed as its own row and correcting those animals is
// the commonest reason to use this at all.
type CorrectCensusSliceRequest struct {
	ShedID         string `json:"shed_id"`
	PartitionLabel string `json:"partition_label,omitempty"`
	// The row being corrected.
	ManagementStage string `json:"management_stage"`
	Breed           string `json:"breed"`
	Sex             string `json:"sex"`
	// Field is "breed" or "sex". One field per command, so one audit row states one decision.
	Field string `json:"field"`
	// Value is the corrected value, matched case-insensitively against the tenant's vocabulary and
	// echoed back canonicalized.
	Value string `json:"value"`
	// Reason is required on commit and ignored on preview. This write has no approval step and no
	// proof behind it, so the reason is the account of why the register was changed.
	Reason string `json:"reason,omitempty"`
}

// CensusSliceCorrectionPreviewResponse is the whole-slice answer to "what would this change".
type CensusSliceCorrectionPreviewResponse struct {
	ShedID                     string `json:"shed_id"`
	ShedName                   string `json:"shed_name"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	Field                      string `json:"field"`
	CurrentValue               string `json:"current_value"`
	Value                      string `json:"value"`
	TotalLive                  int    `json:"total_live"`
	TraceID                    string `json:"trace_id,omitempty"`
}

// CensusSliceCorrectionResponse is what actually happened. Corrected can be lower than TotalLive
// when another writer moved animals out of the slice between the preview and the commit.
type CensusSliceCorrectionResponse struct {
	ShedID                     string `json:"shed_id"`
	ShedName                   string `json:"shed_name"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display"`
	Field                      string `json:"field"`
	CurrentValue               string `json:"current_value"`
	Value                      string `json:"value"`
	TotalLive                  int    `json:"total_live"`
	Corrected                  int    `json:"corrected"`
	IdempotencyKey             string `json:"idempotency_key,omitempty"`
	TraceID                    string `json:"trace_id,omitempty"`
}
