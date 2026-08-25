import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "../../proxy.ts"), "utf8");

assert.match(
  source,
  /FIREBASE_REFRESH_TOKEN_COOKIE/,
  "proxy must know about the durable Firebase refresh-token cookie",
);

assert.match(
  source,
  /const hasRefreshToken = Boolean\(request\.cookies\.get\(FIREBASE_REFRESH_TOKEN_COOKIE\)\?\.value\.trim\(\)\)/,
  "proxy must allow a request with only the refresh-token cookie to reach SSR refresh logic",
);

assert.match(
  source,
  /maxAgeForFirebaseIdToken[\s\S]*\|\|[\s\S]*hasRefreshToken[\s\S]*\|\|[\s\S]*hasLocalBearerFallback/,
  "refresh-token cookie must participate in the session gate before redirecting to /login",
);
