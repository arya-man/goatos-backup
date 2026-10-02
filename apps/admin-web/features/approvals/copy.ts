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
import type { AdminWebApprovalRequestType, AdminWebApprovalStatus } from "@/lib/api/server";

/** Status choices and request-type tabs, in order (the page and its loading.tsx both read these). */
export const STATUS_TABS: AdminWebApprovalStatus[] = ["pending", "approved", "rejected"];
export const TYPE_TABS: Array<"all" | AdminWebApprovalRequestType> = ["all", "birth", "death", "shifting"];
/**
 * Rows-per-page choices (template TablePaginationCustom). The server caps one page at 20
 * (domain.MaxApprovalPageSize, the mobile page-size rule), so no choice goes past it.
 */
export const PAGE_SIZES = [5, 10, 20] as const;
export const DEFAULT_PAGE_SIZE = 20;
/** Placeholder rows in the queue skeleton (route loading + the panel fallback): the pending queue is
 *  a handful of rows, so a short page keeps the Dense / rows-per-page footer in the first screen as
 *  the loaded card has it (TR3-P0-4). */
export const APPROVALS_SKELETON_ROWS = 5;

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
  // The status chip's words; the lowercase wire enum never reaches the screen.
  statusLabel: {
    pending: "Pending",
    approved: "Approved",
    rejected: "Rejected",
    cancelled: "Cancelled",
    unknown: "Unknown",
  },
  typeTab: {
    all: "All types",
    birth: "Birth",
    death: "Death",
    shifting: "Shifting",
  } as Record<string, string>,
  // "Park", the word every other screen's park filter uses (A11, pr294); the keys stay `farm`.
  farmTab: {
    all: "All parks",
  },
  filter: {
    status: "Status",
    farm: "Park",
  },
  dateFilter: {
    field: "Raised",
    any: "Any date",
    clear: "Clear dates",
    today: "Today",
    single: "One day",
    range: "Date range",
    aria: "Filter by the day a request was raised",
    previousMonth: "Previous month",
    nextMonth: "Next month",
    rangeStartHint: "Pick the first day",
    rangeEndHint: "Pick the last day",
    rangeSeparator: "–",
  },
  pager: {
    next: "Older requests",
    first: "Back to newest",
    rowsPerPage: "Rows per page:",
    dense: "Dense",
    of: "of",
  },
  kpi: {
    rowsInView: "Rows in view",
  },
  table: {
    // No id columns: "Subject" renders readable detail (shed move / request type), never a UUID.
    // "Raised by" is the backend-resolved raised_by_name (an unresolvable id is dropped, never shown).
    columns: ["Type", "Subject", "Raised by", "Raised", "Status", "Action"],
  },
  drawer: {
    eyebrow: "Approval request",
    aria: "Approval request record",
    closeLabel: "Close",
    metaType: "Request type",
    metaStatus: "Status",
    metaRaisedBy: "Raised by",
    metaRaisedAt: "Raised at",
    metaSummary: "Summary",
    metaSubjectGoat: "Subject goat",
    metaShiftingEvent: "Shifting event",
    metaDecidedBy: "Decided by",
    metaDecidedAt: "Decided at",
    metaDecisionReason: "Decision reason",
    summaryTitle: "Request detail",
    summaryEmpty: "No additional detail on this request.",
    note: "Approving records the birth or death, or clears the pen move to go ahead. Rejecting changes nothing and needs a reason, which the person who raised it will see.",
  },
  capture: {
    title: "Recorded on the form",
    version: "Form version",
    media: "Photos and videos",
    open: "Open",
    opening: "Opening…",
    unavailable: "This proof could not be opened.",
    kind: { video: "Video", photo: "Photo" } as Record<string, string>,
    missing: "Not captured (older app)",
    review: {
      pending: "Report proof awaiting the verifier",
      approved: "Report proof approved by the verifier",
      rework: "Report proof sent back for a re-shoot",
    } as Record<string, string>,
  },
  decision: {
    title: "Decision",
    or: "or",
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
    failed: "Decision not saved",
    // Farm-language sentences for a refused decision, keyed by the server's error code. The code
    // itself is a machine key and is never printed.
    errors: {
      death_evidence_incomplete: "All the death report steps must be recorded before it can be approved.",
      approval_already_decided: "This request was already decided or changed. Refresh to see its current state.",
      idempotency_conflict: "This request was already decided or changed. Refresh to see its current state.",
      version_conflict: "This request was already decided or changed. Refresh to see its current state.",
      stale_row_version: "This request was already decided or changed. Refresh to see its current state.",
      approval_request_not_found: "This request was already decided or changed. Refresh to see its current state.",
      death_already_applied: "This animal's death was already approved on another report. Reject this one as a duplicate.",
      permission_denied: "You can't decide this request.",
      park_scope_forbidden: "You can't decide this request.",
      missing_reason: "Write a reason before rejecting the request.",
      reason_too_long: "The reason is too long. Shorten it and try again.",
      default: "Could not save the decision. Try again.",
    },
  },
  error: {
    queueUnavailable: "Approvals queue is unavailable",
    queueUnavailableBody:
      "The requests could not be loaded right now. This is not an empty list — try again in a moment.",
    empty: "No approval requests match the current filters.",
  },
} as const;
