// /approvals display: pen names carry their partition, the confirmation survives the decided row
// leaving the list, and no machine code or wire enum reaches the screen.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  APPROVAL_REASON_MAX_BYTES,
  approvalDetailRows,
  approvalErrorSentence,
  approvalReasonTooLong,
  approvalStatusLabel,
  approvalSubject,
  approvalSuccessSentence,
} from "./approval-display.ts";
import { APPROVALS_COPY } from "./copy.ts";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
const names = { park: "Coimbatore", castro: "Castro", godel: "Godel 1", yashoda: "Yashoda" };
const shifting = (summary) => ({ approval_request_id: "r1", request_type: "shifting", status: "pending", summary });

test("A1: a shifting subject names both PENS, numeric partition labels included", () => {
  const item = shifting({
    source_park_id: "park",
    source_shed_id: "castro",
    source_partition_label: "2",
    destination_shed_id: "castro",
    destination_partition_label: "3",
  });
  assert.equal(approvalSubject(item, names), "Coimbatore: Castro 2 → Castro 3");
});

test("A1: worded labels use the dash, an undivided pen stays bare, 'whole' never leaks", () => {
  const item = shifting({
    source_park_id: "park",
    source_shed_id: "godel",
    source_partition_label: "Part 3",
    destination_shed_id: "yashoda",
    destination_partition_label: null,
  });
  assert.equal(approvalSubject(item, names), "Coimbatore: Godel 1 - Part 3 → Yashoda");
  const whole = shifting({ source_shed_id: "yashoda", source_partition_label: "whole", destination_shed_id: "castro", destination_partition_label: "1" });
  assert.equal(approvalSubject(whole, names), "Yashoda → Castro 1");
});

test("A1: the drawer's From / To rows carry the park and the full pen name", () => {
  const rows = approvalDetailRows(
    {
      source_park_id: "park",
      source_shed_id: "castro",
      source_partition_label: "2",
      destination_park_id: "park",
      destination_shed_id: "godel",
      destination_partition_label: "Part 3",
    },
    names,
  );
  assert.deepEqual(
    rows.filter((r) => r.label === "From" || r.label === "To"),
    [
      { label: "From", value: "Coimbatore / Castro 2" },
      { label: "To", value: "Coimbatore / Godel 1 - Part 3" },
    ],
  );
});

test("A1: the page and the drawer render through the pen-aware helpers, not a local shed lookup", () => {
  const page = read("./approvals-page.tsx");
  const drawer = read("./approvals-drawer.tsx");
  assert.match(page, /approvalSubject\(item, locationNames\)/);
  assert.match(drawer, /approvalDetailRows\(item\.summary, locationNames/);
  for (const src of [page, drawer]) assert.doesNotMatch(src, /source_shed_id/, "no local shed-only composition");
});

test("A1: an unresolved shed id never renders a bare partition label or an id", () => {
  const rows = approvalDetailRows({ source_shed_id: "unknown-uuid", source_partition_label: "2" }, names);
  assert.equal(rows.find((r) => r.label === "From"), undefined);
});

test("A2: success is confirmed at page level even though the decided row has left the list", () => {
  assert.equal(approvalSuccessSentence("success", "approved"), "Request approved.");
  assert.equal(approvalSuccessSentence("success", "rejected"), "Request rejected.");
  assert.equal(approvalSuccessSentence("error", "death_evidence_incomplete"), null);
  const page = read("./approvals-page.tsx");
  assert.match(page, /approvalSuccessSentence\(feedback\.status, feedback\.code\)/);
  // The drawer shows only refusals now; it cannot show a success for a row that is gone.
  const drawer = read("./approvals-drawer.tsx");
  assert.match(drawer, /feedback\.status === "error"/);
  assert.doesNotMatch(drawer, /"Done"/);
});

test("A3: refusal codes become farm sentences and the raw code is never printed", () => {
  assert.equal(
    approvalErrorSentence("death_evidence_incomplete"),
    "All the death report steps must be recorded before it can be approved.",
  );
  const conflict = "This request was already decided or changed. Refresh to see its current state.";
  assert.equal(approvalErrorSentence("approval_already_decided"), conflict);
  assert.equal(approvalErrorSentence("idempotency_conflict"), conflict);
  assert.equal(approvalErrorSentence("permission_denied"), "You can't decide this request.");
  assert.equal(approvalErrorSentence("something_new"), "Could not save the decision. Try again.");
  assert.equal(approvalErrorSentence(undefined), "Could not save the decision. Try again.");
  const drawer = read("./approvals-drawer.tsx");
  assert.doesNotMatch(drawer, /\{feedback\.code \?\? ""\}/);
  const page = read("./approvals-page.tsx");
  assert.doesNotMatch(page, /queue\.error\.code/);
});

test("A3: the status chip says a word, never the lowercase wire enum", () => {
  assert.equal(approvalStatusLabel("pending"), "Pending");
  assert.equal(approvalStatusLabel("approved"), "Approved");
  assert.equal(approvalStatusLabel("rejected"), "Rejected");
  assert.equal(approvalStatusLabel("cancelled"), "Cancelled");
  for (const src of [read("./approvals-page.tsx"), read("./approvals-drawer.tsx")]) {
    assert.doesNotMatch(src, />\{item\.status\}</);
  }
});

test("A3: no internal wording in the page copy", () => {
  const strings = [];
  const walk = (v) => (typeof v === "string" ? strings.push(v) : v && typeof v === "object" && Object.values(v).forEach(walk));
  walk(APPROVALS_COPY);
  assert.ok(strings.length > 40);
  for (const s of strings) assert.doesNotMatch(s, /\b(backend|API|environment)\b/i, s);
});

test("A5: the reject reason is capped at the server's limit, in bytes too", () => {
  assert.equal(APPROVAL_REASON_MAX_BYTES, 2000);
  assert.equal(approvalReasonTooLong("a".repeat(2000)), false);
  assert.equal(approvalReasonTooLong("a".repeat(2001)), true);
  // 700 Telugu characters = 2100 bytes: under the character cap, over the server's byte cap.
  assert.equal(approvalReasonTooLong("క".repeat(700)), true);
  assert.match(read("./approvals-drawer.tsx"), /maxLength=\{APPROVAL_REASON_MAX_BYTES\}/);
  assert.match(read("./actions.ts"), /approvalReasonTooLong\(reason\)/);
});

test("calendar filter: a well-formed pair passes, a lone end fills the other, bad input is 'any date'", async () => {
  const { approvalDateRange } = await import("./approval-display.ts");
  assert.deepEqual(approvalDateRange("2026-09-16", "2026-09-20"), { from: "2026-09-16", to: "2026-09-20" });
  assert.deepEqual(approvalDateRange("2026-09-16", ""), { from: "2026-09-16", to: "2026-09-16" });
  assert.deepEqual(approvalDateRange("", "2026-09-20"), { from: "2026-09-20", to: "2026-09-20" });
  assert.deepEqual(approvalDateRange("2026-09-20", "2026-09-16"), { from: "", to: "" }, "inverted range");
  assert.deepEqual(approvalDateRange("16-09-2026", undefined), { from: "", to: "" }, "not ISO");
  assert.deepEqual(approvalDateRange(undefined, undefined), { from: "", to: "" });
});

test("calendar filter: the page sends the dates to the server and the filter clears the cursor", () => {
  const page = readFileSync(new URL("./approvals-page.tsx", import.meta.url), "utf8");
  assert.match(page, /raised_from: dateRange\.from/);
  const filter = readFileSync(new URL("./approvals-date-filter.tsx", import.meta.url), "utf8");
  assert.match(filter, /"ap_cursor"/, "a new date range must drop the page cursor (the server binds it to the filter)");
});

test("a link to a request that is not on this page still opens its drawer (read on its own)", () => {
  const page = readFileSync(new URL("./approvals-page.tsx", import.meta.url), "utf8");
  assert.match(page, /getAdminWebApproval\(selectedId/);
  assert.match(page, /items=\{drawerItems\}/);
  assert.match(page, /justDecided/, "a just-decided row must not be re-read into a reopened drawer");
});

test("the single-request read is anchored to the generated contract, not a hand-cast path", () => {
  // PR #430 review: the endpoint must be in OpenAPI / the generated client, and the web must not
  // reach it through an unchecked string cast.
  const server = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");
  assert.match(server, /"\/admin-web\/counts\/approvals\/\{request_id\}" satisfies keyof AppApiPaths/);
  assert.doesNotMatch(server, /`\/admin-web\/counts\/approvals\/\$\{encodeURIComponent\(requestId\)\}` as keyof AppApiPaths/);
  const client = readFileSync(new URL("../../../../packages/api-client/src/generated/app-api.ts", import.meta.url), "utf8");
  assert.match(client, /"\/admin-web\/counts\/approvals\/\{request_id\}": \{/);
});
