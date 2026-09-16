"use server";

// The Configure drawer's one write: switch a rule on or off and set its threshold. Runs as a
// Server Action (authenticated through the server config, never a client fetch) and returns the
// backend's answer to the drawer, which re-renders the row from what the server read back.
import { revalidatePath } from "next/cache";
import { setAlertRuleConfig, type AlertRuleConfig } from "@/lib/api/alerts-server";

const PATHNAME = "/alerts";

export type SaveAlertRuleResult = { ok: true; rule: AlertRuleConfig } | { ok: false; code: string };

export async function saveAlertRuleAction(input: { ruleKey: string; enabled: boolean; threshold: string }): Promise<SaveAlertRuleResult> {
  const ruleKey = input.ruleKey.trim();
  const raw = input.threshold.trim();
  // A blank field is not a zero. The backend owns the range refusal; this only refuses a value
  // that is not a whole number at all, because there is nothing to send.
  if (!ruleKey) return { ok: false, code: "config_invalid" };
  if (raw === "" || !/^\d+$/.test(raw)) return { ok: false, code: "config_invalid" };
  const threshold = Number(raw);
  const idempotencyKey = `alert-rule:${ruleKey}:${input.enabled ? "on" : "off"}:${threshold}`;
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
  // The rules decide which alerts the page shows, so the page is stale the moment one changes.
  revalidatePath(PATHNAME);
  return { ok: true, rule: result.data };
}
