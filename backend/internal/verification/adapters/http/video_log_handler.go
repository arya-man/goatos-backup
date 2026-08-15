package http

import (
	nethttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
	"github.com/vgoats/goatos/backend/internal/platform/oploc"
	"github.com/vgoats/goatos/backend/internal/verification/app"
	"github.com/vgoats/goatos/backend/internal/verification/ports"
)

type videoLogProofResponse struct {
	ProofID string `json:"proof_id"`
	Ordinal int    `json:"ordinal"`
	Label   string `json:"label"`
	// MediaKind is "video" or "photo". Feed distribution is the one category that mixes them, so a
	// client must not assume every proof on this log is a video.
	MediaKind string `json:"media_kind"`
	// UploadedAt is when the server accepted the bytes. Absent for a proof that was registered but
	// never finished uploading -- a real state the log shows rather than hides.
	UploadedAt *string `json:"uploaded_at,omitempty"`
	// RegisteredAt is when the client took an upload URL. On mobile the outbox registers at capture
	// and retries the upload later, so the gap between these two is real upload lag.
	RegisteredAt string `json:"registered_at"`
}

type videoLogRowResponse struct {
	ItemID        string `json:"item_id"`
	Module        string `json:"module"`
	ModuleLabel   string `json:"module_label,omitempty"`
	NavModule     string `json:"nav_module,omitempty"`
	Category      string `json:"category"`
	CategoryLabel string `json:"category_label,omitempty"`
	// Grain is "animal" or "shed" -- the producer's own declaration of what its work was about.
	Grain string `json:"grain"`
	// The row's own location. Redundant when one shed's detail is on screen, and essential in the
	// whole-day export, where a row otherwise cannot say which shed its video came from.
	ShedID                     string `json:"shed_id,omitempty"`
	ShedLabel                  string `json:"shed_label,omitempty"`
	PartitionLabel             string `json:"partition_label,omitempty"`
	OperationalLocationDisplay string `json:"operational_location_display,omitempty"`
	ParkLabel                  string `json:"park_label,omitempty"`
	// SubjectLabel is legitimately EMPTY for feed transport, whose producer writes no label because
	// the shed header already names it. A client must render nothing there, not a placeholder.
	SubjectLabel string                  `json:"subject_label,omitempty"`
	Status       string                  `json:"status"`
	OperatorName string                  `json:"operator_name,omitempty"`
	CapturedAt   string                  `json:"captured_at"`
	Proofs       []videoLogProofResponse `json:"proofs"`
}

type videoLogShedResponse struct {
	ShedID    string `json:"shed_id"`
	ShedLabel string `json:"shed_label,omitempty"`
	// ShedKey is the composite "<shed_uuid>#<normalized partition>" the shed filter uses. It is
	// what a client sends back as shed_id to open this location's detail -- a bare shed uuid cannot
	// tell Castro - 1 from Castro - 2.
	ShedKey                    string   `json:"shed_key"`
	PartitionLabel             string   `json:"partition_label,omitempty"`
	OperationalLocationDisplay string   `json:"operational_location_display"`
	ParkID                     string   `json:"park_id,omitempty"`
	ParkLabel                  string   `json:"park_label,omitempty"`
	ProofCount                 int      `json:"proof_count"`
	ItemCount                  int      `json:"item_count"`
	AwaitingUploadCount        int      `json:"awaiting_upload_count"`
	FirstUploadAt              *string  `json:"first_upload_at,omitempty"`
	LastUploadAt               *string  `json:"last_upload_at,omitempty"`
	Modules                    []string `json:"modules"`
}

type videoLogResponse struct {
	BusinessDate   string                 `json:"business_date"`
	Sheds          []videoLogShedResponse `json:"sheds"`
	SelectedShedID string                 `json:"selected_shed_id,omitempty"`
	Rows           []videoLogRowResponse  `json:"rows"`
	RowsTruncated  bool                   `json:"rows_truncated"`
	TraceID        string                 `json:"trace_id"`
}

// GetVideoLog serves the VIDEO LOG: for ONE business day, per shed, the time each proof was
// uploaded (maintainer decision 2026-08-14).
//
// Authorization is enforced at the route-permission layer (permissions.VerificationEvidenceTimeline
// on this route in routes.go), matching every other route in this module. There is no in-handler
// role check, and deliberately no category narrowing: the log is CROSS-MODULE for every caller who
// holds the capability, verifier included, because the question it answers is "what arrived from
// this shed today" and a shed's day is feed AND vaccination AND a death together. That is the one
// place this surface departs from the verifier's queue scoping -- it grants no verdict authority
// and reshapes no queue. See permissions.VerificationEvidenceTimeline.
//
// The PARK clamp still applies, from the same helper the queue read uses: a caller whose grant is
// park-scoped sees only their parks.
func (h *Handler) GetVideoLog(w nethttp.ResponseWriter, r *nethttp.Request) {
	q := r.URL.Query()
	restricted, parkIDs := verificationParkScope(r, permissions.VerificationEvidenceTimeline)

	limit := 0
	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			h.respondError(w, r, app.BadRequest("invalid_limit", "limit must be a positive integer"))
			return
		}
		limit = parsed
	}

	result, err := h.service.VideoLog(r.Context(), ports.VideoLogParams{
		TenantID:        tenantID(r),
		BusinessDate:    strings.TrimSpace(q.Get("business_date")),
		ScopeRestricted: restricted,
		ParkIDs:         parkIDs,
		ParkID:          strings.TrimSpace(q.Get("park_id")),
		ShedID:          strings.TrimSpace(q.Get("shed_id")),
		// all_sheds is the CSV export's whole-day read. Only "true" enables it; anything else
		// (including a typo) falls back to the ordinary bounded behaviour rather than silently
		// widening the read.
		AllSheds: strings.EqualFold(strings.TrimSpace(q.Get("all_sheds")), "true"),
		Limit:    limit,
	})
	if err != nil {
		h.respondError(w, r, err)
		return
	}

	// Always JSON ARRAYS, never null: a renderer that has to special-case null for "nothing
	// arrived" is one more place the empty state gets written wrong.
	sheds := make([]videoLogShedResponse, len(result.Sheds))
	for i, shed := range result.Sheds {
		loc := oploc.OperationalLocation{
			ShedID:         shed.ShedID,
			ShedName:       shed.ShedLabel,
			PartitionLabel: shed.PartitionLabel,
		}
		modules := shed.Modules
		if modules == nil {
			modules = []string{}
		}
		sheds[i] = videoLogShedResponse{
			ShedID:    shed.ShedID,
			ShedLabel: shed.ShedLabel,
			ShedKey:   loc.Key(),
			// The raw partition is carried alongside the composed display, per the operational
			// location convention -- but the DISPLAY is the only one a screen may render.
			PartitionLabel:             shed.PartitionLabel,
			OperationalLocationDisplay: loc.Display(),
			ParkID:                     shed.ParkID,
			ParkLabel:                  shed.ParkLabel,
			ProofCount:                 shed.ProofCount,
			ItemCount:                  shed.ItemCount,
			AwaitingUploadCount:        shed.AwaitingUploadCount,
			FirstUploadAt:              formatOptionalInstant(shed.FirstUploadAt),
			LastUploadAt:               formatOptionalInstant(shed.LastUploadAt),
			Modules:                    modules,
		}
	}

	rows := make([]videoLogRowResponse, len(result.Rows))
	for i, row := range result.Rows {
		proofs := make([]videoLogProofResponse, len(row.Proofs))
		for j, proof := range row.Proofs {
			proofs[j] = videoLogProofResponse{
				ProofID:      proof.ProofID,
				Ordinal:      proof.Ordinal,
				Label:        proof.Label,
				MediaKind:    proof.MediaKind,
				UploadedAt:   formatOptionalInstant(proof.UploadedAt),
				RegisteredAt: proof.RegisteredAt.UTC().Format(time.RFC3339),
			}
		}
		loc := oploc.OperationalLocation{
			ShedID:         row.ShedID,
			ShedName:       row.ShedLabel,
			PartitionLabel: row.PartitionLabel,
		}
		rows[i] = videoLogRowResponse{
			ItemID:                     row.ItemID,
			ShedID:                     row.ShedID,
			ShedLabel:                  row.ShedLabel,
			PartitionLabel:             row.PartitionLabel,
			OperationalLocationDisplay: loc.Display(),
			// The park DISAMBIGUATES the location in the whole-day export: shed names repeat across
			// parks, so a file without it renders two different sheds identically (the OL-1 case).
			ParkLabel:     row.ParkLabel,
			Module:        row.Module,
			ModuleLabel:   row.ModuleLabel,
			NavModule:     row.NavModule,
			Category:      row.Category,
			CategoryLabel: row.CategoryLabel,
			Grain:         string(row.Grain),
			SubjectLabel:  row.SubjectLabel,
			Status:        row.Status,
			OperatorName:  row.OperatorName,
			CapturedAt:    row.CapturedAt.UTC().Format(time.RFC3339),
			Proofs:        proofs,
		}
	}

	httpresponse.WriteJSON(w, nethttp.StatusOK, videoLogResponse{
		BusinessDate:   result.BusinessDate,
		Sheds:          sheds,
		SelectedShedID: result.SelectedShedID,
		Rows:           rows,
		RowsTruncated:  result.RowsTruncated,
		TraceID:        traceID(r),
	})
}

// formatOptionalInstant renders a nullable instant as RFC3339, keeping absent absent. A zero-value
// timestamp must never render as "0001-01-01" on a screen that means it as "has not arrived yet".
func formatOptionalInstant(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := t.UTC().Format(time.RFC3339)
	return &formatted
}
