import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import test from "node:test";

const source = readFileSync(new URL("./check-ui-contract-literals.mjs", import.meta.url), "utf8");
const packageJson = readFileSync(new URL("../package.json", import.meta.url), "utf8");

test("admin UI contract guard verifies frontend lookups have backend producers", () => {
  assert.match(source, /function contractSourceIndex\(\)/);
  assert.match(source, /readProductionAdminUiService/);
  assert.match(source, /routeForFile/);
  assert.match(source, /commonCopyKeys/);
  assert.match(source, /copy key/);
  assert.match(source, /option group/);
  assert.match(source, /table/);
  assert.match(source, /control/);
  assert.match(source, /has no .*production AdminWebPageContract producer or explicit frontend fallback/);
  assert.doesNotMatch(source, /entry\.endsWith\("\\.go"\)/);
});

test("admin UI contract guard self-test pins reference validation behavior", () => {
  const result = spawnSync(process.execPath, [new URL("./check-ui-contract-literals.mjs", import.meta.url).pathname, "--self-test"], {
    cwd: new URL("..", import.meta.url).pathname,
    encoding: "utf8",
  });
  assert.equal(result.status, 0, result.stderr || result.stdout);
  assert.match(result.stdout, /self-test passed/);
});

test("mock fidelity keeps the contract literal and reference guard in the mandatory frontend gate", () => {
  assert.match(packageJson, /check-ui-contract-literals\.mjs/);
  assert.match(packageJson, /"check:mock-fidelity"[\s\S]*check-ui-contract-literals\.mjs/);
});
