#!/usr/bin/env node
// Backend admin-web navigation icon tokens must have explicit frontend mappings.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ADMIN_NAV = "backend/internal/adminui/app/service.go";
const SHELL = "apps/admin-web/components/mesha-shell.tsx";

function backendIconTokens(text) {
  const tokens = new Set();
  const pattern = /\bIcon:\s*"([^"]+)"/g;
  let match;
  while ((match = pattern.exec(text)) !== null) {
    tokens.add(match[1]);
  }
  return [...tokens].sort();
}

function frontendIconTokens(text) {
  const start = text.indexOf("const iconByToken");
  if (start === -1) return new Set();
  const end = text.indexOf("};", start);
  if (end === -1) return new Set();
  const body = text.slice(start, end);
  const tokens = new Set();
  const pattern = /(?:^|\n)\s*(?:"([^"]+)"|([a-zA-Z][\w-]*))\s*:/g;
  let match;
  while ((match = pattern.exec(body)) !== null) {
    tokens.add(match[1] ?? match[2]);
  }
  return tokens;
}

function findMissing(backendText, shellText) {
  const frontend = frontendIconTokens(shellText);
  return backendIconTokens(backendText).filter((token) => !frontend.has(token));
}

function selfTest() {
  const backend = `
    domain.NavigationGroup{ID: "feed", Icon: "wheat"}
    domain.NavigationGroup{ID: "new", Icon: "new-token"}
  `;
  const shell = `
    const iconByToken: Record<string, ElementType> = {
      wheat: Wheat,
      "tower-control": TowerControl,
    };
  `;
  const missing = findMissing(backend, shell);
  if (missing.length !== 1 || missing[0] !== "new-token") {
    console.error(`admin-web-nav-icon-coverage self-test failed: ${JSON.stringify(missing)}`);
    process.exit(1);
  }
  console.log("admin-web-nav-icon-coverage self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const missing = findMissing(
  readFileSync(resolve(repo, ADMIN_NAV), "utf8"),
  readFileSync(resolve(repo, SHELL), "utf8"),
);

if (missing.length) {
  console.error("admin-web-nav-icon-coverage guard FAILED — backend admin navigation icon tokens missing frontend mappings:");
  for (const token of missing) {
    console.error(`- ${token}`);
  }
  console.error(`Add each token to iconByToken in ${SHELL}; do not let admin-web silently fall back to a generic icon.`);
  process.exit(1);
}

console.log("admin-web-nav-icon-coverage guard passed");
