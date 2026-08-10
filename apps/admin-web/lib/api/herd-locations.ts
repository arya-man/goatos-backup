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

export async function listAllFeedConfigPens(params: { park_id?: string } = {}): Promise<ApiResult<FeedConfigPenPage>> {
  const limit = 200;
  const pages = await Promise.all(
    Array.from({ length: 20 }, (_, page) => listFeedConfigPens({ ...params, limit, offset: page * limit })),
  );
  const firstError = pages.find((page) => !page.ok);
  if (firstError && !firstError.ok) return firstError;

  const okPages = pages.filter((page): page is { ok: true; data: FeedConfigPenPage } => page.ok);
  const visiblePages = okPages.slice(0, okPages.findIndex((page) => !page.data.has_more) + 1 || okPages.length);
  const [firstPage] = visiblePages;
  return {
    ok: true,
    data: {
      ...(firstPage?.data ?? { items: [], limit, offset: 0, has_more: false }),
      items: visiblePages.flatMap((page) => page.data.items),
      has_more: okPages.at(-1)?.data.has_more ?? false,
    },
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
  const partitionedShedIds = new Set(
    pens.ok
      ? pens.data.items
        .filter((pen) => pen.partition_label)
        .map((pen) => pen.shed_id)
      : [],
  );
  const penLocations = pens.ok
    ? pens.data.items
      .filter((pen) => usableSheds.some((shed) => shed.id === pen.shed_id))
      .map((pen) => ({
        key: pen.partition_label ? `${pen.shed_id}|${pen.partition_label}` : pen.shed_id,
        shedId: pen.shed_id,
        parkId: pen.park_id ?? null,
        partitionLabel: pen.partition_label ?? null,
        label: pen.operational_location_display || pen.shed_name,
      }))
    : [];
  const penKeys = new Set(penLocations.map((location) => location.key));
  const wholeShedLocations = usableSheds
    .filter((shed) => !partitionedShedIds.has(shed.id))
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
