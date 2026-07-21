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

function selfTest() {
  const badHeva = validateIdentity("Claude Code", "rteja@heva.co");
  if (badHeva.length === 0) throw new Error("self-test: Heva identity was not blocked");

  const badPersonal = validateIdentity("Ravi", "ravi@gmail.com");
  if (badPersonal.length === 0) throw new Error("self-test: personal identity was not blocked");

  const good = validateIdentity("Raviteja", "ravi@mesha.sg");
  if (good.length !== 0) throw new Error(`self-test: Mesha identity was blocked: ${good.join("; ")}`);

  console.log("git-identity guard self-test passed");
}

function main() {
  if (process.argv.includes("--self-test")) {
    selfTest();
    return;
  }

  const name = gitConfig("user.name");
  const email = gitConfig("user.email");
  const errors = validateIdentity(name, email);
  if (errors.length > 0) {
    console.error("git-identity guard failed:");
    for (const error of errors) console.error(`- ${error}`);
    console.error("");
    console.error("Fix: git config user.name 'Raviteja' && git config user.email 'ravi@mesha.sg'");
    process.exit(1);
  }
  console.log(`git-identity guard passed: ${name} <${email}>`);
}

main();
