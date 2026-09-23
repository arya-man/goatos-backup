import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./health-config.tsx", import.meta.url), "utf8");

// The Treatment <-> Diagnosis strip shipped as a bare <a href>, which is a DOCUMENT
// navigation: the browser tore down the shell, the sidebar and every bit of client state
// to swap one panel, and the maintainer reported it as "whole page is loading".
//
// The tabs still NAVIGATE -- they read different server data (the protocol catalog vs the
// registers), which `docs/decisions/admin-web-interaction-patterns.md` rule 3 calls a
// data-changing tab -- so the fix is not client state. It is the console's own Link, which
// makes the click an RSC transition of this segment alone.
test("the rulebook tabs navigate through the console's Link, never a bare anchor", () => {
  assert.match(source, /import Link from "@\/components\/no-prefetch-link";/);

  const strip = source.match(/function RulebookTabs\([\s\S]*?\nexport async function HealthConfigPage/);
  assert.ok(strip, "RulebookTabs must still exist");
  const body = strip[0];

  assert.match(body, /<Link className=\{tab === "treatment" \? "btn" : "btn ghost"\} href=\{href\("treatment"\)\}>/);
  assert.match(body, /<Link className=\{tab === "diagnosis" \? "btn" : "btn ghost"\} href=\{href\("diagnosis"\)\}>/);

  // The regression itself: no raw anchor may carry a tab href again.
  assert.doesNotMatch(body, /<a\s/, "a bare <a> in the tab strip reloads the document");
});

// The tab belongs in the URL so a vet can send someone "the diagnosis register". A Link
// keeps that; client-only state would not.
test("the tab stays in the URL and drops the other tab's selection", () => {
  assert.match(source, /paramsWithout\(searchParams, \["hc_tab", "hc_version", "hc_register", "hc_cursor"\]\)/);
  assert.match(source, /if \(next === "diagnosis"\) params\.set\("hc_tab", "diagnosis"\);/);
});
