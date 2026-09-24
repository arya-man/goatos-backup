import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";

// Stop/delete, watches and monthly-cap reservations are in-process state; a second instance
// breaks them (see docs/agent-rules/ask-mesha.md "Single instance"). Keep the deploy pinned.
test("deploy pins Ask Mesha to a single Cloud Run instance", () => {
  const sh = fs.readFileSync(new URL("../deploy/deploy-stg.sh", import.meta.url), "utf8");
  assert.match(sh, /--max-instances=1\b/);
  assert.doesNotMatch(sh, /--max-instances=(?!1\b)\d+/);
});
