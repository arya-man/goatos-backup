#!/usr/bin/env node

// Fast, portable tracked-file size guard. One Node process stats every tracked
// path; this avoids both macOS Bash 3.2 parsing failures and one `wc` subprocess
// per file.
import { execFileSync } from "node:child_process";
import { statSync } from "node:fs";
import { basename, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const limit = Number(process.env.GOATOS_MAX_TRACKED_FILE_BYTES || 5 * 1024 * 1024);
const exempt = new Set(["package-lock.json", "pnpm-lock.yaml"]);

export function oversized(entries, maxBytes = limit) {
  return entries.filter(({ path, size }) => !exempt.has(basename(path)) && size > maxBytes);
}

function selfTest() {
  const bad = oversized([{ path: "asset.bin", size: 11 }], 10);
  const lock = oversized([{ path: "package-lock.json", size: 11 }], 10);
  const good = oversized([{ path: "asset.bin", size: 10 }], 10);
  if (bad.length !== 1 || lock.length !== 0 || good.length !== 0) {
    throw new Error("large-file guard self-test failed");
  }
  console.log("large-file guard: self-test passed");
}

function scan() {
  const tracked = execFileSync("git", ["ls-files", "-z"], { cwd: repo })
    .toString("utf8")
    .split("\0")
    .filter(Boolean);
  const entries = [];
  for (const path of tracked) {
    try {
      const stat = statSync(resolve(repo, path));
      if (stat.isFile()) entries.push({ path, size: stat.size });
    } catch {
      // Deleted worktree entries are irrelevant to the current tracked tree.
    }
  }
  const bad = oversized(entries);
  if (bad.length > 0) {
    for (const item of bad) console.error(`./${item.path} (${item.size} bytes)`);
    console.error(`Files over ${limit} bytes need a storage decision.`);
    process.exit(1);
  }
  console.log(`large-file guard: ${tracked.length} tracked paths within ${limit} bytes`);
}

if (process.argv.includes("--self-test")) selfTest(); else scan();
