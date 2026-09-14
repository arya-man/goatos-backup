/**
 * The task form's `datetime-local` value (`YYYY-MM-DDTHH:MM`, no zone) is the deadline on the
 * FARM's clock, so it is stamped with the IST offset before it goes on the wire -- Goat OS
 * business time is Asia/Kolkata whatever the browser's zone. Anything that is not that shape
 * is treated as missing; the backend validates the instant itself (must be after the raise).
 */
export function farmDeadlineToRFC3339(local: string): string {
  const trimmed = local.trim();
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(trimmed)) return "";
  const withSeconds = trimmed.length === 16 ? `${trimmed}:00` : trimmed;
  return `${withSeconds}+05:30`;
}
