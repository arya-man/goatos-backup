// Shed-wise vaccination — the MAIN /vaccination surface (one row per shed) + the shed detail
// (planned sessions, per-vaccine breakdown, animal roster) at /vaccination/execution/sheds/{shedId}.
// This replaces the old cohort/vaccine-wise status matrix + per-cohort detail + drive shed-event board
// as the primary vaccination table (see docs/runbooks/staging-vaccination-seed-preflight.md).
export { VaccinationShedBoard, loadVaccinationShedSummary } from "./shed-board";
export { VaccinationShedDetailPage } from "./shed-detail";
export { vaccinationCurrentViewScope } from "./shed-scope";
