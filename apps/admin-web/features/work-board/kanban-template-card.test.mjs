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
  assert.match(tasks, /className="ltb-colempty" sx=\{visuallyHidden\}/);
  assert.match(board, /sx=\{visuallyHidden\}>\{copy\(pageContract, "lane\.empty"\)\}/);
});

test("guard: tasks-new-task-reason -- no dead New task: without assignees the slot shows the reason as text", () => {
  // TR1-#24 put the reason in a tap tooltip on a grey button; J2 P1-1 (DECIDED no dead controls):
  // a disabled primary is still a dead control. Without assignable people the action slot renders
  // the backend reason as visible text, and the button renders only when it can open the dialog.
  const modal = read("../leadership-tasks/new-task-modal.tsx");
  assert.match(modal, /\{assignees\.length \? \(\s*<Button/, "New task renders only when someone can be assigned");
  assert.doesNotMatch(modal, /disabled=\{!assignees\.length\}/, "never a disabled New task");
  assert.match(modal, /text\("new\.no_assignees"/, "the reason is visible text from the contract");
  const service = read("../../../../backend/internal/adminui/app/service.go");
  assert.match(service, /"new\.no_assignees":/, "backend owns the reason copy");
});

// TR2-P2-13: long titles / captions were cut with an ellipsis ("Milk preparation · Coi…").
test("guard: work-board-card-wraps -- the work-board card title and caption wrap, never ellipsis", () => {
  const board = read("./work-board-board.tsx");
  const card = board.slice(board.indexOf("function WorkCard("), board.indexOf("// Board presentation"));
  assert.match(card, /<ItemName name=\{row\.title\} title=\{row\.title\} noWrap=\{false\}/);
  assert.doesNotMatch(card, /variant="caption" noWrap/);
});
