import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

// guard: ask-mesha-docked (TR1-#12, TR1-#13). Ask Mesha lives in the header at every width as a template
// header IconButton. At 390 the old floating 56px bubble covered page content (the /feed/config edit
// pencil, a /vaccination KPI card) and sat above the open phone menu; on some routes the header slot
// was looked up once, before the header committed, so the launcher was missing from the header.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
const panel = read("./ceo-ai-panel.tsx");
const shell = read("../../components/mesha-shell.tsx");
const frame = read("../../app/frame.css");

test("the launcher docks into the header slot at every width", () => {
  assert.match(panel, /\{dockSlot\s*\?\s*createPortal\(/, "dock whenever the shell offers a slot");
  assert.doesNotMatch(panel, /dockSlot && !narrow/, "no phone-only floating bubble under the shell");
  assert.doesNotMatch(frame, /\.topbar-ai-slot\{display:none\}/, "the header slot is never hidden at phone width");
  assert.match(shell, /<span id="topbar-ai-slot"/, "the shell header renders the slot");
});

test("the dock is a template header IconButton, not a filled green bubble", () => {
  const dock = panel.slice(panel.indexOf("{dockSlot"), panel.indexOf("dockSlot,\n"));
  assert.match(dock, /<IconButton/);
  assert.doesNotMatch(dock, /mzai-bubble/);
});

test("the slot lookup follows the live header (no one-time lookup)", () => {
  assert.match(panel, /new MutationObserver\(resolve\)/);
  assert.match(panel, /current\?\.isConnected/);
});

test("no bottom padding is reserved for a floating bubble", () => {
  assert.doesNotMatch(shell, /--layout-dashboard-content-pb/);
});
