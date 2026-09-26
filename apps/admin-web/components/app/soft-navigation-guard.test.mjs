import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

// guard: soft-navigation. A tab, chip, filter or pager click inside the admin must be an App
// Router soft navigation, never a document reload (maintainer P0 2026-09-26: every tab switch
// flashed the whole page to the loading shimmer). Two patterns caused it:
//  - importing Link from "next/dist/client/link" (the Pages Router Link: Next aliases only the
//    exact "next/link" specifier to the App Router Link, so the other one finds no router and lets
//    the browser reload the document);
//  - a GET <form> with a plain action (a native submit reloads the document; use next/form).
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const DIRS = ["app", "components", "features", "lib", "layouts", "theme"];

function* sources(dir) {
  let entries;
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const name of entries) {
    if (name === "node_modules" || name.startsWith(".")) continue;
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) yield* sources(full);
    else if (/\.(tsx?|jsx?)$/.test(name) && !/\.test\./.test(name)) yield full;
  }
}

const files = DIRS.flatMap((dir) => [...sources(path.join(root, dir))]);

test("no admin code imports the Pages Router Link", () => {
  const bad = files.filter((file) => /from\s+["']next\/dist\/client\/link["']/.test(readFileSync(file, "utf8")));
  assert.deepEqual(bad.map((file) => path.relative(root, file)), [], 'import Link from "next/link" (or @/components/no-prefetch-link)');
});

test("no native GET form reloads the document", () => {
  const code = (file) => readFileSync(file, "utf8").split("\n").filter((line) => !/^\s*(\*|\/\/|\/\*)/.test(line)).join("\n");
  const bad = files.filter((file) => /<form\b[^>]*\bmethod=["']get["']/i.test(code(file)));
  assert.deepEqual(bad.map((file) => path.relative(root, file)), [], 'use <Form> from "next/form" for GET filter/search forms');
});

test("the shared Link wrapper is the App Router Link", () => {
  assert.match(readFileSync(path.join(root, "components/no-prefetch-link.tsx"), "utf8"), /from "next\/link";/);
});
