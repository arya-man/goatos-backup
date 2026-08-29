import { NextResponse } from "next/server";
import { getHerdSignalsLive } from "@/lib/api/herd-signals";

export const dynamic = "force-dynamic";

// "Does this animal ALREADY carry a live smart tag?" — asked by the picker the moment an animal is
// chosen, so the conflict is on screen BEFORE the operator presses Map, not returned as a 409
// afterwards.
//
// The answer is read from the same /herd-signals/live projection the table renders, narrowed to
// mapped rows and searched by the animal's own display id (the backend's `q` matches
// g.display_id — backend/internal/herdsignals/adapters/postgres/repository.go). Rows are then
// matched on goat_id, never on the search string, so a display id that is a prefix of another
// animal's cannot produce a false conflict.
//
// An animal that already carries one means the operator wants REPLACE, not MAP. This route only
// reports what is bound; it never decides.
const PROBE_LIMIT = 50;

export async function GET(request: Request) {
  const url = new URL(request.url);
  const goatId = url.searchParams.get("goat_id")?.trim() ?? "";
  const displayId = url.searchParams.get("display_id")?.trim() ?? "";
  if (!goatId || !displayId) {
    return NextResponse.json(
      { error: "goat_id and display_id are required" },
      { status: 400, headers: { "Cache-Control": "no-store" } },
    );
  }

  const result = await getHerdSignalsLive({ q: displayId, mappingState: "mapped", limit: PROBE_LIMIT });
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }

  const tags = result.data.items
    .filter((item) => item.goat_id === goatId)
    .map((item) => ({ tag_id: item.tag_id, tag_mac: item.tag_mac }));

  return NextResponse.json({ tags }, { headers: { "Cache-Control": "no-store" } });
}
