// TEMPORARY EXCEPTION to the backend-driven UI contract rule (apps/admin-web/AGENTS.md ->
// "Backend-Driven UI Contract Rule"), same escape hatch verification-review uses.
//
// /approvals is a NEW top-level decision surface (maintainer decision 2026-07-21): the pending
// birth/death/shifting approval queue, moved off mobile onto admin-web and gated to the four org
// tiers + admin + ceo_internal. The backend admin-ui contract does not register an "approvals" PAGE
// contract yet (backend/internal/adminui/app/service.go composes only the nav item + route label),
// so this page renders from local literal copy instead of AdminUiPageContract.copy/option_groups —
// exactly as /verification does today.
//
// TODO(approvals-contract): once service.go registers route_id "approvals"
// (title/subtitle/columns/status tabs/disabled reasons), delete this file, drop the
// features/approvals entry from scripts/check-ui-contract-literals.mjs SKIP_PATH_PARTS, and switch
// to requireAdminWebPageContract("approvals") + copy/tableLabels from @/lib/admin-ui-contract.
export const APPROVALS_COPY = {
  crumb: "Approvals",
  title: "Approvals",
  subtitle:
    "Pending birth, death, and shifting requests raised from the field. Approve to apply the change, or reject with a reason.",
  statusTab: {
    pending: "Pending",
    approved: "Approved",
    rejected: "Rejected",
  } as Record<string, string>,
  typeTab: {
    all: "All types",
    birth: "Birth",
    death: "Death",
    shifting: "Shifting",
  } as Record<string, string>,
  farmTab: {
    all: "All farms",
  },
  kpi: {
    pendingInView: "Pending in view",
    birthDeathInView: "Birth / death in view",
    shiftingInView: "Shifting in view",
    rowsInView: "Rows in view",
  },
  table: {
    // No id columns: "Subject" renders readable detail (shed move / request type), never a UUID.
    // "Raised by" was dropped because the raiser is stored as a user id with no name source yet.
    columns: ["Type", "Subject", "Raised", "Status", "Action"],
  },
  drawer: {
    eyebrow: "Approval request",
    aria: "Approval request record",
    closeLabel: "Close",
    metaType: "Request type",
    metaStatus: "Status",
    metaRaisedBy: "Raised by",
    metaRaisedAt: "Raised at",
    metaSubjectGoat: "Subject goat",
    metaShiftingEvent: "Shifting event",
    metaDecidedBy: "Decided by",
    metaDecidedAt: "Decided at",
    metaDecisionReason: "Decision reason",
    summaryTitle: "Request detail",
    summaryEmpty: "No additional detail on this request.",
    note: "Approving applies the request's effect atomically (a birth/death lifecycle change, or authorizing a shifting movement). Rejecting applies nothing and requires a reason the field operator will see.",
  },
  approve: {
    title: "Approve",
    submit: "Approve request",
    disabledDecided: "This request has already been decided.",
  },
  reject: {
    title: "Reject",
    reasonLabel: "Rejection reason",
    reasonPlaceholder: "Why is this request being rejected? The operator who raised it will see this.",
    submit: "Reject request",
    disabledDecided: "This request has already been decided.",
  },
  action: {
    close: "Close",
    openAuditLog: "Open Audit Log",
  },
  feedback: {
    approved: "Request approved.",
    rejected: "Request rejected.",
    failed: "Decision failed",
  },
  error: {
    queueUnavailable: "Approvals queue is unavailable",
    queueUnavailableBody:
      "The approval workflow could not be reached on this environment's running API. This is a real backend/environment gap, not empty data — see the error below.",
    empty: "No approval requests match the current filters.",
  },
} as const;
