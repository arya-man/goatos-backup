// Layout constants shared by PlanConsole and /vaccination/plan's loading.tsx, so the skeleton cannot
// drift from the page.
/** The live version's fact tiles (CardHeader + icon avatar), in order. */
export const PLAN_FACT_KEYS = ["since", "applies", "vaccines", "published"] as const;
/** A fact tile's Grid item size (spacing-3 Grid). */
export const PLAN_FACT_SIZE = { xs: 12, sm: 6, md: 3 } as const;
/** Live vaccine table rows per page. */
export const LIVE_PAGE_SIZE = 10;
/** The live vaccine table's columns (TableHeadCustom). */
export const LIVE_HEAD_CELLS = [
  { id: "vaccine", label: "Vaccine" },
  { id: "first", label: "First doses" },
  { id: "repeats", label: "Repeats" },
  { id: "state", label: "", width: 160 },
];
/** The earlier-versions table's columns (TableHeadCustom). */
export const EARLIER_HEAD_CELLS = [
  { id: "version", label: "Version" },
  { id: "inforce", label: "In force" },
  { id: "published", label: "Published" },
  { id: "changed", label: "What changed" },
  { id: "action", label: "", width: 140 },
];

// Loading-twin estimates of rendered sizes the page does not set itself (copy-driven).
/** Header action ("Start a new version") width; the live card's "Published" Label; the earlier card's count Label; Label height. */
export const PLAN_SKELETON = { headerActionWidths: [176], publishedLabelWidth: 91, countLabelWidth: 40, labelHeight: 24, earlierRows: 3 } as const;
