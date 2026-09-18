// The task panel's Activity is three views over ONE backend list (`LeadershipTask.activity`,
// newest first). This renders the production component and pins: the tab set and its counts,
// that each view FILTERS and never re-orders, that a status change draws two chips and an
// arrow from the row's stored KEYS (tone) and LABELS (words), that a comment's text is read
// from the matching note rather than stored twice, and that the empty copy per view comes
// from the contract.
import assert from "node:assert/strict";
import { readFileSync, existsSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import test from "node:test";
import ts from "typescript";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";

import { leadershipTasksFixtureContract as contract } from "./fixture-contract.ts";

const root = fileURLToPath(new URL("../../", import.meta.url));
const require = createRequire(import.meta.url);
const modules = new Map();
function load(filename) {
  if (!path.extname(filename)) filename += existsSync(`${filename}.tsx`) ? ".tsx" : ".ts";
  if (modules.has(filename)) return modules.get(filename).exports;
  const loaded = { exports: {} };
  modules.set(filename, loaded);
  const source = ts.transpileModule(readFileSync(filename, "utf8"), {
    compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText;
  vm.runInThisContext(`(function(require,module,exports){${source}\n})`, { filename })(
    (name) =>
      name.startsWith("@/")
        ? load(path.join(root, name.slice(2)))
        : name.startsWith(".")
          ? load(path.resolve(path.dirname(filename), name))
          : require(name),
    loaded,
    loaded.exports,
  );
  return loaded.exports;
}
const { TaskActivityFeed, activityForView } = load(
  path.join(root, "features/leadership-tasks/task-activity-feed.tsx"),
);

const entry = (over) => ({
  id: "e",
  kind: "created",
  occurred_at: "2026-09-17T03:56:25Z",
  occurred_label: "17/09/2026 09:26",
  actor_user_id: "u",
  actor_name: "Hemant",
  actor_initials: "H",
  from_label: "",
  to_label: "",
  from_value: "",
  to_value: "",
  note_id: "",
  summary: "Hemant created the task",
  ...over,
});
// Newest first, as the backend serves it.
const activity = [
  entry({ id: "e4", kind: "commented", occurred_label: "18/09/2026 10:40", actor_name: "Ravi Teja", actor_initials: "RT", note_id: "n1", summary: "Ravi Teja commented" }),
  entry({ id: "e3", kind: "deadline_changed", occurred_label: "18/09/2026 10:31", actor_name: "Ravi Teja", actor_initials: "RT", from_label: "17/09/2026 17:00", to_label: "20/09/2026 17:00", from_value: "2026-09-17T11:30:00Z", to_value: "2026-09-20T11:30:00Z", summary: "Ravi Teja changed the deadline 17/09/2026 17:00 → 20/09/2026 17:00" }),
  entry({ id: "e2", kind: "status_changed", occurred_label: "18/09/2026 10:30", actor_name: "Ravi Teja", actor_initials: "RT", from_label: "To do", to_label: "In progress", from_value: "open", to_value: "in_progress", summary: "Ravi Teja changed the status To do → In progress" }),
  entry({ id: "e1" }),
];
const notes = [{ note_id: "n1", author_name: "Ravi Teja", body: "Report shared with the lab.", created_at: "2026-09-18T05:10:00Z" }];

const render = (props) =>
  renderToStaticMarkup(React.createElement(TaskActivityFeed, { activity, notes, pageContract: contract, ...props }));

test("the three views filter one list and never re-order it", () => {
  assert.deepEqual(activityForView(activity, "all").map((e) => e.id), ["e4", "e3", "e2", "e1"]);
  assert.deepEqual(activityForView(activity, "history").map((e) => e.id), ["e3", "e2", "e1"]);
  assert.deepEqual(activityForView(activity, "comments").map((e) => e.id), ["e4"]);
});

test("All renders the tabs with counts and every row newest first, worded from the contract", () => {
  const html = render({});
  for (const [tab, count] of [["all", 4], ["history", 3], ["comments", 1]]) {
    assert.match(html, new RegExp(`data-ltd-tab="${tab}"[^>]*>${contract.copy[`activity.tab_${tab}`]}<span class="ltd-feed-count"[^>]*>${count}</span>`));
  }
  assert.match(html, /aria-selected="true" id="[^"]+" aria-controls="[^"]+" class="ltd-feed-tab on" data-ltd-tab="all"/);
  const order = [...html.matchAll(/data-ltd-kind="([a-z_]+)"/g)].map((m) => m[1]);
  assert.deepEqual(order, ["commented", "deadline_changed", "status_changed", "created"]);
  // The verb is the contract's, after the actor's name; the time is the backend's farm-clock label.
  assert.match(html, /<b>Hemant<\/b><span class="ltd-act-verb">created the task<\/span>/);
  assert.match(html, /<time class="ltd-act-when" dateTime="2026-09-17T03:56:25Z">17\/09\/2026 09:26<\/time>/);
  // Avatar initials come from the backend row, never recomputed here.
  assert.match(html, /<span class="ltd-av ltd-av-sm" aria-hidden="true">RT<\/span>/);
});

test("a status change draws the two status chips and an arrow, toned from the stored keys", () => {
  const html = render({ initialView: "history" });
  assert.match(html, /<span class="ltd-actchip ltd-status-warn">To do<\/span><span class="ltd-act-arrow" aria-hidden="true">→<\/span><span class="ltd-actchip ltd-status-info">In progress<\/span>/);
  // A deadline change reads its two labels in plain text.
  assert.match(html, /<span class="ltd-act-val">17\/09\/2026 17:00<\/span><span class="ltd-act-arrow"[^>]*>→<\/span><span class="ltd-act-val">20\/09\/2026 17:00<\/span>/);
  // History hides the comment.
  assert.doesNotMatch(html, /data-ltd-kind="commented"/);
});

test("Comments reads each comment's text from its note and shows the empty copy when there are none", () => {
  const html = render({ initialView: "comments" });
  assert.match(html, /data-ltd-kind="commented"/);
  // The body renders through `note-mentions.ts` (prose runs and mention chips), so the text sits
  // inside the `.ltd-note` paragraph rather than being the paragraph's only child.
  assert.match(html, /<p class="ltd-note"><span>Report shared with the lab\.<\/span><\/p>/);
  assert.doesNotMatch(html, /data-ltd-kind="status_changed"/);
  const empty = render({ initialView: "comments", activity: activity.filter((e) => e.kind !== "commented") });
  assert.match(empty, new RegExp(contract.copy["activity.empty_comments"]));
});

test("the composer is rendered FIRST, under the tabs and above the rows; the older control last", () => {
  // Maintainer, 2026-09-18: "comment box should always be top -- you expect me to scroll all
  // comments to enter a new comment?"
  const html = render({
    composer: React.createElement("div", { id: "composer" }, "compose"),
    older: React.createElement("div", { id: "older" }, "older"),
  });
  assert.ok(html.indexOf('id="composer"') > html.indexOf('class="ltd-feed-tabs"'));
  assert.ok(html.indexOf('id="composer"') < html.indexOf("data-ltd-kind="));
  assert.ok(html.lastIndexOf('id="older"') > html.lastIndexOf("data-ltd-kind="));
});
