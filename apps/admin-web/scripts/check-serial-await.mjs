#!/usr/bin/env node

import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";

const root = process.cwd();
const scanRoots = ["app", "features", "lib"]
  .map((dir) => path.join(root, dir))
  .filter((dir) => {
    try {
      return statSync(dir).isDirectory();
    } catch {
      return false;
    }
  });

const allowMarker = "serial-await: allow";
const skippedPathPatterns = [/^lib\/auth\//];
const findings = [];

function walk(dir, files = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "node_modules" || entry.name.startsWith(".")) continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      walk(full, files);
      continue;
    }
    if (!/\.(ts|tsx)$/.test(entry.name)) continue;
    if (/\.(test|spec)\.(ts|tsx)$/.test(entry.name)) continue;
    files.push(full);
  }
  return files;
}

function braceDelta(line) {
  let delta = 0;
  for (const ch of line) {
    if (ch === "{") delta += 1;
    if (ch === "}") delta -= 1;
  }
  return delta;
}

function isAllowed(line, previous = "") {
  return line.includes(allowMarker) || previous.includes(allowMarker);
}

function isAwaitStatement(line) {
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith("//")) return false;
  if (!trimmed.includes("await ")) return false;
  if (trimmed.includes("Promise.all") || trimmed.includes("Promise.allSettled")) return false;
  if (trimmed.includes("? await ")) return false;
  return /^(?:const|let|var)\s+[\w${}\[\],\s:]+=\s*await\b/.test(trimmed) || /^await\b/.test(trimmed);
}

function awaitedBinding(line) {
  const trimmed = line.trim();
  const direct = trimmed.match(/^(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*await\b/);
  if (direct) return direct[1];
  const object = trimmed.match(/^(?:const|let|var)\s+\{\s*([A-Za-z_$][\w$]*)\s*\}\s*=\s*await\b/);
  return object?.[1] ?? "";
}

function isResetLine(line) {
  const trimmed = line.trim();
  if (!trimmed || trimmed.startsWith("//")) return false;
  if (/^(?:const|let|var)\s+[\w${}\[\],\s:]+=\s*Promise\.(?:all|allSettled)\b/.test(trimmed)) return true;
  return /^(?:if|else|for|while|switch|try|catch|return|throw)\b/.test(trimmed) || /^[})\]]/.test(trimmed);
}

function checkFile(file) {
  const rel = path.relative(root, file);
  if (skippedPathPatterns.some((pattern) => pattern.test(rel))) return;
  const lines = readFileSync(file, "utf8").split(/\r?\n/);
  let depth = 0;
  let previousAwait = null;
  const loopStack = [];

  lines.forEach((line, index) => {
    const lineNo = index + 1;
    const previousLine = index > 0 ? lines[index - 1] : "";
    while (loopStack.length > 0 && depth < loopStack[loopStack.length - 1]) {
      loopStack.pop();
    }
    const startsLoop = /^\s*(?:for|while)\b/.test(line);
    if (startsLoop && line.includes("{")) {
      loopStack.push(depth + 1);
    }

    const awaitStatement = isAwaitStatement(line);
    if (loopStack.length > 0 && line.includes("await ") && !line.includes("Promise.all") && !isAllowed(line, previousLine)) {
      findings.push(`${rel}:${lineNo}: await inside a loop; batch with Promise.all or add // ${allowMarker} <reason>`);
    }
    if (awaitStatement && !isAllowed(line, previousLine)) {
      const dependsOnPrevious = previousAwait?.binding && new RegExp(`\\b${previousAwait.binding}\\b`).test(line);
      if (previousAwait && previousAwait.depth === depth && !dependsOnPrevious) {
        findings.push(`${rel}:${lineNo}: serial await after line ${previousAwait.line}; combine independent work with Promise.all or add // ${allowMarker} <reason>`);
      }
      previousAwait = { line: lineNo, depth, binding: awaitedBinding(line) };
    } else if (isResetLine(line)) {
      previousAwait = null;
    }

    depth += braceDelta(line);
    while (loopStack.length > 0 && depth < loopStack[loopStack.length - 1]) {
      loopStack.pop();
    }
  });
}

for (const dir of scanRoots) {
  for (const file of walk(dir)) {
    checkFile(file);
  }
}

if (findings.length > 0) {
  console.error("check-serial-await: found request-path await waterfalls\n");
  for (const finding of findings) {
    console.error(`  ${finding}`);
  }
  process.exit(1);
}

console.log("check-serial-await: OK");
