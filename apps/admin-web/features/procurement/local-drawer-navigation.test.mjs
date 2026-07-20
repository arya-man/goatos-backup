import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const boardSource = readFileSync(new URL("./source-entry-board.tsx", import.meta.url), "utf8");
const drawerSource = readFileSync(new URL("./source-entry-local-drawer.tsx", import.meta.url), "utf8");

test("Procurement source-load records open and close locally without route navigation", () => {
  assert.match(boardSource, /LocalOverlayLink/);
  assert.match(boardSource, /SourceEntryLocalDrawer/);
  assert.match(drawerSource, /LOCAL_OVERLAY_URL_CHANGE_EVENT/);
  assert.match(drawerSource, /popstate/);
  assert.match(drawerSource, /currentHistoryEntryIsLocalOverlay/);
  assert.doesNotMatch(drawerSource, /<Link[^>]+className="veil"/);
  assert.match(drawerSource, /<Link href=\{displayedItem\.detailHref\}/);
  assert.match(drawerSource, /detailHref.*#hf-evidence/);
});
