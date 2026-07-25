export interface CalendarDateWindow {
  dateFrom: string;
  dateTo: string;
}

export interface CalendarMonthAnchor {
  year: number;
  month: number;
}

export interface CalendarDateKeyParts {
  year: number;
  month: number;
  day: number;
  weekday: number;
}

function parseDateKey(anchorKey: string): { year: number; month: number; day: number } | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(anchorKey);
  if (!match) return null;
  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  if (!Number.isInteger(year) || !Number.isInteger(month) || !Number.isInteger(day)) return null;
  if (month < 1 || month > 12 || day < 1 || day > 31) return null;
  return { year, month, day };
}

export function calendarDateKeyParts(anchorKey: string): CalendarDateKeyParts | null {
  const parts = parseDateKey(anchorKey);
  if (!parts) return null;
  const date = new Date(Date.UTC(parts.year, parts.month - 1, parts.day));
  if (
    date.getUTCFullYear() !== parts.year ||
    date.getUTCMonth() !== parts.month - 1 ||
    date.getUTCDate() !== parts.day
  ) {
    return null;
  }
  return {
    year: parts.year,
    month: parts.month - 1,
    day: parts.day,
    weekday: date.getUTCDay(),
  };
}

export function shiftedDateKey(anchorKey: string, days: number): string {
  const parts = parseDateKey(anchorKey);
  if (!parts) return anchorKey;
  const shifted = new Date(Date.UTC(parts.year, parts.month - 1, parts.day + days));
  return `${shifted.getUTCFullYear()}-${String(shifted.getUTCMonth() + 1).padStart(2, "0")}-${String(shifted.getUTCDate()).padStart(2, "0")}`;
}

export function calendarMonthAnchor(anchorKey: string, fallbackKey: string): CalendarMonthAnchor {
  const source = /^\d{4}-\d{2}-\d{2}$/.test(anchorKey) ? anchorKey : fallbackKey;
  const year = Number(source.slice(0, 4));
  const monthNumber = Number(source.slice(5, 7));
  if (!Number.isInteger(year) || !Number.isInteger(monthNumber) || monthNumber < 1 || monthNumber > 12) {
    return { year: Number(fallbackKey.slice(0, 4)), month: Number(fallbackKey.slice(5, 7)) - 1 };
  }
  return { year, month: monthNumber - 1 };
}

export function shiftedMonthStartKey(year: number, month: number, offset: number): string {
  const shifted = new Date(Date.UTC(year, month + offset, 1));
  return `${shifted.getUTCFullYear()}-${String(shifted.getUTCMonth() + 1).padStart(2, "0")}-01`;
}

/** First/last calendar day of the month containing a YYYY-MM-DD anchor. */
export function monthWindow(anchorKey: string): CalendarDateWindow {
  const year = Number(anchorKey.slice(0, 4));
  const month = Number(anchorKey.slice(5, 7));
  const lastDay = new Date(Date.UTC(year, month, 0)).getUTCDate();
  const ym = anchorKey.slice(0, 7);
  return { dateFrom: `${ym}-01`, dateTo: `${ym}-${String(lastDay).padStart(2, "0")}` };
}

/**
 * Monday-Sunday business week containing an IST date key. UTC calendar
 * arithmetic over the date-only value prevents the server timezone from
 * shifting the anchor into the previous day.
 */
export function weekWindow(anchorKey: string): CalendarDateWindow {
  const [year, month, day] = anchorKey.split("-").map(Number);
  const anchor = new Date(Date.UTC(year, month - 1, day));
  const mondayOffset = (anchor.getUTCDay() + 6) % 7;
  const monday = new Date(anchor);
  monday.setUTCDate(anchor.getUTCDate() - mondayOffset);
  const sunday = new Date(monday);
  sunday.setUTCDate(monday.getUTCDate() + 6);
  const dateKey = (value: Date) => value.toISOString().slice(0, 10);
  return { dateFrom: dateKey(monday), dateTo: dateKey(sunday) };
}

/**
 * Enumerate all 7 days of a Monday-Sunday week starting from a window's dateFrom.
 * Uses UTC date arithmetic to avoid timezone shifts via toISOString().
 * Returns an array of YYYY-MM-DD strings guaranteed to be 7 consecutive calendar days.
 */
export function enumerateWeekDays(weekWindowDateFrom: string): string[] {
  const [year, month, day] = weekWindowDateFrom.split("-").map(Number);
  const days: string[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(Date.UTC(year, month - 1, day + i));
    days.push(d.toISOString().split("T")[0]);
  }
  return days;
}

/** Rolling 45-day history window ending on the anchor IST business date. */
export function historyWindow(anchorKey: string): CalendarDateWindow {
  const [year, month, day] = anchorKey.split("-").map(Number);
  const anchor = new Date(Date.UTC(year, month - 1, day));
  const start = new Date(anchor);
  start.setUTCDate(anchor.getUTCDate() - 44);
  const dateKey = (value: Date) => value.toISOString().slice(0, 10);
  return { dateFrom: dateKey(start), dateTo: dateKey(anchor) };
}
