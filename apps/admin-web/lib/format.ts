export function dash(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—";
  return String(value);
}

export function shortId(value: string | null | undefined): string {
  if (!value) return "—";
  if (value.length <= 12) return value;
  return `${value.slice(0, 8)}…${value.slice(-4)}`;
}

export function dateTime(value: string | null | undefined): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("en-IN", {
    dateStyle: "medium",
    timeStyle: "short",
    hour12: false,
  });
}

export function joinParts(parts: Array<string | null | undefined>): string {
  const filtered = parts.filter((part): part is string => Boolean(part));
  return filtered.length > 0 ? filtered.join(" · ") : "—";
}

// ISO-shaped formatters used by the vaccination / parks process-integrity screens. Distinct from
// the locale-based `dateTime` above on purpose: these mirror the mock's compact ISO presentation.
// Defined once so every screen renders dates identically.

// "YYYY-MM-DD", or "—" when missing, or the raw string when unparseable.
export function fmtDate(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toISOString().slice(0, 10);
}

// "YYYY-MM-DD HH:MM", or "" when missing, or the raw string when unparseable.
export function fmtDateTime(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toISOString().slice(0, 16).replace("T", " ");
}

// "YYYY-MM-DD" for the current day (UTC). Call at module scope in RSC files so the render path
// stays free of `new Date`.
export function todayIso(): string {
  return new Date().toISOString().slice(0, 10);
}
