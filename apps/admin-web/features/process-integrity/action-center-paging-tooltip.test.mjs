// guard: action-center-paging-tooltip (R3OPS-3). The "board shows one page; search covers visible
// cards" note was a loose line of body text above the lanes. It is a template info tooltip on a 44px
// IconButton beside the visible-cards search, shown only when there is a next page, and opens on tap.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const src = readFileSync(new URL("./action-center.tsx", import.meta.url), "utf8");

test("board paging note is an info tooltip, never inline text", () => {
  assert.match(src, /\{boardNextCursor \? <InfoTip title=\{copy\(pageContract, "note\.board_paging"\)\} testId="ac-board-paging-info" \/> : null\}/);
  const uses = src.match(/"note\.board_paging"/g) ?? [];
  assert.equal(uses.length, 1, "the note appears only as the tooltip");
  assert.ok(src.indexOf("note.board_paging") > src.indexOf("<VisibleTableSearch"), "the tooltip sits beside the visible-cards search");
});

// guard: info-tip-tap. The shared InfoTip opens on a tap (controlled; MUI's own touch listener is off
// because a zero-delay touch open is cancelled by touchend), carries a 44px target and its text as the
// accessible name.
test("InfoTip opens on click/tap and is a 44px named IconButton", () => {
  const tip = readFileSync(new URL("../../components/app/info-tip.tsx", import.meta.url), "utf8");
  assert.match(tip, /^"use client";/);
  assert.match(tip, /open=\{open\}[\s\S]*disableTouchListener/);
  assert.match(tip, /onClick=\{\(\) => setOpen\(true\)\}/);
  assert.match(tip, /<ClickAwayListener onClickAway=\{\(\) => setOpen\(false\)\}>/);
  assert.match(tip, /aria-label=\{title\}/);
  assert.match(tip, /width: "var\(--tap-min\)", height: "var\(--tap-min\)"/);
});
