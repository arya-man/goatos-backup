// Guard: client-only-hook-directive. A module that calls Next's client-only hooks (useLinkStatus,
// useRouter/useSearchParams/usePathname from next/navigation) must start with "use client".
// Without it, a server component that imports the module breaks `next build` (Turbopack refuses
// the import) even though typecheck passes -- the PR head broke exactly this way on
// components/no-prefetch-link.tsx (f40322927).
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import test from "node:test";

const root = new URL("..", import.meta.url).pathname;
const DIRS = ["app", "components", "features", "layouts", "lib"];
const CLIENT_ONLY = /\buseLinkStatus\s*\(|\buse(?:Router|SearchParams|Pathname|SelectedLayoutSegments?)\s*\(/;

function walk(dir, out = []) {
  for (const name of readdirSync(dir)) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const abs = join(dir, name);
    if (statSync(abs).isDirectory()) walk(abs, out);
    else if (/\.tsx?$/.test(name) && !/\.(test|stories)\./.test(name)) out.push(abs);
  }
  return out;
}

export function missingDirective(text) {
  const code = text.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
  // Only direct imports from Next count; a template wrapper hook's own module carries the directive.
  const fromNext = /from\s+["']next\/(?:link|navigation)["']/.test(code);
  return fromNext && CLIENT_ONLY.test(code) && !/^\s*["']use client["']/.test(code);
}

test("modules calling client-only Next hooks start with 'use client'", () => {
  const offenders = DIRS.flatMap((dir) => walk(join(root, dir)))
    .filter((abs) => missingDirective(readFileSync(abs, "utf8")))
    .map((abs) => relative(root, abs));
  assert.deepEqual(offenders, [], `add "use client" to: ${offenders.join(", ")}`);
});

test("the check catches a hook module without the directive", () => {
  assert.equal(missingDirective('import { useLinkStatus } from "next/link";\nexport const x = () => useLinkStatus();\n'), true);
  assert.equal(missingDirective('"use client";\nimport { useLinkStatus } from "next/link";\nexport const x = () => useLinkStatus();\n'), false);
});
