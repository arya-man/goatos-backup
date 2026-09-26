// Guard: info-hint-tap-target. The shared "i" glyph (InfoHint) sits on KPI cards, card headers and
// page headers across every page; at 24px it failed the 390px 44x44 tap rule on /protocol-adherence,
// /vaccination and others (r2 audit "Tap target < 44px at 390: span[role=img]"). Its phone box must be
// >= 44px (negative margin keeps the layout footprint); the desktop size stays the glyph size.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./info-hint.tsx", import.meta.url), "utf8");

test("info-hint-tap-target: phone tap box is at least 44px", () => {
  assert.match(source, /width:\s*\{\s*xs:\s*Math\.max\(size,\s*44\)/);
  assert.match(source, /height:\s*\{\s*xs:\s*Math\.max\(size,\s*44\)/);
  assert.match(source, /m:\s*\{\s*xs:\s*size < 44/);
});
