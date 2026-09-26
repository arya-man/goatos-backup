import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const pageSource = readFileSync(new URL("./audit-log.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./audit-log-local-drawer.tsx", import.meta.url), "utf8");

test("audit record drawers open locally without a route/RSC navigation", () => {
  assert.match(pageSource, /LocalOverlayLink/);
  assert.match(pageSource, /#audit_id=/);
  assert.match(pageSource, /AuditLogLocalDrawer/);
  assert.match(drawerSource, /useLocalOverlaySelection/);
  assert.match(drawerSource, /selectionKey: "audit_id"/);
  // Template MinimalDrawer (portal + theme backdrop) via DetailDrawer; backdrop/X/Escape -> closeDrawer.
  assert.match(drawerSource, /<DetailDrawer/);
  assert.match(drawerSource, /onClose=\{closeDrawer\}/);
  assert.doesNotMatch(drawerSource, /className=\{`(scrim|drawer)/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
});
