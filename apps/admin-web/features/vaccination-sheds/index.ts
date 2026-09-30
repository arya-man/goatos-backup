// Shed-wise vaccination — the MAIN /vaccination surface (one row per shed). The pen detail at
// /vaccination/execution/sheds/{shedId} is ShedExecutionDetailPage (features/vaccination-execution).
// This replaces the old cohort/vaccine-wise status matrix + per-cohort detail + drive shed-event board
// as the primary vaccination table (see docs/runbooks/staging-vaccination-seed-preflight.md).
export { VaccinationShedBoard, shedBoardColumns, loadVaccinationShedSummary } from "./shed-board";
export { vaccinationCurrentViewScope } from "./shed-scope";
