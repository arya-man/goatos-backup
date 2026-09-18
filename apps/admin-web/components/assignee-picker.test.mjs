// The assignee picker is ONE component with two hosts. The Work Board keeps the control it
// shipped (the `multi` filter), and the Tasks desk's "For" field is the same anatomy as a form
// field (`single`). These pin the extraction: the board imports the shared path, its markup is
// intact, and the single mode carries what a form host needs.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const picker = readFileSync(new URL("./assignee-picker.tsx", import.meta.url), "utf8");
const board = readFileSync(new URL("../features/work-board/work-board-board.tsx", import.meta.url), "utf8");

test("the Work Board hosts the Tasks people dropdown; the shared picker keeps its multi mode", () => {
  // Ravi, 2026-09-19: the board's owner filter is the Tasks page's dropdown, not the avatar stack.
  assert.match(board, /import \{ TaskPeopleDropdown \} from "@\/features\/leadership-tasks\/task-people-dropdown"/);
  assert.doesNotMatch(board, /AssigneePicker/, "the avatar-stack picker is off the board");
  assert.match(board, /<TaskPeopleDropdown slot="assignee"/);
  assert.match(board, /allLabel=\{copy\(pageContract, "filter\.assignee\.all"\)\}/);
  // The owner still writes the URL through the board's own param writer, ONE owner at a time.
  assert.match(board, /setParam\(p, pageContract, PARAM_OWNER, next\.find\(\(id\) => id !== selectedOwner\)\)/);
  // The multi mode stays intact for any other host: ticked-when-all rows, per-owner counts, and
  // "Select all" as the FIRST checkbox row (ticked while nobody is picked); no foot action.
  assert.match(picker, /className=\{`av\$\{o\.id === selected \? " on" : ""\}`\}/);
  assert.match(picker, /\+\{overflow\}/);
  assert.match(picker, /const on = selected \? o\.id === selected : true;/);
  assert.match(picker, /\{cardsByOwner\[o\.id\] \?\? 0\} \{labels\.rows\}/);
  assert.doesNotMatch(picker, /className="opt foot"/);
  assert.match(picker, /className=\{`opt all\$\{!selected \? " on" : ""\}`\}/);
  assert.match(picker, /\{labels\.selectAll\}/);
  // Every tick is the lucide Check on the brand box, never a glyph that can inherit the fill colour.
  assert.doesNotMatch(picker, /"✓"/);
});

test("single mode is a form field: hidden id, Name — Title rows, Enter picks, one Escape layer", () => {
  assert.match(picker, /mode\?: "multi" \| "single"/);
  assert.match(picker, /\{name \? <input type="hidden" name=\{name\} value=\{selected \?\? ""\} \/> : null\}/);
  assert.match(picker, /<b className="avs-name">\{o\.name\}<\/b>/);
  assert.match(picker, /avs-title"> — \{o\.title\}/);
  // Typing matches the title too, only in single mode.
  assert.match(picker, /mode === "single" && \(o\.title \?\? ""\)\.toLowerCase\(\)\.includes\(q\)/);
  assert.match(picker, /if \(e\.key === "Enter"\) \{\s*e\.preventDefault\(\);\s*const row = shown\[highlight\];\s*if \(row\) pick\(row\.id\);/);
  // Escape is caught on the document in the CAPTURE phase and stopped, so a dialog shell on the
  // same document never hears the press that closed the list.
  assert.match(picker, /document\.addEventListener\("keydown", onKey, true\)/);
  assert.match(picker, /event\.stopPropagation\(\);\s*close\(\);/);
  // No label is a local literal: every string comes in through `labels`.
  assert.doesNotMatch(picker, /placeholder="[A-Za-z]/);
});
