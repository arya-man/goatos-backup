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

// IST ISO-shaped formatters used by vaccination / parks process-integrity screens. Distinct from
// the locale-based `dateTime` above on purpose: these mirror the mock's compact presentation while
// preserving Goat OS' Asia/Kolkata business calendar.
// Defined once so every screen renders dates identically.

// "YYYY-MM-DD", or "—" when missing, or the raw string when unparseable.
export function fmtDate(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : istDate(d);
}

// "YYYY-MM-DD HH:MM", or "" when missing, or the raw string when unparseable.
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
  return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`;
}

// "YYYY-MM-DD" for the current Goat OS business day (Asia/Kolkata). Call at module scope in RSC
// files so the render path stays free of `new Date`.
export function todayIso(): string {
  return istDate(new Date());
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
