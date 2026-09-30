"use server";

import {
  addWorkforceHoliday,
  removeWorkforceHoliday,
  setWorkforceMemberShift,
  setWorkforceParkShiftTiming,
  setWorkforceWeekOffs,
  type TimetablePerson,
  type TimetableShift,
  type WorkforceHoliday,
} from "@/lib/api/server";

// People / HRMS > Timetable writes (maintainer request 2026-09-30). Server Actions, authenticated
// through the server config, never a client fetch. Each RETURNS the saved row and the client puts
// it in place; there is deliberately no revalidatePath -- re-rendering the whole page to show one
// changed cell is the flicker the admin-web interaction rules ban. The backend owns every rule
// and every error sentence.

export type TimetableActionResult<T> = { ok: true; row: T } | { ok: false; code: string; message: string };

export async function setPersonShiftAction(input: {
  personId: string;
  shiftCode: string;
  rowVersion: number;
}): Promise<TimetableActionResult<TimetablePerson>> {
  const result = await setWorkforceMemberShift(input.personId, { shift_code: input.shiftCode, row_version: input.rowVersion });
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind, message: result.error.message };
  return { ok: true, row: result.data.person };
}

export async function setShiftTimingAction(input: {
  parkId: string;
  shiftCode: string;
  startMinute: number | null;
  endMinute: number | null;
  rowVersion: number;
}): Promise<TimetableActionResult<TimetableShift>> {
  const result = await setWorkforceParkShiftTiming(input.parkId, input.shiftCode, {
    start_minute: input.startMinute,
    end_minute: input.endMinute,
    row_version: input.rowVersion,
  });
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind, message: result.error.message };
  return { ok: true, row: result.data.shift };
}

/** A person's weekly off days (ISO weekdays, Monday = 1). */
export async function setWeekOffsAction(input: { personId: string; weekOffs: number[]; rowVersion: number }): Promise<TimetableActionResult<TimetablePerson>> {
  const result = await setWorkforceWeekOffs(input.personId, { week_offs: input.weekOffs, row_version: input.rowVersion });
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind, message: result.error.message };
  return { ok: true, row: result.data.person };
}

/** A holiday for every park (parkId "") or one park. */
export async function addHolidayAction(input: { holidayOn: string; parkId: string; label: string }): Promise<TimetableActionResult<WorkforceHoliday>> {
  const result = await addWorkforceHoliday({ holiday_on: input.holidayOn, park_id: input.parkId, label: input.label });
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind, message: result.error.message };
  return { ok: true, row: result.data.holiday };
}

export async function removeHolidayAction(holidayId: string): Promise<TimetableActionResult<string>> {
  const result = await removeWorkforceHoliday(holidayId);
  if (!result.ok) return { ok: false, code: result.error.code ?? result.error.kind, message: result.error.message };
  return { ok: true, row: holidayId };
}
