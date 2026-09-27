// Layout constants shared by /work-board, its loading.tsx and the board's UrlSuspense fallback.
import { PEOPLE_DROPDOWN_WIDTH } from "@/components/people-dropdown";

/** The toolbar: assignee and module selects (200 from md), the search, then the day stepper group. */
export const WB_MODULE_SELECT_WIDTH = 200;
export const WB_TOOLBAR_FIELDS: (number | "search")[] = [PEOPLE_DROPDOWN_WIDTH, WB_MODULE_SELECT_WIDTH, "search"];
/** The day stepper group: 278 on a row from md, the full row on a phone (the toolbar is a column there). */
export const WB_DAY_STEPPER_WIDTH = { xs: "100%", md: 278 };
/** Placeholder cards per lane while loading (To do, In progress, In review, Done). */
export const WB_SKELETON_LANES = [3, 2, 2, 1];
/** The header's margin before the toolbar (template CustomBreadcrumbs mb { xs: 3, md: 5 }). */
export const WB_HEADER_MB = { xs: 3, md: 5 } as const;
