// T1-T7 (2026-09-25 Tasks review): writes to one task are serialised and read their fence when
// SENT; the drawer shows the newer of its cache and the list; list islands never repaint a row
// backwards; filter chips speak DD/MM/YYYY and never print an id; the board has an empty state; a
// failed detail read offers Retry; cancelled rows fill the board's Cancelled column.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  currentTaskRowVersion,
  publishTaskRow,
  runTaskWrite,
  taskWriteInFlight,
} from "./task-row-store.ts";
import { patchApplies, pickDrawerRow, withCancelledRows, withPatch } from "./task-detail-pick.ts";
import { dateSpanLabel, personFilterLabel } from "./filter-labels.ts";
import { attachmentKindLabel } from "./task-row.ts";

const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

function deferred() {
  let resolve;
  const promise = new Promise((r) => (resolve = r));
  return { promise, resolve };
}

test("T1: a status change right after a comment waits for it and sends the comment's version", async () => {
  const id = "task-t1";
  publishTaskRow(id, { rowVersion: 5 });
  const commentOnTheWire = deferred();
  // The comment is posted: in flight, and it will publish v6 when it lands.
  const comment = runTaskWrite(id, async () => {
    await commentOnTheWire.promise;
    publishTaskRow(id, { rowVersion: 6 });
    return "commented";
  });
  assert.equal(taskWriteInFlight(id), true, "the status menu and edit form must see a write in flight");
  // The OLD behaviour, for the record: a fence captured at the click is the pre-comment one.
  assert.equal(currentTaskRowVersion(id, 5), 5);

  // 0.4 s later the status change is pressed; its fence is read only when its turn comes.
  let sentFence = null;
  const status = runTaskWrite(id, async () => {
    sentFence = currentTaskRowVersion(id, 5);
    return "moved";
  });
  await Promise.resolve();
  assert.equal(sentFence, null, "the status write must not be sent while the comment is on the wire");

  commentOnTheWire.resolve();
  assert.equal(await comment, "commented");
  assert.equal(await status, "moved");
  assert.equal(sentFence, 6, "the status write carries the version the comment published, not v5");
  assert.equal(taskWriteInFlight(id), false);
});

test("T1: a failed earlier write does not block the next one", async () => {
  const id = "task-t1-fail";
  const first = runTaskWrite(id, async () => {
    throw new Error("network");
  });
  const second = runTaskWrite(id, async () => "ok");
  await assert.rejects(first);
  assert.equal(await second, "ok");
  assert.equal(taskWriteInFlight(id), false);
});

test("T1: the status menu, the board drop, the composer and the edit form are all wired to it", () => {
  const menu = read("./task-status-menu.tsx");
  assert.match(menu, /runTaskWrite\(task\.id,/);
  assert.match(menu, /formData\.set\("row_version", String\(currentTaskRowVersion\(task\.id,/);
  assert.match(menu, /useTaskWriteInFlight\(task\.id\)/);
  assert.match(read("./task-activity-composer.tsx"), /runTaskWrite\(task\.id, \(\) => action\(formData\)\)/);
  assert.match(read("./edit-task-modal.tsx"), /disabled=\{overLimit \|\| writing\}/);
  assert.match(read("./task-board-dnd.tsx"), /runTaskWrite\(task\.id,/);
});

const row = (over) => ({ id: "t", rowVersion: 5, status: "in_progress", statusLabel: "In progress", activity: [], notes: [], ...over });

test("T2: the drawer shows the newer of its cached detail and the list row", () => {
  const cachedV5 = row({ rowVersion: 5, activity: [{ id: "a" }] });
  const listV7 = row({ rowVersion: 7, status: "done", statusLabel: "Done" });
  const picked = pickDrawerRow(cachedV5, listV7);
  assert.equal(picked.row.status, "done", "a task moved elsewhere must not reopen on its stale cache");
  assert.equal(picked.detailLoaded, false, "the stale feed must not be shown as final; a fresh read follows");
  // Same version: the cache (with its feed) is the row.
  const same = pickDrawerRow(cachedV5, row({ rowVersion: 5 }));
  assert.equal(same.row, cachedV5);
  assert.equal(same.detailLoaded, true);
  // No cache yet, or a task not on this page.
  assert.deepEqual(pickDrawerRow(undefined, listV7), { row: listV7, detailLoaded: false });
  assert.deepEqual(pickDrawerRow(cachedV5, null), { row: cachedV5, detailLoaded: true });
});

test("T2: the drawer's own write never reads as a stale cache (feed stays shown, no endless loading)", () => {
  // Live 25/09: comment + status in the drawer moved the list row to v7 through the published patch
  // while the cached detail stayed v5, so the drawer showed "Loading activity..." forever.
  const cachedV5 = row({ rowVersion: 5, status: "in_progress" });
  const ownPatch = { rowVersion: 7, status: "done" };
  const listPatched = withPatch(row({ rowVersion: 5, status: "in_progress" }), ownPatch);
  const picked = pickDrawerRow(withPatch(cachedV5, ownPatch), listPatched);
  assert.equal(picked.detailLoaded, true, "our own write must keep the loaded feed");
  assert.equal(picked.row.status, "done");
  // The host lays the patch over both sides; its source must say so.
  const host = read("./task-drawer-host.tsx");
  assert.match(host, /pickDrawerRow\(cached \? withPatch\(cached, patch\)/);
  assert.match(host, /cacheBehindList/);
});

test("T2: a patch older than the row is ignored everywhere (drawer, table, chips)", () => {
  const r = row({ rowVersion: 7, status: "done" });
  assert.equal(withPatch(r, { rowVersion: 5, status: "in_progress" }).status, "done");
  assert.equal(withPatch(r, { status: "open" }).status, "open", "an optimistic move carries no version");
  assert.equal(patchApplies(7, { rowVersion: 5, status: "in_progress" }), false);
  assert.equal(patchApplies(7, { rowVersion: 7, status: "done" }), true);
  assert.equal(patchApplies(7, undefined), false);
  assert.match(read("./leadership-tasks-table.tsx"), /patchApplies\(row\.rowVersion, patch\)/);
  assert.match(read("./leadership-tasks-filters.tsx"), /patchApplies\(row\.rowVersion, patch\)/);
});

test("T2/T5: the detail is re-read on every open, and a failed read shows Retry, never a spinner", () => {
  const host = read("./task-drawer-host.tsx");
  assert.match(host, /setReadNonce\(\(n\) => n \+ 1\)/);
  assert.match(host, /\[openID, readNonce, preview\]/);
  assert.doesNotMatch(host, /details\[openID\] \|\| preview/, "a cached detail must not suppress the re-read");
  assert.doesNotMatch(host, /startTransition\(async/, "a rejected read inside a transition hits the error boundary");
  assert.match(host, /\.catch\(\(\) => \{\s*if \(!cancelled\) setReadFailed\(openID\);/);
  assert.match(host, /loadingDetail=\{!detailLoaded && !failed\}/);
  const panel = read("./task-detail-panel.tsx");
  assert.match(panel, /data-testid="ltd-detail-error"/);
  assert.match(panel, /onClick=\{onRetryDetail\}/);
});

test("T3: date chips read DD/MM/YYYY; a one-day span names the day once", () => {
  assert.equal(dateSpanLabel("2026-09-16", "2026-09-16"), "16/09/2026");
  assert.equal(dateSpanLabel("2026-09-01", "2026-09-16"), "01/09/2026 – 16/09/2026");
  assert.equal(dateSpanLabel("2026-09-01", ""), "01/09/2026");
  assert.equal(dateSpanLabel("", ""), "");
  assert.doesNotMatch(read("./leadership-tasks-filters.tsx"), /`\$\{from\} – \$\{to\}`/);
});

test("T3: an unknown person reads as a neutral label, never the raw id", () => {
  const options = [{ value: "u1", label: "Dinakar" }];
  assert.equal(personFilterLabel(options, ["u1"], "Unknown person"), "Dinakar");
  assert.equal(
    personFilterLabel(options, ["u1", "9b2f0c1e-0000-4000-8000-000000000000"], "Unknown person"),
    "Dinakar, Unknown person",
  );
});

test("T4: the board view renders the same empty / no-match state as the list", () => {
  const page = read("./leadership-tasks-page.tsx");
  assert.match(page, /data-testid="lt-board-empty"/);
  assert.equal((page.match(/\{emptyState\}|emptyState\n/g) ?? []).length >= 2, true, "one empty state, used by both views");
  assert.doesNotMatch(page, /\) : null\}\s*<\/>\s*\}\s*list=/, "the board slot must not render null with zero rows");
});

test("T7: attachment kinds are words, and a cancelled task has its own read-only sentence", () => {
  assert.equal(attachmentKindLabel("audio"), "Voice note");
  assert.equal(attachmentKindLabel("photo"), "Photo");
  assert.equal(attachmentKindLabel("video"), "Video");
  assert.equal(attachmentKindLabel("file"), "File");
  assert.equal(attachmentKindLabel("something_new"), "File");
  const rowSrc = read("./task-row.ts");
  assert.doesNotMatch(rowSrc, /assigneeRole: task\.is_assignee/, "role words come from the page contract");
  assert.match(rowSrc, /assigneeRole: "",/);
  assert.doesNotMatch(rowSrc, /attachmentKinds\.join/);
  for (const f of ["./task-detail-panel.tsx", "./edit-task-modal.tsx"]) {
    assert.doesNotMatch(read(f), /file_name \|\| attachment\.kind\b/, f);
  }
  assert.match(read("./task-detail-panel.tsx"), /detail\.read_only_cancelled/);
});

test("Cancelled tasks are listed: the board's Cancelled column takes the server's cancelled rows", () => {
  const live = [row({ id: "a", status: "open" }), row({ id: "c1", status: "cancelled" })];
  const cancelled = [row({ id: "c1", status: "cancelled" }), row({ id: "c2", status: "cancelled" }), row({ id: "x", status: "open" })];
  assert.deepEqual(withCancelledRows(live, cancelled).map((r) => r.id), ["a", "c1", "c2"], "each task once, cancelled rows only");
  assert.deepEqual(withCancelledRows(live, []).map((r) => r.id), ["a", "c1"]);
  const route = read("../../app/(admin)/tasks/page.tsx");
  assert.match(route, /filter: "cancelled"/);
  assert.match(route, /Promise\.all\(\[/);
});

test("Mentioned people can reply: the composer is shown from can_comment alone", () => {
  const composer = read("./task-activity-composer.tsx");
  assert.match(composer, /const composer = task\.canComment \?/);
  assert.doesNotMatch(composer, /isAssignee|is_assignee|is_raiser/);
});

test("New task: a blank deadline day/hour/minute is caught by the form with one clear sentence", () => {
  // 25/09: the browser's bubble fired on the HOUR box ("Please select an item in the list") and
  // nothing said the DAY was missing. The form now checks all three parts and says so once.
  const modal = read("./new-task-modal.tsx");
  assert.match(modal, /\["deadline_date", "deadline_hour", "deadline_minute"\]/);
  assert.match(modal, /setDeadlineMissing\(true\)/);
  const fields = read("./task-write-forms.tsx");
  assert.match(fields, /feedback\.missing_deadline/);
  assert.doesNotMatch(fields, /name="deadline_hour"\s*\n\s*required=/, "the Hour select must not carry native required (its bubble pre-empts the form's check)");
});
