"use server";

import { actionErrorMessage, actionRedirect, optionalString, requiredString } from "@/lib/action-helpers";
import { revalidateVaccinationCommandLenses } from "@/lib/vaccination-command-lenses";
import { sendCalendarNudge, snoozeCalendarEvent } from "./calendar-server";

// Backend rejects an Idempotency-Key outside 8–200 chars (calendar/app/service.go). The default key is a
// 36-char UUID, but a server action accepts arbitrary FormData, so validate before it reaches the header
// and surfaces as a generic backend failure.
function requiredIdempotencyKey(formData: FormData): string {
  const key = requiredString(formData, "idempotency_key");
  if (key.length < 8 || key.length > 200) {
    throw new Error("idempotency_key must be between 8 and 200 characters");
  }
  return key;
}

// Send nudge — idempotent reminder/escalation. The idempotency key is rendered per drawer view and carried
// in a hidden field, so an accidental double-submit of the same rendered form replays (no duplicate nudge).
export async function sendNudgeAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.nudge_sent";
  try {
    const eventId = requiredString(formData, "event_id");
    const key = requiredIdempotencyKey(formData);
    const reason = optionalString(formData, "reason") ?? "Nudge from Calendar";
    const result = await sendCalendarNudge(eventId, { reason }, key);
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      actionKey = result.data.idempotent_replay ? "action.nudge_replay" : "action.nudge_sent";
      revalidateVaccinationCommandLenses();
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}

// Snooze — durable snooze keyed to the underlying due-work target; does not mutate obligation truth.
export async function snoozeAction(formData: FormData): Promise<void> {
  let status: "success" | "error" = "success";
  let actionKey = "action.snooze_recorded";
  try {
    const eventId = requiredString(formData, "event_id");
    const key = requiredIdempotencyKey(formData);
    // Default one-click snooze: +24h from now (server-side; impure Date is fine outside render).
    const snoozeUntil = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString();
    const reason = optionalString(formData, "reason") ?? "Snoozed from Calendar";
    const result = await snoozeCalendarEvent(eventId, { snooze_until: snoozeUntil, reason, replace_existing: false }, key);
    if (!result.ok) {
      status = "error";
      actionKey = actionErrorMessage(result.error);
    } else {
      actionKey = result.data.idempotent_replay ? "action.snooze_replay" : "action.snooze_recorded";
      revalidateVaccinationCommandLenses();
    }
  } catch (error) {
    void error;
    status = "error";
    actionKey = "action.error_form";
  }
  actionRedirect(formData, status, actionKey);
}
