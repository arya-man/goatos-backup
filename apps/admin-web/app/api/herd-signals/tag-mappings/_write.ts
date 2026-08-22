import { NextResponse } from "next/server";
import type { ApiResult } from "@/lib/api/server";
import type { HerdSignalsTagMappingResponse } from "@/lib/api/herd-signals";

// Shared response shaping for the three Tag Mapping write proxies (MAP / REPLACE / UNMAP).
//
// Bearer-token minting stays server-only, same reasoning as the timeline proxy next door: the
// browser posts same-origin, this hop adds the credential. The dialogs that call these are
// client-local overlays, so a write must not force a route re-run of the whole board.
//
// A mapping refusal is a FIRST-CLASS ANSWER, not a server error — "that tag already belongs to
// another animal", "this animal already carries a live smart tag" are exactly the states these
// endpoints exist to prevent. The backend's own message is forwarded VERBATIM with its status, so
// the dialog can show what actually happened instead of a generic failure.
export async function writeMappingResult(
  result: ApiResult<HerdSignalsTagMappingResponse>,
): Promise<NextResponse> {
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message, code: result.error.code ?? null },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }
  return NextResponse.json(result.data, { headers: { "Cache-Control": "no-store" } });
}

export async function readJsonBody(request: Request): Promise<Record<string, unknown> | null> {
  try {
    const body = (await request.json()) as unknown;
    if (!body || typeof body !== "object" || Array.isArray(body)) return null;
    return body as Record<string, unknown>;
  } catch {
    return null;
  }
}

export function invalidBody(): NextResponse {
  return NextResponse.json(
    { error: "invalid_request_body" },
    { status: 400, headers: { "Cache-Control": "no-store" } },
  );
}

/** Optional string field: absent/empty stays absent so the backend's strict decoder never sees "". */
export function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value.trim() : undefined;
}
