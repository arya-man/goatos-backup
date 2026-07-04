// Procurement Source-Entry — DTO types.
//
// These are the generated admin-api OpenAPI schema types re-exported under the names the procurement
// feature uses, so every procurement screen and its presentation maps (features/procurement/work-state.ts)
// depend on one stable import path. Data is fetched through the server-only generated admin client in
// lib/api/procurement-server.ts — there is no client-side mock, fixture, or local adapter.
import type { AdminApiComponents } from "@goatos/api-client";

// Write request bodies (operator POST flows).
export type CreateProcurementLoadRequest = AdminApiComponents["schemas"]["CreateProcurementLoadRequest"];
export type AddProcurementLoadGoatRequest = AdminApiComponents["schemas"]["AddProcurementLoadGoatRequest"];
export type RecordProcurementSourceHealthRequest = AdminApiComponents["schemas"]["RecordProcurementSourceHealthRequest"];
export type RecordProcurementDecisionRequest = AdminApiComponents["schemas"]["RecordProcurementDecisionRequest"];
export type DispatchProcurementLoadRequest = AdminApiComponents["schemas"]["DispatchProcurementLoadRequest"];
export type RecordProcurementArrivalReviewRequest = AdminApiComponents["schemas"]["RecordProcurementArrivalReviewRequest"];
export type ProcurementArrivalGoatRequest = AdminApiComponents["schemas"]["ProcurementArrivalGoatRequest"];
export type AcceptProcurementIntakeRequest = AdminApiComponents["schemas"]["AcceptProcurementIntakeRequest"];
export type RecordProcurementHFVaccinationEvidenceRequest =
  AdminApiComponents["schemas"]["RecordProcurementHFVaccinationEvidenceRequest"];
export type ReviewProcurementHFVaccinationEvidenceRequest =
  AdminApiComponents["schemas"]["ReviewProcurementHFVaccinationEvidenceRequest"];

// Loads + load detail.
export type ProcurementLoad = AdminApiComponents["schemas"]["ProcurementLoad"];
export type ProcurementLoadListResponse = AdminApiComponents["schemas"]["ProcurementLoadListResponse"];
export type ProcurementLoadDetail = AdminApiComponents["schemas"]["ProcurementLoadDetail"];
export type ProcurementLoadDetailResponse = AdminApiComponents["schemas"]["ProcurementLoadDetailResponse"];
export type ProcurementLoadResponse = AdminApiComponents["schemas"]["ProcurementLoadResponse"];
export type ProcurementLoadGoatResponse = AdminApiComponents["schemas"]["ProcurementLoadGoatResponse"];
export type ProcurementHFVaccinationEvidenceResponse =
  AdminApiComponents["schemas"]["ProcurementHFVaccinationEvidenceResponse"];
export type ProcurementSourceHealthResponse = AdminApiComponents["schemas"]["ProcurementSourceHealthResponse"];
export type ProcurementDecisionResponse = AdminApiComponents["schemas"]["ProcurementDecisionResponse"];
export type ProcurementTransitHandoffResponse = AdminApiComponents["schemas"]["ProcurementTransitHandoffResponse"];
export type ProcurementArrivalReviewResponse = AdminApiComponents["schemas"]["ProcurementArrivalReviewResponse"];
export type ProcurementIntakeHandoffResponse = AdminApiComponents["schemas"]["ProcurementIntakeHandoffResponse"];
export type ProcurementLoadGoat = AdminApiComponents["schemas"]["ProcurementLoadGoat"];
export type ProcurementHoldingStay = AdminApiComponents["schemas"]["ProcurementHoldingStay"];
export type ProcurementSourceHealthCheck = AdminApiComponents["schemas"]["ProcurementSourceHealthCheck"];
export type ProcurementDecision = AdminApiComponents["schemas"]["ProcurementDecision"];
export type ProcurementTransitHandoff = AdminApiComponents["schemas"]["ProcurementTransitHandoff"];
export type ProcurementArrivalReview = AdminApiComponents["schemas"]["ProcurementArrivalReview"];
export type ProcurementArrivalGoat = AdminApiComponents["schemas"]["ProcurementArrivalGoat"];
export type ProcurementPCHandoff = AdminApiComponents["schemas"]["ProcurementPCHandoff"];
export type ProcurementTimelineEvent = AdminApiComponents["schemas"]["ProcurementTimelineEvent"];
export type ProcurementHFVaccinationEvidence = AdminApiComponents["schemas"]["ProcurementHFVaccinationEvidence"];

// Process-integrity lenses (Action Center / Adherence / Control Tower / Workflows).
export type ProcurementWorkRow = AdminApiComponents["schemas"]["ProcurementWorkRow"];
export type ProcurementActionCenterResponse = AdminApiComponents["schemas"]["ProcurementActionCenterResponse"];
export type ProcurementCountByWorkState = AdminApiComponents["schemas"]["ProcurementCountByWorkState"];
export type ProcurementProtocolAdherenceResponse = AdminApiComponents["schemas"]["ProcurementProtocolAdherenceResponse"];
export type ProcurementAdherenceSummary = AdminApiComponents["schemas"]["ProcurementAdherenceSummary"];
export type ProcurementAdherenceRow = AdminApiComponents["schemas"]["ProcurementAdherenceRow"];
export type ProcurementControlTowerResponse = AdminApiComponents["schemas"]["ProcurementControlTowerResponse"];
export type ProcurementControlTowerSummary = AdminApiComponents["schemas"]["ProcurementControlTowerSummary"];
export type ProcurementControlTowerAlert = AdminApiComponents["schemas"]["ProcurementControlTowerAlert"];
export type ProcurementWorkflowDrilldownResponse = AdminApiComponents["schemas"]["ProcurementWorkflowDrilldownResponse"];
export type ProcurementWorkflowNode = AdminApiComponents["schemas"]["ProcurementWorkflowNode"];

// Enums.
export type ProcurementWorkState = AdminApiComponents["schemas"]["ProcurementWorkState"];
export type ProcurementSeverity = AdminApiComponents["schemas"]["ProcurementSeverity"];
export type ProcurementLoadStatus = AdminApiComponents["schemas"]["ProcurementLoadStatus"];
export type ProcurementSelectionState = AdminApiComponents["schemas"]["ProcurementSelectionState"];
export type ProcurementGoatState = AdminApiComponents["schemas"]["ProcurementGoatState"];
export type ProcurementOwnershipState = AdminApiComponents["schemas"]["ProcurementOwnershipState"];
export type ProcurementHealthState = AdminApiComponents["schemas"]["ProcurementHealthState"];
export type ProcurementArrivalState = AdminApiComponents["schemas"]["ProcurementArrivalState"];
export type ProcurementPurpose = AdminApiComponents["schemas"]["ProcurementPurpose"];
export type ProcurementHFVaccinationReviewStatus =
  AdminApiComponents["schemas"]["ProcurementHFVaccinationReviewStatus"];
