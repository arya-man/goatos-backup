package http

import (
	"context"
	nethttp "net/http"
	"strings"

	"github.com/vgoats/goatos/backend/internal/permissions"
	"github.com/vgoats/goatos/backend/internal/platform/httpmiddleware"
	"github.com/vgoats/goatos/backend/internal/sop/app"
	"github.com/vgoats/goatos/backend/internal/sop/domain"
)

// HRMS SOP authoring (maintainer answer 2026-09-30): HR edits and publishes the HRMS SOP -- the
// violation types, their fines and the enquiry questions -- and NO other SOP. The route table
// admits permissions.HRMSSOPAuthor beside the full sop.* permission on the read / save / publish
// routes; THIS file is the other half: a caller admitted ONLY by HRMSSOPAuthor is narrowed to
// the "hrms." codes on every one of those routes. A non-HRMS SOP answers not-found rather than
// forbidden, so its existence is not disclosed either.

// hrmsOnly reports a caller who holds HRMSSOPAuthor but not the full permission the route
// otherwise needs. The permission source is the person's resolved rows when the per-person
// cutover decided the request, else the roles on their grants -- the same source the route
// gate used, so the two can never disagree.
func hrmsOnly(ctx context.Context, full string) bool {
	return !callerHolds(ctx, full) && callerHolds(ctx, permissions.HRMSSOPAuthor)
}

func callerHolds(ctx context.Context, perm string) bool {
	if perms, ok := httpmiddleware.PersonPermissionsFromContext(ctx); ok {
		for _, p := range perms {
			if p == perm {
				return true
			}
		}
		return false
	}
	for _, grant := range httpmiddleware.AuthGrantsFromContext(ctx) {
		if permissions.RoleHasPermission(grant.Role, perm) {
			return true
		}
	}
	return false
}

// hrmsListPrefix narrows a list request's code prefix to the HRMS SOPs for an HRMS-only caller.
func hrmsListPrefix(ctx context.Context, requested string) string {
	if !hrmsOnly(ctx, permissions.SOPRead) {
		return requested
	}
	if strings.HasPrefix(requested, domain.HRMSCodePrefix) {
		return requested
	}
	return domain.HRMSCodePrefix
}

// guardHRMSOnly answers false (and writes not-found) when an HRMS-only caller names a SOP that is
// not an HRMS SOP.
func (h *Handler) guardHRMSOnly(w nethttp.ResponseWriter, r *nethttp.Request, full string) bool {
	if !hrmsOnly(r.Context(), full) {
		return true
	}
	sop, err := h.service.GetSOP(r.Context(), tenantID(r), r.PathValue("sop_id"), traceID(r))
	if err != nil {
		h.respond(w, r, nil, err)
		return false
	}
	if !strings.HasPrefix(sop.SOP.Code, domain.HRMSCodePrefix) {
		h.respond(w, r, nil, app.NotFound("not_found", "SOP not found"))
		return false
	}
	return true
}
