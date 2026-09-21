#!/usr/bin/env node
import { mkdtempSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";

const args = parseArgs(process.argv.slice(2));

if (args.selfTest) {
  selfTest();
  process.exit(0);
}

const refreshToken = process.env.GOATOS_FIREBASE_REFRESH_TOKEN?.trim() ?? "";
const apiKey = firebaseApiKey();
if (!refreshToken) fail("GOATOS_FIREBASE_REFRESH_TOKEN is required to refresh the dashboard automation bearer");
if (!apiKey) fail("GOATOS_FIREBASE_WEB_CONFIG or NEXT_PUBLIC_FIREBASE_API_KEY is required to refresh the dashboard automation bearer");
if (!args.outEnv) fail("--out-env is required");

const refreshed = await refresh(refreshToken, apiKey);
writeFileSync(args.outEnv, [
  shellExport("GOATOS_BEARER_TOKEN", refreshed.idToken),
  shellExport("GOATOS_FIREBASE_REFRESH_TOKEN", refreshed.refreshToken),
  "",
].join("\n"), { mode: 0o600 });

if (args.envFile) {
  updateEnvFile(args.envFile, {
    GOATOS_BEARER_TOKEN: refreshed.idToken,
    GOATOS_FIREBASE_REFRESH_TOKEN: refreshed.refreshToken,
  });
}

console.log("dashboard automation Firebase token refresh: ok");

async function refresh(token, key) {
  const response = await fetch(`https://securetoken.googleapis.com/v1/token?key=${encodeURIComponent(key)}`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams({ grant_type: "refresh_token", refresh_token: token }).toString(),
  });
  if (!response.ok) fail(`Firebase token refresh failed with HTTP ${response.status}`);
  const payload = await response.json();
  const idToken = typeof payload.id_token === "string" ? payload.id_token.trim() : "";
  const rotatedRefresh = typeof payload.refresh_token === "string" ? payload.refresh_token.trim() : token;
  if (!idToken || !rotatedRefresh) fail("Firebase token refresh response did not include usable tokens");
  return { idToken, refreshToken: rotatedRefresh };
}

function firebaseApiKey() {
  const raw = process.env.GOATOS_FIREBASE_WEB_CONFIG?.trim();
  if (raw) {
    try {
      const parsed = JSON.parse(raw);
      if (typeof parsed.apiKey === "string" && parsed.apiKey.trim()) return parsed.apiKey.trim();
    } catch {
      fail("GOATOS_FIREBASE_WEB_CONFIG is not valid JSON");
    }
  }
  return process.env.NEXT_PUBLIC_FIREBASE_API_KEY?.trim() ?? "";
}

function updateEnvFile(file, values) {
  const original = readFileSync(file, "utf8");
  let text = original;
  for (const [key, value] of Object.entries(values)) {
    const line = `${key}=${quoteForEnvFile(value)}`;
    const pattern = new RegExp(`^${escapeRegExp(key)}=.*$`, "m");
    text = pattern.test(text) ? text.replace(pattern, line) : `${text.replace(/\s*$/, "\n")}${line}\n`;
  }
  const dir = dirname(file);
  const tmp = join(mkdtempSync(join(tmpdir(), "goatos-dashboard-token-")), "env");
  writeFileSync(tmp, text, { mode: 0o600 });
  renameSync(tmp, file);
}

function shellExport(key, value) {
  return `export ${key}=${quoteForEnvFile(value)}`;
}

function quoteForEnvFile(value) {
  return `'${String(value).replaceAll("'", "'\\''")}'`;
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function parseArgs(raw) {
  const parsed = {};
  for (let i = 0; i < raw.length; i += 1) {
    const arg = raw[i];
    if (arg === "--self-test") parsed.selfTest = true;
    else if (arg === "--out-env") parsed.outEnv = raw[++i];
    else if (arg === "--env-file") parsed.envFile = raw[++i];
    else throw new Error(`unknown argument: ${arg}`);
  }
  return parsed;
}

function fail(message) {
  console.error(message);
  process.exit(1);
}

function selfTest() {
  const quoted = quoteForEnvFile("a'b");
  if (quoted !== "'a'\\''b'") throw new Error("quoteForEnvFile self-test failed");
  const tmp = join(mkdtempSync(join(tmpdir(), "goatos-dashboard-token-self-test-")), "env");
  writeFileSync(tmp, "A=1\nGOATOS_BEARER_TOKEN=old\n", { mode: 0o600 });
  updateEnvFile(tmp, { GOATOS_BEARER_TOKEN: "new", GOATOS_FIREBASE_REFRESH_TOKEN: "rotated" });
  const updated = readFileSync(tmp, "utf8");
  if (!updated.includes("GOATOS_BEARER_TOKEN='new'")) throw new Error("bearer update self-test failed");
  if (!updated.includes("GOATOS_FIREBASE_REFRESH_TOKEN='rotated'")) throw new Error("refresh append self-test failed");
  console.log("dashboard automation Firebase token refresh: self-test passed");
}
