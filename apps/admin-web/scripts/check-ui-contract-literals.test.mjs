import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./check-ui-contract-literals.mjs", import.meta.url), "utf8");
const packageJson = readFileSync(new URL("../package.json", import.meta.url), "utf8");

test("admin UI contract guard verifies frontend lookups have backend producers", () => {
  assert.match(source, /function contractSourceIndex\(\)/);
  assert.match(source, /readAdminUiBackendSources/);
  assert.match(source, /copy key/);
  assert.match(source, /option group/);
  assert.match(source, /table/);
  assert.match(source, /control/);
  assert.match(source, /has no backend AdminWebPageContract producer or explicit frontend fallback/);
});

test("mock fidelity keeps the contract literal and reference guard in the mandatory frontend gate", () => {
  assert.match(packageJson, /check-ui-contract-literals\.mjs/);
  assert.match(packageJson, /"check:mock-fidelity"[\s\S]*check-ui-contract-literals\.mjs/);
});
