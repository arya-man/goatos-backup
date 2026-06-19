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

type FreshnessEnvelope struct {
	AsOf               *string  `json:"as_of"`
	FreshnessStatus    string   `json:"freshness_status"`
	ServingState       string   `json:"serving_state"`
	Stale              bool     `json:"stale"`
	RebuildRequired    bool     `json:"rebuild_required"`
	SourceWatermark    *string  `json:"source_watermark"`
	UnavailableSources []string `json:"unavailable_sources"`
	ConflictCount      int      `json:"conflict_count"`
	ProjectionVersion  int64    `json:"projection_version"`
	SourceComposition  string   `json:"source_composition"`
}

type ProjectionRow struct {
	Section                      string   `json:"section"`
	Grain                        string   `json:"grain"`
	DimensionKey                 string   `json:"dimension_key"`
	DimensionLabel               string   `json:"dimension_label"`
	MetricKey                    string   `json:"metric_key"`
	Numerator                    *float64 `json:"numerator"`
	Denominator                  *float64 `json:"denominator"`
	DenominatorSourceModule      *string  `json:"denominator_source_module"`
	DenominatorProjectionVersion *int64   `json:"denominator_projection_version"`
	DenominatorSourceWatermark   *string  `json:"denominator_source_watermark"`
	NumeratorSourceComposition   *string  `json:"numerator_source_composition"`
	DenominatorSourceComposition *string  `json:"denominator_source_composition"`
	MixedCompositionExceptionID  *string  `json:"mixed_composition_exception_id"`
	Value                        float64  `json:"value"`
	Unit                         string   `json:"unit"`
	SourceComposition            string   `json:"source_composition"`
}

type DashboardResponse struct {
	Period    string                     `json:"period"`
	Freshness FreshnessEnvelope          `json:"freshness"`
	Summary   []ProjectionRow            `json:"summary"`
	Sections  map[string][]ProjectionRow `json:"sections"`
	TraceID   string                     `json:"trace_id"`
}

type IdempotencyMeta struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Replayed       bool    `json:"replayed"`
	FirstResultID  *string `json:"first_result_id"`
}

type MortalitySyncRunResponse struct {
	SyncRunID                string            `json:"sync_run_id"`
	Mode                     string            `json:"mode"`
	Status                   string            `json:"status"`
	SourceRowsRead           int               `json:"source_rows_read"`
	EventsUpserted           int               `json:"events_upserted"`
	ProjectionRowsWritten    int               `json:"projection_rows_written"`
	RowsSkipped              int               `json:"rows_skipped"`
	DedupCandidateEvents     int               `json:"dedup_candidate_events"`
	UnresolvedLocationLabels int               `json:"unresolved_location_labels"`
	Freshness                FreshnessEnvelope `json:"freshness"`
	Idempotency              IdempotencyMeta   `json:"idempotency"`
	TraceID                  string            `json:"trace_id"`
}
