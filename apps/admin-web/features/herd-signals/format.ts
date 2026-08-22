import type { Tone } from "@/components/ui-primitives";
import type {
  HerdSignalBatteryState,
  HerdSignalBatteryTrend,
  HerdSignalMappingState,
  HerdSignalMovementState,
  HerdSignalPatternState,
  HerdSignalSensorState,
  HerdSignalTone,
} from "@/lib/api/herd-signals";

// Every tone/label mapping in this file exists to keep the approved vocabulary
// (docs/modules/herd-signals.md Section 4) in ONE place — no screen invents a synonym locally.

export const MOVEMENT_LABEL: Record<HerdSignalMovementState, string> = {
  moving: "Moving",
  low: "Low",
  quiet: "Quiet",
  not_moving: "No movement",
  stale: "Stale",
};

export const MOVEMENT_TONE: Record<HerdSignalMovementState, Tone> = {
  moving: "ok",
  low: "teal",
  quiet: "mut",
  not_moving: "warn",
  stale: "dng",
};

export const SIGNAL_LABEL: Record<HerdSignalTone, string> = {
  strong: "Strong",
  ok: "OK",
  weak: "Weak",
};

export const SIGNAL_TONE: Record<HerdSignalTone, Tone> = {
  strong: "ok",
  ok: "mut",
  weak: "warn",
};

export const MAPPING_LABEL: Record<HerdSignalMappingState, string> = {
  mapped: "Mapped",
  unmapped: "Unmapped",
  conflict: "Conflict",
};

export const MAPPING_TONE: Record<HerdSignalMappingState, Tone> = {
  mapped: "ok",
  unmapped: "mut",
  conflict: "dng",
};

export const BATTERY_LABEL: Record<HerdSignalBatteryState, string> = {
  healthy: "OK",
  watch: "Watch",
  low: "Low battery",
  critical: "Critical",
};

export const BATTERY_TONE: Record<HerdSignalBatteryState, Tone> = {
  healthy: "mut",
  watch: "warn",
  low: "warn",
  critical: "dng",
};

// battery_trend is a short, packet-derived voltage direction (Derived — rising/falling/flat over
// the given window), never re-adding the removed life-estimate: it states a direction observed in
// recent readings, not a forecast of remaining time.
export function fmtBatteryTrend(trend: HerdSignalBatteryTrend | null): string {
  if (!trend) return "—";
  const windowLabel = trend.window_seconds >= 3600 ? `${Math.round(trend.window_seconds / 3600)}h` : `${Math.round(trend.window_seconds / 60)}m`;
  const arrow = trend.direction === "rising" ? "up" : trend.direction === "falling" ? "down" : "flat";
  const range =
    trend.first_mv !== null && trend.last_mv !== null
      ? ` (${(trend.first_mv / 1000).toFixed(2)} V \u2192 ${(trend.last_mv / 1000).toFixed(2)} V)`
      : "";
  return `${arrow} over ${windowLabel}${range}`;
}

export const SENSOR_LABEL: Record<HerdSignalSensorState, string> = {
  ok: "OK",
  abnormal: "Sensor abnormal",
};

export const SENSOR_TONE: Record<HerdSignalSensorState, Tone> = {
  ok: "mut",
  abnormal: "dng",
};

export const PATTERN_LABEL: Record<HerdSignalPatternState, string> = {
  no_movement: "No movement now",
  quiet_watch: "Quiet watch",
  inactive: "Inactive alert",
  missing: "Missing signal",
  spike: "Movement spike",
  recovered: "Back to normal",
  normal: "Normal activity",
};

export const PATTERN_TONE: Record<HerdSignalPatternState, Tone> = {
  no_movement: "warn",
  quiet_watch: "mut",
  inactive: "dng",
  missing: "dng",
  spike: "warn",
  recovered: "ok",
  normal: "ok",
};

// Plain-English reason for the current pattern classification, shown under the movement-history
// chart (drawer and full-screen). Text ported from the mock's PATTERNS.<key>.why.
export const PATTERN_WHY: Record<HerdSignalPatternState, string> = {
  no_movement: "delta 0 in the current 15-minute window",
  quiet_watch: "low delta for 1-2 hours",
  inactive: "zero or very low delta for 3+ hours while the tag is still being seen",
  missing: "no packets for 30+ minutes",
  spike: "current delta far above this animal's own baseline",
  recovered: "activity resumed after a quiet period",
  normal: "deltas in line with this animal's baseline",
};

// Signed, tone-coloured delta for the live table ("+140" green, "+0" muted) — matches the mock's
// `.delta.up/.zero/.warnv` treatment instead of a bare unsigned number.
export function fmtSignedDelta(delta: number | null | undefined, moveThreshold = 100): { text: string; tone: "up" | "zero" | "warn" } {
  if (delta === null || delta === undefined) return { text: "—", tone: "zero" };
  const text = `${delta >= 0 ? "+" : ""}${delta.toLocaleString("en-IN")}`;
  if (delta === 0) return { text, tone: "zero" };
  if (delta >= moveThreshold) return { text, tone: "up" };
  return { text, tone: "warn" };
}

export function fmtRssi(dbm: number | null | undefined): string {
  if (dbm === null || dbm === undefined) return "—";
  return `${dbm} dBm`;
}

export function fmtBatteryMv(mv: number | null | undefined): string {
  if (mv === null || mv === undefined) return "—";
  return `${(mv / 1000).toFixed(1)} V`;
}

export function fmtTagTemp(celsius: number | null | undefined): string {
  if (celsius === null || celsius === undefined) return "—";
  return `${celsius.toFixed(1)} °C`;
}

export function fmtDelta(delta: number | null | undefined): string {
  if (delta === null || delta === undefined) return "—";
  return delta.toLocaleString("en-IN");
}

// The backend currently aliases motion_delta_1h to the 15m delta (motion_delta) rather than
// computing a genuine 1h window (tracked backend fix). Printing the identical number under a "1h"
// label states a measurement this page has not actually taken, so this renders "—" whenever the 1h
// value is missing OR indistinguishable from the 15m value — never the repeated number.
export function fmtDelta1h(delta1h: number | null | undefined, delta15m: number | null | undefined): string {
  if (delta1h === null || delta1h === undefined) return "—";
  if (delta15m !== null && delta15m !== undefined && delta1h === delta15m) return "—";
  return delta1h.toLocaleString("en-IN");
}

export function fmtAgo(iso: string | null | undefined, nowMs: number): string {
  if (!iso) return "—";
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const seconds = Math.max(0, Math.round((nowMs - then) / 1000));
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.round(minutes / 60);
  if (hours < 48) return `${hours}h ago`;
  const days = Math.round(hours / 24);
  return `${days}d ago`;
}

// HH:MM in IST, for the reconnect-bar readout's gap window ("14:05 - 16:20 IST"). Always fed a
// received_at value (server clock) — never gateway_seen_at (docs/modules/herd-signals.md "Time,
// clocks, and what happens during a network outage": the gateway's clock runs +02:30:00 ahead of
// real IST and is diagnostics-only).
export function fmtClockIst(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("en-IN", {
    timeZone: "Asia/Kolkata",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(date);
}

export function fmtClockSeconds(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat("en-IN", {
    timeZone: "Asia/Kolkata",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(date);
}

/** The mock renders a BLE MAC as F0:C9:90:A0:00:2A. The gateway sends it bare and
    lowercase (f0c990a0002a), which is unreadable at a glance and does not match any
    other MAC rendered in this product. Normalise for display only; the stored value
    stays exactly as the device sent it. */
export function fmtBleMac(mac: string | null | undefined): string {
  if (!mac) return "—";
  const bare = mac.replace(/[^0-9a-fA-F]/g, "").toUpperCase();
  if (bare.length !== 12) return mac.toUpperCase();
  return bare.match(/.{2}/g)!.join(":");
}
