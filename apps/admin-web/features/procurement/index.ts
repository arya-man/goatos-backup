// Public entrypoint for the procurement source-entry feature. App pages import from
// "@/features/procurement" (not deep submodule paths) per the import-boundary convention.
//
// Procurement is its OWN top-level vertical surface, OPERATIONAL source-entry only: the Source Entry Board
// and per-load detail. Control Tower / Action Center / Protocol Adherence / Workflows are top-level command
// screens — they are NOT duplicated under procurement; procurement data reaches them through the existing
// top-level routes via ?domain=procurement. Read surfaces plus real operator write flows (create load, add
// source goat, source health, pre-dispatch decision, dispatch, arrival review, accept intake) are wired
// through generated POST contracts in load-forms.tsx + actions.ts.
export { SourceEntryBoardPage } from "./source-entry-board";
export { ProcurementLoadDetailPage } from "./load-detail";
