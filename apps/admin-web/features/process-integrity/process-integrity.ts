// Presentation maps for the canonical process-integrity read model (Action Center / Adherence /
// Control Tower). These mirror the generated contract enums — distinct from the Parks physical
// projection's own enums in features/vaccination-execution/work-state.ts. Tone + label defined once.
import type {
  ProcessIntegrityProofState,
  ProcessIntegritySOPState,
  ProcessIntegritySeverity,
  ProcessIntegrityVerificationState,
  WorkState,
} from "@/lib/api/server";

// Mock palette tones (all defined in mesha-theme.css).
export type Tone = "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal";

interface Meta {
  label: string;
  tone: Tone;
}

export const WORK_STATE_META: Record<WorkState, Meta> = {
  due: { label: "Due", tone: "warn" },
  overdue: { label: "Overdue", tone: "dng" },
  blocked: { label: "Blocked", tone: "dng" },
  proof_pending: { label: "Proof pending", tone: "warn" },
  verification_pending: { label: "Verification pending", tone: "pur" },
  rejected: { label: "Rejected", tone: "dng" },
  deferred: { label: "Deferred", tone: "mut" },
  owner_missing: { label: "Owner missing", tone: "dng" },
  scheduled: { label: "Scheduled", tone: "info" },
  in_progress: { label: "In progress", tone: "info" },
  completed: { label: "Completed", tone: "ok" },
};

// Display order for board columns / quick-filter chips (most-broken first).
export const WORK_STATE_ORDER: WorkState[] = [
  "overdue",
  "blocked",
  "owner_missing",
  "rejected",
  "proof_pending",
  "verification_pending",
  "due",
  "in_progress",
  "scheduled",
  "deferred",
  "completed",
];

export const SEVERITY_META: Record<ProcessIntegritySeverity, Meta> = {
  ok: { label: "OK", tone: "ok" },
  watch: { label: "Watch", tone: "info" },
  at_risk: { label: "At risk", tone: "warn" },
  broken: { label: "Broken", tone: "dng" },
};

export const SEVERITY_ORDER: ProcessIntegritySeverity[] = ["broken", "at_risk", "watch", "ok"];
export const SEVERITY_RANK: Record<ProcessIntegritySeverity, number> = { ok: 0, watch: 1, at_risk: 2, broken: 3 };

export const SOP_META: Record<ProcessIntegritySOPState, Meta> = {
  not_started: { label: "SOP: not started", tone: "mut" },
  in_progress: { label: "SOP: in progress", tone: "info" },
  submitted: { label: "SOP: submitted", tone: "warn" },
  accepted: { label: "SOP: accepted", tone: "ok" },
  rework: { label: "SOP: rework", tone: "dng" },
};

export const PROOF_META: Record<ProcessIntegrityProofState, Meta> = {
  not_required: { label: "Proof: n/a", tone: "mut" },
  missing: { label: "Proof: missing", tone: "warn" },
  uploaded: { label: "Proof: uploaded", tone: "info" },
  accepted: { label: "Proof: accepted", tone: "ok" },
  rejected: { label: "Proof: rejected", tone: "dng" },
};

export const VERIFICATION_META: Record<ProcessIntegrityVerificationState, Meta> = {
  not_ready: { label: "Verify: not ready", tone: "mut" },
  pending: { label: "Verify: pending", tone: "pur" },
  accepted: { label: "Verify: accepted", tone: "ok" },
  rejected: { label: "Verify: rejected", tone: "dng" },
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
