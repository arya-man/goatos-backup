import { type NextRequest, NextResponse } from "next/server";
import { TENANT_CONTEXT_HEADER } from "@goatos/api-client";
import { getServerConfig, noteBackendWrite } from "@/lib/api/server";

export const dynamic = "force-dynamic";

// Same-origin proxy for the diagnosis-register sheet routes (maintainer instruction 2026-09-23).
//
// Downloads and uploads are FILES, so they bypass the JSON client and STREAM both ways: the
// backend's CSV/XLSX body is piped to the browser, the browser's multipart upload is piped to the
// backend. Nothing here holds a sheet in memory. Auth is the same server-side bearer and tenant
// the generated client sends; the backend route table enforces health.config.read /
// health.config.write, so this file decides WHICH shapes may be reached and nothing else.
//
// It is a second proxy rather than a widening of the configuration one on purpose: that file
// guards `/admin/configuration/*`, and teaching it a health path would put two modules'
// authorisation surface behind one regular expression.
//
// Shapes accepted (anything else is 404):
//   GET  <type>/template?format=    GET <type>/export?format=    POST <type>/import (multipart)

const TYPE_RE = /^[a-z][a-z0-9_]{0,48}$/;

function backendPath(method: string, path: string[]): string | null {
  if (path.length !== 2) return null;
  const [animalClass, action] = path;
  if (!animalClass || !TYPE_RE.test(animalClass)) return null;
  const base = `/health-config/registers/${encodeURIComponent(animalClass)}`;
  if (method === "GET" && (action === "template" || action === "export")) return `${base}/${action}`;
  if (method === "POST" && action === "import") return `${base}/import`;
  return null;
}

async function proxy(request: NextRequest, path: string[]): Promise<Response> {
  const target = backendPath(request.method, path);
  if (!target) {
    return NextResponse.json({ error: "not_found" }, { status: 404, headers: { "Cache-Control": "no-store" } });
  }
  const config = await getServerConfig(true);
  if (!config.ok) {
    return NextResponse.json({ error: config.error.message }, {
      status: config.error.status ?? 500,
      headers: { "Cache-Control": "no-store" },
    });
  }
  const url = new URL(target, config.data.baseUrl);
  request.nextUrl.searchParams.forEach((value, key) => url.searchParams.set(key, value));

  const headers = new Headers();
  headers.set("Authorization", `Bearer ${config.data.bearerToken}`);
  if (config.data.tenantId) headers.set(TENANT_CONTEXT_HEADER, config.data.tenantId);

  const init: RequestInit & { duplex?: "half" } = { method: request.method, headers, cache: "no-store" };
  if (request.method === "POST") {
    const contentType = request.headers.get("content-type");
    if (contentType) headers.set("content-type", contentType);
    // An upload WRITES, so it carries the idempotency key the backend's write contract requires.
    const idem = request.headers.get("idempotency-key");
    if (idem) headers.set("Idempotency-Key", idem);
    if (request.body) {
      init.body = request.body;
      init.duplex = "half";
    }
  }

  let upstream: Response;
  try {
    upstream = await fetch(url, init);
  } finally {
    // A register import changes health config: the importer must not then read a cached answer.
    if (request.method !== "GET") await noteBackendWrite();
  }
  const out = new Headers({ "Cache-Control": "no-store" });
  for (const name of ["content-type", "content-disposition"]) {
    const value = upstream.headers.get(name);
    if (value) out.set(name, value);
  }
  return new Response(upstream.body, { status: upstream.status, headers: out });
}

export async function GET(request: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(request, (await params).path ?? []);
}

export async function POST(request: NextRequest, { params }: { params: Promise<{ path: string[] }> }) {
  return proxy(request, (await params).path ?? []);
}
