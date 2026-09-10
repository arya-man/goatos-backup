#!/usr/bin/env node
// Blocks Android proof preview patterns that silently turn signed GCS URLs into paid reads.

import { execSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";

const repo = resolve(import.meta.dirname, "../..");
const BASE = process.env.ANDROID_PROOF_MEDIA_EGRESS_BASE || "origin/main";
const ROOT = "apps/goatos-android";

function isSource(rel) {
  return rel.startsWith(`${ROOT}/`) && /\/src\/(?!test\/|androidTest\/)[^/]+\//.test(rel) && /\.(kt|java)$/.test(rel);
}

function walk(dir, acc = []) {
  let entries = [];
  try { entries = readdirSync(dir); } catch { return acc; }
  for (const entry of entries) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) walk(full, acc);
    else {
      const rel = relative(repo, full);
      if (isSource(rel)) acc.push(rel);
    }
  }
  return acc;
}

function changedSources() {
  const names = new Set();
  const collect = (command) => {
    try {
      execSync(command, { encoding: "utf8" })
        .split("\n")
        .map((line) => line.trim())
        .filter(isSource)
        .forEach((line) => names.add(line));
    } catch (error) {
      if (command.includes(`${BASE}...HEAD`)) {
        throw new Error(`Cannot diff Android proof media egress base "${BASE}". Run with a valid ANDROID_PROOF_MEDIA_EGRESS_BASE or fetch origin/main.`);
      }
    }
  };
  collect(`git -C "${repo}" diff --name-only ${BASE}...HEAD`);
  collect(`git -C "${repo}" diff --name-only --cached`);
  collect(`git -C "${repo}" diff --name-only`);
  return [...names];
}

function lineNo(text, index) {
  return text.slice(0, index).split("\n").length;
}

function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function suppressionFor(text, index) {
  const lines = text.slice(0, index).split("\n");
  const previous = lines.at(-2)?.trim() ?? "";
  const current = text.split("\n")[lineNo(text, index) - 1]?.trim() ?? "";
  const nearby = [previous, current]
    .map((line) => line.replace(/^\/\/\s*/, ""))
    .find((line) => line.includes("proof-media-egress:"));
  if (!nearby) return null;
  if (/proof-media-egress:(ignore|stable-local)\s+.{32,}/.test(nearby) && /\b(tap|click|explicit|local|cache|bounded|lifecycle|telemetry|proof id|outbox|slot)\b/i.test(nearby)) return nearby;
  return "invalid";
}

function taintedIdentifiers(text) {
  const names = new Set();
  const sources = "(?:media\\.signedUrl|media\\.url|signedUrl|downloadUrl|mediaUrl|url|path)";
  const declarations = new RegExp(`\\b(?:val|var)\\s+([A-Za-z_][A-Za-z0-9_]*)(?:\\s*:\\s*[A-Za-z0-9_<>,.? ]+)?\\s*=\\s*(?:Uri\\.parse\\s*\\()?(${sources}|[A-Za-z_][A-Za-z0-9_]*)`, "g");
  let changed = true;
  while (changed) {
    changed = false;
    for (const match of text.matchAll(declarations)) {
      const [, name, source] = match;
      if (names.has(name)) continue;
      if (new RegExp(`^${sources}$`).test(source) || names.has(source)) {
        names.add(name);
        changed = true;
      }
    }
  }
  for (const match of text.matchAll(/\b(?:val|var)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*media\b/g)) {
    names.add(`${match[1]}.signedUrl`);
    names.add(`${match[1]}.url`);
  }
  return [...names];
}

function aliasedNames(text, qualifiedName, canonicalName) {
  const names = new Set([canonicalName]);
  const escaped = escapeRegExp(qualifiedName);
  for (const match of text.matchAll(new RegExp(`\\bimport\\s+${escaped}\\s+as\\s+([A-Za-z_][A-Za-z0-9_]*)`, "g"))) {
    names.add(match[1]);
  }
  return [...names];
}

function remoteMediaHelperNames(text) {
  const helpers = new Set();
  const re = /\bfun\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*\{/g;
  for (const match of text.matchAll(re)) {
    const start = match.index ?? 0;
    let depth = 0;
    let end = start;
    for (; end < text.length; end += 1) {
      const ch = text[end];
      if (ch === "{") depth += 1;
      if (ch === "}") {
        depth -= 1;
        if (depth === 0) {
          end += 1;
          break;
        }
      }
    }
    const body = text.slice(start, end);
    const params = match[2]
      .split(",")
      .map((param) => param.trim().match(/^([A-Za-z_][A-Za-z0-9_]*)\s*:/)?.[1])
      .filter(Boolean);
    for (const param of params) {
      const p = escapeRegExp(param);
      const risky = [
        ...aliasedNames(text, "androidx.media3.common.MediaItem", "MediaItem").flatMap((name) => [
          new RegExp(`setMediaItem\\s*\\(\\s*${escapeRegExp(name)}\\.fromUri\\s*\\([^)]*${p}[^)]*\\)[\\s\\S]{0,300}\\bprepare\\s*\\(`),
          new RegExp(`(?:setMediaItems|addMediaItem)\\s*\\([^)]*${escapeRegExp(name)}\\.fromUri\\s*\\([^)]*${p}[^)]*\\)[\\s\\S]{0,300}\\bprepare\\s*\\(`),
        ]),
        new RegExp(`setDataSource\\s*\\(\\s*${p}\\s*\\)[\\s\\S]{0,300}\\bprepareAsync\\s*\\(`),
        ...aliasedNames(text, "java.net.URL", "URL").map((name) => new RegExp(`${escapeRegExp(name)}\\s*\\(\\s*${p}\\s*\\)\\.(?:openStream|readBytes|readText)\\s*\\(`)),
        ...aliasedNames(text, "java.net.URL", "URL").map((name) => new RegExp(`${escapeRegExp(name)}\\s*\\(\\s*${p}\\s*\\)\\.openConnection\\s*\\(\\s*\\)\\.(?:getInputStream\\s*\\(|inputStream\\b)`)),
        ...aliasedNames(text, "java.net.URI", "URI").map((name) => new RegExp(`(?:java\\.net\\.)?${escapeRegExp(name)}\\.create\\s*\\(\\s*${p}\\s*\\)\\.toURL\\s*\\(\\s*\\)\\.(?:openStream|readBytes|readText)\\s*\\(`)),
        new RegExp(`ImageRequest\\.Builder\\s*\\([^)]*\\)[\\s\\S]{0,400}\\.data\\s*\\(\\s*${p}\\s*\\)`),
        ...aliasedNames(text, "coil.compose.AsyncImage", "AsyncImage").map((name) => new RegExp(`${escapeRegExp(name)}\\s*\\([\\s\\S]{0,300}model\\s*=\\s*${p}\\b`)),
        ...aliasedNames(text, "coil.compose.SubcomposeAsyncImage", "SubcomposeAsyncImage").map((name) => new RegExp(`${escapeRegExp(name)}\\s*\\([\\s\\S]{0,300}model\\s*=\\s*${p}\\b`)),
      ].some((rule) => rule.test(body));
      if (risky) helpers.add(match[1]);
    }
  }
  const extensionRe = /\bfun\s+String\.([A-Za-z_][A-Za-z0-9_]*)\s*\([^)]*\)\s*(?::\s*[A-Za-z0-9_<>,.? ]+)?\s*(?:=\s*([^\n]+)|\{)/g;
  for (const match of text.matchAll(extensionRe)) {
    const start = match.index ?? 0;
    let body = match[2] ?? "";
    if (!body) {
      let depth = 0;
      let end = start;
      for (; end < text.length; end += 1) {
        const ch = text[end];
        if (ch === "{") depth += 1;
        if (ch === "}") {
          depth -= 1;
          if (depth === 0) {
            end += 1;
            break;
          }
        }
      }
      body = text.slice(start, end);
    }
    const risky = [
      ...aliasedNames(text, "java.net.URL", "URL").map((name) => new RegExp(`${escapeRegExp(name)}\\s*\\(\\s*this\\s*\\)\\.(?:openStream|readBytes|readText)\\s*\\(`)),
      ...aliasedNames(text, "java.net.URL", "URL").map((name) => new RegExp(`${escapeRegExp(name)}\\s*\\(\\s*this\\s*\\)\\.openConnection\\s*\\(\\s*\\)\\.(?:getInputStream\\s*\\(|inputStream\\b)`)),
      ...aliasedNames(text, "java.net.URI", "URI").map((name) => new RegExp(`${escapeRegExp(name)}\\.create\\s*\\(\\s*this\\s*\\)\\.toURL\\s*\\(\\s*\\)\\.(?:openStream|readBytes|readText)\\s*\\(`)),
    ].some((rule) => rule.test(body));
    if (risky) helpers.add(match[1]);
  }
  return [...helpers];
}

const rules = [
  {
    re: /(?:repository|feedRepository|fastingRepository|pcCareRepository)\.(?:proofDownloadUrl|fetchProofDownloadUrl|resolveProofDownloadUrl|getProofDownloadUrl)\s*\(/g,
    reason: "ViewModel pre-hydrates a signed proof URL; carry /app/proofs/{id}/download until explicit media open",
  },
  {
    re: /URL\s*\([^)]*(?:path|url|signedUrl|downloadUrl)[^)]*\)\.openStream\s*\(/g,
    reason: "raw remote object download from proof/media preview; use explicit player/user action",
  },
  {
    re: /URL\s*\([^)]*(?:path|url|signedUrl|downloadUrl)[^)]*\)\.(?:readBytes|readText)\s*\(/g,
    reason: "raw Kotlin URL helper download from proof/media URL",
  },
  {
    re: /URL\s*\([^)]*(?:path|url|signedUrl|downloadUrl)[^)]*\)\.openConnection\s*\(\s*\)\.getInputStream\s*\(/g,
    reason: "raw remote object input stream from proof/media URL",
  },
  {
    re: /URL\s*\([^)]*(?:path|url|signedUrl|downloadUrl)[^)]*\)\.openConnection\s*\(\s*\)\.inputStream\b/g,
    reason: "raw remote object input stream from proof/media URL",
  },
  {
    re: /\bval\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*URL\s*\([^)]*(?:path|url|signedUrl|downloadUrl)[^)]*\)\.openConnection\s*\(\s*\)[\s\S]{0,300}\b\1\.inputStream\b/g,
    reason: "raw remote object input stream from proof/media URL through connection variable",
  },
  {
    re: /(?:java\.net\.)?URI\.create\s*\([^)]*(?:path|url|signedUrl|downloadUrl)[^)]*\)\.toURL\s*\(\s*\)\.(?:openStream|readBytes|readText)\s*\(/g,
    reason: "raw remote object download through URI.create(...).toURL() from proof/media URL",
  },
  {
    re: /setDataSource\s*\(\s*(?:path|url|signedUrl|downloadUrl)\s*,\s*emptyMap\s*\(\s*\)\s*\)/g,
    reason: "remote MediaMetadataRetriever probe on a signed URL",
  },
  {
    re: /setDataSource\s*\(\s*(?:media\.url|media\.signedUrl|signedUrl|downloadUrl|url)\s*,\s*headers\s*\)/g,
    reason: "remote MediaMetadataRetriever probe on a signed/media URL",
  },
  {
    re: /setDataSource\s*\(\s*context\s*,\s*Uri\.parse\s*\(\s*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url)\s*\)\s*\)/g,
    reason: "remote MediaMetadataRetriever probe on a signed/media URL",
  },
  {
    re: /setDataSource\s*\(\s*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url)\s*\)/g,
    reason: "remote MediaMetadataRetriever/MediaPlayer source on a signed/media URL must be reviewed",
  },
  {
    re: /\bMediaPlayer\s*\(\s*\)[\s\S]{0,500}\.setDataSource\s*\(\s*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url)\s*\)[\s\S]{0,300}\.prepareAsync\s*\(/g,
    reason: "raw MediaPlayer playback of signed proof media must have lifecycle stop, telemetry, and guard-reviewed attribution",
  },
  {
    re: /\b[A-Za-z_][A-Za-z0-9_]*\.setDataSource\s*\(\s*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url)\s*\)[\s\S]{0,300}\.prepareAsync\s*\(/g,
    reason: "raw MediaPlayer playback of signed proof media must have lifecycle stop, telemetry, and guard-reviewed attribution",
  },
  {
    re: /(?:AsyncImage|SubcomposeAsyncImage)\s*\([^)]*model\s*=\s*(?:media\.signedUrl|signedUrl|downloadUrl|mediaUrl|url)/gs,
    reason: "remote image proof preview must be tap-gated and keyed by stable proof identity",
  },
  {
    re: /rememberAsyncImagePainter\s*\([^)]*model\s*=\s*(?:media\.signedUrl|signedUrl|downloadUrl|mediaUrl|url)/gs,
    reason: "remote image proof preview must be tap-gated and keyed by stable proof identity",
  },
  {
    re: /ImageRequest\.Builder\s*\([^)]*\)[\s\S]{0,400}\.data\s*\(\s*(?:media\.signedUrl|signedUrl|downloadUrl|mediaUrl|url)/g,
    reason: "remote image request must be tap-gated and keyed by stable proof identity",
  },
  {
    re: /\b[A-Za-z_][A-Za-z0-9_.]*(?:\s*\([^)]*\))?(?:\.Builder\s*\(\s*\)\.build\s*\(\s*\))?\.newCall\s*\(\s*Request\.Builder\s*\(\s*\)[\s\S]{0,400}\.url\s*\(\s*(?:media\.signedUrl|signedUrl|downloadUrl|mediaUrl|url)/g,
    reason: "direct OkHttp proof/media download must be explicit, bounded, cached, and attributed",
  },
  {
    re: /\bval\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*Request\.Builder\s*\(\s*\)[\s\S]{0,300}\.url\s*\(\s*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url)\s*\)[\s\S]{0,300}\.build\s*\(\s*\)[\s\S]{0,500}\.newCall\s*\(\s*\1\s*\)/g,
    reason: "direct OkHttp proof/media download through a request variable must be explicit, bounded, cached, and attributed",
  },
  {
    re: /setMediaItem\s*\(\s*MediaItem\.fromUri\s*\([^)]*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url)[^)]*\)[\s\S]{0,300}\bprepare\s*\(/g,
    reason: "remote media player prepare must be gated by explicit user action and stable identity",
  },
  {
    re: /(?:setMediaItems|addMediaItem)\s*\([^)]*MediaItem\.fromUri\s*\([^)]*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url)[^)]*\)[\s\S]{0,300}\bprepare\s*\(/g,
    reason: "remote media player prepare must be gated by explicit user action and stable identity",
  },
  {
    re: /setRequestProperty\s*\(\s*"Range"\s*,\s*"bytes=/g,
    reason: "manual byte-range readability probe against proof media",
  },
  {
    re: /remember(?:Saveable)?\s*\([^)]*(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl)[^)]*\)/g,
    reason: "proof player/preview state keyed by signed URL instead of stable proof identity",
  },
  {
    re: /LaunchedEffect\s*\([^)]*resume[^)]*\)\s*\{[\s\S]{0,500}armed\s*=\s*true/g,
    reason: "fullscreen resume must not auto-arm a proof player; wait for explicit play/open",
  },
  {
    re: /(?:signedUrl|downloadUrl|mediaUrl|media\.url|url)\s*(?:\.encodedQuery\s*\(\s*null\s*\)|\.substringBefore\s*\(\s*['"]\?['"]\s*\))/g,
    reason: "signed URL identity derived by stripping query; pass explicit proof/slot identity",
  },
];

function allTaintedNames(text) {
  return [
    ...taintedIdentifiers(text),
    "media.signedUrl",
    "media.url",
    "signedUrl",
    "downloadUrl",
    "mediaUrl",
    "url",
    "path",
  ];
}

function taintedExpression(expr, text) {
  const value = expr.trim();
  return allTaintedNames(text).some((name) => value === name || value.startsWith(`${name}.`)) ||
    /(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl|url|path)/.test(value);
}

function scanRemoteMediaHelperCalls(rel, text, helperNames) {
  const findings = [];
  for (const helper of helperNames) {
    const helperCall = new RegExp(`\\b${escapeRegExp(helper)}\\s*\\(\\s*([^),\\n]+)`, "g");
    for (const match of text.matchAll(helperCall)) {
      const arg = match[1]?.trim() ?? "";
      if (!taintedExpression(arg, text)) continue;
      const line = text.split("\n")[lineNo(text, match.index ?? 0) - 1] ?? "";
      const trimmed = line.trim();
      const suppression = suppressionFor(text, match.index ?? 0);
      if (trimmed.startsWith("//") || suppression?.startsWith("proof-media-egress:ignore ")) continue;
      findings.push({
        rel,
        line: lineNo(text, match.index ?? 0),
        reason: `signed proof/media URL is passed into helper ${helper} that performs remote media IO/prepare`,
        snippet: trimmed.slice(0, 160),
      });
    }
    const extensionCall = new RegExp(`\\b([A-Za-z_][A-Za-z0-9_.]*)\\s*\\.\\s*${escapeRegExp(helper)}\\s*\\(`, "g");
    for (const match of text.matchAll(extensionCall)) {
      const receiver = match[1]?.trim() ?? "";
      if (!taintedExpression(receiver, text)) continue;
      const line = text.split("\n")[lineNo(text, match.index ?? 0) - 1] ?? "";
      const trimmed = line.trim();
      const suppression = suppressionFor(text, match.index ?? 0);
      if (trimmed.startsWith("//") || suppression?.startsWith("proof-media-egress:ignore ")) continue;
      findings.push({
        rel,
        line: lineNo(text, match.index ?? 0),
        reason: `signed proof/media URL receiver calls extension helper ${helper} that performs remote media IO`,
        snippet: trimmed.slice(0, 160),
      });
    }
  }
  return findings;
}

function scanTapArmedRemoteImageRotation(rel, text) {
  const findings = [];
  const armedStateRe = /\bvar\s+([A-Za-z_][A-Za-z0-9_]*)\s+by\s+rememberSaveable\s*\(([^)]*)\)\s*\{\s*mutableStateOf\s*\(\s*false\s*\)\s*\}/g;
  for (const match of text.matchAll(armedStateRe)) {
    const [, stateName, keyArgs] = match;
    if (/(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|mediaUrl)/.test(keyArgs)) continue;
    const state = escapeRegExp(stateName);
    const hasRemoteImageBehindState = new RegExp(`\\bif\\s*\\(\\s*${state}\\s*\\)\\s*\\{[\\s\\S]{0,1600}(?:AsyncImage|SubcomposeAsyncImage|ImageRequest\\.Builder)[\\s\\S]{0,900}(?:\\.data\\s*\\(|model\\s*=\\s*)(?:media\\.signedUrl|media\\.url|signedUrl|downloadUrl|mediaUrl|url)`, "g").test(text);
    if (!hasRemoteImageBehindState) continue;
    const resetsOnUrlRotation = new RegExp(`LaunchedEffect\\s*\\([^)]*(?:media\\.signedUrl|media\\.url|signedUrl|downloadUrl|mediaUrl)[^)]*\\)\\s*\\{[\\s\\S]{0,500}\\b${state}\\s*=\\s*false`, "g").test(text);
    if (resetsOnUrlRotation) continue;
    const suppression = suppressionFor(text, match.index ?? 0);
    if (suppression?.startsWith("proof-media-egress:ignore ")) continue;
    const line = text.split("\n")[lineNo(text, match.index ?? 0) - 1] ?? "";
    findings.push({
      rel,
      line: lineNo(text, match.index ?? 0),
      reason: "tap-gated remote proof image state must reset when signed URL rotates, otherwise a previous tap can silently reload remote bytes",
      snippet: line.trim().slice(0, 160),
    });
  }
  return findings;
}

function scanText(rel, text, externalRemoteMediaHelpers = new Set()) {
  const findings = [];
  for (const rule of rules) {
    for (const match of text.matchAll(rule.re)) {
      const line = text.split("\n")[lineNo(text, match.index ?? 0) - 1] ?? "";
      const trimmed = line.trim();
      const suppression = suppressionFor(text, match.index ?? 0);
      if (
        trimmed.startsWith("//") ||
        suppression === "proof-media-egress:ignore" ||
        suppression?.startsWith("proof-media-egress:ignore ")
      ) continue;
      findings.push({ rel, line: lineNo(text, match.index ?? 0), reason: rule.reason, snippet: trimmed.slice(0, 160) });
    }
  }
  for (const name of taintedIdentifiers(text)) {
    const escaped = escapeRegExp(name);
    const mediaItemNames = aliasedNames(text, "androidx.media3.common.MediaItem", "MediaItem");
    const imageRequestNames = aliasedNames(text, "coil.request.ImageRequest", "ImageRequest");
    const requestNames = aliasedNames(text, "okhttp3.Request", "Request");
    const urlNames = aliasedNames(text, "java.net.URL", "URL");
    const uriNames = aliasedNames(text, "java.net.URI", "URI");
    const asyncImageNames = aliasedNames(text, "coil.compose.AsyncImage", "AsyncImage");
    const subcomposeImageNames = aliasedNames(text, "coil.compose.SubcomposeAsyncImage", "SubcomposeAsyncImage");
    const aliasRules = [
      ...urlNames.map((urlName) => ({
        re: new RegExp(`${escapeRegExp(urlName)}\\s*\\(\\s*${escaped}\\s*\\)\\.open(?:Stream|Connection)\\s*\\(`, "g"),
        reason: `raw remote proof/media download through alias ${name}`,
      })),
      {
        re: new RegExp(`(?:${[...asyncImageNames, ...subcomposeImageNames].map(escapeRegExp).join("|")})\\s*\\([\\s\\S]{0,300}model\\s*=\\s*${escaped}\\b`, "g"),
        reason: `remote image proof preview through signed URL alias ${name}`,
      },
      {
        re: new RegExp(`(?:${imageRequestNames.map(escapeRegExp).join("|")})\\.Builder\\s*\\([^)]*\\)[\\s\\S]{0,400}\\.data\\s*\\(\\s*${escaped}\\s*\\)`, "g"),
        reason: `remote image request through signed URL alias ${name}`,
      },
      {
        re: new RegExp(`rememberAsyncImagePainter\\s*\\([\\s\\S]{0,300}model\\s*=\\s*${escaped}\\b`, "g"),
        reason: `remote image proof preview through signed URL alias ${name}`,
      },
      {
        re: new RegExp(`(?:java\\.net\\.)?(?:${uriNames.map(escapeRegExp).join("|")})\\.create\\s*\\(\\s*${escaped}\\s*\\)\\.toURL\\s*\\(\\s*\\)\\.(?:openStream|readBytes|readText)\\s*\\(`, "g"),
        reason: `raw remote object download through URI.create(...).toURL() alias ${name}`,
      },
      {
        re: new RegExp(`setMediaItem\\s*\\(\\s*(?:${mediaItemNames.map(escapeRegExp).join("|")})\\.fromUri\\s*\\([^)]*${escaped}[^)]*\\)[\\s\\S]{0,300}\\bprepare\\s*\\(`, "g"),
        reason: `remote media player prepare through signed URL alias ${name}`,
      },
      {
        re: new RegExp(`setMediaItem\\s*\\(\\s*(?:${mediaItemNames.map(escapeRegExp).join("|")})\\.Builder\\s*\\(\\s*\\)[\\s\\S]{0,300}\\.setUri\\s*\\([^)]*${escaped}[^)]*\\)[\\s\\S]{0,300}\\.build\\s*\\(\\s*\\)[\\s\\S]{0,300}\\bprepare\\s*\\(`, "g"),
        reason: `remote media player prepare through MediaItem.Builder signed URL alias ${name}`,
      },
      {
        re: new RegExp(`remember(?:Saveable)?\\s*\\([^)]*${escaped}[^)]*\\)`, "g"),
        reason: `proof player/preview state keyed by signed URL alias ${name}`,
      },
      {
        re: new RegExp(`\\b[A-Za-z_][A-Za-z0-9_.]*\\.newCall\\s*\\(\\s*(?:${requestNames.map(escapeRegExp).join("|")})\\.Builder\\s*\\(\\s*\\)[\\s\\S]{0,400}\\.url\\s*\\(\\s*${escaped}\\s*\\)`, "g"),
        reason: `direct OkHttp proof/media download through signed URL alias ${name}`,
      },
      {
        re: new RegExp(`setDataSource\\s*\\(\\s*${escaped}\\s*\\)[\\s\\S]{0,300}\\bprepareAsync\\s*\\(`, "g"),
        reason: `raw MediaPlayer playback through signed URL alias ${name}`,
      },
    ];
    for (const rule of aliasRules) {
      for (const match of text.matchAll(rule.re)) {
        const line = text.split("\n")[lineNo(text, match.index ?? 0) - 1] ?? "";
        const trimmed = line.trim();
        const suppression = suppressionFor(text, match.index ?? 0);
        if (trimmed.startsWith("//") || suppression?.startsWith("proof-media-egress:ignore ")) continue;
        findings.push({ rel, line: lineNo(text, match.index ?? 0), reason: rule.reason, snippet: trimmed.slice(0, 160) });
      }
    }
  }
  findings.push(...scanRemoteMediaHelperCalls(rel, text, new Set([...remoteMediaHelperNames(text), ...externalRemoteMediaHelpers])));
  findings.push(...scanTapArmedRemoteImageRotation(rel, text));
  findings.push(...scanReplacementStateRegression(rel, text));
  if (!/ProofMediaPreview|VerifyProofPhoto|VerifyVideoPlayer|FullscreenVideoDialog/.test(text)) return findings;
  const tainted = taintedIdentifiers(text);
  for (const match of text.matchAll(/ProofMediaPreview\s*\(/g)) {
    const start = match.index ?? 0;
    const prefix = text.slice(Math.max(0, start - 80), start);
    if (/\bfun\s+$/.test(prefix)) continue;
    let depth = 0;
    let end = start;
    for (; end < text.length; end += 1) {
      const ch = text[end];
      if (ch === "(") depth += 1;
      if (ch === ")") {
        depth -= 1;
        if (depth === 0) {
          end += 1;
          break;
        }
      }
    }
    const call = text.slice(start, end);
    const previousLine = text.slice(0, start).split("\n").at(-2)?.trim() ?? "";
    const stableLocal = call.includes("proof-media-egress:stable-local") || previousLine.includes("proof-media-egress:stable-local");
    const path = call.match(/\bpath\s*=\s*([^,\n)]+)/)?.[1]?.trim() ?? "";
    const pathLooksRemote = /(?:media\.signedUrl|media\.url|signedUrl|downloadUrl|remotePreviewUrl|url)/.test(path) ||
      tainted.some((name) => path === name || path.startsWith(`${name}.`));
    if (stableLocal && pathLooksRemote) {
      findings.push({
        rel,
        line: lineNo(text, start),
        reason: "ProofMediaPreview stable-local exemption cannot be used with a signed/remote path",
        snippet: call.split("\n").slice(0, 5).join(" ").trim().slice(0, 160),
      });
      continue;
    }
    if (
      !/\bmediaIdentity\s*=/.test(call) &&
      !stableLocal
    ) {
      findings.push({
        rel,
        line: lineNo(text, start),
        reason: "ProofMediaPreview must pass stable mediaIdentity so refreshed signed URLs cannot reset/re-arm preview state",
        snippet: call.split("\n")[0].trim().slice(0, 160),
      });
    } else {
      const identity = call.match(/\bmediaIdentity\s*=\s*([^,\n)]+)/)?.[1]?.trim() ?? "";
      const invalidIdentity =
        !identity ||
        /^""$/.test(identity) ||
        /\.orEmpty\s*\(\s*\)/.test(identity) ||
        /(?:^|\.)(path|previewPath|previewToShow|title|label|signedUrl|downloadUrl|remotePreviewUrl|url)$/.test(identity) ||
        /^(media\.signedUrl|media\.url)$/.test(identity);
      if (invalidIdentity && !stableLocal) {
        findings.push({
          rel,
          line: lineNo(text, start),
          reason: "ProofMediaPreview mediaIdentity must not be a path/signed URL; use proof, slot, outbox, or attachment identity",
          snippet: call.split("\n").slice(0, 5).join(" ").trim().slice(0, 160),
        });
      }
    }
  }
  return findings;
}

function scanReplacementStateRegression(rel, text) {
  if (!rel.includes("/viewmodel/") || !text.includes("captureReplacingLatest")) return [];
  const findings = [];
  for (const match of text.matchAll(/captureReplacingLatest\s*\(/g)) {
    const start = match.index ?? 0;
    const window = text.slice(start, start + 2500);
    const bad = window.match(/\b(?:videoCaptured|captured)\s*=\s*false\b/);
    if (!bad) continue;
    const absolute = start + (bad.index ?? 0);
    const line = text.split("\n")[lineNo(text, absolute) - 1] ?? "";
    if (line.includes("proof-media-egress:ignore")) continue;
    findings.push({
      rel,
      line: lineNo(text, absolute),
      reason: "captureReplacingLatest failure/cancel paths must preserve old visible proof state; do not set captured/videoCaptured=false unless the new proof succeeded",
      snippet: line.trim().slice(0, 180),
    });
  }
  return findings;
}

function scanFile(rel, externalRemoteMediaHelpers = new Set()) {
  if (!existsSync(resolve(repo, rel))) return [];
  return scanText(rel, readFileSync(resolve(repo, rel), "utf8"), externalRemoteMediaHelpers);
}

function selfTest() {
  const rel = "apps/goatos-android/feature/x/src/main/kotlin/Foo.kt";
  const proof = "ProofMediaPreview(path = signedUrl)\n";
  const stable = "ProofMediaPreview(path = signedUrl, mediaIdentity = proofId)\n";
  const cases = [
    ["openStream", scanText(rel, proof + "URL(path).openStream()").length, 2],
    ["url-read-bytes", scanText(rel, proof + "URL(downloadUrl).readBytes()").length, 2],
    ["url-read-text", scanText(rel, proof + "URL(media.signedUrl).readText()").length, 2],
    ["openConnection", scanText(rel, proof + "URL(downloadUrl).openConnection().getInputStream()").length, 2],
    ["uri-create-to-url-openStream", scanText(rel, proof + "java.net.URI.create(downloadUrl).toURL().openStream().use { it.readBytes() }").length, 2],
    ["uri-create-to-url-alias", scanText(rel, proof + "val proofUri = media.signedUrl\nURI.create(proofUri).toURL().readBytes()").length, 2],
    ["retriever-empty", scanText(rel, proof + "retriever.setDataSource(signedUrl, emptyMap())").length, 2],
    ["retriever-headers", scanText(rel, proof + "retriever.setDataSource(media.url, headers)").length, 2],
    ["retriever-context-uri", scanText(rel, proof + "retriever.setDataSource(context, Uri.parse(media.signedUrl))").length, 2],
    ["async-direct", scanText(rel, proof + "AsyncImage(model = media.signedUrl)").length, 2],
    ["subcompose-async-direct", scanText(rel, proof + "SubcomposeAsyncImage(model = media.signedUrl, contentDescription = null)").length, 2],
    ["async-alias", scanText(rel, proof + "val proofUri = media.signedUrl\nAsyncImage(model = proofUri)").length, 2],
    ["subcompose-async-alias", scanText(rel, proof + "val proofUri = media.signedUrl\nSubcomposeAsyncImage(model = proofUri, contentDescription = null)").length, 2],
    ["subcompose-async-import-alias", scanText(rel, proof + "import coil.compose.SubcomposeAsyncImage as RemoteImage\nval proofUri = media.signedUrl\nRemoteImage(model = proofUri, contentDescription = null)").length, 2],
    ["async-painter", scanText(rel, proof + "rememberAsyncImagePainter(model = media.signedUrl)").length, 2],
    ["media-object-alias", scanText(rel, proof + "val item = media\nImageRequest.Builder(context).data(item.signedUrl).build()").length, 2],
    ["image-direct", scanText(rel, proof + "ImageRequest.Builder(context).data(downloadUrl).build()").length, 2],
    ["image-alias", scanText(rel, proof + "val proofUri = downloadUrl\nImageRequest.Builder(context).data(proofUri).build()").length, 2],
    ["image-request-import-alias", scanText(rel, proof + "import coil.request.ImageRequest as CoilReq\nval proofUri = media.signedUrl\nCoilReq.Builder(context).data(proofUri).build()").length, 2],
    ["okhttp", scanText(rel, proof + "client.newCall(Request.Builder().url(downloadUrl).build())").length, 2],
    ["okhttp-request-import-alias", scanText(rel, proof + "import okhttp3.Request as HttpRequest\nval proofUri = media.signedUrl\nclient.newCall(HttpRequest.Builder().url(proofUri).build())").length, 2],
    ["okhttp-receiver", scanText(rel, proof + "okHttpClient.newCall(Request.Builder().url(downloadUrl).build())").length, 2],
    ["okhttp-builder-chain", scanText(rel, proof + "OkHttpClient.Builder().build().newCall(Request.Builder().url(downloadUrl).build())").length, 2],
    ["okhttp-request-var", scanText(rel, proof + "val request = Request.Builder().url(media.signedUrl).build()\nokHttpClient.newCall(request).execute()").length, 2],
    ["player-direct", scanText(rel, proof + "player.setMediaItem(MediaItem.fromUri(media.url)); player.prepare()").length, 2],
    ["player-set-items", scanText(rel, proof + "player.setMediaItems(listOf(MediaItem.fromUri(Uri.parse(media.signedUrl))))\nplayer.prepare()").length, 2],
    ["player-add-item", scanText(rel, proof + "player.addMediaItem(MediaItem.fromUri(Uri.parse(media.signedUrl)))\nplayer.prepare()").length, 2],
    ["player-alias", scanText(rel, proof + "val proofUri = media.url\nplayer.setMediaItem(MediaItem.fromUri(proofUri)); player.prepare()").length, 2],
    ["player-import-alias", scanText(rel, proof + "import androidx.media3.common.MediaItem as MItem\nval proofUri = media.url\nplayer.setMediaItem(MItem.fromUri(proofUri)); player.prepare()").length, 2],
    ["player-builder-set-uri", scanText(rel, proof + "val proofUri = media.url\nplayer.setMediaItem(MediaItem.Builder().setUri(proofUri).build())\nplayer.prepare()").length, 2],
    ["player-builder-set-uri-import-alias", scanText(rel, proof + "import androidx.media3.common.MediaItem as MItem\nval proofUri = media.url\nplayer.setMediaItem(MItem.Builder().setUri(proofUri).build())\nplayer.prepare()").length, 2],
    ["player-helper-param", scanText(rel, proof + "val playbackUri = media.signedUrl\npreparePlayer(playbackUri)\nfun preparePlayer(source: String) {\nplayer.setMediaItem(MediaItem.fromUri(Uri.parse(source)))\nplayer.prepare()\n}").length, 2],
    ["url-extension-helper", scanText(rel, proof + "val proofUri = media.signedUrl\nproofUri.openProofBytes()\nfun String.openProofBytes(): ByteArray = URL(this).openStream().readBytes()").length, 2],
    ["url-extension-helper-import-alias", scanText(rel, proof + "import java.net.URL as NetUrl\nval proofUri = media.signedUrl\nproofUri.openProofBytes()\nfun String.openProofBytes(): ByteArray = NetUrl(this).openStream().readBytes()").length, 2],
    ["cross-file-player-helper", scanText(rel, proof + "val playbackUri = media.signedUrl\nprepareProofPlayer(playbackUri)\n", new Set(["prepareProofPlayer"])).length, 2],
    ["split-open-connection", scanText(rel, proof + "val connection = URL(downloadUrl).openConnection()\nconnection.inputStream.use { it.readBytes() }").length, 2],
    ["mediaplayer-alias", scanText(rel, proof + "val proofUri = media.url\nMediaPlayer().setDataSource(proofUri)\nmediaPlayer.prepareAsync()").length, 2],
    ["mediaplayer-param-url", scanText(rel, proof + "fun play(url: String) {\nval mediaPlayer = MediaPlayer()\nmediaPlayer.setDataSource(url)\nmediaPlayer.prepareAsync()\n}").length, 5],
    ["range", scanText(rel, proof + "connection.setRequestProperty(\"Range\", \"bytes=0-0\")").length, 2],
    ["stable-remember", scanText(rel, proof + "val player = remember(media.proofId) { factory.create(context) }").length, 1],
    ["remember-saveable-url-key", scanText(rel, proof + "var loadPhoto by rememberSaveable(media.proofSubject, media.signedUrl) { mutableStateOf(false) }").length, 2],
    ["tap-armed-remote-image-no-url-reset", scanText(rel, "var loadPhoto by rememberSaveable(media.proofSubject) { mutableStateOf(false) }\nif (loadPhoto) {\n// proof-media-egress:ignore explicit tap-gated image load; this fixture isolates URL rotation state reset coverage\nAsyncImage(model = ImageRequest.Builder(context).data(media.signedUrl).build(), contentDescription = null)\n}\n").length, 1],
    ["tap-armed-remote-image-url-reset", scanText(rel, "var loadPhoto by rememberSaveable(media.proofSubject) { mutableStateOf(false) }\nLaunchedEffect(media.signedUrl) { loadPhoto = false }\nif (loadPhoto) {\n// proof-media-egress:ignore explicit tap-gated image load; this fixture isolates URL rotation state reset coverage\nAsyncImage(model = ImageRequest.Builder(context).data(media.signedUrl).build(), contentDescription = null)\n}\n").length, 0],
    ["typed-alias-remember-saveable", scanText(rel, proof + "val key: String = media.signedUrl\nvar loadPhoto by rememberSaveable(key) { mutableStateOf(false) }").length, 2],
    ["chained-alias-remember", scanText(rel, proof + "val first: String = media.signedUrl\nval second = first\nval player = remember(second) { factory.create(context) }").length, 2],
    ["resume-auto-arm", scanText(rel, proof + "LaunchedEffect(player, resume) {\nval pending = resume ?: return@LaunchedEffect\nif (player == null) { if (pending.positionMs > 0L && !armed) armed = true }\n}").length, 2],
    ["stable-call", scanText(rel, stable).length, 0],
    ["stable-local-remote", scanText(rel, "ProofMediaPreview(path = media.signedUrl, kind = ProofMediaPreviewKind.Video, // proof-media-egress:stable-local This says local but is remote.\nmediaIdentity = media.signedUrl)\n").length, 1],
    ["stable-local-tainted-alias", scanText(rel, "val previewPath = media.signedUrl\nProofMediaPreview(path = previewPath, kind = ProofMediaPreviewKind.Video, // proof-media-egress:stable-local This says local but is remote.\nmediaIdentity = \"slot\")\n").length, 1],
    ["path-identity", scanText(rel, "ProofMediaPreview(path = signedUrl, mediaIdentity = path)\n").length, 1],
    ["qualified-path-identity", scanText(rel, "ProofMediaPreview(path = state.previewPath, mediaIdentity = state.previewPath)\n").length, 1],
    ["viewmodel-url-hydration", scanText("apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FooViewModel.kt", "repository.proofDownloadUrl(proofRef)\n").length, 1],
    ["replacement-erases-old-proof", scanText("apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FooViewModel.kt", "when (proofCaptureRepository.captureReplacingLatest()) { is AppResult.Err -> state.update { it.copy(videoCaptured = false) } }\n").length, 1],
    ["replacement-preserves-old-proof", scanText("apps/goatos-android/app/src/main/kotlin/sg/mesha/goatos/viewmodel/FooViewModel.kt", "when (proofCaptureRepository.captureReplacingLatest()) { is AppResult.Err -> state.update { it.copy(videoCaptured = it.videoCaptured) } }\n").length, 0],
    ["remember-url", scanText(rel, proof + "val player = remember(media.url) { factory.create(context) }").length, 2],
  ];
  const ok =
    cases.every(([, got, want]) => got === want);
  if (!ok) {
    for (const [name, got, want] of cases) {
      if (got !== want) console.error(`${name}: got ${got}, want ${want}`);
    }
  }
  console.log(ok ? "android-proof-media-egress self-test: ok" : "android-proof-media-egress self-test: FAIL");
  process.exit(ok ? 0 : 1);
}

if (process.argv.includes("--self-test")) selfTest();

const targets = process.argv.includes("--all") ? walk(resolve(repo, ROOT)) : changedSources();
const allSources = walk(resolve(repo, ROOT));
const globalRemoteMediaHelpers = new Set(
  allSources.flatMap((rel) => remoteMediaHelperNames(readFileSync(resolve(repo, rel), "utf8"))),
);
const findings = targets.flatMap((rel) => scanFile(rel, globalRemoteMediaHelpers));
if (findings.length) {
  console.error("android-proof-media-egress guard FAILED:");
  for (const finding of findings) {
    console.error(`- ${finding.rel}:${finding.line}: ${finding.reason}`);
    console.error(`  ${finding.snippet}`);
  }
  process.exit(1);
}
console.log(`android-proof-media-egress guard: ok (${targets.length} files checked)`);
