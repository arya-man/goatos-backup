import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import { publishedFromSearch, publishedHref } from "./published-href.ts";

const followUp = readFileSync(new URL("./followup-editor.tsx", import.meta.url), "utf8");
const inspection = readFileSync(new URL("./inspection-editor.tsx", import.meta.url), "utf8");
const weighing = readFileSync(new URL("./weighing-editor.tsx", import.meta.url), "utf8");
const builder = readFileSync(new URL("./sop-builder.tsx", import.meta.url), "utf8");
const library = readFileSync(new URL("./sop-library.tsx", import.meta.url), "utf8");
const modulePage = readFileSync(new URL("./module-page.tsx", import.meta.url), "utf8");

// Maintainer report 2026-09-15: "on top I'm seeing v3 published but nothing is changing
// visually, so I can't tell whether my change is reflected". Publish must CLOSE the editor and
// the library must say which version went live and light up that card.
test("every SOP editor leaves for the library on a successful publish", () => {
  for (const [name, src] of [["followup", followUp], ["inspection", inspection], ["builder", builder], ["weighing", weighing]]) {
    assert.match(src, /router\.push\(publishedHref\(basePath, /, `${name}: publish must navigate back to the library`);
  }
  // No editor is left standing after a publish: a refresh alone keeps the editor open.
  assert.doesNotMatch(inspection, /if \(res\.ok && publish\) router\.refresh\(\);/);
});

test("the library announces the published version and lights that card", () => {
  assert.match(modulePage, /published=\{publishedFromSearch\(sp\)\}/);
  assert.match(library, /notice\.published\.title/);
  assert.match(library, /sop-just-published/);
  assert.match(library, /sop-published-banner/);
});

test("publishedHref round-trips through the library's search params", () => {
  const href = publishedHref("/counts/sops", "sop-1", 3);
  assert.equal(href, "/counts/sops?published=sop-1&v=3");
  assert.deepEqual(publishedFromSearch({ published: "sop-1", v: "3" }), { sopId: "sop-1", version: 3 });
  assert.deepEqual(publishedFromSearch({ published: "sop-1" }), { sopId: "sop-1", version: null });
  assert.equal(publishedFromSearch({}), null);
  assert.equal(publishedHref("/procurement/sops", "sop-2"), "/procurement/sops?published=sop-2");
});

// Review finding on PR 267: /milk/sops and /weighing/sops were missing, so a publish from those
// modules landed on a still-cached library with the old version on the card.
test("every module SOP route the sidebar serves is revalidated after a SOP mutation", () => {
  const actions = readFileSync(new URL("./sop-actions.ts", import.meta.url), "utf8");
  const service = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
  const served = [...new Set([...service.matchAll(/navLeaf(?:Domain)?\("[^"]+", "[^"]+", "(\/[a-z]+\/sops)"/g)].map((m) => m[1]))].sort();
  const listed = [...actions.match(/const SOP_PAGE_PATHS = \[([^\]]+)\]/)[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]).sort();
  assert.deepEqual(listed, served);
});
