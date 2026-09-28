import type { PageHeaderLayout } from "@/components/app/page-header";
// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
/** The catalogue header: Add disease stays on the title row at 390 (TR3-P0-1; guard: page-header-layout-twin). */
export const HEALTH_CONFIG_HEADER_LAYOUT: PageHeaderLayout = { actionsInline: true };
/** Protocol catalogue rows per keyset page. */
export const CATALOG_PAGE_SIZE = 25;
