// TEMPORARY EXCEPTION to the backend-driven UI contract rule (apps/admin-web/AGENTS.md ->
// "Backend-Driven UI Contract Rule"). Documented per that rule's escape hatch in
// context/frontend/admin-web-backend-ui-contract.md -> "Explicit exceptions".
//
// `/verification` is a NEW Admin / Data Ops authority screen (same tier as /config and /sops per
// context/architecture/verification-module-design.md section 2.1) built ahead of the backend admin-ui
// contract registering a `verification-review` page (backend/internal/adminui/app/service.go has no
// entry for it yet — the Verification module itself lands on a parallel branch,
// feat/verification-backend). `requireAdminWebPageContract("verification-review")` would throw for
// every request until that lands, so this page renders from local literal copy instead of
// `AdminUiPageContract.copy`/`option_groups`.
//
// TODO(verification-contract): once backend/internal/adminui/app/service.go registers route_id
// "verification-review" (title/subtitle/table columns/option groups/disabled reasons), delete this
// file, remove the `features/verification-review/` entry from
// apps/admin-web/scripts/check-ui-contract-literals.mjs SKIP_PATH_PARTS, and switch the page/drawer
// to `requireAdminWebPageContract("verification-review")` + `copy`/`tableLabels`/`optionGroup` from
// `@/lib/admin-ui-contract`, matching every other authority screen (/config, /sops, /operations/dlq).
export const VERIFICATION_REVIEW_COPY = {
  crumb: "Admin / Data Ops",
  title: "Verification — authority review",
  subtitle: "Flagged verifier decisions awaiting Head / Director / CEO action: rework, re-assign, or a penalty note.",
  statusTab: {
    rejected: "Flagged (rejected)",
    approved: "Approved",
    pending: "Awaiting verifier",
  } as Record<string, string>,
  kpi: {
    rejectedInView: "Flagged in view",
    approvedInView: "Approved in view",
    noTaskHandle: "No linked task",
    rowsInView: "Rows in view",
  },
  table: {
    columns: ["Category", "Vertical / module", "Subject", "Captured", "Verifier verdict", "Reason", "Action"],
  },
  filter: {
    categoryLabel: "Category",
    categoryAll: "All categories",
    clearAll: "Clear filters",
  },
  drawer: {
    eyebrow: "Verification record",
    aria: "Verification review record",
    closeLabel: "Close",
    metaStatus: "Verifier verdict",
    metaReason: "Verdict reason",
    metaVerifiedBy: "Verified by",
    metaVerifiedAt: "Verified at",
    metaCaptured: "Captured at",
    metaOperator: "Operator",
    metaShed: "Shed",
    metaPark: "Park",
    metaSourceModule: "Source module",
    metaSourceTask: "Source task",
    metaSourceSubmission: "Source submission",
    mediaTitle: "Proof media",
    mediaEmpty: "No proof media attached to this item.",
    mediaOpen: "Open proof",
    note: "The verifier's approve/reject decision is advisory input only. This screen is where the authority (Park Head / Director / CEO) takes the real action on the linked SOP task.",
  },
  rework: {
    title: "Rework",
    reasonLabel: "Rework reason",
    reasonPlaceholder: "Why is this being sent back for rework?",
    submit: "Request rework",
    disabledNoTask: "No linked SOP task on this verification item — rework cannot be requested from here.",
    disabledNotFlagged: "Rework is offered for rejected (flagged) items. Approved/pending items do not need rework.",
  },
  reassign: {
    title: "Re-assign",
    assigneeLabel: "New assignee",
    assigneePlaceholder: "Select a staff position…",
    reasonLabel: "Re-assign reason",
    reasonPlaceholder: "Why is this being re-assigned?",
    submit: "Re-assign task",
    disabledNoTask: "No linked SOP task on this verification item — re-assignment cannot be requested from here.",
    disabledNoRoster: "No staff positions available to assign in this park/shed scope.",
  },
  penalty: {
    title: "Penalty note",
    reasonLabel: "Penalty / escalation note",
    reasonPlaceholder: "Log a penalty or escalation note (not yet backed by an API)…",
    submit: "Log penalty note",
    disabled: "Penalty/escalation logging is not backed by a Mesha API yet. The daily 'penalties issued' metric is currently tracked manually per the PHC-Director EOD handbook. Tracked as a Verification-module TODO.",
  },
  action: {
    close: "Close",
    openAuditLog: "Open Audit Log",
  },
  error: {
    queueUnavailable: "Verification queue is unavailable",
    queueUnavailableBody:
      "The generic Verification module backend (context/architecture/verification-module-design.md) ships on a parallel branch and may not be registered on this environment's running API yet. This is a real backend/environment gap, not empty data — see the error below.",
    empty: "No verification items match the current filters.",
  },
} as const;
