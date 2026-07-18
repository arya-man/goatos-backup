#!/usr/bin/env node
// Blocks accidental Next.js route prefetch in admin-web.
//
// Next's default Link prefetch can start server navigation work before a user clicks.
// In this app those routes often do authenticated SSR API reads, so admin-web links
// must go through apps/admin-web/components/no-prefetch-link.tsx instead of next/link.

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repo = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const wrapper = "apps/admin-web/components/no-prefetch-link.tsx";
const directImportRe = /from\s+["']next\/link["']/;
const truePrefetchRe = /\bprefetch\s*=\s*(?:\{\s*true\s*\}|["']true["'])/;

function findingsForFiles(files, readText) {
  const findings = [];
  for (const file of files) {
    const text = readText(file);
    if (file !== wrapper && directImportRe.test(text)) {
      findings.push(`${file}: import the admin-web no-prefetch Link wrapper instead of next/link`);
    }
    if (truePrefetchRe.test(text)) {
      findings.push(`${file}: prefetch=true is forbidden in admin-web`);
    }
  }
  return findings;
}

function adminWebSourceFiles() {
  const out = execFileSync("git", ["ls-files", "apps/admin-web"], { cwd: repo, encoding: "utf8" });
  return out
    .split("\n")
    .map((s) => s.trim())
    .filter((file) => /\.(?:ts|tsx|js|jsx|mjs)$/.test(file));
}

function selfTest() {
  const files = ["apps/admin-web/features/a.tsx", wrapper, "apps/admin-web/features/b.tsx"];
  const fixtures = {
    "apps/admin-web/features/a.tsx": 'import Link from "next/link";\n',
    [wrapper]: 'import NextLink from "next/link";\n',
    "apps/admin-web/features/b.tsx": '<Link href="/x" prefetch={true}>x</Link>\n',
  };
  const bad = findingsForFiles(files, (file) => fixtures[file] || "");
  if (bad.length !== 2 || !bad.some((f) => f.includes("next/link")) || !bad.some((f) => f.includes("prefetch=true"))) {
    throw new Error(`self-test: expected direct import and true prefetch findings, got ${JSON.stringify(bad)}`);
  }
  const good = findingsForFiles(["apps/admin-web/features/c.tsx", wrapper], (file) =>
    file === wrapper ? 'import NextLink from "next/link";\n' : 'import Link from "@/components/no-prefetch-link";\n<Link href="/x">x</Link>\n',
  );
  if (good.length !== 0) {
    throw new Error(`self-test: expected clean fixtures, got ${JSON.stringify(good)}`);
  }
  console.log("admin-web-prefetch guard: self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const wrapperAbs = resolve(repo, wrapper);
if (!existsSync(wrapperAbs)) {
  console.error(`admin-web-prefetch guard failed: missing ${wrapper}`);
  process.exit(1);
}

const files = adminWebSourceFiles();
const findings = findingsForFiles(files, (file) => readFileSync(resolve(repo, file), "utf8"));
if (findings.length > 0) {
  console.error("admin-web-prefetch guard failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}

console.log(`admin-web-prefetch guard: ok (${files.length} admin-web source file(s) scanned)`);
