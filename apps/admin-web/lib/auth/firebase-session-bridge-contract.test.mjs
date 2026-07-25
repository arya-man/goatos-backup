import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "../../components/auth/firebase-session-bridge.tsx"), "utf8");

assert.match(
  source,
  /onIdTokenChanged\(auth, \(user\) => \{[\s\S]*?syncFirebaseSession\(user,\s*true,\s*"auth\.sign_in"\)/,
  "initial Firebase auth-state callback must record auth.sign_in so pending CEO/CXO email grants are claimed",
);

assert.match(
  source,
  /setInterval\(\(\) => \{[\s\S]*?syncFirebaseSession\(auth\.currentUser,\s*true\)(?!\s*,)/,
  "periodic token refresh must stay a session refresh, not repeatedly claim sign-in grants",
);
