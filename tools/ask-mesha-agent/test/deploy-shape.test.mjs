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

// Gemini-only: the service deploys and answers with zero Anthropic credentials.
test("deploy carries no Anthropic key/secret and checks the Vertex role", () => {
  const files = ["../deploy/deploy-stg.sh", "../Dockerfile", "../deploy/entrypoint.sh", "../../../cloudbuild.stg.yaml"];
  for (const f of files) {
    const src = fs.readFileSync(new URL(f, import.meta.url), "utf8");
    assert.doesNotMatch(src, /ask-mesha-anthropic-api-key|ANTHROPIC_API_KEY|CLAUDE_CODE_OAUTH_TOKEN|claude-agent-sdk|CLAUDE_AUTH/, f);
  }
  const sh = fs.readFileSync(new URL("../deploy/deploy-stg.sh", import.meta.url), "utf8");
  assert.match(sh, /roles\/aiplatform\.user/);
  assert.match(sh, /--set-secrets="ASK_MESHA_DATABASE_URL=\$\{SECRET_APP_DB\}:latest,ASK_MESHA_READONLY_DB_URL=\$\{SECRET_RO_DB\}:latest"/);
});
