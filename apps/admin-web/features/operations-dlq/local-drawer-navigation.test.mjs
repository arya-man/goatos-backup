import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./index.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./dlq-local-drawer.tsx", import.meta.url), "utf8");

test("DLQ record drawers open locally without a route/RSC navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /#dlq_id=/);
  assert.match(pageSource, /DLQLocalDrawer/);
  assert.match(drawerSource, /useLocalOverlaySelection/);
  assert.match(drawerSource, /selectionKey: "dlq_id"/);
  // Template MinimalDrawer (portal + theme backdrop) via DetailDrawer; backdrop/X/Escape -> closeDrawer.
  assert.match(drawerSource, /<DetailDrawer/);
  assert.match(drawerSource, /onClose=\{closeDrawer\}/);
  assert.doesNotMatch(drawerSource, /className=\{`(scrim|drawer)/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
});
