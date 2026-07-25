import { NextResponse } from "next/server";
import { getServerConfig } from "@/lib/api/server";

// Shared thin-proxy plumbing for every /api/ceo-ai/* route.
//
// The leadership assistant (streaming answers, conversation threads, starters) is
// owned by the Mesha backend (`/ceo-ai/*`), the single authority for leadership +
// tenant scope, planning, tool routing, execution, validation, and audit. admin-web
// is a thin authenticated proxy: it attaches the session bearer + tenant, forwards
// the request, and streams the response back untouched. It re-implements NO routing,
// reads NO business data, and does NOT gate on a display-name regex — the backend
// `ceo_internal` permission is the security boundary (a non-leadership session gets
// 403 from the backend, which the UI surfaces honestly).

const NO_STORE = { "Cache-Control": "no-store" } as const;

type ForwardInit = {
  method: string;
  body?: string;
  stream?: boolean;
  signal?: AbortSignal;
};

type BackendCall =
  | { ok: true; response: Response }
  | { ok: false; status: number; error: string; message: string };

// callBackend resolves the authenticated session and forwards to the backend,
// returning the raw upstream Response (so an SSE body can pipe straight
// through). `ok:false` means we could not reach the backend with a valid
// session — NOT a backend business error (those come back as a real Response).
async function callBackend(path: string, init: ForwardInit, accept: string): Promise<BackendCall> {
  const config = await getServerConfig(true);
  if (!config.ok) {
    const status = config.error.status ?? (config.error.kind === "unauthorized" ? 401 : 503);
    return {
      ok: false,
      status,
      error: config.error.kind === "unauthorized" ? "unauthorized" : "assistant_unreachable",
      message: config.error.message,
    };
  }

  const { baseUrl, bearerToken, tenantId, traceparent } = config.data;
  const headers: Record<string, string> = {
    Authorization: `Bearer ${bearerToken}`,
    Accept: accept,
  };
  if (tenantId) headers["X-GoatOS-Tenant-ID"] = tenantId;
  if (traceparent) headers["traceparent"] = traceparent;
  if (init.body !== undefined) headers["Content-Type"] = "application/json";

  try {
    const response = await fetch(`${baseUrl}${path}`, {
      method: init.method,
      headers,
      body: init.body,
      signal: init.signal,
      cache: "no-store",
    });
    return { ok: true, response };
  } catch (error: unknown) {
    return {
      ok: false,
      status: 503,
      error: "assistant_unreachable",
      message:
        error instanceof Error
          ? `The leadership assistant service is not reachable: ${error.message}`
          : "The leadership assistant service is not reachable from this session yet.",
    };
  }
}

function unreachable(call: Extract<BackendCall, { ok: false }>): Response {
  return NextResponse.json(
    { error: call.error, message: call.message, mode: "degraded" },
    { status: call.status, headers: NO_STORE },
  );
}

// forwardJson proxies a JSON request/response, mirroring the backend status
// verbatim (403 leadership_required, 429 rate_limited, etc.).
export async function forwardJson(path: string, init: ForwardInit): Promise<Response> {
  const call = await callBackend(path, init, "application/json");
  if (!call.ok) return unreachable(call);
  const upstream = call.response;
  const text = await upstream.text();
  return new Response(text, {
    status: upstream.status,
    headers: {
      "Content-Type": upstream.headers.get("Content-Type") ?? "application/json",
      ...NO_STORE,
    },
  });
}

// forwardStream proxies the assistant answer stream. If the backend returns an
// event-stream, its body pipes through untouched (progressive token render). If
// the backend answers JSON (non-streaming fallback / error envelope), that JSON
// is forwarded verbatim so the client has one code path.
export async function forwardStream(path: string, init: ForwardInit): Promise<Response> {
  const call = await callBackend(path, init, "text/event-stream, application/json");
  if (!call.ok) return unreachable(call);

  const upstream = call.response;
  const contentType = upstream.headers.get("Content-Type") ?? "";
  if (contentType.includes("text/event-stream") && upstream.body) {
    return new Response(upstream.body, {
      status: upstream.status,
      headers: {
        "Content-Type": "text/event-stream; charset=utf-8",
        "Cache-Control": "no-store, no-transform",
        Connection: "keep-alive",
        "X-Accel-Buffering": "no",
      },
    });
  }

  const text = await upstream.text();
  return new Response(text, {
    status: upstream.status,
    headers: { "Content-Type": contentType || "application/json", ...NO_STORE },
  });
}
