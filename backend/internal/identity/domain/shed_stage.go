package domain

// ReclassifyShedStageRequest is the wire body for both the preview and the commit of a whole-pen
// cohort reclassification.
//
// ShedID is the exact physical shed to reclassify. PartitionLabel is legacy compatibility metadata
// and is ignored by the exact-shed path.
type ReclassifyShedStageRequest struct {
	ShedID         string `json:"shed_id"`
	PartitionLabel string `json:"partition_label,omitempty"`
	// ManagementStage is the target cohort tag. It is matched case-insensitively against the
	// tenant's active animal_stage_lookup vocabulary and echoed back canonicalized.
	ManagementStage string `json:"management_stage"`
	// Reason is required on the commit and ignored on the preview. It lands in the audit row and on
	// every animal's stage-change event.
	Reason string `json:"reason,omitempty"`
}

// ReclassifyShedStageBucket is one current-cohort row of the preview. Grain: LIVE ANIMAL. Buckets
// are disjoint (each animal appears in exactly one) and sum to TotalLive.
type ReclassifyShedStageBucket struct {
	ManagementStage string `json:"management_stage"`
	AgeBand         string `json:"age_band"`
	Count           int    `json:"count"`
}

// ReclassifyShedStagePreviewResponse shows what the action would do before it does it.
//
// The counts are WHOLE-SCOPE aggregates over the pen, never a page: CurrentStages is the pen's
// complete present composition, so an operator about to flip a mixed pen sees the mix first.
type ReclassifyShedStagePreviewResponse struct {
	ShedID   string `json:"shed_id"`
	ShedName string `json:"shed_name"`
	// PartitionLabel is null for an undivided shed. The 'whole' sentinel is a matching key and
	// never appears here.
	PartitionLabel *string `json:"partition_label"`
	// OperationalLocationDisplay is composed by the backend (oploc.Display). Clients render it
	// verbatim and must never recompose shed name + partition themselves.
	OperationalLocationDisplay string `json:"operational_location_display"`

	// ManagementStage is the canonical resolved target; AgeBand is the kid/adult band that tag
	// carries, or "" when the tag is deliberately unclassified (animals then keep their band).
	ManagementStage string `json:"management_stage"`
	AgeBand         string `json:"age_band"`

	// Changing + Unchanged == TotalLive. Unchanged animals already carry the target tag and will
	// not be written or evented.
	TotalLive int `json:"total_live"`
	Changing  int `json:"changing"`
	Unchanged int `json:"unchanged"`

	CurrentStages []ReclassifyShedStageBucket `json:"current_stages"`
	TraceID       string                      `json:"trace_id,omitempty"`
}

// ReclassifyShedStageResponse reports what the commit actually wrote.
type ReclassifyShedStageResponse struct {
	ShedID                     string  `json:"shed_id"`
	ShedName                   string  `json:"shed_name"`
	PartitionLabel             *string `json:"partition_label"`
	OperationalLocationDisplay string  `json:"operational_location_display"`

	ManagementStage string `json:"management_stage"`
	AgeBand         string `json:"age_band"`

	// Reclassified counts only the animals whose stage genuinely changed. Unchanged animals were
	// already on the target tag and were deliberately left untouched, so re-running the action does
	// not republish stage-change events for animals nothing happened to.
	TotalLive    int `json:"total_live"`
	Reclassified int `json:"reclassified"`
	Unchanged    int `json:"unchanged"`

	IdempotencyKey string `json:"idempotency_key,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
}
