// Layout constants shared by /configuration/items, its loading.tsx and its UrlSuspense fallback.
/** The left rail's and the register's Grid item sizes (template mail layout: nav + list). */
export const ITEMS_RAIL_SIZE = { xs: 12, md: 4, lg: 3 } as const;
export const ITEMS_REGISTER_SIZE = { xs: 12, md: 8, lg: 9 } as const;
/** Register status tabs (Active / Archived / All) with Label counts. */
export const ITEMS_STATUS_TABS = 3;
/** Register toolbar: the filter select (200), then search; the ⋮ closes the row. */
export const ITEMS_TOOLBAR_FIELDS: (number | "search")[] = [200, "search"];
/** Placeholder rail groups while loading (items per group; the real groups come from the catalog). */
export const ITEMS_RAIL_GROUPS = [4, 4, 4, 4, 4];
/** Placeholder table columns while the register's own columns are unknown. */
export const ITEMS_COLUMNS = 6;
