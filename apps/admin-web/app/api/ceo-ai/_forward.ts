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

// Cloud Run service-to-service auth for the coding-agent service. When
// CEO_AI_AGENT_AUDIENCE is set (STG: the Mesha assistant service URL), admin-web mints a
// Google ID token for its runtime SA from the metadata server and sends it as
// X-Serverless-Authorization, which Cloud Run IAM checks (roles/run.invoker) and
// strips — leaving Authorization for the user's Firebase bearer, which the agent
// validates itself. Unset audience (local dev) => no extra header.
const METADATA_IDENTITY_URL =
  "http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/identity";
const ID_TOKEN_TTL_MS = 50 * 60 * 1000;
const idTokenCache = new Map<string, { token: string; expiresAt: number }>();

async function agentIdToken(audience: string): Promise<string> {
  const cached = idTokenCache.get(audience);
  if (cached && cached.expiresAt > Date.now()) return cached.token;
  const response = await fetch(`${METADATA_IDENTITY_URL}?audience=${encodeURIComponent(audience)}`, {
    headers: { "Metadata-Flavor": "Google" },
    cache: "no-store",
    signal: AbortSignal.timeout(5000),
  });
  if (!response.ok) throw new Error(`metadata identity token request failed (${response.status})`);
  const token = (await response.text()).trim();
  if (!token) throw new Error("metadata identity token response was empty");
  idTokenCache.set(audience, { token, expiresAt: Date.now() + ID_TOKEN_TTL_MS });
  return token;
}

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
    // Feature flag: CEO_AI_AGENT_URL diverts the assistant to the coding-agent
    // service (same /ceo-ai/* contract). Unset => legacy backend ceo-ai.
    const agentUrl = process.env.CEO_AI_AGENT_URL?.replace(/\/$/, "");
    const agentAudience = process.env.CEO_AI_AGENT_AUDIENCE?.trim();
    if (agentUrl && agentAudience) {
      headers["X-Serverless-Authorization"] = `Bearer ${await agentIdToken(agentAudience)}`;
    }
    const response = await fetch(`${agentUrl ?? baseUrl.replace(/\/$/, "")}${path}`, {
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

// forwardBinary proxies a file download (attachment previews) with its content
// type, streaming the body through untouched.
export async function forwardBinary(path: string, init: ForwardInit): Promise<Response> {
  const call = await callBackend(path, init, "*/*");
  if (!call.ok) return unreachable(call);
  const upstream = call.response;
  const contentType = upstream.headers.get("Content-Type") ?? "application/octet-stream";
  // The content type is whatever the uploader claimed: only raster images and PDFs
  // render inline; anything else (HTML, SVG, …) downloads so it cannot run script
  // on the admin-web origin.
  const inlineSafe = /^(image\/(png|jpe?g|gif|webp)|application\/pdf)\b/i.test(contentType);
  const disposition = upstream.headers.get("Content-Disposition") ?? "inline";
  return new Response(upstream.body, {
    status: upstream.status,
    headers: {
      "Content-Type": contentType,
      "Content-Disposition": inlineSafe ? disposition : disposition.replace(/^\s*inline/i, "attachment"),
      "Cache-Control": "private, max-age=86400",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
