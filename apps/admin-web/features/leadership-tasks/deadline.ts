/**
 * The task form's `datetime-local` value (`YYYY-MM-DDTHH:MM`, no zone) is the deadline on the
 * FARM's clock, so it is stamped with the IST offset before it goes on the wire -- Mesha
 * business time is Asia/Kolkata whatever the browser's zone. Anything that is not that shape
 * is treated as missing; the backend validates the instant itself (must be after the raise).
 */
export function farmDeadlineToRFC3339(local: string): string {
  const trimmed = local.trim();
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(trimmed)) return "";
  const withSeconds = trimmed.length === 16 ? `${trimmed}:00` : trimmed;
  return `${withSeconds}+05:30`;
}

/**
 * The reverse trip, for the EDIT form: a stored RFC3339 deadline rendered back into the
 * `YYYY-MM-DDTHH:MM` a `datetime-local` input accepts, on the farm's clock (Asia/Kolkata).
 *
 * Converted through Intl rather than by slicing the string: the stored instant may carry any
 * offset (an older row, a client in another zone), and slicing would show the raiser a different
 * wall-clock time than the deadline they set. A value that is not an instant reads as no
 * deadline, which the edit form treats as "leave the stored deadline alone".
 */
export function rfc3339ToFarmDeadlineLocal(iso: string | null | undefined): string {
  const trimmed = (iso ?? "").trim();
  if (!trimmed) return "";
  const at = new Date(trimmed);
  if (Number.isNaN(at.getTime())) return "";
  const parts = new Intl.DateTimeFormat("en-GB", {
    timeZone: "Asia/Kolkata",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(at);
  const get = (type: string) => parts.find((part) => part.type === type)?.value ?? "";
  const hour = get("hour") === "24" ? "00" : get("hour");
  return `${get("year")}-${get("month")}-${get("day")}T${hour}:${get("minute")}`;
}
