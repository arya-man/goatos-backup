import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { SOP_EDITOR_PARAMS, isSopEditorUrl } from "./sop-library-layout.ts";

const read = (rel) => readFileSync(new URL(rel, import.meta.url), "utf8");

// guard: sop-editor-predicate (REVIEW-37 #2). The page opened the builder on compose=1 || new=1, the
// loading.tsx on compose=1 || edit, the URL-panel fallback on compose only: ?new=1 flashed the library
// skeleton and ?edit=X (the library) the editor skeleton. One predicate now serves all three.
test("isSopEditorUrl: compose=1 or new=1 opens the builder; edit alone does not", () => {
  const q = (s) => new URLSearchParams(s);
  assert.equal(isSopEditorUrl(q("compose=1")), true);
  assert.equal(isSopEditorUrl(q("new=1")), true);
  assert.equal(isSopEditorUrl(q("compose=1&edit=abc")), true);
  assert.equal(isSopEditorUrl(q("edit=abc")), false);
  assert.equal(isSopEditorUrl(q("")), false);
  assert.deepEqual([...SOP_EDITOR_PARAMS], ["compose", "new"]);
});

test("page, URL-panel fallback and loading shape share the predicate", () => {
  const page = read("./module-page.tsx");
  assert.match(page, /isSopEditorUrl\(/);
  assert.match(page, /fallbackBy=\{\{ param: SOP_EDITOR_PARAMS\.join\("\|"\)/);
  assert.doesNotMatch(page, /sp\.compose === "1"/);
  const skel = read("./sop-route-skeleton.tsx");
  assert.match(skel, /isSopEditorUrl\(pending \?\? current\)/);
  assert.doesNotMatch(skel, /sp\.get\("edit"\)/);
  // UrlPanel reads "a|b" as any-of-these-params = "1"
  assert.match(read("../../components/app/url-panel.tsx"), /shapeParam\.split\("\|"\)/);
});
