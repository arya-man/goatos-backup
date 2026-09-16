import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./health-config-editor.tsx", import.meta.url), "utf8");

test("health config Back uses a history-entry nonce, not a stale URL marker", () => {
  assert.match(source, /type BackToListMarker = \{[\s\S]*nonce: string;[\s\S]*\};/);
  assert.match(source, /const activeBackToListNonces = new Set<string>\(\);/);
  assert.match(source, /JSON\.stringify\(\{ editorHref, listHref: basePath, nonce: crypto\.randomUUID\(\) \}/);
  assert.match(source, /window\.sessionStorage\.removeItem\(BACK_TO_LIST_MARKER\);[\s\S]*activeBackToListNonces\.add\(marker\.nonce\);[\s\S]*window\.history\.replaceState/);
  assert.match(source, /BACK_TO_LIST_HISTORY_NONCE = "meshaHealthConfigBackNonce"/);
  assert.match(source, /\[BACK_TO_LIST_HISTORY_NONCE\]: marker\.nonce/);
  assert.match(source, /const nonce = historyBackToListNonce\(\);[\s\S]*!activeBackToListNonces\.has\(nonce\)[\s\S]*return;/);
  assert.match(source, /activeBackToListNonces\.delete\(nonce\);[\s\S]*router\.back\(\);/);
});

test("health config Back keeps the safe href fallback for direct opens and reloads", () => {
  assert.match(source, /<a[\s\S]*href=\{href\}[\s\S]*onClick=/);
  assert.match(source, /Reloads also lose the in-memory nonce, so the safe filtered href wins/);
  assert.match(source, /window\.sessionStorage\.removeItem\(BACK_TO_LIST_MARKER\);[\s\S]*return;/);
});
