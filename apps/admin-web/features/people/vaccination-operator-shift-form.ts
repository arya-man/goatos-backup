// Pure helpers for the Vaccination operators screen's Set / Edit / Clear shift form.
//
// The backend (PUT /vaccination/operator-shifts) is the authority on every rule -- label, 24-hour
// times, end after start, week-off -- and refuses a bad field with a farm-worded message the form
// shows verbatim. These helpers only turn a stored shift into the form's text fields and the form
// back into the request, and name the stored shift codes in farm words so `am` / `rover` never
// reach the screen. Kept free of runtime imports so `node --test` can exercise them directly.

export type ShiftLabelCode = 'am' | 'pm' | 'rover';

export const SHIFT_LABEL_OPTIONS: ReadonlyArray<{ value: ShiftLabelCode; label: string }> = [
  { value: 'am', label: 'Morning' },
  { value: 'pm', label: 'Afternoon' },
  { value: 'rover', label: 'Rover' },
];

export const WEEK_OFF_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
  { value: '', label: 'No week off' },
  { value: 'monday', label: 'Monday' },
  { value: 'tuesday', label: 'Tuesday' },
  { value: 'wednesday', label: 'Wednesday' },
  { value: 'thursday', label: 'Thursday' },
  { value: 'friday', label: 'Friday' },
  { value: 'saturday', label: 'Saturday' },
  { value: 'sunday', label: 'Sunday' },
];

export interface StoredShift {
  operatorId: string;
  shiftLabel: string;
  shiftStartMinute: number;
  shiftEndMinute: number;
  weekOffWeekday?: string;
}

export interface ShiftDraft {
  shiftLabel: string;
  shiftStart: string;
  shiftEnd: string;
  weekOffWeekday: string;
}

export function shiftLabelName(code: string): string {
  return SHIFT_LABEL_OPTIONS.find((o) => o.value === code)?.label ?? 'Shift';
}

export function minutesToClock(minutes: number): string {
  const h = Math.floor(minutes / 60);
  const m = minutes % 60;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

/** "Morning · 08:00–17:00" for the roster's Shift column. */
export function shiftSummary(shift: StoredShift): string {
  return `${shiftLabelName(shift.shiftLabel)} · ${minutesToClock(shift.shiftStartMinute)}–${minutesToClock(shift.shiftEndMinute)}`;
}

/**
 * The form's starting values. An existing shift opens as itself; a new one opens EMPTY -- no
 * invented default shift or times, so the admin types what the operator actually works. The
 * week-off starts from the roster seat's own week-off when the operator has one.
 */
export function draftFromShift(shift: StoredShift | undefined, seatWeekOff?: string | null): ShiftDraft {
  if (shift) {
    return {
      shiftLabel: shift.shiftLabel,
      shiftStart: minutesToClock(shift.shiftStartMinute),
      shiftEnd: minutesToClock(shift.shiftEndMinute),
      weekOffWeekday: shift.weekOffWeekday ?? '',
    };
  }
  const seat = (seatWeekOff ?? '').toLowerCase();
  return {
    shiftLabel: '',
    shiftStart: '',
    shiftEnd: '',
    weekOffWeekday: WEEK_OFF_OPTIONS.some((o) => o.value === seat) ? seat : '',
  };
}

/** The PUT body. Blank week-off is sent as null ("no week off"), never as a guessed day. */
export function shiftRequestFromDraft(parkId: string, operatorId: string, draft: ShiftDraft) {
  return {
    park_id: parkId,
    operator_id: operatorId,
    shift_label: draft.shiftLabel as ShiftLabelCode,
    shift_start: draft.shiftStart.trim(),
    shift_end: draft.shiftEnd.trim(),
    week_off_weekday: draft.weekOffWeekday.trim() === '' ? null : draft.weekOffWeekday.trim(),
  };
}
