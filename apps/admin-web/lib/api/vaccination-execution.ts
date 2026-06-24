// Parks Vaccination Execution — DTO types.
//
// These are the generated OpenAPI schema types re-exported under the names the feature uses, so the
// Parks UI and its presentation maps (work-state.ts) depend on one stable import path. Data is fetched
// through the server-only generated app client in lib/api/server.ts
// (getVaccinationExecution / getVaccinationExecutionShedDrilldown) — there is no client-side mock.
import type { AppApiComponents } from "@goatos/api-client";

export type VaccinationExecutionWorkState = AppApiComponents["schemas"]["VaccinationExecutionWorkState"];
export type VaccinationExecutionSeverity = AppApiComponents["schemas"]["VaccinationExecutionSeverity"];
export type SopStatus = AppApiComponents["schemas"]["VaccinationExecutionSOPStatus"];
export type ProofStatus = AppApiComponents["schemas"]["VaccinationExecutionProofStatus"];
export type VerificationStatus = AppApiComponents["schemas"]["VaccinationExecutionVerificationStatus"];
export type VaccinationExecutionOwner = AppApiComponents["schemas"]["VaccinationExecutionOwner"];
export type VaccinationExecutionRow = AppApiComponents["schemas"]["VaccinationExecutionRow"];
export type VaccinationExecutionResponse = AppApiComponents["schemas"]["VaccinationExecutionResponse"];
export type VaccinationExecutionDriveSummary = AppApiComponents["schemas"]["VaccinationExecutionDriveSummary"];
export type VaccinationExecutionShedSummary = AppApiComponents["schemas"]["VaccinationExecutionShedSummary"];
export type VaccinationExecutionShedDrilldown = AppApiComponents["schemas"]["VaccinationExecutionShedDrilldown"];
