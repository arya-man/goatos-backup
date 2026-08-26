package http

import (
	"encoding/json"
	nethttp "net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/domain"
)

// RANDOMIZATION -- the CEO's per-category sampling percentage (maintainer decision 2026-08-26).
//
// Authorization is enforced entirely at the route-permission layer (permissions.VerificationSampling
// on both routes in routes.go), matching every other route in this module: no in-handler role
// check, and no role string anywhere in this file.

type samplingCategoryResponse struct {
	// Category is the registry token. It is carried because the client has to name a row on the
	// write, and it is the ONLY field in this response a renderer may not print: every visible word
	// comes from the label fields beside it.
	Category    string `json:"category"`
	ModuleKey   string `json:"module_key"`
	ModuleLabel string `json:"module_label"`
	PageLabel   string `json:"page_label"`
	// SamplePercent is the share of this category's videos the verifier must watch on the
	// requested business date.
	SamplePercent int `json:"sample_percent"`
	// Waivable is false where the verifier RECORDS the measured quantity rather than checking it,
	// so the percentage is locked at 100. LockedReason is the backend-owned sentence saying why,
	// non-empty exactly when waivable is false; clients render it verbatim and compose no reason of
	// their own (a disabled control with no reason is the defect this prevents).
	Waivable     bool   `json:"waivable"`
	LockedReason string `json:"locked_reason,omitempty"`
	// EffectiveFrom / SetByName / SetAt describe the standing setting, omitted when the CEO has
	// never set one and the category is running at the default.
	EffectiveFrom string `json:"effective_from,omitempty"`
	SetByName     string `json:"set_by_name,omitempty"`
	SetAt         string `json:"set_at,omitempty"`

	// The day, at item grain. NOT disjoint and never to be added together: selected is a subset of
	// captured and reviewed is a subset of selected.
	Captured     int `json:"captured"`
	Selected     int `json:"selected"`
	Reviewed     int `json:"reviewed"`
	AutoAccepted int `json:"auto_accepted"`
	// ProgressPercent is how much of HER SHARE is done: at 40% sampling, all 40% reviewed reads
	// 100. Computed by the backend so the phone, the panel and any report cannot each derive a
	// different completion number for the same day.
	ProgressPercent int `json:"progress_percent"`
}

type samplingResponse struct {
	BusinessDate string                     `json:"business_date"`
	Categories   []samplingCategoryResponse `json:"categories"`
	TraceID      string                     `json:"trace_id,omitempty"`
}

type setSamplingPolicyRequest struct {
	// SamplePercent is 0..100. A value outside that range is REFUSED, never clamped: an author who
	// typed 140 must be told rather than quietly given 100.
	SamplePercent *int `json:"sample_percent"`
}

// GetVerificationSampling serves the Randomization panel for one business day (default today).
func (h *Handler) GetVerificationSampling(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := h.service.SamplingOverview(r.Context(), tenantID(r), strings.TrimSpace(r.URL.Query().Get("business_date")))
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, samplingResponse{
		BusinessDate: result.BusinessDate,
		Categories:   samplingCategoryResponses(result.Categories),
		TraceID:      traceID(r),
	})
}

// SetVerificationSamplingPolicy records one category's percentage, effective from today.
//
// The effective date is the server's and is not accepted from the client: a caller that could name
// its own date could rewrite a day the verifier has already worked.
func (h *Handler) SetVerificationSamplingPolicy(w nethttp.ResponseWriter, r *nethttp.Request) {
	category := strings.TrimSpace(r.PathValue("category"))
	var body setSamplingPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.respondError(w, r, app.BadRequest("invalid_body", "request body must be valid JSON"))
		return
	}
	if body.SamplePercent == nil {
		// ABSENT is not zero. Zero is a real setting ("review none of this category today"), so a
		// missing field must not be read as one -- that would silently switch a module off.
		h.respondError(w, r, app.BadRequest("invalid_sample_percent", "sample_percent is required"))
		return
	}
	updated, err := h.service.SetSamplingPolicy(r.Context(), domain.SetSamplingPolicy{
		TenantID: tenantID(r),
		Category: category,
		Percent:  *body.SamplePercent,
		ActorID:  actorID(r),
		// OPTIONAL here, unlike the verdict route, and derived server-side when absent. A client
		// cannot always name the server's business day, and a key that omitted the day would make
		// "back to 40% next week" look like a replay of last week's 40% and quietly do nothing.
		IdempotencyKey: strings.TrimSpace(r.Header.Get("Idempotency-Key")),
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}
	httpresponse.WriteJSON(w, nethttp.StatusOK, samplingCategoryResponses([]domain.SamplingCategory{updated})[0])
}

func samplingCategoryResponses(rows []domain.SamplingCategory) []samplingCategoryResponse {
	// Always a JSON ARRAY, never null: a panel that has to special-case null for "no categories" is
	// one more place the empty state gets written wrong.
	out := make([]samplingCategoryResponse, len(rows))
	for i, row := range rows {
		out[i] = samplingCategoryResponse{
			Category:        row.Category,
			ModuleKey:       row.ModuleKey,
			ModuleLabel:     row.ModuleLabel,
			PageLabel:       row.PageLabel,
			SamplePercent:   row.SamplePercent,
			Waivable:        row.Waivable,
			LockedReason:    row.LockedReason,
			EffectiveFrom:   row.EffectiveFrom,
			SetByName:       row.SetByName,
			SetAt:           row.SetAt,
			Captured:        row.Stats.Captured,
			Selected:        row.Stats.Selected,
			Reviewed:        row.Stats.Reviewed,
			AutoAccepted:    row.Stats.AutoAccepted,
			ProgressPercent: row.Stats.ProgressPercent(),
		}
	}
	return out
}
