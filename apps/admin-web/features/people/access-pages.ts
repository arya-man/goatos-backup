/**
 * Which admin-web screens the access editor offers, worked out from the ticks ON SCREEN.
 *
 * The backend sends every screen of every module with the permissions it needs
 * (`pages[].required_permissions`) and what each web level grants (`web_level_permissions`).
 * A screen is offered when every permission it needs is granted by some WEB tick, on any
 * module -- Feed SOP needs sop.read from Protocols & SOPs. Phone ticks never open a web
 * screen. This is the same rule the save (access_service.validatedPages) and the sidebar
 * (permissions.PageAccessForAssignments) apply, so a screen offered here is a screen that
 * saves and shows (People / HRMS fixes, 2026-10-02).
 *
 * Before this the editor's screens came from what had been SAVED, so a module switched on
 * for the first time showed no screens and its save was refused.
 */

export type AccessPageRow = {
  module_key: string;
  pages: { page_key: string; required_permissions: string[] }[];
  web_level_permissions: Record<string, string[]>;
};

export type WebTicks = Record<string, { web: string[]; pages: string[] }>;

/** Every permission the web ticks on screen grant, across all modules. */
export function webPermissionsFromTicks(rows: AccessPageRow[], ticks: WebTicks): Set<string> {
  const held = new Set<string>();
  for (const row of rows) {
    for (const level of ticks[row.module_key]?.web ?? []) {
      for (const permission of row.web_level_permissions[level] ?? []) held.add(permission);
    }
  }
  return held;
}

/** The module's screens these ticks can open, in sidebar order. */
export function openablePageKeys(row: AccessPageRow, held: Set<string>): string[] {
  return row.pages
    .filter((page) => page.required_permissions.every((permission) => held.has(permission)))
    .map((page) => page.page_key);
}

/**
 * The page ticks to save, consistent with what the screen shows:
 *  - a module off on the web keeps no page ticks;
 *  - a tick on a screen the current levels no longer open is dropped (it is not on screen);
 *  - a web-held module with openable screens and none ticked gets all of them -- granting a
 *    module opens it on every screen it has, which is what an empty stored list means too.
 *    With `fillEmpty: false` (the save itself) that is left alone: an admin who unticked
 *    every screen gets the backend's "needs at least one screen ticked", never a silent refill.
 */
export function pagesToSave(
  rows: AccessPageRow[],
  ticks: WebTicks,
  { fillEmpty = true }: { fillEmpty?: boolean } = {},
): Record<string, string[]> {
  const held = webPermissionsFromTicks(rows, ticks);
  const out: Record<string, string[]> = {};
  for (const row of rows) {
    const tick = ticks[row.module_key];
    if (!tick || tick.web.length === 0) {
      out[row.module_key] = [];
      continue;
    }
    const openable = openablePageKeys(row, held);
    const kept = openable.filter((key) => tick.pages.includes(key));
    out[row.module_key] = kept.length > 0 || !fillEmpty ? kept : openable;
  }
  return out;
}
