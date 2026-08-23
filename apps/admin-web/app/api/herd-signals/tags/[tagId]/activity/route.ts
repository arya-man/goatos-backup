import { NextResponse } from "next/server";
import { getHerdSignalsActivity } from "@/lib/api/herd-signals";

export const dynamic = "force-dynamic";

// Authenticated same-origin proxy for the full-screen history's activity overlays.
//
// The full-screen view is a CLIENT component, and lib/api/herd-signals.ts is server-only (it mints
// the bearer token and reads next/headers). Importing the reader directly from the client pulled
// "server-only" into the browser bundle and webpack refused to build the app AT ALL -- every route,
// including pages that have nothing to do with Herd Signals, returned 500. The drawer's timeline
// already fetches through a proxy for exactly this reason; overlays follow the same path.
export async function GET(request: Request, { params }: { params: Promise<{ tagId: string }> }) {
  const { tagId } = await params;
  if (!tagId) {
    return NextResponse.json({ error: "invalid_tag_id" }, { status: 400, headers: { "Cache-Control": "no-store" } });
  }

  // from/to are REQUIRED by the reader: the activity window has to be stated, never guessed. A
  // missing bound would otherwise silently become "all of time", which the monitoring boundary
  // exists to prevent.
  const url = new URL(request.url);
  const from = url.searchParams.get("from");
  const to = url.searchParams.get("to");
  if (!from || !to) {
    return NextResponse.json(
      { error: "from and to are required" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  const result = await getHerdSignalsActivity({ tagId, from, to });

  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }

  return NextResponse.json(result.data, { headers: { "Cache-Control": "no-store" } });
}
