// The EMPTY Cancelled column is a slim RAIL (title + count, ~56px) and widens the moment it holds
// a card; the other columns never collapse. This renders the production TaskBoardColumns
// through react-dom/server and pins the `data-ltb-rail` / `.is-rail` decision on both sides, plus
// that the rail keeps the header block (title + count pill) and drops the body.
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
      name === "@grafana/faro-web-sdk"
        ? { faro: { api: { pushEvent() {}, pushError() {} } } }
        : name.startsWith("@/")
          ? load(path.join(root, name.slice(2)))
          : name.startsWith(".")
            ? load(path.resolve(path.dirname(filename), name))
            : require(name),
    loaded,
    loaded.exports,
  );
  return loaded.exports;
}
const { TaskBoardColumns } = load(path.join(root, "features/leadership-tasks/task-board-dnd.tsx"));

const task = (over) => ({
  id: "11111111-1111-4111-8111-111111111111",
  number: "#18",
  title: "Check the west fence",
  body: "",
  comment: "",
  canComment: false,
  canEdit: false,
  rowVersion: 1,
  status: "open",
  statusLabel: "To do",
  statusOptions: [],
  assignee: "Satish",
  assigneeRole: "Park Head",
  assigneeUserID: "a",
  raisedBy: "Manju",
  raisedByUserID: "r",
  raisedOn: "17/09/2026",
  age: "1 day",
  attachments: 0,
  attachmentRows: [],
  evidence: "",
  deadlineLabel: "",
  deadlineTone: undefined,
  daysLeft: null,
  deadlineStateLabel: "",
  notes: [],
  activity: [],
  ...over,
});
const columns = [
  { key: "open", label: "To do", total: 172, emptyMessage: "Nothing here", focusHref: null },
  { key: "in_progress", label: "In progress", total: 119, emptyMessage: "Nothing here", focusHref: null },
  { key: "done", label: "Done", total: 117, emptyMessage: "Nothing here", focusHref: null },
  { key: "cancelled", label: "Cancelled", total: null, emptyMessage: "Nothing here", focusHref: null },
];
const render = (rows, activeFilter = "all") =>
  renderToStaticMarkup(
    React.createElement(TaskBoardColumns, {
      pageContract: contract,
      columns,
      rows,
      cardHrefs: Object.fromEntries(rows.map((r) => [r.id, `/tasks?task=${r.id}`])),
      activeFilter,
      placeholder: "—",
      action: async () => {},
      returnTo: "/tasks",
    }),
  );

test("an empty Cancelled column is a rail that keeps its header and drops its body", () => {
  const html = render([task({ status: "open" })]);
  const rail = html.match(/<section class="ltb-col ltb-col-cancelled is-rail[^"]*"[^>]*data-ltb-rail="true"[^>]*aria-label="Cancelled"[^>]*>([\s\S]*?)<\/section>/);
  assert.ok(rail, `no rail in: ${html.slice(0, 400)}`);
  assert.match(rail[1], /class="ltb-colname">Cancelled</);
  assert.match(rail[1], /class="ltb-colcount"[^>]*>—</);
  // The body and the "0 on this page" line are STILL rendered (the drop target's markup does not
  // change) and hidden by the rail's CSS; what must not appear is a second header treatment.
  assert.equal((rail[1].match(/ltb-colhd/g) || []).length, 1);
  // An empty To do column is never a rail: an empty To do is news, not noise.
  const openOnly = render([task({ status: "done" })]);
  assert.doesNotMatch(openOnly, /ltb-col-open is-rail/);
  assert.match(openOnly, /ltb-col-cancelled is-rail/);
});

test("the Cancelled column widens the moment it holds a card, and never rails while it is the focused filter", () => {
  const withCard = render([task({ status: "cancelled", statusLabel: "Cancelled" })]);
  assert.doesNotMatch(withCard, /is-rail/);
  assert.doesNotMatch(withCard, /data-ltb-rail/);
  assert.match(withCard, /ltb-col ltb-col-cancelled[^"]*"[\s\S]*?ltb-card/);
  const focused = render([], "cancelled");
  assert.doesNotMatch(focused, /is-rail/);
});
