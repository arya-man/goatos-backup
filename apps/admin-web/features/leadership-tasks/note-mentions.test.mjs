import assert from "node:assert/strict";
import { test } from "node:test";

import { segmentNoteBody } from "./note-mentions.ts";

const MANJU = { user_id: "u-manju", name: "Manju" };
const RAVI = { user_id: "u-ravi", name: "Ravi Teja" };
const RAVI_K = { user_id: "u-ravi-k", name: "Ravi" };

test("a stored mention draws as a chip, the rest stays prose", () => {
  assert.deepEqual(segmentNoteBody("@Manju all done", [MANJU]), [
    { kind: "mention", text: "@Manju", user_id: "u-manju" },
    { kind: "text", text: " all done" },
  ]);
  assert.deepEqual(segmentNoteBody("ping @Manju, thanks", [MANJU]), [
    { kind: "text", text: "ping " },
    { kind: "mention", text: "@Manju", user_id: "u-manju" },
    { kind: "text", text: ", thanks" },
  ]);
});

test("no stored mentions means plain text, never a regex guess", () => {
  assert.deepEqual(segmentNoteBody("@Manju all done"), [{ kind: "text", text: "@Manju all done" }]);
  assert.deepEqual(segmentNoteBody(""), []);
  // A "@" inside an address is not a person even when a stored name happens to follow it.
  assert.deepEqual(segmentNoteBody("mail ravi@Manju.example", [MANJU]), [
    { kind: "text", text: "mail ravi@Manju.example" },
  ]);
});

test("the longest name wins, so a two-word name is one chip", () => {
  assert.deepEqual(segmentNoteBody("@Ravi Teja and @Ravi", [RAVI_K, RAVI]), [
    { kind: "mention", text: "@Ravi Teja", user_id: "u-ravi" },
    { kind: "text", text: " and " },
    { kind: "mention", text: "@Ravi", user_id: "u-ravi-k" },
  ]);
  // "@Manjula" is not "@Manju": the token must end at a boundary.
  assert.deepEqual(segmentNoteBody("@Manjula please", [MANJU]), [
    { kind: "text", text: "@Manjula please" },
  ]);
});
