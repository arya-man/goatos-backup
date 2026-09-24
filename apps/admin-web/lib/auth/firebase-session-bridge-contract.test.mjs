import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "../../components/auth/firebase-session-bridge.tsx"), "utf8");
const client = readFileSync(join(here, "firebase-client.ts"), "utf8");

// Incident goatos-stg 2026-09-24: the bridge re-posted auth.sign_in with a forced token refresh
// on every page load, so one user produced ~12 sign_in events in 6 minutes.
assert.doesNotMatch(
  source,
  /"auth\.sign_in"/,
  "the session bridge must never hard-code auth.sign_in; the deduper decides (once per browser session)",
);
assert.match(
  source,
  /onIdTokenChanged\(auth, \(user\) => \{[\s\S]*?syncBridgeSession\(user\)/,
  "onIdTokenChanged must go through the deduping bridge sync",
);
assert.match(
  client,
  /export async function syncBridgeSession\(user: User, forceRefresh = false\)[\s\S]*?sessionSyncDeduper\.bridge\(/,
  "bridge sync goes through the deduper and forces no refresh by default",
);
assert.match(
  client,
  /async function syncSignedInUser[\s\S]*?user\.getIdToken\(\)[\s\S]*?sessionSyncDeduper\.signIn\(/,
  "explicit login records auth.sign_in through the deduper without forcing a second token mint",
);
// The 50-minute timer forces a refresh, which also fires onIdTokenChanged for the SAME new
// token; both must go through the deduper so the interval posts once, not twice.
assert.match(
  source,
  /setInterval\(\(\) => \{[\s\S]*?syncBridgeSession\(auth\.currentUser,\s*true\)/,
  "periodic token refresh must go through the deduper",
);
assert.doesNotMatch(source, /syncFirebaseSession\(/, "the bridge must not bypass the deduper");
assert.match(
  client,
  /export async function syncBridgeSession\(user: User, forceRefresh = false\)[\s\S]*?user\.getIdToken\(forceRefresh\)/,
  "bridge sync forces a refresh only when the timer asks for one",
);
// A busy auth database (503 auth_database_busy) is transient: tell the user to retry and keep
// the Firebase sign-in so a retry does not need the credentials again.
assert.match(client, /case "auth_database_busy":\s*return "[^"]*busy[^"]*retry[^"]*"/i);
assert.match(
  client,
  /async function syncSignedInUser[\s\S]*?isFirebaseSessionError\(error, "auth_database_busy"\)[\s\S]*?signOut\(auth\)/,
  "an explicit login that hits a busy auth database must not sign the user out of Firebase",
);
const route = readFileSync(join(here, "../../app/api/auth/session/route.ts"), "utf8");
assert.match(route, /authEventFailure\(/, "the route maps backend failures through authEventFailure");
assert.match(route, /"Retry-After"/, "the route passes Retry-After through to the browser");

assert.match(
  client,
  /export async function clearFirebaseSession\(\)[\s\S]*?sessionSyncDeduper\.forget\(/,
  "sign-out must forget the uid's sign-in marker so the next login records auth.sign_in",
);
