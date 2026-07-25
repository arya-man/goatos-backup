import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { assertOriginMainLocalStack, isSharedAdminWebPort } from "./origin-main-local-stack-guard.mjs";

test("exact-origin/main guard applies only to the shared admin-web port", () => {
  assert.equal(isSharedAdminWebPort(3300), true);
  assert.equal(isSharedAdminWebPort("3300"), true);
  assert.equal(isSharedAdminWebPort(3000), false);
  assert.equal(isSharedAdminWebPort(13300), false);
});

test("isolated admin-web does not require shared origin/main checkout", () => {
  assert.doesNotThrow(() => assertOriginMainLocalStack("/does/not/exist", "isolated-admin-web", { port: 3000 }));
});

test("preverified shared admin-web still rejects dirty or mismatched checkout", () => {
  const repo = mkdtempSync(path.join(os.tmpdir(), "goatos-admin-origin-guard-"));
  try {
    git(repo, ["init", "--quiet"]);
    git(repo, ["config", "user.email", "guard@example.invalid"]);
    git(repo, ["config", "user.name", "Guard Test"]);
    writeFileSync(path.join(repo, "tracked.txt"), "one\n");
    git(repo, ["add", "tracked.txt"]);
    git(repo, ["commit", "--quiet", "-m", "first"]);
    git(repo, ["remote", "add", "origin", "https://github.com/vgoats/goatos.git"]);
    const first = git(repo, ["rev-parse", "HEAD"]);
    git(repo, ["update-ref", "refs/remotes/origin/main", first]);

    assert.equal(runGuard(repo).status, 0, "clean exact-main checkout should pass");

    writeFileSync(path.join(repo, "tracked.txt"), "dirty\n");
    assert.equal(runGuard(repo).status, 2, "dirty tracked checkout should fail");

    writeFileSync(path.join(repo, "tracked.txt"), "one\n");
    writeFileSync(path.join(repo, "second.txt"), "two\n");
    git(repo, ["add", "second.txt"]);
    git(repo, ["commit", "--quiet", "-m", "second"]);
    assert.equal(runGuard(repo).status, 2, "HEAD different from origin/main should fail");
  } finally {
    removeTempRepo(repo);
  }
});

function removeTempRepo(repo) {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      rmSync(repo, { recursive: true, force: true, maxRetries: 3, retryDelay: 50 });
      return;
    } catch (err) {
      if (attempt === 2) throw err;
    }
  }
}

function runGuard(repo) {
  const guardUrl = new URL("./origin-main-local-stack-guard.mjs", import.meta.url).href;
  const source = `import { assertOriginMainLocalStack } from ${JSON.stringify(guardUrl)}; assertOriginMainLocalStack(process.argv[1], "test-admin", { port: 3300 });`;
  return spawnSync(process.execPath, ["--input-type=module", "-e", source, repo], {
    env: { ...process.env, GOATOS_ORIGIN_MAIN_PREVERIFIED: "1" },
    encoding: "utf8",
  });
}

function git(repo, args) {
  return execFileSync("git", ["-C", repo, ...args], { encoding: "utf8" }).trim();
}
