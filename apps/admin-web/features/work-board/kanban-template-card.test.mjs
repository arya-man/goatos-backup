import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// TR1-#24 (/work-board and /tasks vs the template kanban): cards carried a strip of small chips
// (module, park, clock) and a hand-made progress bar, and empty columns showed a dashed
// "Nothing here" box. The template item is priority arrow + name + ItemInfo row; an empty column is
// the bare list.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");

test("guard: kanban-template-card -- work-board card is the template item anatomy", () => {
  const board = read("./work-board-board.tsx");
  const card = board.slice(board.indexOf("function WorkCard("), board.indexOf("// Board presentation"));
  assert.ok(card.length > 0);
  assert.doesNotMatch(card, /<Label\b|<Chip\b|ClockLabel|WorkProgress/, "no chip strip or progress bar on the card");
  assert.match(card, /<ItemStatus status=\{cardStatus\(row\)\} \/>/);
  assert.match(card, /<ItemName name=\{row\.title\}/);
  assert.match(card, /<ItemInfo assignee=\{assignee\}/);
  assert.match(card, /moduleOpt\?\.label \?\? row\.module, parkLabel\(/, "module + park stay on the card as caption words");
});

test("guard: kanban-template-card -- empty columns are the bare template list", () => {
  const board = read("./work-board-board.tsx");
  const tasks = read("../leadership-tasks/task-board-dnd.tsx");
  for (const [name, src] of [["work-board", board], ["tasks", tasks]]) {
    assert.doesNotMatch(src, /borderStyle: "dashed", borderColor: "divider"|borderStyle: "dashed",\s*borderColor: "divider"/, `${name}: no dashed empty-column box`);
  }
  assert.match(tasks, /className="ltb-colempty sr-only"/);
  assert.match(board, /className="sr-only">\{copy\(pageContract, "lane\.empty"\)\}/);
});

test("guard: tasks-new-task-reason -- a disabled New task says why", () => {
  // TR1-#24: /tasks "New task" rendered grey with no reason when the assignees read came back
  // empty; a reviewer read it as a broken style. Disabled stays, but the reason is on hover + tap.
  const modal = read("../leadership-tasks/new-task-modal.tsx");
  assert.match(modal, /<Tooltip title=\{assignees\.length \? "" : text\("new\.no_assignees"/);
  assert.match(modal, /enterTouchDelay=\{0\}/, "opens on tap in the webview");
  assert.match(modal, /disabled=\{!assignees\.length\}/);
  const service = read("../../../../backend/internal/adminui/app/service.go");
  assert.match(service, /"new\.no_assignees":/, "backend owns the reason copy");
});
