package http

// This file mounts the leadership assistant onto the protected mux. There is a
// SINGLE public route — POST /ceo-ai/ask — matching the committed permission
// entry (operationID askCeoAssistant, permission admin_web_bootstrap). Whether
// a request is answered as an SSE token stream or a single JSON envelope is a
// TRANSPORT choice driven by the request body's `stream` field (default true),
// never a separate route/permission. This keeps the authz surface one line and
// the frontend contract (apps/admin-web/app/api/ceo-ai/ask) exact.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

const (
	askPath        = "POST /ceo-ai/ask"
	adminTracePath = "GET /ceo-ai/admin/trace/{request_id}"
)

// Router pairs the two transport handlers behind the single ask route and,
// optionally, the admin-only step-trace debug route.
type Router struct {
	handler    *Handler           // JSON (non-streaming) transport
	stream     *StreamHandler     // SSE (streaming) transport
	adminTrace *AdminTraceHandler // admin-only GET /ceo-ai/admin/trace/{request_id}
}

// NewRouter builds the ask router. Both transports drive the SAME orchestrator.
func NewRouter(handler *Handler, stream *StreamHandler) *Router {
	return &Router{handler: handler, stream: stream}
}

// WithAdminTrace attaches the admin-only step-trace handler so Register also
// mounts GET /ceo-ai/admin/trace/{request_id}. A nil handler leaves the route
// unmounted (the endpoint stays absent, never open).
func (rt *Router) WithAdminTrace(h *AdminTraceHandler) *Router {
	rt.adminTrace = h
	return rt
}

// Register mounts POST /ceo-ai/ask. It peeks the request body once to decide
// streaming vs JSON, then hands a rewound body to the chosen transport so each
// transport still owns its own decode/validation. Streaming is the default.
func (rt *Router) Register(mux *http.ServeMux) {
	mux.HandleFunc(askPath, func(w http.ResponseWriter, r *http.Request) {
		stream := true // default UX is progressive SSE
		if rt.stream == nil {
			stream = false
		}

		// Buffer the (small, capped) body so we can inspect `stream` and still
		// replay it to the delegate. The delegates re-apply MaxBytesReader, so
		// this cap is a cheap pre-guard, not the authoritative limit.
		body, err := io.ReadAll(io.LimitReader(r.Body, streamMaxBodyBytes+1))
		_ = r.Body.Close()
		if err == nil && len(body) <= streamMaxBodyBytes {
			var peek struct {
				Stream *bool `json:"stream"`
			}
			if json.Unmarshal(body, &peek) == nil && peek.Stream != nil {
				stream = *peek.Stream && rt.stream != nil
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		if stream {
			rt.stream.Stream(w, r)
			return
		}
		rt.handler.Ask(w, r)
	})

	// Admin-only internal step-trace debug surface. The handler itself is the
	// hard gate (RoleCEOInternal + session tenant scope); mounting it here makes
	// it reachable on the live server instead of only in unit tests.
	if rt.adminTrace != nil {
		mux.HandleFunc(adminTracePath, rt.adminTrace.Trace)
	}
}
