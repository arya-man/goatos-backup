// Pure display helpers for /approvals, kept free of React and of the `@/` alias so they run under
// `node --test` exactly as the page runs them.
//
// Every pen name goes through the ONE shared composer (lib/operational-location.ts): a shifting
// payload carries the physical shed id AND the partition label, and dropping the label rendered
// "Castro → Castro" for a Castro 2 → Castro 3 move (AGENTS.md, Operational Location Rule 5).
import type { AdminWebApprovalItem, AdminWebApprovalStatus } from "@/lib/api/server";
import { operationalLocationLabel } from "../../lib/operational-location.ts";
import { APPROVALS_COPY as COPY } from "./copy.ts";

type Summary = Record<string, unknown>;

function summaryObject(summary: unknown): Summary {
  return summary && typeof summary === "object" && !Array.isArray(summary) ? (summary as Summary) : {};
}

function strOf(s: Summary, k: string): string {
  const v = s[k];
  if (typeof v === "string") return v;
  if (typeof v === "number") return String(v);
  return "";
}

function titleCase(v: string): string {
  return v ? v.charAt(0).toUpperCase() + v.slice(1).replace(/_/g, " ") : "";
}

// The top-level park ("farm") an approval belongs to: shifting source park, else a birth placement
// park, else the destination park. Empty when the request carries no park (e.g. some deaths).
export function approvalParkId(item: Pick<AdminWebApprovalItem, "summary">): string {
  const s = summaryObject(item.summary);
  return strOf(s, "source_park_id") || strOf(s, "park_id") || strOf(s, "destination_park_id");
}

// A pen = shed name + its partition label, composed by the shared helper. Empty when the shed id
// does not resolve (never a raw id, never a bare partition label with no shed).
function penName(s: Summary, shedKey: string, partitionKey: string, locationNames: Record<string, string>): string {
  const shedId = strOf(s, shedKey);
  const shedName = shedId && locationNames[shedId] ? locationNames[shedId] : "";
  if (!shedName) return "";
  return operationalLocationLabel({ shedName, partitionLabel: strOf(s, partitionKey) });
}

// Readable list subject: "<Farm>: <from pen> → <to pen>" for a move, the animal's BACKEND-OWNED
// operational location for a death, otherwise the request type prefixed with its farm when known.
// Never a raw UUID.
export function approvalSubject(item: AdminWebApprovalItem, locationNames: Record<string, string>): string {
  const s = summaryObject(item.summary);
  const parkId = approvalParkId(item);
  const farm = parkId && locationNames[parkId] ? locationNames[parkId] : "";
  if (item.request_type === "death" && item.subject_animal_location) {
    return item.subject_animal_location;
  }
  if (item.request_type === "shifting") {
    const from = penName(s, "source_shed_id", "source_partition_label", locationNames);
    const to = penName(s, "destination_shed_id", "destination_partition_label", locationNames);
    if (from || to) {
      const move = `${from || "?"} → ${to || "?"}`;
      return farm ? `${farm}: ${move}` : move;
    }
  }
  const label = titleCase(item.request_type);
  return farm ? `${farm}: ${label}` : label;
}

// Turns the backend `summary` JSON into HUMAN-READABLE detail rows: park names and full pen names
// ("From" / "To"), category/priority title-cased, reason shown verbatim. Raw UUIDs are never shown.
export function approvalDetailRows(
  summary: unknown,
  locationNames: Record<string, string>,
  subjectAnimalLocation?: string,
): Array<{ label: string; value: string }> {
  const s = summaryObject(summary);
  const name = (id: string): string => (id && locationNames[id] ? locationNames[id] : "");
  const place = (parkKey: string, shedKey: string, partitionKey: string): string =>
    [name(strOf(s, parkKey)), penName(s, shedKey, partitionKey, locationNames)].filter(Boolean).join(" / ");

  const out: Array<{ label: string; value: string }> = [];
  const category = strOf(s, "category");
  if (category) out.push({ label: "Category", value: titleCase(category) });
  const priority = strOf(s, "priority");
  if (priority) out.push({ label: "Priority", value: titleCase(priority) });
  const from = place("source_park_id", "source_shed_id", "source_partition_label");
  if (from) out.push({ label: "From", value: from });
  const to = place("destination_park_id", "destination_shed_id", "destination_partition_label");
  if (to) out.push({ label: "To", value: to });
  if (subjectAnimalLocation) out.push({ label: "Location", value: subjectAnimalLocation });
  const reason = strOf(s, "reason");
  if (reason) out.push({ label: "Reason", value: reason });
  // The raiser's own words on WHY the animals are moving, captured on the phone at raise time.
  const comment = strOf(s, "comment");
  if (comment) out.push({ label: "Operator note", value: comment });
  return out;
}

// The status chip's words. The wire status is a lowercase enum and never reaches the screen.
export function approvalStatusLabel(status: AdminWebApprovalStatus | string): string {
  return (COPY.statusLabel as Record<string, string>)[status] ?? COPY.statusLabel.unknown;
}

// The farm-language sentence for a refused decision. The server's error CODE is a machine key and
// is never printed; an unknown code falls back to a generic retry sentence.
export function approvalErrorSentence(code: string | undefined): string {
  const errors = COPY.feedback.errors as Record<string, string>;
  return (code && errors[code]) || COPY.feedback.errors.default;
}

// Page-level confirmation after a decision. The decided row leaves the Pending list the moment the
// action redirects, so the drawer (which renders only a row still in the list) cannot carry it.
export function approvalSuccessSentence(status: string | undefined, code: string | undefined): string | null {
  if (status !== "success") return null;
  if (code === "approved") return COPY.feedback.approved;
  if (code === "rejected") return COPY.feedback.rejected;
  return null;
}

// Mirrors domain.MaxApprovalDecisionReasonLength (backend/internal/counts/domain/approval.go). The
// server measures BYTES (Go len), so the textarea's character cap alone cannot guarantee a fit for
// non-Latin text; the server action checks bytes too.
export const APPROVAL_REASON_MAX_BYTES = 2000;

export function approvalReasonTooLong(reason: string): boolean {
  return new TextEncoder().encode(reason).length > APPROVAL_REASON_MAX_BYTES;
}


const DAY_RE = /^\d{4}-\d{2}-\d{2}$/;

/**
 * The calendar filter as the page will send it: both ends YYYY-MM-DD, a lone end filling the
 * other, and an inverted or malformed pair dropped to "any date" (the server would 400 it, and a
 * bookmark with a typo should still open the page).
 */
export function approvalDateRange(rawFrom?: string, rawTo?: string): { from: string; to: string } {
  const from = rawFrom?.trim() ?? "";
  const to = rawTo?.trim() ?? "";
  const okFrom = DAY_RE.test(from) && !Number.isNaN(Date.parse(from));
  const okTo = DAY_RE.test(to) && !Number.isNaN(Date.parse(to));
  if (!okFrom && !okTo) return { from: "", to: "" };
  const a = okFrom ? from : to;
  const b = okTo ? to : from;
  if (a > b) return { from: "", to: "" };
  return { from: a, to: b };
}
