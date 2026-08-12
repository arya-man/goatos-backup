// Presentation helpers for the live drive tracker.
//
// Every timestamp on this page is rendered in the BUSINESS timezone, never the viewer's: a drive day
// is an Asia/Kolkata day, and a browser in another zone must not shift a shed's last-proof time onto
// a different hour than the operator who produced it sees.

const IST = "Asia/Kolkata";

const clockFormatter = new Intl.DateTimeFormat("en-GB", {
  timeZone: IST,
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

const shortClockFormatter = new Intl.DateTimeFormat("en-GB", {
  timeZone: IST,
  hour: "2-digit",
  minute: "2-digit",
  hour12: false,
});

const driveDayFormatter = new Intl.DateTimeFormat("en-GB", {
  timeZone: IST,
  weekday: "short",
  day: "numeric",
  month: "short",
});

export function fmtClockSeconds(iso: string | null | undefined): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return clockFormatter.format(date);
}

export function fmtClock(iso: string | null | undefined): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return shortClockFormatter.format(date);
}

export function fmtDriveDay(businessDate: string): string {
  // A bare YYYY-MM-DD is parsed as UTC midnight; anchoring it at midday keeps the IST rendering on
  // the same calendar day for every viewer timezone.
  const date = new Date(`${businessDate}T12:00:00+05:30`);
  if (Number.isNaN(date.getTime())) return businessDate;
  return driveDayFormatter.format(date);
}

export function pct(done: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, Math.min(100, Math.round((done / total) * 100)));
}

export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return "";
  if (parts.length === 1) return parts[0].slice(0, 1).toUpperCase();
  return (parts[0].slice(0, 1) + parts[parts.length - 1].slice(0, 1)).toUpperCase();
}

// Progress-bar tone follows the same thresholds the backend uses for row state, so the bar colour
// and the status tag can never disagree.
export function progressTone(done: number, total: number): "" | "warn" | "dng" {
  if (total <= 0 || done === 0) return "dng";
  if (done / total < 0.25) return "warn";
  return "";
}
