#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const scriptByHook = new Map([
  ["session-start", "session-start.mjs"],
  ["prompt-submit", "prompt-submit.mjs"],
  ["pre-tool-use", "pre-tool-use.mjs"],
  ["post-tool-use", "post-tool-use.mjs"],
  ["pre-compact", "pre-compact.mjs"],
  ["stop", "stop.mjs"],
]);

const hookName = process.argv[2] || "";
const scriptName = scriptByHook.get(hookName);
if (!scriptName) {
  process.exit(0);
}

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = dirname(dirname(__dirname));
const agentmemoryRoot =
  process.env.AGENTMEMORY_PLUGIN_ROOT ||
  "/opt/homebrew/lib/node_modules/@agentmemory/agentmemory/plugin/scripts";
const scriptPath = join(agentmemoryRoot, scriptName);
const projectName = process.env.GOATOS_AGENTMEMORY_PROJECT_NAME || "goatos";

if (!existsSync(scriptPath)) {
  process.exit(0);
}

let input = "";
for await (const chunk of process.stdin) {
  input += chunk;
}

let payload = null;
try {
  payload = input.trim() ? JSON.parse(input) : {};
} catch {
  payload = null;
}

let forwardedInput = input;
if (payload && typeof payload === "object") {
  // Codex project hooks can be launched from the Mesha workspace root while the
  // intended shared memory namespace is the Goat OS repo. Pin both namespace
  // and stored cwd so Claude Code and Codex sessions converge on one project.
  payload.cwd = repoRoot;
  payload.project = projectName;
  forwardedInput = `${JSON.stringify(payload)}\n`;
}

const result = spawnSync(process.execPath, [scriptPath], {
  input: forwardedInput,
  stdio: ["pipe", "inherit", "inherit"],
  env: {
    ...process.env,
    AGENTMEMORY_PROJECT_NAME: projectName,
    AGENTMEMORY_URL:
      process.env.AGENTMEMORY_URL || "http://localhost:3111",
    AGENTMEMORY_INJECT_CONTEXT:
      process.env.GOATOS_AGENTMEMORY_INJECT_CONTEXT || "false",
  },
});

process.exit(result.status ?? 0);
