// Pure route helpers for the HRMS split (maintainer request 2026-09-30). Kept free of runtime
// imports so `node --test` exercises them directly.

type SearchParams = Record<string, string | string[] | undefined>;

/**
 * Where an old /people?tab=… link now lives. People / HRMS was one page with a tab strip; each
 * view is now its own page. Bookmarks, pushes and the Clock drawer's `?tab=clock&clocking=<id>`
 * deep link keep working: the tab is dropped and every other query parameter is carried across.
 */
export const LEGACY_PEOPLE_TAB_ROUTES: Readonly<Record<string, string>> = {
  clock: "/people/clock",
  notifications: "/people/notifications",
  vaccination: "/people/vaccination",
};

export function legacyPeopleTabRedirect(searchParams: SearchParams): string | null {
  const raw = searchParams.tab;
  const tab = Array.isArray(raw) ? raw[0] : raw;
  const target = tab ? LEGACY_PEOPLE_TAB_ROUTES[tab] : undefined;
  if (!target) return null;
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (key === "tab" || value === undefined) continue;
    for (const v of Array.isArray(value) ? value : [value]) query.append(key, v);
  }
  const qs = query.toString();
  return qs ? `${target}?${qs}` : target;
}
