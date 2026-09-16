"use server";

// The Configure drawer's one write: switch a rule on or off and set its threshold. Runs as a
// Server Action (authenticated through the server config, never a client fetch) and returns the
// backend's answer to the drawer, which re-renders the row from what the server read back.
import { setAlertRuleConfig, type AlertRuleConfig } from "@/lib/api/alerts-server";

export type SaveAlertRuleResult = { ok: true; rule: AlertRuleConfig } | { ok: false; code: string };

export async function saveAlertRuleAction(input: { ruleKey: string; enabled: boolean; threshold: string; editedFrom: string }): Promise<SaveAlertRuleResult> {
  const ruleKey = input.ruleKey.trim();
  const raw = input.threshold.trim();
  // A blank field is not a zero. The backend owns the range refusal; this only refuses a value
  // that is not a whole number at all, because there is nothing to send.
  if (!ruleKey) return { ok: false, code: "config_invalid" };
  if (raw === "" || !/^\d+$/.test(raw)) return { ok: false, code: "config_invalid" };
  const threshold = Number(raw);
  // Derived, never random -- a double-click or a retried action is ONE write -- but keyed on the
  // ROW VERSION the drawer was edited from (its last-changed label, or "default" while no row
  // exists), not on the value alone. Keyed on the value, setting 8 after an earlier 9 -> 8 -> 9
  // would replay the old "8" write, save nothing, and still say "Rule saved" while the server
  // handed back 9. Every successful save moves the version, so the next edit is a new key.
  const idempotencyKey = `alert-rule:${ruleKey}:from:${input.editedFrom || "default"}:${input.enabled ? "on" : "off"}:${threshold}`;
  const result = await setAlertRuleConfig(ruleKey, { enabled: input.enabled, threshold }, idempotencyKey);
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
