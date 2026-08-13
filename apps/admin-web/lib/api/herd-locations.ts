import "server-only";

// Real location options for the Herd Register drawers. Parks, sheds, and farms come from the admin
// locations master (canonical Postgres truth) via the generated client — never invented UI values. Sheds
// carry their parent park id so the Register drawer can scope the shed select to the chosen park. On any
// error each list is empty and the drawer renders an honest "locations unavailable" blocker rather than a
// faked dropdown.
import { listFeedConfigPens, listLocations, type ApiResult, type FeedConfigPenPage, type LocationSummary } from "@/lib/api/server";

export type LocationOption = {
  id: string;
  code: string | null;
  name: string;
  parentId: string | null;
};

export type HerdRegisterLocations = {
  parks: LocationOption[];
  sheds: LocationOption[];
  farms: LocationOption[];
  operationalLocations: {
    key: string;
    shedId: string;
    parkId: string | null;
    partitionLabel: string | null;
    label: string;
  }[];
  available: boolean;
};

function toOption(l: LocationSummary): LocationOption {
  return { id: l.location_id, code: l.location_code, name: l.name, parentId: l.parent_location_id };
}

// Sheds usable for vaccination only — a goat registered into a non-vaccination shed cannot anchor the
// vaccination trigger, so we do not offer it as a clean create target.
function shedUsable(l: LocationSummary): boolean {
  return l.operational.usable_for_vaccination && !l.operational.is_holding;
}

/**
 * The whole pen catalog, read one page at a time until the backend says there is no more.
 *
 * Requested SEQUENTIALLY on `has_more`, never as a fixed fan-out. The previous shape fired 20 pages
 * concurrently whether or not they existed, so a tenant holding 130 pens — the live figure across
 * both parks — sent 19 requests that returned nothing, on every Feed Config render and therefore on
 * every filter change. Following `has_more` costs ONE request in that case.
 *
 * Bounded config, not a herd scan: pens are authored infrastructure (two parks, ~20 sheds and ~40
 * pens each) and cannot grow with animals. MAX_PAGES is a backstop against a backend that never
 * clears `has_more`, not an expected page count — the loop normally exits on the first page.
 */
export async function listAllFeedConfigPens(params: { park_id?: string } = {}): Promise<ApiResult<FeedConfigPenPage>> {
  const limit = 200;
  const MAX_PAGES = 20;
  const items: FeedConfigPenPage["items"] = [];
  let first: FeedConfigPenPage | null = null;
  let hasMore = false;

  for (let page = 0; page < MAX_PAGES; page += 1) {
    // serial-await: allow page N+1 is requested only because page N said has_more, so the pages cannot be batched with Promise.all without guessing how many exist -- which is exactly the fixed 20-request fan-out this replaced. The live catalog answers in ONE iteration.
    const result = await listFeedConfigPens({ ...params, limit, offset: page * limit });
    if (!result.ok) return result;
    first ??= result.data;
    items.push(...result.data.items);
    hasMore = result.data.has_more;
    if (!hasMore) break;
  }

  return {
    ok: true,
    data: { ...(first ?? { items: [], limit, offset: 0, has_more: false }), items, has_more: hasMore },
  };
}

/**
 * Census location options — every ACTIVE farm and shed, with no vaccination-usability filter.
 *
 * This deliberately does NOT reuse getHerdRegisterLocations(): that helper drops sheds where
 * `!usable_for_vaccination || is_holding`, which is right for the Register-animal drawer (you
 * cannot anchor a vaccination trigger in a holding shed) and wrong for a census, where a
 * holding shed still physically contains animals. Filtering here would silently undercount.
 */
export async function getCensusLocations(): Promise<HerdRegisterLocations> {
  const [parks, sheds, farms] = await Promise.all([
    listLocations({ type: "park", status: "active" }),
    listLocations({ type: "shed", status: "active" }),
    listLocations({ type: "farm", status: "active" }),
  ]);

  return {
    parks: parks.ok ? parks.data.items.map(toOption) : [],
    sheds: sheds.ok ? sheds.data.items.map(toOption) : [],
    farms: farms.ok ? farms.data.items.map(toOption) : [],
    operationalLocations: [],
    available: parks.ok && sheds.ok && farms.ok,
  };
}

export async function getHerdRegisterLocations(): Promise<HerdRegisterLocations> {
  const [parks, sheds, farms, pens] = await Promise.all([
    listLocations({ type: "park", status: "active" }),
    listLocations({ type: "shed", status: "active" }),
    listLocations({ type: "farm", status: "active" }),
    listAllFeedConfigPens(),
  ]);

  const usableSheds = sheds.ok ? sheds.data.items.filter(shedUsable).map(toOption) : [];
  const penLocationsRaw = pens.ok
    ? pens.data.items
      .filter((pen) => usableSheds.some((shed) => shed.id === pen.shed_id))
      .map((pen) => ({
        key: pen.shed_id,
        shedId: pen.shed_id,
        parkId: pen.park_id ?? null,
        partitionLabel: null,
        label: pen.operational_location_display || pen.shed_name,
      }))
    : [];
  const penLocations = [...new Map(penLocationsRaw.map((location) => [location.shedId, location])).values()];
  const penKeys = new Set(penLocations.map((location) => location.key));
  const wholeShedLocations = usableSheds
    .filter((shed) => !penKeys.has(shed.id))
    .map((shed) => ({
      key: shed.id,
      shedId: shed.id,
      parkId: shed.parentId,
      partitionLabel: null,
      label: `${shed.name}${shed.code ? ` · ${shed.code}` : ""}`,
    }))
    .filter((location) => !penKeys.has(location.key));
  const available = parks.ok && sheds.ok && farms.ok && pens.ok;
  return {
    parks: parks.ok ? parks.data.items.map(toOption) : [],
    sheds: usableSheds,
    farms: farms.ok ? farms.data.items.map(toOption) : [],
    operationalLocations: [...penLocations, ...wholeShedLocations],
    available,
  };
}
