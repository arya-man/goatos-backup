// Public entrypoint for the Feed vertical. App routes import from "@/features/feed" (not deep
// submodule paths) per the import-boundary convention.
//
// Three module surfaces:
//   Feed Direction — the generated per-shed feed sheet for one park and one business day.
//   Feed Packing   — the same generated day rolled up to what the store weighs out. Read-only.
//   Feed Config    — the authored ration grid, shed factors, session template and dispatch clock.
//
// Direction and Packing are two VIEWS of one generation run, not two runs. Config is the input that
// run reads.
//
// Before changing any quantity rendering on any of the three, read the header of feed-quantity.tsx:
// a blocked cell (no authored ration, shed will not be fed) and an authored zero (fed nothing of
// this item on purpose) are opposite states and must never collapse into each other.
export { FeedDirectionPage } from "./feed-direction";
export { FeedPackingPage } from "./feed-packing";
export { FeedConfigPage } from "./feed-config";
export { FeedAnalyticsPage } from "./feed-analytics";
export { ToxinReportPage } from "./toxin-report";
