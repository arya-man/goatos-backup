import { type NextRequest, NextResponse } from "next/server";
import { TENANT_CONTEXT_HEADER } from "@goatos/api-client";
import { getServerConfig, noteBackendWrite } from "@/lib/api/server";

export const dynamic = "force-dynamic";

// Same-origin proxy for the Configuration bulk-sheet routes (maintainer instruction 2026-09-18).
// Downloads and uploads are FILES, so they bypass the JSON client and STREAM both ways: the
// backend's CSV/XLSX body is piped to the browser, the browser's multipart upload is piped to
// the backend. Nothing here holds a sheet in memory. Auth is the same server-side bearer and
// tenant the generated client sends; the backend route table enforces configuration.read /
// configuration.write.
//
// Shapes accepted (anything else is 404):
//   GET  <register>/export?format=&status=    GET <register>/template?format=
//   GET  <register>/imports                   POST <register>/imports   (multipart, `file`)
//   GET  jobs/<id>  jobs/<id>/rows  jobs/<id>/errors?format=
//   POST jobs/<id>/apply  jobs/<id>/cancel
//   The onboarding workbook (one Excel, one tab per list) rides the same shapes with the
//   literal register `workbook`, and its bundle routes under bundles/<id>:
//   GET  workbook/template  workbook/export  workbook/imports   POST workbook/imports
//   GET  bundles/<id>  bundles/<id>/errors?format=   POST bundles/<id>/apply  bundles/<id>/cancel

const REGISTER_RE = /^(?:[a-z][a-z0-9_]{0,39}|ref:[a-z][a-z0-9_]{0,39})$/;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function backendPath(method: string, path: string[]): string | null {
  if (path[0] === "bundles") {
    const [, bundleId, action] = path;
    if (!bundleId || !UUID_RE.test(bundleId) || path.length > 3) return null;
    const base = `/admin/configuration-import-bundles/${bundleId}`;
    if (method === "GET" && (action === undefined || action === "errors")) return action ? `${base}/${action}` : base;
    if (method === "POST" && (action === "apply" || action === "cancel")) return `${base}/${action}`;
    return null;
  }
  if (path[0] === "jobs") {
    const [, jobId, action] = path;
    if (!jobId || !UUID_RE.test(jobId) || path.length > 3) return null;
    const base = `/admin/configuration-imports/${jobId}`;
    if (method === "GET" && (action === undefined || action === "rows" || action === "errors")) {
      return action ? `${base}/${action}` : base;
    }
    if (method === "POST" && (action === "apply" || action === "cancel")) return `${base}/${action}`;
    return null;
  }
  const [register, action] = path;
  if (!register || !REGISTER_RE.test(register) || path.length !== 2) return null;
  const base = `/admin/configuration/${encodeURIComponent(register)}`;
  if (method === "GET" && (action === "export" || action === "template" || action === "imports")) return `${base}/${action}`;
  if (method === "POST" && action === "imports") return `${base}/imports`;
  return null;
}

async function proxy(request: NextRequest, path: string[]): Promise<Response> {
  const target = backendPath(request.method, path);
  if (!target) {
    return NextResponse.json({ error: "not_found" }, { status: 404, headers: { "Cache-Control": "no-store" } });
  }
  const config = await getServerConfig(true);
  if (!config.ok) {
    return NextResponse.json({ error: config.error.message }, { status: config.error.status ?? 500, headers: { "Cache-Control": "no-store" } });
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
    if (request.body) {
      init.body = request.body;
      init.duplex = "half";
    }
  }
  let upstream: Response;
  try {
    upstream = await fetch(url, init);
  } finally {
    // Config imports change analytics inputs: the importer must not then read a cached answer.
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
