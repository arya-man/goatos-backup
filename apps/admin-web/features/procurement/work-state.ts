// Presentation maps for the procurement source-entry read models (Source Entry Board, Load Detail, and
// the Action Center / Adherence / Control Tower / Workflows lenses). These mirror the generated admin-api
// enums exactly. Tone + label defined once so every procurement screen renders states identically.
//
// Business-rule helpers live here too: source warmup is purpose-specific. Breeding stock has a long
// 45-70 day warmup; fattening/non-breeding can legitimately move in a 0-14 day window. Only
// accepted-intake goats may flow to Preventive Care (PC) / Parks — every rejected, source-only, or unresolved goat stays
// procurement history. UI must reflect that boundary, not leak it.
import type { Tone } from "@/components/ui-primitives";
import type {
  ProcurementArrivalState,
  ProcurementGoatState,
  ProcurementHealthState,
  ProcurementLoadStatus,
  ProcurementOwnershipState,
  ProcurementSelectionState,
  ProcurementSeverity,
  ProcurementWorkState,
} from "@/lib/api/procurement";

export interface Meta {
  label: string;
  tone: Tone;
}

// ---- Work state (lens rows) ----
export const PROC_WORK_STATE_META: Record<ProcurementWorkState, Meta> = {
  due: { label: "Due", tone: "warn" },
  overdue: { label: "Overdue", tone: "dng" },
  proof_pending: { label: "Proof pending", tone: "warn" },
  deferred: { label: "Deferred", tone: "mut" },
  blocked: { label: "Blocked", tone: "dng" },
  owner_missing: { label: "Owner missing", tone: "dng" },
  rejected: { label: "Rejected", tone: "dng" },
  completed: { label: "Completed", tone: "ok" },
};

// Board columns / quick-filter chips, most-broken first.
export const PROC_WORK_STATE_ORDER: ProcurementWorkState[] = [
  "overdue",
  "blocked",
  "owner_missing",
  "rejected",
  "proof_pending",
  "due",
  "deferred",
  "completed",
];

// ---- Severity (5 levels — adds "critical" above the vaccination model) ----
export const PROC_SEVERITY_META: Record<ProcurementSeverity, Meta> = {
  ok: { label: "OK", tone: "ok" },
  watch: { label: "Watch", tone: "info" },
  at_risk: { label: "At risk", tone: "warn" },
  critical: { label: "Critical", tone: "dng" },
  broken: { label: "Broken", tone: "dng" },
};

export const PROC_SEVERITY_ORDER: ProcurementSeverity[] = ["broken", "critical", "at_risk", "watch", "ok"];
export const PROC_SEVERITY_RANK: Record<ProcurementSeverity, number> = {
  ok: 0,
  watch: 1,
  at_risk: 2,
  critical: 3,
  broken: 4,
};

// ---- Load status (Source Entry Board grouping) ----
export const PROC_LOAD_STATUS_META: Record<ProcurementLoadStatus, Meta> = {
  source_warmup: { label: "Source warmup", tone: "info" },
  health_pending: { label: "Health pending", tone: "warn" },
  pre_dispatch_pending: { label: "Pre-dispatch pending", tone: "warn" },
  dispatch_ready: { label: "Dispatch ready", tone: "teal" },
  in_transit: { label: "In transit", tone: "info" },
  arrival_review: { label: "Arrival review", tone: "pur" },
  accepted_intake: { label: "Accepted intake", tone: "ok" },
  rejected: { label: "Rejected", tone: "dng" },
  deferred: { label: "Deferred", tone: "mut" },
  blocked: { label: "Blocked", tone: "dng" },
  canceled: { label: "Canceled", tone: "mut" },
};

// Source Entry Board column order — source side first, intake last, terminal states trailing.
export const PROC_LOAD_STATUS_ORDER: ProcurementLoadStatus[] = [
  "source_warmup",
  "health_pending",
  "pre_dispatch_pending",
  "dispatch_ready",
  "in_transit",
  "arrival_review",
  "accepted_intake",
  "deferred",
  "blocked",
  "rejected",
  "canceled",
];

// ---- Per-goat states ----
export const PROC_SELECTION_META: Record<ProcurementSelectionState, Meta> = {
  source_only: { label: "Source only", tone: "mut" },
  candidate: { label: "Candidate", tone: "info" },
  purchased: { label: "Purchased", tone: "teal" },
  accepted: { label: "Accepted", tone: "ok" },
  rejected: { label: "Rejected", tone: "dng" },
  deferred: { label: "Deferred", tone: "mut" },
  blocked: { label: "Blocked", tone: "dng" },
  loaded: { label: "Loaded", tone: "info" },
  arrival_accepted: { label: "Arrival accepted", tone: "ok" },
  arrival_rejected: { label: "Arrival rejected", tone: "dng" },
  accepted_herd_intake: { label: "Accepted intake", tone: "ok" },
  dead: { label: "Dead", tone: "dng" },
  sold: { label: "Sold", tone: "mut" },
  lost: { label: "Lost", tone: "dng" },
};

export const PROC_GOAT_STATE_META: Record<ProcurementGoatState, Meta> = {
  source_holding: { label: "Source holding", tone: "mut" },
  source_warmup: { label: "Source warmup", tone: "info" },
  source_candidate: { label: "Source candidate", tone: "info" },
  source_health_pending: { label: "Health pending", tone: "warn" },
  source_health_passed: { label: "Health passed", tone: "ok" },
  source_health_failed: { label: "Health failed", tone: "dng" },
  source_rejected: { label: "Source rejected", tone: "dng" },
  pre_dispatch_pending: { label: "Pre-dispatch pending", tone: "warn" },
  pre_dispatch_accepted: { label: "Accepted for truck", tone: "ok" },
  pre_dispatch_rejected: { label: "Rejected before truck", tone: "dng" },
  pre_dispatch_deferred: { label: "Pre-dispatch deferred", tone: "mut" },
  pre_dispatch_blocked: { label: "Pre-dispatch blocked", tone: "dng" },
  dispatch_ready: { label: "Dispatch ready", tone: "teal" },
  loading_pending: { label: "Loading pending", tone: "warn" },
  loaded: { label: "Loaded", tone: "info" },
  in_transit: { label: "In transit", tone: "info" },
  arrival_review_pending: { label: "Arrival review", tone: "pur" },
  arrival_accepted: { label: "Arrival accepted", tone: "ok" },
  arrival_rejected: { label: "Arrival rejected", tone: "dng" },
  accepted_herd_intake: { label: "Accepted intake", tone: "ok" },
  dead: { label: "Dead", tone: "dng" },
  sold: { label: "Sold", tone: "mut" },
  lost: { label: "Lost", tone: "dng" },
  canceled: { label: "Canceled", tone: "mut" },
};

export const PROC_OWNERSHIP_META: Record<ProcurementOwnershipState, Meta> = {
  pending: { label: "Ownership pending", tone: "warn" },
  shared_pending: { label: "Shared / pending", tone: "warn" },
  mesha_owned: { label: "Mesha owned", tone: "ok" },
  blocked: { label: "Ownership blocked", tone: "dng" },
  not_owned: { label: "Not owned", tone: "mut" },
  settled: { label: "Settled", tone: "ok" },
};

export const PROC_HEALTH_META: Record<ProcurementHealthState, Meta> = {
  pending: { label: "Health pending", tone: "warn" },
  passed: { label: "Health passed", tone: "ok" },
  failed: { label: "Health failed", tone: "dng" },
  deferred: { label: "Health deferred", tone: "mut" },
};

export const PROC_ARRIVAL_META: Record<ProcurementArrivalState, Meta> = {
  matched: { label: "Matched", tone: "ok" },
  missing: { label: "Missing", tone: "dng" },
  extra_unresolved: { label: "Extra / unresolved", tone: "dng" },
  health_flag: { label: "Health flag", tone: "warn" },
  weight_flag: { label: "Weight flag", tone: "warn" },
  accepted: { label: "Accepted", tone: "ok" },
  rejected: { label: "Rejected", tone: "dng" },
  deferred: { label: "Deferred", tone: "mut" },
  blocked: { label: "Blocked", tone: "dng" },
};

export type IdentityReviewState = "pending" | "clean" | "conflict" | "unknown_extra";
export const PROC_IDENTITY_META: Record<IdentityReviewState, Meta> = {
  pending: { label: "Identity: pending", tone: "warn" },
  clean: { label: "Identity: clean", tone: "ok" },
  conflict: { label: "Identity: conflict", tone: "dng" },
  unknown_extra: { label: "Identity: unknown/extra", tone: "dng" },
};

// Column swatch color per tone — mirrors the mock's per-status swatch.
export const TONE_SWATCH: Record<Tone, string> = {
  ok: "var(--brand)",
  warn: "var(--amber)",
  dng: "var(--danger)",
  info: "var(--info)",
  pur: "var(--purple)",
  teal: "var(--teal)",
  mut: "var(--line2)",
};

export function warmupExpectation(purpose: string | null | undefined): { label: string; maxDays: number; note: string } {
  if (purpose === "fattening" || purpose === "non_breeding") {
    return {
      label: "0-14d",
      maxDays: 14,
      note: "fattening / non-breeding warmup can be same-day to about two weeks",
    };
  }
  if (purpose === "breeding") {
    return {
      label: "45-70d",
      maxDays: 70,
      note: "breeding stock warmup is expected to be 45-70 days",
    };
  }
  return {
    label: "set purpose",
    maxDays: 70,
    note: "set purpose to classify the warmup window; fallback is the breeding-safe 70 day ceiling",
  };
}

// Source warmup classification. Long breeding warmup is valid and must not look anomalous; short
// fattening/non-breeding warmup is also valid. Beyond the purpose window is a watch, not a hard error.
export function warmupMeta(
  days: number | null | undefined,
  purpose?: string | null,
): { label: string; tone: Tone; note?: string; expectation: string } {
  const expected = warmupExpectation(purpose);
  if (days === null || days === undefined) return { label: "—", tone: "mut", note: expected.note, expectation: expected.label };
  const label = `${days}d`;
  if (days > expected.maxDays) {
    return {
      label,
      tone: "warn",
      note: `outside ${expected.label} purpose window — still valid, review before dispatch`,
      expectation: expected.label,
    };
  }
  if (purpose === "breeding" && days < 45) {
    return { label, tone: "info", note: expected.note, expectation: expected.label };
  }
  return { label, tone: "ok", note: expected.note, expectation: expected.label };
}

// Goats whose journey ended before accepted intake. These remain procurement history/work and must NEVER
// be presented as active Preventive Care (PC) vaccination work.
const PROCUREMENT_HISTORY_GOAT_STATES = new Set<ProcurementGoatState>([
  "source_rejected",
  "pre_dispatch_rejected",
  "pre_dispatch_blocked",
  "arrival_rejected",
  "dead",
  "sold",
  "lost",
  "canceled",
]);
export function isProcurementHistoryOnly(state: ProcurementGoatState): boolean {
  return PROCUREMENT_HISTORY_GOAT_STATES.has(state);
}

// The single handoff that makes a procured goat eligible for post-arrival Preventive Care (PC) vaccination work.
export function isAcceptedIntake(state: ProcurementGoatState): boolean {
  return state === "accepted_herd_intake";
}
