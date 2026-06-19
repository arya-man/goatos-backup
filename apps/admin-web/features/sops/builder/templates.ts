// Palette, helpers, flow patterns, and static reference content for the SOP
// Builder. Ported from source-material/sop-playground-local/playground.html,
// retyped to the backend-valid vocabulary in model.ts.

import type { BuilderLink, BuilderNode, FieldType, NodeType } from "./model";

export interface PaletteItem {
  type: FieldType;
  icon: string;
  label: string;
}

// Field palette — backend-valid types only, friendly operator-facing labels.
export const PALETTE: PaletteItem[] = [
  { type: "text", icon: "✎", label: "Text" },
  { type: "number", icon: "#", label: "Number" },
  { type: "date_time", icon: "📅", label: "Date / time" },
  { type: "select", icon: "▾", label: "Select" },
  { type: "multiselect", icon: "☷", label: "Multi-select" },
  { type: "goat_lookup", icon: "🐐", label: "Goat scan" },
  { type: "rfid_scan", icon: "📡", label: "RFID scan" },
  { type: "location_picker", icon: "🏠", label: "Shed / location" },
  { type: "photo_proof", icon: "📷", label: "Photo proof" },
  { type: "video_proof", icon: "🎥", label: "Video proof" },
];

export interface NodePaletteItem {
  key: string;
  icon: string;
  label: string;
  type: NodeType;
  role: string;
}

// Advanced step palette + "place after" select source.
export const NODE_PALETTE: NodePaletteItem[] = [
  { key: "start", icon: "▶", label: "Start / trigger", type: "operator_execution", role: "task created" },
  { key: "approval", icon: "☑", label: "Approval gate", type: "approval", role: "approval gate" },
  { key: "operator", icon: "👤", label: "Assign operator", type: "operator_execution", role: "operator" },
  { key: "android", icon: "📱", label: "Android fill", type: "operator_execution", role: "android" },
  { key: "proof", icon: "🎥", label: "Proof gate", type: "proof_verification", role: "proof gate" },
  { key: "verifier", icon: "🔍", label: "Verifier review", type: "proof_verification", role: "verifier" },
  { key: "rework", icon: "↩", label: "Rework", type: "rework", role: "rework" },
  { key: "review", icon: "⚠", label: "Review queue", type: "blocked", role: "review queue" },
  { key: "event", icon: "◆", label: "Domain event", type: "accepted", role: "domain event" },
];

export const RULE_ACTIONS: { value: string; label: string; type: import("./model").RuleType }[] = [
  { value: "show_field", label: "show field", type: "visible_if" },
  { value: "require_field", label: "require field", type: "required_if" },
  { value: "block_submission", label: "block submission", type: "block_submission_if" },
  { value: "require_proof", label: "require proof", type: "proof_required_if" },
  { value: "validate", label: "validation check", type: "validation_rule" },
];

// ── helpers ────────────────────────────────────────────────────────────────
export function titleCase(key: string): string {
  return key.replace(/_/g, " ").replace(/\b\w/g, (m) => m.toUpperCase());
}

export function slugify(value: string, fallback: string): string {
  const out = String(value || "")
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "_")
    .replace(/^_+|_+$/g, "");
  return out || fallback;
}

export function nodeTypeFromRole(role: string): NodeType {
  const r = role.toLowerCase();
  if (r.includes("reject")) return "rejected";
  if (r.includes("approval") || r.includes("supervisor")) return "approval";
  if (r.includes("verif") || r.includes("proof") || r.includes("video") || r.includes("park")) return "proof_verification";
  if (r.includes("rework") || r.includes("re-submit")) return "rework";
  if (r.includes("review") || r.includes("blocked")) return "blocked";
  if (r.includes("event") || r.includes("accepted")) return "accepted";
  return "operator_execution";
}

export function nodeVisualClass(node: BuilderNode, isFirst: boolean): string {
  if (isFirst || node.id === "start") return "start";
  if (node.type === "accepted") return "accept";
  if (node.type === "rework") return "rework";
  if (node.type === "blocked" || node.type === "rejected") return "review";
  return "";
}

export function suggestLinkLabel(fromRole: string, toRole: string): string {
  const t = toRole.toLowerCase();
  if (t.includes("approval")) return "request";
  if (t.includes("rework")) return "rejected";
  if (t.includes("review") || t.includes("blocked")) return "blocked";
  if (t.includes("accepted") || t.includes("event")) return "accepted";
  if (t.includes("proof") || t.includes("verif")) return "needs proof";
  return "next";
}

export function suggestLinkAbout(fromLabel: string, toLabel: string, label: string): string {
  return `${fromLabel} → ${toLabel} when ${label || "the step completes"}.`;
}

// ── flow patterns (port of FLOW_PATTERNS) ────────────────────────────────────
export interface FlowPattern {
  name: string;
  copy: string;
  nodes: BuilderNode[];
  links: BuilderLink[];
}

function n(id: string, label: string, role: string, x: number, y: number): BuilderNode {
  return { id, label, role, type: nodeTypeFromRole(role), x, y };
}

export const FLOW_PATTERNS: Record<string, FlowPattern> = {
  simple: {
    name: "Simple submit",
    copy: "Operator fills form, backend validates, final event is saved.",
    nodes: [
      n("start", "Task Created", "task created", 18, 80),
      n("fill", "Operator Fills Form", "android", 260, 80),
      n("accept", "Final Event Saved", "domain event", 500, 80),
    ],
    links: [
      ["start", "fill", "assigned", "The task appears on the operator app."],
      ["fill", "accept", "accepted", "Backend validates the form and saves the event."],
    ],
  },
  proof: {
    name: "Submit with proof review",
    copy: "Operator submits proof, verifier accepts, rejects, or asks for rework.",
    nodes: [
      n("start", "Proof Submitted", "task created", 18, 60),
      n("verify", "Verifier Review", "verifier", 260, 60),
      n("accept", "Proof Accepted", "domain event", 18, 260),
      n("rework", "Rework Task", "rework", 260, 260),
      n("review", "Escalate Review", "review queue", 500, 260),
    ],
    links: [
      ["start", "verify", "queued", "Proof appears in verifier queue."],
      ["verify", "accept", "accepted", "Proof is accepted and attached to the event."],
      ["verify", "rework", "needs fix", "Operator must upload corrected proof."],
      ["verify", "review", "unclear", "Ambiguous proof escalates to review."],
    ],
  },
  approval: {
    name: "Request, approval, execution",
    copy: "Request goes to supervisor first, then operator completes the work.",
    nodes: [
      n("start", "Request Raised", "task created", 18, 60),
      n("approve", "Supervisor Approval", "approval gate", 250, 60),
      n("fill", "Operator Executes", "android", 500, 60),
      n("accept", "Event Saved", "domain event", 250, 260),
      n("review", "Rejected / Review", "review queue", 500, 260),
    ],
    links: [
      ["start", "approve", "request", "Request waits for supervisor decision."],
      ["approve", "fill", "approved", "Approved request becomes operator work."],
      ["fill", "accept", "completed", "Backend validates and saves the event."],
      ["approve", "review", "rejected", "Rejected or unclear request goes to review."],
    ],
  },
  scheduled: {
    name: "Scheduled task",
    copy: "System creates a due task, operator completes it, optional verifier closes it.",
    nodes: [
      n("start", "Schedule Creates Task", "task created", 18, 60),
      n("assign", "Assign Operator", "operator", 250, 60),
      n("fill", "Operator Completes", "android", 500, 60),
      n("verify", "Optional Verification", "verifier", 250, 260),
      n("accept", "History Updated", "domain event", 500, 260),
    ],
    links: [
      ["start", "assign", "due", "System creates the due task."],
      ["assign", "fill", "assigned", "Operator receives the scheduled task."],
      ["fill", "verify", "submitted", "Submission is ready for optional verification."],
      ["verify", "accept", "accepted", "History/audit record is updated."],
    ],
  },
  review: {
    name: "Exception review",
    copy: "Blocked or unclear submissions go to a review queue before saving.",
    nodes: [
      n("start", "Submission Blocked", "task created", 18, 80),
      n("review", "Review Queue", "review queue", 260, 80),
      n("fill", "Correct and Resubmit", "android", 500, 80),
      n("accept", "Accepted Event", "domain event", 260, 270),
    ],
    links: [
      ["start", "review", "blocked", "A rule prevents silent save and sends it to review."],
      ["review", "fill", "needs correction", "Operator or admin fixes missing/wrong information."],
      ["fill", "accept", "accepted", "Corrected submission is validated and saved."],
    ],
  },
};

export function inferFlowPattern(key: string): string {
  if (key === "video_verification" || key === "death_report") return "proof";
  if (key === "shifting") return "approval";
  if (key === "vaccination" || key === "feed_report") return "scheduled";
  if (key === "health_diagnosis" || key === "birth_abortion" || key === "procurement_arrival") return "review";
  return "simple";
}

// ── static reference content (teaching panels) ───────────────────────────────
export const MENTAL_MODEL = [
  { label: "Part 1", title: "Operator Form", copy: "The exact fields shown on Android: goat, shed, proof, comments, medicine batch, and so on." },
  { label: "Part 2", title: "Form Logic", copy: "Simple if/then rules: if source is wrong, require exception reason; if proof is missing, block submit." },
  { label: "Part 3", title: "Task Flow", copy: "Who touches the task after submit: supervisor approval, verifier review, rework, review queue, or final event." },
];

export const BUILD_STEPS = [
  { num: "Step 1", name: "Choose SOP", why: "Start from a known operation or custom draft." },
  { num: "Step 2", name: "Operator Form", why: "Create what the operator sees and fills." },
  { num: "Step 3", name: "Form Logic", why: "Decide when fields show, become required, or block submit." },
  { num: "Step 4", name: "Task Flow", why: "Decide approvals, review, rework, and final event." },
];

export const COVERAGE_MAP = [
  { title: "Covered in catalog", items: ["Shifting / movement", "Vaccination", "Health diagnosis", "Death report"] },
  { title: "Covered in catalog", items: ["Birth / abortion", "Feed report", "Video verification", "Procurement / arrival"] },
  { title: "Custom path", items: ["blank SOP shell", "custom fields", "custom conditions", "custom workflow nodes/links"] },
  { title: "Needs source inventory", items: ["all old Slack/App Script flows", "all edge-case approvals", "all notification rules", "final migration checklist"] },
];

export const CAPABILITIES = [
  { title: "Fields", items: ["text, number, date/time", "select and multiselect", "goat/RFID scan", "shed/location picker"] },
  { title: "Media and files", items: ["photo and video proof", "proof metadata", "original/rectified proof", "before/after capture"] },
  { title: "Rules", items: ["visible/required if", "block submission", "proof required if", "validation checks"] },
  { title: "Workflow", items: ["request approval", "operator execution", "proof verification", "reject/rework/accept paths"] },
  { title: "Data sources", items: ["active goats", "parks/sheds", "operators by scope", "medicine/feed catalogs"] },
  { title: "Integration", items: ["Android runner", "backend dry-run", "audit/outbox", "Slack/WhatsApp notification only"] },
];

export const OPTION_SOURCES = [
  { name: "active_goats", desc: "goat IDs, RFID, lifecycle, current location" },
  { name: "active_sheds", desc: "park/shed/partition with active flag" },
  { name: "operators_by_scope", desc: "role, park/shed scope, task capability" },
  { name: "movement_reasons", desc: "health, growth, breeding, delivery, routine" },
  { name: "proof_subjects", desc: "goat, batch, shed, medicine, feed, load" },
  { name: "verification_roles", desc: "park head, supervisor, central verifier" },
];

export const RULE_LIBRARY = [
  { title: "Visibility", items: ["show/hide field", "enable/disable field", "section-level conditions"] },
  { title: "Validation", items: ["required if", "range/pattern checks", "block submission if"] },
  { title: "Workflow", items: ["requires supervisor", "requires verifier", "rework on rejection"] },
  { title: "Calculations", items: ["due date", "expected count", "task risk"] },
  { title: "Proof", items: ["proof required if", "per-goat/per-batch", "before submit/final approval"] },
  { title: "Server checks", items: ["live goat state", "location active", "idempotency/duplicates"] },
];

export const DOMAINS = ["Movement", "Health Ops", "Lifecycle", "Feed / Stock", "Proof Review", "Custom Ops"];
export const TRIGGERS = ["Manual task / request", "Scheduled task", "Event-triggered", "Review queue"];
