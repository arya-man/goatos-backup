// Shared presentation maps for vaccination-execution work state / severity / SOP / proof /
// verification. Kept here (not inline in each component) so a state's tone + label are defined once.
import type {
  VaccinationExecutionSeverity,
  VaccinationExecutionWorkState,
  ProofStatus,
  SopStatus,
  VerificationStatus,
} from "@/lib/api/vaccination-execution";

// Mock palette tones: ok | warn | dng | info | mut | pur | teal (all defined in mesha-theme.css).
export type Tone = "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal";

// Severity ordering for the vaccination-execution domain. Defined once here (was inline in
// execution-board.tsx) so the chip order and the worst-of rollup rank stay in sync.
export const SEVERITY_ORDER: VaccinationExecutionSeverity[] = ["broken", "at_risk", "watch", "ok"];
export const SEVERITY_RANK: Record<VaccinationExecutionSeverity, number> = { ok: 0, watch: 1, at_risk: 2, broken: 3 };

interface StateMeta {
  label: string;
  tone: Tone;
}

export const WORK_STATE_META: Record<VaccinationExecutionWorkState, StateMeta> = {
  due: { label: "Due", tone: "warn" },
  overdue: { label: "Overdue", tone: "dng" },
  scheduled: { label: "Scheduled", tone: "info" },
  in_progress: { label: "In progress", tone: "info" },
  proof_pending: { label: "Proof pending", tone: "warn" },
  verification_pending: { label: "Verification pending", tone: "pur" },
  rejected: { label: "Rejected", tone: "dng" },
  deferred: { label: "Deferred", tone: "mut" },
  blocked: { label: "Blocked", tone: "dng" },
  owner_missing: { label: "Owner missing", tone: "dng" },
  completed: { label: "Completed", tone: "ok" },
};

export const SEVERITY_META: Record<VaccinationExecutionSeverity, StateMeta> = {
  ok: { label: "OK", tone: "ok" },
  watch: { label: "Watch", tone: "info" },
  at_risk: { label: "At risk", tone: "warn" },
  broken: { label: "Broken", tone: "dng" },
};

export const SOP_META: Record<SopStatus, StateMeta> = {
  not_started: { label: "SOP: not started", tone: "mut" },
  in_progress: { label: "SOP: in progress", tone: "info" },
  submitted: { label: "SOP: submitted", tone: "warn" },
  accepted: { label: "SOP: accepted", tone: "ok" },
  rework: { label: "SOP: rework", tone: "dng" },
};

export const PROOF_META: Record<ProofStatus, StateMeta> = {
  not_required: { label: "Proof: n/a", tone: "mut" },
  missing: { label: "Proof: missing", tone: "warn" },
  uploaded: { label: "Proof: uploaded", tone: "info" },
  rejected: { label: "Proof: rejected", tone: "dng" },
  accepted: { label: "Proof: accepted", tone: "ok" },
};

export const VERIFICATION_META: Record<VerificationStatus, StateMeta> = {
  not_ready: { label: "Verify: not ready", tone: "mut" },
  pending: { label: "Verify: pending", tone: "pur" },
  verified: { label: "Verify: verified", tone: "ok" },
  rejected: { label: "Verify: rejected", tone: "dng" },
};

// Display order for work-state quick-filter chips (most-broken first so attention sorts to the top).
export const WORK_STATE_ORDER: VaccinationExecutionWorkState[] = [
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
