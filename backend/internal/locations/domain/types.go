package domain

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

type IdempotencyMeta struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Replayed       bool    `json:"replayed"`
	FirstResultID  *string `json:"first_result_id"`
}

type OperationalAttributes struct {
	UsableForCounts      bool    `json:"usable_for_counts"`
	UsableForFeed        bool    `json:"usable_for_feed"`
	UsableForVaccination bool    `json:"usable_for_vaccination"`
	UsableForSOP         bool    `json:"usable_for_sop"`
	IsHolding            bool    `json:"is_holding"`
	IsQuarantine         bool    `json:"is_quarantine"`
	IsICU                bool    `json:"is_icu"`
	DisplayOrder         int     `json:"display_order"`
	Notes                *string `json:"notes"`
}

type LocationSummary struct {
	LocationID       string                `json:"location_id"`
	LocationType     string                `json:"location_type"`
	LocationCode     *string               `json:"location_code"`
	Name             string                `json:"name"`
	ParentLocationID *string               `json:"parent_location_id"`
	ParentName       *string               `json:"parent_name"`
	Status           string                `json:"status"`
	Country          string                `json:"country"`
	StateRegion      *string               `json:"state_region"`
	District         *string               `json:"district"`
	Pincode          *string               `json:"pincode"`
	Lat              *float64              `json:"lat"`
	Lng              *float64              `json:"lng"`
	Timezone         string                `json:"timezone"`
	Operational      OperationalAttributes `json:"operational"`
	CurrentCapacity  *int                  `json:"current_capacity"`
	AliasCount       int                   `json:"alias_count"`
	ChildCount       int                   `json:"child_count"`
	RowVersion       int                   `json:"row_version"`
	CreatedAt        string                `json:"created_at"`
	UpdatedAt        string                `json:"updated_at"`
}

type LocationListResponse struct {
	Items   []LocationSummary `json:"items"`
	TraceID string            `json:"trace_id"`
}

type LocationResponse struct {
	Location LocationSummary `json:"location"`
	TraceID  string          `json:"trace_id"`
}

type LocationMutationResponse struct {
	Location    LocationSummary `json:"location"`
	Idempotency IdempotencyMeta `json:"idempotency"`
	TraceID     string          `json:"trace_id"`
}

type LocationDeleteResponse struct {
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	LocationID   *string         `json:"location_id"`
	Deleted      bool            `json:"deleted"`
	Idempotency  IdempotencyMeta `json:"idempotency"`
	TraceID      string          `json:"trace_id"`
}

type LocationAlias struct {
	AliasID             string  `json:"alias_id"`
	AliasCode           string  `json:"alias_code"`
	CanonicalLocationID string  `json:"canonical_location_id"`
	SourceContext       string  `json:"source_context"`
	Status              string  `json:"status"`
	Notes               *string `json:"notes"`
	RowVersion          int     `json:"row_version"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

type LocationAliasListResponse struct {
	Items   []LocationAlias `json:"items"`
	TraceID string          `json:"trace_id"`
}

type LocationAliasResponse struct {
	Alias       LocationAlias   `json:"alias"`
	Idempotency IdempotencyMeta `json:"idempotency"`
	TraceID     string          `json:"trace_id"`
}

type LocationCapacityRecord struct {
	CapacityRecordID string  `json:"capacity_record_id"`
	LocationID       string  `json:"location_id"`
	CapacityKind     string  `json:"capacity_kind"`
	CapacityValue    int     `json:"capacity_value"`
	EffectiveFrom    string  `json:"effective_from"`
	EffectiveTo      *string `json:"effective_to"`
	Source           string  `json:"source"`
	SourceRef        *string `json:"source_ref"`
	Notes            *string `json:"notes"`
	RowVersion       int     `json:"row_version"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

type LocationCapacityListResponse struct {
	Items   []LocationCapacityRecord `json:"items"`
	TraceID string                   `json:"trace_id"`
}

type LocationCapacityResponse struct {
	Capacity    LocationCapacityRecord `json:"capacity"`
	Idempotency IdempotencyMeta        `json:"idempotency"`
	TraceID     string                 `json:"trace_id"`
}

type LocationReviewItem struct {
	ReviewID              string   `json:"review_id"`
	ReviewType            string   `json:"review_type"`
	Status                string   `json:"status"`
	SourceContext         *string  `json:"source_context"`
	SourceLabel           *string  `json:"source_label"`
	NormalizedSourceLabel *string  `json:"normalized_source_label"`
	CanonicalLocationID   *string  `json:"canonical_location_id"`
	CandidateLocationIDs  []string `json:"candidate_location_ids"`
	EvidenceHash          string   `json:"evidence_hash"`
	ResolutionNotes       *string  `json:"resolution_notes"`
	RowVersion            int      `json:"row_version"`
	CreatedAt             string   `json:"created_at"`
	UpdatedAt             string   `json:"updated_at"`
	ResolvedAt            *string  `json:"resolved_at"`
}

type LocationReviewListResponse struct {
	Items   []LocationReviewItem `json:"items"`
	TraceID string               `json:"trace_id"`
}

type LocationReviewItemResponse struct {
	ReviewItem  LocationReviewItem `json:"review_item"`
	Idempotency IdempotencyMeta    `json:"idempotency"`
	TraceID     string             `json:"trace_id"`
}

type SourceLabelResolution struct {
	SourceContext         string              `json:"source_context"`
	SourceLabel           string              `json:"source_label"`
	NormalizedSourceLabel string              `json:"normalized_source_label"`
	Status                string              `json:"status"`
	Location              *LocationSummary    `json:"location"`
	ReviewItem            *LocationReviewItem `json:"review_item"`
	CandidateLocationIDs  []string            `json:"candidate_location_ids"`
	TraceID               string              `json:"trace_id"`
}

type LocationUsageResponse struct {
	LocationID              string `json:"location_id"`
	GoatsCurrentlyAssigned  int64  `json:"goats_currently_assigned"`
	GoatLocationHistoryRows int64  `json:"goat_location_history_rows"`
	ChildLocations          int64  `json:"child_locations"`
	ActiveAliases           int64  `json:"active_aliases"`
	ActiveRBACGrants        int64  `json:"active_rbac_grants"`
	ActiveSOPDependencies   int64  `json:"active_sop_dependencies"`
	ImportOrSourceRows      int64  `json:"import_or_source_rows"`
	HasBlockingUsage        bool   `json:"has_blocking_usage"`
	TraceID                 string `json:"trace_id"`
}
