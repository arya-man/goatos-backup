// Layout constants shared by /procurement/animal-purchases and its loading twin.
/** The headline KPI cards' Grid item size (four cards: loads, pending, accepted, rejected). */
export const ANIMAL_KPI_SIZE = { xs: 12, sm: 6, md: 3 } as const;
export const ANIMAL_KPI_COUNT = 4;
/** Loads per page when the contract offers no page sizes. */
export const ANIMAL_LOADS_DEFAULT_LIMIT = 20;
/** The contract's `animal-purchase-loads` columns (backend adminui service), in order. */
export const ANIMAL_LOAD_COLUMNS = ["load_ref", "vendor_name", "farm", "expected_count", "total", "pending", "accepted", "rejected", "created_at"] as const;
/** The contract's `animal_purchase_decisions` keys (backend adminui service): the decision tabs. */
export const ANIMAL_DECISION_KEYS = ["pending", "accepted", "rejected", "all"] as const;
/** Animals per page (contract `animal-purchase-animals` page size). */
export const ANIMALS_PAGE_SIZE = 10;
/** The animal cards' JobList columns. */
export const ANIMAL_CARD_COLUMNS = { xs: 1, lg: 2 } as const;
/** Placeholder width of the toolbar's Apply button (its label's width; skeleton only). */
export const ANIMAL_APPLY_TWIN_WIDTH = 72;
/**
 * Load rows the route placeholder shows: the loads on offer are a handful (a page of 20 is the
 * pager's ceiling, not the usual list), so the first paint reserves five; a page / page-size click
 * shows the real page size (the rows' UrlSuspense fallback takes the URL limit).
 */
export const ANIMAL_LOADS_SKELETON_ROWS = 5;
/** The loads card pill strip: "All loads" (a second pill names the picked load when one is selected). */
export const ANIMAL_LOAD_STRIP_TABS = 1;
