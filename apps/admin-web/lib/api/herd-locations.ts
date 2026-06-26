import "server-only";

// Real location options for the Herd Register drawers. Parks, sheds, and farms come from the admin
// locations master (canonical Postgres truth) via the generated client — never invented UI values. Sheds
// carry their parent park id so the Register drawer can scope the shed select to the chosen park. On any
// error each list is empty and the drawer renders an honest "locations unavailable" blocker rather than a
// faked dropdown.
import { listLocations, type LocationSummary } from "@/lib/api/server";

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

export async function getHerdRegisterLocations(): Promise<HerdRegisterLocations> {
  const [parks, sheds, farms] = await Promise.all([
    listLocations({ type: "park", status: "active" }),
    listLocations({ type: "shed", status: "active" }),
    listLocations({ type: "farm", status: "active" }),
  ]);

  const available = parks.ok && sheds.ok && farms.ok;
  return {
    parks: parks.ok ? parks.data.items.map(toOption) : [],
    sheds: sheds.ok ? sheds.data.items.filter(shedUsable).map(toOption) : [],
    farms: farms.ok ? farms.data.items.map(toOption) : [],
    available,
  };
}
