// The Toxin review tab (maintainer decision 2026-08-25) — two properties this pins:
//
// 1. The list renders BACKEND-OWNED copy verbatim: rows come out of toxinReviewRows carrying
//    context_line / status_chip / outcome_label / origin_line untouched, with the round chip
//    present exactly when round_no > 1. Client-side composition of that copy is the defect the
//    copy-firewall rule exists to prevent.
// 2. The tab hides entirely when the backend contract does not enable the toxin_tab control:
//    the page gates BOTH the chip and the screen swap on controlEnabled(pageContract,
//    "toxin_tab", false) — role-scoped UI is capability-gated, never a role-string conditional.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const { toxinReviewRows } = await import("./toxin-rows.ts");

const fixture = [
  {
    task_id: "aaaaaaaa-0000-4000-8000-000000000001",
    feed_purchase_id: "bbbbbbbb-0000-4000-8000-000000000001",
    round_no: 1,
    origin: "purchase",
    farm_label: "CBE",
    feed_item_label: "Maize",
    vendor: "Sri Feeds",
    batch_no: 4,
    purchase_date: "2026-08-20",
    quantity_kg: 1200,
    status: "pending_review",
    status_chip: "Waiting for review",
    outcome: "negative",
    outcome_label: "Negative",
    steps_done: 6,
    steps_total: 6,
    row_version: 3,
    created_at: "2026-08-21T04:00:00Z",
    context_line: "Maize · Sri Feeds · CBE · 2026-08-20",
  },
  {
    task_id: "aaaaaaaa-0000-4000-8000-000000000002",
    feed_purchase_id: "bbbbbbbb-0000-4000-8000-000000000001",
    round_no: 2,
    origin: "invalid_retest",
    origin_line: "Retest — the last strip was invalid",
    farm_label: "CPT",
    feed_item_label: "Cotton seed",
    vendor: "",
    batch_no: 2,
    purchase_date: "2026-08-22",
    quantity_kg: 800,
    status: "pending_review",
    status_chip: "Waiting for review",
    outcome_label: "Positive",
    steps_done: 6,
    steps_total: 6,
    row_version: 1,
    created_at: "2026-08-23T04:00:00Z",
    context_line: "Cotton seed · CPT · 2026-08-22",
  },
];

test("rows carry the backend payload copy verbatim", () => {
  const rows = toxinReviewRows(fixture);
  assert.equal(rows.length, 2);
  assert.equal(rows[0].contextLine, "Maize · Sri Feeds · CBE · 2026-08-20");
  assert.equal(rows[0].statusChip, "Waiting for review");
  assert.equal(rows[0].outcomeLabel, "Negative");
  assert.equal(rows[0].purchaseDate, "2026-08-20");
  assert.equal(rows[0].rowVersion, 3);
});

test("the round chip appears only past round 1 and is the backend's origin_line", () => {
  const rows = toxinReviewRows(fixture);
  assert.equal(rows[0].roundChip, "", "round 1 must carry no round chip");
  assert.equal(rows[1].roundChip, "Retest — the last strip was invalid");
});

const pageSource = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
const listSource = readFileSync(new URL("./toxin-review-list.tsx", import.meta.url), "utf8");
const actionsSource = readFileSync(new URL("./toxin-actions.ts", import.meta.url), "utf8");

test("the toxin tab hides entirely when the contract control is disabled", () => {
  // Both the chip and the screen swap key on the SAME contract read, defaulting to hidden — an
  // older backend that declares no toxin_tab control shows nothing.
  assert.match(pageSource, /controlEnabled\(pageContract, "toxin_tab", false\)/);
  assert.match(pageSource, /const toxinActive = toxinTabEnabled &&/);
  assert.match(pageSource, /toxinOption=\{toxinTabEnabled \?/);
  // No role-string / permission-string conditionals anywhere in the toxin surfaces.
  for (const source of [pageSource, listSource]) {
    assert.doesNotMatch(source, /ceo_internal|RoleCEO|toxin\.verdict/);
  }
});

test("the verdict is idempotent, version-fenced, and reject requires a reason", () => {
  assert.match(actionsSource, /toxin-verdict-\$\{taskId\}-\$\{rowVersion\}-\$\{decision\}/);
  assert.match(actionsSource, /reject_reason_required/);
  assert.match(actionsSource, /row_version: rowVersion/);
  // The reject button stays disabled until a reason is typed.
  assert.match(listSource, /disabled=\{!reason\.trim\(\)\}/);
});

// The strip-photo remains reviewable, but the toxin drawer must not render it through <img src>
// during detail hydration. That old shape starts a proof-media download just because a drawer is
// visible. The safe shape is an explicit open link to the backend proof route.
test("the strip photo is an explicit open link, not an auto-fetching image", () => {
  const anchor = listSource.indexOf("toxin.drawer.strip_photo");
  assert.ok(anchor > 0, "strip photo section must still render");
  const stripSection = listSource.slice(Math.max(0, anchor - 400), anchor + 700);
  assert.match(stripSection, /<MuiLink href=/);
  assert.match(stripSection, /drawer\.media\.open/);
  assert.doesNotMatch(stripSection, /<img\b/);
  assert.doesNotMatch(stripSection, /vr-image-proof/);
});

// guard: toxin-panel-suspense (R3OPS-3). Switching to the Toxin tab or paging it must not blank the
// page: the screen shell (header + tabs) renders with no await, and only the list waits on
// GET /toxin/review inside UrlSuspense with a table skeleton.
test("toxin screen: sync shell, list in UrlSuspense with a table skeleton", () => {
  const section = readFileSync(new URL("./toxin-review-section.tsx", import.meta.url), "utf8");
  assert.match(section, /export function ToxinReviewScreen\(/, "the shell is not async");
  assert.doesNotMatch(section.slice(section.indexOf("export function ToxinReviewScreen("), section.indexOf("async function ToxinReviewPanel(")), /await /, "the shell awaits nothing");
  assert.match(section, /<UrlSuspense searchParams=\{sp\} watch=\{TOXIN_WATCH\} fallback=\{<TableSkeleton bare header=\{false\} columns=\{3\}/);
  assert.match(section, /async function ToxinReviewPanel\([\s\S]*await listToxinReview\(/, "the read lives in the suspended panel");
});

// guard: video-log-park-seed (FJ1-P1-7). /verify?park=X opens the video log on park X and the CSV
// follows it: the drawer's park filter falls back to the page park.
test("video log drawer seeds its park filter from the page park", () => {
  const page = readFileSync(new URL("./verification-review-page.tsx", import.meta.url), "utf8");
  const log = readFileSync(new URL("./video-log.tsx", import.meta.url), "utf8");
  assert.match(page, /parkFilter=\{one\(sp, VIDEO_LOG_PARK_KEY\) \|\| scope\.parkId \|\| undefined\}/);
  assert.match(log, /parkId=\{parkFilter \|\| parkId\}/, "the CSV download follows the same park");
});
