// The 8 catalog SOP templates, ported from the playground and converted to the
// backend-valid field/rule/node vocabulary. Each is an editable seed: users can
// add/edit/remove anything after loading one.

import type { BuilderField, BuilderLink, BuilderNode, BuilderRule, FieldType, RuleOperator, RuleType, SopMeta } from "./model";
import { nodeTypeFromRole, titleCase } from "./templates";

function f(key: string, type: FieldType, required: boolean, opts: Partial<BuilderField> = {}): BuilderField {
  return {
    key,
    label: opts.label ?? titleCase(key),
    type,
    required,
    description: "",
    placeholder: "",
    defaultValue: "",
    options: "",
    ...opts,
  };
}

function rule(
  id: string,
  type: RuleType,
  whenField: string,
  operator: RuleOperator,
  value: string,
  message: string,
  targetField = "",
): BuilderRule {
  return { id, type, whenField, operator, value, targetField, message, on: true };
}

function n(id: string, label: string, role: string, x: number, y: number): BuilderNode {
  return { id, label, role, type: nodeTypeFromRole(role), x, y };
}

export interface SopTemplate {
  meta: SopMeta;
  fields: BuilderField[];
  rules: BuilderRule[];
  nodes: BuilderNode[];
  links: BuilderLink[];
  badge: string;
  tags: string[];
  summary: string;
}

function meta(p: Partial<SopMeta>): SopMeta {
  return {
    title: "Custom SOP",
    subtitle: "Draft",
    code: "ops.custom",
    domain: "Custom Ops",
    trigger: "Manual task / request",
    runner: "Operator Android",
    submitLabel: "Submit",
    typeLabel: "TASK",
    repeatForEachGoat: false,
    ...p,
  };
}

export const SOP_TEMPLATES: Record<string, SopTemplate> = {
  shifting: {
    badge: "editing",
    tags: ["Phase 2 seed", "batch goats", "video proof"],
    summary: "Direction/request, due-date rules, proof video, approvals, movement event, location history.",
    meta: meta({
      title: "Shifting SOP", subtitle: "Direction / Request", code: "movement.shifting", domain: "Movement",
      trigger: "Manual task / request", submitLabel: "Submit shifting", typeLabel: "DIRECTION", repeatForEachGoat: true,
    }),
    fields: [
      f("shift_type", "select", true, { options: "request,direction" }),
      f("category", "select", true, { options: "routine,health,sale,procurement,emergency" }),
      f("priority", "select", true, { options: "low,normal,high,urgent" }),
      f("goat_ids", "goat_lookup", true, { label: "Goats", note: "repeat for each", optionSource: "active_goats" }),
      f("source_location_id", "location_picker", true, { label: "Source Location", optionSource: "locations.active" }),
      f("destination_location_id", "location_picker", true, { label: "Destination Location", optionSource: "locations.active" }),
      f("destination_count", "number", true),
      f("reason", "text", false),
      f("proof_video", "video_proof", true, { label: "Destination Proof Video" }),
    ],
    rules: [
      rule("shifting_dest", "block_submission_if", "destination_location_id", "empty", "", "Destination location is required."),
      rule("shifting_proof", "proof_required_if", "category", "not_empty", "", "Video proof is required before verification.", "proof_video"),
    ],
    nodes: [
      n("start", "Request Raised", "task created", 18, 60),
      n("approve", "Supervisor Approval", "approval gate", 250, 60),
      n("fill", "Operator Executes", "android", 500, 60),
      n("proof", "Proof Verification", "verifier", 500, 230),
      n("accept", "Movement Event", "domain event", 250, 260),
      n("rework", "Rework Requested", "rework", 18, 260),
    ],
    links: [
      ["start", "approve", "request", "Request waits for supervisor decision."],
      ["approve", "fill", "approved", "Approved request becomes operator work."],
      ["fill", "proof", "needs proof", "Submission requires proof before verification."],
      ["proof", "accept", "accepted", "Verifier accepts; backend writes the movement event."],
      ["proof", "rework", "rejected", "Verifier rejects proof; a rework task is created."],
    ],
  },

  vaccination: {
    badge: "next",
    tags: ["due schedule", "medicine", "verification"],
    summary: "Schedule, assigned operator, goat scan, medicine/batch metadata, proof, park-head verification.",
    meta: meta({
      title: "Vaccination SOP", subtitle: "Scheduled health task", code: "health.vaccination", domain: "Health Ops",
      trigger: "Scheduled task", submitLabel: "Submit vaccination", typeLabel: "SCHEDULED",
    }),
    fields: [
      f("scheduled_at", "date_time", true),
      f("assigned_operator", "text", true, { optionSource: "operators_by_scope" }),
      f("goats", "goat_lookup", true, { note: "batch or individual", optionSource: "active_goats" }),
      f("vaccine_name", "select", true, { options: "PPR,ET,FMD,HS,BQ" }),
      f("medicine_batch", "text", true),
      f("dose_ml", "number", true),
      f("administered_at", "date_time", true),
      f("proof_photo", "photo_proof", true),
      f("adverse_reaction", "select", false, { options: "yes,no" }),
      f("reaction_notes", "text", false, { cond: "adverse reaction" }),
      f("verification_notes", "text", false),
    ],
    rules: [
      rule("vacc_batch", "block_submission_if", "medicine_batch", "empty", "", "Medicine batch is required."),
      rule("vacc_reaction", "required_if", "adverse_reaction", "equals", "yes", "Record reaction notes when there is an adverse reaction.", "reaction_notes"),
      rule("vacc_proof", "proof_required_if", "administered_at", "not_empty", "", "Photo proof is required after administering.", "proof_photo"),
    ],
    nodes: [
      n("start", "Schedule Created", "task created", 18, 40),
      n("assign", "Assign Operator", "operator", 245, 40),
      n("scan", "Scan + Dose", "android", 470, 40),
      n("proof", "Photo Proof", "proof gate", 245, 240),
      n("verify", "Park Head Verify", "verifier", 18, 240),
      n("accept", "Vaccination Event", "domain event", 18, 350),
      n("rework", "Rectify Entry", "rework", 245, 350),
    ],
    links: [
      ["start", "assign", "scheduled", "Daily plan creates vaccination tasks."],
      ["assign", "scan", "assigned", "Operator scans goats and records the dose."],
      ["scan", "proof", "dose recorded", "Medicine and batch details require proof."],
      ["proof", "verify", "proof captured", "Park head verifies the proof and metadata."],
      ["verify", "accept", "accepted", "Backend writes vaccination history."],
      ["verify", "rework", "rejected", "Incorrect proof returns for rectification."],
    ],
  },

  health_diagnosis: {
    badge: "template",
    tags: ["conditional", "doctor/park head", "rework"],
    summary: "Symptoms, disease selection, quarantine/blocking states, treatment plan, repeat follow-ups.",
    meta: meta({
      title: "Health Diagnosis SOP", subtitle: "Symptoms / treatment / follow-up", code: "health.diagnosis", domain: "Health Ops",
      trigger: "Manual task / request", runner: "Doctor / park head", submitLabel: "Submit diagnosis", typeLabel: "HEALTH",
    }),
    fields: [
      f("goat", "goat_lookup", true, { optionSource: "active_goats" }),
      f("symptoms", "multiselect", true, { options: "fever,cough,lameness,wound,diarrhea,off-feed" }),
      f("temperature_c", "number", false),
      f("disease_suspected", "select", true, { options: "unknown,pneumonia,parasite,injury,digestive" }),
      f("severity", "select", true, { options: "low,medium,high,critical" }),
      f("quarantine_required", "select", false, { options: "yes,no" }),
      f("treatment_plan", "text", true),
      f("medicine", "text", false),
      f("follow_up_at", "date_time", true),
      f("proof_photo", "photo_proof", false),
    ],
    rules: [
      rule("hd_followup", "block_submission_if", "follow_up_at", "empty", "", "Follow-up date is required."),
      rule("hd_severe", "required_if", "severity", "in", "high,critical", "High/critical cases require a treatment plan.", "treatment_plan"),
    ],
    nodes: [
      n("start", "Case Opened", "task created", 18, 40),
      n("triage", "Triage Symptoms", "android", 245, 40),
      n("doctor", "Doctor / Park Review", "verifier", 470, 40),
      n("treatment", "Treatment Plan", "android", 470, 170),
      n("accept", "Health Record Updated", "domain event", 18, 320),
      n("review", "Needs Recheck", "review queue", 470, 320),
    ],
    links: [
      ["start", "triage", "reported", "Operator records symptoms and vitals."],
      ["triage", "doctor", "needs review", "Doctor or park head reviews serious symptoms."],
      ["doctor", "treatment", "approved", "Reviewer approves the treatment plan."],
      ["treatment", "accept", "accepted", "Backend updates health status and restrictions."],
      ["doctor", "review", "rejected", "Diagnosis is incomplete and must be rechecked."],
    ],
  },

  death_report: {
    badge: "template",
    tags: ["terminal", "media proof", "audit"],
    summary: "Death reason, location, proof media, verifier decision, terminal lifecycle update.",
    meta: meta({
      title: "Death Report SOP", subtitle: "Terminal event / proof", code: "lifecycle.death_report", domain: "Lifecycle",
      trigger: "Event-triggered", submitLabel: "Submit death report", typeLabel: "TERMINAL",
    }),
    fields: [
      f("goat", "goat_lookup", true, { optionSource: "active_goats" }),
      f("death_reason", "select", true, { options: "disease,injury,age,predator,unknown" }),
      f("death_time", "date_time", true),
      f("location", "location_picker", true, { optionSource: "locations.active" }),
      f("proof_media", "photo_proof", true),
      f("body_disposal_method", "select", true, { options: "buried,disposed,vet pickup,unknown" }),
      f("verifier_decision", "select", true, { options: "accepted,rejected,rework" }),
      f("audit_notes", "text", false),
    ],
    rules: [
      rule("death_proof", "proof_required_if", "death_reason", "not_empty", "", "Proof media is required before the terminal update.", "proof_media"),
      rule("death_proof_block", "block_submission_if", "proof_media", "empty", "", "Cannot close lifecycle without proof media."),
    ],
    nodes: [
      n("start", "Death Reported", "task created", 18, 60),
      n("proof", "Capture Proof", "proof gate", 250, 60),
      n("verify", "Verifier Decision", "verifier", 480, 60),
      n("accept", "Lifecycle Closed", "domain event", 250, 250),
      n("rework", "Rework Proof", "rework", 480, 250),
    ],
    links: [
      ["start", "proof", "reported", "Operator records the terminal report."],
      ["proof", "verify", "proof captured", "Verifier reviews proof before lifecycle close."],
      ["verify", "accept", "accepted", "Backend marks goat terminal and preserves audit."],
      ["verify", "rework", "rejected", "Proof/metadata must be corrected."],
    ],
  },

  birth_abortion: {
    badge: "template",
    tags: ["repeat kids", "relationships", "exceptions"],
    summary: "Mother goat, kids count, sex/breed/weight fields, abortion path, relation evidence.",
    meta: meta({
      title: "Birth / Abortion SOP", subtitle: "Lifecycle / mother-child relation", code: "lifecycle.birth_abortion", domain: "Lifecycle",
      trigger: "Manual task / request", submitLabel: "Submit lifecycle event", typeLabel: "LIFECYCLE",
    }),
    fields: [
      f("mother_goat", "goat_lookup", true, { optionSource: "active_goats" }),
      f("event_type", "select", true, { options: "birth,abortion" }),
      f("kids_count", "number", false, { cond: "birth" }),
      f("kid_sex", "multiselect", false, { options: "male,female", cond: "birth" }),
      f("kid_weight_notes", "text", false),
      f("abortion_reason", "text", false, { cond: "abortion" }),
      f("proof_photo", "photo_proof", true),
      f("relation_evidence", "text", false),
    ],
    rules: [
      rule("ba_birth", "required_if", "event_type", "equals", "birth", "Births require a kids count.", "kids_count"),
      rule("ba_abortion", "required_if", "event_type", "equals", "abortion", "Abortions require a reason.", "abortion_reason"),
    ],
    nodes: [
      n("start", "Lifecycle Event", "task created", 18, 50),
      n("fill", "Record Mother/Kids", "android", 245, 50),
      n("proof", "Evidence Proof", "proof gate", 470, 50),
      n("verify", "Verify Relation", "verifier", 245, 230),
      n("accept", "Relation Event", "domain event", 18, 350),
      n("review", "Review Queue", "review queue", 470, 350),
    ],
    links: [
      ["start", "fill", "started", "Operator records birth or abortion details."],
      ["fill", "proof", "needs proof", "Proof/evidence is attached."],
      ["proof", "verify", "proof captured", "Verifier checks relation evidence."],
      ["verify", "accept", "accepted", "Backend writes the lifecycle relationship event."],
      ["verify", "review", "ambiguous", "Ambiguous relation goes to review."],
    ],
  },

  feed_report: {
    badge: "template",
    tags: ["number fields", "stock refs", "proof"],
    summary: "Feed type, quantity, shed/park scope, photo/video proof, variance review.",
    meta: meta({
      title: "Feed Report SOP", subtitle: "Stock / consumption / proof", code: "stock.feed_report", domain: "Feed / Stock",
      trigger: "Scheduled task", submitLabel: "Submit feed report", typeLabel: "STOCK",
    }),
    fields: [
      f("feed_type", "select", true, { options: "hay,concentrate,mineral,silage,other" }),
      f("quantity_kg", "number", true),
      f("shed", "location_picker", true, { optionSource: "locations.active" }),
      f("goat_group", "text", false),
      f("stock_ref", "text", false),
      f("proof_photo", "photo_proof", true),
      f("variance_reason", "text", false, { cond: "variance high" }),
    ],
    rules: [
      rule("feed_proof", "block_submission_if", "proof_photo", "empty", "", "Feed proof photo is required."),
      rule("feed_proof_req", "proof_required_if", "quantity_kg", "not_empty", "", "Capture proof for recorded feed quantity.", "proof_photo"),
    ],
    nodes: [
      n("start", "Feed Task", "task created", 18, 50),
      n("fill", "Record Feed Use", "android", 245, 50),
      n("proof", "Proof Capture", "proof gate", 470, 50),
      n("verify", "Variance Review", "verifier", 245, 230),
      n("accept", "Stock Event", "domain event", 18, 350),
      n("rework", "Correct Quantity", "rework", 470, 350),
    ],
    links: [
      ["start", "fill", "scheduled", "Daily feed task opens."],
      ["fill", "proof", "reported", "Quantity and shed scope are recorded."],
      ["proof", "verify", "proof captured", "Proof and variance are reviewed."],
      ["verify", "accept", "accepted", "Backend writes the stock/feed usage event."],
      ["verify", "rework", "rejected", "Incorrect stock report returns for correction."],
    ],
  },

  video_verification: {
    badge: "template",
    tags: ["original/rectified", "verifier", "correction"],
    summary: "Original proof, verifier proof, rectified proof, rejection reason, rework assignment.",
    meta: meta({
      title: "Video Verification SOP", subtitle: "Proof review / rectification", code: "proof.video_verification", domain: "Proof Review",
      trigger: "Review queue", runner: "Verifier console", submitLabel: "Submit verification", typeLabel: "PROOF",
    }),
    fields: [
      f("original_proof", "video_proof", true),
      f("proof_subject", "select", true, { options: "goat,batch,shed,feed,medicine,load" }),
      f("verifier_decision", "select", true, { options: "accepted,rejected,rework" }),
      f("rejection_reason", "text", false, { cond: "rejected/rework" }),
      f("rectified_proof", "video_proof", false, { cond: "rework" }),
    ],
    rules: [
      rule("vv_reason", "required_if", "verifier_decision", "in", "rejected,rework", "Rejected/rework needs a rejection reason.", "rejection_reason"),
      rule("vv_rework", "required_if", "verifier_decision", "equals", "rework", "Rework requires rectified proof.", "rectified_proof"),
    ],
    nodes: [
      n("start", "Proof Submitted", "task created", 18, 60),
      n("verify", "Verifier Review", "verifier", 250, 60),
      n("accept", "Proof Accepted", "domain event", 18, 260),
      n("rework", "Rectification Task", "rework", 250, 260),
      n("review", "Escalate Review", "review queue", 480, 260),
    ],
    links: [
      ["start", "verify", "queued", "Original proof appears in the verifier queue."],
      ["verify", "accept", "accepted", "Proof becomes attached to the source event."],
      ["verify", "rework", "needs fix", "Operator must upload rectified proof."],
      ["verify", "review", "unclear", "Ambiguous proof escalates."],
    ],
  },

  procurement_arrival: {
    badge: "future",
    tags: ["load", "files", "handoff"],
    summary: "Load proof, supplier/farmer refs, arrival verification, replacement/exception flow.",
    meta: meta({
      title: "Procurement / Arrival SOP", subtitle: "Load intake / verification", code: "procurement.arrival", domain: "Custom Ops",
      trigger: "Manual task / request", submitLabel: "Submit arrival", typeLabel: "INTAKE",
    }),
    fields: [
      f("load_ref", "text", true),
      f("supplier_ref", "text", true),
      f("farmer_ref", "text", false),
      f("arrival_time", "date_time", true),
      f("arrival_location", "location_picker", true, { optionSource: "locations.active" }),
      f("expected_count", "number", true),
      f("received_count", "number", true),
      f("load_proof", "photo_proof", true),
      f("replacement_reason", "text", false, { cond: "count mismatch" }),
    ],
    rules: [
      rule("proc_proof", "block_submission_if", "load_proof", "empty", "", "Load proof is required to close arrival."),
      rule("proc_proof_req", "proof_required_if", "arrival_time", "not_empty", "", "Capture load proof on arrival.", "load_proof"),
    ],
    nodes: [
      n("start", "Load Arrives", "task created", 18, 50),
      n("fill", "Record Intake", "android", 245, 50),
      n("proof", "Load Proof", "proof gate", 470, 50),
      n("verify", "Arrival Verify", "verifier", 245, 230),
      n("accept", "Intake Event", "domain event", 18, 350),
      n("rework", "Replacement / Exception", "rework", 470, 350),
    ],
    links: [
      ["start", "fill", "arrived", "Operator records supplier/load details."],
      ["fill", "proof", "needs proof", "Load evidence is attached."],
      ["proof", "verify", "proof captured", "Verifier checks count and source references."],
      ["verify", "accept", "accepted", "Backend records the procurement/arrival event."],
      ["verify", "rework", "mismatch", "Count/source mismatch creates exception flow."],
    ],
  },
};

export const CATALOG_ORDER = [
  "shifting", "vaccination", "health_diagnosis", "death_report",
  "birth_abortion", "feed_report", "video_verification", "procurement_arrival",
];

// Blank custom-draft seed used by the "Create Custom SOP Draft" path.
export function blankDraft(input: { name: string; code: string; domain: string; trigger: string }): SopTemplate {
  const shortName = input.name.replace(/\s+SOP$/i, "");
  return {
    badge: "draft",
    tags: [],
    summary: "Custom draft",
    meta: meta({
      title: /\bSOP\b/i.test(input.name) ? input.name : `${input.name} SOP`,
      subtitle: "Custom draft",
      code: input.code,
      domain: input.domain,
      trigger: input.trigger,
      submitLabel: `Submit ${shortName.toLowerCase()}`,
      typeLabel: "CUSTOM",
    }),
    fields: [
      f("performed_at", "date_time", true, { label: "Performed At", description: "Captured when the operator completes the task." }),
      f("performed_by", "text", true, { label: "Performed By", description: "Operator accountable for this task.", optionSource: "operators_by_scope" }),
      f("comments", "text", false, { label: "Comments", description: "Optional notes for reviewer/audit." }),
    ],
    rules: [
      rule("custom_required", "block_submission_if", "performed_at", "empty", "", "Required fields must be filled before submission."),
    ],
    nodes: [
      n("start", "Draft Started", "task created", 18, 60),
      n("fill", `Fill ${shortName}`, "android", 260, 60),
      n("accept", `${shortName} Event`, "domain event", 260, 250),
    ],
    links: [
      ["start", "fill", "assigned", `Create the ${shortName} task and send it to the operator form.`],
      ["fill", "accept", "accepted", `Validated ${shortName} submission writes a domain event or audit record.`],
    ],
  };
}
