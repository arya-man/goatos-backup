import "server-only";

import { randomUUID } from "crypto";
import { createAdminApiClient, createAppApiClient, GoatOSApiError } from "@goatos/api-client";
import type { AdminApiComponents, AdminApiPaths, AppApiComponents, AppApiPaths } from "@goatos/api-client";
import { headers } from "next/headers";
import { cache } from "react";
import { resolveFirebaseIdToken } from "@/lib/auth/server-session";
import { mintLocalDevBearerToken } from "./local-dev-token";
import type { ParkScopeOption } from "./park-scope";
import { AdminBootstrapCache } from "./admin-bootstrap-cache";

type ErrorEnvelope = AppApiComponents["schemas"]["ErrorEnvelope"];

export type AdminWebBootstrapResponse = AppApiComponents["schemas"]["AdminWebBootstrapResponse"];
export type AdminWebPageContract = AppApiComponents["schemas"]["AdminWebPageContract"];
export type GoatPassportResponse = AppApiComponents["schemas"]["GoatPassportResponse"];
export type GoatSearchResponse = AppApiComponents["schemas"]["GoatSearchResponse"];
export type CountsBreakdownResponse = AppApiComponents["schemas"]["CountsBreakdownResponse"];
export type CountsBreakdownRow = AppApiComponents["schemas"]["CountsBreakdownRow"];
export type CountsBreakdownSeriesPoint = AppApiComponents["schemas"]["CountsBreakdownSeriesPoint"];
export type GoatTimelineResponse = AppApiComponents["schemas"]["GoatTimelineResponse"];
export type IdentifierType = AppApiComponents["schemas"]["IdentifierType"];
export type ActionCenterObligation = AppApiComponents["schemas"]["ActionCenterObligation"];
export type ActionCenterResponse = AppApiComponents["schemas"]["ActionCenterResponse"];
export type VaccinationQueueItem = AppApiComponents["schemas"]["VaccinationQueueItem"];
export type VaccinationQueueResponse = AppApiComponents["schemas"]["VaccinationQueueResponse"];

// Process-integrity read model — the canonical truth feeding Action Center, Protocol Adherence,
// Control Tower, and workflow drilldown. One backend projection, not the Parks physical projection.
export type WorkState = AppApiComponents["schemas"]["WorkState"];
export type ProcessIntegritySeverity = AppApiComponents["schemas"]["ProcessIntegritySeverity"];
export type ProcessIntegrityOwner = AppApiComponents["schemas"]["ProcessIntegrityOwner"];
export type ProcessIntegrityEvidence = AppApiComponents["schemas"]["ProcessIntegrityEvidence"];
export type ProcessIntegritySOPState = AppApiComponents["schemas"]["ProcessIntegritySOPState"];
export type ProcessIntegrityProofState = AppApiComponents["schemas"]["ProcessIntegrityProofState"];
export type ProcessIntegrityVerificationState = AppApiComponents["schemas"]["ProcessIntegrityVerificationState"];
export type CountByWorkState = AppApiComponents["schemas"]["CountByWorkState"];
export type AdherenceSummary = AppApiComponents["schemas"]["AdherenceSummary"];
export type AdherenceRow = AppApiComponents["schemas"]["AdherenceRow"];
export type ProtocolAdherenceResponse = AppApiComponents["schemas"]["ProtocolAdherenceResponse"];
export type ControlTowerSummary = AppApiComponents["schemas"]["ControlTowerSummary"];
export type ControlTowerAlert = AppApiComponents["schemas"]["ControlTowerAlert"];
export type ControlTowerResponse = AppApiComponents["schemas"]["ControlTowerResponse"];
export type WorkflowNode = AppApiComponents["schemas"]["WorkflowNode"];
export type WorkflowDrilldownResponse = AppApiComponents["schemas"]["WorkflowDrilldownResponse"];
export type ImpactPreviewInput = AppApiComponents["schemas"]["ImpactPreviewInput"];
export type ImpactPreviewResult = AppApiComponents["schemas"]["ImpactPreviewResult"];
export type ProtocolConfigItem = AppApiComponents["schemas"]["ProtocolConfigItem"];
export type ProtocolConfigListResponse = AppApiComponents["schemas"]["ProtocolConfigListResponse"];
export type ProtocolVersionResponse = AppApiComponents["schemas"]["ProtocolVersionResponse"];
export type AnimalStageItem = AppApiComponents["schemas"]["AnimalStageItem"];
export type AnimalStageListResponse = AppApiComponents["schemas"]["AnimalStageListResponse"];
export type VaccinationPassportDue = AppApiComponents["schemas"]["VaccinationPassportDue"];
export type VaccinationPassportHistoryItem = AppApiComponents["schemas"]["VaccinationPassportHistoryItem"];
export type VaccinationPassport = AppApiComponents["schemas"]["VaccinationPassport"];
export type CreateProofUploadResponse = AppApiComponents["schemas"]["CreateProofUploadResponse"];
export type ProofResponse = AppApiComponents["schemas"]["ProofResponse"];
export type SubmissionResponse = AppApiComponents["schemas"]["SubmissionResponse"];

export type VaccinationExecutionResponse = AppApiComponents["schemas"]["VaccinationExecutionResponse"];
export type VaccinationExecutionRow = AppApiComponents["schemas"]["VaccinationExecutionRow"];
export type VaccinationOperationsResponse = AppApiComponents["schemas"]["VaccinationOperationsResponse"];
export type VaccinationOperationsCohort = AppApiComponents["schemas"]["VaccinationOperationsCohort"];
export type VaccinationOperationsProtocol = AppApiComponents["schemas"]["VaccinationOperationsProtocol"];
export type VaccinationOperationsCell = AppApiComponents["schemas"]["VaccinationOperationsCell"];
export type VaccinationOperationsCounts = AppApiComponents["schemas"]["VaccinationOperationsCounts"];
export type VaccinationExecutionShedDrilldown = AppApiComponents["schemas"]["VaccinationExecutionShedDrilldown"];
export type VaccinationExecutionWorkState = AppApiComponents["schemas"]["VaccinationExecutionWorkState"];
export type VaccinationExecutionSeverity = AppApiComponents["schemas"]["VaccinationExecutionSeverity"];
export type VaccinationExecutionSOPStatus = AppApiComponents["schemas"]["VaccinationExecutionSOPStatus"];
export type VaccinationExecutionProofStatus = AppApiComponents["schemas"]["VaccinationExecutionProofStatus"];
export type VaccinationExecutionVerificationStatus = AppApiComponents["schemas"]["VaccinationExecutionVerificationStatus"];

// Shed-wise vaccination read model (the main /vaccination table + shed detail + capacity planner).
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
export type VaccinationCapacityConfig = AppApiComponents["schemas"]["VaccinationCapacityConfig"];
export type VaccinationOperatorAssignmentConfig = AppApiComponents["schemas"]["VaccinationOperatorAssignmentConfig"];
export type VaccinationOperatorShift = AppApiComponents["schemas"]["VaccinationOperatorShift"];
export type UpdateVaccinationOperatorAssignmentConfigRequest = AppApiComponents["schemas"]["UpdateVaccinationOperatorAssignmentConfigRequest"];
export type UpdateVaccinationCapacityConfigRequest = AppApiComponents["schemas"]["UpdateVaccinationCapacityConfigRequest"];
export type VaccinationDriveAssignmentRow = AppApiComponents["schemas"]["VaccinationDriveAssignmentRow"];
export type VaccinationDriveAssignmentResponse = AppApiComponents["schemas"]["VaccinationDriveAssignmentResponse"];
export type WeighingCampaign = AppApiComponents["schemas"]["WeighingCampaign"];
export type WeighingCampaignShed = AppApiComponents["schemas"]["WeighingCampaignShed"];
export type WeighingCampaignListResponse = AppApiComponents["schemas"]["WeighingCampaignListResponse"];
export type WeighingPlannerCatalogResponse = AppApiComponents["schemas"]["WeighingPlannerCatalogResponse"];
export type WeighingPlannerParkBucketsResponse = AppApiComponents["schemas"]["WeighingPlannerParkBucketsResponse"];
export type WeighingPlannerShed = AppApiComponents["schemas"]["WeighingPlannerShed"];
export type WeighingPlannerPark = AppApiComponents["schemas"]["WeighingPlannerPark"];
export type CreateWeighingCampaignRequest = AppApiComponents["schemas"]["CreateWeighingCampaignRequest"];
export type WeighingCampaignResponse = AppApiComponents["schemas"]["WeighingCampaignResponse"];

// CEO vaccination command board read model.
export type VaccinationCommandBoardResponse = AppApiComponents["schemas"]["VaccinationCommandBoardResponse"];
export type VaccinationCommandBoardKPI = AppApiComponents["schemas"]["VaccinationCommandBoardKPI"];
export type VaccinationCommandBoardCohortCell = AppApiComponents["schemas"]["VaccinationCommandBoardCohortCell"];
export type ShedDoseMatrixCell = AppApiComponents["schemas"]["ShedDoseMatrixCell"];
export type WeeklyGivenRow = AppApiComponents["schemas"]["WeeklyGivenRow"];
export type VerificationQueueRow = AppApiComponents["schemas"]["VerificationQueueRow"];

export type AdminGoatResponse = AdminApiComponents["schemas"]["AdminGoatResponse"];
export type CreateAdminGoatRequest = AdminApiComponents["schemas"]["CreateAdminGoatRequest"];
export type AdminGoatBulkPreviewRequest = AdminApiComponents["schemas"]["AdminGoatBulkPreviewRequest"];
export type AdminGoatBulkCommitRequest = AdminApiComponents["schemas"]["AdminGoatBulkCommitRequest"];
export type AdminGoatBulkResponse = AdminApiComponents["schemas"]["AdminGoatBulkResponse"];
export type AdminGoatBulkRowResult = AdminApiComponents["schemas"]["AdminGoatBulkRowResult"];
export type AdminGoatBulkSummary = AdminApiComponents["schemas"]["AdminGoatBulkSummary"];
export type GenerationStatus = AdminApiComponents["schemas"]["GenerationStatus"];
export type StageGoatRequest = AdminApiComponents["schemas"]["StageGoatRequest"];
export type ReproductiveGoatRequest = AdminApiComponents["schemas"]["ReproductiveGoatRequest"];
export type BulkStatusPreviewRequest = AdminApiComponents["schemas"]["BulkStatusPreviewRequest"];
export type BulkStatusPreviewResponse = AdminApiComponents["schemas"]["BulkStatusPreviewResponse"];
export type BulkStatusCommitRequest = AdminApiComponents["schemas"]["BulkStatusCommitRequest"];
export type BulkStatusCommitResponse = AdminApiComponents["schemas"]["BulkStatusCommitResponse"];
export type CreateLocationRequest = AdminApiComponents["schemas"]["CreateLocationRequest"];
export type LocationSummary = AdminApiComponents["schemas"]["LocationSummary"];
export type LocationListResponse = AdminApiComponents["schemas"]["LocationListResponse"];
export type LocationMutationResponse = AdminApiComponents["schemas"]["LocationMutationResponse"];
export type AddIdentifierRequestBody = AdminApiComponents["schemas"]["AddIdentifierRequest"];
export type RetireIdentifierRequestBody = AdminApiComponents["schemas"]["RetireIdentifierRequest"];
export type OperationsAuditRow = AdminApiComponents["schemas"]["OperationsAuditRow"];
export type OperationsAuditListResponse = AdminApiComponents["schemas"]["OperationsAuditListResponse"];
export type OperationsAuditSummaryResponse = AdminApiComponents["schemas"]["OperationsAuditSummaryResponse"];
export type OperationsAuditActorType = AdminApiComponents["parameters"]["OperationsAuditActorType"];
export type OutboxDLQMessage = AdminApiComponents["schemas"]["OutboxDLQMessage"];
export type OutboxDLQListResponse = AdminApiComponents["schemas"]["OutboxDLQListResponse"];
export type OutboxDLQActionRequest = AdminApiComponents["schemas"]["OutboxDLQActionRequest"];
export type OutboxDLQActionResponse = AdminApiComponents["schemas"]["OutboxDLQActionResponse"];
export type OperationsKernelHealthResponse = AdminApiComponents["schemas"]["OperationsKernelHealthResponse"];
export type RunVaccinationManualCampaignRequest = AdminApiComponents["schemas"]["RunVaccinationManualCampaignRequest"];
export type VaccinationGenerationRunResponse = AdminApiComponents["schemas"]["VaccinationGenerationRunResponse"];
export type OutboxDLQStatus = AdminApiComponents["parameters"]["OutboxDLQStatus"];

// SOP Library (Admin / Data Ops) — real generated admin-api types, no hand-rolled shapes.
export type SOPDefinition = AdminApiComponents["schemas"]["SOPDefinition"];
export type SOPVersion = AdminApiComponents["schemas"]["SOPVersion"];
export type SOPListResponse = AdminApiComponents["schemas"]["SOPListResponse"];
export type SOPResponse = AdminApiComponents["schemas"]["SOPResponse"];
export type SOPVersionResponse = AdminApiComponents["schemas"]["SOPVersionResponse"];
export type CreateSOPRequest = AdminApiComponents["schemas"]["CreateSOPRequest"];
export type CreateSOPVersionRequest = AdminApiComponents["schemas"]["CreateSOPVersionRequest"];
export type DryRunRequest = AdminApiComponents["schemas"]["DryRunRequest"];
export type DryRunResponse = AdminApiComponents["schemas"]["DryRunResponse"];
export type SOPValidationReport = AdminApiComponents["schemas"]["ValidationReport"];
export type ReviewTaskRequest = AdminApiComponents["schemas"]["ReviewTaskRequest"];
export type TaskResponse = AdminApiComponents["schemas"]["TaskResponse"];
export type AssignTaskRequest = AdminApiComponents["schemas"]["AssignTaskRequest"];

// Generic Verification vertical (context/architecture/verification-module-design.md +
// verifier-app-and-flow.md). Admin-web is the AUTHORITY act surface: it reads the Verifier's
// approve/reject queue and acts on the SOURCE task via the EXISTING /admin/tasks/{task_id}/verify|
// rework|assign contract below — it never writes a verdict itself (that is the standalone Verifier
// mobile app's job, gated on verification.review, built separately).
//
// Real generated app-api types — the backend Verification module (1a) landed on main and the
// client regenerated (`packages/api-client/src/generated/app-api.ts`,
// `contracts/openapi/app-api.yaml`). `/verification/queue` is now a properly typed AppApiPaths
// entry too, so `listVerificationQueue` below no longer needs the `as keyof AppApiPaths & string`
// cast.
export type VerificationItemStatus = AppApiComponents["schemas"]["VerificationItemStatus"];
export type VerificationSourceRef = AppApiComponents["schemas"]["VerificationSourceRef"];
export type VerificationMediaItem = AppApiComponents["schemas"]["VerificationMediaItem"];
export type VerificationQueueItem = AppApiComponents["schemas"]["VerificationQueueItem"];
export type VerificationQueueResponse = AppApiComponents["schemas"]["VerificationQueueResponse"];

export type ApiErrorKind =
  | "missing_config"
  | "unauthorized"
  | "tenant_scope_mismatch"
  | "permission_denied"
  | "bad_request"
  | "not_found"
  | "backend_down"
  | "api_error";

export type ApiUiError = {
  kind: ApiErrorKind;
  status?: number;
  code?: string;
  message: string;
  traceId?: string;
  retryable?: boolean;
  // BUG-019: a 409 `park_scope_ambiguous` is not a plain failure — the backend is handing back
  // the park menu a tenant-wide actor must choose from. `normalizeApiError` otherwise reshapes
  // every error into this fixed type, which DROPPED the menu before it reached the browser and
  // left the CEO on a dead-end screen. Park options stay backend-owned (golden frontend rule);
  // this field only carries them through the Next.js hop intact.
  availableParks?: ParkScopeOption[];
};

export type ApiResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: ApiUiError };

type ServerConfig = {
  baseUrl: string;
  bearerToken: string;
  tenantId: string;
  // W3C traceparent forwarded from the incoming request, when present. On a browser-initiated
  // fetch (client-side navigation or an explicit client fetch), Faro's fetch instrumentation
  // (apps/admin-web/components/observability/faro-provider.tsx) attaches this header on the
  // browser -> Next.js hop; forwarding it onto the backend call below chains the RUM trace onto
  // the backend's otelhttp span for the same request.
  traceparent?: string;
};

export type HerdSearchParams = {
  limit: number;
  cursor?: string;
  q?: string;
  goat_id?: string;
  identifier_type?: IdentifierType;
  scope_key?: string;
  breed?: string;
  sex?: string;
  farm_id?: string;
  park_id?: string;
  location_id?: string;
  status?: string;
};

export type GoatTimelineParams = {
  goatId: string;
  limit: number;
  cursor?: string;
};

export type OperationsAuditListParams = {
  limit?: number;
  cursor?: string;
  from?: string;
  to?: string;
  actorType?: OperationsAuditActorType;
  actorId?: string;
  action?: string;
  resourceType?: string;
  resourceId?: string;
  scopeType?: string;
  scopeId?: string;
  domain?: string;
  module?: string;
  category?: string;
  result?: string;
  status?: string;
  q?: string;
  anomaliesOnly?: boolean;
  proofGaps?: boolean;
};

export type OutboxDLQListParams = {
  status?: OutboxDLQStatus;
  eventType?: string;
  topic?: string;
  limit?: number;
};

export const getServerConfig = cache(async function getServerConfig(requireTenant = false): Promise<ApiResult<ServerConfig>> {
  const baseUrl = process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080";
  const firebaseIdToken = await resolveFirebaseIdToken();
  // Local bearer mode: self-mint a FRESH token per request (never the stale boot-time token) so the dev
  // server can't 401 with invalid_bearer_token after its original token's TTL lapses. Falls back to the
  // static GOATOS_BEARER_TOKEN only if self-minting isn't possible (e.g. dev secret unset). See
  // lib/api/local-dev-token.ts — strictly local-only.
  const localBearerToken =
    process.env.GOATOS_ENV === "local" && process.env.GOATOS_AUTH_MODE === "bearer"
      ? (mintLocalDevBearerToken() ?? process.env.GOATOS_BEARER_TOKEN)
      : undefined;
  const bearerToken = localBearerToken ?? firebaseIdToken;
  const tenantId = process.env.GOATOS_TENANT_ID;
  const traceparent = (await headers()).get("traceparent") ?? undefined;

  if (!bearerToken) {
    return {
      ok: false,
      error: {
        kind: "unauthorized",
        status: 401,
        code: "firebase_session_missing",
        message: "Sign in with Google to start or refresh your admin session.",
      },
    };
  }
  if (requireTenant && !tenantId) {
    return {
      ok: false,
      error: {
        kind: "missing_config",
        message: "Server configuration missing: tenant id.",
      },
    };
  }

  return {
    ok: true,
    data: {
      baseUrl,
      bearerToken: bearerToken ?? "",
      tenantId: tenantId ?? "",
      traceparent,
    },
  };
});

export function getAdminRuntimeStatus() {
  return {
    baseUrl: process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080",
    hasLocalBearerFallback:
      process.env.GOATOS_ENV === "local" &&
      process.env.GOATOS_AUTH_MODE === "bearer" &&
      Boolean(process.env.GOATOS_BEARER_TOKEN),
    hasTenantId: Boolean(process.env.GOATOS_TENANT_ID),
  };
}

export function apiClientOptions(config: ServerConfig) {
  const traceparent = config.traceparent;
  return {
    baseUrl: config.baseUrl,
    bearerToken: config.bearerToken,
    tenantId: config.tenantId || undefined,
    // See the ServerConfig.traceparent comment: forwards the browser's Faro-instrumented trace
    // context (if any) onto the backend call so RUM and backend spans join one trace.
    getTraceHeaders: traceparent ? () => ({ traceparent }) : undefined,
  };
}

export function isAuthRequiredError(error: ApiUiError): boolean {
  return error.kind === "unauthorized" || error.status === 401;
}

export function firstAuthRequiredError(
  ...results: Array<ApiResult<unknown> | null | undefined>
): ApiUiError | null {
  for (const result of results) {
    if (result && !result.ok && isAuthRequiredError(result.error)) {
      return result.error;
    }
  }
  return null;
}

const adminBootstrapCache = new AdminBootstrapCache<AdminWebBootstrapResponse>();

export const getAdminWebBootstrap = cache(async (): Promise<ApiResult<AdminWebBootstrapResponse>> => {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => adminBootstrapCache.get(config.data, async (etag) => {
    const result = await client.requestWithResponse<AdminWebBootstrapResponse>("/admin-web/bootstrap", {
      cache: "no-store",
      headers: etag ? { "If-None-Match": etag } : undefined,
    });
    return {
      data: result.data,
      status: result.response.status,
      etag: result.response.headers.get("ETag"),
    };
  }));
});

export async function getAdminWebPageContract(routeId: string): Promise<AdminWebPageContract | null> {
  const contract = await getAdminWebBootstrap();
  if (!contract.ok) return null;
  return contract.data.pages.find((page) => page.route_id === routeId) ?? null;
}

export async function requireAdminWebPageContract(routeId: string): Promise<AdminWebPageContract> {
  const contract = await getAdminWebBootstrap();
  if (!contract.ok) {
    throw new Error(`Admin-web contract unavailable for ${routeId}: ${contract.error.code ?? contract.error.kind}`);
  }
  const page = contract.data.pages.find((item) => item.route_id === routeId);
  if (!page) {
    throw new Error(`Admin-web page contract missing route_id=${routeId}`);
  }
  return page;
}

export async function searchGoats(params: HerdSearchParams): Promise<ApiResult<GoatSearchResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<GoatSearchResponse>("/goats/search", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// Herd Register summary read model. The row list stays on bounded /goats/search
// (searchGoats) because those render fields (tag1/tag2, weight, health, breeding)
// are separate from the KPI totals; totals come from canonical scoped goat counts.

export type HerdRegisterSummaryCounts = {
  parkId: string | null;
  farmId: string | null;
  currentLocationId: string | null;
  breed: string | null;
  sex: string;
  lifecycleStatus: string;
  totalCount: number;
  activeCount: number;
  adultCount: number;
  kidCount: number;
  untaggedKidCount: number;
  deadCount: number;
  soldCount: number;
  culledCount: number;
  projectedAt: string;
};

export type HerdRegisterSummaryResponse = {
  items: HerdRegisterSummaryCounts[];
};

export type HerdRegisterSummaryParams = {
  lifecycle_status?: string;
  park_id?: string;
  breed?: string;
  sex?: string;
};

/** Read exact summary counts from canonical goats. */
export async function getHerdRegisterSummary(
  params: HerdRegisterSummaryParams,
): Promise<ApiResult<HerdRegisterSummaryResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<HerdRegisterSummaryResponse>("/herd-register/summary", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export type CountsBreakdownParams = {
  farm_id?: string;
  park_id?: string;
  shed_id?: string;
  management_stage?: string;
  breed?: string;
  sex?: string;
  lifecycle_status?: string;
  limit?: number;
  offset?: number;
};

/**
 * Counts Breakdown census. One call returns the page of grain rows, the whole-result totals,
 * the four chart series, and the filter facets — so the page never fans out one fetch per
 * chart. `total_count` and every chart series are rolled up server-side over the FULL filtered
 * set, so they must be read from the response, never recomputed from the returned page.
 */
export async function getCountsBreakdown(
  params: CountsBreakdownParams,
): Promise<ApiResult<CountsBreakdownResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CountsBreakdownResponse>("/counts/breakdown", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// ---------------------------------------------------------------------------------------------
// Feed vertical — Direction, Packing and Config.
//
// Two facts drive every signature below and must survive any refactor:
//
//  1. BLOCKED IS NOT ZERO. `FeedDirectionItemQuantity.quantity_kg` is null IF AND ONLY IF
//     `status === "blocked"`, meaning no ration was ever authored for that (ration group, shed tag,
//     feed item). A blocked cell has NO number: it must never be defaulted, summed, or coerced to 0
//     anywhere in this layer or above it — that is the difference between "this shed eats nothing of
//     this item on purpose" and "this shed goes unfed and the sheet looked complete".
//     An authored `"0.000"` with `status: "resolved"` is the opposite state and is fully numeric.
//
//  2. QUANTITIES ARE EXACT DECIMAL STRINGS, never JSON numbers, so an authored rate cannot drift
//     through a float round trip. They stay strings all the way to the DOM; nothing here parses them.
//
// The read pages page by offset and get `has_more` rather than a total — counting the filtered set on
// every request would be compute-on-read — so the pager is prev/next, not numbered.
export type FeedDirectionPreviewPage = AppApiComponents["schemas"]["FeedDirectionPreviewPage"];
export type FeedDirectionRow = AppApiComponents["schemas"]["FeedDirectionRow"];
export type FeedDirectionItemQuantity = AppApiComponents["schemas"]["FeedDirectionItemQuantity"];
export type FeedDirectionPreviewSummary = AppApiComponents["schemas"]["FeedDirectionPreviewSummary"];
export type FeedDirectionLifecycle = AppApiComponents["schemas"]["FeedDirectionLifecycle"];
export type FeedDirectionWorkflowLifecycle = AppApiComponents["schemas"]["FeedDirectionWorkflowLifecycle"];
export type FeedPackingWorklistPage = AppApiComponents["schemas"]["FeedPackingWorklistPage"];
export type FeedPackingRow = AppApiComponents["schemas"]["FeedPackingRow"];
export type FeedConfigRationRatePage = AppApiComponents["schemas"]["FeedConfigRationRatePage"];
export type FeedConfigRationRate = AppApiComponents["schemas"]["FeedConfigRationRate"];
export type FeedConfigShedFactorPage = AppApiComponents["schemas"]["FeedConfigShedFactorPage"];
export type FeedConfigShedFactor = AppApiComponents["schemas"]["FeedConfigShedFactor"];
export type FeedConfigSessionTemplatePage = AppApiComponents["schemas"]["FeedConfigSessionTemplatePage"];
export type FeedConfigSessionTemplate = AppApiComponents["schemas"]["FeedConfigSessionTemplate"];
export type FeedConfigSchedulePage = AppApiComponents["schemas"]["FeedConfigSchedulePage"];
export type FeedConfigSchedule = AppApiComponents["schemas"]["FeedConfigSchedule"];
export type FeedConfigFeedItemPage = AppApiComponents["schemas"]["FeedConfigFeedItemPage"];
export type FeedConfigFeedItem = AppApiComponents["schemas"]["FeedConfigFeedItem"];
export type FeedConfigRationGroupPage = AppApiComponents["schemas"]["FeedConfigRationGroupPage"];
export type FeedConfigShedTagPage = AppApiComponents["schemas"]["FeedConfigShedTagPage"];
export type FeedConfigWriteResult = AppApiComponents["schemas"]["FeedConfigWriteResult"];
export type UpsertFeedConfigRationRateRequest = AppApiComponents["schemas"]["UpsertFeedConfigRationRateRequest"];
export type UpsertFeedConfigShedFactorRequest = AppApiComponents["schemas"]["UpsertFeedConfigShedFactorRequest"];
export type UpsertFeedConfigScheduleRequest = AppApiComponents["schemas"]["UpsertFeedConfigScheduleRequest"];
export type FeedConfigExperimentPage = AppApiComponents["schemas"]["FeedConfigExperimentPage"];
export type FeedConfigExperiment = AppApiComponents["schemas"]["FeedConfigExperiment"];
export type UpsertFeedConfigExperimentRequest = AppApiComponents["schemas"]["UpsertFeedConfigExperimentRequest"];
export type SetFeedConfigExperimentShedStatusRequest =
  AppApiComponents["schemas"]["SetFeedConfigExperimentShedStatusRequest"];

export type FeedDirectionPreviewParams = {
  /** Required: the ration grid, the session split and the dispatch clock are all park-scoped. */
  park_id: string;
  /** Required: the feed day as an Asia/Kolkata business date, never an instant. */
  target_date: string;
  shed_id?: string;
  session?: number;
  limit?: number;
  offset?: number;
};

export async function getFeedDirectionPreview(
  params: FeedDirectionPreviewParams,
): Promise<ApiResult<FeedDirectionPreviewPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedDirectionPreviewPage>("/feed-direction/preview", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getFeedPackingWorklist(params: {
  park_id: string;
  target_date: string;
  limit?: number;
  offset?: number;
}): Promise<ApiResult<FeedPackingWorklistPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPackingWorklistPage>("/feed-packing/worklist", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function listFeedConfigRationRates(params: {
  park_id: string;
  ration_group?: string;
  shed_tag?: string;
  feed_item?: string;
  limit?: number;
  offset?: number;
}): Promise<ApiResult<FeedConfigRationRatePage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigRationRatePage>("/feed-config/ration-rates", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function listFeedConfigShedFactors(params: {
  park_id: string;
  shed_id?: string;
  feed_item?: string;
  limit?: number;
  offset?: number;
}): Promise<ApiResult<FeedConfigShedFactorPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigShedFactorPage>("/feed-config/shed-factors", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function listFeedConfigSessionTemplates(params: {
  park_id: string;
  limit?: number;
  offset?: number;
}): Promise<ApiResult<FeedConfigSessionTemplatePage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigSessionTemplatePage>("/feed-config/session-templates", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function listFeedConfigSchedule(params: {
  park_id: string;
  workflow?: string;
  limit?: number;
  offset?: number;
}): Promise<ApiResult<FeedConfigSchedulePage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigSchedulePage>("/feed-config/schedule", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function listFeedConfigFeedItems(params: {
  limit?: number;
  offset?: number;
} = {}): Promise<ApiResult<FeedConfigFeedItemPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigFeedItemPage>("/feed-config/feed-items", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function listFeedConfigRationGroups(params: {
  limit?: number;
  offset?: number;
} = {}): Promise<ApiResult<FeedConfigRationGroupPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigRationGroupPage>("/feed-config/ration-groups", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function listFeedConfigShedTags(params: {
  limit?: number;
  offset?: number;
} = {}): Promise<ApiResult<FeedConfigShedTagPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigShedTagPage>("/feed-config/shed-tags", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// The three Feed Config writes. Each is effective-dated server-side (an earlier day's row is CLOSED
// and a new one opened, so history survives) and each REQUIRES an Idempotency-Key: an exact replay
// returns the original result with `idempotent_replay: true`, and the same key with a different
// payload is a 409.
//
// `grams_per_head` / `multiplier` are REQUIRED numbers here on purpose — the caller must decide
// between "author this value" and "leave it unconfigured" BEFORE reaching this layer. There is
// deliberately no optional/undefined variant that this function could quietly turn into 0, because
// absence of a rate means "not configured" (blocking) and 0 means "feed nothing" (correct for
// milk-fed kids). Out-of-range values are forwarded verbatim so the backend rejects them; nothing
// here clamps or rounds.
export async function upsertFeedConfigRationRate(
  body: UpsertFeedConfigRationRateRequest,
  idempotencyKey = `feed-ration-rate-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/ration-rates", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/**
 * One bounded page of a park's hand-authored experiment sheds.
 *
 * `status` is left off by default on purpose: a withdrawn shed's rows are retired rather than
 * deleted, and the config screen must keep showing them. They are the authored quantities that come
 * back if the shed is restored, and hiding them would make an accidental withdrawal invisible on the
 * very screen that owns that decision.
 */
export async function listFeedConfigExperiment(params: {
  park_id: string;
  shed_id?: string;
  status?: "active" | "retired";
  limit?: number;
  offset?: number;
}): Promise<ApiResult<FeedConfigExperimentPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigExperimentPage>("/feed-config/experiment", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// `absolute_kg` is a REQUIRED number here for the same reason `grams_per_head` is above, and the
// failure mode is quieter: a missing ration rate BLOCKS a shed visibly, while a missing experiment
// row silently drops the shed back onto the per-head grid and prints a complete-looking sheet with
// roughly twice the authored quantity. There is deliberately no optional variant this function could
// turn into 0. `head_count` may be null ("not recorded") but must never be invented.
export async function upsertFeedConfigExperiment(
  body: UpsertFeedConfigExperimentRequest,
  idempotencyKey = `feed-experiment-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/experiment", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/**
 * Switch a whole shed between the experiment workflow and the normal per-head ration grid.
 *
 * This changes WHAT THE ANIMALS ARE FED, not what is displayed: active feeds the shed its authored
 * absolute kg, retired returns it to projected head count x grams per head x shed factor.
 */
export async function setFeedConfigExperimentShedStatus(
  body: SetFeedConfigExperimentShedStatusRequest,
  idempotencyKey = `feed-experiment-status-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/experiment/shed-status", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function upsertFeedConfigShedFactor(
  body: UpsertFeedConfigShedFactorRequest,
  idempotencyKey = `feed-shed-factor-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/shed-factors", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function upsertFeedConfigSchedule(
  body: UpsertFeedConfigScheduleRequest,
  idempotencyKey = `feed-schedule-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/schedule", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getGoatPassport(goatId: string): Promise<ApiResult<GoatPassportResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/goats/${encodeURIComponent(goatId)}` as keyof AppApiPaths & string;
  return request(() => client.request<GoatPassportResponse>(path, { cache: "no-store" }));
}

export async function getGoatTimeline(params: GoatTimelineParams): Promise<ApiResult<GoatTimelineResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/goats/${encodeURIComponent(params.goatId)}/timeline` as keyof AppApiPaths & string;
  return request(() =>
    client.request<GoatTimelineResponse>(path, {
      cache: "no-store",
      query: compactQuery({
        limit: params.limit,
        cursor: params.cursor,
      }),
    }),
  );
}

// Action Center — the canonical process-integrity rows for vaccination work. Real dedicated endpoint
// (/vaccination/action-center), not the Parks physical projection. Filters + counts are server-side.
export async function getVaccinationActionCenter(
  params: {
    parkId?: string;
    shedId?: string;
    workState?: WorkState;
    severity?: ProcessIntegritySeverity;
    asOf?: string;
    dueBefore?: string;
    cursor?: string;
    limit?: number;
  } = {},
): Promise<ApiResult<ActionCenterResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const result = await request(() =>
    client.request<ActionCenterResponse>("/vaccination/action-center", {
      cache: "no-store",
      query: compactQuery({
        park_id: params.parkId,
        shed_id: params.shedId,
        work_state: params.workState,
        severity: params.severity,
        as_of: params.asOf,
        due_before: params.dueBefore,
        cursor: params.cursor,
        limit: params.limit ?? 200,
      }),
    }),
  );
  if (!result.ok) return result;
  return { ok: true, data: absolutizeActionCenterMedia(result.data, config.data.baseUrl) };
}

export async function getVaccinationActionCenterCounts(
  params: {
    parkId?: string;
    shedId?: string;
    workState?: WorkState;
    severity?: ProcessIntegritySeverity;
    asOf?: string;
    dueBefore?: string;
  } = {},
): Promise<ApiResult<Pick<ActionCenterResponse, "source" | "counts_by_work_state" | "total_count">>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<Pick<ActionCenterResponse, "source" | "counts_by_work_state" | "total_count">>("/vaccination/action-center/counts" as keyof AppApiPaths & string, {
      cache: "no-store",
      query: compactQuery({
        park_id: params.parkId,
        shed_id: params.shedId,
        work_state: params.workState,
        severity: params.severity,
        as_of: params.asOf,
        due_before: params.dueBefore,
      }),
    }),
  );
}

// Protocol Adherence — per rule/drive/cohort expected-vs-actual ledger (real /vaccination/adherence).
export async function getVaccinationAdherence(
  params: {
    parkId?: string;
    shedId?: string;
    workState?: WorkState;
    severity?: ProcessIntegritySeverity;
    asOf?: string;
    cursor?: string;
    limit?: number;
  } = {},
): Promise<ApiResult<ProtocolAdherenceResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const result = await request(() =>
    client.request<ProtocolAdherenceResponse>("/vaccination/adherence", {
      cache: "no-store",
      query: compactQuery({
        park_id: params.parkId,
        shed_id: params.shedId,
        work_state: params.workState,
        severity: params.severity,
        as_of: params.asOf,
        cursor: params.cursor,
        limit: params.limit ?? 200,
      }),
    }),
  );
  if (!result.ok) return result;
  return { ok: true, data: absolutizeAdherenceMedia(result.data, config.data.baseUrl) };
}

// Control Tower — exception-only leadership summary + alerts (real /control-tower/vaccination).
export async function getVaccinationControlTower(
  params: {
    parkId?: string;
    shedId?: string;
    asOf?: string;
    dueBefore?: string;
    workState?: WorkState;
    severity?: ProcessIntegritySeverity;
    cursor?: string;
    limit?: number;
  } = {},
): Promise<ApiResult<ControlTowerResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ControlTowerResponse>("/control-tower/vaccination", {
      cache: "no-store",
      query: compactQuery({
        park_id: params.parkId,
        shed_id: params.shedId,
        as_of: params.asOf,
        due_before: params.dueBefore,
        work_state: params.workState,
        severity: params.severity,
        cursor: params.cursor,
        limit: params.limit,
      }),
    }),
  );
}

// Workflow drilldown — config -> obligation -> drive -> SOP -> proof -> verify -> completion chain
// for a single Action Center / Adherence row (real /vaccination/workflows/{row_id}).
export async function getVaccinationWorkflowDrilldown(
  rowId: string,
): Promise<ApiResult<WorkflowDrilldownResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/workflows/${encodeURIComponent(rowId)}` as keyof AppApiPaths & string;
  return request(() => client.request<WorkflowDrilldownResponse>(path, { cache: "no-store" }));
}

export async function getVaccinationVerificationQueue(
  params: { parkId?: string; limit?: number; cursor?: string } = {},
): Promise<ApiResult<VaccinationQueueResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationQueueResponse>("/vaccination/verification-queue", {
      cache: "no-store",
      query: compactQuery({ park_id: params.parkId, limit: params.limit ?? 100, cursor: params.cursor }),
    }),
  );
}

export async function getVaccinationExecution(
  params: { parkId?: string; workState?: VaccinationExecutionWorkState; asOf?: string; limit?: number; cursor?: string } = {},
): Promise<ApiResult<VaccinationExecutionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationExecutionResponse>("/vaccination/execution", {
      cache: "no-store",
      query: compactQuery({ park_id: params.parkId, work_state: params.workState, as_of: params.asOf, limit: params.limit, cursor: params.cursor }),
    }),
  );
}

// Source-backed vaccination operations read model — cohort × protocol matrix + per-cohort detail with real
// last_dose. Powers the /vaccination matrix + cohort-detail sections (NOT the Action Center pivot).
export async function getVaccinationOperations(
  params: { parkId?: string; asOf?: string; dueBefore?: string; limit?: number; cursor?: string } = {},
): Promise<ApiResult<VaccinationOperationsResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(6000, (signal) =>
      client.request<VaccinationOperationsResponse>("/vaccination/operations", {
        cache: "no-store",
        signal,
        query: compactQuery({ park_id: params.parkId, as_of: params.asOf, due_before: params.dueBefore, limit: params.limit, cursor: params.cursor }),
      }),
    ),
  );
}

// Full Schedule reads a canonical server-side monthly window; there is no schedule projection warmup.
export async function getVaccinationSchedule(
  params: { parkId?: string; year: number; month: number; limit?: number; cursor?: string } = { year: new Date().getFullYear(), month: new Date().getMonth() + 1 },
): Promise<ApiResult<VaccinationOperationsResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(2500, (signal) =>
      client.request<VaccinationOperationsResponse>("/vaccination/schedule", {
        cache: "no-store",
        signal,
        query: compactQuery({ park_id: params.parkId, year: params.year, month: params.month, limit: params.limit, cursor: params.cursor }),
      }),
    ),
  );
}

// Operator-cap drive ledger: one row per planned date × operator × physical shed × partition.
export async function getVaccinationDriveAssignments(
  params: { parkId?: string; year: number; month: number; limit?: number } = { year: new Date().getFullYear(), month: new Date().getMonth() + 1 },
): Promise<ApiResult<VaccinationDriveAssignmentResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(2500, (signal) =>
      client.request<VaccinationDriveAssignmentResponse>("/vaccination/drive-assignments", {
        cache: "no-store",
        signal,
        query: compactQuery({ park_id: params.parkId, year: params.year, month: params.month, limit: params.limit }),
      }),
    ),
  );
}

export async function getWeighingCampaigns(
  params: { cursor?: string; limit?: number } = {},
): Promise<ApiResult<WeighingCampaignListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(2500, (signal) =>
      client.request<WeighingCampaignListResponse>("/weighing/campaigns", {
        cache: "no-store",
        signal,
        query: compactQuery({ cursor: params.cursor, limit: params.limit }),
      }),
    ),
  );
}

export async function getWeighingPlannerCatalog(
  periodStartDate: string,
): Promise<ApiResult<WeighingPlannerCatalogResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(2500, (signal) =>
      client.request<WeighingPlannerCatalogResponse>("/app/weighing/planner/catalog", {
        cache: "no-store",
        signal,
        query: compactQuery({ period_start_date: periodStartDate }),
      }),
    ),
  );
}

export async function getWeighingPlannerParkBuckets(
  parkId: string,
  periodStartDate: string,
  cursor?: string,
  limit = 100,
): Promise<ApiResult<WeighingPlannerParkBucketsResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  // The generated path union carries the `{park_id}` template, so the interpolated
  // concrete path is asserted back onto it -- same shape as reproductiveGoat above.
  const path = `/app/weighing/planner/parks/${encodeURIComponent(parkId)}/buckets` as keyof AppApiPaths & string;
  return request(() =>
    withApiTimeout(2500, (signal) =>
      client.request<WeighingPlannerParkBucketsResponse>(
        path,
        {
          cache: "no-store",
          signal,
          query: compactQuery({ period_start_date: periodStartDate, cursor, limit }),
        },
      ),
    ),
  );
}

export async function createWeighingCampaign(
  body: CreateWeighingCampaignRequest,
  idempotencyKey = `weighing-campaign-create-${randomUUID()}`,
): Promise<ApiResult<WeighingCampaignResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<WeighingCampaignResponse>("/weighing/campaigns", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function updateWeighingCampaign(
  campaignId: string,
  body: CreateWeighingCampaignRequest,
  idempotencyKey = `weighing-campaign-update-${campaignId}-${randomUUID()}`,
): Promise<ApiResult<WeighingCampaignResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/weighing/campaigns/${campaignId}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<WeighingCampaignResponse>(path, {
      method: "PUT",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function publishWeighingCampaign(
  campaignId: string,
  idempotencyKey = `weighing-campaign-publish-${campaignId}-${randomUUID()}`,
): Promise<ApiResult<WeighingCampaignResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/weighing/campaigns/${campaignId}/publish` as keyof AppApiPaths & string;
  return request(() =>
    client.request<WeighingCampaignResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
    }),
  );
}

export async function postponeVaccinationDriveDate(body: {
  park_id: string;
  vaccine_code: string;
  original_drive_date: string;
  override_date: string;
  reason: string;
}, idempotencyKey = `vaccination-drive-date-override-${randomUUID()}`): Promise<ApiResult<Record<string, unknown>>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<Record<string, unknown>>("/vaccination/schedule/drive-date-overrides", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getVaccinationExecutionShedDrilldown(
  shedId: string,
  params: { asOf?: string } = {},
): Promise<ApiResult<VaccinationExecutionShedDrilldown>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/execution/sheds/${encodeURIComponent(shedId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<VaccinationExecutionShedDrilldown>(path, {
      cache: "no-store",
      query: compactQuery({ as_of: params.asOf }),
    }),
  );
}

// Shed-wise vaccination summary — the MAIN /vaccination table (one row per shed, animal-level Due/Done,
// planned Sessions, capacity, merged Status). Filters (park/shed/status/capacity/search) + offset
// pagination are applied server-side.
export async function getVaccinationShedSummary(
  params: {
    parkId?: string;
    shedId?: string;
    status?: VaccinationShedStatus;
    capacity?: VaccinationCapacityStatus;
    search?: string;
    sort?: VaccinationShedSortKey;
    limit?: number;
    offset?: number;
    asOf?: string;
  } = {},
): Promise<ApiResult<VaccinationShedSummaryResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationShedSummaryResponse>("/vaccination/sheds", {
      cache: "no-store",
      query: compactQuery({
        park_id: params.parkId,
        shed_id: params.shedId,
        status: params.status,
        capacity: params.capacity,
        q: params.search,
        sort: params.sort,
        limit: params.limit,
        offset: params.offset,
        as_of: params.asOf,
      }),
    }),
  );
}

export async function getVaccinationShedDetail(
  shedId: string,
  params: { asOf?: string } = {},
): Promise<ApiResult<VaccinationShedDetail>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/sheds/${encodeURIComponent(shedId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<VaccinationShedDetail>(path, {
      cache: "no-store",
      query: compactQuery({ as_of: params.asOf }),
    }),
  );
}

export async function getVaccinationShedAnimals(
  shedId: string,
  params: { cursor?: string; limit?: number; asOf?: string; driveDueDate?: string } = {},
): Promise<ApiResult<VaccinationShedAnimalPage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/sheds/${encodeURIComponent(shedId)}/animals` as keyof AppApiPaths & string;
  return request(() =>
    client.request<VaccinationShedAnimalPage>(path, {
      cache: "no-store",
      query: compactQuery({ cursor: params.cursor, limit: params.limit, as_of: params.asOf, drive_due_date: params.driveDueDate }),
    }),
  );
}

// Admin daily operator animal capacity config (Config screen). Capacity is authored through protocol publish;
// this endpoint is read-only so the planner can show the published values.
export async function getVaccinationCapacityConfig(): Promise<ApiResult<VaccinationCapacityConfig>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationCapacityConfig>("/vaccination/capacity-config", { cache: "no-store" }),
  );
}

// Admin vaccination operator assignment config (N + default operator per park, shift assignments).
// Returns the park's active-operators-per-day + default-operator config plus every operator's shift.
export async function getVaccinationOperatorAssignmentConfig(parkId?: string): Promise<ApiResult<VaccinationOperatorAssignmentConfig>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationOperatorAssignmentConfig>("/vaccination/operator-assignment/config", {
      method: "GET",
      query: parkId ? { park_id: parkId } : {},
      cache: "no-store",
    }),
  );
}

// CEO vaccination command board — KPIs, cohort matrix, shed dose matrix, weekly given, verification queue.
export async function getVaccinationCommandBoard(params: {
  driveBatchId?: string;
  parkId?: string;
  asOf?: string;
} = {}): Promise<ApiResult<VaccinationCommandBoardResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationCommandBoardResponse>("/vaccination/command", {
      cache: "no-store",
      query: compactQuery({
        drive_batch_id: params.driveBatchId,
        park_id: params.parkId,
        as_of: params.asOf,
      }),
    }),
  );
}

// Admin update vaccination operator assignment config (validate-or-reject, optimistic concurrency via rowVersion).
export async function putVaccinationOperatorAssignmentConfig(
  body: UpdateVaccinationOperatorAssignmentConfigRequest
): Promise<ApiResult<VaccinationOperatorAssignmentConfig>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationOperatorAssignmentConfig>("/vaccination/operator-assignment/config", {
      method: "PUT",
      cache: "no-store",
      body,
    }),
  );
}

// Admin update vaccination capacity config (common operator daily animal cap + per-animal shot-cap
// override). Validate-or-reject, optimistic concurrency via rowVersion. The backend write emits
// vaccination.capacity.changed per active park, which re-plans all future vaccination drives.
export async function putVaccinationCapacityConfig(
  body: UpdateVaccinationCapacityConfigRequest
): Promise<ApiResult<VaccinationCapacityConfig>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationCapacityConfig>("/vaccination/capacity-config", {
      method: "PUT",
      cache: "no-store",
      body,
    }),
  );
}

export async function previewVaccinationImpact(body: ImpactPreviewInput): Promise<ApiResult<ImpactPreviewResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<ImpactPreviewResult>("/protocols/vaccination/impact-preview", { method: "POST", cache: "no-store", body }));
}

// listProtocolConfigs reads the Config authority list (B3): every protocol version (draft/published/
// retired) in a category with rule count, source-review state, linked SOP, effective window, and
// publisher metadata. Read-only; rows are never fabricated — an empty list renders the empty state.
export async function listProtocolConfigs(category: string): Promise<ApiResult<ProtocolConfigListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ProtocolConfigListResponse>("/protocols", {
      cache: "no-store",
      query: compactQuery({ category }),
    }),
  );
}

export async function getProtocolVersion(versionId: string): Promise<ApiResult<ProtocolVersionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/versions/${encodeURIComponent(versionId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<ProtocolVersionResponse>(path, { cache: "no-store" }),
  );
}

// listAnimalStages reads the tenant's active animal-stage reference data (animal_stage_lookup) so the
// Config authoring stage picker is backend-driven, not hardcoded K0/K1/K2 literals (Preventive Care (PC) vaccination
// TRD). Read-only; an empty list is honest — the editor shows a seed-stages state, never fallback codes.
export async function listAnimalStages(): Promise<ApiResult<AnimalStageListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<AnimalStageListResponse>("/protocols/animal-stages", { cache: "no-store" }),
  );
}

export async function createProtocolDefinition(body: {
  code: string;
  name: string;
  category: string;
}, idempotencyKey = `protocol-definition-${randomUUID()}`): Promise<ApiResult<{ protocol_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<{ protocol_id: string }>("/protocols", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function createProtocolVersion(
  protocolId: string,
  body: {
    scope_type: string;
    scope_id?: string;
    version?: number;
    version_label?: string;
    effective_from: string;
    rule_dsl: unknown;
    proof_policy?: unknown;
    sop_version_id?: string;
  },
  idempotencyKey = `protocol-version-${randomUUID()}`,
): Promise<ApiResult<{ protocol_version_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/${encodeURIComponent(protocolId)}/versions` as keyof AppApiPaths & string;
  return request(() =>
    client.request<{ protocol_version_id: string }>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function addProtocolRule(
  versionId: string,
  body: Record<string, unknown>,
  idempotencyKey = `protocol-rule-${randomUUID()}`,
): Promise<ApiResult<{ rule_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/versions/${encodeURIComponent(versionId)}/rules` as keyof AppApiPaths & string;
  return request(() =>
    client.request<{ rule_id: string }>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function publishProtocolVersion(versionId: string, idempotencyKey = `protocol-publish-${randomUUID()}`): Promise<ApiResult<Record<string, never>>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/versions/${encodeURIComponent(versionId)}/publish` as keyof AppApiPaths & string;
  return request(() =>
    client.request<Record<string, never>>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
    }),
  );
}

export async function getGoatVaccinationPassport(goatId: string): Promise<ApiResult<VaccinationPassport>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/goats/${encodeURIComponent(goatId)}/passport` as keyof AppApiPaths & string;
  return request(() => client.request<VaccinationPassport>(path, { cache: "no-store" }));
}

export async function verifySopTask(taskId: string, body: ReviewTaskRequest): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}/verify` as keyof AdminApiPaths & string;
  return request(() => client.request<TaskResponse>(path, { method: "POST", cache: "no-store", body }));
}

export async function requestSopTaskRework(taskId: string, body: ReviewTaskRequest): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}/rework` as keyof AdminApiPaths & string;
  return request(() => client.request<TaskResponse>(path, { method: "POST", cache: "no-store", body }));
}

// Current row_version for one SOP task (GET /admin/tasks/{task_id}). The Verification queue item's
// own row_version guards the verification_item row, NOT the source SOP task — the authority act
// actions below (rework / re-assign) need a FRESH task row_version for optimistic concurrency, so
// the verification-review drawer fetches this once per selected item before submitting either form.
export async function getSopTask(taskId: string): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<TaskResponse>(path, { cache: "no-store" }));
}

// Re-assign / assign a task to another operator (POST /admin/tasks/{task_id}/assign). Used by the
// verification-review AUTHORITY act drawer's "Re-assign" action.
export async function assignSopTask(taskId: string, body: AssignTaskRequest): Promise<ApiResult<TaskResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/tasks/${encodeURIComponent(taskId)}/assign` as keyof AdminApiPaths & string;
  return request(() => client.request<TaskResponse>(path, { method: "POST", cache: "no-store", body }));
}

// The Verifier's read-only media queue (GET /verification/queue, real generated app-api contract).
// NOTE the contract has NO park_id query filter yet — the drawer/list below narrow the
// already-fetched bounded page by the top-bar park scope client-side (small page, ~20 rows, never
// a full-table read) until a park_id query param is added server-side.
export async function listVerificationQueue(
  params: { category?: string; vertical?: string; module?: string; status?: VerificationItemStatus; cursor?: string; limit?: number } = {},
): Promise<ApiResult<VerificationQueueResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const result = await request(() =>
    client.request<VerificationQueueResponse>("/verification/queue", {
      cache: "no-store",
      query: compactQuery({
        category: params.category,
        vertical: params.vertical,
        module: params.module,
        status: params.status,
        cursor: params.cursor,
        limit: params.limit ?? 20,
      }),
    }),
  );
  if (!result.ok) return result;
  return { ok: true, data: absolutizeVerificationMedia(result.data, config.data.baseUrl) };
}

function absolutizeVerificationMedia(queue: VerificationQueueResponse, baseUrl: string): VerificationQueueResponse {
  return {
    ...queue,
    items: queue.items.map((item) => ({
      ...item,
      media: item.media.map((media) => ({
        ...media,
        download_url: absolutizeBackendURL(media.download_url, baseUrl),
      })),
    })),
  };
}

function absolutizeActionCenterMedia(response: ActionCenterResponse, baseUrl: string): ActionCenterResponse {
  return {
    ...response,
    items: response.items.map((row) => ({
      ...row,
      evidence: absolutizeProcessIntegrityEvidenceMedia(row.evidence, baseUrl),
    })),
  };
}

function absolutizeAdherenceMedia(response: ProtocolAdherenceResponse, baseUrl: string): ProtocolAdherenceResponse {
  return {
    ...response,
    rows: response.rows.map((row) => ({
      ...row,
      evidence: absolutizeProcessIntegrityEvidenceMedia(row.evidence, baseUrl),
    })),
  };
}

function absolutizeProcessIntegrityEvidenceMedia(evidence: ProcessIntegrityEvidence, baseUrl: string): ProcessIntegrityEvidence {
  const evidenceWithMedia = evidence as ProcessIntegrityEvidence & {
    media?: Array<{ download_url: string }>;
  };
  if (!evidenceWithMedia.media) return evidence;
  const normalized: typeof evidenceWithMedia = {
    ...evidenceWithMedia,
    media: evidenceWithMedia.media.map((media) => ({
      ...media,
      download_url: absolutizeBackendURL(media.download_url, baseUrl),
    })),
  };
  return normalized;
}

function absolutizeBackendURL(value: string, baseUrl: string): string {
  try {
    return new URL(value, baseUrl).toString();
  } catch {
    return value;
  }
}

// ---- Proof upload wrappers for vaccination drawer ----
// These enable the frontend to initiate proof uploads for vaccination completions and task submissions.

export async function createProofUpload(body: {
  proof_type: "photo" | "video" | "attachment";
  mime_type: string;
  scope_type: "task";
  scope_id: string;
  subject_type: "task" | "administration";
  subject_id?: string | null;
  metadata?: Record<string, unknown>;
}): Promise<ApiResult<CreateProofUploadResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CreateProofUploadResponse>("/app/proofs/uploads", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function uploadProofLocal(
  uploadUrl: string,
  uploadHeaders: Record<string, string>,
  file: File,
): Promise<ApiResult<void>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  return request(async () => {
    const target = new URL(uploadUrl, config.data.baseUrl);
    const apiOrigin = new URL(config.data.baseUrl).origin;
    const headers = new Headers(uploadHeaders);
    if (target.origin === apiOrigin) {
      headers.set("Authorization", `Bearer ${config.data.bearerToken}`);
      if (config.data.tenantId) headers.set("X-GoatOS-Tenant-ID", config.data.tenantId);
    }
    if (file.type) headers.set("Content-Type", file.type);
    const res = await fetch(target, {
      method: "PUT",
      cache: "no-store",
      headers,
      body: file,
    });
    if (!res.ok) {
      throw new Error(`Proof upload failed with HTTP ${res.status}`);
    }
  });
}

export async function completeProofUpload(
  proofId: string,
  mediaType: string,
  sizeBytes: number,
): Promise<ApiResult<ProofResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/app/proofs/${encodeURIComponent(proofId)}/complete` as keyof AppApiPaths & string;
  return request(() =>
    client.request<ProofResponse>(path, {
      method: "POST",
      cache: "no-store",
      body: { mime_type: mediaType, size_bytes: sizeBytes },
    }),
  );
}

export async function submitAppTask(
  taskId: string,
  submissionData: {
    sop_version_id: string;
    idempotency_key: string;
    answers: Record<string, unknown>;
    proof_refs: Array<{
      proof_id: string;
      proof_type: "photo" | "video" | "attachment";
      subject_type: "task" | "administration";
      subject_id?: string | null;
      upload_state: "completed";
      metadata: Record<string, unknown>;
    }>;
  },
): Promise<ApiResult<SubmissionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/app/tasks/${encodeURIComponent(taskId)}/submissions` as keyof AppApiPaths & string;
  return request(() =>
    client.request<SubmissionResponse>(path, {
      method: "POST",
      cache: "no-store",
      body: submissionData,
    }),
  );
}

// ---- SOP Library (Admin / Data Ops) — committed admin SOP engine endpoints ----
// All go through the Admin API client (tenant-scoped). The list endpoint is intentionally thin
// (SOPDefinition only); the SOP Library fetches per-SOP detail to read the latest version's
// form_dsl + proof_policy for the card facets. No client-side mock rows, no fake `source: mock`.

export async function listSops(params: { status?: string; codePrefix?: string; q?: string; limit?: number; cursor?: string } = {}): Promise<ApiResult<SOPListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SOPListResponse>("/admin/sops", {
      cache: "no-store",
      query: compactQuery({
        status: params.status,
        code_prefix: params.codePrefix,
        q: params.q,
        limit: params.limit ?? 25,
        cursor: params.cursor,
      }),
    }),
  );
}

export async function getSop(sopId: string): Promise<ApiResult<SOPResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/sops/${encodeURIComponent(sopId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<SOPResponse>(path, { cache: "no-store" }));
}

export async function createSop(body: CreateSOPRequest): Promise<ApiResult<SOPResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<SOPResponse>("/admin/sops", { method: "POST", cache: "no-store", body }));
}

export async function createSopVersion(
  sopId: string,
  body: CreateSOPVersionRequest,
): Promise<ApiResult<SOPVersionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/sops/${encodeURIComponent(sopId)}/versions` as keyof AdminApiPaths & string;
  return request(() => client.request<SOPVersionResponse>(path, { method: "POST", cache: "no-store", body }));
}

export async function dryRunSopVersion(
  sopId: string,
  versionId: string,
  body: DryRunRequest,
): Promise<ApiResult<DryRunResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path =
    `/admin/sops/${encodeURIComponent(sopId)}/versions/${encodeURIComponent(versionId)}/dry-run` as keyof AdminApiPaths & string;
  return request(() => client.request<DryRunResponse>(path, { method: "POST", cache: "no-store", body }));
}

export async function publishSopVersion(
  sopId: string,
  versionId: string,
  rowVersion: number,
): Promise<ApiResult<SOPVersionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path =
    `/admin/sops/${encodeURIComponent(sopId)}/versions/${encodeURIComponent(versionId)}/publish` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<SOPVersionResponse>(path, { method: "POST", cache: "no-store", body: { row_version: rowVersion } }),
  );
}

export async function retireSopVersion(
  sopId: string,
  versionId: string,
  rowVersion: number,
): Promise<ApiResult<SOPVersionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path =
    `/admin/sops/${encodeURIComponent(sopId)}/versions/${encodeURIComponent(versionId)}/retire` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<SOPVersionResponse>(path, { method: "POST", cache: "no-store", body: { row_version: rowVersion } }),
  );
}

export async function addGoatIdentifier(
  goatId: string,
  body: AddIdentifierRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/goats/${encodeURIComponent(goatId)}/identifiers` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminGoatResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function retireGoatIdentifier(
  goatId: string,
  identifierId: string,
  body: RetireIdentifierRequestBody,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/goats/${encodeURIComponent(goatId)}/identifiers/${encodeURIComponent(identifierId)}/retire` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminGoatResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function stageGoat(
  goatId: string,
  body: StageGoatRequest,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/goats/${encodeURIComponent(goatId)}/stage` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminGoatResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

// Reproductive status write path. Mirrors stageGoat: POST /admin/goats/{goat_id}/reproductive with an
// Idempotency-Key so a double-submit replays the original result. The reproductive value list is
// backend-owned (herd-register contract option group `herd_reproductive`, compiled from active
// reproductive status_definitions) — never a hardcoded UI vocabulary.
export async function reproductiveGoat(
  goatId: string,
  body: ReproductiveGoatRequest,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/goats/${encodeURIComponent(goatId)}/reproductive` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminGoatResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

// Health status writes intentionally have no admin-web wrapper yet. Add one only with a backend-owned
// Goat Passport/Herd action contract that carries labels, options, disabled reasons, and authority copy.

// Bulk status-update write path. Spans reproductive/health/exit axes, requires dual write grants.
// Preview is a pure read (no idempotency key, no state mutation) that returns per-row decisions
// and a signed preview_token. Commit is idempotent on Idempotency-Key and enqueues a durable job.
export async function previewBulkStatusUpdate(
  body: BulkStatusPreviewRequest,
): Promise<ApiResult<BulkStatusPreviewResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<BulkStatusPreviewResponse>("/admin/goats/bulk-status/preview", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function commitBulkStatusUpdate(
  body: BulkStatusCommitRequest,
  idempotencyKey: string,
): Promise<ApiResult<BulkStatusCommitResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<BulkStatusCommitResponse>("/admin/goats/bulk-status/commit", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

// Counts -> Herd Register write path. Each create/commit carries an Idempotency-Key so a double-submit or
// retry replays the original result instead of writing a second goat. The backend derives the actor from the
// auth token; the body carries only operator-entered identity. preview is a pure read (no key, no writes).
export async function createAdminGoat(
  body: CreateAdminGoatRequest,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<AdminGoatResponse>("/admin/goats", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function previewAdminGoatBulkImport(
  body: AdminGoatBulkPreviewRequest,
): Promise<ApiResult<AdminGoatBulkResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<AdminGoatBulkResponse>("/admin/goats/bulk-preview", { method: "POST", cache: "no-store", body }),
  );
}

export async function commitAdminGoatBulkImport(
  body: AdminGoatBulkCommitRequest,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatBulkResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<AdminGoatBulkResponse>("/admin/goats/bulk-commit", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

// Location master read — backs the real park/shed/farm selectors in the Herd Register drawers. Physical
// locations are bounded (parks/sheds/farms, not goats), so a capped list is scale-safe.
export async function listLocations(
  params: { type?: string; status?: string; parentLocationId?: string; limit?: number } = {},
): Promise<ApiResult<LocationListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LocationListResponse>("/admin/locations", {
      cache: "no-store",
      query: compactQuery({
        type: params.type,
        status: params.status,
        parent_location_id: params.parentLocationId,
        limit: params.limit ?? 500,
      }),
    }),
  );
}

export async function createLocation(
  body: CreateLocationRequest,
  idempotencyKey: string,
): Promise<ApiResult<LocationMutationResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LocationMutationResponse>("/admin/locations", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function listOperationsAudit(
  params: OperationsAuditListParams = {},
): Promise<ApiResult<OperationsAuditListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<OperationsAuditListResponse>("/operations/audit", {
      cache: "no-store",
      query: compactQuery({
        limit: params.limit ?? 100,
        cursor: params.cursor,
        from: params.from,
        to: params.to,
        actor_type: params.actorType,
        actor_id: params.actorId,
        action: params.action,
        resource_type: params.resourceType,
        resource_id: params.resourceId,
        scope_type: params.scopeType,
        scope_id: params.scopeId,
        domain: params.domain,
        module: params.module,
        category: params.category,
        result: params.result,
        status: params.status,
        q: params.q,
        anomalies_only: params.anomaliesOnly,
        proof_gaps: params.proofGaps,
      }),
    }),
  );
}

export async function getOperationsAuditSummary(
  params: Omit<OperationsAuditListParams, "limit" | "cursor"> = {},
): Promise<ApiResult<OperationsAuditSummaryResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<OperationsAuditSummaryResponse>("/operations/audit/summary", {
      cache: "no-store",
      query: compactQuery({
        from: params.from,
        to: params.to,
        actor_type: params.actorType,
        actor_id: params.actorId,
        action: params.action,
        resource_type: params.resourceType,
        resource_id: params.resourceId,
        scope_type: params.scopeType,
        scope_id: params.scopeId,
        domain: params.domain,
        module: params.module,
        category: params.category,
        result: params.result,
        status: params.status,
        q: params.q,
        anomalies_only: params.anomaliesOnly,
        proof_gaps: params.proofGaps,
      }),
    }),
  );
}

export async function listOutboxDLQ(params: OutboxDLQListParams = {}): Promise<ApiResult<OutboxDLQListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<OutboxDLQListResponse>("/operations/dlq", {
      cache: "no-store",
      query: compactQuery({
        status: params.status,
        event_type: params.eventType,
        topic: params.topic,
        limit: params.limit ?? 100,
      }),
    }),
  );
}

export async function getOperationsKernelHealth(): Promise<ApiResult<OperationsKernelHealthResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() => client.request<OperationsKernelHealthResponse>("/operations/kernel-health", { cache: "no-store" }));
}

export async function runVaccinationManualCampaign(
  body: RunVaccinationManualCampaignRequest,
  idempotencyKey = `vaccination-manual-${randomUUID()}`,
): Promise<ApiResult<VaccinationGenerationRunResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationGenerationRunResponse>("/vaccination/manual-campaigns", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function replayOutboxDLQ(
  body: OutboxDLQActionRequest,
  idempotencyKey = `dlq-replay-${randomUUID()}`,
): Promise<ApiResult<OutboxDLQActionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<OutboxDLQActionResponse>("/operations/dlq/replay", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function discardOutboxDLQ(
  body: OutboxDLQActionRequest,
  idempotencyKey = `dlq-discard-${randomUUID()}`,
): Promise<ApiResult<OutboxDLQActionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<OutboxDLQActionResponse>("/operations/dlq/discard", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

// Workforce / HR roster — staff positions, backup configurations, and coverage windows.
// Used by the /people page to display operational staffing, coverage, and timetable.
export type Position = AdminApiComponents["schemas"]["Position"];
export type BackupConfig = AdminApiComponents["schemas"]["BackupConfig"];
export type Coverage = AdminApiComponents["schemas"]["Coverage"];

export type PositionListResponse = AdminApiComponents["schemas"]["PositionListResponse"];
export type BackupConfigListResponse = {
  items: BackupConfig[];
  trace_id: string;
};
export type CoverageListResponse = {
  items: Coverage[];
  trace_id: string;
};
export type StaffPositionsQuery = NonNullable<AdminApiPaths["/admin/roster/positions"]["get"]["parameters"]["query"]>;
export type UpdatePositionRequest = AdminApiComponents["schemas"]["UpdatePositionRequest"];
export type BackupConfigQuery = NonNullable<AdminApiPaths["/admin/roster/backup-config"]["get"]["parameters"]["query"]>;
export type CoverageQuery = NonNullable<AdminApiPaths["/admin/roster/coverage"]["get"]["parameters"]["query"]>;
export type StaffLeave = AdminApiComponents["schemas"]["StaffLeave"];
export type StaffLeaveListResponse = AdminApiComponents["schemas"]["StaffLeaveListResponse"];
export type ApplyStaffLeaveRequest = AdminApiComponents["schemas"]["ApplyStaffLeaveRequest"];
export type ApproveStaffLeaveRequest = AdminApiComponents["schemas"]["ApproveStaffLeaveRequest"];
export type StaffLeaveQuery = NonNullable<AdminApiPaths["/admin/roster/leave"]["get"]["parameters"]["query"]>;

export async function listStaffPositions(
  params: StaffPositionsQuery = {},
): Promise<ApiResult<PositionListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<PositionListResponse>("/admin/roster/positions", {
      cache: "no-store",
      query: compactQuery({ ...params, limit: params.limit ?? 500 }),
    }),
  );
}

export type PositionProfileResponse = AdminApiComponents["schemas"]["PositionProfileResponse"];

export async function getStaffPositionProfile(
  positionId: string,
): Promise<ApiResult<PositionProfileResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/roster/positions/${encodeURIComponent(positionId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<PositionProfileResponse>(path, { cache: "no-store" }));
}

export async function updateStaffPosition(
  positionId: string,
  body: UpdatePositionRequest,
  idempotencyKey: string,
): Promise<ApiResult<AdminApiComponents["schemas"]["PositionResponse"]>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/roster/positions/${encodeURIComponent(positionId)}` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminApiComponents["schemas"]["PositionResponse"]>(path, {
      method: "PATCH",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function listStaffLeave(
  params: StaffLeaveQuery = {},
): Promise<ApiResult<StaffLeaveListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<StaffLeaveListResponse>("/admin/roster/leave", {
      cache: "no-store",
      query: compactQuery({ ...params, limit: params.limit ?? 500 }),
    }),
  );
}

export async function applyStaffLeave(
  body: ApplyStaffLeaveRequest,
): Promise<ApiResult<AdminApiComponents["schemas"]["StaffLeaveResponse"]>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<AdminApiComponents["schemas"]["StaffLeaveResponse"]>("/admin/roster/leave", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function approveStaffLeave(
  absenceId: string,
  body: ApproveStaffLeaveRequest,
  idempotencyKey?: string,
): Promise<ApiResult<AdminApiComponents["schemas"]["StaffLeaveResponse"]>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/roster/leave/${encodeURIComponent(absenceId)}/approve` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminApiComponents["schemas"]["StaffLeaveResponse"]>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined,
      body,
    }),
  );
}

export async function listBackupConfig(
  params: BackupConfigQuery = {},
): Promise<ApiResult<BackupConfigListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<BackupConfigListResponse>("/admin/roster/backup-config", {
      cache: "no-store",
      query: compactQuery({ ...params, limit: params.limit ?? 500 }),
    }),
  );
}

export async function listCoverage(
  params: CoverageQuery = {},
): Promise<ApiResult<CoverageListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CoverageListResponse>("/admin/roster/coverage", {
      cache: "no-store",
      query: compactQuery({ ...params, limit: params.limit ?? 500 }),
    }),
  );
}

export async function request<T>(fn: () => Promise<T>): Promise<ApiResult<T>> {
  try {
    return { ok: true, data: await fn() };
  } catch (error) {
    return { ok: false, error: normalizeApiError(error) };
  }
}

async function withApiTimeout<T>(ms: number, fn: (signal: AbortSignal) => Promise<T>): Promise<T> {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), ms);
  try {
    return await fn(controller.signal);
  } finally {
    clearTimeout(timeout);
  }
}

function normalizeApiError(error: unknown): ApiUiError {
  if (error instanceof DOMException && error.name === "AbortError") {
    return {
      kind: "backend_down",
      message: "The backend took too long to return vaccination data. Try again after the local API finishes warming up.",
      retryable: true,
    };
  }
  if (error instanceof GoatOSApiError) {
    const envelope = parseEnvelope(error.body);
    const code = envelope?.code;
    if (error.status === 401) {
      return {
        kind: "unauthorized",
        status: error.status,
        code,
        message: "Your Google sign-in session is missing or expired. Sign in again to refresh the admin session.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 403 && code === "tenant_scope_mismatch") {
      return {
        kind: "tenant_scope_mismatch",
        status: error.status,
        code,
        message: "Configured tenant does not match the signed-in admin session.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 403 && code === "route_not_registered") {
      // Backend-side 403 from the route registry (permissions.Match miss), NOT a DB grant. It means
      // the running API does not register this route — typically a stale/older API build. This is an
      // env/runtime issue, not an authorization one; do not blame DB grants.
      return {
        kind: "permission_denied",
        status: error.status,
        code,
        message: "Backend route is not registered — the running API is stale or built from older source. Restart the API from current source.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 403) {
      return {
        kind: "permission_denied",
        status: error.status,
        code: code ?? "permission_denied",
        message: "Signed in, but the active DB grants do not allow this view.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 409 && code === "park_scope_ambiguous") {
      // BUG-019: carry the backend-owned park menu through this hop. Reshaping to a bare
      // message here is what stranded tenant-wide CEO/CXO accounts: the backend correctly
      // refused to guess a park AND supplied the choices, but the choices were dropped, so
      // the screen had nothing to render and threw.
      return {
        kind: "bad_request",
        status: error.status,
        code,
        message: envelope?.message ?? "Your scope covers more than one park; choose one to continue.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
        availableParks: parseParkScopeOptions(envelope),
      };
    }
    if (error.status === 400) {
      return {
        kind: "bad_request",
        status: error.status,
        code,
        message: envelope?.message ?? "The service rejected these filters. Check the selected tenant, cursor, and limit.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    if (error.status === 404) {
      return {
        kind: "not_found",
        status: error.status,
        code,
        message: envelope?.message ?? "Not found, or this admin token is not allowed to view it.",
        traceId: envelope?.trace_id,
        retryable: envelope?.retryable,
      };
    }
    return {
      kind: "api_error",
      status: error.status,
      code,
      message: envelope?.message ?? `Backend service returned ${error.status}.`,
      traceId: envelope?.trace_id,
      retryable: envelope?.retryable,
    };
  }
  if (error instanceof TypeError) {
    return {
      kind: "backend_down",
      message: "Backend service is not reachable from the Mesha admin server.",
    };
  }
  return {
    kind: "api_error",
    message: error instanceof Error ? error.message : "Unexpected API error.",
  };
}

// parseParkScopeOptions reads the backend-owned park menu off a 409 `park_scope_ambiguous`
// envelope. Returns undefined rather than [] when absent, so "backend sent no menu" stays
// distinguishable from "backend sent an empty menu" — zero authorized parks is a genuinely
// different situation from several, and the screen must not report it as "choose one".
function parseParkScopeOptions(envelope: ErrorEnvelope | null): ParkScopeOption[] | undefined {
  const raw = (envelope as { availableParks?: unknown } | null)?.availableParks;
  if (!Array.isArray(raw)) return undefined;
  const parks = raw.flatMap((entry) => {
    if (!entry || typeof entry !== "object") return [];
    const { parkId, code, name } = entry as Partial<ParkScopeOption>;
    if (typeof parkId !== "string" || typeof name !== "string") return [];
    return [{ parkId, code: typeof code === "string" ? code : "", name }];
  });
  return parks;
}

function parseEnvelope(body: unknown): ErrorEnvelope | null {
  if (!body || typeof body !== "object") return null;
  const maybe = body as Partial<ErrorEnvelope>;
  if (typeof maybe.code !== "string" || typeof maybe.message !== "string") {
    return null;
  }
  return maybe as ErrorEnvelope;
}

export function compactQuery(values: Record<string, string | number | boolean | null | undefined>) {
  const query: Record<string, string | number | boolean> = {};
  for (const [key, value] of Object.entries(values)) {
    if (value === null || value === undefined || value === "") continue;
    query[key] = value;
  }
  return query;
}

// ---------------------------------------------------------------------------
// Counts Approvals (admin-web) — maintainer decision 2026-07-21.
//
// The pending birth/death/shifting approval queue, moved off mobile onto the admin-web Approvals
// page. Served by the SAME approval service as the (now web-only) decision surface, under the
// /admin-web/* prefix so it rides the admin session. Access is gated server-side by
// counts.approve_access (the four org tiers + admin + ceo_internal); park_head no longer holds it.
//
// The approvals endpoints predate the OpenAPI contract (mobile consumed them via hand-written DTOs),
// so these shapes are declared here rather than pulled from the generated client — the one
// documented exception, mirroring how procurement/verification bootstrapped before their codegen.
export type AdminWebApprovalRequestType = "birth" | "death" | "shifting";
export type AdminWebApprovalStatus = "pending" | "approved" | "rejected" | "cancelled";

export type AdminWebApprovalItem = {
  approval_request_id: string;
  request_type: AdminWebApprovalRequestType;
  status: AdminWebApprovalStatus;
  raised_by_user_id: string;
  raised_at: string;
  shifting_event_id?: string;
  subject_goat_id?: string;
  summary: unknown;
  decided_by_user_id?: string;
  decided_at?: string;
  decision_reason?: string;
};

export type AdminWebApprovalListResponse = {
  items: AdminWebApprovalItem[];
  next_cursor?: string;
};

export type AdminWebApprovalDecisionResponse = {
  approval_request_id: string;
  request_type: AdminWebApprovalRequestType;
  status: AdminWebApprovalStatus;
  idempotent_replay: boolean;
};

export type AdminWebApprovalListParams = {
  status?: string;
  cursor?: string;
  page_size?: number;
};

/** One keyset page of approval requests the caller may decide (GET /admin-web/counts/approvals). */
export async function listAdminWebApprovals(
  params: AdminWebApprovalListParams,
): Promise<ApiResult<AdminWebApprovalListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = "/admin-web/counts/approvals" as keyof AppApiPaths & string;
  return request(() =>
    client.request<AdminWebApprovalListResponse>(path, {
      cache: "no-store",
      query: compactQuery({ status: params.status, cursor: params.cursor, page_size: params.page_size }),
    }),
  );
}

/**
 * Approve or reject one request (POST /admin-web/counts/approvals/{id}/approve|reject). Carries the
 * mandatory Idempotency-Key so a retry cannot double-apply; the backend re-checks the caller's
 * authority against the request's STORED type, so an out-of-authority decision fails closed there.
 */
export async function decideAdminWebApproval(args: {
  requestId: string;
  approve: boolean;
  reason: string;
  idempotencyKey: string;
}): Promise<ApiResult<AdminWebApprovalDecisionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const verb = args.approve ? "approve" : "reject";
  const path =
    `/admin-web/counts/approvals/${encodeURIComponent(args.requestId)}/${verb}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<AdminWebApprovalDecisionResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": args.idempotencyKey },
      body: { reason: args.reason },
    }),
  );
}
