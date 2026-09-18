// The assignee picker is ONE component with two hosts. The Work Board keeps the control it
// shipped (the `multi` filter), and the Tasks desk's "For" field is the same anatomy as a form
// field (`single`). These pin the extraction: the board imports the shared path, its markup is
// intact, and the single mode carries what a form host needs.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const picker = readFileSync(new URL("./assignee-picker.tsx", import.meta.url), "utf8");
const board = readFileSync(new URL("../features/work-board/work-board-board.tsx", import.meta.url), "utf8");

test("the Work Board imports the shared picker and keeps its filter behaviour", () => {
  assert.match(board, /import \{ AssigneePicker \} from "@\/components\/assignee-picker"/);
  assert.doesNotMatch(board, /function AssigneePicker/, "no second copy on the board");
  assert.match(board, /<AssigneePicker mode="multi"/);
  // The owner still writes the URL through the board's own param writer.
  assert.match(board, /onSelect=\{\(id\) => write\(\(p\) => setParam\(p, pageContract, PARAM_OWNER, id\)\)\}/);
  // The multi mode is the board's markup: avatar stack, +N chip, ticked-when-all rows, per-owner
  // counts, and "Select all" as the FIRST checkbox row (ticked while nobody is picked) -- there
  // is no foot action any more.
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
