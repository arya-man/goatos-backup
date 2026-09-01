#!/usr/bin/env node

// check-ui-title-case.mjs
//
// User-facing frontend/mobile navigation and title copy uses caption case:
// "Purchase and born", "Preventive Care", "Feed transport". Do not shout
// whole labels in ALL CAPS and do not use internal short forms like "PC" in
// visible module titles.

import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const SMALL_WORDS = new Set(["a", "an", "and", "as", "at", "but", "by", "for", "from", "in", "nor", "of", "on", "or", "per", "the", "to", "with"]);
const ALLOWED_ACRONYMS = new Set(["CEO", "COO", "CXO", "DLQ", "ET", "FMD", "HF", "HRMS", "ID", "KPI", "ORS", "PPR", "RFID", "SOP", "TT", "UHT"]);

function stripPlaceholders(value) {
  return value
    .replace(/%[0-9$]*[sd]/g, "")
    .replace(/\{[^}]+\}/g, "")
    .replace(/&amp;/g, "&")
    .trim();
}

function words(value) {
  return stripPlaceholders(value).match(/[A-Za-z][A-Za-z']*/g) ?? [];
}

function isAllCapsWord(word) {
  return word.length > 1 && word === word.toUpperCase() && /[A-Z]/.test(word) && !ALLOWED_ACRONYMS.has(word);
}

function checkCaptionCase(value) {
  const out = [];
  const clean = stripPlaceholders(value);
  if (/\bPurchase and Born\b/.test(clean)) out.push('write "Purchase and born", not "Purchase and Born"');
  if (/\bPC\b/.test(clean)) out.push('visible title uses short form "PC"; write "Preventive Care"');
  const ws = words(clean);
  ws.forEach((word, index) => {
    if (isAllCapsWord(word)) out.push(`all-caps word "${word}" in visible title`);
    if (index > 0 && SMALL_WORDS.has(word.toLowerCase()) && word[0] === word[0].toUpperCase()) {
      out.push(`small word "${word}" should be lowercase in caption case`);
    }
  });
  return out;
}

function backendNavFindings(source) {
  const findings = [];
  const re = /\b(?:navItem|navItemDomain|navLeaf|navLeafDomain)\(\s*"[^"]+"\s*,\s*"([^"]+)"/g;
  let match;
  while ((match = re.exec(source)) !== null) {
    for (const reason of checkCaptionCase(match[1])) {
      const line = source.slice(0, match.index).split("\n").length;
      findings.push(`backend/internal/adminui/app/service.go:${line}: ${reason}: ${JSON.stringify(match[1])}`);
    }
  }
  const groupRe = /\bID:\s*"[^"]+"\s*,\s*Label:\s*"([^"]+)"/g;
  while ((match = groupRe.exec(source)) !== null) {
    for (const reason of checkCaptionCase(match[1])) {
      const line = source.slice(0, match.index).split("\n").length;
      findings.push(`backend/internal/adminui/app/service.go:${line}: ${reason}: ${JSON.stringify(match[1])}`);
    }
  }
  return findings;
}

function androidStringFindings(file, source) {
  const findings = [];
  const re = /<string\s+name="([^"]*(?:title|label|nav|module|tab|group)[^"]*)">([^<]+)<\/string>/g;
  let match;
  while ((match = re.exec(source)) !== null) {
    const [, name, raw] = match;
    for (const reason of checkCaptionCase(raw)) {
      const line = source.slice(0, match.index).split("\n").length;
      findings.push(`${file}:${line}: ${reason}: ${name}=${JSON.stringify(raw)}`);
    }
  }
  return findings;
}

function cssFindings(source) {
  const findings = [];
  const navGroupRule = source.match(/\.ggrp\{[^}]*\}/)?.[0] ?? "";
  if (/text-transform\s*:\s*uppercase/i.test(navGroupRule)) {
    findings.push("apps/admin-web/app/mesha-theme.css: .ggrp must not force module labels to uppercase");
  }
  return findings;
}

function findings() {
  const out = [];
  out.push(...backendNavFindings(readFileSync(resolve(repo, "backend/internal/adminui/app/service.go"), "utf8")));
  out.push(...cssFindings(readFileSync(resolve(repo, "apps/admin-web/app/mesha-theme.css"), "utf8")));
  const androidFiles = execFileSync("git", ["ls-files", "apps/goatos-android/**/src/main/res/values/strings.xml"], { cwd: repo, encoding: "utf8" })
    .trim()
    .split("\n")
    .filter(Boolean);
  for (const file of androidFiles) {
    out.push(...androidStringFindings(file, readFileSync(resolve(repo, file), "utf8")));
  }
  return out;
}

function selfTest() {
  const bad = [
    ["Purchase and Born", "Purchase and born"],
    ["PREVENTIVE CARE", "all-caps"],
    ["Preventive Care (PC)", "short form"],
  ];
  for (const [value, expected] of bad) {
    if (!checkCaptionCase(value).some((item) => item.includes(expected))) {
      throw new Error(`self-test failed to flag ${value}`);
    }
  }
  for (const good of ["Purchase and born", "Preventive Care", "RFID reader", "ET+TT"]) {
    const got = checkCaptionCase(good);
    if (got.length) throw new Error(`self-test wrongly flagged ${good}: ${got.join(", ")}`);
  }
  console.log("check-ui-title-case self-test: PASS");
}

if (process.argv.includes("--self-test")) {
  selfTest();
} else {
  const got = findings();
  if (got.length) {
    console.error("check-ui-title-case: FAIL");
    for (const item of got) console.error(`- ${item}`);
    process.exit(1);
  }
  console.log("check-ui-title-case: PASS");
}
