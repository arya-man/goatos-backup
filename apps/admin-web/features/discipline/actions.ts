"use server";

import {
  recordWorkforceViolation,
  submitWorkforceEnquiry,
  withdrawWorkforceViolation,
  type EnquiryDetail,
  type RecordViolationBody,
  type SubmitEnquiryBody,
  type Violation,
} from "@/lib/api/server";

// HRMS violations and enquiries writes (maintainer decisions 2026-09-30). Server Actions,
// authenticated through the server config. Each RETURNS the saved row and the client puts it in
// place; there is deliberately no revalidatePath (the admin-web interaction rule: return the row
// OR re-render, never both). The backend owns every rule and every error sentence.

export type DisciplineResult<T> = { ok: true; row: T } | { ok: false; code: string; message: string };

export async function recordViolationAction(body: RecordViolationBody): Promise<DisciplineResult<Violation>> {
  const res = await recordWorkforceViolation(body);
  if (!res.ok) return { ok: false, code: res.error.code ?? res.error.kind, message: res.error.message };
  return { ok: true, row: res.data.violation };
}

export async function withdrawViolationAction(violationId: string, reason: string, rowVersion: number): Promise<DisciplineResult<Violation>> {
  const res = await withdrawWorkforceViolation(violationId, { reason, row_version: rowVersion });
  if (!res.ok) return { ok: false, code: res.error.code ?? res.error.kind, message: res.error.message };
  return { ok: true, row: res.data.violation };
}

export async function submitEnquiryAction(enquiryId: string, body: SubmitEnquiryBody): Promise<DisciplineResult<EnquiryDetail>> {
  const res = await submitWorkforceEnquiry(enquiryId, body);
  if (!res.ok) return { ok: false, code: res.error.code ?? res.error.kind, message: res.error.message };
  return { ok: true, row: res.data };
}
