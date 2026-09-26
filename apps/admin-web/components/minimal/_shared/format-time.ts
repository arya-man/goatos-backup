// Adapted from the licensed MUI Minimal template (src/utils/format-time.ts).
// Visible dates follow the admin-web Date Display Rule: DD/MM/YYYY via lib/format.
import dayjs from 'dayjs';

import { fmtDate } from '@/lib/format';

type DateInput = dayjs.Dayjs | Date | string | number | null | undefined;

export function fIsAfter(start: DateInput, end: DateInput): boolean {
  if (!start || !end) return false;
  const s = dayjs(start);
  const e = dayjs(end);
  if (!s.isValid() || !e.isValid()) return false;
  return s.isAfter(e);
}

/**
 * Range label: always full DD/MM/YYYY on both ends. The template's `initial`
 * flag picks full dates over compact ones; the Date Display Rule bans compact
 * forms, so both paths already return the full form. Kept for call-site parity.
 */
export function fDateRangeShortLabel(start: DateInput, end: DateInput, initial?: boolean): string {
  void initial;
  if (!start || !end) return '';
  const s = dayjs(start);
  const e = dayjs(end);
  if (!s.isValid() || !e.isValid() || s.isAfter(e)) return 'Invalid date';
  const a = fmtDate(s.format('YYYY-MM-DD'));
  const b = fmtDate(e.format('YYYY-MM-DD'));
  return s.isSame(e, 'day') ? b : `${a} - ${b}`;
}
