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

// guard: info-tip-tap (REVIEW-15 O19). No hand-made "i" button may come back: a local Tooltip around
// an .ihelp / "i" button opens on hover only, so a tap in the Android WebView does nothing
// (/herd-signals column help). Every info "i" is components/app/info-tip.tsx.
test("no local hover-only info buttons: every info tip is the shared InfoTip", async () => {
  const { readdirSync, statSync } = await import("node:fs");
  const { join } = await import("node:path");
  const root = new URL("../../", import.meta.url).pathname;
  const offenders = [];
  const walk = (dir) => {
    for (const name of readdirSync(dir)) {
      if (name === "node_modules" || name === ".next" || name === "minimal") continue;
      const abs = join(dir, name);
      if (statSync(abs).isDirectory()) walk(abs);
      else if (/\.tsx$/.test(name) && !abs.endsWith("components/app/info-tip.tsx")) {
        const text = readFileSync(abs, "utf8");
        if (/className=["'][^"']*\b(ihelp|tipwrap)\b/.test(text)) offenders.push(`${abs.slice(root.length)} (.ihelp/.tipwrap)`);
        if (/function\s+Info(Tip|Tooltip)\s*\(/.test(text)) offenders.push(`${abs.slice(root.length)} (local InfoTip)`);
        if (/<Tooltip\b[^>]*>\s*(<span[^>]*>\s*)?<(button|IconButton)[^>]*>\s*i\s*</.test(text)) offenders.push(`${abs.slice(root.length)} (Tooltip "i" button)`);
      }
    }
  };
  for (const dir of ["features", "components", "app"]) walk(join(root, dir));
  assert.deepEqual(offenders, []);
  // self-test: the patterns catch the shapes they are meant to catch.
  assert.match('<Tooltip title={t}><span className="tipwrap"><button className="ihelp">i</button></span></Tooltip>', /className=["'][^"']*\b(ihelp|tipwrap)\b/);
  assert.match("function InfoTip({ label }) {", /function\s+Info(Tip|Tooltip)\s*\(/);
  assert.match('<Tooltip title={t}><button type="button">i</button></Tooltip>', /<Tooltip\b[^>]*>\s*(<span[^>]*>\s*)?<(button|IconButton)[^>]*>\s*i\s*</);
});
