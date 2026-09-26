// Layout constants shared by the page and its loading.tsx, so the skeleton cannot drift from it.
import type { VendorFilterKey } from "./vendor-filter-bar";

/** Vendors per page. */
export const PAGE_SIZE = 25;
/** The filter bar's select fields, in order. */
export const VENDOR_FILTER_KEYS: VendorFilterKey[] = ["record_type", "status", "state", "city", "breed"];
