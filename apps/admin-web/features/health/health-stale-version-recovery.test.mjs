import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const config = readFileSync(new URL("./health-config.tsx", import.meta.url), "utf8");
const register = readFileSync(new URL("./health-register.tsx", import.meta.url), "utf8");
const recovery = readFileSync(new URL("./health-stale-version-recovery.tsx", import.meta.url), "utf8");

// A dead ?hc_version= / ?hc_register= used to return the notice ALONE. The list is deliberately
// not read behind an open editor, so "Everything below is up to date" sat over an EMPTY screen
// with one link to press -- and the commonest way to reach it is the ordinary one: PUBLISHING
// retires the draft id the editor URL still holds.
test("a dead version recovers to the list on both halves, never to a bare notice", () => {
  // Treatment: the catalog is read on the stale path and feeds the normal list render.
  assert.match(config, /const recoveryCatalogResult = selectedVersionIsGone\s*\?\s*await listHealthConfigProtocols\(/);
  assert.match(config, /const effectiveCatalogResult = catalogResult \?\? recoveryCatalogResult;/);
  assert.match(config, /const catalog = effectiveCatalogResult\?\.ok \? effectiveCatalogResult\.data : null;/);

  // Diagnosis: same shape, its own list read.
  assert.match(register, /const listResult = firstListResult \?\? \(selectedVersionIsGone \? await listHealthConfigRegisters\(\) : null\);/);

  // The regression: neither half may return early with only the alert.
  assert.doesNotMatch(config, /if \(selectedVersionIsGone\) \{\s*return \(/);
  assert.doesNotMatch(register, /if \(selectedVersionIsGone\) \{\s*return \(/);

  // Both render the shared notice above their recovered list.
  assert.match(config, /<StaleVersionNotice/);
  assert.match(register, /<StaleVersionNotice/);
});

// The dead id must leave the address bar, or a reload asks for the missing version again and the
// notice follows the author around.
test("the dead id is replaced out of the URL, never pushed", () => {
  assert.match(recovery, /replaceLocalOverlayUrl\(`\$\{target\.pathname\}\$\{target\.search\}`\)/);
  assert.doesNotMatch(recovery, /pushLocalOverlayUrl/);
  // It only ever narrows this page's own query; a path change is a navigation, not a render effect.
  assert.match(recovery, /if \(here\.pathname !== target\.pathname\) return;/);
  assert.match(recovery, /if \(here\.search === target\.search\) return;/);
});
