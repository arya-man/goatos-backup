import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import test from "node:test";

const repoRoot = resolve(import.meta.dirname, "../../..");

function source(path) {
  return readFileSync(resolve(repoRoot, path), "utf8");
}

test("admin-web maps every backend navigation icon token", () => {
  const shell = source("apps/admin-web/components/mesha-shell.tsx");
  const service = source("backend/internal/adminui/app/service.go");

  const mappedTokens = new Set();
  const iconMap = shell.match(/const iconByToken: Record<string, ElementType> = \{([\s\S]*?)\n\};/);
  assert.ok(iconMap, "iconByToken map should be parseable");
  for (const match of iconMap[1].matchAll(/^\s*(?:"([^"]+)"|([a-zA-Z0-9_-]+))\s*:/gm)) {
    mappedTokens.add(match[1] ?? match[2]);
  }

  const backendTokens = new Set();
  for (const match of service.matchAll(/navItem\([^)]*?,\s*"([^"]+)"\s*,\s*"[^"]*"\s*\)/g)) {
    backendTokens.add(match[1]);
  }

  const missing = [...backendTokens].filter((token) => !mappedTokens.has(token)).sort();
  assert.deepEqual(missing, []);
});

test("unknown navigation icon tokens cannot blank the admin shell", () => {
  const shell = source("apps/admin-web/components/mesha-shell.tsx");

  assert.doesNotMatch(shell, /Admin-web navigation icon token is not mapped/);
  assert.match(shell, /function navIconForToken\(token: string\): ElementType \{\s*return iconByToken\[token\] \?\? TowerControl;\s*\}/);
});
