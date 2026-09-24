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
  /export async function syncBridgeSession\(user: User\)[\s\S]*?user\.getIdToken\(\)[\s\S]*?sessionSyncDeduper\.bridge\(/,
  "bridge sync must not force a token refresh and must go through the deduper",
);
assert.match(
  client,
  /async function syncSignedInUser[\s\S]*?user\.getIdToken\(\)[\s\S]*?sessionSyncDeduper\.signIn\(/,
  "explicit login records auth.sign_in through the deduper without forcing a second token mint",
);
assert.match(
  source,
  /setInterval\(\(\) => \{[\s\S]*?syncFirebaseSession\(auth\.currentUser,\s*true\)(?!\s*,)/,
  "periodic token refresh must stay a session refresh, not repeatedly claim sign-in grants",
);
