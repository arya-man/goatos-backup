package http

import (
	"net/http"
	"strconv"

	"github.com/vgoats/goatos/backend/internal/weighing/domain"
)

// Fasting (feed & water removal) precondition routes (maintainer decision
// 2026-09-03; product rule and clocks in weighing/domain/fasting.go).

// ListMyFastingShedCards serves the removal operator's OWN cards — ONE CARD
// PER SHED (maintainer correction #2, 2026-09-03). The 20:00 IST visibility
// window is applied server-side; before that instant the evening's cards
// simply are not in the page.
func (h *Handler) ListMyFastingShedCards(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	page, err := h.service.ListMyFastingShedCards(r.Context(), actor(r), r.URL.Query().Get("cursor"), limit)
	h.respond(w, r, map[string]any{"fasting_shed_cards": page.Items, "next_cursor": page.NextCursor, "trace_id": traceID(r)}, err)
}

type submitFastingShedRequest struct {
	FeedProofRef  string `json:"feed_proof_ref"`
	WaterProofRef string `json:"water_proof_ref"`
}

// SubmitFastingShed records ONE shed's two removal videos. The round runs
// tomorrow only when EVERY shed's card is submitted before midnight IST; the
// videos still go to the verifier afterwards, one item per shed.
func (h *Handler) SubmitFastingShed(w http.ResponseWriter, r *http.Request) {
	var req submitFastingShedRequest
	if !h.decode(w, r, &req) {
		return
	}
	card, err := h.service.SubmitFastingShed(r.Context(), actor(r), domain.SubmitFastingShed{
		FastingTaskID:  r.PathValue("fasting_task_id"),
		CampaignShedID: r.PathValue("campaign_shed_id"),
		FeedProofRef:   req.FeedProofRef,
		WaterProofRef:  req.WaterProofRef,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	h.respond(w, r, map[string]any{"fasting_shed_card": card, "trace_id": traceID(r)}, err)
}
