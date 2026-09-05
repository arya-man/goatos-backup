import "server-only";

// Server-only generated-client fetchers for the procurement source-entry slice. Every call goes through
// the tenant-scoped admin API client and the same ApiResult envelope + helpers as lib/api/server.ts.
// There is no client-side mock, fixture, or local route handler — these hit the backend process-integrity
// and source-entry read models (GET) and the operator write contracts (POST, idempotency-keyed) directly.
import { createAdminApiClient, createAppApiClient } from "@goatos/api-client";
import type { AdminApiPaths, AppApiPaths } from "@goatos/api-client";
import {
  apiClientOptions,
  compactQuery,
  getServerConfig,
  request,
  type ApiResult,
} from "@/lib/api/server";
import type { LoadCostWrite, LoadwiseSales, LoadwiseWeights,
  FeedPurchase,
  FeedPurchaseOptions,
  FeedPurchasePage,
  FeedPurchasePaymentWrite,
  FeedPurchaseEdit,
  FeedPurchaseDeliveryWrite,
  FeedPurchaseStatusWrite,
  FeedPurchaseWrite,
  SalesBenchmarkWrite,
  SalesBuyerLead,
  SalesBuyerLeadPage,
  SalesBuyerLeadWrite,
  SalesDeal,
  SalesDealPage,
  SalesDealPaymentWrite,
  SalesDealStatusWrite,
  SalesDealWrite,
  SalesFpoLead,
  SalesFpoLeadPage,
  SalesFpoLeadWrite,
  SalesLeadStatusWrite,
  SalesOverview,
  SalesRecorded,
  SalesSoldTagsResult,
  SalesSoldTagsWrite,
  SalesWeightCheckWrite,
} from "@/lib/api/procurement";
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
  ProcurementHFVaccinationEvidenceResponse,
  ProcurementSourceHealthResponse,
  ProcurementTransitHandoffResponse,
  RecordProcurementHFVaccinationEvidenceRequest,
  RecordProcurementArrivalReviewRequest,
  RecordProcurementDecisionRequest,
  RecordProcurementSourceHealthRequest,
  ReviewProcurementHFVaccinationEvidenceRequest,
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
// transit handoffs, arrival reviews (arrival gate), Preventive Care (PC) handoffs, and the merged timeline.
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

export async function recordProcurementHFVaccinationEvidence(
  goatId: string,
  body: RecordProcurementHFVaccinationEvidenceRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementHFVaccinationEvidenceResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/goats/${encodeURIComponent(goatId)}/hf-vaccination-evidence` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementHFVaccinationEvidenceResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function reviewProcurementHFVaccinationEvidence(
  evidenceId: string,
  body: ReviewProcurementHFVaccinationEvidenceRequest,
  idempotencyKey: string,
): Promise<ApiResult<ProcurementHFVaccinationEvidenceResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAdminApiClient(apiClientOptions(config.data));
  const path = `/procurement/source-entry/hf-vaccination-evidence/${encodeURIComponent(evidenceId)}/review` as keyof AdminApiPaths & string;
  return request(() =>
    client.request<ProcurementHFVaccinationEvidenceResponse>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
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

// ---- Sales (app API) ----
// The sales board reads the whole-page overview contract and one bounded page of the deals ledger.
// These go through the APP api client (the /sales endpoints live in app-api.yaml), with the same
// ApiResult envelope as everything else in this file.

export async function getSalesOverview(
  params: { farm?: string } = {},
): Promise<ApiResult<SalesOverview>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesOverview>("/sales/overview", {
      cache: "no-store",
      query: compactQuery({ farm: params.farm }),
    }),
  );
}

// The load-wise reconciliation: every purchased load's counts and money, served whole (the
// newest window) by the procurement read. ONE bounded request, never a paged walk. park_id is
// the top-bar park selector's value (a location UUID); the backend narrows rows, totals and
// summary together so the page cannot show a filtered chart over unfiltered tiles.
export async function getLoadwiseSales(
  params: { park_id?: string } = {},
): Promise<ApiResult<LoadwiseSales>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LoadwiseSales>("/procurement/loadwise-sales", {
      cache: "no-store",
      query: compactQuery({ park_id: params.park_id }),
    }),
  );
}

/**
 * The UNPRICED load read for the ADG Analytics Comparison tab: identity, head counts and the
 * bought-at weight, and nothing costed. A principal who may monitor weighing but may not read
 * sales money is served this and only this; getLoadwiseSales stays behind sales read access.
 */
export async function getLoadwiseWeights(
  params: { park_id?: string } = {},
): Promise<ApiResult<LoadwiseWeights>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<LoadwiseWeights>("/procurement/loadwise-weights", {
      cache: "no-store",
      query: compactQuery({ park_id: params.park_id }),
    }),
  );
}

// Records (or clears) one load's landed cost. A PUT of the full state — naturally idempotent on
// the backend, so no Idempotency-Key header is minted here.
export async function setLoadCost(
  loadId: string,
  body: LoadCostWrite,
): Promise<ApiResult<{ status: string }>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<{ status: string }>(
      `/procurement/loads/${encodeURIComponent(loadId)}/cost` as keyof AppApiPaths & string,
      { method: "PUT", cache: "no-store", body },
    ),
  );
}

export async function listSalesDeals(
  params: { farm?: string; limit?: number; offset?: number } = {},
): Promise<ApiResult<SalesDealPage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesDealPage>("/sales/deals", {
      cache: "no-store",
      query: compactQuery({ farm: params.farm, limit: params.limit, offset: params.offset }),
    }),
  );
}

export async function createSalesDeal(
  body: SalesDealWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesDeal>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesDeal>("/sales/deals", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

// ---- Feed purchases (/procurement/feed-purchases). The BUYING side of the feed chain: these are
// the loads the stock and days-left cards on /feed/analytics are counted from. ----

export async function listFeedPurchases(
  params: { farm?: string; delivery?: string; limit?: number; offset?: number } = {},
): Promise<ApiResult<FeedPurchasePage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPurchasePage>("/procurement/feed-purchases", {
      cache: "no-store",
      query: compactQuery({ farm: params.farm, delivery: params.delivery, limit: params.limit, offset: params.offset }),
    }),
  );
}

/**
 * Marks a load reached, or corrects an already-reached load's arrival day / received weight. The
 * backend flips the state, starts stock from the arrival day and raises the toxin test in one
 * transaction on the first call; later calls only move the figures.
 */
export async function recordFeedPurchaseDelivery(
  purchaseId: string,
  body: FeedPurchaseDeliveryWrite,
): Promise<ApiResult<FeedPurchase>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPurchase>(`/procurement/feed-purchases/${encodeURIComponent(purchaseId)}/delivery` as keyof AppApiPaths & string, {
      method: "PUT",
      cache: "no-store",
      body,
    }),
  );
}

export async function getFeedPurchaseOptions(): Promise<ApiResult<FeedPurchaseOptions>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPurchaseOptions>("/procurement/feed-purchase-options", { cache: "no-store" }),
  );
}

export async function createFeedPurchase(
  body: FeedPurchaseWrite,
  idempotencyKey: string,
): Promise<ApiResult<FeedPurchase>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPurchase>("/procurement/feed-purchases", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function recordFeedPurchasePayment(
  purchaseId: string,
  body: FeedPurchasePaymentWrite,
  idempotencyKey: string,
): Promise<ApiResult<FeedPurchase>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPurchase>(`/procurement/feed-purchases/${encodeURIComponent(purchaseId)}/payments` as keyof AppApiPaths & string, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function setFeedPurchasePaymentStatus(
  purchaseId: string,
  body: FeedPurchaseStatusWrite,
): Promise<ApiResult<FeedPurchase>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPurchase>(`/procurement/feed-purchases/${encodeURIComponent(purchaseId)}/payment-status` as keyof AppApiPaths & string, {
      method: "PUT",
      cache: "no-store",
      body,
    }),
  );
}

export async function editFeedPurchase(
  purchaseId: string,
  body: FeedPurchaseEdit,
): Promise<ApiResult<FeedPurchase>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<FeedPurchase>(`/procurement/feed-purchases/${encodeURIComponent(purchaseId)}` as keyof AppApiPaths & string, {
      method: "PUT",
      cache: "no-store",
      body,
    }),
  );
}

export async function recordSalesDealPayment(
  dealId: string,
  body: SalesDealPaymentWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesDeal>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesDeal>(`/sales/deals/${encodeURIComponent(dealId)}/payments` as keyof AppApiPaths & string, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function updateSalesDealPayment(
  dealId: string,
  paymentId: string,
  body: SalesDealPaymentWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesDeal>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesDeal>(
      `/sales/deals/${encodeURIComponent(dealId)}/payments/${encodeURIComponent(paymentId)}` as keyof AppApiPaths & string,
      {
        method: "PUT",
        cache: "no-store",
        headers: idempotentHeaders(idempotencyKey),
        body,
      },
    ),
  );
}

export async function deleteSalesDealPayment(
  dealId: string,
  paymentId: string,
  idempotencyKey: string,
): Promise<ApiResult<SalesDeal>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesDeal>(
      `/sales/deals/${encodeURIComponent(dealId)}/payments/${encodeURIComponent(paymentId)}` as keyof AppApiPaths & string,
      {
        method: "DELETE",
        cache: "no-store",
        headers: idempotentHeaders(idempotencyKey),
      },
    ),
  );
}

export async function setSalesDealStatus(
  dealId: string,
  body: SalesDealStatusWrite,
): Promise<ApiResult<SalesDeal>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesDeal>(`/sales/deals/${encodeURIComponent(dealId)}/status` as keyof AppApiPaths & string, {
      method: "POST",
      cache: "no-store",
      body,
    }),
  );
}

export async function listSalesBuyerLeads(
  // `search` is an infix over the lead's own words AND its phone number; `status` is an exact
  // call_status, with the sentinel "uncontacted" selecting the not-yet-called bucket. Both are
  // resolved by the backend against the WHOLE pipeline, so `total` stays a whole-filter count and
  // the drawer's pager can reach the last lead of 208 rather than the newest twenty.
  params: { limit?: number; offset?: number; search?: string; status?: string } = {},
): Promise<ApiResult<SalesBuyerLeadPage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesBuyerLeadPage>("/sales/buyer-leads", {
      cache: "no-store",
      query: compactQuery({
        limit: params.limit,
        offset: params.offset,
        search: params.search,
        status: params.status,
      }),
    }),
  );
}

export async function createSalesBuyerLead(
  body: SalesBuyerLeadWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesBuyerLead>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesBuyerLead>("/sales/buyer-leads", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function setSalesBuyerLeadStatus(
  leadId: string,
  body: SalesLeadStatusWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesBuyerLead>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/sales/buyer-leads/${encodeURIComponent(leadId)}/status` as keyof AppApiPaths & string;
  return request(() =>
    client.request<SalesBuyerLead>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

/**
 * Replaces every editable field on one buyer lead.
 *
 * A REPLACE, not a patch: the drawer's edit form always submits the whole lead, so a field the
 * person cleared is genuinely cleared rather than silently keeping its old value. The status-only
 * setter above stays as the fast path used while working down a call list.
 */
export async function updateSalesBuyerLead(
  leadId: string,
  body: SalesBuyerLeadWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesBuyerLead>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/sales/buyer-leads/${encodeURIComponent(leadId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<SalesBuyerLead>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function listSalesFpoLeads(
  // Same search/status contract as the buyer pipeline above.
  params: { limit?: number; offset?: number; search?: string; status?: string } = {},
): Promise<ApiResult<SalesFpoLeadPage>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesFpoLeadPage>("/sales/fpo-leads", {
      cache: "no-store",
      query: compactQuery({
        limit: params.limit,
        offset: params.offset,
        search: params.search,
        status: params.status,
      }),
    }),
  );
}

export async function createSalesFpoLead(
  body: SalesFpoLeadWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesFpoLead>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesFpoLead>("/sales/fpo-leads", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function setSalesFpoLeadStatus(
  leadId: string,
  body: SalesLeadStatusWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesFpoLead>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/sales/fpo-leads/${encodeURIComponent(leadId)}/status` as keyof AppApiPaths & string;
  return request(() =>
    client.request<SalesFpoLead>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

/** Replaces every editable field on one farmer-group lead. Same replace semantics as the buyer edit. */
export async function updateSalesFpoLead(
  leadId: string,
  body: SalesFpoLeadWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesFpoLead>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/sales/fpo-leads/${encodeURIComponent(leadId)}` as keyof AppApiPaths & string;
  return request(() =>
    client.request<SalesFpoLead>(path, {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function createSalesBenchmark(
  body: SalesBenchmarkWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesRecorded>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesRecorded>("/sales/market-benchmarks", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function createSalesSoldTags(
  body: SalesSoldTagsWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesSoldTagsResult>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesSoldTagsResult>("/sales/sold-tags", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
  );
}

export async function createSalesWeightCheck(
  body: SalesWeightCheckWrite,
  idempotencyKey: string,
): Promise<ApiResult<SalesRecorded>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    client.request<SalesRecorded>("/sales/weight-checks", {
      method: "POST",
      cache: "no-store",
      headers: idempotentHeaders(idempotencyKey),
      body,
    }),
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
