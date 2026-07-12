export interface CalendarDateWindow {
  dateFrom: string;
  dateTo: string;
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

/** Rolling 45-day history window ending on the anchor IST business date. */
export function historyWindow(anchorKey: string): CalendarDateWindow {
  const [year, month, day] = anchorKey.split("-").map(Number);
  const anchor = new Date(Date.UTC(year, month - 1, day));
  const start = new Date(anchor);
  start.setUTCDate(anchor.getUTCDate() - 44);
  const dateKey = (value: Date) => value.toISOString().slice(0, 10);
  return { dateFrom: dateKey(start), dateTo: dateKey(anchor) };
}
