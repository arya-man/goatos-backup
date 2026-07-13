#!/usr/bin/env node

import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const ZERO_SHA = /^0+$/;
const STG_REF = "refs/heads/stg";
const EXPECTED_REPOSITORY = "vgoats/goatos";

export function parsePrePushUpdates(input) {
  return input
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [localRef, localSha, remoteRef, remoteSha] = line.split(/\s+/);
      return { localRef, localSha, remoteRef, remoteSha };
    });
}

export function forbiddenPrePushUpdates(input) {
  return parsePrePushUpdates(input).filter(({ remoteRef }) => remoteRef === STG_REF);
}

function unquote(token) {
  return token.replace(/^["']+|["',)]+$/g, "");
}

function isStgDestination(token) {
  const value = unquote(token);
  return (
    value === "stg" ||
    value === STG_REF ||
    value.endsWith(":stg") ||
    value.endsWith(`:${STG_REF}`)
  );
}

export function commandAttemptsDirectStgPush(command) {
  if (typeof command !== "string" || !command.trim()) return false;

  // Inspect every git push command segment, including commands nested inside
  // zsh/bash -c strings. This intentionally errs on the side of refusing a
  // shell command that could update the staging ref.
  const matcher =
    /\bgit(?:\s+(?:-c|-C)\s+\S+|\s+--(?:git-dir|work-tree|namespace)(?:=\S+|\s+\S+)|\s+--(?:bare|no-pager|literal-pathspecs|glob-pathspecs|noglob-pathspecs|icase-pathspecs))*\s+(mesha-push|push)\b([^;&|\n]*)/g;
  for (const match of command.matchAll(matcher)) {
    const args = match[2]
      .trim()
      .split(/\s+/)
      .filter(Boolean);
    if (args.some(isStgDestination)) return true;
  }
  return false;
}

function commandFromHookPayload(raw) {
  let payload;
  try {
    payload = JSON.parse(raw || "{}");
  } catch {
    return raw || "";
  }

  const input = payload?.tool_input ?? payload?.input ?? payload?.arguments ?? {};
  if (typeof input === "string") return input;
  if (input && typeof input === "object") {
    const command = input.command ?? input.cmd ?? input.argv ?? input.source;
    if (Array.isArray(command)) return command.join(" ");
    if (typeof command === "string") return command;
  }
  const command = payload?.command ?? payload?.cmd ?? payload?.argv;
  if (Array.isArray(command)) return command.join(" ");
  if (typeof command === "string") return command;

  // functions.exec can place JavaScript source in a string-valued tool input.
  // Falling back to the serialized payload preserves the safety check there.
  return raw || "";
}

export function isValidPromotionPullRequest(pr, { repository, sha }) {
  return Boolean(
    pr &&
      pr.merged_at &&
      pr.merge_commit_sha === sha &&
      pr.base?.ref === "stg" &&
      pr.head?.ref === "main" &&
      pr.base?.repo?.full_name === repository &&
      pr.head?.repo?.full_name === repository,
  );
}

async function verifyGitHubPromotion() {
  const repository = process.env.GOATOS_PROMOTION_REPOSITORY || process.env.GITHUB_REPOSITORY;
  const sha = process.env.GOATOS_PROMOTION_SHA || process.env.GITHUB_SHA;
  const ref = process.env.GOATOS_PROMOTION_REF || process.env.GITHUB_REF_NAME;
  const token = process.env.GITHUB_TOKEN;

  if (repository !== EXPECTED_REPOSITORY) {
    throw new Error(`staging deploy is restricted to ${EXPECTED_REPOSITORY}; got ${repository || "<empty>"}`);
  }
  if (ref !== "stg") {
    throw new Error(`staging deploy must run from refs/heads/stg; got ${ref || "<empty>"}`);
  }
  if (!/^[0-9a-f]{40}$/i.test(sha || "") || ZERO_SHA.test(sha)) {
    throw new Error(`invalid staging commit SHA: ${sha || "<empty>"}`);
  }
  if (!token) throw new Error("GITHUB_TOKEN is required to verify the merged promotion PR");

  const owner = repository.split("/")[0];
  const url = new URL(`https://api.github.com/repos/${repository}/pulls`);
  url.searchParams.set("state", "closed");
  url.searchParams.set("base", "stg");
  url.searchParams.set("head", `${owner}:main`);
  url.searchParams.set("sort", "updated");
  url.searchParams.set("direction", "desc");
  url.searchParams.set("per_page", "100");

  let lastStatus = "no matching merged PR";
  for (let attempt = 1; attempt <= 5; attempt += 1) {
    const response = await fetch(url, {
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Bearer ${token}`,
        "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": "goatos-stg-promotion-guard",
      },
    });
    if (!response.ok) {
      lastStatus = `GitHub API returned ${response.status}: ${await response.text()}`;
    } else {
      const pulls = await response.json();
      const promotion = pulls.find((pr) => isValidPromotionPullRequest(pr, { repository, sha }));
      if (promotion) {
        console.log(`stg promotion verified: PR #${promotion.number} main -> stg at ${sha}`);
        return;
      }
      lastStatus = `no merged ${repository} main -> stg PR produced ${sha}`;
    }
    if (attempt < 5) await new Promise((resolve) => setTimeout(resolve, 3000));
  }
  throw new Error(`refusing staging deploy: ${lastStatus}`);
}

function verifyRepositoryWiring() {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const repo = path.resolve(here, "../..");
  const read = (relative) => fs.readFileSync(path.join(repo, relative), "utf8");

  const deploy = read(".github/workflows/stg-deploy.yml");
  assert.match(deploy, /pull-requests:\s*read/, "stg-deploy must have pull request read permission");
  assert.match(
    deploy,
    /node tools\/ci\/check-stg-promotion\.mjs --github/,
    "stg-deploy must verify the exact merged promotion before cloud authentication",
  );
  assert.ok(
    deploy.indexOf("check-stg-promotion.mjs --github") < deploy.indexOf("google-github-actions/auth"),
    "promotion verification must run before cloud authentication",
  );

  for (const hookConfig of [".codex/hooks.json", ".claude/settings.json"]) {
    assert.match(read(hookConfig), /check-stg-promotion\.mjs[^\n]*--agent-hook/, `${hookConfig} must block agent stg pushes`);
  }
  assert.match(read("Makefile"), /install-stg-push-guard\.sh/, "ai-setup must install the local pre-push guard");
  assert.match(read("AGENTS.md"), /Never push any local ref[\s\S]{0,240}remote `stg`/, "AGENTS.md must prohibit direct stg pushes");
  assert.match(
    read(".agents/skills/goatos-build/SKILL.md"),
    /Never push any local ref[\s\S]{0,240}remote\s+`stg`/,
    "the build skill must prohibit direct stg pushes",
  );
  console.log("stg promotion repository wiring: OK");
}

function selfTest() {
  const sha = "1".repeat(40);
  const old = "2".repeat(40);
  for (const localRef of ["refs/heads/codex/cloud-deploy", "refs/heads/main", "refs/heads/stg", "(delete)"]) {
    const localSha = localRef === "(delete)" ? "0".repeat(40) : sha;
    const input = `${localRef} ${localSha} ${STG_REF} ${old}\n`;
    assert.equal(forbiddenPrePushUpdates(input).length, 1, `must reject ${localRef} -> stg`);
  }
  assert.equal(forbiddenPrePushUpdates(`refs/heads/main ${sha} refs/heads/main ${old}\n`).length, 0);
  assert.equal(forbiddenPrePushUpdates(`refs/tags/v1 ${sha} refs/tags/v1 ${old}\n`).length, 0);

  for (const command of [
    "zsh -ic 'git mesha-push HEAD:stg'",
    "git mesha-push stg",
    "git push origin main:stg",
    "git push --force origin HEAD:refs/heads/stg",
    "git push --delete origin stg",
    "git -c push.default=current push origin stg",
    "git -C /tmp/goatos push origin HEAD:stg",
  ]) {
    assert.equal(commandAttemptsDirectStgPush(command), true, `must reject: ${command}`);
  }
  for (const command of ["git mesha-push main", "git push origin main", "git push origin feature/foo", "make test"]) {
    assert.equal(commandAttemptsDirectStgPush(command), false, `must allow: ${command}`);
  }

  const valid = {
    merged_at: "2026-07-14T00:00:00Z",
    merge_commit_sha: sha,
    base: { ref: "stg", repo: { full_name: EXPECTED_REPOSITORY } },
    head: { ref: "main", repo: { full_name: EXPECTED_REPOSITORY } },
  };
  assert.equal(isValidPromotionPullRequest(valid, { repository: EXPECTED_REPOSITORY, sha }), true);
  assert.equal(isValidPromotionPullRequest({ ...valid, merged_at: null }, { repository: EXPECTED_REPOSITORY, sha }), false);
  assert.equal(
    isValidPromotionPullRequest({ ...valid, head: { ...valid.head, ref: "codex/foo" } }, { repository: EXPECTED_REPOSITORY, sha }),
    false,
  );
  assert.equal(isValidPromotionPullRequest(valid, { repository: EXPECTED_REPOSITORY, sha: old }), false);
  console.log("stg promotion guard self-test: OK");
}

function refuse(message) {
  console.error("\nGOATOS STAGING PROMOTION BLOCKED");
  console.error(message);
  console.error("Only a merged same-repository vgoats/goatos main -> stg pull request may update and deploy staging.\n");
  process.exit(2);
}

async function main() {
  const mode = process.argv[2];
  if (mode === "--self-test") return selfTest();
  if (mode === "--repository") return verifyRepositoryWiring();
  if (mode === "--pre-push") {
    const input = fs.readFileSync(0, "utf8");
    const forbidden = forbiddenPrePushUpdates(input);
    if (forbidden.length) {
      const sources = forbidden.map(({ localRef }) => localRef).join(", ");
      refuse(`Direct local push to refs/heads/stg was attempted from: ${sources}.`);
    }
    return;
  }
  if (mode === "--agent-hook") {
    const raw = fs.readFileSync(0, "utf8");
    const command = commandFromHookPayload(raw);
    if (commandAttemptsDirectStgPush(command)) refuse("The shell command attempts a direct push to stg.");
    return;
  }
  if (mode === "--github") return verifyGitHubPromotion();
  throw new Error("usage: check-stg-promotion.mjs --self-test|--repository|--pre-push|--agent-hook|--github");
}

main().catch((error) => {
  console.error(`stg promotion guard: ${error.message}`);
  process.exit(1);
});
