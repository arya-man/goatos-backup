// guard: route-fonts (PR #294 F9). The route lane requires exactly the fonts the app renders with.
// Barlow is theme-config's unused `fontFamily.secondary` and is loaded only by Storybook, so the
// route lane must not require it (every route reported `font-not-loaded Barlow`), while the story
// lane, which does load it, keeps it. If an app surface starts using the secondary font, load it in
// the app and add it back here.
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const root = new URL("..", import.meta.url).pathname;
const read = (p) => readFileSync(join(root, p), "utf8");

test("route lane fonts are the fonts the app loads", () => {
  assert.match(read("scripts/smoke-routes-visual.mjs"), /const ROUTE_FONTS = \["Public Sans"\];/);
  assert.match(read("scripts/smoke-stories-visual.mjs"), /const STORY_FONTS = \["Public Sans", "Barlow"\];/);
});

test("no app surface renders with the secondary font while the route lane ignores it", () => {
  const hits = [];
  const walk = (dir) => {
    for (const name of readdirSync(join(root, dir))) {
      const rel = join(dir, name);
      if (statSync(join(root, rel)).isDirectory()) walk(rel);
      else if (/\.(tsx?|css)$/.test(name) && !/\.stories\./.test(name) && /fontSecondaryFamily|Barlow/.test(read(rel))) hits.push(rel);
    }
  };
  for (const dir of ["app", "components", "features", "layouts"]) walk(dir);
  assert.deepEqual(hits, [], "an app surface uses Barlow: load it in the app and require it in ROUTE_FONTS");
});
