#!/usr/bin/env node
// Backend-composed mobile nav keys must have explicit Android icon mappings.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const BOOTSTRAP = "backend/internal/workforce/app/bootstrap_copy.go";
const ICONS = "apps/goatos-android/core/core-designsystem/src/main/kotlin/sg/mesha/goatos/core/designsystem/icon/MeshaIcons.kt";

function navKeysFromBootstrap(text) {
  const keys = new Set();
  const uncommented = text
    .split("\n")
    .map((line) => line.replace(/\/\/.*$/, ""))
    .join("\n");
  const navItemPattern = /\{key:\s*"([^"]+)"/g;
  let match;
  while ((match = navItemPattern.exec(uncommented)) !== null) {
    keys.add(match[1]);
  }
  return [...keys].sort();
}

function mappedKeysFromIcons(text) {
  const bodyStart = text.indexOf("fun forNavKey");
  if (bodyStart === -1) return new Set();
  const body = text.slice(bodyStart);
  const keys = new Set();
  const casePattern = /((?:"[^"]+"\s*,\s*)*"[^"]+")\s*->/g;
  let match;
  while ((match = casePattern.exec(body)) !== null) {
    const stringPattern = /"([^"]+)"/g;
    let keyMatch;
    while ((keyMatch = stringPattern.exec(match[1])) !== null) {
      keys.add(keyMatch[1]);
    }
  }
  return keys;
}

function findMissing(bootstrapText, iconsText) {
  const navKeys = navKeysFromBootstrap(bootstrapText);
  const mapped = mappedKeysFromIcons(iconsText);
  return navKeys.filter((key) => !mapped.has(key));
}

function selfTest() {
  const bootstrap = `
    navItems: []moduleNavItem{
      {key: "vaccination", labelKey: "nav.drives", href: "/vaccination"},
      {key: "vaccine_stock", labelKey: "nav.stock", href: "/pc/vaccine-stock"},
      {key: "new_backend_tab", labelKey: "nav.new", href: "/new"},
    }
  `;
  const icons = `
    fun forNavKey(key: String): ImageVector = when (key.lowercase().removePrefix("verify_")) {
      "vaccination" -> Syringe
      "vaccine_stock" -> Vaccine
      else -> Module
    }
  `;
  const missing = findMissing(bootstrap, icons);
  if (missing.length !== 1 || missing[0] !== "new_backend_tab") {
    console.error(`android-nav-icon-coverage self-test failed: ${JSON.stringify(missing)}`);
    process.exit(1);
  }
  console.log("android-nav-icon-coverage self-test passed");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const missing = findMissing(
  readFileSync(resolve(repo, BOOTSTRAP), "utf8"),
  readFileSync(resolve(repo, ICONS), "utf8"),
);

if (missing.length) {
  console.error("android-nav-icon-coverage guard FAILED — backend nav keys missing Android MeshaIcons.forNavKey mapping:");
  for (const key of missing) {
    console.error(`- ${key}`);
  }
  console.error(`Add an explicit mapping in ${ICONS}; do not let user-facing tabs fall back to the generic Module icon.`);
  process.exit(1);
}

console.log("android-nav-icon-coverage guard passed");
