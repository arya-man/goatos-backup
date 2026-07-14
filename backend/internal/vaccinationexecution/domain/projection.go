package domain

import (
	"errors"
	"time"
)

type ProjectionFreshness struct {
	ProjectionVersion int64     `json:"projectionVersion"`
	ProjectedAt       time.Time `json:"projectedAt"`
	AsOf              time.Time `json:"asOf"`
	Status            string    `json:"status"`
	LagSeconds        int64     `json:"lagSeconds"`
}

// ErrProjectionUnavailable is returned by the read repository when a tenant's shed-wise vaccination
// read model (vaccination_shed_projection_rows/_state) has no serving version yet (never built, or
// rebuilding without a prior serving version). ShedSummary/ShedDetail request paths MUST NOT fall back
// to the canonical compute-on-read replay of obligation_instances/vaccination_completions/
// obligation_status_events — even across the current 5,000-50,000-animal release envelope (query-plan
// proof to the ~500k obligation-row upper bound) that god-CTE is the slowest possible path exactly when
// the system is already degraded, and it only worsens toward the future 1-5M certification bar. Callers
// surface this as an honest, retryable "temporarily unavailable" state (HTTP 503), mirroring
// backend/internal/processintegrity/domain.ErrProjectionUnavailable. See
// docs/decisions/high-scale-dashboard-projections.md, context/execution/vaccexec-readmodel-design.md,
// and docs/decisions/operational-kernel-5k-50k-scale-envelope.md.
var ErrProjectionUnavailable = errors.New("vaccinationexecution: read model projection unavailable")

type ExecutionProjectionRecomputeRequest struct {
	TenantID  string
	AsOf      time.Time
	DueBefore time.Time
}

type ExecutionProjectionRecomputeResult struct {
	TenantID          string
	ProjectionVersion int64
	ProjectedAt       time.Time
	AsOf              time.Time
	Rows              int64
}

type OperationsProjectionRecomputeRequest struct {
	TenantID  string
	AsOf      time.Time
	DueBefore time.Time
}

type OperationsProjectionRecomputeResult struct {
	TenantID          string
	ProjectionVersion int64
	ProjectedAt       time.Time
	AsOf              time.Time
	Rows              int64
}
