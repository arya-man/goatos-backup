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

  // status=alive, the value the identity search actually accepts (backend/internal/identity) --
  // a picker must not offer an animal that is dead or sold. An invented "active" here silently
  // matched NOTHING and made every search look like "no such animal".
  const result = await searchGoats({ limit: MAX_RESULTS, q, status: "alive" });
  if (!result.ok) {
    return NextResponse.json(
      { error: result.error.message },
      { status: result.error.status ?? 500, headers: { "Cache-Control": "no-store" } },
    );
  }

  // /goats/search returns ONE ROW PER MATCHING IDENTIFIER, so an animal whose id and both ear-tag
  // values all match the query came back four times and the picker listed the same animal four
  // times over. The picker chooses an ANIMAL, so it is keyed by goat_id.
  const seen = new Set<string>();
  const unique = result.data.items.filter((goat) => {
    if (seen.has(goat.goat_id)) return false;
    seen.add(goat.goat_id);
    return true;
  });

  return NextResponse.json(
    {
      items: unique.map((goat) => ({
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
