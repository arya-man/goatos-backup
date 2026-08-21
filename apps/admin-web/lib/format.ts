export function dash(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  return String(value);
}

export function shortId(value: string | null | undefined): string {
  if (!value) return "—";
  if (value.length <= 12) return value;
  return `${value.slice(0, 8)}…${value.slice(-4)}`;
}

export const GOATOS_TIME_ZONE = "Asia/Kolkata";
const IST_OFFSET = "+05:30";

function partsByType(date: Date, options: Intl.DateTimeFormatOptions): Record<string, string> {
  return Object.fromEntries(
    new Intl.DateTimeFormat("en-GB", { timeZone: GOATOS_TIME_ZONE, ...options })
      .formatToParts(date)
      .map((part) => [part.type, part.value]),
  );
}

function istDate(value: Date): string {
  const parts = partsByType(value, { year: "numeric", month: "2-digit", day: "2-digit" });
  return `${parts.year}-${parts.month}-${parts.day}`;
}

export function dateTime(value: string | null | undefined): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("en-IN", {
    timeZone: GOATOS_TIME_ZONE,
    dateStyle: "medium",
    timeStyle: "short",
    hour12: false,
  }).format(date);
}

export function joinParts(parts: Array<string | null | undefined>): string {
  const filtered = parts.filter((part): part is string => Boolean(part));
  return filtered.length > 0 ? filtered.join(" · ") : "—";
}

// IST formatters used across admin-web screens, defined once so every screen
// renders dates identically, preserving Goat OS' Asia/Kolkata business calendar.
//
// DATE DISPLAY RULE (maintainer decision 2026-08-21): every VISIBLE date in an
// admin-web table, card, or drawer renders DD-MM-YYYY through fmtDate. Chart
// axes use the compact dd-mm-yy in components/svg-series.tsx. Wire formats —
// query params, API payloads, keys — stay ISO YYYY-MM-DD (todayIso/istDayPlus).
// Machine gate: make admin-web-date-format-guard.

// "DD-MM-YYYY", or "—" when missing, or the raw string when unparseable.
export function fmtDate(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const parts = partsByType(d, { year: "numeric", month: "2-digit", day: "2-digit" });
  return `${parts.day}-${parts.month}-${parts.year}`;
}

// "DD-MM-YYYY HH:MM", or "" when missing, or the raw string when unparseable.
export function fmtDateTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const parts = partsByType(d, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
  });
  return `${parts.day}-${parts.month}-${parts.year} ${parts.hour}:${parts.minute}`;
}

// "YYYY-MM-DD" for the current Goat OS business day (Asia/Kolkata). Call at module scope in RSC
// files so the render path stays free of `new Date`.
export function todayIso(): string {
  return istDate(new Date());
}

// humanizeDurationMs renders a non-negative millisecond span as a compact "Nd Nh" / "Nh Nm" / "Nm"
// string, "just now" under a minute. Used by the Verify queue table's "In queue" (age since
// captured_at) and "Review took" (verified_at - captured_at) columns.
export function humanizeDurationMs(ms: number): string {
  const clamped = Math.max(0, ms);
  const minutes = Math.floor(clamped / 60000);
  if (minutes < 1) return "just now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ${minutes % 60}m`;
  const days = Math.floor(hours / 24);
  return `${days}d ${hours % 24}h`;
}

/**
 * Shifts a "YYYY-MM-DD" Goat OS business day by whole days.
 *
 * Pure calendar arithmetic on the already-resolved day string: the timezone conversion happened
 * once in `todayIso()`, so this must NOT re-enter a timezone (adding 24h to an instant would land
 * on the wrong IST day across a DST-shifted locale, and hardcoding +05:30 here would duplicate a
 * fact `GOATOS_TIME_ZONE` already owns). `Date.UTC` is used purely as a month/year-rollover
 * calculator on the bare Y-M-D, never as a clock.
 */
export function istDayPlus(day: string, days: number): string {
  const [year, month, date] = day.split("-").map(Number);
  if (!Number.isFinite(year) || !Number.isFinite(month) || !Number.isFinite(date)) return day;
  const shifted = new Date(Date.UTC(year, month - 1, date + days));
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${shifted.getUTCFullYear()}-${pad(shifted.getUTCMonth() + 1)}-${pad(shifted.getUTCDate())}`;
}

export function istBusinessDayStartInstantIso(day: string): string {
  const instant = new Date(`${day}T00:00:00${IST_OFFSET}`);
  return Number.isNaN(instant.getTime()) ? day : instant.toISOString();
}
