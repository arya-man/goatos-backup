// Procurement Source-Entry — DTO types.
//
// These are the generated admin-api OpenAPI schema types re-exported under the names the procurement
// feature uses, so every procurement screen and its presentation maps (features/procurement/work-state.ts)
// depend on one stable import path. Data is fetched through the server-only generated admin client in
// lib/api/procurement-server.ts — there is no client-side mock, fixture, or local adapter.
import type { AdminApiComponents, AppApiComponents } from "@goatos/api-client";

// Sales — app-api schema types (the /sales endpoints live in app-api.yaml, not the admin API).
export type SalesOverview = AppApiComponents["schemas"]["SalesOverview"];
export type SalesOverviewSummary = AppApiComponents["schemas"]["SalesOverviewSummary"];
export type SalesOverviewMonthly = AppApiComponents["schemas"]["SalesOverviewMonthly"];
export type SalesPriceBand = AppApiComponents["schemas"]["SalesPriceBand"];
export type SalesBuyer = AppApiComponents["schemas"]["SalesBuyer"];
export type SalesMarketBenchmark = AppApiComponents["schemas"]["SalesMarketBenchmark"];
export type SalesDeal = AppApiComponents["schemas"]["SalesDeal"];
export type SalesDealPayment = AppApiComponents["schemas"]["SalesDealPayment"];
export type SalesDealPaymentWrite = AppApiComponents["schemas"]["SalesDealPaymentWrite"];
export type SalesDealStatusWrite = AppApiComponents["schemas"]["SalesDealStatusWrite"];
export type SalesDealPage = AppApiComponents["schemas"]["SalesDealPage"];
export type SalesDealWrite = AppApiComponents["schemas"]["SalesDealWrite"];

// Load-wise sales — every purchased load reconciled (/procurement/loadwise-sales).
export type LoadwiseSales = AppApiComponents["schemas"]["LoadwiseSales"];
export type LoadwiseLoad = AppApiComponents["schemas"]["LoadwiseLoad"];
export type LoadwiseSummary = AppApiComponents["schemas"]["LoadwiseSummary"];
export type LoadCostWrite = AppApiComponents["schemas"]["LoadCostWrite"];

// Feed purchases — the buying side of the feed chain (/procurement/feed-purchases).
export type FeedPurchase = AppApiComponents["schemas"]["FeedPurchase"];
export type FeedPurchasePage = AppApiComponents["schemas"]["FeedPurchasePage"];
export type FeedPurchaseWrite = AppApiComponents["schemas"]["FeedPurchaseWrite"];
export type FeedPurchaseOptions = AppApiComponents["schemas"]["FeedPurchaseOptions"];
export type FeedPurchasePayment = AppApiComponents["schemas"]["FeedPurchasePayment"];
export type FeedPurchasePaymentWrite = AppApiComponents["schemas"]["FeedPurchasePaymentWrite"];
export type FeedPurchaseStatusWrite = AppApiComponents["schemas"]["FeedPurchaseStatusWrite"];
export type FeedPurchaseEdit = AppApiComponents["schemas"]["FeedPurchaseEdit"];
export type SalesBuyerLead = AppApiComponents["schemas"]["SalesBuyerLead"];
export type SalesBuyerLeadPage = AppApiComponents["schemas"]["SalesBuyerLeadPage"];
export type SalesBuyerLeadWrite = AppApiComponents["schemas"]["SalesBuyerLeadWrite"];
export type SalesFpoLead = AppApiComponents["schemas"]["SalesFpoLead"];
export type SalesFpoLeadPage = AppApiComponents["schemas"]["SalesFpoLeadPage"];
export type SalesFpoLeadWrite = AppApiComponents["schemas"]["SalesFpoLeadWrite"];
export type SalesLeadStatusWrite = AppApiComponents["schemas"]["SalesLeadStatusWrite"];
export type SalesBenchmarkWrite = AppApiComponents["schemas"]["SalesBenchmarkWrite"];
export type SalesSoldTagsWrite = AppApiComponents["schemas"]["SalesSoldTagsWrite"];
export type SalesSoldTagsResult = AppApiComponents["schemas"]["SalesSoldTagsResult"];
export type SalesWeightCheckWrite = AppApiComponents["schemas"]["SalesWeightCheckWrite"];
export type SalesRecorded = AppApiComponents["schemas"]["SalesRecorded"];

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
