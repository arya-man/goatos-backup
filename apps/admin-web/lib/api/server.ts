import "server-only";

import { randomUUID } from "crypto";
import { createAdminApiClient, createAppApiClient, GoatOSApiError } from "@goatos/api-client";
import type { AdminApiComponents, AdminApiPaths, AppApiComponents, AppApiPaths } from "@goatos/api-client";
import { cache } from "react";
import { getFirebaseIdTokenCookie } from "@/lib/auth/server-session";
import { mintLocalDevBearerToken } from "./local-dev-token";

type ErrorEnvelope = AppApiComponents["schemas"]["ErrorEnvelope"];

export type AdminWebBootstrapResponse = AppApiComponents["schemas"]["AdminWebBootstrapResponse"];
export type AdminWebPageContract = AppApiComponents["schemas"]["AdminWebPageContract"];
export type GoatPassportResponse = AppApiComponents["schemas"]["GoatPassportResponse"];
export type GoatSearchResponse = AppApiComponents["schemas"]["GoatSearchResponse"];
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
export type AnimalStageItem = AppApiComponents["schemas"]["AnimalStageItem"];
export type AnimalStageListResponse = AppApiComponents["schemas"]["AnimalStageListResponse"];
export type VaccinationPassportDue = AppApiComponents["schemas"]["VaccinationPassportDue"];
export type VaccinationPassportHistoryItem = AppApiComponents["schemas"]["VaccinationPassportHistoryItem"];
export type VaccinationPassport = AppApiComponents["schemas"]["VaccinationPassport"];
export type AcceptVaccinationCompletionResponse = AppApiComponents["schemas"]["AcceptVaccinationCompletionResponse"];
export type RejectVaccinationCompletionResponse = AppApiComponents["schemas"]["RejectVaccinationCompletionResponse"];
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

export type AdminGoatResponse = AdminApiComponents["schemas"]["AdminGoatResponse"];
export type CreateAdminGoatRequest = AdminApiComponents["schemas"]["CreateAdminGoatRequest"];
export type AdminGoatBulkPreviewRequest = AdminApiComponents["schemas"]["AdminGoatBulkPreviewRequest"];
export type AdminGoatBulkCommitRequest = AdminApiComponents["schemas"]["AdminGoatBulkCommitRequest"];
export type AdminGoatBulkResponse = AdminApiComponents["schemas"]["AdminGoatBulkResponse"];
export type AdminGoatBulkRowResult = AdminApiComponents["schemas"]["AdminGoatBulkRowResult"];
export type AdminGoatBulkSummary = AdminApiComponents["schemas"]["AdminGoatBulkSummary"];
export type GenerationStatus = AdminApiComponents["schemas"]["GenerationStatus"];
export type StageGoatRequest = AdminApiComponents["schemas"]["StageGoatRequest"];
export type HealthGoatRequest = AdminApiComponents["schemas"]["HealthGoatRequest"];
export type LocationSummary = AdminApiComponents["schemas"]["LocationSummary"];
export type LocationListResponse = AdminApiComponents["schemas"]["LocationListResponse"];
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
};

export type ApiResult<T> =
  | { ok: true; data: T }
  | { ok: false; error: ApiUiError };

type ServerConfig = {
  baseUrl: string;
  bearerToken: string;
  tenantId: string;
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

export async function getServerConfig(requireTenant = false): Promise<ApiResult<ServerConfig>> {
  const baseUrl = process.env.GOATOS_API_BASE_URL ?? "http://127.0.0.1:8080";
  const firebaseIdToken = await getFirebaseIdTokenCookie();
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
    },
  };
}

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
  return {
    baseUrl: config.baseUrl,
    bearerToken: config.bearerToken,
    tenantId: config.tenantId || undefined,
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

export const getAdminWebBootstrap = cache(async (): Promise<ApiResult<AdminWebBootstrapResponse>> => {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<AdminWebBootstrapResponse>("/admin-web/bootstrap", { cache: "no-store" }));
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
  return request(() =>
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
  return request(() =>
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
}

// Control Tower — exception-only leadership summary + alerts (real /control-tower/vaccination).
export async function getVaccinationControlTower(
  params: { parkId?: string; shedId?: string; asOf?: string; dueBefore?: string; limit?: number } = {},
): Promise<ApiResult<ControlTowerResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ControlTowerResponse>("/control-tower/vaccination", {
      cache: "no-store",
      query: compactQuery({ park_id: params.parkId, shed_id: params.shedId, as_of: params.asOf, due_before: params.dueBefore, limit: params.limit }),
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
  params: { parkId?: string; limit?: number } = {},
): Promise<ApiResult<VaccinationQueueResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationQueueResponse>("/vaccination/verification-queue", {
      cache: "no-store",
      query: compactQuery({ park_id: params.parkId, limit: params.limit ?? 100 }),
    }),
  );
}

export async function getVaccinationExecution(
  params: { parkId?: string; workState?: VaccinationExecutionWorkState; asOf?: string; limit?: number } = {},
): Promise<ApiResult<VaccinationExecutionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationExecutionResponse>("/vaccination/execution", {
      cache: "no-store",
      query: compactQuery({ park_id: params.parkId, work_state: params.workState, as_of: params.asOf, limit: params.limit }),
    }),
  );
}

// Source-backed vaccination operations read model — cohort × protocol matrix + per-cohort detail with real
// last_dose. Powers the /vaccination matrix + cohort-detail sections (NOT the Action Center pivot).
export async function getVaccinationOperations(
  params: { parkId?: string; asOf?: string; dueBefore?: string; limit?: number } = {},
): Promise<ApiResult<VaccinationOperationsResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<VaccinationOperationsResponse>("/vaccination/operations", {
      cache: "no-store",
      query: compactQuery({ park_id: params.parkId, as_of: params.asOf, due_before: params.dueBefore, limit: params.limit }),
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

// listAnimalStages reads the tenant's active animal-stage reference data (animal_stage_lookup) so the
// Config authoring stage picker is backend-driven, not hardcoded K0/K1/K2 literals (PHC vaccination
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
}): Promise<ApiResult<{ protocol_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() => client.request<{ protocol_id: string }>("/protocols", { method: "POST", cache: "no-store", body }));
}

export async function createProtocolVersion(
  protocolId: string,
  body: {
    scope_type: string;
    scope_id?: string;
    version: number;
    effective_from: string;
    rule_dsl: unknown;
    proof_policy?: unknown;
    sop_version_id?: string;
  },
): Promise<ApiResult<{ protocol_version_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/${encodeURIComponent(protocolId)}/versions` as keyof AppApiPaths & string;
  return request(() => client.request<{ protocol_version_id: string }>(path, { method: "POST", cache: "no-store", body }));
}

export async function addProtocolRule(versionId: string, body: Record<string, unknown>): Promise<ApiResult<{ rule_id: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/versions/${encodeURIComponent(versionId)}/rules` as keyof AppApiPaths & string;
  return request(() => client.request<{ rule_id: string }>(path, { method: "POST", cache: "no-store", body }));
}

export async function publishProtocolVersion(versionId: string): Promise<ApiResult<Record<string, never>>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/protocols/versions/${encodeURIComponent(versionId)}/publish` as keyof AppApiPaths & string;
  return request(() => client.request<Record<string, never>>(path, { method: "POST", cache: "no-store" }));
}

export async function getGoatVaccinationPassport(goatId: string): Promise<ApiResult<VaccinationPassport>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/goats/${encodeURIComponent(goatId)}/passport` as keyof AppApiPaths & string;
  return request(() => client.request<VaccinationPassport>(path, { cache: "no-store" }));
}

export async function acceptVaccinationCompletion(
  completionId: string,
): Promise<ApiResult<AcceptVaccinationCompletionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/completions/${encodeURIComponent(completionId)}/accept` as keyof AppApiPaths & string;
  return request(() => client.request<AcceptVaccinationCompletionResponse>(path, { method: "POST", cache: "no-store" }));
}

export async function rejectVaccinationCompletion(
  completionId: string,
  reason: string,
): Promise<ApiResult<RejectVaccinationCompletionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/vaccination/completions/${encodeURIComponent(completionId)}/reject` as keyof AppApiPaths & string;
  return request(() => client.request<RejectVaccinationCompletionResponse>(path, { method: "POST", cache: "no-store", body: { reason } }));
}

// ---- Proof upload wrappers for vaccination drawer ----
// These enable the frontend to initiate proof uploads for vaccination completions and task submissions.

export async function createProofUpload(
  obligationId: string,
): Promise<ApiResult<CreateProofUploadResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<CreateProofUploadResponse>("/app/proofs/uploads", {
      method: "POST",
      cache: "no-store",
      body: { obligation_id: obligationId },
    }),
  );
}

export async function uploadProofLocal(
  proofId: string,
  file: File,
): Promise<ApiResult<void>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const formData = new FormData();
  formData.append("file", file);
  const path = `/app/proofs/${encodeURIComponent(proofId)}/upload` as keyof AppApiPaths & string;
  return request(() =>
    client.request<void>(path, {
      method: "PUT",
      cache: "no-store",
      body: formData,
    }),
  );
}

export async function completeProofUpload(
  proofId: string,
  mediaType: string,
): Promise<ApiResult<ProofResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/app/proofs/${encodeURIComponent(proofId)}/complete` as keyof AppApiPaths & string;
  return request(() =>
    client.request<ProofResponse>(path, {
      method: "POST",
      cache: "no-store",
      body: { media_type: mediaType },
    }),
  );
}

export async function submitAppTask(
  taskId: string,
  submissionData: Record<string, unknown>,
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

export async function listSops(params: { status?: string; limit?: number } = {}): Promise<ApiResult<SOPListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SOPListResponse>("/admin/sops", {
      cache: "no-store",
      query: compactQuery({ status: params.status, limit: params.limit ?? 200 }),
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

export async function healthGoat(
  goatId: string,
  body: HealthGoatRequest,
  idempotencyKey: string,
): Promise<ApiResult<AdminGoatResponse>> {
  const config = await getServerConfig();
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/admin/goats/${encodeURIComponent(goatId)}/health` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<AdminGoatResponse>(path, {
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

export async function request<T>(fn: () => Promise<T>): Promise<ApiResult<T>> {
  try {
    return { ok: true, data: await fn() };
  } catch (error) {
    return { ok: false, error: normalizeApiError(error) };
  }
}

function normalizeApiError(error: unknown): ApiUiError {
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
