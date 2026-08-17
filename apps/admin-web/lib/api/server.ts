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
export type WeightGainBucket = AppApiComponents["schemas"]["WeighingWeightGainBucket"];
export type WeightDemographicBucket = AppApiComponents["schemas"]["WeighingWeightDemographicBucket"];
export type WeightDemographicsResponse = AppApiComponents["schemas"]["WeighingWeightDemographicsResponse"];
export type WeighingLosingAnimal = AppApiComponents["schemas"]["WeighingGrowthLosingAnimal"];
export type WeighingGrowthResponse = AppApiComponents["schemas"]["WeighingGrowthADGResponse"];
export type ShedWeightsSummary = AppApiComponents["schemas"]["WeighingShedWeightsSummary"];
export type ShedWeightsRow = AppApiComponents["schemas"]["WeighingShedWeightsRow"];
export type ShedWeightsResponse = AppApiComponents["schemas"]["WeighingShedWeightsResponse"];
export type MilkPreparationRow = AppApiComponents["schemas"]["MilkPreparationRow"];
export type MilkPreparationPage = AppApiComponents["schemas"]["MilkPreparationPage"];
export type GoatTimelineResponse = AppApiComponents["schemas"]["GoatTimelineResponse"];
export type IdentifierType = AppApiComponents["schemas"]["IdentifierType"];
export type ActionCenterObligation = AppApiComponents["schemas"]["ActionCenterObligation"] & {
  // The Go producer (processintegrity/domain.Row) emits BOTH of these; the generated client
  // has not been regenerated since the schema gained them. Narrow extension rather than `any`
  // so the call sites are type-checked today and need no edit when the client is regenerated.
  // Both are optional here precisely because the generated type cannot yet prove them.
  partition_label?: string | null;
  operational_location_display?: string | null;
};
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
export type AdherenceRow = AppApiComponents["schemas"]["AdherenceRow"] & {
  source_shed_name?: string | null;
};
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
export type VaccinationExecutionRow = AppApiComponents["schemas"]["VaccinationExecutionRow"] & {
  operational_location_display?: string;
};
export type VaccinationOperationsResponse = AppApiComponents["schemas"]["VaccinationOperationsResponse"];
export type VaccinationOperationsCohort = AppApiComponents["schemas"]["VaccinationOperationsCohort"] & {
  partitionLabel?: string;
  operationalLocationDisplay?: string;
};
export type VaccinationOperationsProtocol = AppApiComponents["schemas"]["VaccinationOperationsProtocol"];
export type VaccinationOperationsCell = AppApiComponents["schemas"]["VaccinationOperationsCell"];
export type VaccinationOperationsCounts = AppApiComponents["schemas"]["VaccinationOperationsCounts"];
export type VaccinationExecutionShedDrilldown = AppApiComponents["schemas"]["VaccinationExecutionShedDrilldown"] & {
  operationalLocationDisplay?: string;
};
export type VaccinationExecutionWorkState = AppApiComponents["schemas"]["VaccinationExecutionWorkState"];
export type VaccinationExecutionSeverity = AppApiComponents["schemas"]["VaccinationExecutionSeverity"];
export type VaccinationExecutionSOPStatus = AppApiComponents["schemas"]["VaccinationExecutionSOPStatus"];
export type VaccinationExecutionProofStatus = AppApiComponents["schemas"]["VaccinationExecutionProofStatus"];
export type VaccinationExecutionVerificationStatus = AppApiComponents["schemas"]["VaccinationExecutionVerificationStatus"];

// Shed-wise vaccination read model (the main /vaccination table + shed detail + capacity planner).
export type VaccinationShedSummaryResponse = AppApiComponents["schemas"]["VaccinationShedSummaryResponse"];
export type VaccinationLiveTrackerResponse = AppApiComponents["schemas"]["VaccinationLiveTrackerResponse"];
export type VaccinationLiveTrackerStatus = NonNullable<
  AppApiPaths["/vaccination/live-tracker"]["get"]["parameters"]["query"]
>["status"];
export type VaccinationShedSummaryRow = AppApiComponents["schemas"]["VaccinationShedSummaryRow"] & {
  partitionLabel?: string;
  operationalLocationDisplay?: string;
};
export type VaccinationShedDetail = AppApiComponents["schemas"]["VaccinationShedDetail"] & {
  source_shed_name?: string | null;
};
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
export type ReclassifyShedStageRequest = AdminApiComponents["schemas"]["ReclassifyShedStageRequest"];
export type CorrectCensusSliceRequest = AdminApiComponents["schemas"]["CorrectCensusSliceRequest"];
export type CensusSliceCorrectionPreviewResponse = AdminApiComponents["schemas"]["CensusSliceCorrectionPreviewResponse"];
export type CensusSliceCorrectionResponse = AdminApiComponents["schemas"]["CensusSliceCorrectionResponse"];
export type ReclassifyShedStagePreviewResponse = AdminApiComponents["schemas"]["ReclassifyShedStagePreviewResponse"];
export type ReclassifyShedStageResponse = AdminApiComponents["schemas"]["ReclassifyShedStageResponse"];
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
// verifier-app-and-flow.md). /actions serves BOTH personas of that vertical, split by the page
// contract's controls rather than by route:
//   - the VERIFIER (verification.verdict -- that role ALONE) records the approve/reject verdict;
//   - the AUTHORITY (verification.act) acts on the SOURCE task via /admin/tasks/{task_id}/rework
//     |assign.
// Admin-web gained the verdict half on 2026-08-03, when the verifier-only web workspace landed;
// before that, verdicts were mobile-only.
//
// Real generated app-api types — the backend Verification module (1a) landed on main and the
// client regenerated (`packages/api-client/src/generated/app-api.ts`,
// `contracts/openapi/app-api.yaml`). `/verification/queue` is now a properly typed AppApiPaths
// entry too, so `listVerificationQueue` below no longer needs the `as keyof AppApiPaths & string`
// cast.
export type VerificationItemStatus = AppApiComponents["schemas"]["VerificationItemStatus"];
export type VerificationSourceRef = AppApiComponents["schemas"]["VerificationSourceRef"];
export type VerificationMediaItem = AppApiComponents["schemas"]["VerificationMediaItem"];
export type VerificationQueueItem = AppApiComponents["schemas"]["VerificationQueueItem"] & {
  verified_by_name?: string | null;
};
type VerificationQueueFilterOption = {
  key: string;
  label: string;
};
type VerificationActionTypeOption = {
  category: string;
  label: string;
  module_label: string;
};
type VerificationStatusOption = {
  key: string;
  label: string;
  status?: VerificationItemStatus | null;
};
type VerificationShedOption = {
  id: string;
  label: string;
  park_label?: string | null;
  operational_location_display?: string | null;
};
export type VerificationQueueResponse = Omit<AppApiComponents["schemas"]["VerificationQueueResponse"], "items" | "filter_options"> & {
  items: VerificationQueueItem[];
  filter_options: {
    modules?: VerificationQueueFilterOption[];
    module_key?: string | null;
    action_types: VerificationActionTypeOption[];
    statuses: VerificationStatusOption[];
    sheds: VerificationShedOption[];
    counts: Record<"pending" | "approved" | "rejected", number>;
  };
};
export type VerificationDecision = AppApiComponents["schemas"]["VerificationDecision"];
export type VerificationVerdictRequest = AppApiComponents["schemas"]["VerificationVerdictRequest"];
export type VerificationVerdictResponse = AppApiComponents["schemas"]["VerificationVerdictResponse"];
// The VERIFIER's weight correction on a weighing proof (maintainer decision 2026-08-17). Weighing
// owns the route; the verification item tells the client which record to address, via
// measurement_correction.
export type WeighingWeightCorrectionRequest = AppApiComponents["schemas"]["WeighingWeightCorrectionRequest"];
export type WeighingWeightCorrectionResponse = AppApiComponents["schemas"]["WeighingWeightCorrectionResponse"];
export type VerificationReviewEvent = AppApiComponents["schemas"]["VerificationReviewEvent"];
export type VerificationReviewEventBatchRequest = AppApiComponents["schemas"]["VerificationReviewEventBatchRequest"];
export type VerificationReviewEventBatchResponse = AppApiComponents["schemas"]["VerificationReviewEventBatchResponse"];

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

/**
 * The first route this principal's compiled navigation actually offers — their landing.
 *
 * Returns null when the contract is unavailable (the shell renders its own contract-unavailable
 * state) or the navigation is empty.
 */
export async function adminWebLandingHref(): Promise<string | null> {
  const contract = await getAdminWebBootstrap();
  if (!contract.ok) return null;
  const first =
    contract.data.navigation.primary.find((item) => item.enabled) ??
    contract.data.navigation.groups.flatMap((group) => group.leaves).find((item) => item.enabled);
  return first?.href ?? null;
}

/**
 * Whether this principal's compiled navigation offers `href`.
 *
 * Contract-backed pages fail closed on their own: a principal who may not open one has no page
 * contract for it, so requireAdminWebPageContract throws. A page that renders from LOCAL literal
 * copy has no such contract to withhold, so it must ask this explicitly — otherwise it would render
 * its module chrome to anyone who types the URL, even though its data calls 403. `/approvals` is
 * the current instance; a future contract-less route needs the same call.
 *
 * Returns true when the contract is unavailable, leaving that failure to the shell rather than
 * turning a backend hiccup into a spurious redirect.
 */
export async function adminWebRouteOffered(href: string): Promise<boolean> {
  const contract = await getAdminWebBootstrap();
  if (!contract.ok) return true;
  const offered = [
    ...contract.data.navigation.primary,
    ...contract.data.navigation.groups.flatMap((group) => group.leaves),
  ];
  return offered.some((item) => item.enabled && item.href === href);
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
  partition_label?: string | null;
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

export async function getMilkPreparation(params: {
  park_id?: string;
  limit?: number;
  offset?: number;
}): Promise<ApiResult<MilkPreparationPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<MilkPreparationPage>("/counts/milk-preparation", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// ---------------------------------------------------------------------------------------------
// Weighing — the admin-web Weights read-out.
//
// ONE request serves the whole screen. `summary` is a WHOLE-FILTER aggregate computed by the
// backend over every shed in scope, so this layer must never re-derive a KPI by summing `rows`:
// rows are bounded, the summary is not, and the two would silently disagree the moment the
// estate outgrows the row cap.
export async function getShedWeights(params: {
  park_id?: string;
  from?: string;
  to?: string;
}): Promise<ApiResult<ShedWeightsResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ShedWeightsResponse>("/weighing/shed-weights", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// Average weight by breed, sex and stage. The one weighing read that resolves a scanned tag
// to its animal, so these three dimensions can exist at all.
export async function getWeightDemographics(params: {
  park_id?: string;
  from?: string;
  to?: string;
}): Promise<ApiResult<WeightDemographicsResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<WeightDemographicsResponse>("/weighing/weight-demographics", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// The losing-kids list on the Weights page. Reuses the growth read model rather than adding a
// second one: it already computes "kids whose latest pair of weighs went down", named by scanned
// tag, which is exactly the table.
export async function getWeighingGrowth(params: {
  park_id?: string;
  from?: string;
  to?: string;
}): Promise<ApiResult<WeighingGrowthResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<WeighingGrowthResponse>("/weighing/leadership/growth", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

// ---------------------------------------------------------------------------------------------
// Growth Director — the analytics block under the Weights page. One request serves the whole
// block; every aggregate (bands, medians, feed-per-kg ratios) is computed by the backend over the
// whole filter, so this layer never re-derives a number from a row slice.
export type GrowthDirectorWeightsResponse = AppApiComponents["schemas"]["GrowthDirectorWeightsResponse"];

export async function getGrowthDirector(params: {
  park_id?: string;
  from?: string;
  to?: string;
}): Promise<ApiResult<GrowthDirectorWeightsResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<GrowthDirectorWeightsResponse>("/growth-director/weights", {
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
export type FeedDirectionPreviewPage = Omit<AppApiComponents["schemas"]["FeedDirectionPreviewPage"], "items"> & {
  items: FeedDirectionRow[];
};
export type FeedDirectionRow = AppApiComponents["schemas"]["FeedDirectionRow"] & {
  partition_label?: string | null;
  operational_location_display?: string | null;
  blocked_reasons?: AppApiComponents["schemas"]["FeedDirectionItemQuantity"]["blocked_reason"][];
};
export type FeedDirectionItemQuantity = AppApiComponents["schemas"]["FeedDirectionItemQuantity"];
export type FeedDirectionPreviewSummary = AppApiComponents["schemas"]["FeedDirectionPreviewSummary"];
export type FeedDirectionLifecycle = AppApiComponents["schemas"]["FeedDirectionLifecycle"];
export type FeedDirectionWorkflowLifecycle = AppApiComponents["schemas"]["FeedDirectionWorkflowLifecycle"];
export type FeedPackingWorklistPage = Omit<AppApiComponents["schemas"]["FeedPackingWorklistPage"], "items"> & {
  items: FeedPackingRow[];
};
export type FeedPackingRow = AppApiComponents["schemas"]["FeedPackingRow"] & {
  partition_label?: string | null;
  operational_location_display?: string | null;
};
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
export type CreateFeedConfigFeedItemRequest = AppApiComponents["schemas"]["CreateFeedConfigFeedItemRequest"];
export type SetFeedConfigFeedItemStatusRequest = AppApiComponents["schemas"]["SetFeedConfigFeedItemStatusRequest"];
export type SetFeedConfigSessionTemplateItemRequest =
  AppApiComponents["schemas"]["SetFeedConfigSessionTemplateItemRequest"];
export type FeedConfigSessionTemplateItem = AppApiComponents["schemas"]["FeedConfigSessionTemplateItem"];
export type UpsertFeedConfigShedFactorRequest = AppApiComponents["schemas"]["UpsertFeedConfigShedFactorRequest"];
export type UpsertFeedConfigScheduleRequest = AppApiComponents["schemas"]["UpsertFeedConfigScheduleRequest"];
export type FeedConfigExperimentPage = Omit<AppApiComponents["schemas"]["FeedConfigExperimentPage"], "items"> & {
  items: FeedConfigExperiment[];
};
export type FeedConfigExperiment = AppApiComponents["schemas"]["FeedConfigExperiment"] & {
  partition_label?: string;
  park_name?: string;
  operational_location_display?: string;
};
export type UpsertFeedConfigExperimentRequest = AppApiComponents["schemas"]["UpsertFeedConfigExperimentRequest"] & {
  partition_label?: string;
};
export type SetFeedConfigExperimentShedStatusRequest =
  AppApiComponents["schemas"]["SetFeedConfigExperimentShedStatusRequest"] & {
    partition_label?: string;
  };
export type FeedConfigPen = AppApiComponents["schemas"]["FeedConfigPen"];
export type FeedConfigPenPage = AppApiComponents["schemas"]["FeedConfigPenPage"];
export type UpsertFeedConfigExperimentBatchRequest =
  AppApiComponents["schemas"]["UpsertFeedConfigExperimentBatchRequest"];

export type HealthConfigProtocolRow = {
  disease_key: string;
  display_name: string;
  age_band: "adult" | "kid";
  duration_days: number;
  step_count: number;
  medication_count: number;
  critical_action_count: number;
  published_version_id?: string | null;
  published_version?: number | null;
  has_draft: boolean;
  draft_version_id?: string | null;
};
export type HealthConfigProtocolPage = {
  items: HealthConfigProtocolRow[];
  next_cursor?: string | null;
};
export type HealthConfigStep = {
  step_id?: string | null;
  day_no: number;
  seq: number;
  session?: string | null;
  record_type?: string | null;
  medicine_name?: string | null;
  dosage_text?: string | null;
  dosage_denominator?: string | null;
  medicine_route?: string | null;
  instruction?: string | null;
  critical_action_type?: string | null;
};
export type HealthConfigVersionSummary = {
  protocol_version_id: string;
  version: number;
  status: "draft" | "published" | "retired";
  step_count: number;
  duration_days: number;
  published_at?: string | null;
};
export type HealthConfigProtocolDetail = {
  protocol_version_id: string;
  disease_key: string;
  display_name: string;
  age_band: "adult" | "kid";
  status: "draft" | "published" | "retired";
  version: number;
  duration_days: number;
  open_case_count: number;
  row_version?: string | null;
  steps?: HealthConfigStep[];
  history?: HealthConfigVersionSummary[];
};
export type HealthConfigWriteResult = {
  outcome: string;
};
export type HealthConfigFieldError = {
  field: string;
  message: string;
};
export type CreateHealthConfigDiseaseRequest = {
  display_name: string;
  duration_days?: number;
};
export type OpenHealthConfigDraftRequest = {
  disease_key: string;
  age_band: "adult" | "kid";
};
export type SaveHealthConfigDraftRequest = {
  disease_key: string;
  age_band: "adult" | "kid";
  display_name: string;
  duration_days?: number;
  steps: HealthConfigStep[];
};

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


// ---- Feed Analytics (windowed rollups of the frozen sheet; DIRECTED kg only) ----

export type FeedAnalyticsDirectedResponse = AppApiComponents["schemas"]["FeedAnalyticsDirectedResponse"];
export type FeedAnalyticsExecutionResponse = AppApiComponents["schemas"]["FeedAnalyticsExecutionResponse"];
export type FeedAnalyticsExperimentResponse = AppApiComponents["schemas"]["FeedAnalyticsExperimentResponse"];

export type FeedAnalyticsParams = {
  /** Optional: absent means every authorized park. */
  park_id?: string;
  /** Optional inclusive business dates; the backend defaults to the 30 days ending yesterday. */
  date_from?: string;
  date_to?: string;
};

export async function getFeedAnalyticsDirected(
  params: FeedAnalyticsParams,
): Promise<ApiResult<FeedAnalyticsDirectedResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedAnalyticsDirectedResponse>("/feed-analytics/directed", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getFeedAnalyticsExecution(
  params: FeedAnalyticsParams,
): Promise<ApiResult<FeedAnalyticsExecutionResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedAnalyticsExecutionResponse>("/feed-analytics/execution", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

export async function getFeedAnalyticsExperiment(
  params: FeedAnalyticsParams,
): Promise<ApiResult<FeedAnalyticsExperimentResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedAnalyticsExperimentResponse>("/feed-analytics/experiment", {
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
  /**
   * A BREED, not a ration group. The backend resolves it through the breed -> ration-group map
   * (Beetal and Sirohi both land on "Beetal/Sirohi"); see the OpenAPI description for why the two
   * are separate filters rather than one.
   */
  breed?: string;
  shed_tag?: string;
  /** A SET. Sent as a repeated query parameter, never comma-joined — see the api-client serializer. */
  feed_item?: string[];
  /** Half of one filter: both or neither. The backend rejects a lone half rather than defaulting it. */
  grams_op?: "gt" | "gte" | "eq" | "lte" | "lt" | "neq";
  grams_value?: string;
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

// ---------------------------------------------------------------------------------------------
// Procurement vendor register (/procurement/vendors)
// ---------------------------------------------------------------------------------------------

export type ProcurementVendor = AppApiComponents["schemas"]["ProcurementVendor"];
export type ProcurementVendorPage = AppApiComponents["schemas"]["ProcurementVendorPage"];
export type ProcurementVendorWrite = AppApiComponents["schemas"]["ProcurementVendorWrite"];
export type ProcurementVendorCatalog = AppApiComponents["schemas"]["ProcurementVendorCatalog"];

/**
 * One keyset page of the vendor register.
 *
 * `total` on the response is the WHOLE-FILTER count and must be rendered as-is; it is deliberately
 * not `vendors.length` -- the page count is derived from it.
 *
 * Paging is by BOUNDED offset (capped server-side), not a keyset cursor, because the register needs
 * a Back control and a page number and a forward-only cursor can express neither. See the endpoint
 * description for why that is safe here and not a licence to use offset on herd-sized tables.
 */
export async function listProcurementVendors(params: {
  search?: string;
  record_type?: string;
  status?: string;
  state?: string;
  city?: string;
  breed?: string;
  limit?: number;
  offset?: number;
} = {}): Promise<ApiResult<ProcurementVendorPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ProcurementVendorPage>("/procurement/vendors", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

/** The business-managed dropdown vocabularies behind the register's filters and form. */
export async function listProcurementVendorCatalog(): Promise<ApiResult<ProcurementVendorCatalog>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ProcurementVendorCatalog>("/procurement/vendor-catalog", { cache: "no-store" }),
  );
}

export async function createProcurementVendor(
  body: ProcurementVendorWrite,
): Promise<ApiResult<ProcurementVendor>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ProcurementVendor>("/procurement/vendors", { method: "POST", cache: "no-store", body }),
  );
}

/**
 * Replace a vendor. `body.row_version` MUST carry the value read with the row -- the backend
 * rejects a stale one with 409 rather than overwriting another editor's save.
 */
export async function updateProcurementVendor(
  vendorId: string,
  body: ProcurementVendorWrite,
): Promise<ApiResult<ProcurementVendor>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/procurement/vendors/${encodeURIComponent(vendorId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<ProcurementVendor>(path, { method: "PUT", cache: "no-store", body }),
  );
}

/**
 * Change ONLY a vendor's trading status.
 *
 * Deliberately not routed through updateProcurementVendor: that is a replace, so a status flip
 * through it would have to resend every field and would clear anything the caller's screen did not
 * render (payment details, for a caller without the finance permission).
 */
export async function updateProcurementVendorStatus(
  vendorId: string,
  body: { status: string; row_version: number },
): Promise<ApiResult<ProcurementVendor>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/procurement/vendors/${encodeURIComponent(vendorId)}/status` as keyof AppApiPaths & string;
  return request(() =>
    client.request<ProcurementVendor>(path, { method: "POST", cache: "no-store", body }),
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

/**
 * Add one entry to the tenant's feed-item catalog.
 *
 * The odd one out among the Feed Config writes, in two ways worth stating at the call site.
 *
 * It is TENANT-scoped — no `park_id` — because `feed_item_catalog` is keyed on (tenant, item) and
 * both parks author quantities against one vocabulary.
 *
 * And it AUTHORS NO QUANTITY. Adding an item makes the name selectable on the ration grid, the shed
 * factors and the experiment sheds; every combination using it stays unconfigured, and therefore
 * BLOCKED, until a rate is authored for it. Do not "help" by following this call with a rate write:
 * an implicit 0 would record "feed none of it" for every group and tag in the tenant.
 *
 * The four attributes are OPTIONAL here — genuinely optional, unlike `grams_per_head` below — and
 * the optionality carries meaning: omitted stores NULL ("not measured"), while an explicit 0 stores
 * a measured zero. A missing energy value blocks a nutritional rollup, never a feeding decision,
 * which is why absence is representable here and is not on a rate. Out-of-range values are still
 * forwarded verbatim for the backend to reject.
 */
export async function createFeedConfigFeedItem(
  body: CreateFeedConfigFeedItemRequest,
  idempotencyKey = `feed-item-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/feed-items", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/**
 * Retires one feed item, or restores a retired one.
 *
 * This is how a feed item is REMOVED, and it is a status flip rather than a delete: the item's
 * authored rates, shed factors and experiment cells survive untouched, so a past feed sheet stays
 * explainable and putting the item back restores it fully configured. It is not a display setting —
 * generation reads the catalog `WHERE status = 'active'`, so a retired item leaves every feed sheet
 * issued from that point onward.
 */
export async function setFeedConfigFeedItemStatus(
  body: SetFeedConfigFeedItemStatusRequest,
  idempotencyKey = `feed-item-status-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/feed-items/status", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/**
 * Declares a feed on one feeding session's recipe, or withdraws it.
 *
 * THIS IS THE WRITE THAT DECIDES WHETHER A FEED REACHES AN ANIMAL. Generation walks a session's
 * declared slots and looks each one up in the ration grid, so a feed with a grid quantity but no
 * slot is never looked up — it is absent from the sheet, the totals and the packing worklist, and
 * nothing reports a gap. Authoring grams for an undeclared feed looks entirely correct and feeds
 * nobody.
 *
 * Declaring is refused (409 `slot_rates_incomplete`) when the feed has no rate in every cell of the
 * park, because a declared slot is priced for EVERY shed and a missing rate blocks that shed's whole
 * sheet. Withdrawing closes the row rather than deleting it, so issued sheets stay explainable.
 */
export async function setFeedConfigSessionTemplateItem(
  body: SetFeedConfigSessionTemplateItemRequest,
  idempotencyKey = `feed-session-slot-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/session-template-items", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
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
  /**
   * OPTIONAL on this read alone. Omitted means every authored experiment cell in the tenant, across
   * both parks -- the company-wide view. Every other /feed-config read stays park-scoped because it
   * is park-OWNED; an experiment cell carries its own park, so it can be listed tenant-wide.
   */
  park_id?: string;
  shed_id?: string;
  /**
   * ONE PEN of that shed, by its human partition label. Absent returns every pen of the shed.
   *
   * Both halves travel together: this section's rows are PENS, and Castro holds three of them, so
   * shed_id alone answers a coarser question than the list it returns.
   */
  partition_label?: string;
  status?: "active" | "retired";
  /** A SET, sent as a repeated query parameter. A pen survives when any of its cells matches. */
  feed_item?: string[];
  experiment_category?: string;
  /**
   * Half of one filter: both or neither. Named kg_ and not grams_ because this compares an ABSOLUTE
   * PEN TOTAL, while the ration grid's grams_op compares a PER-HEAD RATE.
   */
  kg_op?: "gt" | "gte" | "eq" | "lte" | "lt" | "neq";
  kg_value?: string;
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
/**
 * The park's PEN CATALOG — every operational location a quantity may be authored against.
 *
 * The authoritative answer to "what can I enrol?", and deliberately not derived from the experiment
 * cell list. Deriving it from those rows made two things impossible: a pen that already has some
 * cells looked like it had them ALL (so a new pen of an enrolled shed was unreachable — the Godel 1
 * - Part 8 case), and a pen whose cells happened to fall on another page of the paginated read
 * looked unconfigured. This reads the locations/shed_partitions catalog instead, so a pen holding
 * zero animals is still listed and still authorable, and each row states for itself whether it is
 * already configured.
 */
export async function listFeedConfigPens(params: {
  park_id?: string;
  limit?: number;
  offset?: number;
}): Promise<ApiResult<FeedConfigPenPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigPenPage>("/feed-config/pens", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

/**
 * Author EVERY feed item of one pen in a single atomic write.
 *
 * One request, one transaction, one row in the idempotency ledger: either the pen gets all of its
 * quantities or it gets none. Enrolling through N single-cell posts could half-succeed and leave a
 * pen enrolled (membership is the workflow flag) while being fed a subset of what was entered —
 * which is worse than not enrolling it at all.
 */
export async function upsertFeedConfigExperimentBatch(
  body: UpsertFeedConfigExperimentBatchRequest,
  idempotencyKey = `feed-experiment-batch-${randomUUID()}`,
): Promise<ApiResult<FeedConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedConfigWriteResult>("/feed-config/experiment/batch", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

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
/**
 * One bounded page of the authored treatment rulebook: one row per disease per age band, each
 * carrying the LIVE published version and the OPEN DRAFT side by side.
 *
 * There is no total and no page-count, and that is deliberate rather than an omission: the backend
 * returns a keyset `next_cursor` because counting the filtered catalog on every request is
 * compute-on-read. A "N protocols" headline computed from the visible page would be a false
 * statement about the catalog.
 */
export async function listHealthConfigProtocols(params: {
  age_band?: "adult" | "kid";
  search?: string;
  draft_only?: boolean;
  cursor?: string;
  limit?: number;
}): Promise<ApiResult<HealthConfigProtocolPage>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<HealthConfigProtocolPage>("/health-config/protocols", {
      cache: "no-store",
      query: compactQuery(params),
    }),
  );
}

/** One protocol version with its ordered steps, version history and open-case count. */
export async function getHealthConfigProtocol(
  protocolVersionId: string,
): Promise<ApiResult<HealthConfigProtocolDetail>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/health-config/protocols/${encodeURIComponent(protocolVersionId)}` as keyof AppApiPaths &
    string;
  return request(() => client.request<HealthConfigProtocolDetail>(path, { cache: "no-store" }));
}

/**
 * Add a disease. Opens a draft for BOTH age bands; nothing is live until each is published.
 *
 * `duration_days` is optional here on purpose, and the optionality carries meaning: OMITTED takes
 * the backend's declared default, while a present out-of-range value is forwarded verbatim so the
 * backend rejects it. Nothing in this layer clamps, rounds, or turns a cleared input into 0.
 */
export async function createHealthConfigDisease(
  body: CreateHealthConfigDiseaseRequest,
  idempotencyKey = `health-disease-${randomUUID()}`,
): Promise<ApiResult<HealthConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<HealthConfigWriteResult>("/health-config/diseases", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/**
 * Open the draft for a protocol, copying the published version if none is open.
 *
 * No idempotency key: at most one draft can exist per protocol, so the uniqueness constraint IS the
 * idempotency and a repeated call returns the same draft.
 */
export async function openHealthConfigDraft(
  body: OpenHealthConfigDraftRequest,
): Promise<ApiResult<HealthConfigProtocolDetail>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<HealthConfigProtocolDetail>("/health-config/drafts", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

/** Replace a draft's whole content. Steps carry no seq — order is positional. */
export async function saveHealthConfigDraft(
  body: SaveHealthConfigDraftRequest,
  idempotencyKey = `health-draft-save-${randomUUID()}`,
): Promise<ApiResult<HealthConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<HealthConfigWriteResult>("/health-config/drafts/save", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

/** Publish a draft, retiring the version it replaces. Open cases keep their pinned version. */
export async function publishHealthConfigDraft(
  protocolVersionId: string,
  idempotencyKey = `health-draft-publish-${randomUUID()}`,
): Promise<ApiResult<HealthConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/health-config/protocols/${encodeURIComponent(protocolVersionId)}/publish` as keyof AppApiPaths &
    string;
  return request(() =>
    client.request<HealthConfigWriteResult>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
    }),
  );
}

/** Discard a draft without publishing it. */
export async function discardHealthConfigDraft(
  protocolVersionId: string,
  idempotencyKey = `health-draft-discard-${randomUUID()}`,
): Promise<ApiResult<HealthConfigWriteResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/health-config/protocols/${encodeURIComponent(protocolVersionId)}/discard` as keyof AppApiPaths &
    string;
  return request(() =>
    client.request<HealthConfigWriteResult>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
    }),
  );
}

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

export type VaccinationDriveDateOverrideResponse = {
  park_id: string;
  vaccine_code: string;
  original_drive_date: string;
  override_date: string;
  requested_override_date?: string;
  applied_override_date?: string;
  auto_shifted?: boolean;
  shift_reason?: string;
  conflicting_vaccine_code?: string;
  conflicting_vaccine_label?: string;
  conflicting_date?: string;
  conflicting_rule?: string;
  reason: string;
  created_by: string;
  created_at: string;
};

export async function postponeVaccinationDriveDate(body: {
  park_id: string;
  vaccine_code: string;
  original_drive_date: string;
  override_date: string;
  reason: string;
}, idempotencyKey = `vaccination-drive-date-override-${randomUUID()}`): Promise<ApiResult<VaccinationDriveDateOverrideResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationDriveDateOverrideResponse>("/vaccination/schedule/drive-date-overrides", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

export async function getVaccinationExecutionShedDrilldown(
  shedId: string,
  params: { asOf?: string; partitionLabel?: string } = {},
): Promise<ApiResult<VaccinationExecutionShedDrilldown>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/execution/sheds/${encodeURIComponent(shedId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<VaccinationExecutionShedDrilldown>(path, {
      cache: "no-store",
      query: compactQuery({ as_of: params.asOf, partition_label: params.partitionLabel }),
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
  /**
   * Park of the selected drive. Narrows the board's sections to that park's share of the drive
   * while leaving driveOptions at parkId's scope, so one request serves both the narrowed numbers
   * and the full picker.
   */
  driveParkId?: string;
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
        drive_park_id: params.driveParkId,
      }),
    }),
  );
}

// Live drive-day tracker. ONE read backs the whole page: KPI tiles, operator board, shed proof
// progress, combo doses, activity feed, attention and verification. It is one call rather than six
// because a single filter set has to narrow every section at once — and because this page polls, so
// each extra endpoint would multiply the refresh cost.
export async function getVaccinationLiveTracker(
  params: {
    businessDate?: string;
    parkId?: string;
    shedId?: string;
    partitionLabel?: string;
    operatorId?: string;
    vaccineCode?: string;
    status?: VaccinationLiveTrackerStatus;
    activityLimit?: number;
    // Both halves of the feed's keyset cursor. The feed's sort key is (occurred_at, event_id);
    // sending the timestamp alone drops every event tied with the previous page's last row.
    activityBefore?: string;
    activityBeforeId?: string;
  } = {},
): Promise<ApiResult<VaccinationLiveTrackerResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(6000, (signal) =>
      client.request<VaccinationLiveTrackerResponse>("/vaccination/live-tracker", {
        cache: "no-store",
        signal,
        query: compactQuery({
          business_date: params.businessDate,
          park_id: params.parkId,
          shed_id: params.shedId,
          partition_label: params.partitionLabel,
          operator_id: params.operatorId,
          vaccine_code: params.vaccineCode,
          status: params.status,
          activity_limit: params.activityLimit,
          activity_before: params.activityBefore,
          activity_before_id: params.activityBeforeId,
        }),
      }),
    ),
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
export async function listVerificationQueue(
  params: {
    category?: string;
    vertical?: string;
    module?: string;
    // navModule is the verifier-drawer MODULE key (filter_options.modules): it filters to every
    // category that module registers, so Feed means distribution + packing + transport. Distinct
    // from `module`, which filters the item's own module column.
    navModule?: string;
    // "all" is the explicit no-status-filter selection; omitting status lands on pending.
    status?: VerificationItemStatus | "all";
    businessDate?: string;
    // Inclusive capture-date range. Sent INSTEAD of businessDate — the backend answers 400
    // invalid_date_scope if both arrive, and 400 invalid_business_date_range unless both ends of
    // the range are present.
    businessDateFrom?: string;
    businessDateTo?: string;
    missed?: boolean;
    parkId?: string;
    shedId?: string;
    cursor?: string;
    limit?: number;
  } = {},
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
        nav_module: params.navModule,
        status: params.status,
        business_date: params.businessDate,
        business_date_from: params.businessDateFrom,
        business_date_to: params.businessDateTo,
        missed: params.missed,
        park_id: params.parkId,
        shed_id: params.shedId,
        cursor: params.cursor,
        limit: params.limit ?? 20,
      }),
    }),
  );
  if (!result.ok) return result;
  return { ok: true, data: absolutizeVerificationMedia(result.data, config.data.baseUrl) };
}

export type VerificationOversightAnalyticsResponse =
  AppApiComponents["schemas"]["VerificationOversightAnalyticsResponse"];

// CEO/PC-Director oversight analytics (GET /verification/oversight-analytics), gated on
// permissions.VerificationOversee -- the same capability as the /verify page contract's
// oversight_analytics control. A caller without the capability gets 403 here; the page must only
// call this when controlEnabled(pageContract, "oversight_analytics", false) is true, so the
// component never renders a bare error card for a verifier.
export async function getVerificationOversightAnalytics(): Promise<ApiResult<VerificationOversightAnalyticsResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VerificationOversightAnalyticsResponse>("/verification/oversight-analytics", {
      cache: "no-store",
    }),
  );
}

export type VerificationVideoLogResponse = AppApiComponents["schemas"]["VerificationVideoLogResponse"];

// The VIDEO LOG (GET /verification/video-log): for one business day, per shed, when each proof
// arrived. Gated on permissions.VerificationEvidenceTimeline -- the same capability as the /verify
// page contract's video_log control, and a DIFFERENT one from verification.oversee, so the verifier
// reaches this while the oversight analytics stay leadership-only. The page must only call this
// when controlEnabled(pageContract, "video_log", false) is true, so a caller without the capability
// never renders a bare error card.
export async function getVerificationVideoLog(params: {
  businessDate?: string;
  parkId?: string;
  shedId?: string;
  /**
   * Whole-day EXPORT: every shed's rows, each carrying its own location. Reserved for the CSV
   * download — the panel never renders this, because a park-day can carry several hundred items.
   */
  allSheds?: boolean;
}): Promise<ApiResult<VerificationVideoLogResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VerificationVideoLogResponse>("/verification/video-log", {
      cache: "no-store",
      query: compactQuery({
        business_date: params.businessDate,
        park_id: params.parkId,
        shed_id: params.shedId,
        all_sheds: params.allSheds ? "true" : undefined,
      }),
    }),
  );
}

/**
 * Record the Verifier's approve/reject decision on one verification item
 * (POST /verification/items/{item_id}/verdict, gated on verification.verdict -- the verifier role
 * ALONE, never CEO/CxO/director/park-head, per the verdict-exclusivity lock in AGENTS.md).
 *
 * `row_version` is the item's optimistic-concurrency guard: a stale value returns 409 rather than
 * overwriting a verdict someone else recorded between page render and submit. A rejection without
 * a reason is refused by the backend with 422 — the UI requires the reason too, but the backend is
 * the rule's owner.
 *
 * The Idempotency-Key header is REQUIRED (8-200 chars) — the route rejects a request without one.
 * Note the rejection is reported as `invalid_json`, the same code a malformed body gets, so a
 * missing key looks exactly like a broken payload when debugging.
 */
export async function recordVerificationVerdict(
  itemId: string,
  body: VerificationVerdictRequest,
  idempotencyKey: string,
): Promise<ApiResult<VerificationVerdictResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/verification/items/${encodeURIComponent(itemId)}/verdict` as keyof AppApiPaths & string;
  return request(() =>
    client.request<VerificationVerdictResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

// correctWeighingObservationWeight replaces the weight a verifier judged wrong, on the observation
// the verification item points at. Same route the phone calls: one act, one rule, one endpoint.
export async function correctWeighingObservationWeight(
  observationId: string,
  body: WeighingWeightCorrectionRequest,
  idempotencyKey: string,
): Promise<ApiResult<WeighingWeightCorrectionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/app/weighing/observations/${encodeURIComponent(observationId)}/weight-correction` as keyof AppApiPaths &
    string;
  return request(() =>
    client.request<WeighingWeightCorrectionResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
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

// Whole-pen stage change. Preview is a pure read and deliberately carries NO Idempotency-Key: the
// drawer re-checks whenever the operator changes the pen or the target stage, and burning a key per
// keystroke would leave the commit unable to reuse one. Commit is idempotent on Idempotency-Key,
// which is the only thing stopping a double-clicked button from re-emitting stage-change events --
// this write has no approval step behind it.
export async function previewReclassifyShedStage(
  body: ReclassifyShedStageRequest,
): Promise<ApiResult<ReclassifyShedStagePreviewResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ReclassifyShedStagePreviewResponse>("/admin/goats/shed-stage/preview", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function commitReclassifyShedStage(
  body: ReclassifyShedStageRequest,
  idempotencyKey: string,
): Promise<ApiResult<ReclassifyShedStageResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ReclassifyShedStageResponse>("/admin/goats/shed-stage/commit", {
      method: "POST",
      cache: "no-store",
      headers: { "Idempotency-Key": idempotencyKey },
      body,
    }),
  );
}

// Census-slice correction: fix a wrongly recorded breed or sex on ONE Counts Breakdown row. Same
// preview/commit split and the same reasoning as the stage change above -- the preview is a pure
// read that repeats freely, the commit is idempotent on Idempotency-Key.
//
// Scope differs from the stage change and that is the point: this touches the ROW's animals, not
// the whole pen, because breed and sex belong to the animal while a cohort tag belongs to the pen.
export async function previewCorrectCensusSlice(
  body: CorrectCensusSliceRequest,
): Promise<ApiResult<CensusSliceCorrectionPreviewResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CensusSliceCorrectionPreviewResponse>("/admin/goats/census-slice/preview", {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function commitCorrectCensusSlice(
  body: CorrectCensusSliceRequest,
  idempotencyKey: string,
): Promise<ApiResult<CensusSliceCorrectionResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CensusSliceCorrectionResponse>("/admin/goats/census-slice/commit", {
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

export function compactQuery(
  values: Record<string, string | number | boolean | readonly string[] | null | undefined>,
) {
  const query: Record<string, string | number | boolean | readonly string[]> = {};
  for (const [key, value] of Object.entries(values)) {
    if (value === null || value === undefined || value === "") continue;
    // An EMPTY array is dropped like an empty string: a multi-valued filter with nothing selected is
    // "no filter", and emitting `feed_item=` would ask the backend for an item named "".
    if (Array.isArray(value) && value.length === 0) continue;
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
  // Backend-composed display copy (added 2026-08-05 with the mobile Approvals module). The queue
  // now resolves the raiser and the shed ids to NAMES server-side, so both surfaces read one
  // authored line instead of each composing its own from raw ids. Optional by contract: absent
  // when nothing was resolvable, in which case a renderer drops the line rather than showing an id.
  //
  // This page does not render raised_by_name or summary_line today — it deliberately omits the
  // raiser and builds its own readable subject from resolved location names (see readableSubject).
  // They are declared so the shape stays true to the contract and so this page can adopt the
  // shared line later.
  raised_by_name?: string;
  summary_line?: string;
  shifting_event_id?: string;
  subject_goat_id?: string;
  // subject_animal_location: present only for a death request whose animal resolves to a real
  // park/shed. BACKEND-OWNED DISPLAY COPY — the same park/shed/partition fact already folded into
  // summary_line, exposed as its own field so this page's structured (non-summary_line) rendering
  // can show it directly instead of parsing it back out of a composed string. Render verbatim;
  // absent means the animal's location could not be resolved, in which case drop the row rather
  // than falling back to an id.
  subject_animal_location?: string;
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

/**
 * Post a batch of verification review events to the backend for proof-of-watching.
 * (POST /verification/review-events, gated on verification.verdict -- verifier-only authority.
 * Reading the derived facts is gated on verification.review instead, so leadership can SEE the
 * integrity signal it is not allowed to write.)
 *
 * Events are buffered client-side and submitted in batches (max 200 per batch).
 * The client_event_id is the idempotency key: a replay of the same batch with
 * the same client_event_ids inserts nothing new.
 */
export async function postVerificationReviewEvents(
  events: VerificationReviewEvent[],
  idempotencyKey?: string,
): Promise<ApiResult<VerificationReviewEventBatchResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = "/verification/review-events" as keyof AppApiPaths & string;
  return request(() =>
    client.request<VerificationReviewEventBatchResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotencyKey ? { "Idempotency-Key": idempotencyKey } : undefined,
      body: { events },
    }),
  );
}
