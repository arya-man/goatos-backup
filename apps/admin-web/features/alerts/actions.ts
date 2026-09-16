"use server";

// The Configure drawer's one write: switch a rule on or off and set its threshold. Runs as a
// Server Action (authenticated through the server config, never a client fetch) and returns the
// backend's answer to the drawer, which re-renders the row from what the server read back.
import { createHash } from "node:crypto";
import {
  createAlertEventRule,
  deleteAlertEventRule,
  setAlertRuleConfig,
  updateAlertEventRule,
  type AlertEventRule,
  type AlertEventRuleRequest,
  type AlertRuleConfig,
} from "@/lib/api/alerts-server";

export type SaveAlertRuleResult = { ok: true; rule: AlertRuleConfig } | { ok: false; code: string };

// One key per SAVE ATTEMPT: the drawer mints a fresh nonce on every click and a retried Server
// Action carries the same one, so a network retry is ONE write while two deliberate saves are
// two -- even 8 -> 9 -> 8 inside one minute. Keying on the row's last-changed label was wrong
// twice over: the label is minute-grained, so a second change in the same minute replayed the
// first and said "Rule saved." over a value the server never took. The key is HASHED so the
// header stays ASCII whatever the values hold (a Telugu alert name, an arrow, a comma).
function idempotencyKey(...parts: string[]): string {
  return createHash("sha256").update(parts.join("\u001f")).digest("hex");
}

export async function saveAlertRuleAction(input: { ruleKey: string; enabled: boolean; threshold: string; attempt: string }): Promise<SaveAlertRuleResult> {
  const ruleKey = input.ruleKey.trim();
  const raw = input.threshold.trim();
  // A blank field is not a zero. The backend owns the range refusal; this only refuses a value
  // that is not a whole number at all, because there is nothing to send.
  if (!ruleKey) return { ok: false, code: "config_invalid" };
  if (raw === "" || !/^\d+$/.test(raw)) return { ok: false, code: "config_invalid" };
  const threshold = Number(raw);
  if (!input.attempt.trim()) return { ok: false, code: "config_failed" };
  const result = await setAlertRuleConfig(ruleKey, { enabled: input.enabled, threshold }, idempotencyKey("alert-rule", ruleKey, input.attempt, String(input.enabled), String(threshold)));
  if (!result.ok) {
    switch (result.error.code) {
      case "threshold_out_of_range":
        return { ok: false, code: "config_threshold_out_of_range" };
      case "idempotency_conflict":
        return { ok: false, code: "config_conflict" };
      default:
        return { ok: false, code: "config_failed" };
    }
  }
  // Deliberately NO revalidatePath here. The drawer is open on a locally pushed URL
  // (?configure=1) that Next's router never saw; a revalidation refresh restores the router's
  // own URL, the overlay hook reads the dropped parameter as "closed", and the drawer shuts a
  // second after every save (found on the 2026-09-16 click-through). The row re-renders from
  // the server's answer below; the alerts behind the drawer follow on the next page load, which
  // is what the drawer's intro copy says.
  return { ok: true, rule: result.data };
}

export type SaveEventRuleResult = { ok: true; rule: AlertEventRule } | { ok: false; code: string };
export type DeleteEventRuleResult = { ok: true } | { ok: false; code: string };

function eventRuleErrorCode(code: string | undefined): string {
  switch (code) {
    case "unknown_event_kind":
      return "config_unknown_event";
    case "invalid_event_rule":
      return "config_invalid_event";
    case "event_rule_not_found":
      return "config_event_missing";
    case "idempotency_conflict":
      return "config_conflict";
    default:
      return "config_failed";
  }
}

// Compose or change one event alert. Same per-attempt nonce as above; the label rides inside the
// hash, never in the header, so a non-ASCII name saves.
export async function saveAlertEventRuleAction(input: { id?: string; label: string; kind: string; severity: string; enabled: boolean; attempt: string }): Promise<SaveEventRuleResult> {
  const label = input.label.trim();
  if (!label) return { ok: false, code: "config_invalid_event" };
  if (!input.attempt.trim()) return { ok: false, code: "config_failed" };
  const body = { label, kind: input.kind.trim(), severity: input.severity as AlertEventRuleRequest["severity"], enabled: input.enabled };
  const key = idempotencyKey("alert-event", input.id ?? "new", input.attempt, body.kind, body.severity, String(body.enabled), label);
  const result = input.id ? await updateAlertEventRule(input.id, body, key) : await createAlertEventRule(body, key);
  if (!result.ok) return { ok: false, code: eventRuleErrorCode(result.error.code) };
  return { ok: true, rule: result.data };
}

export async function deleteAlertEventRuleAction(input: { id: string }): Promise<DeleteEventRuleResult> {
  const result = await deleteAlertEventRule(input.id);
  if (!result.ok) return { ok: false, code: eventRuleErrorCode(result.error.code) };
  return { ok: true };
}
