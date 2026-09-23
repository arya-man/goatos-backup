import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, extname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

// THE BUILD MUST NOT REACH THE NETWORK FOR A FONT (7088230b1).
//
// `next/font/google` looks like a local import and is not: at BUILD time it fetches the font from
// Google and inlines it. A build machine without egress — a sandboxed CI runner, a Cloud Build
// step behind a policy, a laptop on a bad connection — fails, or worse hangs, on something no one
// reading layout.tsx would guess was a network call. 7088230b1 dropped Inter for the system stack
// and shipped no test, so the next person to want a nicer typeface re-adds one import and the
// build starts reaching out again with nothing to say so.
//
// This is a whole-tree scan rather than a check on layout.tsx, because the hazard is the IMPORT,
// not the file that happened to hold it.
const root = resolve(dirname(fileURLToPath(import.meta.url)), "..", "..");
const SKIP = new Set(["node_modules", ".next", ".git", "public", ".codex-goatos-render"]);
const SOURCE = new Set([".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"]);

function sourceFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    if (SKIP.has(entry)) continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      out.push(...sourceFiles(full));
      continue;
    }
    if (SOURCE.has(extname(entry))) out.push(full);
  }
  return out;
}

const files = sourceFiles(root);

test("the app tree is scanned at all", () => {
  // Guards the guard: a scan that silently walked nothing would pass every assertion below.
  assert.ok(files.length > 200, `only ${files.length} source files scanned — the walk is not reaching the app`);
  assert.ok(
    files.some((f) => relative(root, f) === join("app", "layout.tsx")),
    "app/layout.tsx must be in the scanned set — it is the file this rule was written about",
  );
});

test("no source file imports a remotely-fetched font", () => {
  const offenders = files.filter((file) => /from\s+["']next\/font\/google["']|require\(\s*["']next\/font\/google["']\s*\)/.test(readFileSync(file, "utf8")));
  assert.deepEqual(
    offenders.map((f) => relative(root, f)),
    [],
    "next/font/google fetches from Google during `next build`; use the system stack in globals.css, or self-host with next/font/local",
  );
});

test("the font stack the layout dropped Inter for is actually declared", () => {
  // The other half: removing the import without leaving a stack behind would have been a
  // regression dressed as a fix, and this test would happily pass on it.
  const css = readFileSync(join(root, "app", "globals.css"), "utf8");
  assert.match(css, /--font-sans:\s*[^;]*system-ui/, "globals.css must declare a --font-sans stack starting from system-ui");
});
