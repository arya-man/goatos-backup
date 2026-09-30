// The phone-tab editor's form contract (maintainer instruction 2026-10-01,
// docs/decisions/simple-task-phone-tabs.md): how the client form encodes a tab into FormData, how
// the Server Action decodes it back into the generated PenRoutineTabWrite body, and how a saved tab
// lands in the list the section holds. Pure module -- no server-only import, no React -- so the node
// test exercises the same decoder the action runs.
//
// Scalars travel as plain fields; the two LISTS (filters, routine ids) travel as ONE hidden JSON
// field each. The backend re-validates everything (label length, the closed module / icon / filter
// vocabularies, the routine cap); this decoder only shapes, it never invents a value.

export type PhoneTabFilterKey = "status" | "date" | "pen";

export type PhoneTabWriteBody = {
  label: string;
  module_key: string;
  icon_key: string;
  filters: PhoneTabFilterKey[];
  routine_ids: string[];
  /** Update only: the version the drawer loaded with. */
  row_version?: number;
};

export type PhoneTabStatusBody = {
  status: "active" | "retired";
  row_version: number;
};

/** The label a bottom-bar slot can hold; the backend enforces the same number. */
export const TAB_LABEL_MAX = 24;

export const TAB_FORM_JSON_FIELDS = { filters: "filters_json", routineIds: "routine_ids_json" } as const;

const FILTER_KEYS: readonly PhoneTabFilterKey[] = ["status", "date", "pen"];

function text(formData: FormData, key: string): string {
  const value = formData.get(key);
  return typeof value === "string" ? value.trim() : "";
}

function jsonStrings(formData: FormData, key: string): string[] {
  const raw = formData.get(key);
  if (typeof raw !== "string" || raw === "") return [];
  const parsed: unknown = JSON.parse(raw);
  if (!Array.isArray(parsed)) throw new Error(`${key} is not a list`);
  return parsed.filter((item): item is string => typeof item === "string" && item !== "");
}

function rowVersion(formData: FormData): number | undefined {
  const raw = text(formData, "row_version");
  if (!raw) return undefined;
  const value = Number.parseInt(raw, 10);
  return Number.isFinite(value) ? value : undefined;
}

/**
 * The write body. Throws when the form lacks what every tab needs (a name, a module, an icon), so
 * the action reports the form error instead of posting a body the backend would refuse anyway.
 * A filter key outside the phone's closed set is dropped rather than sent: the form only ever
 * offers the backend's own keys, so an unknown one can only be a tampered field.
 */
export function decodePhoneTabWrite(formData: FormData): PhoneTabWriteBody {
  const label = text(formData, "label").split(/\s+/).filter(Boolean).join(" ");
  const moduleKey = text(formData, "module_key");
  const iconKey = text(formData, "icon_key");
  if (!label || !moduleKey || !iconKey) throw new Error("phone tab form is missing a required field");
  const filters = [...new Set(jsonStrings(formData, TAB_FORM_JSON_FIELDS.filters))].filter((key): key is PhoneTabFilterKey =>
    (FILTER_KEYS as readonly string[]).includes(key),
  );
  const routineIds = [...new Set(jsonStrings(formData, TAB_FORM_JSON_FIELDS.routineIds))];
  const body: PhoneTabWriteBody = { label, module_key: moduleKey, icon_key: iconKey, filters, routine_ids: routineIds };
  const version = rowVersion(formData);
  if (version !== undefined) body.row_version = version;
  return body;
}

export function decodePhoneTabStatus(formData: FormData): PhoneTabStatusBody {
  const status = text(formData, "status");
  if (status !== "active" && status !== "retired") throw new Error("phone tab status is not one the backend knows");
  return { status, row_version: rowVersion(formData) ?? 0 };
}

type TabLike = { tab_id: string; routines: { routine_id: string }[] };

/**
 * Applies a saved tab to the list the section holds, exactly as the backend did it: the saved tab
 * replaces its old self (or joins the end when new), and every routine it now carries leaves any
 * OTHER tab, because a routine sits on one tab at a time.
 */
export function applySavedTab<T extends TabLike>(tabs: T[], saved: T): T[] {
  const moved = new Set(saved.routines.map((routine) => routine.routine_id));
  let found = false;
  const next = tabs.map((tab) => {
    if (tab.tab_id === saved.tab_id) {
      found = true;
      return saved;
    }
    if (!tab.routines.some((routine) => moved.has(routine.routine_id))) return tab;
    return { ...tab, routines: tab.routines.filter((routine) => !moved.has(routine.routine_id)) };
  });
  return found ? next : [...next, saved];
}

type RoutineLike = { routine_id: string; name: string; park_id: string; park_name: string; status: string };

export type RoutineParkGroup<R> = { parkId: string; parkName: string; routines: R[] };

/**
 * The routines a tab may carry, grouped by park in the order the list served them (CBE, then CPT --
 * the backend's order, never re-sorted here). A retired routine raises nothing, so it is offered
 * only while the tab already carries it.
 */
export function routinesByPark<R extends RoutineLike>(routines: R[], keep: ReadonlySet<string>): RoutineParkGroup<R>[] {
  const groups: RoutineParkGroup<R>[] = [];
  const index = new Map<string, RoutineParkGroup<R>>();
  for (const routine of routines) {
    if (routine.status === "retired" && !keep.has(routine.routine_id)) continue;
    let group = index.get(routine.park_id);
    if (!group) {
      group = { parkId: routine.park_id, parkName: routine.park_name, routines: [] };
      index.set(routine.park_id, group);
      groups.push(group);
    }
    group.routines.push(routine);
  }
  return groups;
}
