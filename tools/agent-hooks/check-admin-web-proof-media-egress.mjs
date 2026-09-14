#!/usr/bin/env node
// Blocks admin-web drawer/list/detail reads from resolving proof refs into signed GCS URLs
// or rendering proof media bytes before an explicit reviewer click.

import { execSync } from "node:child_process";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const ROOT = "apps/admin-web";
const BASE = process.env.ADMIN_WEB_PROOF_MEDIA_EGRESS_BASE || "origin/main";

function isSource(rel) {
  return rel.startsWith(`${ROOT}/`) &&
    /\.(ts|tsx)$/.test(rel) &&
    !/\.(test|spec|mock|stories)\.(ts|tsx)$/.test(rel) &&
    !rel.includes("/node_modules/") &&
    !rel.includes("/.next/") &&
    !rel.includes("/__tests__/") &&
    !rel.includes("/__mocks__/");
}

function walk(dir, acc = []) {
  let entries = [];
  try { entries = readdirSync(dir); } catch { return acc; }
  for (const entry of entries) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      if (["node_modules", ".next", "build", ".turbo"].includes(entry)) continue;
      walk(full, acc);
      continue;
    }
    const rel = relative(repo, full);
    if (isSource(rel)) acc.push(rel);
  }
  return acc;
}

function changedSources() {
  const names = new Set();
  const collect = (command) => {
    try {
      execSync(command, { cwd: repo, encoding: "utf8" })
        .split("\n")
        .map((line) => line.trim())
        .filter(isSource)
        .forEach((line) => names.add(line));
    } catch {
      if (command.includes(`${BASE}...HEAD`)) {
        throw new Error(`Cannot diff admin-web proof media egress base "${BASE}". Run with ADMIN_WEB_PROOF_MEDIA_EGRESS_BASE or fetch origin/main.`);
      }
    }
  };
  collect(`git diff --name-only ${BASE}...HEAD`);
  collect("git diff --name-only --cached");
  collect("git diff --name-only");
  return [...names];
}

function lineNo(text, index) {
  return text.slice(0, index).split("\n").length;
}

function hasSuppressionNear(source, index) {
  const line = lineNo(source, index) - 1;
  const lines = source.split("\n");
  return lines.slice(Math.max(0, line - 6), line + 1).some((value) => value.includes("admin-proof-media-egress:ignore"));
}

function functionRanges(source) {
  const ranges = [];
  const re = /\b(?:(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_][A-Za-z0-9_]*)\b|(?:export\s+)?(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^=\n]+)?=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_][A-Za-z0-9_]*)\s*=>)/g;
  const matches = [...source.matchAll(re)];
  for (let i = 0; i < matches.length; i += 1) {
    const match = matches[i];
    const start = match.index ?? 0;
    const end = i + 1 < matches.length ? (matches[i + 1].index ?? source.length) : source.length;
    ranges.push({ name: match[1] ?? match[2], start, end, body: source.slice(start, end) });
  }
  return ranges;
}

const hotReadName = /^(load|get|list|fetch|search|review|render|hydrate|open.*Drawer|.*Detail)/i;
const explicitOpenName = /^(resolve|open|download|play|share|get).*(Proof|VoiceNote).*(Url|Route|Media)?(Action)?$/i;

function scanText(rel, source) {
  if (rel === "apps/admin-web/app/api/proof-media/[proof_id]/route.ts") return [];
  if (rel === "apps/admin-web/lib/api/server.ts") {
    return scanServerApiText(rel, source);
  }
  if (/^\s*["']use client["']/m.test(source)) {
    return scanClientText(rel, source);
  }
  const findings = [];
  for (const fn of functionRanges(source)) {
    const isExplicitOpen = explicitOpenName.test(fn.name);
    const isHotRead = hotReadName.test(fn.name) || fn.name.endsWith("Action");
    if (!isHotRead || isExplicitOpen) continue;
    const forbidden = [
      ["getProofDownloadUrl(", "server action/detail read resolves a signed proof URL; return getProofDownloadRoute and sign only on explicit open"],
      ["download_url", "server action/detail read exposes signed download_url data; return backend proof route until explicit open"],
    ];
    for (const [needle, reason] of forbidden) {
      let offset = 0;
      while (true) {
        const local = fn.body.indexOf(needle, offset);
        if (local === -1) break;
        offset = local + needle.length;
        const index = fn.start + local;
        const line = source.split("\n")[lineNo(source, index) - 1] ?? "";
        if (hasSuppressionNear(source, index)) continue;
        findings.push({ rel, line: lineNo(source, index), reason, snippet: line.trim().slice(0, 180) });
      }
    }
  }
  return findings;
}

function scanServerApiText(rel, source) {
  const findings = [];
  for (const fn of functionRanges(source)) {
    if (!explicitOpenName.test(fn.name) && !hotReadName.test(fn.name)) continue;
    const proofDownloadCall = /client\.request\s*<[^>]*>\s*\(\s*path\s*,\s*\{[\s\S]{0,500}\}\s*\)/g;
    for (const match of fn.body.matchAll(proofDownloadCall)) {
      const call = match[0];
      const index = fn.start + (match.index ?? 0);
      if (!fn.body.includes("/app/proofs/") && !source.slice(Math.max(0, fn.start - 500), fn.end).includes("/app/proofs/")) continue;
      if (/headers\s*:\s*\{[\s\S]{0,160}Accept\s*:\s*["']application\/json["']/.test(call)) continue;
      if (/redirect\s*:\s*["']manual["']/.test(call)) continue;
      if (hasSuppressionNear(source, index)) continue;
      const line = source.split("\n")[lineNo(source, index) - 1] ?? "";
      findings.push({
        rel,
        line: lineNo(source, index),
        reason: "admin explicit proof download must request JSON or use manual redirect; otherwise Node can follow the 307 and stream GCS bytes",
        snippet: line.trim().slice(0, 180),
      });
    }
  }
  return findings;
}

function scanClientText(rel, source) {
  const findings = [];
  const proofUrlNames = new Set();
  for (const match of source.matchAll(/\b(?:const|let|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*[^;\n]*(?:proofUrls|download_url|downloadUrl|proofUrl)\b/g)) {
    proofUrlNames.add(match[1]);
  }
  const names = ["download_url", "activeMedia.download_url", "loaded.proofUrls", ...proofUrlNames];
  if (/\bopen\s*=\s*[^;\n]*(?:items\.find|find\(\s*\(?\s*item\b)[\s\S]{0,220}\bproofRef\b/.test(source)) {
    names.push("open.url");
  }
  for (const name of names) {
    const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const rules = [
      [new RegExp(`<img\\b[^>]*\\bsrc=\\{?[^>\\n]*${escaped}\\b`, "g"), "admin proof image/video must not auto-fetch proof bytes on drawer/list render; render an explicit open link/action"],
      [new RegExp(`<(?:video|audio|source)\\b[^>]*\\bsrc=\\{?[^>\\n]*${escaped}\\b`, "g"), "admin proof video/audio must not auto-fetch proof bytes on drawer/list render; resolve/play only after explicit user action"],
      [new RegExp(`<[A-Z][A-Za-z0-9_.]*\\b[^>]*(?:\\bsrc|\\burl|\\bmodel)=\\{?[^>\\n]*${escaped}\\b`, "g"), "admin proof media wrapper component must not auto-fetch proof bytes on drawer/list render; pass a click/open action instead"],
    ];
    for (const [re, reason] of rules) {
      for (const match of source.matchAll(re)) {
        const line = source.split("\n")[lineNo(source, match.index ?? 0) - 1] ?? "";
        if (hasSuppressionNear(source, match.index ?? 0)) continue;
        addFinding(findings, { rel, line: lineNo(source, match.index ?? 0), reason, snippet: line.trim().slice(0, 180) });
      }
    }
  }
  return findings;
}

function addFinding(findings, finding) {
  if (findings.some((existing) => existing.rel === finding.rel && existing.line === finding.line && existing.snippet === finding.snippet)) {
    return;
  }
  findings.push(finding);
}

function selfTest() {
  const serverBad = `export async function loadToxinTaskDetailAction(id: string) {
  const proofUrl = await getProofDownloadUrl(id);
  return { proofUrl };
}`;
  const serverGood = `export async function loadToxinTaskDetailAction(id: string) {
  const proofUrl = await getProofDownloadRoute(id);
  return { proofUrl };
}
export async function resolveVendorVoiceNoteUrlAction(id: string) {
  return getProofDownloadUrl(id);
}`;
  const serverApiBad = `export async function getProofDownloadUrl(proofRef: string) {
  const path = \`/app/proofs/\${encodeURIComponent(proofRef)}/download\`;
  return client.request<{ download_url: string }>(path, { cache: "no-store" });
}`;
  const serverApiGood = `export async function getProofDownloadUrl(proofRef: string) {
  const path = \`/app/proofs/\${encodeURIComponent(proofRef)}/download\`;
  return client.request<{ download_url: string }>(path, { cache: "no-store", headers: { Accept: "application/json" } });
}`;
  const clientBad = `'use client';
export function Drawer({ loaded, task }) {
  return <img src={loaded.proofUrls[task.strip_photo_ref] ?? undefined} />;
}`;
  const clientWrapperBad = `'use client';
export function Drawer({ activeMedia }) {
  return <Image src={activeMedia.download_url} alt="" />;
}`;
  const clientGood = `'use client';
export function Drawer({ loaded, task }) {
  return <a href={loaded.proofUrls[task.strip_photo_ref] ?? undefined}>Open media</a>;
}`;
  const clientGenericOpenBad = `'use client';
export function Lightbox({ items }) {
  const open = items.find((item) => item.proofRef === "proof-1");
  return open ? <img src={open.url} alt="" /> : null;
}`;
  const clientGenericOpenGood = `'use client';
export function Lightbox({ items }) {
  const open = items.find((item) => item.proofRef === "proof-1");
  return open ? (
    // admin-proof-media-egress:ignore explicit one-proof open with telemetry
    <img src={open.url} alt="" />
  ) : null;
}`;
  const cases = [
    ["server-bad", scanText("apps/admin-web/features/x/actions.ts", serverBad).length, 1],
    ["server-arrow-bad", scanText("apps/admin-web/features/x/actions.ts", serverBad.replace("export async function loadToxinTaskDetailAction(id: string)", "export const loadToxinTaskDetailAction = async (id: string) =>")).length, 1],
    ["server-good", scanText("apps/admin-web/features/x/actions.ts", serverGood).length, 0],
    ["server-api-bad", scanText("apps/admin-web/lib/api/server.ts", serverApiBad).length, 1],
    ["server-api-good", scanText("apps/admin-web/lib/api/server.ts", serverApiGood).length, 0],
    ["client-bad", scanText("apps/admin-web/features/x/drawer.tsx", clientBad).length, 1],
    ["client-wrapper-bad", scanText("apps/admin-web/features/x/drawer.tsx", clientWrapperBad).length, 1],
    ["client-good", scanText("apps/admin-web/features/x/drawer.tsx", clientGood).length, 0],
    ["client-generic-open-bad", scanText("apps/admin-web/features/x/lightbox.tsx", clientGenericOpenBad).length, 1],
    ["client-generic-open-good", scanText("apps/admin-web/features/x/lightbox.tsx", clientGenericOpenGood).length, 0],
  ];
  const ok = cases.every(([, got, want]) => got === want);
  if (!ok) {
    for (const [name, got, want] of cases) {
      if (got !== want) console.error(`${name}: got ${got}, want ${want}`);
    }
  }
  console.log(ok ? "admin-web-proof-media-egress self-test: ok" : "admin-web-proof-media-egress self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const targets = process.argv.includes("--all") ? walk(resolve(repo, ROOT)) : changedSources();
const findings = targets.flatMap((rel) => scanText(rel, readFileSync(resolve(repo, rel), "utf8")));
if (findings.length) {
  console.error("admin-web-proof-media-egress guard FAILED:");
  for (const finding of findings) {
    console.error(`- ${finding.rel}:${finding.line}: ${finding.reason}`);
    console.error(`  ${finding.snippet}`);
  }
  process.exit(1);
}
console.log(`admin-web-proof-media-egress guard: ok (${targets.length} files checked)`);
