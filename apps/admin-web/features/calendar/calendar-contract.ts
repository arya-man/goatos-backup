// Calendar vaccination slice — generated-client types + presentation meta + mapping helpers.
//
// Types come straight from the generated client (`@goatos/api-client`), so the frontend can never drift
// from the backend contract. The mapping helpers below convert the generated shapes (string severity,
// nested JSONBlock detail, index-signature links) into what the presentational components render — this is
// the adapter layer the swap requires (it is NOT a 1:1 rename of the old local types).
import type { AppApiComponents } from "@goatos/api-client";
import type { Tone } from "@/components/ui-primitives";
import { copy, optionGroup, optionLabel, optionTone, type AdminUiPageContract } from "@/lib/admin-ui-contract";
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

export type CalendarDriveTarget = AppApiComponents["schemas"]["CalendarDriveTarget"];
export type CalendarDriveTargetListResponse = AppApiComponents["schemas"]["CalendarDriveTargetListResponse"];
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
  color: string;
};

export type OwnerPresentationMeta = {
  label: string;
  scopeLabel: string;
  color: string;
};

export type OwnerPresentationMap = Record<string, OwnerPresentationMeta>;

const FALLBACK_OWNER_CONFIG: Record<CalendarOwnerFilter, FallbackOwnerConfig> = {
  all: {
    key: "all",
    color: "var(--brand)",
  },
  pc: {
    key: "pc",
    color: "var(--brand)",
  },
  inventory: {
    key: "inventory",
    color: "var(--amber)",
  },
  admin_data_ops: {
    key: "admin_data_ops",
    color: "var(--purple)",
  },
};

function fallbackOwner(ownerKey: string): FallbackOwnerConfig {
  return FALLBACK_OWNER_CONFIG[(ownerKey || "all") as CalendarOwnerFilter] ?? FALLBACK_OWNER_CONFIG.all;
}

function calendarViewQuery(key: string): CalendarPresentationQuery {
  if (key === "month") return { view: "month", day: "" };
  return { view: "" };
}

function calendarWorkstreamQuery(key: string, index: number): CalendarPresentationQuery {
  if (index === 0) return {};
  return { workstream_key: key };
}

export function fallbackCalendarPresentation(pageContract: AdminUiPageContract, ownerKey: string): CalendarPresentation {
  const active = fallbackOwner(ownerKey);
  const ownerTabs = optionGroup(pageContract, "calendar_owner_tabs");
  const activeOwner = ownerTabs.find((tab) => tab.key === active.key) ?? ownerTabs[0];
  const activeOwnerKey = (activeOwner?.key ?? "all") as CalendarOwnerFilter;
  const activeOwnerLabel = activeOwner?.label ?? activeOwnerKey;
  const activeOwnerScopeLabel = activeOwner?.title || activeOwnerLabel;
  const viewTabs = optionGroup(pageContract, "calendar_view_tabs");
  const workstreamTabs = optionGroup(pageContract, `calendar_workstream_tabs_${activeOwnerKey}`);
  const rhythmDays = optionGroup(pageContract, `calendar_rhythm_days_${activeOwnerKey}`);
  const eventTypes = optionGroup(pageContract, "calendar_event_types");
  return {
    page_title: pageContract.title,
    page_subtitle: pageContract.subtitle,
    view_tabs: viewTabs.map((tab) => ({
      key: tab.key,
      label: tab.label,
      active: false,
      enabled: true,
      disabled_reason: "",
      query: calendarViewQuery(tab.key),
    })),
    owner_tabs: ownerTabs.map((tab) => {
      const cfg = fallbackOwner(tab.key);
      return {
        key: tab.key,
        label: tab.label,
        scope_label: tab.title || tab.label,
        color: cfg.color,
        active: activeOwnerKey === tab.key,
        enabled: true,
        disabled_reason: "",
        query: { owner_key: tab.key === "all" ? "" : tab.key },
      };
    }),
    workstream_tabs: workstreamTabs.map((tab, index) => ({
      key: tab.key,
      label: tab.label,
      active: index === 0,
      enabled: index === 0,
      disabled_reason: index === 0 ? tab.title || "" : tab.title || copy(pageContract, "reason.calendar_subworkstream_disabled"),
      query: calendarWorkstreamQuery(tab.key, index),
    })),
    rhythm: {
      title: copy(pageContract, `calendar.rhythm.title.${activeOwnerKey}`),
      note: copy(pageContract, `calendar.rhythm.note.${activeOwnerKey}`),
      days: rhythmDays.map((day) => ({ day: day.key, label: day.label, tone: day.tone || "exec", enabled: true, query: { day: day.key } })),
    },
    week: {
      title: copy(pageContract, `calendar.week.title.${activeOwnerKey}`),
      scope_label: activeOwnerScopeLabel,
      scope_only_message: copy(pageContract, `calendar.week.scope_only.${activeOwnerKey}`),
      clear_scope_label: copy(pageContract, "calendar.week.clear_scope"),
      whole_period_message: copy(pageContract, "calendar.week.whole_period"),
      all_days_selected_label: copy(pageContract, "calendar.week.all_days_selected"),
      clear_day_label: copy(pageContract, "calendar.week.clear_day"),
      empty_message: copy(pageContract, "calendar.week.empty"),
      reminder_title: copy(pageContract, "calendar.week.reminder_title"),
      reminder_empty_message: copy(pageContract, "calendar.week.reminder_empty"),
      reminder_note: copy(pageContract, "calendar.week.reminder_note"),
      as_of_hint: "",
      cell_note: "",
    },
    month: {
      title: "",
      scope_label: activeOwnerScopeLabel,
      scope_only_message: "",
      clear_scope_label: "",
      whole_period_message: "",
      all_days_selected_label: "",
      clear_day_label: "",
      empty_message: "",
      reminder_title: "",
      reminder_empty_message: "",
      reminder_note: "",
      as_of_hint: copy(pageContract, "calendar.month.as_of_hint"),
      cell_note: copy(pageContract, "calendar.month.cell_note"),
    },
    new_event: {
      label: copy(pageContract, "calendar.new_event.label"),
      enabled: false,
      disabled_reason: copy(pageContract, "calendar.new_event.disabled_reason"),
    },
    empty_state: {
      ok_message: copy(pageContract, "calendar.empty.ok"),
      error_message: copy(pageContract, "calendar.empty.error"),
      primary_label: copy(pageContract, "calendar.empty.primary"),
      secondary_label: copy(pageContract, "calendar.empty.secondary"),
    },
    event_types: eventTypes.map((eventType) => ({ key: eventType.key, label: eventType.label })),
    active_owner_key: activeOwnerKey,
    active_owner_label: activeOwnerLabel,
    active_owner_scope_label: activeOwnerScopeLabel,
    active_owner_color: active.color,
    all_owners_selected_label: copy(pageContract, "calendar.week.clear_scope"),
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
  return ownerMeta[ownerKey]?.label ?? ownerKey;
}

export function ownerScopeLabel(ownerKey: string, ownerMeta: OwnerPresentationMap): string {
  return ownerMeta[ownerKey]?.scopeLabel ?? ownerKey;
}

export function ownerColor(ownerKey: string, ownerMeta: OwnerPresentationMap): string {
  return ownerMeta[ownerKey]?.color ?? fallbackOwner(ownerKey).color ?? "var(--brand)";
}

export const EVENT_TYPE_ICON: Record<CalendarEventType, LucideIcon> = {
  vaccination_dose_due: Syringe,
  vaccination_drive: Syringe,
  vaccination_campaign: CalendarClock,
  vaccination_booster_due: Syringe,
  vaccination_defer_review: ShieldAlert,
  vaccination_evidence_review: FileCheck2,
  vaccination_proof_verification: ShieldCheck,
  vaccination_rework_due: GitBranch,
  vaccine_stock_readiness: PackageCheck,
  vaccine_cold_chain_check: Snowflake,
  vaccine_reorder_expiry_grn: Boxes,
  pc_stock_anti_misuse: ShieldAlert,
  vaccination_config_activation_review: FileCheck2,
};

export function eventTypeMeta(eventType: string, presentation?: CalendarPresentation): { label: string; icon: LucideIcon } {
  const fallback = { label: eventType, icon: EVENT_TYPE_ICON[eventType as CalendarEventType] ?? CalendarClock };
  const backendLabel = presentation?.event_types.find((item) => item.key === eventType)?.label;
  return { ...fallback, label: backendLabel ?? fallback.label };
}

// ── Mapping helpers ──────────────────────────────────────────────────────────────────────────────

// Backend reminder_state includes escalation side effects; escalation_state carries finite level-aware
// projection states. "not_scheduled"/"none" mean nothing is queued yet - they are NOT a live reminder.
// States that represent an ACTIVE reminder/escalation a CEO should see in the rail.
const ESCALATION_LEVEL_STATES = [
  "level_1_open",
  "level_2_open",
  "level_3_open",
  "level_4_open",
  "level_1_acknowledged",
  "level_2_acknowledged",
  "level_3_acknowledged",
  "level_4_acknowledged",
];
const ACTIVE_REMINDER = new Set(["scheduled", "queued", "nudged", "snoozed", "sent", "escalated"]);
const ACTIVE_ESCALATION = new Set(["pending", "queued", "escalated", "acknowledged", ...ESCALATION_LEVEL_STATES]);

export function activeReminderState(event: CalendarEvent): string {
  return ACTIVE_REMINDER.has(event.reminder_state) ? event.reminder_state : "";
}

export function activeEscalationState(event: CalendarEvent): string {
  return ACTIVE_ESCALATION.has(event.escalation_state) ? event.escalation_state : "";
}

// True only when an actual reminder or escalation is live (drives the reminder rail + row badge). Excludes
// not_scheduled/none so idle rows don't pollute the rail.
export function hasReminderOrEscalation(event: CalendarEvent): boolean {
  return Boolean(activeReminderState(event) || activeEscalationState(event));
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
    out.push({ label: k, value });
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

export function parseLinks(event: CalendarEvent, pageContract: AdminUiPageContract): CalendarLink[] {
  const links: CalendarEventLinks = event.links ?? {};
  const out: CalendarLink[] = [];
  const link = (key: string, appPath: string) => ({
    key,
    label: optionLabel(pageContract, "calendar_links", key),
    appPath,
    tone: optionTone(pageContract, "calendar_links", key) as Tone,
  });
  if (linkPresent(links, "vaccination")) out.push(link("vaccination", "/vaccination"));
  if (linkPresent(links, "drive") && event.shed_id) out.push(link("drive", `/vaccination/execution/sheds/${encodeURIComponent(event.shed_id)}`));
  if (linkPresent(links, "workflow")) out.push(link("workflow", `/workflows/${encodeURIComponent(event.event_id)}`));
  if (linkPresent(links, "action_center")) out.push(link("action_center", "/action-center"));
  if (linkPresent(links, "adherence")) out.push(link("adherence", "/protocol-adherence"));
  if (linkPresent(links, "audit")) out.push(link("audit", "/operations/audit"));
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
