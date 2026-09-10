#!/usr/bin/env node
// Blocks backend hot read models from minting signed proof-media URLs on list/dashboard reads.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");

const HOT_READ_FILES = [
  "backend/internal/verification/app/service.go",
  "backend/internal/verification/adapters/proofmedia/resolver.go",
  "backend/internal/processintegrity/app/service.go",
  "backend/internal/vaccinationexecution/app/service.go",
  "backend/internal/weighing/adapters/http/handler.go",
  "backend/internal/bootstrap/api.go",
];

const forbidden = [
  "ResolveMedia(",
  "DownloadURL(",
  "DownloadArtifact(",
  "ResolveProofDownloadURL(",
];

const forbiddenMethodNames = [
  "ResolveMedia",
  "DownloadURL",
  "DownloadArtifact",
  "ResolveProofDownloadURL",
];

const hotFunctionsByFile = {
  "backend/internal/verification/app/service.go": ["ListQueue", "resolveMedia"],
  "backend/internal/verification/adapters/proofmedia/resolver.go": ["ResolveMedia"],
  "backend/internal/processintegrity/app/service.go": ["ActionCenter", "ProtocolAdherence", "withEvidenceMedia"],
  "backend/internal/vaccinationexecution/app/service.go": ["ScanRoster"],
  "backend/internal/weighing/adapters/http/handler.go": ["GetLeadershipShedVideos", "ListLeadershipSheds", "resolveLeadershipMedia"],
  "backend/internal/bootstrap/api.go": ["ResolveProofDownloadURL"],
};

const broadAllowlist = [
  "backend/internal/verification/app/verdict_evidence_gate_test.go",
];

const explicitSignerFunctions = new Map([
  ["backend/internal/proof/adapters/http/handler.go", new Set(["Download", "DownloadSigned", "logProofDownloadURLIssued", "logProofDownloadEvent", "verifySignedURL"])],
  ["backend/internal/leadershiptasks/app/service.go", new Set(["AttachmentDownloadURL"])],
  ["backend/internal/leadershiptasks/adapters/http/handler.go", new Set(["DownloadAttachment"])],
]);

const hotName = /^(List|Get|Search|Scan|Summary|Dashboard|ControlTower|Overview|Roster|Load|Fetch|Export|ActionCenter|ProtocolAdherence|withEvidenceMedia|resolveLeadershipMedia|resolveProofVideoURL)/;

function text(rel) {
  return readFileSync(resolve(repo, rel), "utf8");
}

function lineNo(source, index) {
  return source.slice(0, index).split("\n").length;
}

function walk(dir, acc = []) {
  let entries = [];
  try { entries = readdirSync(dir); } catch { return acc; }
  for (const entry of entries) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      walk(full, acc);
      continue;
    }
    const rel = relative(repo, full);
    if (
      rel.startsWith("backend/internal/") &&
      rel.endsWith(".go") &&
      !rel.endsWith("_test.go") &&
      !rel.includes("/ports/")
    ) {
      acc.push(rel);
    }
  }
  return acc;
}

function functionRanges(source) {
  const ranges = [];
  const re = /\nfunc (?:\([^)]*\) )?([A-Za-z0-9_]+)\b/g;
  const matches = [...source.matchAll(re)];
  for (let i = 0; i < matches.length; i += 1) {
    const match = matches[i];
    const start = (match.index ?? 0) + 1;
    const end = i + 1 < matches.length ? (matches[i + 1].index ?? source.length) + 1 : source.length;
    ranges.push({ name: match[1], start, end, body: source.slice(start, end) });
  }
  return ranges;
}

function packageDir(rel) {
  const index = rel.lastIndexOf("/");
  return index >= 0 ? rel.slice(0, index) : "";
}

function packageSignerHelpers(files) {
  const helpers = new Map();
  for (const rel of files) {
    if (broadAllowlist.some((prefix) => rel.startsWith(prefix) || rel === prefix)) continue;
    const source = text(rel);
    for (const fn of functionRanges(source)) {
      if (explicitSignerFunctions.get(rel)?.has(fn.name)) continue;
      if (!functionUsesForbiddenSigner(fn.body)) continue;
      const dir = packageDir(rel);
      if (!helpers.has(dir)) helpers.set(dir, new Set());
      helpers.get(dir).add(fn.name);
    }
  }
  return helpers;
}

function functionUsesForbiddenSigner(body) {
  if (forbiddenMethodNames.some((name) => forbiddenCallRegex(name).test(body))) return true;
  return forbiddenMethodNames.some((name) => {
    const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    return methodValueCaptureRegex(escaped).test(body);
  });
}

function forbiddenCallRegex(name) {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`\\b${escaped}\\s*\\(`, "g");
}

function methodValueCaptureRegex(escapedName, flags = "") {
  return new RegExp(`\\b(?:var\\s+)?[A-Za-z_][A-Za-z0-9_]*\\s*(?::=|(?<![=!<>])=(?!=))\\s*[^\\n;]*\\.${escapedName}\\b(?!\\s*\\()`, flags);
}

function scanFile(rel) {
  if (!existsSync(resolve(repo, rel))) return [`missing hot read file: ${rel}`];
  const source = text(rel);
  const findings = [];
  for (const fn of hotFunctionsByFile[rel] ?? []) {
    const fnStart = source.search(new RegExp(`func \\([^)]*\\) ${fn}\\b|func ${fn}\\b`));
    if (fnStart === -1) {
      findings.push(`${rel}: missing hot read function ${fn}`);
      continue;
    }
    const nextFn = source.slice(fnStart + 1).search(/\nfunc (?:\([^)]*\) )?[A-Za-z0-9_]+\b/);
    const bodyEnd = nextFn === -1 ? source.length : fnStart + 1 + nextFn;
    const body = source.slice(fnStart, bodyEnd);
    for (const name of forbiddenMethodNames) {
      for (const match of body.matchAll(forbiddenCallRegex(name))) {
        const index = fnStart + (match.index ?? 0);
        const line = source.split("\n")[lineNo(source, index) - 1] ?? "";
        if (line.trimStart().startsWith("func ")) continue;
        if (line.includes("backend-proof-media-egress:ignore")) continue;
        findings.push(`${rel}:${lineNo(source, index)}: hot read function ${fn} must not call ${name}; return /app/proofs/{id}/download and sign only on explicit open`);
      }
    }
    for (const name of forbiddenMethodNames) {
      const re = methodValueCaptureRegex(name, "g");
      for (const match of body.matchAll(re)) {
        const index = fnStart + (match.index ?? 0);
        const line = source.split("\n")[lineNo(source, index) - 1] ?? "";
        if (line.includes("backend-proof-media-egress:ignore")) continue;
        findings.push(`${rel}:${lineNo(source, index)}: hot read function ${fn} must not capture ${name} as a signer method value; return /app/proofs/{id}/download and sign only on explicit open`);
      }
    }
  }
  return findings;
}

function scanBroadFile(rel, packageHelpers = new Map()) {
  if (broadAllowlist.some((prefix) => rel.startsWith(prefix) || rel === prefix)) return [];
  if (!existsSync(resolve(repo, rel))) return [];
  const source = text(rel);
  const findings = [];
  const signerHelpers = packageHelpers.get(packageDir(rel)) ?? new Set();
  for (const fn of functionRanges(source)) {
    if (explicitSignerFunctions.get(rel)?.has(fn.name)) continue;
    if (!hotName.test(fn.name)) continue;
    for (const name of forbiddenMethodNames) {
      for (const match of fn.body.matchAll(forbiddenCallRegex(name))) {
        const index = fn.start + (match.index ?? 0);
        const line = source.split("\n")[lineNo(source, index) - 1] ?? "";
        if (line.trimStart().startsWith("func ")) continue;
        if (line.includes("backend-proof-media-egress:ignore")) continue;
        findings.push(`${rel}:${lineNo(source, index)}: read-like function ${fn.name} must not call ${name}; sign on explicit media open only`);
      }
    }
    for (const name of forbiddenMethodNames) {
      const re = methodValueCaptureRegex(name, "g");
      for (const match of fn.body.matchAll(re)) {
        const index = fn.start + (match.index ?? 0);
        const line = source.split("\n")[lineNo(source, index) - 1] ?? "";
        if (line.includes("backend-proof-media-egress:ignore")) continue;
        findings.push(`${rel}:${lineNo(source, index)}: read-like function ${fn.name} must not capture ${name} as a signer method value; sign on explicit media open only`);
      }
    }
    for (const helper of signerHelpers) {
      if (helper === fn.name) continue;
      const re = new RegExp(`\\b${helper}\\s*\\(`, "g");
      for (const match of fn.body.matchAll(re)) {
        const index = fn.start + (match.index ?? 0);
        const line = source.split("\n")[lineNo(source, index) - 1] ?? "";
        if (line.includes("backend-proof-media-egress:ignore")) continue;
        findings.push(`${rel}:${lineNo(source, index)}: read-like function ${fn.name} calls helper ${helper}, which mints signed proof-media URLs; sign on explicit media open only`);
      }
    }
  }
  return findings;
}

function scanBroadText(source, rel = "backend/internal/example/app/service.go") {
  const findings = [];
  const funcs = functionRanges(`\n${source}`);
  const helpers = new Set(funcs.filter((fn) => functionUsesForbiddenSigner(fn.body)).map((fn) => fn.name));
  for (const fn of funcs) {
    if (!hotName.test(fn.name)) continue;
    for (const name of forbiddenMethodNames) {
      if (forbiddenCallRegex(name).test(fn.body)) findings.push(`${rel}: ${fn.name}: ${name}`);
    }
    for (const name of forbiddenMethodNames) {
      if (methodValueCaptureRegex(name).test(fn.body)) findings.push(`${rel}: ${fn.name}: method value ${name}`);
    }
    for (const helper of helpers) {
      if (helper !== fn.name && new RegExp(`\\b${helper}\\s*\\(`).test(fn.body)) findings.push(`${rel}: ${fn.name}: helper ${helper}`);
    }
  }
  return findings;
}

function scanBroadTexts(files) {
  const helpers = packageSignerHelpersFromTexts(files);
  return files.flatMap(({ rel, source }) => scanBroadTextWithHelpers(source, rel, helpers.get(packageDir(rel)) ?? new Set()));
}

function packageSignerHelpersFromTexts(files) {
  const helpers = new Map();
  for (const { rel, source } of files) {
    for (const fn of functionRanges(`\n${source}`)) {
      if (!functionUsesForbiddenSigner(fn.body)) continue;
      const dir = packageDir(rel);
      if (!helpers.has(dir)) helpers.set(dir, new Set());
      helpers.get(dir).add(fn.name);
    }
  }
  return helpers;
}

function scanBroadTextWithHelpers(source, rel, helpers) {
  const findings = [];
  for (const fn of functionRanges(`\n${source}`)) {
    if (!hotName.test(fn.name)) continue;
    for (const helper of helpers) {
      if (helper !== fn.name && new RegExp(`\\b${helper}\\s*\\(`).test(fn.body)) findings.push(`${rel}: ${fn.name}: helper ${helper}`);
    }
  }
  return findings;
}

function selfTest() {
  const bad = "func (s *Service) ListQueue(){ _ = media.ResolveMedia(ctx, tenant, ids) }\nfunc (s *Service) RecordVerdict(){ _ = media.ResolveMedia(ctx, tenant, ids) }\n";
  const good = 'func (s *Service) ListQueue(){ url := "/app/proofs/" + proofID + "/download" }\nfunc (s *Service) RecordVerdict(){ _ = media.ResolveMedia(ctx, tenant, ids) }\n';
  const broadBad = "func (h *Handler) GetGallery(){ _ = media.DownloadURL(ctx, tenant, id) }\n";
  const broadGood = 'func (h *Handler) GetGallery(){ url := "/app/proofs/" + proofID + "/download" }\n';
  const summaryBad = "func (h *Handler) Summary(){ _ = media.DownloadArtifact(ctx, tenant, id) }\n";
  const allowedDownload = "func (h *Handler) Download(){ _ = media.DownloadArtifact(ctx, tenant, id) }\n";
  const helperBad = "func (s *Service) ListQueue(){ _ = s.attachMedia(row) }\nfunc (s *Service) attachMedia(row Row){ _ = s.media.DownloadURL(ctx, tenant, row.ID) }\n";
  const methodValueBad = "func (s *Service) ListQueue(){ sign := s.proofMedia.DownloadURL; _ = sign(ctx, tenant, proofID) }\n";
  const whitespaceCallBad = "func (s *Service) ListQueue(){ _ = media.DownloadURL (ctx, tenant, proofID) }\n";
  const exportBad = "func (s *Service) ExportCampaignCSV(){ _ = media.ResolveProofDownloadURL(ctx, tenant, proofID) }\n";
  const crossFileHelperBad = scanBroadTexts([
    { rel: "backend/internal/foo/app/service.go", source: "func (s *Service) ListDashboard(){ _ = s.attachSignedProof(ctx, proofID) }\n" },
    { rel: "backend/internal/foo/app/media.go", source: "func (s *Service) attachSignedProof(ctx context.Context, proofID string) string { url, _ := s.proofs.DownloadURL(ctx, tenantID, proofID); return url }\n" },
  ]);
  const crossFileMethodValueHelperBad = scanBroadTexts([
    { rel: "backend/internal/foo/app/service.go", source: "func (s *Service) ListDashboard(){ _ = s.attachSignedProof(ctx, proofID) }\n" },
    { rel: "backend/internal/foo/app/media.go", source: "func (s *Service) attachSignedProof(ctx context.Context, proofID string) string { sign := s.proofs.DownloadURL; url, _ := sign(ctx, tenantID, proofID); return url }\n" },
  ]);
  const fakeRel = "backend/internal/verification/app/service.go";
  const scan = (source) => {
    const findings = [];
    for (const fn of ["ListQueue"]) {
      const fnStart = source.search(new RegExp(`func \\([^)]*\\) ${fn}\\b|func ${fn}\\b`));
      const nextFn = source.slice(fnStart + 1).search(/\nfunc (?:\([^)]*\) )?[A-Za-z0-9_]+\b/);
      const bodyEnd = nextFn === -1 ? source.length : fnStart + 1 + nextFn;
      const body = source.slice(fnStart, bodyEnd);
      for (const name of forbiddenMethodNames) {
        if (forbiddenCallRegex(name).test(body)) findings.push(`${fakeRel}: ${name}`);
      }
    }
    return findings;
  };
  const allowedRel = "backend/internal/proof/adapters/http/handler.go";
  const cases = [
    ["pinned-hot-read", scan(bad).length, 1],
    ["pinned-good", scan(good).length, 0],
    ["broad-direct", scanBroadText(broadBad).length, 1],
    ["broad-good", scanBroadText(broadGood).length, 0],
    ["summary-direct", scanBroadText(summaryBad).length, 1],
    ["same-file-helper", scanBroadText(helperBad).length, 1],
    ["method-value", scanBroadText(methodValueBad).length, 1],
    ["whitespace-call", scanBroadText(whitespaceCallBad).length, 1],
    ["export-signing", scanBroadText(exportBad).length, 1],
    ["cross-file-helper", crossFileHelperBad.length, 1],
    ["cross-file-method-value-helper", crossFileMethodValueHelperBad.length, 1],
    ["explicit-download-allowed", scanBroadText(allowedDownload, allowedRel).length, 0],
  ];
  if (!cases.every(([, got, want]) => got === want)) {
    for (const [name, got, want] of cases) {
      if (got !== want) console.error(`${name}: got ${got}, want ${want}`);
    }
    console.error("backend-proof-media-egress self-test: FAIL");
    process.exit(1);
  }
  console.log("backend-proof-media-egress self-test: ok");
}

if (process.argv.includes("--self-test")) {
  selfTest();
  process.exit(0);
}

const findings = HOT_READ_FILES.flatMap(scanFile);
const backendFiles = walk(resolve(repo, "backend/internal"));
const packageHelpers = packageSignerHelpers(backendFiles);
findings.push(...backendFiles.flatMap((rel) => scanBroadFile(rel, packageHelpers)));
for (const rel of explicitSignerFunctions.keys()) {
  if (!existsSync(resolve(repo, rel))) findings.push(`missing explicit-open signer file: ${rel}`);
}

if (findings.length > 0) {
  console.error("backend-proof-media-egress guard FAILED:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}

console.log(`backend-proof-media-egress guard: ok (${HOT_READ_FILES.length} pinned hot files + broad backend runtime scan)`);
