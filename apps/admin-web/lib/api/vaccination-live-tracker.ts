import type { AppApiComponents } from "@goatos/api-client";

// Type barrel for /vaccination/live-tracker. Every alias resolves through the generated OpenAPI
// client — there is no hand-written DTO here, so a backend contract change surfaces as a typecheck
// failure rather than as a silently missing cell on the board.

export type VaccinationLiveTracker = AppApiComponents["schemas"]["VaccinationLiveTrackerResponse"];
export type LiveTrackerKPIs = AppApiComponents["schemas"]["VaccinationLiveTrackerKPIs"];
export type LiveTrackerParkCount = AppApiComponents["schemas"]["VaccinationLiveTrackerParkCount"];
export type LiveTrackerOperatorRow = AppApiComponents["schemas"]["VaccinationLiveTrackerOperatorRow"];
export type LiveTrackerShedRow = AppApiComponents["schemas"]["VaccinationLiveTrackerShedRow"];
export type LiveTrackerCombo = AppApiComponents["schemas"]["VaccinationLiveTrackerCombo"];
export type LiveTrackerComboRow = AppApiComponents["schemas"]["VaccinationLiveTrackerComboRow"];
export type LiveTrackerComboDose = AppApiComponents["schemas"]["VaccinationLiveTrackerComboDose"];
export type LiveTrackerActivity = AppApiComponents["schemas"]["VaccinationLiveTrackerActivity"];
export type LiveTrackerActivityItem = AppApiComponents["schemas"]["VaccinationLiveTrackerActivityItem"];
export type LiveTrackerAttentionRow = AppApiComponents["schemas"]["VaccinationLiveTrackerAttentionRow"];
export type LiveTrackerVerification = AppApiComponents["schemas"]["VaccinationLiveTrackerVerification"];
export type LiveTrackerFilterOption = AppApiComponents["schemas"]["VaccinationLiveTrackerFilterOption"];
export type LiveTrackerFilterOptions = AppApiComponents["schemas"]["VaccinationLiveTrackerFilterOptions"];

export type LiveTrackerOperatorState = LiveTrackerOperatorRow["state"];
export type LiveTrackerShedState = LiveTrackerShedRow["state"];
export type LiveTrackerProofState = LiveTrackerComboRow["proof_state"];
export type LiveTrackerDoseState = LiveTrackerComboDose["state"];
export type LiveTrackerActivityKind = LiveTrackerActivityItem["kind"];
export type LiveTrackerAttentionKind = LiveTrackerAttentionRow["kind"];
