"use client";

import { useMemo, useState, useTransition } from "react";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import TextField from "@mui/material/TextField";
import Typography from "@mui/material/Typography";

import { phoneTapSx } from "@/components/app/tap";
import { Iconify } from "@/components/minimal/iconify";
import { Label } from "@/components/minimal/label";

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
        icon: <Iconify icon="solar:settings-bold" width={18} aria-hidden="true" />,
        body: rules ? (
          <div data-testid="alerts-configure-drawer">
            <Typography variant="body2" sx={{ color: "text.secondary", mb: 1.5 }}>
              {t("configure.intro")}
            </Typography>
            <SectionTitle>{t("configure.builtin.title")}</SectionTitle>
            {rules.map((rule) => (
              <RuleRow key={rule.key} rule={rule} pageContract={pageContract} />
            ))}
            <EventRules pageContract={pageContract} initialRules={eventRules} kinds={eventKinds} />
            <Typography variant="body2" sx={{ color: "text.secondary", mt: 2 }}>
              {t("configure.more_rules")}
            </Typography>
          </div>
        ) : (
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {t("configure.unavailable")}
          </Typography>
        ),
      },
    ],
    // eslint-disable-next-line react-hooks/exhaustive-deps -- t is derived from pageContract
    [pageContract, rules, eventRules, eventKinds],
  );
  return (
    <LocalOverlayDrawer
      items={items}
      selectionKey={PARAM_CONFIGURE}
      initialSelectedId={initialOpen ? "1" : undefined}
      closeHref={closeHref}
      ariaLabel={t("configure.title")}
      closeLabel={t("configure.close")}
    />
  );
}

function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="subtitle2" component="h4" sx={{ mt: 2, mb: 1 }}>
      {children}
    </Typography>
  );
}

function Secondary({ children }: { children: React.ReactNode }) {
  return (
    <Typography variant="body2" sx={{ color: "text.secondary" }}>
      {children}
    </Typography>
  );
}

function StatusMessage({ message }: { message: { tone: "ok" | "dng"; text: string } | null }) {
  if (!message) return null;
  return (
    <Typography variant="body2" role="status" sx={{ color: message.tone === "dng" ? "error.main" : "success.main" }}>
      {message.text}
    </Typography>
  );
}

function EnabledLabel({ enabled, pageContract }: { enabled: boolean; pageContract: AdminUiPageContract }) {
  return (
    <Label variant="soft" color={enabled ? "success" : "default"} sx={{ flexShrink: 0 }}>
      {enabled ? copy(pageContract, "configure.enabled") : copy(pageContract, "configure.disabled")}
    </Label>
  );
}

const ruleCardSx = { p: 2, mb: 1.5, display: "grid", gap: 1 } as const;
const actionRowSx = {
  display: "flex",
  alignItems: "center",
  gap: 1,
  flexWrap: "wrap",
} as const;

function RuleRow({ rule: initial, pageContract }: { rule: AlertRuleConfig; pageContract: AdminUiPageContract }) {
  const t = (key: string) => copy(pageContract, key);
  const [rule, setRule] = useState(initial);
  const [enabled, setEnabled] = useState(initial.enabled);
  const [threshold, setThreshold] = useState(String(initial.threshold));
  const [message, setMessage] = useState<{
    tone: "ok" | "dng";
    text: string;
  } | null>(null);
  const [pending, startTransition] = useTransition();
  const dirty = enabled !== rule.enabled || threshold.trim() !== String(rule.threshold);
  const inputId = `alert-rule-${rule.key}-threshold`;

  const onSave = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await saveAlertRuleAction({
        ruleKey: rule.key,
        enabled,
        threshold,
        attempt: newAttempt(),
      });
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
    <Card variant="outlined" sx={ruleCardSx} data-testid="alerts-rule-row" data-rule={rule.key}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.25 }}>
        <FormControlLabel
          disabled={pending}
          control={
            <Checkbox
              checked={enabled}
              onChange={(event) => setEnabled(event.target.checked)}
              sx={{ p: { xs: 1.5, sm: 1 } }}
              slotProps={{
                input: {
                  "data-testid": "alerts-rule-enabled",
                } as React.InputHTMLAttributes<HTMLInputElement>,
              }}
            />
          }
          label={rule.label}
          slotProps={{ typography: { variant: "body2" } }}
          sx={{ flex: 1, mr: 0 }}
        />
        <EnabledLabel enabled={enabled} pageContract={pageContract} />
      </Box>
      <Secondary>{rule.description}</Secondary>
      <Box sx={actionRowSx}>
        <Typography variant="body2" component="label" htmlFor={inputId}>
          {rule.threshold_label}
        </Typography>
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
        <Secondary>{rule.threshold_unit}</Secondary>
        <Secondary>
          · {t("configure.default")} {rule.default_threshold}
        </Secondary>
        <Box sx={{ flex: 1 }} />
        <Button variant="contained" size="small" disabled={pending || !dirty} onClick={onSave} data-testid="alerts-rule-save" sx={phoneTapSx}>
          {t("configure.save")}
        </Button>
      </Box>
      {rule.stored && rule.updated_at ? (
        <Secondary>
          {t("configure.updated_by")} {rule.updated_by || "—"} · {rule.updated_at}
        </Secondary>
      ) : null}
      <StatusMessage message={message} />
    </Card>
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
      <SectionTitle>{t("configure.events.title")}</SectionTitle>
      <Typography variant="body2" sx={{ color: "text.secondary", mb: 1.5 }}>
        {t("configure.events.intro")}
      </Typography>
      {rules.length === 0 ? (
        <Typography variant="body2" sx={{ color: "text.secondary", mb: 1.5 }}>
          {t("configure.events.empty")}
        </Typography>
      ) : null}
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

function EventRuleRow({
  rule,
  kinds,
  pageContract,
  onSaved,
  onRemoved,
}: {
  rule: AlertEventRule;
  kinds: AlertEventKind[];
  pageContract: AdminUiPageContract;
  onSaved: (rule: AlertEventRule) => void;
  onRemoved: (id: string) => void;
}) {
  const t = (key: string) => copy(pageContract, key);
  const [label, setLabel] = useState(rule.label);
  const [kind, setKind] = useState(rule.kind);
  const [severity, setSeverity] = useState<string>(rule.severity);
  const [enabled, setEnabled] = useState(rule.enabled);
  const [message, setMessage] = useState<{
    tone: "ok" | "dng";
    text: string;
  } | null>(null);
  const [pending, startTransition] = useTransition();
  const dirty = label.trim() !== rule.label || kind !== rule.kind || severity !== rule.severity || enabled !== rule.enabled;
  const save = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await saveAlertEventRuleAction({
        id: rule.id,
        label,
        kind,
        severity,
        enabled,
        attempt: newAttempt(),
      });
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
    <Card variant="outlined" sx={ruleCardSx} data-testid="alerts-event-rule" data-rule-id={rule.id}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1.25 }}>
        <FormControlLabel
          disabled={pending}
          sx={{ flex: 1, mr: 0, "& .MuiFormControlLabel-label": { flex: 1 } }}
          control={
            <Checkbox
              checked={enabled}
              onChange={(e) => setEnabled(e.target.checked)}
              sx={{ p: { xs: 1.5, sm: 1 } }}
              slotProps={{
                input: {
                  "data-testid": "alerts-event-enabled",
                } as React.InputHTMLAttributes<HTMLInputElement>,
              }}
            />
          }
          label={
            <TextField
              size="small"
              value={label}
              disabled={pending}
              onChange={(e) => setLabel(e.target.value)}
              sx={{ flex: 1 }}
              slotProps={{
                htmlInput: {
                  maxLength: 80,
                  "aria-label": t("configure.events.label"),
                  "data-testid": "alerts-event-label",
                },
              }}
            />
          }
        />
        <EnabledLabel enabled={enabled} pageContract={pageContract} />
      </Box>
      <Box sx={actionRowSx}>
        <Typography variant="body2">{t("configure.events.when")}</Typography>
        <div data-testid="alerts-event-kind">
          <TextField
            select
            label={t("configure.events.kind")}
            value={kind}
            disabled={pending}
            onChange={(event) => setKind(event.target.value)}
            sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
            slotProps={{
              inputLabel: { shrink: true },
              select: {
                displayEmpty: true,
                MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } },
              },
            }}
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
            slotProps={{
              inputLabel: { shrink: true },
              select: {
                displayEmpty: true,
                MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } },
              },
            }}
          >
            {severityOptions(pageContract).map((o) => (
              <MenuItem key={o.key} value={o.key}>
                {o.label}
              </MenuItem>
            ))}
          </TextField>
        </div>
        <Box sx={{ flex: 1 }} />
        <Button variant="outlined" color="inherit" size="small" disabled={pending} onClick={remove} data-testid="alerts-event-remove" sx={phoneTapSx}>
          {t("configure.events.remove")}
        </Button>
        <Button variant="contained" size="small" disabled={pending || !dirty} onClick={save} data-testid="alerts-event-save" sx={phoneTapSx}>
          {t("configure.save")}
        </Button>
      </Box>
      <Secondary>{kinds.find((k) => k.key === kind)?.description ?? ""}</Secondary>
      {rule.updated_at ? (
        <Secondary>
          {t("configure.updated_by")} {rule.updated_by || "—"} · {rule.updated_at}
        </Secondary>
      ) : null}
      <StatusMessage message={message} />
    </Card>
  );
}

function NewEventRule({ pageContract, kinds, onCreated }: { pageContract: AdminUiPageContract; kinds: AlertEventKind[]; onCreated: (rule: AlertEventRule) => void }) {
  const t = (key: string) => copy(pageContract, key);
  const [label, setLabel] = useState("");
  const [kind, setKind] = useState(kinds[0]?.key ?? "");
  const [severity, setSeverity] = useState("warning");
  const [message, setMessage] = useState<{
    tone: "ok" | "dng";
    text: string;
  } | null>(null);
  const [pending, startTransition] = useTransition();
  const add = () => {
    setMessage(null);
    startTransition(async () => {
      const result = await saveAlertEventRuleAction({
        label,
        kind,
        severity,
        enabled: true,
        attempt: newAttempt(),
      });
      if (result.ok) {
        onCreated(result.rule);
        setLabel("");
        setMessage({ tone: "ok", text: t("action.config_event_added") });
      } else setMessage({ tone: "dng", text: t(`action.${result.code}`) });
    });
  };
  return (
    <Card variant="outlined" sx={{ ...ruleCardSx, borderStyle: "dashed" }} data-testid="alerts-event-new">
      <Typography variant="subtitle2">{t("configure.events.add")}</Typography>
      <TextField
        size="small"
        fullWidth
        value={label}
        placeholder={t("configure.events.label.placeholder")}
        disabled={pending}
        onChange={(e) => setLabel(e.target.value)}
        slotProps={{
          htmlInput: {
            maxLength: 80,
            "aria-label": t("configure.events.label"),
            "data-testid": "alerts-event-new-label",
          },
        }}
      />
      <Box sx={actionRowSx}>
        <Typography variant="body2">{t("configure.events.when")}</Typography>
        <div data-testid="alerts-event-new-kind">
          <TextField
            select
            label={t("configure.events.kind")}
            value={kind}
            disabled={pending}
            onChange={(event) => setKind(event.target.value)}
            sx={{ minWidth: { xs: 0, sm: 150 }, flexShrink: 0, maxWidth: 1 }}
            slotProps={{
              inputLabel: { shrink: true },
              select: {
                displayEmpty: true,
                MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } },
              },
            }}
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
            slotProps={{
              inputLabel: { shrink: true },
              select: {
                displayEmpty: true,
                MenuProps: { slotProps: { paper: { sx: { maxHeight: 300 } } } },
              },
            }}
          >
            {severityOptions(pageContract).map((o) => (
              <MenuItem key={o.key} value={o.key}>
                {o.label}
              </MenuItem>
            ))}
          </TextField>
        </div>
        <Box sx={{ flex: 1 }} />
        <Button variant="contained" size="small" disabled={pending || !label.trim() || !kind} onClick={add} data-testid="alerts-event-new-add" sx={phoneTapSx}>
          {t("configure.events.add_button")}
        </Button>
      </Box>
      <Secondary>{kinds.find((k) => k.key === kind)?.description ?? ""}</Secondary>
      <StatusMessage message={message} />
    </Card>
  );
}
