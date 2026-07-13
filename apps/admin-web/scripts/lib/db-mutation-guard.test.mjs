// DRV-R3a regression test for the dev:local wrapper's local-DB trust classification + guard.
// Run: node --test apps/admin-web/scripts/lib/db-mutation-guard.test.mjs (via make db-mutation-guard-test).
import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { assertMutableLocalDb, classifyLocalDatabaseUrl } from "./db-mutation-guard.mjs";

const FALLBACK = "postgres://postgres:goatos@127.0.0.1:5433/goatos?sslmode=disable";
const here = path.dirname(fileURLToPath(import.meta.url));

test("inherited DATABASE_URL is untrusted", () => {
  const c = classifyLocalDatabaseUrl({
    inheritedUrl: "postgres://u:p@10.0.0.5:5432/prod?sslmode=disable",
    dockerDetectedUrl: undefined,
    fallbackUrl: FALLBACK,
  });
  assert.equal(c.source, "inherited");
  assert.equal(c.trusted, false);
  assert.equal(c.url, "postgres://u:p@10.0.0.5:5432/prod?sslmode=disable");
});

test("detected docker container is trusted", () => {
  const detected = "postgres://postgres:goatos@127.0.0.1:55432/goatos?sslmode=disable";
  const c = classifyLocalDatabaseUrl({ inheritedUrl: undefined, dockerDetectedUrl: detected, fallbackUrl: FALLBACK });
  assert.equal(c.source, "docker");
  assert.equal(c.trusted, true);
  assert.equal(c.url, detected);
});

test("no-docker 127.0.0.1:5433 fallback is UNTRUSTED (DRV-R3a: direct dev:local)", () => {
  const c = classifyLocalDatabaseUrl({ inheritedUrl: undefined, dockerDetectedUrl: undefined, fallbackUrl: FALLBACK });
  assert.equal(c.source, "fallback");
  assert.equal(c.trusted, false);
  assert.equal(c.url, FALLBACK);
});

test("empty-string inherited/detected are treated as absent", () => {
  const c = classifyLocalDatabaseUrl({ inheritedUrl: "", dockerDetectedUrl: "", fallbackUrl: FALLBACK });
  assert.equal(c.source, "fallback");
  assert.equal(c.trusted, false);
});

test("assertMutableLocalDb allows a trusted DB", () => {
  assert.equal(assertMutableLocalDb(true, {}), true);
});

test("assertMutableLocalDb allows an untrusted DB with GOATOS_ALLOW_DB_MUTATION=1", () => {
  assert.equal(assertMutableLocalDb(false, { GOATOS_ALLOW_DB_MUTATION: "1" }), true);
});

test("assertMutableLocalDb refuses an untrusted DB without opt-in (exit 1)", () => {
  const script =
    `import { assertMutableLocalDb } from ${JSON.stringify(path.join(here, "db-mutation-guard.mjs"))};` +
    ` assertMutableLocalDb(false, {});`;
  let code = 0;
  try {
    execFileSync(process.execPath, ["--input-type=module", "-e", script], { stdio: "pipe" });
  } catch (e) {
    code = e.status;
  }
  assert.equal(code, 1);
});
