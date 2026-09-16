"use client";

import { useState, useTransition } from "react";
import { Settings2 } from "lucide-react";

import { LocalOverlayDrawer } from "@/components/local-overlay-drawer";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { AlertRuleConfig } from "@/lib/api/alerts-server";
import { saveAlertRuleAction } from "./actions";

export const PARAM_CONFIGURE = "configure";

/**
 * The Configure drawer: one row per catalog rule -- on/off, the threshold with its backend-named
 * unit, Save. Rendered ONLY when the page's `configure_alerts` control is enabled (the caller
 * gates it); the rules come from GET /alerts/config, which the same capability gates, so the
 * drawer never renders a bare error for someone the server would refuse. Open/close is local UI
 * state mirrored in the URL (?configure=1); Save goes through a Server Action and the row
 * re-renders from what the backend read back.
 */
export function AlertsConfigure({ pageContract, rules, initialOpen, closeHref }: { pageContract: AdminUiPageContract; rules: AlertRuleConfig[] | null; initialOpen: boolean; closeHref: string }) {
  const t = (key: string) => copy(pageContract, key);
  const item = {
    id: "1",
    eyebrow: t("crumb"),
    title: t("configure.title"),
    icon: <Settings2 className="ic" aria-hidden="true" />,
    body: rules ? (
      <div data-testid="alerts-configure-drawer">
        <p className="small muted" style={{ marginTop: 0 }}>
          {t("configure.intro")}
        </p>
        {rules.map((rule) => (
          <RuleRow key={rule.key} rule={rule} pageContract={pageContract} />
        ))}
        <p className="small muted" style={{ marginTop: 14 }}>
          {t("configure.more_rules")}
        </p>
      </div>
    ) : (
      <div className="small muted">{t("configure.unavailable")}</div>
    ),
  };
  return <LocalOverlayDrawer items={[item]} selectionKey={PARAM_CONFIGURE} initialSelectedId={initialOpen ? "1" : undefined} closeHref={closeHref} ariaLabel={t("configure.title")} closeLabel={t("configure.close")} />;
}

function RuleRow({ rule: initial, pageContract }: { rule: AlertRuleConfig; pageContract: AdminUiPageContract }) {
  const t = (key: string) => copy(pageContract, key);
  const [rule, setRule] = useState(initial);
  const [enabled, setEnabled] = useState(initial.enabled);
  const [threshold, setThreshold] = useState(String(initial.threshold));
  const [message, setMessage] = useState<{ tone: "ok" | "dng"; text: string } | null>(null);
  const [pending, startTransition] = useTransition();
  const dirty = enabled !== rule.enabled || threshold.trim() !== String(rule.threshold);
  const inputId = `alert-rule-${rule.key}-threshold`;

  const onSave = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await saveAlertRuleAction({ ruleKey: rule.key, enabled, threshold });
      if (result.ok) {
        setRule(result.rule);
        setEnabled(result.rule.enabled);
        setThreshold(String(result.rule.threshold));
        setMessage({ tone: "ok", text: t("action.config_saved") });
      } else {
        setMessage({ tone: "dng", text: t(`action.${result.code}`) });
      }
    });
  };

  return (
    <div className="card" style={{ marginBottom: 10 }} data-testid="alerts-rule-row" data-rule={rule.key}>
      <div className="bd" style={{ display: "grid", gap: 8 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <label className="small" style={{ display: "flex", alignItems: "center", gap: 8, fontWeight: 700 }}>
            <input type="checkbox" checked={enabled} disabled={pending} onChange={(event) => setEnabled(event.target.checked)} data-testid="alerts-rule-enabled" />
            {rule.label}
          </label>
          <span className="sp" style={{ flex: 1 }} />
          <span className={`tag ${enabled ? "t-ok" : "t-mut"}`}>{enabled ? t("configure.enabled") : t("configure.disabled")}</span>
        </div>
        <div className="small muted">{rule.description}</div>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <label className="small" htmlFor={inputId}>
            {rule.threshold_label}
          </label>
          <input
            id={inputId}
            className="inp"
            type="number"
            inputMode="numeric"
            min={rule.min_threshold}
            max={rule.max_threshold}
            step={1}
            value={threshold}
            disabled={pending}
            onChange={(event) => setThreshold(event.target.value)}
            style={{ width: 96 }}
            data-testid="alerts-rule-threshold"
          />
          <span className="small muted">{rule.threshold_unit}</span>
          <span className="small muted">
            · {t("configure.default")} {rule.default_threshold}
          </span>
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn primary sm" disabled={pending || !dirty} onClick={onSave} data-testid="alerts-rule-save">
            {t("configure.save")}
          </button>
        </div>
        {rule.stored && rule.updated_at ? (
          <div className="small muted">
            {t("configure.updated_by")} {rule.updated_by || "—"} · {rule.updated_at}
          </div>
        ) : null}
        {message ? (
          <div className="small" role="status" style={{ color: message.tone === "dng" ? "var(--danger)" : "var(--ok)" }}>
            {message.text}
          </div>
        ) : null}
      </div>
    </div>
  );
}
