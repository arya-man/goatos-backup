// Shed-wise vaccination read model — DTO types for the main /vaccination table (one row per shed) and the
// shed detail (planned sessions + per-vaccine breakdown + animal roster + capacity planner).
//
// These re-export the generated OpenAPI schema types under the names the feature uses, so the shed board,
// shed detail, and their presentation maps depend on one stable, client-safe import path. Data is fetched
// through the server-only generated app client in lib/api/server.ts (getVaccinationShedSummary /
// getVaccinationShedDetail / getVaccinationShedAnimals) — there is no client-side mock.
import type { AppApiComponents } from "@goatos/api-client";

export type VaccinationShedSummaryResponse = AppApiComponents["schemas"]["VaccinationShedSummaryResponse"];
export type VaccinationShedSummaryRow = AppApiComponents["schemas"]["VaccinationShedSummaryRow"];
export type VaccinationShedDetail = AppApiComponents["schemas"]["VaccinationShedDetail"];
export type VaccinationShedVaccineRow = AppApiComponents["schemas"]["VaccinationShedVaccineRow"];
export type VaccinationShedAnimalPage = AppApiComponents["schemas"]["VaccinationShedAnimalPage"];
export type VaccinationShedAnimalRow = AppApiComponents["schemas"]["VaccinationShedAnimalRow"];
export type VaccinationShedOwner = AppApiComponents["schemas"]["VaccinationShedOwner"];
export type VaccinationShedStatus = AppApiComponents["schemas"]["VaccinationShedStatus"];
export type VaccinationCapacityStatus = AppApiComponents["schemas"]["VaccinationCapacityStatus"];
export type VaccinationPlannedSession = AppApiComponents["schemas"]["VaccinationPlannedSession"];
export type VaccinationShedSortKey = AppApiComponents["schemas"]["VaccinationShedSortKey"];
export type VaccinationPageInfo = AppApiComponents["schemas"]["VaccinationPageInfo"];
export type VaccinationOperationsCounts = AppApiComponents["schemas"]["VaccinationOperationsCounts"];
export type WorkState = AppApiComponents["schemas"]["WorkState"];
