import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const board = readFileSync(new URL("./shed-board.tsx", import.meta.url), "utf8");
const service = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

// pr294 L-C10 (1440): the pen table ran 1,235px in a 1,060px card -- "Assignment" cut at the edge and
// Status off the card. The Assignment column restated the operator names as "2 operators".
test("vaccination by pen has no separate Assignment column", () => {
  assert.match(service, /tableP\("shed-summary", [^\n]*"next_due", "manager", "status"\}/);
  assert.doesNotMatch(board, /assignmentLabel\(/);
  assert.doesNotMatch(board, /label\.assignment/);
});

test("count headers may wrap so the pen table fits the 1440 card", () => {
  assert.match(board, /NUMERIC_COLUMNS\.has\(col\.key\) \? \{ whiteSpace: "normal", maxWidth: 96 \}/);
});

// pr294 L-C10: two unlabelled pill strips both offered "Capacity action" / "Split".
test("each pill strip names what it filters", () => {
  assert.match(board, /copy\(pageContract, "label\.strip_status"\)/);
  assert.match(board, /copy\(pageContract, "label\.strip_capacity"\)/);
  assert.match(service, /"label\.strip_status":\s+"Pen status"/);
});

// pr294 L-C10 / N7: a lone (i) sat in an empty band between the last row and the pager.
test("the pen counts note rides the section header tip, not its own band", () => {
  assert.doesNotMatch(board, /<InfoHint text=\{copy\(pageContract, "note\.sheds_counts"\)\}/);
  assert.match(board, /section\.sheds\.note"\)\} \$\{copy\(pageContract, "note\.sheds_counts"\)\}/);
});
