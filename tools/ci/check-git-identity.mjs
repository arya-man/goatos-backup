#!/usr/bin/env node
import { execSync } from "node:child_process";
import process from "node:process";

function validateIdentity(name, email) {
  const errors = [];
  const cleanName = String(name || "").trim();
  const cleanEmail = String(email || "").trim().toLowerCase();

  if (!cleanName) errors.push("git user.name is not configured");
  if (!cleanEmail) errors.push("git user.email is not configured");
  if (cleanEmail && !cleanEmail.endsWith("@mesha.sg")) {
    errors.push(`git user.email must be a Mesha address, got ${email}`);
  }
  if (/heva|slice|gmail|personal/i.test(`${cleanName} ${cleanEmail}`)) {
    errors.push(`git identity must not use Heva/Slice/personal identity, got ${cleanName} <${email}>`);
  }
  return errors;
}

function gitConfig(key) {
  try {
    return execSync(`git config --get ${key}`, { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim();
  } catch {
    return "";
  }
}

function validateCommitEmails(lines) {
  const errors = [];
  for (const raw of lines) {
    const line = String(raw || "").trim();
    if (!line) continue;
    const [sha = "unknown", role = "email", email = ""] = line.split("\t");
    const cleanEmail = email.trim().toLowerCase();
    if (!cleanEmail.endsWith("@mesha.sg")) {
      errors.push(`commit ${sha} has non-Mesha ${role} email: ${email}`);
    }
    if (/heva|slice|gmail|personal/i.test(cleanEmail)) {
      errors.push(`commit ${sha} must not use Heva/Slice/personal ${role} email: ${email}`);
    }
  }
  return errors;
}

function commitEmailLines() {
  const isGitHubPR = process.env.GITHUB_EVENT_NAME === "pull_request";
  let base = process.env.GIT_IDENTITY_BASE || process.env.GOATOS_CI_BASE || "origin/main";

  // On GitHub Actions PR merge, HEAD is a synthetic merge commit. We need to inspect
  // the actual PR commits, excluding the runner's synthetic merge commit.
  // GitHub Actions sets GITHUB_BASE_REF to the target branch name.
  if (isGitHubPR && process.env.GITHUB_BASE_REF) {
    base = `origin/${process.env.GITHUB_BASE_REF}`;
  }

  const ranges = isGitHubPR
    ? [`${base}..HEAD^2`, `${base}..HEAD~1`] // For merge commits: HEAD^2 is the PR tip, HEAD~1 is fallback
    : [`${base}..HEAD`, "HEAD~1..HEAD"];

  for (const range of ranges) {
    try {
      const ref = range.split("..")[0];
      execSync(`git rev-parse --verify --quiet ${ref}^{commit}`, { stdio: "ignore" });
      return execSync(`git log --format=%H%x09author%x09%ae%n%H%x09committer%x09%ce ${range}`, {
        encoding: "utf8",
        stdio: ["ignore", "pipe", "ignore"],
      })
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean);
    } catch {
      // Try the next range.
    }
  }
  return [];
}

function selfTest() {
  const badHeva = validateIdentity("Claude Code", "rteja@heva.co");
  if (badHeva.length === 0) throw new Error("self-test: Heva identity was not blocked");

  const badPersonal = validateIdentity("Ravi", "ravi@gmail.com");
  if (badPersonal.length === 0) throw new Error("self-test: personal identity was not blocked");

  const good = validateIdentity("Raviteja", "ravi@mesha.sg");
  if (good.length !== 0) throw new Error(`self-test: Mesha identity was blocked: ${good.join("; ")}`);

  const badCommit = validateCommitEmails([
    "abc123\tauthor\trteja@heva.co",
    "abc123\tcommitter\travi@mesha.sg",
  ]);
  if (badCommit.length === 0) throw new Error("self-test: foreign commit author was not blocked");

  const badCommitter = validateCommitEmails([
    "def456\tauthor\travi@mesha.sg",
    "def456\tcommitter\tbot@gmail.com",
  ]);
  if (badCommitter.length === 0) throw new Error("self-test: personal commit committer was not blocked");

  const goodCommit = validateCommitEmails([
    "fedcba\tauthor\travi@mesha.sg",
    "fedcba\tcommitter\tbuild@mesha.sg",
  ]);
  if (goodCommit.length !== 0) throw new Error(`self-test: Mesha commit emails were blocked: ${goodCommit.join("; ")}`);

  console.log("git-identity guard self-test passed");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }

  // On CI runners, skip local git config checks (CI env has empty config).
  // Only validate commit emails from the PR.
  const isCI = process.env.CI === "true" || process.env.GITHUB_ACTIONS === "true";

  const name = gitConfig("user.name");
  const email = gitConfig("user.email");
  const commitErrors = validateCommitEmails(commitEmailLines());

  if (!isCI) {
    // Local machine: check both config and commit emails
    const configErrors = validateIdentity(name, email);
    const errors = [...configErrors, ...commitErrors];
    if (errors.length > 0) {
      console.error("git-identity guard failed:");
      for (const error of errors) console.error(`- ${error}`);
      console.error("");
      console.error("Fix config with: git config user.name 'Raviteja' && git config user.email 'ravi@mesha.sg'");
      console.error("Fix bad commits by amending/rebasing so author and committer emails are @mesha.sg.");
      process.exit(1);
    }
    console.log(`git-identity guard passed: ${name} <${email}>`);
  } else {
    // CI runner: only check commit emails (runner config is empty)
    if (commitErrors.length > 0) {
      console.error("git-identity guard failed:");
      for (const error of commitErrors) console.error(`- ${error}`);
      console.error("");
      console.error("Fix bad commits by amending/rebasing so author and committer emails are @mesha.sg.");
      process.exit(1);
    }
    console.log("git-identity guard passed (CI mode: commit emails validated)");
  }
}

main();
