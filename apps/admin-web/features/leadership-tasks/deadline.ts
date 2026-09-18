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

/**
 * The deadline as the two modals now POST it: the console's own calendar (`ThemedDatePicker`)
 * gives a `YYYY-MM-DD` day and two selects give the hour and minute, on the farm's clock. The
 * three are joined back into the `YYYY-MM-DDTHH:MM` shape `farmDeadlineToRFC3339` already
 * accepts, so the wire contract and the edit form's reverse trip are untouched.
 *
 * A native `datetime-local` used to be the field; Chrome draws its picker OUTSIDE the modal box,
 * over the attachment buttons and Send, and it ignores the DD/MM/YYYY rule the console renders
 * every other date in. The old single field is still read first, so a form that posts
 * `deadline_at` (an older tab) keeps working.
 *
 * A day with no time is NOT a deadline: the backend refuses a raise without an instant, and
 * silently filling midnight would set a deadline the raiser never chose. It reads as missing.
 */
export function composeFarmDeadlineLocal(
  date: string,
  hour: string,
  minute: string,
): string {
  const day = date.trim();
  const hh = hour.trim();
  const mm = minute.trim();
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return "";
  if (!/^\d{2}$/.test(hh) || Number(hh) > 23) return "";
  if (!/^\d{2}$/.test(mm) || Number(mm) > 59) return "";
  return `${day}T${hh}:${mm}`;
}

/**
 * Split a `YYYY-MM-DDTHH:MM` farm-clock value into the three parts the form's controls take.
 * Anything else is three blanks, which the controls render as "not chosen yet".
 */
export function splitFarmDeadlineLocal(local: string): {
  date: string;
  hour: string;
  minute: string;
} {
  const match = /^(\d{4}-\d{2}-\d{2})T(\d{2}):(\d{2})/.exec(local.trim());
  if (!match) return { date: "", hour: "", minute: "" };
  return { date: match[1], hour: match[2], minute: match[3] };
}

/**
 * What a posted form means by its deadline: the single `deadline_at` field when it is there
 * (an older tab), else the day + hour + minute the current form posts. One reader for both
 * Server Actions, so they cannot disagree about the field names.
 */
export function farmDeadlineLocalFromForm(formData: FormData): string {
  const single = String(formData.get("deadline_at") ?? "").trim();
  if (single) return single;
  return composeFarmDeadlineLocal(
    String(formData.get("deadline_date") ?? ""),
    String(formData.get("deadline_hour") ?? ""),
    String(formData.get("deadline_minute") ?? ""),
  );
}
