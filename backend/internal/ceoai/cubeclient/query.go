package cubeclient

// Query is a typed Cube load query. It mirrors the subset of the Cube REST
// `/cubejs-api/v1/load` query contract the leadership assistant needs:
// governed metrics (measures) sliced by dimensions and time, with filters.
//
// The tenant filter is NEVER part of this struct. Tenant scope is bound
// server-side from the session and injected by Cube's queryRewrite (see
// analytics/cube/cube.js). Callers pass the tenant separately to Load.
type Query struct {
	// Measures are governed metric members, e.g. "kpi_vaccination.vaccination_overdue".
	Measures []string `json:"measures,omitempty"`
	// Dimensions slice the metric, e.g. "kpi_vaccination.park_label".
	Dimensions []string `json:"dimensions,omitempty"`
	// TimeDimensions bucket by an IST business-day time member.
	TimeDimensions []TimeDimension `json:"timeDimensions,omitempty"`
	// Filters are non-tenant business filters (status, park, etc.).
	Filters []Filter `json:"filters,omitempty"`
	// Order maps a member to "asc" | "desc".
	Order map[string]string `json:"order,omitempty"`
	// Limit bounds the returned rows. Load enforces a hard ceiling.
	Limit int `json:"limit,omitempty"`
	// Offset is intentionally unsupported for hot paths (keyset preferred);
	// exposed only for small bounded reads.
	Offset int `json:"offset,omitempty"`
}

// TimeDimension buckets a metric over an IST business-day member.
type TimeDimension struct {
	Dimension   string   `json:"dimension"`
	Granularity string   `json:"granularity,omitempty"` // day|week|month
	DateRange   []string `json:"dateRange,omitempty"`   // ["2026-07-01","2026-07-22"]
}

// Filter is a Cube member filter. Only business filters belong here; the tenant
// filter is applied by Cube server-side.
type Filter struct {
	Member   string   `json:"member"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

// Result is the decoded, typed response of a Load call.
type Result struct {
	// Rows are the metric rows; keys are fully-qualified members
	// (e.g. "kpi_animals.active_animal_count"). Values are raw JSON scalars
	// (Cube returns numeric measures as strings).
	Rows []map[string]any `json:"data"`
	// Annotation carries Cube's member metadata (titles, types); optional.
	Annotation map[string]any `json:"annotation,omitempty"`
}
