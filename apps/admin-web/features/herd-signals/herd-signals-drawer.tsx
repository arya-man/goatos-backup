"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Maximize2, Radio } from "lucide-react";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Typography from "@mui/material/Typography";
import { Label, type LabelColor } from "@/components/minimal/label";
import { DetailDrawer, DrawerMetaGrid, DrawerMetaItem } from "@/components/app/detail-drawer";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import { Tag } from "@/components/ui-primitives";
import type { HerdSignalItem, HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import { operationalLocationLabel } from "@/lib/operational-location";
import {
  MAPPING_LABEL,
  MOVEMENT_LABEL,
  MOVEMENT_TONE,
  PATTERN_LABEL,
  PATTERN_TONE,
  PATTERN_WHY,
  fmtAgo,
  fmtBatteryMv,
  fmtBleMac,
  fmtDelta,
  fmtRssi,
  fmtTagTemp,
} from "./format";
import { ChartReadout, HistoryChart, historyChartLegend } from "./herd-signals-history-chart";
import { useNowMs } from "./herd-signals-stream-bridge";
import { BlockSkeleton } from "@/components/app/skeletons";

type RangeKey = "1h" | "6h" | "24h";
const RANGE_SECONDS: Record<RangeKey, number> = { "1h": 3600, "6h": 6 * 3600, "24h": 24 * 3600 };
// Every drawer range stays on the 300s (5-minute) tier, exactly as the mock draws it: rolling 24h
// up to hourly leaves 24 fat bars instead of 288 readable ones, and the baseline chip is quoted per
// 5 minutes, so a 3600s bar cannot be compared against it without a unit mismatch.
const RANGE_BUCKET_SECONDS: Record<RangeKey, number> = { "1h": 300, "6h": 300, "24h": 300 };

function drawerBatteryVoltage(mv: number | null | undefined): string {
  if (mv === null || mv === undefined) return "—";
  return `${fmtBatteryMv(mv)} (${mv} mV)`;
}

function drawerGateway(gatewayId: string | null | undefined): string {
  if (!gatewayId) return "—";
  if (/^\d+$/.test(gatewayId)) return `GW-${gatewayId}`;
  return gatewayId;
}

async function readTimeline(tagId: string, range: RangeKey): Promise<{ ok: true; buckets: HerdSignalTimelineBucket[] } | { ok: false; error: string }> {
  const to = new Date();
  const from = new Date(to.getTime() - RANGE_SECONDS[range] * 1000);
  try {
    const response = await fetch(
      `/api/herd-signals/tags/${encodeURIComponent(tagId)}/timeline?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}&bucket_seconds=${RANGE_BUCKET_SECONDS[range]}`,
      { headers: { Accept: "application/json" }, cache: "no-store" },
    );
    const payload = (await response.json().catch(() => ({}))) as { buckets?: HerdSignalTimelineBucket[]; error?: string };
    if (!response.ok) return { ok: false, error: payload.error ?? `timeline_read_${response.status}` };
    return { ok: true, buckets: payload.buckets ?? [] };
  } catch {
    return { ok: false, error: "timeline_unreachable" };
  }
}

export function HerdSignalsDrawer({
  rows,
  rowId,
  initialSelectedId,
  closeHref,
}: {
  rows: HerdSignalItem[];
  rowId: (item: HerdSignalItem) => string;
  initialSelectedId?: string;
  closeHref: string;
}) {
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: rows,
    itemId: rowId,
    selectionKey: "hs_tag",
    initialSelectedId,
    closeHref,
  });
  const [range, setRange] = useState<RangeKey>("6h");
  const nowMs = useNowMs();
  const [hovered, setHovered] = useState<HerdSignalTimelineBucket | null>(null);
  // Bumped by the Retry button so a failed read can be re-fetched without changing tag or range —
  // included in the effect's dependency array below so a retry actually re-runs the fetch, not just
  // clears the error text back to a loading skeleton that never resolves again.
  const [retryToken, setRetryToken] = useState(0);
  // Chart state is keyed to (tag, range, retryToken) and reset by comparing that key DURING RENDER
  // (React's sanctioned alternative to an Effect that resets state — see "You Might Not Need an
  // Effect"). The Effect below only ever calls setState from inside the async .then(), never
  // synchronously at its top, which is what react-hooks/set-state-in-effect requires.
  const chartKey = displayedItem ? `${displayedItem.tag_id}|${range}|${retryToken}` : "";
  const [chart, setChart] = useState<{ key: string; buckets: HerdSignalTimelineBucket[] | null; error: string | null }>({
    key: chartKey,
    buckets: null,
    error: null,
  });
  if (chartKey !== chart.key) {
    setChart({ key: chartKey, buckets: null, error: null });
  }
  const requestId = useRef(0);

  // Depend on the tag ID STRING, never the item OBJECT. The board re-renders every second (the
  // live clock ticks), which hands us a fresh `displayedItem` identity each time; with the object
  // in the dependency array this effect re-fired every second, each run cancelling the previous
  // request through requestId, so the fetch never committed and the chart sat on its loading
  // skeleton forever. Three in-flight timeline requests with different from/to was the tell.
  const displayedTagId = displayedItem?.tag_id ?? null;

  useEffect(() => {
    if (!displayedTagId) return;
    const id = ++requestId.current;
    const key = `${displayedTagId}|${range}|${retryToken}`;
    void readTimeline(displayedTagId, range).then((result) => {
      if (requestId.current !== id) return;
      if (result.ok) setChart((prev) => (prev.key === key ? { ...prev, buckets: result.buckets } : prev));
      else setChart((prev) => (prev.key === key ? { ...prev, error: result.error } : prev));
    });
  }, [displayedTagId, range, retryToken]);

  if (!displayedItem) return null;
  const item = displayedItem;
  const { buckets, error: chartError } = chart;
  const location = item.operational_location_display
    ? item.operational_location_display
    : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
  const expandHref = `${closeHref}${closeHref.includes("?") ? "&" : "?"}hs_history=${encodeURIComponent(item.tag_id)}#hs-history-${encodeURIComponent(item.tag_id)}`;
  const animalLabel = item.animal_identifier_1 || item.animal_identifier_2 || item.display_id;
  const titleLine = animalLabel ? `${animalLabel} · ${item.tag_id}` : `Unmapped tag ${item.tag_id}`;
  const subtitleLine = [fmtBleMac(item.tag_mac), location || null, item.gateway_id || null].filter(Boolean).join(" · ");

  const sensorState = (ok: boolean | null | undefined) => (ok === null || ok === undefined ? "—" : ok ? "OK" : "Abnormal");

  // Template temporary drawer (portal, backdrop, focus trapped and returned). The body is portalled
  // outside `.herd-signals-page`, so it is built from MUI/template parts only, never page-scoped CSS.
  return (
    <DetailDrawer
      open={drawerOpen}
      onClose={closeDrawer}
      title={titleLine}
      subtitle={subtitleLine || "—"}
      icon={<Radio aria-hidden="true" />}
      ariaLabel="Tag detail"
      closeLabel="Close tag detail"
    >
      {/* Control row directly under the header: pattern + baseline chips, range picker, Expand. */}
      <Box sx={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: 1 }}>
        {item.pattern_state ? <Tag tone={PATTERN_TONE[item.pattern_state]}>{PATTERN_LABEL[item.pattern_state]}</Tag> : null}
        {item.baseline_delta !== null && item.baseline_delta !== undefined ? (
          <Tag tone="mut">baseline {item.baseline_delta} / 5 min</Tag>
        ) : null}
        <Box sx={{ flex: 1 }} />
        <ToggleButtonGroup
          size="small"
          exclusive
          value={range}
          onChange={(_, next: RangeKey | null) => {
            if (next) setRange(next);
          }}
          aria-label="History range"
        >
          {(["1h", "6h", "24h"] as RangeKey[]).map((key) => (
            <ToggleButton key={key} value={key}>
              {key}
            </ToggleButton>
          ))}
        </ToggleButtonGroup>
        <Button
          component={LocalOverlayLink}
          href={expandHref}
          scroll={false}
          size="small"
          variant="outlined"
          color="inherit"
          startIcon={<Maximize2 size={16} aria-hidden="true" />}
          title="Full history, custom date range and farm-activity overlay"
        >
          Expand
        </Button>
      </Box>

      {/* Movement-history chart FIRST, above the readings, so it needs no scrolling. */}
      <Card variant="outlined" sx={{ p: 2 }}>
        {chartError ? (
          // A failed read is a distinct state from "this tag has no history" (empty buckets)
          // and must never render as a blank/grey panel indistinguishable from either —
          // docs/modules/herd-signals.md "Required UI states" is explicit about this exact case.
          <Alert
            severity="error"
            action={
              <Button color="inherit" size="small" onClick={() => setRetryToken((current) => current + 1)}>
                Retry
              </Button>
            }
          >
            <AlertTitle>History read failed</AlertTitle>
            {chartError}.
          </Alert>
        ) : buckets === null ? (
          <BlockSkeleton card={false} height={110} />
        ) : (
          <HistoryChart buckets={buckets} baseline={item.baseline_delta} height={110} onHover={setHovered} />
        )}
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1.5, rowGap: 1, pt: 1.5 }}>
          {historyChartLegend().filter((entry) => entry.className !== "b-reconnect").map((entry) => (
            <Typography key={entry.label} variant="caption" sx={{ display: "inline-flex", alignItems: "center", gap: 0.75, color: "text.secondary" }}>
              <Box
                component="span"
                aria-hidden="true"
                sx={
                  entry.dashed
                    ? { width: 10, borderTop: "2px dashed var(--info)" }
                    : { width: 10, aspectRatio: "1", borderRadius: "var(--r-sm)", bgcolor: LEGEND_SWATCH[entry.className] ?? "text.disabled", opacity: LEGEND_OPACITY[entry.className] ?? 1 }
                }
              />
              {entry.label}
            </Typography>
          ))}
        </Box>
        <Typography variant="caption" component="div" sx={{ color: "text.secondary", pt: 1, "& b": { color: "text.primary", fontVariantNumeric: "tabular-nums" }, "&:empty": { display: "none" } }} aria-live="polite">
          <ChartReadout buckets={buckets} hovered={hovered} />
        </Typography>
        {/* Why this pattern was flagged (0192898c9 / 07e72a680). */}
        <Typography variant="caption" component="div" sx={{ color: "text.disabled", pt: 1 }}>
          {item.pattern_state ? `${PATTERN_WHY[item.pattern_state].charAt(0).toUpperCase()}${PATTERN_WHY[item.pattern_state].slice(1)}. ` : ""}
          Activity uses motion-count deltas from historical packets. Quiet periods are normal; alerts use sustained patterns.
        </Typography>
      </Card>

      <DrawerMetaGrid>
        <Reading label="Tag ID" source="direct">{item.tag_id}</Reading>
        <Reading label="BLE MAC" source="direct">{fmtBleMac(item.tag_mac)}</Reading>
        <Reading label="Animal" source="derived">{animalLabel || "Unmapped"}</Reading>
        <Reading label="Location" source="correlated">{location || "—"}</Reading>
        <Reading label="Gateway" source="direct">{drawerGateway(item.gateway_id)}</Reading>
        <Reading label="RSSI" source="direct">{fmtRssi(item.rssi_dbm)}</Reading>
        <Reading label="Battery voltage" source="direct">{drawerBatteryVoltage(item.battery_mv)}</Reading>
        <Reading label="Tag temp" source="direct">{fmtTagTemp(item.tag_temperature_c)}</Reading>
        <Reading label="Motion count" source="direct">{fmtDelta(item.motion_count)}</Reading>
        <Reading
          label="15m motion delta"
          source="derived"
          title={item.gap_delta ? "Accumulated across a reception gap — timing within the gap is unknown, not a normal 15m reading" : undefined}
        >
          {fmtDelta(item.motion_delta)}
          {item.gap_delta ? <sup title="Gap total">*</sup> : null}
        </Reading>
        <Reading label="24h motion delta" source="derived" title="Rolling 24-hour motion-counter delta. Movement units, not steps.">
          {fmtDelta(item.motion_delta_24h)}
        </Reading>
        <Reading label="Movement state" source="inferred">
          {item.movement_state ? <Tag tone={MOVEMENT_TONE[item.movement_state]}>{MOVEMENT_LABEL[item.movement_state]}</Tag> : "—"}
        </Reading>
        <Reading label="Last seen" source="direct">{fmtAgo(item.last_seen_at, nowMs)}</Reading>
        <Reading label="Temp sensor" source="direct">{sensorState(item.temperature_sensor_ok)}</Reading>
        <Reading label="Accelerometer" source="direct">{sensorState(item.accelerometer_sensor_ok)}</Reading>
        <Reading label="Mapping state" source="derived">
          {item.mapping_state === "conflict" ? "mapping conflict" : MAPPING_LABEL[item.mapping_state]}
        </Reading>
      </DrawerMetaGrid>
    </DetailDrawer>
  );
}

// Legend swatches carry the same palette tokens the history chart bars draw with.
const LEGEND_SWATCH: Record<string, string> = {
  "b-move": "var(--ok)",
  "b-low": "var(--muted)",
  "b-zero": "var(--line)",
  "b-spike": "var(--warn)",
  gap: "var(--danger)",
};
const LEGEND_OPACITY: Record<string, number> = { "b-low": 0.55, gap: 0.35 };

// Where a reading comes from: read straight off the packet, derived, inferred, or correlated.
type ReadingSource = "direct" | "derived" | "inferred" | "correlated";
const SOURCE_LABEL: Record<ReadingSource, { text: string; color: LabelColor }> = {
  direct: { text: "Direct", color: "success" },
  derived: { text: "Derived", color: "info" },
  inferred: { text: "Inferred", color: "secondary" },
  correlated: { text: "Correlated", color: "warning" },
};

function Reading({ label, source, title, children }: { label: string; source: ReadingSource; title?: string; children: ReactNode }) {
  const src = SOURCE_LABEL[source];
  return (
    <DrawerMetaItem label={label}>
      <Box component="span" title={title} sx={{ display: "inline-flex", alignItems: "center", flexWrap: "wrap", gap: 0.75 }}>
        {children}
        <Label variant="soft" color={src.color}>{src.text}</Label>
      </Box>
    </DrawerMetaItem>
  );
}