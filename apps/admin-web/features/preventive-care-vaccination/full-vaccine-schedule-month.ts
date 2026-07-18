const SCHEDULE_MONTH_PARTS = new Intl.DateTimeFormat("en-CA", {
  timeZone: "Asia/Kolkata",
  year: "numeric",
  month: "2-digit",
});

export function isInScheduleMonth(iso: string | undefined, year: number, month: number): boolean {
  if (!iso) return false;
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return false;
  const parts = SCHEDULE_MONTH_PARTS.formatToParts(date);
  const part = (type: string) => parts.find((item) => item.type === type)?.value ?? "";
  return Number(part("year")) === year && Number(part("month")) === month;
}
