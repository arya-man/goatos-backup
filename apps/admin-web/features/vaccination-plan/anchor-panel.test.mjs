import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const panel = readFileSync(new URL("./anchor-panel.tsx", import.meta.url), "utf8");
const actions = readFileSync(new URL("./plan-actions.ts", import.meta.url), "utf8");
const server = readFileSync(new URL("../../lib/api/server.ts", import.meta.url), "utf8");

test("anchor panel defaults all safety flags to true", () => {
  assert.match(panel, /suppressBeforeAnchor:\s*true/);
  assert.match(panel, /chainFutureFromAnchor:\s*true/);
  assert.match(panel, /enforceAgeEligibility:\s*true/);
});

test("anchor panel reports animals by RFID identifier, not internal goat id", () => {
  assert.match(panel, /<th>RFID<\/th>/);
  assert.match(panel, /animal\.identifier/);
  assert.doesNotMatch(panel, /<td>\s*\{animal\.goat_id\}/);
});

test("anchor actions call preview and create endpoints with idempotency", () => {
  assert.match(actions, /previewAnchor/);
  assert.match(actions, /createAnchor/);
  assert.match(server, /\/vaccination\/anchors\/preview/);
  assert.match(server, /\/vaccination\/anchors"/);
  assert.match(server, /Idempotency-Key/);
});
