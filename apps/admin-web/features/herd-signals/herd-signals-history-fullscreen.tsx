"use client";
import dayjs from "dayjs";
import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardContent from "@mui/material/CardContent";
import CardHeader from "@mui/material/CardHeader";
import Chip from "@mui/material/Chip";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Table from "@mui/material/Table";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import ToggleButton from "@mui/material/ToggleButton";
import ToggleButtonGroup from "@mui/material/ToggleButtonGroup";
import Typography from "@mui/material/Typography";
import useMediaQuery from "@mui/material/useMediaQuery";
import { DateTimePicker } from "@mui/x-date-pickers/DateTimePicker";

import { Tag } from "@/components/ui-primitives";
import { Label, type LabelColor } from "@/components/minimal/label";
import { Iconify } from "@/components/minimal/iconify";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { listOrEmpty } from "@/lib/list-or-empty";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { useTheme, type Theme } from "@mui/material/styles";
import Dialog from "@mui/material/Dialog";
import { EmptyState } from "@/components/app/empty-state";
import { useLocalOverlaySelection } from "@/components/local-overlay-link";
import type { HerdSignalItem, HerdSignalTimelineBucket } from "@/lib/api/herd-signals";
import type { HerdSignalActivityResponse } from "@/lib/api/herd-signals";
import { operationalLocationLabel } from "@/lib/operational-location";
import {
  fmtBleMac,
  fmtBatteryMv,
  fmtDelta1h,
  fmtRssi,
  fmtTagTemp,
  MOVEMENT_LABEL,
  PATTERN_LABEL,
  MAPPING_LABEL,
} from "./format";
import { ChartReadout, HistoryChart, HistoryChartLegend } from "./herd-signals-history-chart";
import { HS_FAINT, HS_MONO, respTableSx } from "./herd-signals-sx";
import { BlockSkeleton } from "@/components/app/skeletons";

type RangeKey = "1h" | "6h" | "12h" | "24h" | "3d" | "7d" | "30d" | "custom";
const RANGE_LABEL: Record<RangeKey, string> = {
  "1h": "1h",
  "6h": "6h",
  "12h": "12h",
  "24h": "24h",
  "3d": "3d",
  "7d": "7d",
  "30d": "30d",
  custom: "Custom",
};
const RANGE_SECONDS: Partial<Record<RangeKey, number>> = {
  "1h": 3600,
  "6h": 6 * 3600,
  "12h": 12 * 3600,
  "24h": 24 * 3600,
  "3d": 3 * 24 * 3600,
  "7d": 7 * 24 * 3600,
  "30d": 30 * 24 * 3600,
};
// Bucket tier by range. ONLY the tiers activity windows are actually stored at may be requested:
// domain.SupportedBucketSeconds is {60, 300, 3600} and the backend rejects anything else. The mock's
// roll-up thresholds implied a 6-hourly (21600s) tier for 7d/30d, and asking for it returned
// 400 "unsupported bucket_seconds 21600" -- so the 7d and 30d ranges were simply broken, and the
// full-screen chart, its buckets-with-packets count and its signal-gap count all failed together.
//
// 30 days on the hourly tier is 720 bars, comfortably inside MaxTimelineBuckets (2000), so hourly is
// the correct answer rather than a compromise. Up to 24h stays on the 300s tier (288 readable bars);
// rolling that up to hourly would leave 24 fat bars and break the per-5-minute baseline comparison.
function bucketSecondsFor(range: RangeKey, seconds: number): number {
  if (range === "custom") return seconds <= 24 * 3600 ? 300 : 3600;
  if (range === "3d" || range === "7d" || range === "30d") return 3600;
  return 300;
}
const BUCKET_LABEL: Record<number, string> = { 60: "1-minute", 300: "5-minute", 3600: "hourly" };

// The six farm-activity overlays. GET /herd-signals/tags/{id}/activity is the data source (see
// readTimeline's sibling fetch below); each toggle here filters the correlation table AND the
// chart markers by event.kind, using the same colour for a kind's chip, its markers, and its
// legend text everywhere so the three never drift out of sync. Hues come from the theme palette
// (scheme-aware CSS variables), not literal hexes, so they follow the light/dark themes.
type Palette = Theme["vars"]["palette"];
const KIND_META: Record<HerdSignalActivityResponse["events"][number]["kind"], { label: string; color: (p: Palette) => string }> = {
  vaccination: { label: "Vaccination", color: (p) => p.secondary.main },
  feed_given: { label: "Feed given", color: (p) => p.warning.main },
  weighing: { label: "Weighing", color: (p) => p.info.main },
  treatment: { label: "Treatment", color: (p) => p.error.main },
  hoof_trimming: { label: "Hoof trimming", color: (p) => p.primary.darker },
  shed_move: { label: "Pen move", color: (p) => p.grey[500] },
};
const OVERLAY_KINDS = Object.keys(KIND_META) as (keyof typeof KIND_META)[];

// Custom range values are local wall-clock minutes (what the picker shows), parsed by `new Date()`.
const LOCAL_MINUTE = "YYYY-MM-DDTHH:mm";

// Where a reading comes from: read straight off the packet, derived, inferred, or correlated (the
// drawer's SOURCE_LABEL, same template Label colours).
type ReadingSource = "direct" | "derived" | "inferred" | "correlated";
const SOURCE_LABEL: Record<ReadingSource, { text: string; color: LabelColor }> = {
  direct: { text: "Direct", color: "success" },
  derived: { text: "Derived", color: "info" },
  inferred: { text: "Inferred", color: "secondary" },
  correlated: { text: "Correlated", color: "warning" },
};

function DetailRow({ label, source, mono, title, children }: { label: string; source: ReadingSource; mono?: boolean; title?: string; children: ReactNode }) {
  const src = SOURCE_LABEL[source];
  return (
    <>
      <Box component="dt" sx={{ color: "text.secondary", whiteSpace: { sm: "nowrap" } }}>
        {label}
      </Box>
      <Box
        component="dd"
        title={title}
        sx={{ m: 0, fontWeight: "fontWeightSemiBold", textAlign: { xs: "left", sm: "right" }, overflowWrap: "anywhere", ...(mono ? HS_MONO : {}) }}
      >
        {children}
        <Label variant="soft" color={src.color} sx={{ ml: 0.75, verticalAlign: "middle" }}>
          {src.text}
        </Label>
      </Box>
    </>
  );
}

const CHANGE_COLOR = { ok: "success.main", warn: "warning.main", mut: "text.secondary" } as const;

async function readTimeline(
  tagId: string,
  from: string,
  to: string,
  bucketSeconds: number,
): Promise<{ ok: true; buckets: HerdSignalTimelineBucket[] } | { ok: false; error: string }> {
  try {
    const response = await fetch(
      `/api/herd-signals/tags/${encodeURIComponent(tagId)}/timeline?from=${encodeURIComponent(from)}&to=${encodeURIComponent(to)}&bucket_seconds=${bucketSeconds}`,
      { headers: { Accept: "application/json" }, cache: "no-store" },
    );
    const payload = (await response.json().catch(() => ({}))) as { buckets?: HerdSignalTimelineBucket[]; error?: string };
    if (!response.ok) return { ok: false, error: payload.error ?? `timeline_read_${response.status}` };
    return { ok: true, buckets: payload.buckets ?? [] };
  } catch {
    return { ok: false, error: "timeline_unreachable" };
  }
}

function rowId(item: HerdSignalItem): string {
  return item.tag_id;
}

export function HerdSignalsHistoryFullscreen({ rows, closeHref }: { rows: HerdSignalItem[]; closeHref: string }) {
  const { displayedItem, drawerOpen, closeDrawer } = useLocalOverlaySelection({
    items: rows,
    itemId: rowId,
    selectionKey: "hs_history",
    closeHref,
  });
  const theme = useTheme();
  const palette = theme.vars.palette;
  const phone = useMediaQuery(theme.breakpoints.down("sm"));
  const [range, setRange] = useState<RangeKey>("24h");
  const [customFrom, setCustomFrom] = useState("");
  const [customTo, setCustomTo] = useState("");
  const [hovered, setHovered] = useState<HerdSignalTimelineBucket | null>(null);
  const [overlaysOn, setOverlaysOn] = useState<Record<string, boolean>>(() =>
    Object.fromEntries(OVERLAY_KINDS.map((kind) => [kind, true])),
  );
  const bounds = useMemo(() => {
    if (range === "custom") {
      if (!customFrom || !customTo) return null;
      const fromDate = new Date(customFrom);
      const toDate = new Date(customTo);
      return {
        from: fromDate.toISOString(),
        to: toDate.toISOString(),
        seconds: Math.max(300, Math.round((toDate.getTime() - fromDate.getTime()) / 1000)),
      };
    }
    const seconds = RANGE_SECONDS[range] ?? 3600;
    const to = new Date();
    const from = new Date(to.getTime() - seconds * 1000);
    return { from: from.toISOString(), to: to.toISOString(), seconds };
  }, [range, customFrom, customTo]);

  // Reset-on-key-change happens DURING RENDER (React's sanctioned alternative to an Effect that
  // resets state), not as a synchronous setState at the top of the Effect body — the Effect below
  // only calls setState from inside the async .then().
  const chartKey = displayedItem && bounds ? `${displayedItem.tag_id}|${bounds.from}|${bounds.to}|${range}` : "";
  const [chart, setChart] = useState<{ key: string; buckets: HerdSignalTimelineBucket[] | null; error: string | null }>({
    key: chartKey,
    buckets: null,
    error: null,
  });
  if (chartKey !== chart.key) {
    setChart({ key: chartKey, buckets: null, error: null });
  }

  // Activity state follows the same reset-on-key-change pattern
  const activityKey = displayedItem && bounds ? `${displayedItem.tag_id}|${bounds.from}|${bounds.to}` : "";
  const [activity, setActivity] = useState<{ key: string; data: HerdSignalActivityResponse | null; error: string | null }>({
    key: activityKey,
    data: null,
    error: null,
  });
  if (activityKey !== activity.key) {
    setActivity({ key: activityKey, data: null, error: null });
  }

  useEffect(() => {
    if (!displayedItem || !bounds) return;
    let active = true;
    const key = `${displayedItem.tag_id}|${bounds.from}|${bounds.to}|${range}`;
    void readTimeline(displayedItem.tag_id, bounds.from, bounds.to, bucketSecondsFor(range, bounds.seconds)).then((result) => {
      if (!active) return;
      if (result.ok) setChart((prev) => (prev.key === key ? { ...prev, buckets: result.buckets } : prev));
      else setChart((prev) => (prev.key === key ? { ...prev, error: result.error } : prev));
    });
    return () => {
      active = false;
    };
  }, [displayedItem, bounds, range]);

  // Fetch activity data for overlay
  useEffect(() => {
    if (!displayedItem || !bounds) return;
    let active = true;
    const key = `${displayedItem.tag_id}|${bounds.from}|${bounds.to}`;
    // Fetched through the same-origin proxy, NOT by importing the server reader: that reader is
    // server-only (it mints the bearer token and reads next/headers), and importing it from this
    // client component pulled "server-only" into the browser bundle and broke the build for the
    // WHOLE app. The drawer's timeline uses the same proxy pattern.
    void fetch(
      `/api/herd-signals/tags/${encodeURIComponent(displayedItem.tag_id)}/activity?from=${encodeURIComponent(bounds.from)}&to=${encodeURIComponent(bounds.to)}`,
      { cache: "no-store" },
    )
      .then(async (response) => {
        if (!active) return;
        if (!response.ok) {
          setActivity({ key, data: null, error: `Failed to load activity (${response.status})` });
          return;
        }
        const data = (await response.json()) as HerdSignalActivityResponse;
        setActivity({ key, data, error: null });
      })
      .catch(() => {
        if (active) setActivity({ key, data: null, error: "Failed to load activity" });
      });
    return () => {
      active = false;
    };
  }, [displayedItem, bounds]);
  const { buckets, error } = chart;

  // The chips filter, not just style: events outside the toggled-on kinds never reach the
  // correlation table or the chart markers below. Both consume this SAME filtered list so a
  // toggle can never leave the table and the chart disagreeing about what is "shown".
  const filteredEvents = useMemo(
    () => (activity.data ? listOrEmpty(activity.data.events).filter((event) => overlaysOn[event.kind]) : []),
    [activity.data, overlaysOn],
  );
  const chartMarkers = useMemo(
    () =>
      filteredEvents.map((event) => ({
        atMs: new Date(event.at).getTime(),
        color: KIND_META[event.kind].color(palette),
        label: `${KIND_META[event.kind].label} · ${event.label}`,
      })),
    [filteredEvents, palette],
  );

  // Prefill custom date inputs when a preset is active (not custom range)
  const prefillCustomInputsWhenPresetActive = useMemo(() => {
    if (range !== "custom" && bounds) {
      const from = new Date(bounds.from);
      const to = new Date(bounds.to);
      // Local wall-clock yyyy-MM-ddTHH:mm, the same shape a picked custom value is stored in.
      const fromIso = dayjs(from).format(LOCAL_MINUTE);
      const toIso = dayjs(to).format(LOCAL_MINUTE);
      return { fromIso, toIso };
    }
    return null;
  }, [range, bounds]);

  if (!displayedItem) return null;
  const item = displayedItem;
  const location = item.operational_location_display
    ? item.operational_location_display
    : operationalLocationLabel({ shedName: item.shed_name, partitionLabel: item.partition_label });
  const bucketSeconds = bucketSecondsFor(range, bounds?.seconds ?? 3600);
  const rangeSummary =
    range === "custom"
      ? bounds
        ? `${new Date(bounds.from).toLocaleString("en-IN", { timeZone: "Asia/Kolkata" })} → ${new Date(bounds.to).toLocaleString("en-IN", { timeZone: "Asia/Kolkata" })}`
        : "Pick a from and a to"
      : `last ${RANGE_LABEL[range]}`;
  const titleLine = `${item.display_id ? `${item.display_id} · ` : "Unmapped tag "}${item.tag_id} — movement history`;
  const subtitleLine = [fmtBleMac(item.tag_mac), location || null, item.gateway_id || null].filter(Boolean).join(" · ");

  // MUI Dialog fullScreen: portals to <body> (a transformed ancestor or the page's stacking context
  // can no longer clip it or paint the Ask Mesha FAB over it), traps focus and restores it. Escape
  // stays with useLocalOverlaySelection (it owns the URL/history step), so the Dialog ignores
  // its own Escape reason -- two handlers would step history back twice. The page-scope wrapper keeps the
  // existing .herd-signals-page .fs styles applying inside the portal.
  const detailRows: { label: string; source: ReadingSource; value: ReactNode; mono?: boolean; title?: string }[] = [
    { label: "Animal", source: "derived", value: item.display_id || <Box component="span" sx={HS_FAINT}>Unmapped</Box> },
    { label: "Tag ID", source: "direct", value: item.tag_id, mono: true },
    { label: "BLE MAC", source: "direct", value: fmtBleMac(item.tag_mac), mono: true },
    { label: "Park / pen", source: "correlated", value: item.park_name && item.shed_name ? `${item.park_name} · ${item.shed_name}` : "—" },
    { label: "Gateway", source: "direct", value: item.gateway_id || "—", mono: true },
    { label: "Motion count", source: "direct", value: item.motion_count !== null ? item.motion_count.toLocaleString("en-IN") : "—" },
    { label: "15m delta", source: "derived", value: item.motion_delta !== null ? "+" + item.motion_delta.toLocaleString("en-IN") : "—" },
    {
      label: "1h delta",
      source: "derived",
      value:
        fmtDelta1h(item.motion_delta_1h, item.motion_delta) && fmtDelta1h(item.motion_delta_1h, item.motion_delta) !== "—"
          ? "+" + fmtDelta1h(item.motion_delta_1h, item.motion_delta)
          : "—",
    },
    {
      label: "24h delta",
      source: "derived",
      title: "Rolling 24-hour motion-counter delta. Movement units, not steps.",
      value: item.motion_delta_24h !== null ? "+" + item.motion_delta_24h.toLocaleString("en-IN") : "—",
    },
    { label: "Movement state", source: "inferred", value: item.movement_state ? MOVEMENT_LABEL[item.movement_state] : "—" },
    { label: "Pattern", source: "inferred", value: item.pattern_state ? PATTERN_LABEL[item.pattern_state] : "—" },
    { label: "RSSI", source: "direct", value: fmtRssi(item.rssi_dbm) },
    { label: "Battery voltage", source: "direct", value: fmtBatteryMv(item.battery_mv) },
    { label: "Tag temp", source: "direct", value: fmtTagTemp(item.tag_temperature_c) },
    {
      label: "Buckets with packets",
      source: "derived",
      value:
        buckets === null
          ? "—"
          : `${buckets.filter((bucket) => !bucket.is_gap).length.toLocaleString("en-IN")} of ${buckets.length.toLocaleString("en-IN")}`,
    },
    { label: "Signal-gap buckets", source: "derived", value: buckets === null ? "—" : buckets.filter((bucket) => bucket.is_gap).length.toLocaleString("en-IN") },
    {
      label: "Sensors",
      source: "direct",
      value:
        item.temperature_sensor_ok && item.accelerometer_sensor_ok
          ? "temp OK · accel OK"
          : item.temperature_sensor_ok
            ? "temp OK · accel abnormal"
            : item.accelerometer_sensor_ok
              ? "temp abnormal · accel OK"
              : "temp abnormal · accel abnormal",
    },
    { label: "Mapping", source: "derived", value: item.mapping_state ? MAPPING_LABEL[item.mapping_state] : "—" },
  ];

  const fromValue = customFrom || prefillCustomInputsWhenPresetActive?.fromIso || "";
  const toValue = customTo || prefillCustomInputsWhenPresetActive?.toIso || "";
  const pickCustom = (set: (value: string) => void) => (value: dayjs.Dayjs | null) => {
    // A half-typed value is invalid until complete; only a whole date and time moves the range.
    if (!value || !value.isValid()) return;
    set(value.format(LOCAL_MINUTE));
    setRange("custom");
  };
  const pickerSlotProps = (label: string) => ({
    textField: { size: "small" as const, inputProps: { "aria-label": label }, sx: { width: { xs: 1, sm: 220 } } },
  });
  const overlaysShown = OVERLAY_KINDS.filter((kind) => overlaysOn[kind] ?? true);
  const allEvents = activity.data ? listOrEmpty(activity.data.events) : [];

  // MUI Dialog fullScreen: portals to <body> (a transformed ancestor or the page's stacking context
  // can no longer clip it or paint the Ask Mesha FAB over it), traps focus and restores it. Escape
  // stays with useLocalOverlaySelection (it owns the URL/history step), so the Dialog ignores
  // its own Escape reason -- two handlers would step history back twice. The body is template/MUI
  // parts on sx only; nothing depends on a page-scoped stylesheet.
  return (
    <Dialog
      fullScreen
      open={drawerOpen}
      onClose={(_event, reason) => {
        if (reason !== "escapeKeyDown") closeDrawer();
      }}
      slotProps={{ paper: { "aria-label": "Full movement history" } }}
    >
      <Box sx={{ height: 1, bgcolor: "background.default", display: "flex", flexDirection: "column" }}>
        <Box
          sx={{
            display: "flex",
            alignItems: "center",
            flexWrap: "wrap",
            gap: 1.5,
            px: { xs: 1.5, sm: 2.5 },
            py: 1.5,
            flex: "none",
            bgcolor: "background.paper",
            borderBottom: 1,
            borderColor: "divider",
          }}
        >
          <Iconify icon="eva:activity-fill" width={24} sx={{ color: "primary.main", flexShrink: 0 }} />
          <Box sx={{ minWidth: 0, flex: "1 1 240px" }}>
            <Typography variant="subtitle1" component="h2">
              {titleLine}
            </Typography>
            <Typography variant="caption" component="div" sx={{ ...HS_MONO, color: "text.secondary" }}>
              {subtitleLine || "—"}
            </Typography>
          </Box>
          <Button
            variant="outlined"
            color="inherit"
            onClick={closeDrawer}
            startIcon={<Iconify icon="mingcute:close-line" />}
            sx={{ width: { xs: 1, sm: "auto" } }}
          >
            Close
          </Button>
        </Box>

        <Stack spacing={2} sx={{ flex: 1, minHeight: 0, overflow: "auto", overflowX: { xs: "hidden", sm: "auto" }, px: { xs: 1.5, sm: 2.5 }, pt: 2, pb: 5, "& > *": { flexShrink: 0 } }}>
          <Card sx={{ p: { xs: 1.5, sm: 2 } }}>
            <Stack direction={{ xs: "column", md: "row" }} spacing={1.5} sx={{ alignItems: { xs: "stretch", md: "center" }, flexWrap: "wrap", rowGap: 1.5 }}>
              <Typography variant="overline" sx={{ color: "text.secondary" }}>
                Range
              </Typography>
              <Box sx={{ overflowX: "auto", maxWidth: 1 }}>
                <ToggleButtonGroup
                  size="small"
                  exclusive
                  value={range === "custom" ? null : range}
                  onChange={(_, next: RangeKey | null) => {
                    if (next) setRange(next);
                  }}
                  aria-label="History range"
                >
                  {(Object.keys(RANGE_LABEL) as RangeKey[])
                    .filter((key) => key !== "custom")
                    .map((key) => (
                      <ToggleButton key={key} value={key}>
                        {RANGE_LABEL[key]}
                      </ToggleButton>
                    ))}
                </ToggleButtonGroup>
              </Box>
              <Typography variant="body2" sx={{ color: "text.secondary" }}>
                or
              </Typography>
              <DateTimePicker
                label="From"
                ampm={false}
                format="DD/MM/YYYY HH:mm"
                value={fromValue ? dayjs(fromValue) : null}
                onChange={pickCustom(setCustomFrom)}
                slotProps={pickerSlotProps("From")}
              />
              <Typography variant="body2" sx={{ color: "text.secondary" }}>
                to
              </Typography>
              <DateTimePicker
                label="To"
                ampm={false}
                format="DD/MM/YYYY HH:mm"
                value={toValue ? dayjs(toValue) : null}
                onChange={pickCustom(setCustomTo)}
                slotProps={pickerSlotProps("To")}
              />
              <Box sx={{ flex: 1, display: { xs: "none", md: "block" } }} />
              <Typography variant="caption" sx={{ color: "text.secondary" }}>
                {rangeSummary} · Asia/Kolkata
              </Typography>
            </Stack>
          </Card>

          <Card sx={{ p: { xs: 1.5, sm: 2 } }}>
            <Stack direction={{ xs: "column", md: "row" }} spacing={1} sx={{ alignItems: { xs: "stretch", md: "center" }, flexWrap: "wrap", rowGap: 1 }}>
              <Typography variant="overline" sx={{ color: "text.secondary" }}>
                Overlay activity
              </Typography>
              <Box sx={{ display: "flex", flexWrap: "wrap", gap: 1 }}>
              {OVERLAY_KINDS.map((kind) => {
                const meta = KIND_META[kind];
                const on = overlaysOn[kind] ?? true;
                const color = meta.color(palette);
                return (
                  <Chip
                    key={kind}
                    size="small"
                    variant="outlined"
                    clickable
                    aria-pressed={on}
                    label={meta.label}
                    icon={<Box component="span" aria-hidden="true" sx={{ width: 8, height: 8, borderRadius: 0.75, bgcolor: "currentColor", flexShrink: 0 }} />}
                    disabled={activity.data === null && activity.error !== null}
                    title={activity.error ? `Failed to load activity: ${activity.error}` : undefined}
                    onClick={() => setOverlaysOn((current) => ({ ...current, [kind]: !(current[kind] ?? true) }))}
                    sx={{
                      fontWeight: "fontWeightBold",
                      color: on ? color : "text.secondary",
                      borderColor: on ? color : "divider",
                      "& .MuiChip-icon": { color: "inherit", ml: 1 },
                    }}
                  />
                );
              })}
              </Box>
              <Box sx={{ flex: 1, display: { xs: "none", md: "block" } }} />
              <Typography variant="caption" sx={{ color: "text.secondary" }}>
                Overlays are joins onto existing Mesha records — they are context, not cause.
              </Typography>
            </Stack>
          </Card>

          <Card>
            <CardHeader
              title="Movement trend"
              subheader={`${BUCKET_LABEL[bucketSeconds] ?? `${bucketSeconds / 60}-minute`} buckets`}
              action={
                <Stack direction="row" spacing={1} sx={{ flexWrap: "wrap", rowGap: 1, justifyContent: "flex-end" }}>
                  <Tag tone="mut">{item.pattern_state ? PATTERN_LABEL[item.pattern_state] : "—"}</Tag>
                  <Tag tone="mut">
                    baseline {item.baseline_delta !== null ? item.baseline_delta.toLocaleString("en-IN") : "—"} / 5 min
                  </Tag>
                </Stack>
              }
              slotProps={{ action: { sx: { alignSelf: "center" } } }}
            />
            {error ? (
              <Box sx={{ px: { xs: 1.5, sm: 2.5 }, pt: 2 }}>
                <Alert severity="error">
                  <AlertTitle>History read failed</AlertTitle>
                  {error}. Try selecting the range again.
                </Alert>
              </Box>
            ) : buckets === null ? (
              <Box sx={{ px: { xs: 1.5, sm: 2.5 }, pt: 2 }}>
                <BlockSkeleton card={false} height={phone ? 170 : 240} />
              </Box>
            ) : (
              <Box sx={{ px: 1.5, pt: 1.5 }}>
                <HistoryChart buckets={buckets} baseline={item.baseline_delta} height={phone ? 170 : 240} onHover={setHovered} markers={chartMarkers} />
              </Box>
            )}
            <Box sx={{ px: { xs: 1.5, sm: 2.5 } }}>
              <HistoryChartLegend
                footer={overlaysShown.length > 0 ? `Showing: ${overlaysShown.map((kind) => KIND_META[kind].label).join(" · ")}` : "No overlay categories selected — all markers hidden"}
              />
            </Box>
            <Typography
              variant="caption"
              component="div"
              aria-live="polite"
              sx={{ px: { xs: 1.5, sm: 2.5 }, pt: 1, color: "text.secondary", "& b": { color: "text.primary", fontVariantNumeric: "tabular-nums" }, "&:empty": { display: "none" } }}
            >
              <ChartReadout buckets={buckets} hovered={hovered} />
            </Typography>
            <Typography variant="caption" component="p" sx={{ px: { xs: 1.5, sm: 2.5 }, pt: 1, pb: 2, m: 0, color: "text.secondary" }}>
              Activity uses motion-count deltas from historical packets. Quiet periods are normal; alerts use sustained
              patterns. Overlaid markers are other recorded farm activity for the same animal or its pen — read them as
              correlation, never as behaviour, cause, or a clinical finding.
            </Typography>
          </Card>

          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <Card sx={{ height: 1 }}>
                <CardHeader
                  avatar={<Iconify icon="solar:danger-triangle-bold" sx={{ color: "warning.main" }} />}
                  title="Activity around recorded farm activity"
                  action={<Tag tone="mut">Correlated</Tag>}
                  slotProps={{ action: { sx: { alignSelf: "center" } } }}
                />
                <Scrollbar>
                  <Table size="small" sx={respTableSx()}>
                    <TableHead>
                      <TableRow>
                        <TableCell component="th">Activity</TableCell>
                        <TableCell component="th">When (IST)</TableCell>
                        <TableCell component="th" data-num>2h before</TableCell>
                        <TableCell component="th" data-num>2h after</TableCell>
                        <TableCell component="th" data-num>Change</TableCell>
                        <TableCell component="th">Read</TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {activity.error && activity.key ? (
                        <TableRow>
                          <TableCell colSpan={6}>
                            <Alert severity="error">
                              <AlertTitle>Activity read failed</AlertTitle>
                              {activity.error}
                            </Alert>
                          </TableCell>
                        </TableRow>
                      ) : activity.data === null ? (
                        <TableRow>
                          <TableCell colSpan={6}>
                            <BlockSkeleton card={false} height={40} />
                          </TableCell>
                        </TableRow>
                      ) : allEvents.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={6}>
                            <EmptyState
                              title={activity.data.reason ? (
                                <>
                                  No activity {activity.data.reason === "tag_not_mapped_to_animal" && "— tag is not mapped to an animal"}
                                  {activity.data.reason === "monitoring_boundary_unknown" && "— mapping start time unknown"}
                                  {activity.data.reason === "window_entirely_before_monitoring_start" && "— window before tag mapping"}
                                </>
                              ) : (
                                "No recorded farm activity in this window"
                              )}
                              description={!activity.data.reason ? "No vaccination, feed, weighing, treatment, hoof trimming, or pen move records found." : undefined}
                            />
                          </TableCell>
                        </TableRow>
                      ) : filteredEvents.length === 0 ? (
                        <TableRow>
                          <TableCell colSpan={6}>
                            <EmptyState
                              title="All recorded activity is hidden"
                              description={<>{allEvents.length.toLocaleString("en-IN")} event
                                {allEvents.length === 1 ? "" : "s"} found in this window, but every overlay
                                category above is turned off. Turn one on to show it.</>}
                            />
                          </TableCell>
                        </TableRow>
                      ) : (
                        filteredEvents.map((event, idx) => {
                          const eventTime = new Date(event.at);
                          const isoTime = eventTime.toLocaleString("en-IN", { timeZone: "Asia/Kolkata", hour12: false });

                          // Format before/after deltas and change percent
                          const formatDelta = (delta: number | null, incomplete: boolean): string => {
                            if (delta === null) return "—";
                            if (incomplete) return `±${Math.abs(delta).toLocaleString("en-IN")}*`;
                            return `+${delta.toLocaleString("en-IN")}`;
                          };

                          const formatChange = (pct: number | null, incBefore: boolean, incAfter: boolean): string => {
                            if (pct === null || incBefore || incAfter) return "—";
                            return `${pct >= 0 ? "+" : ""}${pct.toLocaleString("en-IN")}%`;
                          };

                          const changePercent = formatChange(event.motion_change_percent, event.before_window_incomplete, event.after_window_incomplete);
                          const beforeDelta = formatDelta(event.motion_delta_before_2h, event.before_window_incomplete);
                          const afterDelta = formatDelta(event.motion_delta_after_2h, event.after_window_incomplete);

                          // Tone from the change percent, for visual feedback
                          let tone: keyof typeof CHANGE_COLOR = "mut";
                          if (event.motion_change_percent !== null && !event.before_window_incomplete && !event.after_window_incomplete) {
                            if (event.motion_change_percent >= 40) tone = "ok";
                            else if (event.motion_change_percent <= -40) tone = "warn";
                          }

                          const grainLabel = event.grain === "animal" ? "" : event.grain === "shed" ? " (pen)" : " (scanned)";
                          return (
                            <TableRow key={`${event.at}${idx}`}>
                              <TableCell data-l="Activity">
                                <Label variant="soft" sx={{ color: KIND_META[event.kind].color(palette), mr: 0.75 }}>
                                  {KIND_META[event.kind].label}
                                </Label>
                                <Typography component="span" variant="caption" sx={HS_FAINT}>
                                  {event.label}
                                  {grainLabel}
                                </Typography>
                              </TableCell>
                              <TableCell data-l="When (IST)" sx={{ ...HS_MONO, typography: "caption" }}>{isoTime}</TableCell>
                              <TableCell data-l="2h before" data-num sx={{ typography: "caption" }} title={event.before_window_incomplete ? "Window contains gaps or reconnect delta" : undefined}>{beforeDelta}</TableCell>
                              <TableCell data-l="2h after" data-num sx={{ typography: "caption" }} title={event.after_window_incomplete ? "Window contains gaps or reconnect delta" : undefined}>{afterDelta}</TableCell>
                              <TableCell data-l="Change" data-num sx={{ typography: "caption", fontWeight: "fontWeightBold", color: CHANGE_COLOR[tone] }} title={event.before_window_incomplete || event.after_window_incomplete ? "Change computed from incomplete windows" : undefined}>{changePercent}</TableCell>
                              <TableCell data-l="Read">
                                <Tag tone="mut">{event.grain === "animal" ? "Animal" : event.grain === "shed" ? "Pen" : "ID"}</Tag>
                              </TableCell>
                            </TableRow>
                          );
                        })
                      )}
                    </TableBody>
                  </Table>
                </Scrollbar>
                <Typography variant="caption" component="p" sx={{ px: { xs: 1.5, sm: 2.5 }, py: 2, m: 0, color: "text.secondary" }}>
                  Change compares the summed motion-count delta in the two hours before and after the recorded activity. A
                  feed correlation is a pen-level response to feeding — it is not eating detection. A post-vaccination or
                  post-treatment change is a watch signal, never a diagnosis or an adverse-event finding.
                </Typography>
              </Card>
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <Card sx={{ height: 1 }}>
                <CardHeader title="Animal & tag detail" />
                <CardContent>
                  <Box
                    component="dl"
                    sx={{ m: 0, display: "grid", gridTemplateColumns: { xs: "1fr", sm: "auto 1fr" }, columnGap: 1.5, rowGap: { xs: 0.5, sm: 1 }, typography: "body2", alignItems: "baseline" }}
                  >
                    {detailRows.map((row) => (
                      <DetailRow key={row.label} label={row.label} source={row.source} mono={row.mono} title={row.title}>
                        {row.value}
                      </DetailRow>
                    ))}
                  </Box>
                </CardContent>
              </Card>
            </Grid>
          </Grid>
        </Stack>
      </Box>
    </Dialog>
  );
}
