import assert from "node:assert/strict";
import test from "node:test";

import { worklistFilterIsStaged, worklistFilterSearchKey } from "./worklist-filter-draft.ts";

// A staged filter bar is only useful if Apply is honest about whether there is anything to apply.
// Enabled when nothing changed re-renders the page for the same question; disabled when something
// did leaves the operator's pick stranded with no way to commit it — the worse of the two, and the
// exact complaint the staged bar exists to answer.

test("nothing touched means nothing to apply", () => {
  assert.equal(worklistFilterIsStaged(null, "fc_tag=Kid", "fc_offset"), false);
});

test("a changed dropdown enables Apply", () => {
  assert.equal(worklistFilterIsStaged("fc_tag=Adult", "fc_tag=Kid", "fc_offset"), true);
});

test("a filter cleared from the draft enables Apply", () => {
  assert.equal(worklistFilterIsStaged("", "fc_tag=Kid", "fc_offset"), true);
});

test("changing back to the applied value disables Apply again", () => {
  assert.equal(worklistFilterIsStaged("fc_tag=Kid", "fc_tag=Kid", "fc_offset"), false);
});

// The draft always drops the offset, because applying resets paging. Comparing raw query strings
// would light Apply up on a bar whose filters are untouched and whose reader is simply on page 3.
test("the page parameter alone is not a staged change", () => {
  assert.equal(worklistFilterIsStaged("fc_tag=Kid", "fc_tag=Kid&fc_offset=20", "fc_offset"), false);
});

test("each bar excludes only its OWN page parameter", () => {
  // The experiment bar shares the page with the ration grid; the grid's offset is a real difference
  // it must not silently discard when it commits.
  assert.equal(
    worklistFilterIsStaged("fc_exp_arm=A", "fc_exp_arm=A&fc_offset=20", "fc_exp_offset"),
    true,
  );
});

// Order-insensitive: the draft is rebuilt by mutation, so `delete` + `append` moves a repeated
// parameter to the end. A re-ordered but identical set is not a change the operator made.
test("re-ordered parameters are the same question", () => {
  assert.equal(
    worklistFilterIsStaged("fc_tag=Pregnant&fc_breed=Boer", "fc_breed=Boer&fc_tag=Pregnant", "fc_offset"),
    false,
  );
});

test("the same multi-select picks in the other order are the same question", () => {
  assert.equal(
    worklistFilterIsStaged("fc_item=Maize&fc_item=Bhusa", "fc_item=Bhusa&fc_item=Maize", "fc_offset"),
    false,
  );
});

test("dropping one of two multi-select picks enables Apply", () => {
  assert.equal(worklistFilterIsStaged("fc_item=Maize", "fc_item=Bhusa&fc_item=Maize", "fc_offset"), true);
});

// A value that legitimately contains the delimiter must not be able to look like two parameters —
// feed items are free text ("RGS/Vijay Concentrate"), which is why the URL carries repeated
// parameters rather than a joined encoding in the first place.
test("a delimiter inside a value does not forge a key boundary", () => {
  assert.notEqual(
    worklistFilterSearchKey("fc_item=a%26fc_tag%3Db", "fc_offset"),
    worklistFilterSearchKey("fc_item=a&fc_tag=b", "fc_offset"),
  );
});
