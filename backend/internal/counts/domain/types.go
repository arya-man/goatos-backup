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
	SourceComposition  string   `json:"source_composition"`
	ConflictCount      int      `json:"conflict_count"`
	ProjectionVersion  int64    `json:"projection_version"`
}

type ProjectionRow struct {
	Section                 string   `json:"section"`
	Grain                   string   `json:"grain"`
	DimensionKey            string   `json:"dimension_key"`
	DimensionLabel          string   `json:"dimension_label"`
	SecondaryDimensionKey   *string  `json:"secondary_dimension_key"`
	SecondaryDimensionLabel *string  `json:"secondary_dimension_label"`
	MetricKey               string   `json:"metric_key"`
	CountValue              *int64   `json:"count_value"`
	NumericValue            *float64 `json:"numeric_value"`
	Unit                    string   `json:"unit"`
	Denominator             *float64 `json:"denominator"`
	SourceComposition       string   `json:"source_composition"`
}

type DashboardResponse struct {
	View              string                     `json:"view"`
	SnapshotDate      *string                    `json:"snapshot_date"`
	SummarySourceDate *string                    `json:"summary_source_date"`
	Freshness         FreshnessEnvelope          `json:"freshness"`
	Summary           []ProjectionRow            `json:"summary"`
	Sections          map[string][]ProjectionRow `json:"sections"`
	TraceID           string                     `json:"trace_id"`
}

type IdempotencyMeta struct {
	IdempotencyKey string  `json:"idempotency_key"`
	Replayed       bool    `json:"replayed"`
	FirstResultID  *string `json:"first_result_id"`
}

type CountsSyncRunResponse struct {
	SyncRunID                string            `json:"sync_run_id"`
	Mode                     string            `json:"mode"`
	Status                   string            `json:"status"`
	SnapshotDate             *string           `json:"snapshot_date"`
	SourceRowsRead           int               `json:"source_rows_read"`
	ProjectionRowsWritten    int               `json:"projection_rows_written"`
	RowsSkipped              int               `json:"rows_skipped"`
	UnresolvedLocationLabels int               `json:"unresolved_location_labels"`
	Freshness                FreshnessEnvelope `json:"freshness"`
	Idempotency              IdempotencyMeta   `json:"idempotency"`
	TraceID                  string            `json:"trace_id"`
}
