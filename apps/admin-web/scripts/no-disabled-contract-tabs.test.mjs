// guard: no-disabled-contract-tabs, widened (TR-2 P1-4 follow-up). /people rendered contract tabs the
// backend disables as greyed "soon" tabs, and so did the /calendar owner / workstream filters: an
// option that does nothing is a dead control, and so is a greyed sidebar entry (REVIEW-57, the shell
// nav). Every feature AND shared component renders ONLY the options the contract enables (filter on
// `.enabled`), never `disabled: !x.enabled`. components/minimal is the verbatim template (skipped).
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("..", import.meta.url));
const DEAD = /disabled:\s*!\s*\w+\.enabled\b/;

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) {
      if (!path.endsWith(join("components", "minimal"))) walk(path, out);
    }
    else if (/\.tsx?$/.test(name)) out.push(path);
  }
  return out;
}

export function deadContractOptionFiles(files, read = (f) => readFileSync(f, "utf8")) {
  return files.filter((file) => DEAD.test(read(file))).map((file) => file.slice(root.length));
}

test("no feature or shared component renders a contract option disabled from `.enabled`", () => {
  assert.deepEqual(deadContractOptionFiles([...walk(join(root, "features")), ...walk(join(root, "components"))]), []);
});

test("self-test: the inert-option mapping is caught", () => {
  const src = { a: "options: tabs.map((tab) => ({ key: tab.key, disabled: !tab.enabled }))", b: "tabs.filter((tab) => tab.enabled)" };
  assert.deepEqual(deadContractOptionFiles([`${root}a`, `${root}b`], (f) => src[f.slice(root.length)]), ["a"]);
});
