import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { contractUnavailableCopy } from "./admin-shell-unavailable.ts";

// The shell's emergency screen printed the raw code `backend_down` and "The backend-owned UI
// contract could not be loaded" when the server was unreachable (Sales E2E, 2026-09-26). It says
// what happened in farm words; the code goes to telemetry only.
const BANNED = /backend|frontend|\bapi\b|contract|admin-web|tenant|session|backend_down|_/i;

test("every failure kind has farm words and no internal code", () => {
  for (const kind of ["backend_down", "unauthorized", "permission_denied", "tenant_scope_mismatch", "missing_config", "api_error", "not_found", "bad_request", "something_new"]) {
    const copy = contractUnavailableCopy(kind);
    for (const text of [copy.title, copy.body, copy.retry]) {
      assert.ok(text && text.length > 3, `${kind}: empty copy`);
      assert.doesNotMatch(text, BANNED, `${kind}: "${text}"`);
    }
  }
  assert.notEqual(contractUnavailableCopy("backend_down").title, contractUnavailableCopy("unauthorized").title);
});

test("the shell renders the copy, never the error's code, kind or raw message", () => {
  const shell = readFileSync(new URL("./admin-shell.tsx", import.meta.url), "utf8");
  assert.match(shell, /contractUnavailableCopy\(/);
  assert.doesNotMatch(shell, /\{contract\.error\.(code|kind|message)/);
  assert.doesNotMatch(shell, /Admin-web contract unavailable|UI contract could not be loaded/);
});
