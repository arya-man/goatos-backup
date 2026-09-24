import type { AppApiComponents, AppApiPaths } from "@goatos/api-client";
import { createAppApiClient } from "@goatos/api-client";
import {
  apiClientOptions,
  compactQuery,
  getServerConfig,
  request,
  withApiTimeout,
  type ApiResult,
} from "@/lib/api/server";

// Field names are fixed by the contract handed to every agent working this feature: never rename
// them locally to "fix" a naming preference — a rename here silently breaks the wire shape.

type AppSchemas = AppApiComponents["schemas"];

export type HerdSignalMovementState = AppSchemas["HerdSignalMovementState"];
export type HerdSignalMappingState = AppSchemas["HerdSignalMappingState"];
export type HerdSignalPatternState = AppSchemas["HerdSignalPatternState"];
export type HerdSignalRiskState = AppSchemas["HerdSignalRiskState"];
export type HerdSignalTone = AppSchemas["HerdSignalTone"];
export type HerdSignalBatteryState = AppSchemas["HerdSignalBatteryState"];
export type HerdSignalBatteryTrend = AppSchemas["HerdSignalBatteryTrend"];
export type HerdSignalSensorState = AppSchemas["HerdSignalSensorState"];
export type HerdSignalsSummary = AppSchemas["HerdSignalsSummary"];

// last_seen_at (and every other rendered timestamp in this file) is sourced from received_at —
// OUR server clock, the only trusted one (docs/modules/herd-signals.md "Time, clocks, and what
// happens during a network outage"). The gateway's own clock (gateway_seen_at on the wire) runs
// +02:30:00 ahead of real IST and is stored uncorrected for diagnostics only. Do not add a field
// here that surfaces gateway_seen_at as a rendered "when" — if it is ever needed on screen it must
// be explicitly labelled as the gateway's own reported clock, never presented as when something
// happened.
export type HerdSignalItem = AppSchemas["HerdSignalItem"];
export type HerdSignalsLiveResponse = AppSchemas["HerdSignalsLiveResponse"];

export interface HerdSignalTimelineBucket {
  bucket_start: string;
  bucket_seconds: number;
  first_motion_count: number | null;
  last_motion_count: number | null;
  motion_delta: number | null;
  packet_count: number;
  avg_rssi_dbm: number | null;
  min_rssi_dbm: number | null;
  max_rssi_dbm: number | null;
  is_gap: boolean;
  // The reconnect bucket after a reception gap: motion_delta here is the TOTAL accumulated across
  // the whole gap (motion_count is cumulative), attributed to this single bucket because we cannot
  // know when inside the gap it happened. Never smear it across the gap's buckets, never treat it
  // as a normal reading for the p75 baseline or the spike comparison (the backend already excludes
  // it from both), and never colour it as a movement spike in the UI.
  gap_delta: boolean;
}

export interface HerdSignalTimelineResponse {
  buckets: HerdSignalTimelineBucket[];
}

export type HerdGatewayStatus = "online" | "offline";
export type HerdGatewayNetworkMode = "wifi" | "ble" | "wifi_ble";

export interface HerdGateway {
  gateway_id: string;
  label: string | null;
  park_name: string | null;
  shed_name: string | null;
  partition_label: string | null;
  operational_location_display: string | null;
  network_mode: HerdGatewayNetworkMode | null;
  wifi_mac: string | null;
  ble_mac: string | null;
  status: HerdGatewayStatus;
  last_seen_at: string | null;
  // Nullable on the wire deliberately: the backend has not wired these three aggregates yet
  // (tracked TODO). A hardcoded 0 would report a false fact ("zero weak tags") that this screen has
  // not actually measured — render "—" for null, never a bare 0 that looks like a real count.
  tags_seen_recently: number | null;
  weak_tags: number | null;
  unmapped_tags: number | null;
  // 15-minute window aggregates: computed over the most recent 15 minutes of packets/tags for this gateway.
  // Bounded by the stated window so operators know these are not lifetime counts. Null means not yet computed.
  tags_seen_in_window: number | null;
  distinct_motion_deltas: number | null;
  packets_received_in_window: number | null;
}

export interface HerdGatewaysResponse {
  gateways: HerdGateway[];
}

export type HerdSignalType = "direct" | "derived" | "correlated" | "inferred";

export interface HerdInsightCard {
  key: string;
  label: string;
  value: string | number | null;
  unit: string | null;
  signal_type: HerdSignalType;
  formula: string;
  caveat: string | null;
}

export interface HerdInsightsResponse {
  cards: HerdInsightCard[];
}

export interface HerdSignalsLiveParams {
  parkId?: string;
  shedId?: string;
  movementState?: HerdSignalMovementState;
  liveState?: "moving_now" | "active_1m";
  mappingState?: HerdSignalMappingState;
  // "not_normal" is a server-side sentinel (backend/internal/herdsignals/adapters/postgres/
  // repository.go herdSignalsLiveFilter), not a literal pattern_state value: it is the whole-fleet
  // alerting partition (pattern_state <> 'normal'). The Alerts tab uses it so the alerting subset
  // is selected by the query, not by filtering whatever rows happen to be on the fetched page.
  pattern?: HerdSignalPatternState | "not_normal";
  riskState?: HerdSignalRiskState | "attention";
  q?: string;
  cursor?: string;
  limit?: number;
  sort?: "smart_tag" | "tag_temp" | "last_seen" | "motion_count" | "delta_15m" | "delta_1h";
  sortDir?: "asc" | "desc";
}

// Live table + summary read. 6s timeout matches the sibling live-tracker read — this endpoint is
// polled on a 5s cadence by default, so a call that takes longer than the next tick is already
// stale by the time it would resolve.
export async function getHerdSignalsLive(params: HerdSignalsLiveParams = {}): Promise<ApiResult<HerdSignalsLiveResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(6000, (signal) =>
      client.request<HerdSignalsLiveResponse>("/herd-signals/live" as keyof AppApiPaths & string, {
        cache: "no-store",
        signal,
        query: compactQuery({
          park_id: params.parkId,
          shed_id: params.shedId,
          movement_state: params.movementState,
          live_state: params.liveState,
          mapping_state: params.mappingState,
          pattern: params.pattern,
          risk_state: params.riskState,
          q: params.q,
          cursor: params.cursor,
          limit: params.limit,
          sort: params.sort,
          dir: params.sortDir,
        }),
      }),
    ),
  );
}

export interface HerdSignalsTimelineParams {
  tagId: string;
  from?: string;
  to?: string;
  bucketSeconds?: number;
}

export async function getHerdSignalsTimeline(params: HerdSignalsTimelineParams): Promise<ApiResult<HerdSignalTimelineResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/herd-signals/tags/${encodeURIComponent(params.tagId)}/timeline` as keyof AppApiPaths & string;
  return request(() =>
    withApiTimeout(8000, (signal) =>
      client.request<HerdSignalTimelineResponse>(path, {
        cache: "no-store",
        signal,
        query: compactQuery({
          from: params.from,
          to: params.to,
          bucket_seconds: params.bucketSeconds,
        }),
      }),
    ),
  );
}

// Gateway health view — refreshes every 15s per docs/modules/herd-signals.md Section 7.
export async function getHerdSignalsGateways(): Promise<ApiResult<HerdGatewaysResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(6000, (signal) =>
      client.request<HerdGatewaysResponse>("/herd-signals/gateways" as keyof AppApiPaths & string, {
        cache: "no-store",
        signal,
      }),
    ),
  );
}

export async function getHerdSignalsInsights(): Promise<ApiResult<HerdInsightsResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(6000, (signal) =>
      client.request<HerdInsightsResponse>("/herd-signals/insights" as keyof AppApiPaths & string, {
        cache: "no-store",
        signal,
      }),
    ),
  );
}

export interface HerdSignalsActivityParams {
  tagId: string;
  from: string;
  to: string;
}

export async function getHerdSignalsActivity(params: HerdSignalsActivityParams): Promise<ApiResult<HerdSignalActivityResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  const path = `/herd-signals/tags/${encodeURIComponent(params.tagId)}/activity` as keyof AppApiPaths & string;
  return request(() =>
    withApiTimeout(8000, (signal) =>
      client.request<HerdSignalActivityResponse>(path, {
        cache: "no-store",
        signal,
        query: compactQuery({
          from: params.from,
          to: params.to,
        }),
      }),
    ),
  );
}

// Import the generated type from the API client for the activity response
// The component should use this type once the generated client includes it
export type HerdSignalActivityResponse = {
  tag_id: string;
  from: string;
  to: string;
  monitoring_since: string | null;
  events: Array<{
    kind: "vaccination" | "feed_given" | "weighing" | "treatment" | "hoof_trimming" | "shed_move";
    at: string;
    label: string;
    grain: "animal" | "shed" | "scanned_identifier";
    motion_delta_before_2h: number | null;
    motion_delta_after_2h: number | null;
    motion_change_percent: number | null;
    before_window_incomplete: boolean;
    after_window_incomplete: boolean;
  }>;
  unavailable_kinds: Array<{
    kind: "vaccination" | "feed_given" | "weighing" | "treatment" | "hoof_trimming" | "shed_move";
    reason: string;
  }>;
  reason: ("tag_not_mapped_to_animal" | "monitoring_boundary_unknown" | "window_entirely_before_monitoring_start") | null;
  truncated: boolean;
  correlation_note: string;
};

// ---------------------------------------------------------------------------
// Tag mapping WRITES.
//
// Three verbs, and only three: MAP (bind a tag to an animal), REPLACE (swap the
// tag on an animal in one operation), UNMAP (release a binding). There is
// deliberately no "mark as smart tag": every row on the Tag Mapping screen is
// already a BLE smart tag — that is why the gateway reported it and why it is
// listed at all — so asking an operator to declare one as such asserts nothing.
// `smart_tag_capable` is an internal consequence of binding, never a user
// action, and the backend exposes no endpoint for it (contracts/openapi/
// app-api.yaml, `unmapHerdSignalTagMapping`).
//
// These are gated by `herd_signals.map`, NOT the read permission: deciding which
// animal a tag belongs to is the decision every animal-attributed number in the
// module depends on, and it stamps the instant that animal's monitoring starts
// (backend/migrations/postgres 000197).
// ---------------------------------------------------------------------------

export type HerdSignalIdentifierType = "smart_ble_tag" | "animal_identifier_1" | "animal_identifier_2" | "temporary_tag";

export interface HerdSignalsBindTagMappingRequest {
  goat_id: string;
  tag_id: string;
  tag_mac?: string;
  identifier_type?: HerdSignalIdentifierType;
}

export interface HerdSignalsReplaceTagMappingRequest {
  goat_id: string;
  new_tag_id: string;
  new_tag_mac?: string;
  identifier_type?: HerdSignalIdentifierType;
}

export interface HerdSignalsUnmapTagMappingRequest {
  tag_id: string;
  tag_mac?: string;
}

export interface HerdSignalsTagMappingResponse {
  goat_id: string;
  tag_id: string;
  tag_mac: string | null;
  identifier_ids: string[];
  mapping_state: "mapped" | "unmapped";
  // The instant animal monitoring STARTS for this tag. Null means unmapped, in which case NO
  // animal-attributed value may be produced for it — not a zero, not a default. Everything the tag
  // emitted before this instant stays device telemetry and can never enter this animal's baseline,
  // pattern window, or correlations.
  monitoring_since: string | null;
  unbound_identifier_ids: string[];
}

// 10s: a mapping write takes row locks inside one transaction and must not be abandoned halfway by
// a timeout tuned for a polled read.
const MAPPING_WRITE_TIMEOUT_MS = 10_000;

async function postMappingWrite<Body>(path: string, body: Body): Promise<ApiResult<HerdSignalsTagMappingResponse>> {
  const config = await getServerConfig(true);
  if (!config.ok) return config;
  const client = createAppApiClient(apiClientOptions(config.data));
  return request(() =>
    withApiTimeout(MAPPING_WRITE_TIMEOUT_MS, (signal) =>
      client.request<HerdSignalsTagMappingResponse>(path as keyof AppApiPaths & string, {
        method: "POST",
        cache: "no-store",
        signal,
        body,
      }),
    ),
  );
}

/** MAP: bind an observed BLE tag to an animal. Starts that animal's monitoring period. */
export function bindHerdSignalTagMapping(body: HerdSignalsBindTagMappingRequest) {
  return postMappingWrite("/herd-signals/tag-mappings", body);
}

/** REPLACE: swap the tag on an animal. ONE operation — never unmap-then-map as two writes. */
export function replaceHerdSignalTagMapping(body: HerdSignalsReplaceTagMappingRequest) {
  return postMappingWrite("/herd-signals/tag-mappings/replace", body);
}

/** UNMAP: release a binding. The tag keeps broadcasting; it simply stops being an animal's tag. */
export function unmapHerdSignalTagMapping(body: HerdSignalsUnmapTagMappingRequest) {
  return postMappingWrite("/herd-signals/tag-mappings/unmap", body);
}
