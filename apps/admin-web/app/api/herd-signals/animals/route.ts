import { NextResponse } from "next/server";
import { searchGoats } from "@/lib/api/server";

export const dynamic = "force-dynamic";

// Animal picker feed for the "Map to animal" / "Replace tag" dialogs.
//
// This reuses the SAME bounded /goats/search read the Herd Register row list already runs
// (lib/api/server.ts searchGoats) rather than inventing a second animal-lookup path — one search
// contract, one set of scope rules, one place where the tenant filter lives.
//
// Bounded on purpose: a picker is a typeahead, not a herd export. `limit` is capped so a stray
// query can never pull the whole register into a dialog.
const MAX_RESULTS = 8;

export async function GET(request: Request) {
  const url = new URL(request.url);
  const q = url.searchParams.get("q")?.trim() ?? "";
  if (q.length < 2) {
    // Not an error: a one-character query would return most of the register. The dialog shows its
    // own "keep typing" hint for this case.
    return NextResponse.json({ items: [] }, { headers: { "Cache-Control": "no-store" } });
  }

  const result = await searchGoats({ limit: MAX_RESULTS, q, status: "active" });
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }

  return NextResponse.json(
    {
      items: result.data.items.map((goat) => ({
        goat_id: goat.goat_id,
        display_id: goat.display_id,
        animal_identifier_1: goat.animal_identifier_1 ?? null,
        animal_identifier_2: goat.animal_identifier_2 ?? null,
        sex: goat.sex,
        breed: goat.breed ?? null,
      })),
    },
    { headers: { "Cache-Control": "no-store" } },
  );
}
