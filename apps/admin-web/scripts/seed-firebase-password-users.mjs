#!/usr/bin/env node

import { randomBytes } from "node:crypto";
import { execFileSync } from "node:child_process";

const IDENTITY_TOOLKIT_BASE_URL = "https://identitytoolkit.googleapis.com/v1";

const options = parseArgs(process.argv.slice(2));

if (options.help || !options.project || options.users.length === 0) {
  printUsage(options.help ? 0 : 1);
}

const activeProject = gcloudValue(["config", "get-value", "project"]);
const activeAccount = gcloudValue(["config", "get-value", "account"]);
if (activeProject !== options.project && !options.allowProjectMismatch) {
  fail(
    `Active gcloud project is ${activeProject || "<unset>"}, expected ${options.project}. ` +
      "Pass --allow-project-mismatch only after manually verifying the target.",
  );
}

const accessToken = process.env.GOOGLE_OAUTH_ACCESS_TOKEN || gcloudValue(["auth", "print-access-token"]);
if (!accessToken) {
  fail("Could not obtain a Google OAuth access token. Run `gcloud auth login ravi@mesha.sg` first.");
}

console.log(`Target project: ${options.project}`);
console.log(`Active gcloud account: ${activeAccount || "<unset>"}`);
console.log(`Mode: ${options.dryRun ? "dry-run" : "write"}`);
console.log(`Reset emails: ${options.sendResetEmail ? "send" : "skip"}`);
console.log("");

const results = [];
for (const user of options.users) {
  results.push(await seedUser(user));
}

console.table(
  results.map((result) => ({
    email: result.email,
    action: result.action,
    uid: result.localId,
    verified: result.emailVerified,
    reset_email: result.resetEmail,
  })),
);

async function seedUser(user) {
  const existing = await lookupUser(user.email);
  if (options.dryRun) {
    return {
      email: user.email,
      action: existing ? "would_update_existing" : "would_create",
      localId: existing?.localId || "",
      emailVerified: existing?.emailVerified === true,
      resetEmail: options.sendResetEmail ? "would_send" : "skipped",
    };
  }

  let action = "existing";
  let account = existing;
  if (!account) {
    account = await createUser(user);
    action = "created";
  }

  if (!account.emailVerified) {
    account = await markEmailVerified(account.localId);
    action = action === "created" ? "created_verified" : "verified_existing";
  }
  if (user.password && existing) {
    account = await setPassword(account.localId, user.password);
    action = action === "verified_existing" ? "verified_password_set" : "password_set";
  }

  let resetEmail = "skipped";
  if (options.sendResetEmail) {
    await sendPasswordReset(user.email);
    resetEmail = "sent";
  }

  return {
    email: user.email,
    action,
    localId: account.localId,
    emailVerified: account.emailVerified === true,
    resetEmail,
  };
}

async function lookupUser(email) {
  const response = await identityToolkitRequest(`projects/${encodeURIComponent(options.project)}/accounts:lookup`, {
    email: [email],
    targetProjectId: options.project,
  });
  return Array.isArray(response.users) && response.users.length > 0 ? response.users[0] : null;
}

async function createUser(user) {
  const response = await identityToolkitRequest(`projects/${encodeURIComponent(options.project)}/accounts`, {
    email: user.email,
    password: user.password || randomTemporaryPassword(),
    displayName: user.displayName,
    emailVerified: true,
    targetProjectId: options.project,
  });
  return {
    localId: response.localId,
    email: response.email || user.email,
    displayName: response.displayName || user.displayName,
    emailVerified: true,
  };
}

async function markEmailVerified(localId) {
  const response = await identityToolkitRequest(`projects/${encodeURIComponent(options.project)}/accounts:update`, {
    localId,
    emailVerified: true,
    targetProjectId: options.project,
  });
  return {
    localId: response.localId || localId,
    email: response.email,
    displayName: response.displayName,
    emailVerified: response.emailVerified === true,
  };
}

async function setPassword(localId, password) {
  const response = await identityToolkitRequest(`projects/${encodeURIComponent(options.project)}/accounts:update`, {
    localId,
    password,
    targetProjectId: options.project,
  });
  return {
    localId: response.localId || localId,
    email: response.email,
    displayName: response.displayName,
    emailVerified: response.emailVerified === true,
  };
}

async function sendPasswordReset(email) {
  const body = {
    requestType: "PASSWORD_RESET",
    email,
    targetProjectId: options.project,
  };
  if (options.continueUrl) {
    body.continueUrl = options.continueUrl;
  }
  await identityToolkitRequest(`projects/${encodeURIComponent(options.project)}/accounts:sendOobCode`, body);
}

async function identityToolkitRequest(path, body) {
  const response = await fetch(`${IDENTITY_TOOLKIT_BASE_URL}/${path}`, {
    method: "POST",
    headers: {
      "Authorization": `Bearer ${accessToken}`,
      "Content-Type": "application/json",
      "x-goog-user-project": options.project,
    },
    body: JSON.stringify(body),
  });
  const payload = await response.json().catch(() => ({}));
  if (!response.ok) {
    const message = payload?.error?.message || response.statusText || "unknown_error";
    throw new Error(`${path} failed: ${message}`);
  }
  return payload;
}

function randomTemporaryPassword() {
  return `${randomBytes(24).toString("base64url")}Aa1!`;
}

function gcloudValue(args) {
  try {
    return execFileSync("gcloud", args, {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "";
  }
}

function parseArgs(args) {
  const parsed = {
    allowProjectMismatch: false,
    continueUrl: "",
    dryRun: false,
    help: false,
    project: "",
    sendResetEmail: false,
    users: [],
  };

  for (let index = 0; index < args.length; index += 1) {
    const arg = args[index];
    if (arg === "--help" || arg === "-h") parsed.help = true;
    else if (arg === "--allow-project-mismatch") parsed.allowProjectMismatch = true;
    else if (arg === "--dry-run") parsed.dryRun = true;
    else if (arg === "--send-reset-email") parsed.sendResetEmail = true;
    else if (arg === "--project") parsed.project = nextValue(args, ++index, arg);
    else if (arg === "--continue-url") parsed.continueUrl = nextValue(args, ++index, arg);
    else if (arg === "--email") addEmailUsers(parsed.users, nextValue(args, ++index, arg));
    else if (arg === "--user") parsed.users.push(parseUser(nextValue(args, ++index, arg)));
    else if (arg === "--user-password") {
      const credential = parseUserPassword(nextValue(args, ++index, arg));
      const existing = parsed.users.find((user) => user.email === credential.email);
      if (existing) existing.password = credential.password;
      else {
        parsed.users.push({
          email: credential.email,
          displayName: displayNameFromEmail(credential.email),
          password: credential.password,
        });
      }
    }
    else fail(`Unknown argument: ${arg}`);
  }

  parsed.users = uniqueUsers(parsed.users);
  return parsed;
}

function nextValue(args, index, flag) {
  const value = args[index];
  if (!value || value.startsWith("--")) {
    fail(`${flag} requires a value.`);
  }
  return value;
}

function addEmailUsers(users, value) {
  for (const email of value.split(",")) {
    const normalized = normalizeEmail(email);
    if (normalized) users.push({ email: normalized, displayName: displayNameFromEmail(normalized) });
  }
}

function parseUser(value) {
  const [rawEmail, ...nameParts] = value.split("=");
  const email = normalizeEmail(rawEmail);
  if (!email) fail(`Invalid --user email: ${value}`);
  const displayName = nameParts.join("=").trim() || displayNameFromEmail(email);
  return { email, displayName };
}

function parseUserPassword(value) {
  const [rawEmail, ...passwordParts] = value.split("=");
  const email = normalizeEmail(rawEmail);
  const password = passwordParts.join("=");
  if (!email) fail(`Invalid --user-password email: ${value}`);
  if (password.length < 6) fail(`Password for ${email} is too short.`);
  return { email, password };
}

function uniqueUsers(users) {
  const seen = new Set();
  const unique = [];
  for (const user of users) {
    if (seen.has(user.email)) continue;
    seen.add(user.email);
    unique.push(user);
  }
  return unique;
}

function normalizeEmail(value) {
  return value.trim().toLowerCase();
}

function displayNameFromEmail(email) {
  return email
    .split("@")[0]
    .split(/[._-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function printUsage(exitCode) {
  console.log(`Usage:
  npm --prefix apps/admin-web run auth:seed-password-users -- \\
    --project goatos-stg \\
    --user ravi@mesha.sg=Ravi \\
    --send-reset-email

Options:
  --project <id>              Required Google Cloud/Firebase project id.
  --user <email=Display>      Seed one user with a display name. Repeatable.
  --user-password <email=pw>  Optional fixed temporary password for a seeded user.
  --email <a,b,c>             Seed emails with display names derived from local parts.
  --send-reset-email          Send Firebase password-reset emails after seeding.
  --continue-url <url>        Optional post-reset dashboard URL.
  --dry-run                   Show planned creates/updates/emails without writes.
  --allow-project-mismatch    Override active gcloud project guard.
`);
  process.exit(exitCode);
}

function fail(message) {
  console.error(message);
  process.exit(1);
}
