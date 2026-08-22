import type { Tone } from "@/components/ui-primitives";
import type {
  HerdSignalBatteryState,
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
  ok: "OK",
  low: "Low battery",
};

export const BATTERY_TONE: Record<HerdSignalBatteryState, Tone> = {
  ok: "mut",
  low: "warn",
};

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

export function fmtRssi(dbm: number | null | undefined): string {
  if (dbm === null || dbm === undefined) return "—";
  return `${dbm} dBm`;
}

export function fmtBatteryMv(mv: number | null | undefined): string {
  if (mv === null || mv === undefined) return "—";
  return `${(mv / 1000).toFixed(2)} V`;
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
