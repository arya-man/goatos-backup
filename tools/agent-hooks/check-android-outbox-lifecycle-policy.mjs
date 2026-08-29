#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const repo = path.resolve(new URL("../..", import.meta.url).pathname);
const enumPath = "apps/goatos-android/core/core-database/src/main/kotlin/sg/mesha/goatos/core/database/outbox/OutboxEntity.kt";
const policyPath = "apps/goatos-android/core/core-database/src/main/kotlin/sg/mesha/goatos/core/database/outbox/OutboxLifecyclePolicy.kt";
const REQUIRED_AXES = ["userImpact", "immediate", "success", "terminalFailure", "processDeath"];

function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/^\s*\/\/.*$/gm, "");
}

function balancedBody(source, start, open = "{", close = "}") {
  let depth = 0;
  for (let i = start; i < source.length; i += 1) {
    if (source[i] === open) depth += 1;
    if (source[i] === close) depth -= 1;
    if (depth === 0) return source.slice(start + 1, i);
  }
  throw new Error(`unbalanced '${open}${close}' block`);
}

export function enumEntries(source) {
  const clean = stripComments(source);
  const declaration = clean.indexOf("enum class OutboxOpType");
  if (declaration < 0) throw new Error("OutboxOpType enum declaration not found");
  const body = balancedBody(clean, clean.indexOf("{", declaration));
  return body
    .split(",")
    .map((part) => part.trim().match(/^([A-Z][A-Z0-9_]*)\b/)?.[1])
    .filter(Boolean);
}

function lifecycleArguments(source, functionName) {
  const helper = new RegExp(`private\\s+fun\\s+${functionName}\\s*\\(\\s*\\)\\s*=\\s*lifecycle\\s*\\(`).exec(source);
  if (!helper) throw new Error(`${functionName}: policy profile does not resolve directly to lifecycle(...)`);
  const paren = source.indexOf("(", helper.index + helper[0].lastIndexOf("lifecycle"));
  return balancedBody(source, paren, "(", ")");
}

export function policyArms(source) {
  const clean = stripComments(source);
  const mapping = clean.indexOf("val OutboxOpType.lifecyclePolicy");
  if (mapping < 0) throw new Error("OutboxOpType.lifecyclePolicy declaration not found");
  const whenStart = clean.indexOf("when (this)", mapping);
  const body = balancedBody(clean, clean.indexOf("{", whenStart));
  if (/\belse\s*->/.test(body)) {
    throw new Error("lifecycle policy must not use an else branch; new enum values must fail compilation");
  }

  const arms = [];
  const matcher = /OutboxOpType\.([A-Z][A-Z0-9_]*)\s*->\s*([a-zA-Z][a-zA-Z0-9_]*)\s*\(/g;
  for (let match = matcher.exec(body); match; match = matcher.exec(body)) {
    arms.push({
      name: match[1],
      profile: match[2],
      arguments: match[2] === "lifecycle"
        ? balancedBody(body, matcher.lastIndex - 1, "(", ")")
        : lifecycleArguments(clean, match[2]),
    });
  }
  return arms;
}

export function validate(enumSource, policySource) {
  const problems = [];
  let operations;
  let arms;
  try {
    operations = enumEntries(enumSource);
    arms = policyArms(policySource);
  } catch (error) {
    return [error.message];
  }

  const counts = new Map();
  for (const arm of arms) counts.set(arm.name, (counts.get(arm.name) || 0) + 1);
  for (const operation of operations) {
    const count = counts.get(operation) || 0;
    if (count === 0) problems.push(`${operation}: missing lifecycle policy`);
    if (count > 1) problems.push(`${operation}: lifecycle policy declared ${count} times`);
  }
  for (const arm of arms) {
    if (!operations.includes(arm.name)) problems.push(`${arm.name}: policy names no OutboxOpType`);
    for (const axis of REQUIRED_AXES) {
      if (!new RegExp(`\\b${axis}\\s*=`).test(arm.arguments)) {
        problems.push(`${arm.name}: ${arm.profile} is missing explicit ${axis} policy`);
      }
    }
  }
  return problems;
}

function selfTest() {
  const enumSource = "enum class OutboxOpType { ONE, TWO }";
  const validPolicy = `
    val OutboxOpType.lifecyclePolicy get() = when (this) {
      OutboxOpType.ONE -> lifecycle(userImpact = U, immediate = I, success = S, terminalFailure = F, processDeath = P)
      OutboxOpType.TWO -> completeLifecycle()
    }
    private fun completeLifecycle() = lifecycle(userImpact = U, immediate = I, success = S, terminalFailure = F, processDeath = P)
  `;
  if (validate(enumSource, validPolicy).length !== 0) throw new Error("self-test: valid fixture failed");

  const missing = validate(enumSource, validPolicy.replace(/OutboxOpType\.TWO[^\n]+/, ""));
  if (!missing.some((problem) => problem.includes("TWO: missing"))) {
    throw new Error("self-test: missing operation was not rejected");
  }

  const duplicate = validate(enumSource, validPolicy.replace("OutboxOpType.TWO", "OutboxOpType.ONE"));
  if (!duplicate.some((problem) => problem.includes("declared 2 times"))) {
    throw new Error("self-test: duplicate operation was not rejected");
  }

  const missingAxis = validate(enumSource, validPolicy.replace("terminalFailure = F, ", ""));
  if (!missingAxis.some((problem) => problem.includes("terminalFailure"))) {
    throw new Error("self-test: missing lifecycle axis was not rejected");
  }

  const wildcard = validate(enumSource, validPolicy.replace("OutboxOpType.TWO", "else"));
  if (!wildcard.some((problem) => problem.includes("else branch"))) {
    throw new Error("self-test: wildcard branch was not rejected");
  }

  console.log("android outbox lifecycle policy guard: self-test passed");
}

function run() {
  const enumSource = fs.readFileSync(path.join(repo, enumPath), "utf8");
  const policySource = fs.readFileSync(path.join(repo, policyPath), "utf8");
  const problems = validate(enumSource, policySource);
  if (problems.length > 0) {
    console.error("android outbox lifecycle policy guard: FAIL");
    for (const problem of problems) console.error(`  - ${problem}`);
    process.exit(1);
  }
  console.log(`android outbox lifecycle policy guard: ${enumEntries(enumSource).length} operations covered`);
}

if (process.argv.includes("--self-test")) selfTest();
else run();
