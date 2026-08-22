// Herd Signals reads. Hand-typed against the fixed contract in docs/modules/herd-signals.md while
// backend/herdsignals lands in parallel — see the note at the top of ./types.ts.
import { apiClientOptions, compactQuery, getServerConfig, request, type ApiResult } from "@/lib/api/server";
import { createAppApiClient } from "@goatos/api-client";
import type {
  HerdSignalsGatewaysResponse,
  HerdSignalsInsightsResponse,
  HerdSignalsLiveParams,
  HerdSignalsLiveResponse,
  HerdSignalsTimelineParams,
  HerdSignalsTimelineResponse,
} from "./types";

async function client() {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  return { ok: true as const, data: createAppApiClient(apiClientOptions(config.data)) };
}

export async function getHerdSignalsLive(params: HerdSignalsLiveParams): Promise<ApiResult<HerdSignalsLiveResponse>> {
  const clientResult = await client();
  if (!clientResult.ok) return clientResult;
  return request(() =>
    clientResult.data.request<HerdSignalsLiveResponse>("/herd-signals/live", {
      cache: "no-store",
      query: compactQuery({
        park_id: params.parkId,
        shed_id: params.shedId,
        movement_state: params.movementState,
        mapping_state: params.mappingState,
        pattern: params.pattern,
        q: params.q,
        cursor: params.cursor,
        limit: params.limit,
      }),
    }),
  );
}

export async function getHerdSignalsTimeline(
  params: HerdSignalsTimelineParams,
): Promise<ApiResult<HerdSignalsTimelineResponse>> {
  const clientResult = await client();
  if (!clientResult.ok) return clientResult;
  return request(() =>
    clientResult.data.request<HerdSignalsTimelineResponse>(`/herd-signals/tags/${encodeURIComponent(params.tagId)}/timeline`, {
      cache: "no-store",
      query: compactQuery({
        from: params.from,
        to: params.to,
        bucket_seconds: params.bucketSeconds,
      }),
    }),
  );
}

export async function getHerdSignalsGateways(): Promise<ApiResult<HerdSignalsGatewaysResponse>> {
  const clientResult = await client();
  if (!clientResult.ok) return clientResult;
  return request(() =>
    clientResult.data.request<HerdSignalsGatewaysResponse>("/herd-signals/gateways", {
      cache: "no-store",
    }),
  );
}

export async function getHerdSignalsInsights(): Promise<ApiResult<HerdSignalsInsightsResponse>> {
  const clientResult = await client();
  if (!clientResult.ok) return clientResult;
  return request(() =>
    clientResult.data.request<HerdSignalsInsightsResponse>("/herd-signals/insights", {
      cache: "no-store",
    }),
  );
}