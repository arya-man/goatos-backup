import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./inline-cell-editor.tsx", import.meta.url), "utf8");

test("the tag editor is the template popover, anchored fresh from the cell on every open", () => {
  // MUI places the popover against the anchor each time it opens, so no stale coordinates can paint.
  assert.match(source, /<CustomPopover\s+open=\{open\}\s+anchorEl=\{anchorEl\}\s+onClose=\{close\}/);
  assert.match(source, /setAnchorEl\(anchor\);\s*setPhase\(\{ kind: "picking" \}\)/);
  assert.match(source, /function close\(\) \{[\s\S]*setAnchorEl\(null\);[\s\S]*\}/);
  assert.doesNotMatch(source, /createPortal|popStyle/);
});
