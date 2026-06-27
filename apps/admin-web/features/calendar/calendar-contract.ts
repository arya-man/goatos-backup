// Calendar vaccination slice — generated-client types + presentation meta + mapping helpers.
//
// Types come straight from the generated client (`@goatos/api-client`), so the frontend can never drift
// from the backend contract. The mapping helpers below convert the generated shapes (string severity,
// nested JSONBlock detail, index-signature links) into what the presentational components render — this is
// the adapter layer the swap requires (it is NOT a 1:1 rename of the old local types).
import type { AppApiComponents } from "@goatos/api-client";
import type { Tone } from "@/components/ui-primitives";
import {
  Boxes,
  CalendarClock,
  FileCheck2,
  GitBranch,
  PackageCheck,
  ShieldAlert,
  ShieldCheck,
  Snowflake,
  Syringe,
  type LucideIcon,
} from "lucide-react";

export type CalendarEvent = AppApiComponents["schemas"]["CalendarEvent"];
export type CalendarEventDetail = AppApiComponents["schemas"]["CalendarEventDetail"];
export type CalendarEventListResponse = AppApiComponents["schemas"]["CalendarEventListResponse"];
export type CalendarHistoryItem = AppApiComponents["schemas"]["CalendarHistoryItem"];
export type CalendarStatus = AppApiComponents["schemas"]["CalendarStatus"];
export type CalendarSeverity = AppApiComponents["schemas"]["CalendarSeverity"];
export type CalendarEventType = AppApiComponents["schemas"]["CalendarEventType"];
export type CalendarOwnerKey = AppApiComponents["schemas"]["CalendarOwnerKey"];
export type CalendarOwnerFilter = AppApiComponents["schemas"]["CalendarOwnerFilter"];
export type CalendarEventLinks = AppApiComponents["schemas"]["CalendarEventLinks"];
export type CalendarJSONBlock = AppApiComponents["schemas"]["CalendarJSONBlock"];
export type CalendarPresentation = AppApiComponents["schemas"]["CalendarPresentation"];
export type CalendarPresentationTab = AppApiComponents["schemas"]["CalendarPresentationTab"];
export type CalendarOwnerPresentationTab = AppApiComponents["schemas"]["CalendarOwnerPresentationTab"];
export type CalendarRhythmDay = AppApiComponents["schemas"]["CalendarRhythmDay"];
export type CalendarPresentationQuery = AppApiComponents["schemas"]["CalendarPresentationQuery"];

// ── Owner presentation ─────────────────────────────────────────────────────────────────────────────

type FallbackOwnerConfig = {
  key: CalendarOwnerFilter;
  label: string;
  scopeLabel: string;
  color: string;
  rhythmTitle: string;
  rhythmNote: string;
  rhythmDays: CalendarRhythmDay[];
  workstreamTabs: CalendarPresentationTab[];
  weekTitle: string;
  scopeOnlyMessage: string;
};

export type OwnerPresentationMeta = {
  label: string;
  scopeLabel: string;
  color: string;
};

export type OwnerPresentationMap = Record<string, OwnerPresentationMeta>;

const FALLBACK_OWNER_ORDER: CalendarOwnerFilter[] = ["all", "phc", "inventory", "admin_data_ops"];

const FALLBACK_OWNER_CONFIG: Record<CalendarOwnerFilter, FallbackOwnerConfig> = {
  all: {
    key: "all",
    label: "All",
    scopeLabel: "All owner lanes",
    color: "var(--brand)",
    rhythmTitle: "Vaccination operating rhythm",
    rhythmNote: "from the SOP handbook - all owner lanes",
    rhythmDays: [
      rhythmDay("Mon", "PLAN", "plan"),
      rhythmDay("Tue", "LOGISTICS", "log"),
      rhythmDay("Wed", "EXECUTE", "exec"),
      rhythmDay("Thu", "EXECUTE", "exec"),
      rhythmDay("Fri", "EXECUTE", "exec"),
      rhythmDay("Sat", "EXECUTE", "exec"),
      rhythmDay("Sun", "REST", "rest"),
    ],
    workstreamTabs: [activeTab("vaccination", "Vaccination", "Calendar is currently showing the PHC vaccination module.")],
    weekTitle: "This week",
    scopeOnlyMessage: "",
  },
  phc: {
    key: "phc",
    label: "PHC",
    scopeLabel: "PHC",
    color: "var(--brand)",
    rhythmTitle: "PHC vaccination rhythm",
    rhythmNote: "plan drives - prepare teams - execute proof",
    rhythmDays: [
      rhythmDay("Mon", "PLAN", "plan"),
      rhythmDay("Tue", "PREP", "log"),
      rhythmDay("Wed", "DRIVE", "exec"),
      rhythmDay("Thu", "DRIVE", "exec"),
      rhythmDay("Fri", "VERIFY", "exec"),
      rhythmDay("Sat", "CATCH-UP", "exec"),
      rhythmDay("Sun", "REST", "rest"),
    ],
    workstreamTabs: [activeTab("vaccination", "Vaccination", "Calendar is currently showing the PHC vaccination module.")],
    weekTitle: "PHC this week",
    scopeOnlyMessage: "Showing PHC work only.",
  },
  inventory: {
    key: "inventory",
    label: "Inventory / Stock",
    scopeLabel: "Inventory / Stock",
    color: "var(--amber)",
    rhythmTitle: "Vaccination stock readiness rhythm",
    rhythmNote: "stock, FEFO, cold-chain, reorder, GRN",
    rhythmDays: [
      rhythmDay("Mon", "COUNT", "plan"),
      rhythmDay("Tue", "FEFO", "log"),
      rhythmDay("Wed", "ISSUE", "exec"),
      rhythmDay("Thu", "MONITOR", "exec"),
      rhythmDay("Fri", "REORDER", "log"),
      rhythmDay("Sat", "CLOSE", "exec"),
      rhythmDay("Sun", "REST", "rest"),
    ],
    workstreamTabs: [
      activeTab("all_inventory_stock", "All Inventory / Stock"),
      disabledTab("stock_readiness", "Stock readiness"),
      disabledTab("cold_chain", "Cold chain"),
      disabledTab("reorder_expiry", "Reorder / expiry"),
      disabledTab("grn_fefo", "GRN / FEFO"),
    ],
    weekTitle: "Inventory / Stock this week",
    scopeOnlyMessage: "Showing Inventory / Stock work only.",
  },
  admin_data_ops: {
    key: "admin_data_ops",
    label: "Admin / Data Ops",
    scopeLabel: "Admin / Data Ops",
    color: "var(--purple)",
    rhythmTitle: "Vaccination governance rhythm",
    rhythmNote: "source review, config approval, import, audit follow-up",
    rhythmDays: [
      rhythmDay("Mon", "REVIEW", "plan"),
      rhythmDay("Tue", "CONFIG", "log"),
      rhythmDay("Wed", "IMPORT", "exec"),
      rhythmDay("Thu", "AUDIT", "exec"),
      rhythmDay("Fri", "APPROVE", "log"),
      rhythmDay("Sat", "FOLLOW-UP", "exec"),
      rhythmDay("Sun", "REST", "rest"),
    ],
    workstreamTabs: [
      activeTab("all_admin_data_ops", "All Admin / Data Ops"),
      disabledTab("source_review", "Source review"),
      disabledTab("config_approval", "Config approval"),
      disabledTab("import_replay", "Import / replay"),
      disabledTab("audit_follow_up", "Audit follow-up"),
    ],
    weekTitle: "Admin / Data Ops this week",
    scopeOnlyMessage: "Showing Admin / Data Ops work only.",
  },
};

function rhythmDay(day: string, label: string, tone: string): CalendarRhythmDay {
  return { day, label, tone, enabled: true, query: { day } };
}

function activeTab(key: string, label: string, disabledReason = ""): CalendarPresentationTab {
  return { key, label, active: true, enabled: true, disabled_reason: disabledReason, query: {} };
}

function disabledTab(key: string, label: string): CalendarPresentationTab {
  return {
    key,
    label,
    active: false,
    enabled: false,
    disabled_reason: "Sub-workstream filters need backend event_type support; this row is the module context.",
    query: { workstream_key: key },
  };
}

function fallbackOwner(ownerKey: string): FallbackOwnerConfig {
  return FALLBACK_OWNER_CONFIG[(ownerKey || "all") as CalendarOwnerFilter] ?? FALLBACK_OWNER_CONFIG.all;
}

export function fallbackCalendarPresentation(ownerKey: string): CalendarPresentation {
  const active = fallbackOwner(ownerKey);
  return {
    page_title: "Calendar",
    page_subtitle:
      "Vaccination due work by time - source-backed obligations, drives, boosters, proof/rework, defer reviews, and the stock / config tasks that gate them. Click an event for its rich detail and deep links.",
    view_tabs: [
      { key: "week", label: "Week", active: false, enabled: true, disabled_reason: "", query: { view: "" } },
      { key: "month", label: "Month", active: false, enabled: true, disabled_reason: "", query: { view: "month", day: "" } },
    ],
    owner_tabs: FALLBACK_OWNER_ORDER.map((key) => {
      const cfg = FALLBACK_OWNER_CONFIG[key];
      return {
        key,
        label: cfg.label,
        scope_label: cfg.scopeLabel,
        color: cfg.color,
        active: active.key === key,
        enabled: true,
        disabled_reason: "",
        query: { owner_key: key === "all" ? "" : key },
      };
    }),
    workstream_tabs: active.workstreamTabs,
    rhythm: { title: active.rhythmTitle, note: active.rhythmNote, days: active.rhythmDays },
    week: {
      title: active.weekTitle,
      scope_label: active.scopeLabel,
      scope_only_message: active.scopeOnlyMessage,
      clear_scope_label: "all owner lanes",
      whole_period_message: "Showing whole week.",
      all_days_selected_label: "all days selected",
      clear_day_label: "whole week",
      empty_message: "No vaccination due work",
      reminder_title: "Reminders & escalation",
      reminder_empty_message: "No reminders scheduled in this scope.",
      reminder_note: "Reminders, nudges, snoozes, and escalations are durable backend kernel state. Open an event to act.",
      as_of_hint: "",
      cell_note: "",
    },
    month: {
      title: "",
      scope_label: active.scopeLabel,
      scope_only_message: "",
      clear_scope_label: "",
      whole_period_message: "",
      all_days_selected_label: "",
      clear_day_label: "",
      empty_message: "",
      reminder_title: "",
      reminder_empty_message: "",
      reminder_note: "",
      as_of_hint: "month follows the top-bar as-of date",
      cell_note: "Each cell shows that day's source-backed vaccination due work. Tap an event for its rich detail.",
    },
    new_event: {
      label: "New event",
      enabled: false,
      disabled_reason:
        "Calendar events are generated from source-backed obligations. Create a campaign/catch-up via Config or the PHC catch-up path - not a free-form Calendar entry.",
    },
    empty_state: {
      ok_message:
        "No vaccination due work for this scope and date window. Events appear once source-backed obligations, drives, boosters, proof/rework, or the stock/config tasks that gate them are due.",
      error_message: "Calendar is unavailable - resolve the error above, then reload.",
      primary_label: "Config",
      secondary_label: "Vaccination",
    },
    event_types: Object.entries(EVENT_TYPE_META).map(([key, meta]) => ({ key, label: meta.label })),
    active_owner_key: active.key,
    active_owner_label: active.label,
    active_owner_scope_label: active.scopeLabel,
    active_owner_color: active.color,
    all_owners_selected_label: "all owner lanes",
  };
}

export function presentationQueryToSearch(query: CalendarPresentationQuery | undefined | null): Record<string, string | undefined> {
  const out: Record<string, string | undefined> = {};
  for (const [key, value] of Object.entries(query ?? {})) out[key] = value === "" ? undefined : value;
  return out;
}

export function ownerMetaFromPresentation(presentation: CalendarPresentation): OwnerPresentationMap {
  const meta: OwnerPresentationMap = {};
  for (const tab of presentation.owner_tabs) {
    meta[tab.key] = { label: tab.label, scopeLabel: tab.scope_label, color: tab.color };
  }
  if (!meta[presentation.active_owner_key]) {
    meta[presentation.active_owner_key] = {
      label: presentation.active_owner_label,
      scopeLabel: presentation.active_owner_scope_label,
      color: presentation.active_owner_color,
    };
  }
  return meta;
}

export function ownerLabel(ownerKey: string, ownerMeta: OwnerPresentationMap): string {
  return ownerMeta[ownerKey]?.label ?? fallbackOwner(ownerKey).label ?? humanize(ownerKey);
}

export function ownerScopeLabel(ownerKey: string, ownerMeta: OwnerPresentationMap): string {
  return ownerMeta[ownerKey]?.scopeLabel ?? fallbackOwner(ownerKey).scopeLabel ?? humanize(ownerKey);
}

export function ownerColor(ownerKey: string, ownerMeta: OwnerPresentationMap): string {
  return ownerMeta[ownerKey]?.color ?? fallbackOwner(ownerKey).color ?? "var(--brand)";
}

// ── Status presentation (generated CalendarStatus union — identical members, so this stays exhaustive) ─

export const STATUS_META: Record<CalendarStatus, { label: string; tone: Tone }> = {
  scheduled: { label: "Scheduled", tone: "mut" },
  due: { label: "Due", tone: "warn" },
  overdue: { label: "Overdue", tone: "dng" },
  in_progress: { label: "In progress", tone: "info" },
  proof_pending: { label: "Proof pending", tone: "warn" },
  verification_pending: { label: "Verification pending", tone: "warn" },
  rejected: { label: "Rejected", tone: "dng" },
  rework_due: { label: "Rework due", tone: "warn" },
  deferred: { label: "Deferred", tone: "pur" },
  blocked: { label: "Blocked", tone: "dng" },
  completed: { label: "Completed", tone: "ok" },
  canceled: { label: "Canceled", tone: "mut" },
};

// Backend severity is info | warning | critical (NOT the process-integrity broken/at_risk/watch/ok set).
export const SEVERITY_META: Record<CalendarSeverity, { label: string; tone: Tone }> = {
  info: { label: "Info", tone: "info" },
  warning: { label: "Warning", tone: "warn" },
  critical: { label: "Critical", tone: "dng" },
};

export function statusMeta(status: string): { label: string; tone: Tone } {
  return STATUS_META[status as CalendarStatus] ?? { label: humanize(status), tone: "mut" };
}

export function severityMeta(severity: string): { label: string; tone: Tone } {
  return SEVERITY_META[severity as CalendarSeverity] ?? { label: humanize(severity), tone: "mut" };
}

export const EVENT_TYPE_META: Record<CalendarEventType, { label: string; icon: LucideIcon }> = {
  vaccination_dose_due: { label: "Dose due", icon: Syringe },
  vaccination_drive: { label: "Shed / cohort drive", icon: Syringe },
  vaccination_campaign: { label: "Campaign / catch-up", icon: CalendarClock },
  vaccination_booster_due: { label: "Booster due", icon: Syringe },
  vaccination_defer_review: { label: "Defer / waiver review", icon: ShieldAlert },
  vaccination_evidence_review: { label: "HF / historical evidence review", icon: FileCheck2 },
  vaccination_proof_verification: { label: "Proof verification", icon: ShieldCheck },
  vaccination_rework_due: { label: "Rework due", icon: GitBranch },
  vaccine_stock_readiness: { label: "Stock readiness", icon: PackageCheck },
  vaccine_cold_chain_check: { label: "Cold-chain check", icon: Snowflake },
  vaccine_reorder_expiry_grn: { label: "Reorder / expiry / GRN", icon: Boxes },
  phc_stock_anti_misuse: { label: "Stock anti-misuse", icon: ShieldAlert },
  vaccination_config_source_approval: { label: "Config / source approval", icon: FileCheck2 },
};

export function eventTypeMeta(eventType: string, presentation?: CalendarPresentation): { label: string; icon: LucideIcon } {
  const fallback = EVENT_TYPE_META[eventType as CalendarEventType] ?? { label: humanize(eventType), icon: CalendarClock };
  const backendLabel = presentation?.event_types.find((item) => item.key === eventType)?.label;
  return { ...fallback, label: backendLabel ?? fallback.label };
}

// ── Mapping helpers ──────────────────────────────────────────────────────────────────────────────

export function humanize(key: string): string {
  if (!key) return "—";
  return key.replace(/_/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
}

// Backend reminder_state ∈ {not_scheduled, scheduled, queued, nudged, snoozed}; escalation_state ∈
// {none, pending, …}. "not_scheduled"/"none" mean nothing is queued yet — they are NOT a live reminder.
const NO_STATE = new Set(["", "none", "not_scheduled"]);
// States that represent an ACTIVE reminder/escalation a CEO should see in the rail.
const ACTIVE_REMINDER = new Set(["scheduled", "queued", "nudged", "snoozed", "sent"]);
const ACTIVE_ESCALATION = new Set(["pending", "queued", "escalated"]);

// Humanize a state for display; empty/none/not_scheduled fall back to the provided label.
export function stateLabel(state: string | null | undefined, none = "None"): string {
  if (!state || NO_STATE.has(state)) return none;
  return humanize(state);
}

// True only when an actual reminder or escalation is live (drives the reminder rail + row badge). Excludes
// not_scheduled/none so idle rows don't pollute the rail.
export function hasReminderOrEscalation(event: CalendarEvent): boolean {
  return ACTIVE_REMINDER.has(event.reminder_state) || ACTIVE_ESCALATION.has(event.escalation_state);
}

// Short badge for an agenda row: escalation wins, else an active reminder state, else nothing.
export function rowReminderBadge(event: CalendarEvent): string {
  if (ACTIVE_ESCALATION.has(event.escalation_state)) return event.escalation_state === "pending" ? "escalation" : event.escalation_state;
  if (ACTIVE_REMINDER.has(event.reminder_state)) return event.reminder_state;
  return "";
}

// Flatten one detail JSONBlock ({[k]: unknown}) into label/value rows for a metagrid. Scalars only;
// nested objects/arrays are stringified compactly. Empty blocks → no rows (card hidden).
export function blockEntries(block: CalendarJSONBlock | undefined | null): { label: string; value: string }[] {
  if (!block || typeof block !== "object") return [];
  const out: { label: string; value: string }[] = [];
  for (const [k, v] of Object.entries(block)) {
    if (v === null || v === undefined || v === "") continue;
    let value: string;
    if (typeof v === "object") {
      const compact = Array.isArray(v) ? v.join(", ") : Object.values(v as Record<string, unknown>).filter(Boolean).join(" · ");
      if (!compact) continue;
      value = compact;
    } else {
      value = String(v);
    }
    out.push({ label: humanize(k), value });
  }
  return out;
}

// The backend `links` object is { key: hrefOrFlag }. Its href values are BACKEND/mock paths
// (e.g. "/vaccination/operations", "/vaccination/workflows/<id>") that do NOT match admin-web routes, so
// we map the KEY to the real app route (built from event fields), preserving top-bar scope at the call
// site. Only confidently-mappable keys become links; unknown keys are skipped (no dead links).
export type CalendarLink = { key: string; label: string; appPath: string; tone: Tone };

// CalendarEventLinks is the generated index-signature map ({ [key]: string | boolean | null }). The index
// signature types every lookup as non-undefined, but at RUNTIME an absent key is `undefined` — so the
// undefined check is mandatory (without it every event gets fake Workflow/Action Center/Adherence links).
function linkPresent(links: CalendarEventLinks, key: string): boolean {
  const v = links[key] as string | boolean | null | undefined;
  return v !== undefined && v !== null && v !== false && v !== "";
}

export function parseLinks(event: CalendarEvent): CalendarLink[] {
  const links: CalendarEventLinks = event.links ?? {};
  const out: CalendarLink[] = [];
  if (linkPresent(links, "vaccination")) out.push({ key: "vaccination", label: "vaccination", appPath: "/vaccination", tone: "teal" });
  if (linkPresent(links, "drive") && event.shed_id) out.push({ key: "drive", label: "drive", appPath: `/vaccination/execution/sheds/${encodeURIComponent(event.shed_id)}`, tone: "teal" });
  if (linkPresent(links, "workflow")) out.push({ key: "workflow", label: "workflow record", appPath: `/workflows/${encodeURIComponent(event.event_id)}`, tone: "info" });
  if (linkPresent(links, "action_center")) out.push({ key: "action_center", label: "action center", appPath: "/action-center", tone: "info" });
  if (linkPresent(links, "adherence")) out.push({ key: "adherence", label: "adherence", appPath: "/protocol-adherence", tone: "warn" });
  if (linkPresent(links, "audit")) out.push({ key: "audit", label: "audit", appPath: "/operations/audit", tone: "mut" });
  return out;
}

// Whether the event has a drive (shed-execution) deep link — drives the "Open drive" vs "Open workflow"
// footer choice in the drawer.
export function driveShedId(event: CalendarEvent): string | null {
  const links: CalendarEventLinks = event.links ?? {};
  return linkPresent(links, "drive") && event.shed_id ? event.shed_id : null;
}

export function hasWorkflowLink(event: CalendarEvent): boolean {
  return linkPresent(event.links ?? {}, "workflow");
}
