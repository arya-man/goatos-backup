"use client";

import { useMemo, useState, useTransition } from "react";
import TextField from "@mui/material/TextField";
import { Settings2 } from "lucide-react";

import { LocalOverlayDrawer } from "@/components/local-overlay-drawer";
import MenuItem from "@mui/material/MenuItem";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { AlertEventKind, AlertEventRule, AlertRuleConfig } from "@/lib/api/alerts-server";
import { deleteAlertEventRuleAction, saveAlertEventRuleAction, saveAlertRuleAction } from "./actions";
import { PARAM_CONFIGURE } from "./alerts-model";
import Checkbox from "@mui/material/Checkbox";
import FormControlLabel from "@mui/material/FormControlLabel";

/**
 * The Configure drawer: one row per catalog rule -- on/off, the threshold with its backend-named
 * unit, Save. Mounted ONLY when the page's `configure_alerts` control is enabled (the caller
 * gates it); the rules come from GET /alerts/config, which the same capability gates, so the
 * drawer never renders a bare error for someone the server would refuse. Open/close is local UI
 * state mirrored in the URL (?configure=1); Save goes through a Server Action and the row
 * re-renders from what the backend read back.
 */
export function AlertsConfigure({
  pageContract,
  rules,
  eventRules,
  eventKinds,
  initialOpen,
  closeHref,
}: {
  pageContract: AdminUiPageContract;
  rules: AlertRuleConfig[] | null;
  eventRules: AlertEventRule[];
  eventKinds: AlertEventKind[];
  initialOpen: boolean;
  closeHref: string;
}) {
  const t = (key: string) => copy(pageContract, key);
  // Memoised: the overlay hook re-syncs from the URL whenever `items` changes identity, so an
  // inline array rebuilt every render would re-open the drawer on each state change inside it.
  const items = useMemo(
    () => [
      {
        id: "1",
        eyebrow: t("crumb"),
        title: t("configure.title"),
        icon: <Settings2 className="ic" aria-hidden="true" />,
        body: rules ? (
          <div data-testid="alerts-configure-drawer">
            <p className="small muted" style={{ marginTop: 0 }}>
              {t("configure.intro")}
            </p>
            <h4 className="alerts-cfg-h">{t("configure.builtin.title")}</h4>
            {rules.map((rule) => (
              <RuleRow key={rule.key} rule={rule} pageContract={pageContract} />
            ))}
            <EventRules pageContract={pageContract} initialRules={eventRules} kinds={eventKinds} />
            <p className="small muted" style={{ marginTop: 14 }}>
              {t("configure.more_rules")}
            </p>
          </div>
        ) : (
          <div className="small muted">{t("configure.unavailable")}</div>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps -- t is derived from pageContract
    [pageContract, rules, eventRules, eventKinds],
  );
  return <LocalOverlayDrawer items={items} selectionKey={PARAM_CONFIGURE} initialSelectedId={initialOpen ? "1" : undefined} closeHref={closeHref} ariaLabel={t("configure.title")} closeLabel={t("configure.close")} />;
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
      const result = await saveAlertRuleAction({ ruleKey: rule.key, enabled, threshold, attempt: newAttempt() });
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
    <div className="card alerts-rule" style={{ marginBottom: 10 }} data-testid="alerts-rule-row" data-rule={rule.key}>
      <div className="bd" style={{ display: "grid", gap: 8 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <FormControlLabel
            className="small"
            disabled={pending}
            control={<Checkbox checked={enabled} onChange={(event) => setEnabled(event.target.checked)} sx={{ p: { xs: 1.5, sm: 1 } }} slotProps={{ input: { "data-testid": "alerts-rule-enabled" } as React.InputHTMLAttributes<HTMLInputElement> }} />}
            label={rule.label}
          />
          <span className="sp" style={{ flex: 1 }} />
          <span className={`tag ${enabled ? "t-ok" : "t-mut"}`}>{enabled ? t("configure.enabled") : t("configure.disabled")}</span>
        </div>
        <div className="small muted">{rule.description}</div>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <label className="small" htmlFor={inputId}>
            {rule.threshold_label}
          </label>
          <TextField
            id={inputId}
            type="number"
            size="small"
            value={threshold}
            disabled={pending}
            onChange={(event) => setThreshold(event.target.value)}
            sx={{ width: 96 }}
            slotProps={{
              htmlInput: {
                inputMode: "numeric",
                min: rule.min_threshold,
                max: rule.max_threshold,
                step: 1,
                "data-testid": "alerts-rule-threshold",
              },
            }}
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

// EventRules is the farm's own alerts: "tell me when <event> happens". Every existing rule is a
// card with its switch, severity and Remove; the form at the bottom composes a new one from the
// backend catalog (`kinds`). Rows re-render from what the server read back after each write.
function EventRules({ pageContract, initialRules, kinds }: { pageContract: AdminUiPageContract; initialRules: AlertEventRule[]; kinds: AlertEventKind[] }) {
  const t = (key: string) => copy(pageContract, key);
  const [rules, setRules] = useState(initialRules);
  return (
    <div data-testid="alerts-event-rules">
      <h4 className="alerts-cfg-h">{t("configure.events.title")}</h4>
      <p className="small muted" style={{ marginTop: 0 }}>
        {t("configure.events.intro")}
      </p>
      {rules.length === 0 ? <div className="small muted" style={{ marginBottom: 10 }}>{t("configure.events.empty")}</div> : null}
      {rules.map((rule) => (
        <EventRuleRow
          key={rule.id}
          rule={rule}
          kinds={kinds}
          pageContract={pageContract}
          onSaved={(saved) => setRules((current) => current.map((r) => (r.id === saved.id ? saved : r)))}
          onRemoved={(id) => setRules((current) => current.filter((r) => r.id !== id))}
        />
      ))}
      <NewEventRule pageContract={pageContract} kinds={kinds} onCreated={(created) => setRules((current) => [...current, created])} />
    </div>
  );
}

// newAttempt mints the nonce one save click rides on (see actions.ts idempotencyKey). The
// button is disabled while a save is pending, so a click is one attempt; a network-level retry
// of the same Server Action reuses it.
function newAttempt(): string {
  return typeof crypto !== "undefined" && "randomUUID" in crypto ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`;
}

function severityOptions(pageContract: AdminUiPageContract): { key: string; label: string }[] {
  return [
    { key: "critical", label: copy(pageContract, "severity.critical") },
    { key: "warning", label: copy(pageContract, "severity.warning") },
  ];
}

function EventRuleRow({ rule, kinds, pageContract, onSaved, onRemoved }: { rule: AlertEventRule; kinds: AlertEventKind[]; pageContract: AdminUiPageContract; onSaved: (rule: AlertEventRule) => void; onRemoved: (id: string) => void }) {
  const t = (key: string) => copy(pageContract, key);
  const [label, setLabel] = useState(rule.label);
  const [kind, setKind] = useState(rule.kind);
  const [severity, setSeverity] = useState<string>(rule.severity);
  const [enabled, setEnabled] = useState(rule.enabled);
  const [message, setMessage] = useState<{ tone: "ok" | "dng"; text: string } | null>(null);
  const [pending, startTransition] = useTransition();
  const dirty = label.trim() !== rule.label || kind !== rule.kind || severity !== rule.severity || enabled !== rule.enabled;
  const save = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await saveAlertEventRuleAction({ id: rule.id, label, kind, severity, enabled, attempt: newAttempt() });
      if (result.ok) {
        onSaved(result.rule);
        setMessage({ tone: "ok", text: t("action.config_saved") });
      } else setMessage({ tone: "dng", text: t(`action.${result.code}`) });
    });
  };
  const remove = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await deleteAlertEventRuleAction({ id: rule.id });
      if (result.ok) onRemoved(rule.id);
      else setMessage({ tone: "dng", text: t(`action.${result.code}`) });
    });
  };
  return (
    <div className="card alerts-rule" style={{ marginBottom: 10 }} data-testid="alerts-event-rule" data-rule-id={rule.id}>
      <div className="bd" style={{ display: "grid", gap: 8 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
          <FormControlLabel
            className="small"
            disabled={pending}
            control={<Checkbox checked={enabled} onChange={(e) => setEnabled(e.target.checked)} sx={{ p: { xs: 1.5, sm: 1 } }} slotProps={{ input: { "data-testid": "alerts-event-enabled" } as React.InputHTMLAttributes<HTMLInputElement> }} />}
            label={<TextField size="small" value={label} disabled={pending} onChange={(e) => setLabel(e.target.value)} sx={{ flex: 1 }} slotProps={{ htmlInput: { maxLength: 80, "aria-label": t("configure.events.label"), "data-testid": "alerts-event-label" } }} />}
          />
          <span className={`tag ${enabled ? "t-ok" : "t-mut"}`}>{enabled ? t("configure.enabled") : t("configure.disabled")}</span>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <span className="small">{t("configure.events.when")}</span>
          <div data-testid="alerts-event-kind">
            <TextField
              select
              label={t("configure.events.kind")}
              value={kind}
              disabled={pending}
              onChange={(event) => setKind(event.target.value)}
              sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              {kinds.map((k) => (
                <MenuItem key={k.key} value={k.key}>
                  {k.label}
                </MenuItem>
              ))}
            </TextField>
          </div>
          <div data-testid="alerts-event-severity">
            <TextField
              select
              label={t("filter.severity")}
              value={severity}
              disabled={pending}
              onChange={(event) => setSeverity(event.target.value)}
              sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              {severityOptions(pageContract).map((o) => (
                <MenuItem key={o.key} value={o.key}>
                  {o.label}
                </MenuItem>
              ))}
            </TextField>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn sm" disabled={pending} onClick={remove} data-testid="alerts-event-remove">
            {t("configure.events.remove")}
          </button>
          <button type="button" className="btn primary sm" disabled={pending || !dirty} onClick={save} data-testid="alerts-event-save">
            {t("configure.save")}
          </button>
        </div>
        <div className="small muted">{kinds.find((k) => k.key === kind)?.description ?? ""}</div>
        {rule.updated_at ? (
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

function NewEventRule({ pageContract, kinds, onCreated }: { pageContract: AdminUiPageContract; kinds: AlertEventKind[]; onCreated: (rule: AlertEventRule) => void }) {
  const t = (key: string) => copy(pageContract, key);
  const [label, setLabel] = useState("");
  const [kind, setKind] = useState(kinds[0]?.key ?? "");
  const [severity, setSeverity] = useState("warning");
  const [message, setMessage] = useState<{ tone: "ok" | "dng"; text: string } | null>(null);
  const [pending, startTransition] = useTransition();
  const add = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await saveAlertEventRuleAction({ label, kind, severity, enabled: true, attempt: newAttempt() });
      if (result.ok) {
        onCreated(result.rule);
        setLabel("");
        setMessage({ tone: "ok", text: t("action.config_event_added") });
      } else setMessage({ tone: "dng", text: t(`action.${result.code}`) });
    });
  };
  return (
    <div className="card alerts-rule" style={{ marginBottom: 10, borderStyle: "dashed" }} data-testid="alerts-event-new">
      <div className="bd" style={{ display: "grid", gap: 8 }}>
        <b className="small">{t("configure.events.add")}</b>
        <TextField size="small" fullWidth value={label} placeholder={t("configure.events.label.placeholder")} disabled={pending} onChange={(e) => setLabel(e.target.value)} slotProps={{ htmlInput: { maxLength: 80, "aria-label": t("configure.events.label"), "data-testid": "alerts-event-new-label" } }} />
        <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
          <span className="small">{t("configure.events.when")}</span>
          <div data-testid="alerts-event-new-kind">
            <TextField
              select
              label={t("configure.events.kind")}
              value={kind}
              disabled={pending}
              onChange={(event) => setKind(event.target.value)}
              sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              {kinds.map((k) => (
                <MenuItem key={k.key} value={k.key}>
                  {k.label}
                </MenuItem>
              ))}
            </TextField>
          </div>
          <div data-testid="alerts-event-new-severity">
            <TextField
              select
              label={t("filter.severity")}
              value={severity}
              disabled={pending}
              onChange={(event) => setSeverity(event.target.value)}
              sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
              slotProps={{ inputLabel: { shrink: true }, select: { displayEmpty: true, MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } } } }}
            >
              {severityOptions(pageContract).map((o) => (
                <MenuItem key={o.key} value={o.key}>
                  {o.label}
                </MenuItem>
              ))}
            </TextField>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <button type="button" className="btn primary sm" disabled={pending || !label.trim() || !kind} onClick={add} data-testid="alerts-event-new-add">
            {t("configure.events.add_button")}
          </button>
        </div>
        <div className="small muted">{kinds.find((k) => k.key === kind)?.description ?? ""}</div>
        {message ? (
          <div className="small" role="status" style={{ color: message.tone === "dng" ? "var(--danger)" : "var(--ok)" }}>
            {message.text}
          </div>
        ) : null}
      </div>
    </div>
  );
}
