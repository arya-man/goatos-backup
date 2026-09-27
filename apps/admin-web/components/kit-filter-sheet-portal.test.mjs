import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const filterBar = readFileSync(new URL("./app/filter-bar.tsx", import.meta.url), "utf8");

// The fold is the template filters drawer (MUI Drawer = portaled to body, anchored to the viewport),
// with its own padded body so the fields never touch the screen edge.
test("FilterBar phone fold uses the portaled template filters drawer", () => {
  assert.match(filterBar, /import \{ MinimalDrawer \} from "@\/components\/app\/drawer";/);
  assert.match(filterBar, /<MinimalDrawer open=\{open\}[\s\S]*?<Box sx=\{\{ p: 2\.5,/);
});
