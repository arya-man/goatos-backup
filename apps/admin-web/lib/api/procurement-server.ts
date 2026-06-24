import "server-only";

// Server-only generated-client fetchers for the procurement source-entry slice. Every call goes through
// the tenant-scoped admin API client and the same ApiResult envelope + helpers as lib/api/server.ts.
// There is no client-side mock, fixture, or local route handler — these hit the backend process-integrity
// and source-entry read models (GET) and the operator write contracts (POST, idempotency-keyed) directly.
import { createAdminApiClient } from "@goatos/api-client";
import type { AdminApiPaths } from "@goatos/api-client";
import {
  apiClientOptions,
  compactQuery,
  getServerConfig,
  request,
  type ApiResult,
} from "@/lib/api/server";
import type {
  AcceptProcurementIntakeRequest,
  AddProcurementLoadGoatRequest,
  CreateProcurementLoadRequest,
  DispatchProcurementLoadRequest,
  ProcurementArrivalReviewResponse,
  ProcurementDecisionResponse,
  ProcurementIntakeHandoffResponse,
  ProcurementLoadDetailResponse,
  ProcurementLoadGoatResponse,
  ProcurementLoadListResponse,
  ProcurementLoadResponse,
  ProcurementSourceHealthResponse,
  ProcurementTransitHandoffResponse,
  RecordProcurementArrivalReviewRequest,
  RecordProcurementDecisionRequest,
  RecordProcurementSourceHealthRequest,
} from "@/lib/api/procurement";

// NOTE: command lenses are TOP-LEVEL for every vertical. Procurement Action Center / Protocol Adherence /
// Control Tower / Workflow data is served by the top-level command screens via ?domain=procurement (or a
// generic process-integrity endpoint) — NOT by nested /procurement/source-entry/* lens routes. The backend
// no longer registers those nested routes, so no client helpers exist for them here.

// Source Entry Board — loads grouped by state, owner, warmup age, proof, next action. Capped + cursored:
// the backend list endpoint accepts an opaque `cursor` and returns `next_cursor`, so the board can page
// through every load at scale instead of stopping at the first page.
export async function listProcurementLoads(
  params: { status?: string; limit?: number; cursor?: string } = {},
): Promise<ApiResult<ProcurementLoadListResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ProcurementLoadListResponse>("/procurement/source-entry/loads", {
      cache: "no-store",
      query: compactQuery({ status: params.status, limit: params.limit ?? 200, cursor: params.cursor }),
    }),
  );
}

// Load Detail — full journey: load, per-goat rows, holding stays, source health, pre-dispatch decisions,
// transit handoffs, arrival reviews (arrival gate), PHC handoffs, and the merged timeline.
export async function getProcurementLoad(loadId: string): Promise<ApiResult<ProcurementLoadDetailResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/loads/${encodeURIComponent(loadId)}` as keyof AdminApiPaths & string;
  return request(() => client.request<ProcurementLoadDetailResponse>(path, { cache: "no-store" }));
}

// ---- Write flows (operator POST actions) ----
// Each sends an Idempotency-Key so retries cannot duplicate a load, decision, dispatch, or intake. The
// backend derives the actor from the auth token; the body carries only operator-entered data.
function idempotentHeaders(idempotencyKey: string) {
  return { "Idempotency-Key": idempotencyKey };
}

export async function createProcurementLoad(
  body: CreateProcurementLoadRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementLoadResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<ProcurementLoadResponse>("/procurement/source-entry/loads", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function addProcurementLoadGoat(
  loadId: string,
  body: AddProcurementLoadGoatRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementLoadGoatResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/loads/${encodeURIComponent(loadId)}/goats` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementLoadGoatResponse>(path, { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }),
  );
}

export async function recordProcurementSourceHealth(
  goatId: string,
  body: RecordProcurementSourceHealthRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementSourceHealthResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/goats/${encodeURIComponent(goatId)}/source-health` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementSourceHealthResponse>(path, { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }),
  );
}

export async function recordProcurementPreDispatchDecision(
  goatId: string,
  body: RecordProcurementDecisionRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementDecisionResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/goats/${encodeURIComponent(goatId)}/pre-dispatch-decision` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementDecisionResponse>(path, { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }),
  );
}

export async function dispatchProcurementLoad(
  loadId: string,
  body: DispatchProcurementLoadRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementTransitHandoffResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/loads/${encodeURIComponent(loadId)}/dispatch` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementTransitHandoffResponse>(path, { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }),
  );
}

export async function recordProcurementArrivalReview(
  loadId: string,
  body: RecordProcurementArrivalReviewRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementArrivalReviewResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/loads/${encodeURIComponent(loadId)}/arrival-review` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementArrivalReviewResponse>(path, { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }),
  );
}

export async function acceptProcurementIntake(
  loadId: string,
  body: AcceptProcurementIntakeRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementIntakeHandoffResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/loads/${encodeURIComponent(loadId)}/accept-intake` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementIntakeHandoffResponse>(path, { method: "POST", cache: "no-store", headers: idempotentHeaders(idempotencyKey), body }),
  );
}
