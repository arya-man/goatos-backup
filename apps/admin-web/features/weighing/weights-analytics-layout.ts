// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** The analytics tabs, in order ("general" is the default). */
export const WEIGHTS_TABS = ["general", "breed", "birth", "shed", "weight", "time", "load", "fcr"] as const;
/** Pens-table rows per page. */
export const WEIGHTS_DEFAULT_LIMIT = 25;
