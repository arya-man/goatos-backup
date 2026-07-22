import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const appRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("all admin-web dev starts go through the local auth wrapper", () => {
  const pkg = JSON.parse(readFileSync(path.join(appRoot, "package.json"), "utf8"));
  assert.equal(pkg.scripts.dev, "node scripts/run-local-next.mjs dev");
  assert.equal(pkg.scripts["dev:local"], "node scripts/run-local-next.mjs dev");
});

test("local wrapper accepts custom isolated ports instead of bypassing auth", () => {
  const source = readFileSync(path.join(appRoot, "scripts/run-local-next.mjs"), "utf8");
  assert.match(source, /optionValue\(extraArgs, \["-H", "--hostname"\]\)/);
  assert.match(source, /optionValue\(extraArgs, \["-p", "--port"\]\)/);
  assert.match(source, /if \(port !== 3300\) return;/);
});
