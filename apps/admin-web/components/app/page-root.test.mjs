// guard: page-root-sx (FIXJ3). The template page root keeps the page column's 24px grid rhythm as
// theme sx (the legacy frame.css `.wrap>.screen` grid) and carries no legacy class; its skeleton
// twin renders the same root, and the r2 audit anchors on [data-page-root] like `.screen`.
import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const read = (rel) => readFileSync(new URL(`../../${rel}`, import.meta.url), "utf8");

test("PageRoot is the sx page grid with the data-page-root hook and no legacy class", () => {
  const src = read("components/app/page-root.tsx");
  assert.match(src, /display: "grid"/);
  assert.match(src, /gap: "var\(--sp-3\)"/, "the 24px page gap");
  assert.match(src, /minWidth: 0/);
  assert.match(src, /data-page-root=""/);
  assert.doesNotMatch(src, /className=/, "no legacy screen/on class behind the wrapper");
});

test("the skeleton twin and the audit know the PageRoot root", () => {
  assert.match(read("components/app/skeletons/blocks.tsx"), /root === "page-root"[\s\S]*?data-page-root=""[\s\S]*?PAGE_ROOT_SX/);
  assert.match(read("scripts/r2-audit-checks/page-rhythm.mjs"), /\[data-page-root\]/);
  assert.match(read("scripts/r2-skeleton-iou.mjs"), /\[data-page-root\]/);
  assert.match(read("scripts/r2-visual-audit.mjs"), /PAGE_WRAPPER_SELECTOR = "[^"]*\[data-page-root\]/);
});
