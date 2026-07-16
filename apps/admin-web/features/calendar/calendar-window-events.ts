export interface CalendarWindowEvent {
  due_at: string;
}

export function calendarEventDateKey(iso: string): string {
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) return iso.slice(0, 10);
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: "Asia/Kolkata",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(parsed);
  const value = (kind: string) => parts.find((part) => part.type === kind)?.value ?? "";
  return `${value("year")}-${value("month")}-${value("day")}`;
}

export function calendarEventWeekday(iso: string): string {
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) return "";
  return new Intl.DateTimeFormat("en-US", { timeZone: "Asia/Kolkata", weekday: "short" }).format(parsed);
}

export function selectedWeekDateKeys(anchorDay: string): Set<string> {
  const [year, month, day] = anchorDay.split("-").map(Number);
  const anchor = new Date(Date.UTC(year, month - 1, day));
  const mondayOffset = (anchor.getUTCDay() + 6) % 7;
  const monday = new Date(anchor);
  monday.setUTCDate(anchor.getUTCDate() - mondayOffset);

  const days: string[] = [];
  for (let i = 0; i < 7; i++) {
    const d = new Date(monday);
    d.setUTCDate(monday.getUTCDate() + i);
    days.push(d.toISOString().slice(0, 10));
  }
  return new Set(days);
}

export function filterEventsForSelectedWeek<T extends CalendarWindowEvent>(events: T[], anchorDay: string, dayFilter?: string): T[] {
  const weekDates = selectedWeekDateKeys(anchorDay);
  return events.filter((event) => {
    if (!weekDates.has(calendarEventDateKey(event.due_at))) return false;
    return !dayFilter || calendarEventWeekday(event.due_at) === dayFilter;
  });
}
