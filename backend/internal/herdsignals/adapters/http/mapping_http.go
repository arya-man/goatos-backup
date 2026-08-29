package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/vgoats/goatos/backend/internal/herdsignals/domain"
	"github.com/vgoats/goatos/backend/internal/platform/httpresponse"
)

// The mapping WRITE endpoints. Until these existed the Tag Mapping tab rendered three disabled
// buttons -- "Map selected BLE tag", "Mark as smart tag", "Replace smart tag" -- because the
// module had no write path at all: five routes, one ingest and four reads.
//
// The three verbs are MAP, REPLACE and UNMAP. There is deliberately no "mark as smart tag"
// endpoint: every row on the Tag Mapping screen is ALREADY a smart tag -- it is listed precisely
// because the gateway is receiving its advertisements -- so asking a user to declare one as such
// asserts nothing. smart_tag_capable is an internal consequence of binding.
//
// These are gated by permissions.HerdSignalsMap, NOT by the read permission: deciding which
// animal a tag belongs to is a different authority from looking at the dashboard, and it is the
// decision every animal-attributed number downstream depends on.

// maxMappingBodyBytes bounds every mapping request body. These payloads are a handful of short
// fields; the same reasoning as the ingest cap applies (an unbounded body decoded inside a
// transaction that takes row locks).
const maxMappingBodyBytes = 16 << 10 // 16 KiB

// decodeMappingBody applies the module's standard request discipline: bound the body BEFORE
// decoding, then decode strictly (unknown fields are an error, never silently dropped -- a
// client that misspells goat_id must be told, not have the field ignored).
func decodeMappingBody(w http.ResponseWriter, r *http.Request, dst interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxMappingBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}

// writeMappingError maps a write failure to the right status. A mapping refusal is a
// FIRST-CLASS ANSWER, not a server error: "that tag already belongs to another animal" and "this
// animal already carries a live smart tag" are exactly the states these endpoints exist to
// prevent, and the operator must see them as such.
func (h *Handler) writeMappingError(w http.ResponseWriter, r *http.Request, op string, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "invalid_request", "message": err.Error()}, err)
	case errors.Is(err, domain.ErrMappingNotFound):
		httpresponse.WriteError(w, r, h.log, http.StatusNotFound,
			map[string]interface{}{"code": "not_found", "message": err.Error()}, err)
	case errors.Is(err, domain.ErrMappingConflict):
		httpresponse.WriteError(w, r, h.log, http.StatusConflict,
			map[string]interface{}{"code": "mapping_conflict", "message": err.Error()}, err)
	default:
		h.log.Error(op+"_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": op + "_failed", "message": "failed to update tag mapping"}, err)
	}
}

func (h *Handler) mappingActor(w http.ResponseWriter, r *http.Request) (domain.Actor, bool) {
	actor := domain.Actor{TenantID: tenantID(r), UserID: actorID(r)}
	if actor.TenantID == "" || actor.UserID == "" {
		httpresponse.WriteError(w, r, h.log, http.StatusUnauthorized,
			map[string]interface{}{"code": "unauthorized", "message": "authentication required"}, nil)
		return domain.Actor{}, false
	}
	return actor, true
}

// BindTagMapping handles POST /herd-signals/tag-mappings.
func (h *Handler) BindTagMapping(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.mappingActor(w, r)
	if !ok {
		return
	}
	var req domain.BindTagMappingRequest
	if err := decodeMappingBody(w, r, &req); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	resp, err := h.service.BindTagMapping(r.Context(), actor, req)
	if err != nil {
		h.writeMappingError(w, r, "bind_tag_mapping", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// ReplaceTagMapping handles POST /herd-signals/tag-mappings/replace.
func (h *Handler) ReplaceTagMapping(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.mappingActor(w, r)
	if !ok {
		return
	}
	var req domain.ReplaceTagMappingRequest
	if err := decodeMappingBody(w, r, &req); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	resp, err := h.service.ReplaceTagMapping(r.Context(), actor, req)
	if err != nil {
		h.writeMappingError(w, r, "replace_tag_mapping", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// UnmapTagMapping handles POST /herd-signals/tag-mappings/unmap.
func (h *Handler) UnmapTagMapping(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.mappingActor(w, r)
	if !ok {
		return
	}
	var req domain.UnmapTagMappingRequest
	if err := decodeMappingBody(w, r, &req); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	resp, err := h.service.UnmapTagMapping(r.Context(), actor, req)
	if err != nil {
		h.writeMappingError(w, r, "unmap_tag_mapping", err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// RecordGatewayHeartbeat handles POST /herd-signals/heartbeats.
func (h *Handler) RecordGatewayHeartbeat(w http.ResponseWriter, r *http.Request) {
	actor, ok := h.mappingActor(w, r)
	if !ok {
		return
	}
	var req domain.GatewayHeartbeatRequest
	if err := decodeMappingBody(w, r, &req); err != nil {
		h.writeDecodeError(w, r, err)
		return
	}
	resp, err := h.service.RecordGatewayHeartbeat(r.Context(), actor, req)
	if err != nil {
		if errors.Is(err, domain.ErrValidation) {
			httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
				map[string]interface{}{"code": "invalid_request", "message": err.Error()}, err)
			return
		}
		h.log.Error("record_gateway_heartbeat_failed", "error", err.Error())
		httpresponse.WriteError(w, r, h.log, http.StatusInternalServerError,
			map[string]interface{}{"code": "heartbeat_failed", "message": "failed to record gateway heartbeat"}, err)
		return
	}
	httpresponse.WriteJSON(w, http.StatusOK, resp)
}

// writeDecodeError distinguishes an oversized body from malformed JSON, so a caller that hit the
// cap is told which limit it hit rather than being told its perfectly valid JSON was invalid.
func (h *Handler) writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
			map[string]interface{}{"code": "request_too_large", "message": fmt.Sprintf("request body exceeds %d bytes", maxMappingBodyBytes)}, err)
		return
	}
	httpresponse.WriteError(w, r, h.log, http.StatusBadRequest,
		map[string]interface{}{"code": "invalid_request", "message": "invalid request body"}, err)
}
