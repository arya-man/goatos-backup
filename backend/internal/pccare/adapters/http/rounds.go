package http

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/pccare/app"
	"github.com/vgoats/goatos/backend/internal/pccare/domain"
	"github.com/vgoats/goatos/backend/internal/pccare/ports"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// createRoundRequest is the round create body (maintainer decision 2026-09-05): the same
// fields as a task create, with the single pen replaced by a pen LIST.
type createRoundRequest struct {
	Category string `json:"category"`
	ParkID   string `json:"park_id"`
	// Pens are the pens this round covers, identity only — the display label is composed
	// server-side from the pen catalog so a client cannot name a pen the farm does not use.
	Pens                []createRoundPen `json:"pens"`
	PlannedBusinessDate string           `json:"planned_business_date"`
	AssigneeUserIDs     []string         `json:"assignee_user_ids"`
	// FeedRemovalRequired (deworming only) also plans the evening-before feed & water
	// removal: ONE card for the round, one feed video + one water video PER PEN. On any
	// other category these fields are rejected, never dropped.
	FeedRemovalRequired    bool     `json:"feed_removal_required"`
	RemovalOperatorUserIDs []string `json:"removal_operator_user_ids"`
}

type createRoundPen struct {
	ShedID         string `json:"shed_id"`
	PartitionLabel string `json:"partition_label"`
}

// roundDTO is one round on the wire. Status and every pen label are BACKEND-OWNED: the
// client renders them verbatim and never re-derives a round's status from its pen list.
type roundDTO struct {
	RoundID             string    `json:"round_id"`
	Category            string    `json:"category"`
	CategoryLabel       string    `json:"category_label"`
	ParkID              string    `json:"park_id"`
	ParkName            string    `json:"park_name"`
	PlannedBusinessDate string    `json:"planned_business_date"`
	Status              string    `json:"status"`
	PenCount            int32     `json:"pen_count"`
	Pens                []taskDTO `json:"pens"`
	// RemovalTaskID names the round's feed & water removal card when one gates it; the
	// phone opens that card from the round. Empty when the round needs no removal.
	RemovalTaskID string `json:"removal_task_id,omitempty"`
	RemovalStatus string `json:"removal_status,omitempty"`
}

// roundCardDTO is ONE row of the planner's round-grained list. Every label is backend-owned
// and rendered verbatim; the client never re-derives a card's status or composes a pen name.
type roundCardDTO struct {
	// CardKey is the client's stable list key: the round id, or the task id for a legacy
	// round-less task.
	CardKey string `json:"card_key"`
	RoundID string `json:"round_id,omitempty"`
	// SingleTaskID is set only on a round-less card, so the phone opens that pen's task
	// directly instead of drilling into a round of one.
	SingleTaskID        string   `json:"single_task_id,omitempty"`
	Category            string   `json:"category"`
	CategoryLabel       string   `json:"category_label"`
	ParkID              string   `json:"park_id"`
	ParkName            string   `json:"park_name"`
	PlannedBusinessDate string   `json:"planned_business_date"`
	DueBusinessDate     string   `json:"due_business_date"`
	Status              string   `json:"status"`
	PenCount            int32    `json:"pen_count"`
	PenLabels           []string `json:"pen_labels"`
	AssigneeNames       []string `json:"assignee_names"`
	AnimalCount         int32    `json:"animal_count"`
	RemovalTaskID       string   `json:"removal_task_id,omitempty"`
	RemovalStatus       string   `json:"removal_status,omitempty"`
}

type roundCardPageDTO struct {
	Items      []roundCardDTO `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// GetRoundCards serves the planner's list at ROUND grain.
func (h *Handler) GetRoundCards(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	page, err := h.service.ListRoundCards(
		r.Context(), a,
		strings.TrimSpace(r.URL.Query().Get("park_id")),
		strings.TrimSpace(r.URL.Query().Get("category")),
		strings.TrimSpace(r.URL.Query().Get("date")),
		strings.TrimSpace(r.URL.Query().Get("filter")),
		strings.TrimSpace(r.URL.Query().Get("cursor")),
		intQuery(r, "limit", 25),
		r.URL.Query().Get("current_or_carry") == "true",
	)
	if err != nil {
		h.writeServiceError(w, r, "pc care round cards", err)
		return
	}
	resp := roundCardPageDTO{Items: make([]roundCardDTO, 0, len(page.Cards)), NextCursor: page.NextCursor}
	for _, c := range page.Cards {
		resp.Items = append(resp.Items, roundCardDTO{
			CardKey: c.CardKey, RoundID: c.RoundID, SingleTaskID: c.SingleTaskID,
			Category: c.Category, CategoryLabel: domain.CategoryLabel(c.Category),
			ParkID: c.ParkID, ParkName: c.ParkName,
			PlannedBusinessDate: c.PlannedBusinessDate, DueBusinessDate: c.DueBusinessDate,
			Status: c.Status, PenCount: c.PenCount,
			PenLabels: c.PenLabels, AssigneeNames: c.AssigneeNames, AnimalCount: c.AnimalCount,
			RemovalTaskID: c.RemovalTaskID, RemovalStatus: c.RemovalStatus,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) PostCreateRound(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r, h)
	if !ok {
		return
	}
	var body createRoundRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	pens := make([]app.RoundPenInput, 0, len(body.Pens))
	for _, pen := range body.Pens {
		pens = append(pens, app.RoundPenInput{ShedID: pen.ShedID, PartitionLabel: pen.PartitionLabel})
	}
	round, err := h.service.CreateRound(r.Context(), a, app.CreateRoundInput{
		Category:               body.Category,
		ParkID:                 body.ParkID,
		Pens:                   pens,
		PlannedBusinessDate:    body.PlannedBusinessDate,
		AssigneeUserIDs:        body.AssigneeUserIDs,
		FeedRemovalRequired:    body.FeedRemovalRequired,
		RemovalOperatorUserIDs: body.RemovalOperatorUserIDs,
		IdempotencyKey:         key,
		ActorID:                a.UserID,
		ActorType:              "human",
		TraceID:                httpmiddleware.TraceIDFromContext(r.Context()),
	})
	if err != nil {
		h.writeServiceError(w, r, "pc care create round", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, roundDTOFrom(round))
}

func (h *Handler) GetRound(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	round, err := h.service.GetRound(r.Context(), a, r.PathValue("round_id"))
	if err != nil {
		h.writeServiceError(w, r, "pc care get round", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, roundDTOFrom(round))
}

func roundDTOFrom(round ports.RoundRow) roundDTO {
	pens := make([]taskDTO, 0, len(round.Pens))
	for _, pen := range round.Pens {
		pens = append(pens, taskDTOFrom(pen))
	}
	return roundDTO{
		RoundID:             round.RoundID,
		Category:            round.Category,
		CategoryLabel:       domain.CategoryLabel(round.Category),
		ParkID:              round.ParkID,
		ParkName:            round.ParkName,
		PlannedBusinessDate: round.PlannedBusinessDate,
		Status:              round.Status,
		PenCount:            round.PenCount,
		Pens:                pens,
		RemovalTaskID:       round.RemovalTaskID,
		RemovalStatus:       round.RemovalStatus,
	}
}

// removalPenDTO is one pen's slot pair on a round-grain removal card. Labels are
// backend-owned; the phone renders the pen label verbatim and never composes its own.
type removalPenDTO struct {
	RemovalPenID  string `json:"removal_pen_id"`
	GatedTaskID   string `json:"gated_task_id"`
	PenLabel      string `json:"pen_label"`
	FeedProofRef  string `json:"feed_proof_ref,omitempty"`
	WaterProofRef string `json:"water_proof_ref,omitempty"`
	Status        string `json:"status"`
	ReworkReason  string `json:"rework_reason,omitempty"`
	RowVersion    int32  `json:"row_version"`
}

type removalPensResponse struct {
	Pens []removalPenDTO `json:"pens"`
}

type registerRemovalPenProofRequest struct {
	// GatedTaskID names the pen by the work task it gates — the same identity the round
	// create wrote, so a client cannot address a pen outside the gated round.
	GatedTaskID string `json:"gated_task_id"`
	ProofRef    string `json:"proof_ref"`
}

// GetRemovalPens serves the operator's pen-by-pen slot list for a removal card: which pens
// still owe a feed or water video, and which have been shot.
func (h *Handler) GetRemovalPens(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	pens, err := h.service.RemovalPenProofs(r.Context(), a, r.PathValue("task_id"))
	if err != nil {
		h.writeServiceError(w, r, "pc care removal pens", err)
		return
	}
	out := make([]removalPenDTO, 0, len(pens))
	for _, pen := range pens {
		out = append(out, removalPenDTO{
			RemovalPenID: pen.RemovalPenID, GatedTaskID: pen.GatedTaskID, PenLabel: pen.PenLabel,
			FeedProofRef: pen.FeedProofRef, WaterProofRef: pen.WaterProofRef,
			Status: pen.Status, ReworkReason: pen.ReworkReason, RowVersion: pen.RowVersion,
		})
	}
	httpresponse.WriteJSON(w, http.StatusOK, removalPensResponse{Pens: out})
}

// PutRemovalPenProof stores ONE pen's feed or water video on a removal card.
func (h *Handler) PutRemovalPenProof(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requireAuthed(w, r)
	if !ok {
		return
	}
	key, ok := idempotencyKey(w, r, h)
	if !ok {
		return
	}
	var body registerRemovalPenProofRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest, "request body must be valid JSON", nil)
		return
	}
	if err := h.service.RegisterRemovalPenProof(r.Context(), a, app.RegisterRemovalPenProofInput{
		RemovalTaskID:  r.PathValue("task_id"),
		GatedTaskID:    body.GatedTaskID,
		SlotKey:        r.PathValue("slot"),
		ProofRef:       body.ProofRef,
		IdempotencyKey: key,
		ActorID:        a.UserID,
		ActorType:      "operator",
		TraceID:        httpmiddleware.TraceIDFromContext(r.Context()),
	}); err != nil {
		h.writeServiceError(w, r, "pc care removal pen proof", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}
