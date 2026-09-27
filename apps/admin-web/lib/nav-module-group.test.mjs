import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { moduleGroupForPath } from "./nav-module-group.ts";

// guard: nav-module-fallback (TR1-#14). /counts/herd has no nav leaf (withheld), so neither the
// sidebar nor the phone menu opened or highlighted Counts.
const groups = [
  { id: "counts", leaves: [{ href: "/counts/analytics?x=1", enabled: true }, { href: "/counts/breakdown", enabled: true }] },
  { id: "weighing", leaves: [{ href: "/weighing/analytics", enabled: true }, { href: "/weighing/sops", enabled: false }] },
  { id: "others", leaves: [{ href: "/leave", enabled: true }, { href: "/people", enabled: true }] },
];

test("a leaf-less module page opens its module group", () => {
  assert.equal(moduleGroupForPath("/counts/herd", groups), "counts");
  assert.equal(moduleGroupForPath("/weighing/weights", groups), "weighing");
});

test("paths outside every single-prefix group claim nothing", () => {
  assert.equal(moduleGroupForPath("/goats/abc", groups), null);
  assert.equal(moduleGroupForPath("/", groups), null);
  assert.equal(moduleGroupForPath("/leave", [groups[2]]), null, "a mixed-prefix group never claims by prefix");
});

test("the shell only falls back when no nav entry is active", () => {
  const shell = readFileSync(new URL("../components/mesha-shell.tsx", import.meta.url), "utf8");
  assert.match(shell, /const fallbackGroupId = active \? null : moduleGroupForPath\(pathname, contract\.navigation\.groups\)/);
  assert.match(shell, /\|\| g\.id === fallbackGroupId/);
});
