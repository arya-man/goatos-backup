// Local response types for the Herd Signals module.
//
// These are hand-written rather than generated from contracts/openapi because this feature was
// built in parallel with the backend implementation of the fixed API contract (see
// docs/modules/herd-signals.md). Once backend/contracts publishes the generated
// AppApiComponents["schemas"] entries for these shapes, this file should be replaced by importing
// those types the way lib/api/server.ts does for every other endpoint — do not hand-maintain both.

export type HerdSignalsMovementState = "moving" | "quiet" | "not_moving" | "unknown";
export type HerdSignalsSignalState = "strong" | "weak" | "stale" | "missing";
export type HerdSignalsBatteryState = "ok" | "low" | "critical" | "unknown";
export type HerdSignalsPatternState = "normal" | "elevated" | "suppressed" | "unknown";
export type HerdSignalsSensorState = "ok" | "abnormal" | "unknown";
export type HerdSignalsMappingState = "mapped" | "unmapped" | "conflict";

export type HerdSignalsSummary = {
  tags_seen: number;
  mapped_animals: number;
  unmapped_tags: number;
  moving: number;
  quiet: number;
  not_moving: number;
  stale: number;
  weak_signal: number;
  low_battery: number;
  sensor_abnormal: number;
};

export type HerdSignalsItem = {
  tag_id: string;
  tag_mac: string;
  goat_id: string | null;
  display_id: string | null;
  park_id: string;
  park_name: string;
  shed_id: string | null;
  shed_name: string | null;
  partition_label: string | null;
  operational_location_display: string | null;
  gateway_id: string | null;
  last_seen_at: string | null;
  rssi_dbm: number | null;
  signal_state: HerdSignalsSignalState;
  battery_mv: number | null;
  battery_state: HerdSignalsBatteryState;
  battery_life_estimate: string | null;
  tag_temperature_c: number | null;
  motion_count: number | null;
  motion_delta: number | null;
  motion_delta_1h: number | null;
  motion_window_seconds: number | null;
  movement_state: HerdSignalsMovementState;
  pattern_state: HerdSignalsPatternState;
  baseline_delta: number | null;
  sensor_state: HerdSignalsSensorState;
  temperature_sensor_ok: boolean | null;
  accelerometer_sensor_ok: boolean | null;
  mapping_state: HerdSignalsMappingState;
};

export type HerdSignalsLiveResponse = {
  summary: HerdSignalsSummary;
  items: HerdSignalsItem[];
  next_cursor: string | null;
};

export type HerdSignalsLiveParams = {
  parkId?: string;
  shedId?: string;
  movementState?: string;
  mappingState?: string;
  pattern?: string;
  q?: string;
  cursor?: string;
  limit?: number;
};

export type HerdSignalsTimelineBucket = {
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
};

export type HerdSignalsTimelineResponse = {
  buckets: HerdSignalsTimelineBucket[];
};

export type HerdSignalsTimelineParams = {
  tagId: string;
  from?: string;
  to?: string;
  bucketSeconds?: number;
};

export type HerdSignalsGateway = {
  gateway_id: string;
  label: string;
  park_name: string;
  shed_name: string | null;
  network_mode: string;
  wifi_mac: string | null;
  ble_mac: string | null;
  status: "online" | "offline" | "degraded" | "unknown";
  last_seen_at: string | null;
  tags_seen_recently: number;
  weak_tags: number;
  unmapped_tags: number;
};

export type HerdSignalsGatewaysResponse = {
  items: HerdSignalsGateway[];
};

export type HerdSignalsInsightCard = {
  key: string;
  label: string;
  value: string;
  unit: string | null;
  signal_type: "direct" | "derived" | "correlated" | "inferred";
  formula: string | null;
  caveat: string | null;
};

export type HerdSignalsInsightsResponse = {
  cards: HerdSignalsInsightCard[];
};
