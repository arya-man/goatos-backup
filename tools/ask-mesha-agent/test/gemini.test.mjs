// Gemini adapter, code tools and the shared instruction pack. Mocks live here only (tests);
// the product path has no mock fallback.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import { execFileSync } from "node:child_process";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import {
  runAgent, connectMcp, mcpToolToDeclaration, historyContents, callCostUsd, isRetryable, geminiConfig, DEFAULT_MODEL, DEFAULT_FAST_MODEL, EMPTY_TURN_NUDGE,
  MAX_STEPS_NOTE, MAX_PARALLEL_TOOLS, mapLimit,
} from "../gemini.mjs";
import { createCodeTools, isSecretPath, CODE_TOOL_DECLARATIONS } from "../code-tools.mjs";
import { appendPrompt, instructionPack, geminiInstructionPack, GEMINI_TOOL_NOTE, tableIndexSection, ceoRepoInstructions, ceoRules, CODING_AGENT_LINE } from "../instructions.mjs";

const sha = (s) => crypto.createHash("sha256").update(s).digest("hex");

// ---- shared instruction pack ------------------------------------------------------------
test("instructions: CEO rules are byte-identical to the pre-split server.mjs APPEND_PROMPT", () => {
  // Hashes of APPEND_PROMPT taken from server.mjs at origin/main 9043002022 before the split.
  assert.equal(sha(appendPrompt({ readonly: true })), "396731c39d5e18ead0af8144585d3490afecb9deeb3458692195681899dc8342");
  assert.equal(sha(appendPrompt({ readonly: false })), "9feb6a3c46d3e195cfed5943654c2a962fd3308d91615c28a5b9ac1ce7a0bb56");
});

test("instructions: pack = CLAUDE.md(+@imports) + rules + data map + table index, Gemini adds only the tool note", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pack-"));
  fs.writeFileSync(path.join(dir, "CLAUDE.md"), "@AGENTS.md\n\n## Local\n");
  fs.writeFileSync(path.join(dir, "AGENTS.md"), "# Agents rules\n");
  fs.mkdirSync(path.join(dir, "tools/ask-mesha-agent"), { recursive: true });
  fs.writeFileSync(path.join(dir, "tools/ask-mesha-agent/data-map-core.md"), "MAP");
  const table = tableIndexSection("public.goats ~100");
  const pack = instructionPack({ cwd: dir, tableSection: table });
  assert.equal(pack,
    "\n\n# Repository instructions (CLAUDE.md)\n# Agents rules\n\n## Local\n" + appendPrompt({ readonly: true }) +
    "\n\n# Mesha data map (cheat-sheet)\nMAP" + table);
  assert.equal(geminiInstructionPack({ cwd: dir, tableSection: table }),
    GEMINI_TOOL_NOTE + ceoRepoInstructions(dir) + ceoRules() + "\n\n# Mesha data map (cheat-sheet)\nMAP" + table);
  assert.ok(appendPrompt().includes(CODING_AGENT_LINE));
  assert.ok(!ceoRules().includes("Never merge to main"));
  assert.equal(tableIndexSection(""), "");
});

// ---- model ids -------------------------------------------------------------------------
test("config: newest Gemini 3.x defaults, all env-overridable, nothing older", () => {
  const c = geminiConfig({});
  assert.equal(c.model, DEFAULT_MODEL);
  assert.equal(c.fastModel, DEFAULT_FAST_MODEL);
  assert.equal(c.location, "global");
  for (const id of [DEFAULT_MODEL, DEFAULT_FAST_MODEL]) assert.match(id, /^gemini-3\./);
  const o = geminiConfig({ ASK_MESHA_MODEL: "m1", ASK_MESHA_FAST_MODEL: "f1", ASK_MESHA_GEMINI_LOCATION: "us-central1", ASK_MESHA_GEMINI_PROJECT: "p" });
  assert.deepEqual([o.model, o.deepModel, o.fastModel, o.checkModel, o.location, o.project], ["m1", "m1", "f1", "f1", "us-central1", "p"]);
});

test("cost: thinking tokens bill as output, cached input at 10%", () => {
  const u = { promptTokenCount: 100_000, cachedContentTokenCount: 0, candidatesTokenCount: 0, thoughtsTokenCount: 0 };
  assert.equal(callCostUsd("gemini-3.1-pro-preview", u, {}).toFixed(2), "0.20");
  assert.equal(callCostUsd("gemini-3.1-pro-preview", { ...u, promptTokenCount: 1_000_000 }, {}), 4, ">200k prompt uses the long-context rate");
  assert.equal(callCostUsd("gemini-3.1-pro-preview", { ...u, cachedContentTokenCount: 100_000 }, {}).toFixed(2), "0.02"); // 0.2 * 10%
  assert.equal(callCostUsd("gemini-3.8-flash", { promptTokenCount: 0, candidatesTokenCount: 500_000, thoughtsTokenCount: 500_000 }, {}), 3);
  assert.equal(isRetryable({ status: 429 }), true);
  assert.equal(isRetryable({ status: 400 }), false);
});

test("historyContents: alternating user/model turns, starts with user, ends with model", () => {
  const h = [
    { role: "assistant", content: "hi" },
    { role: "user", content: "q1" }, { role: "user", content: "q1b" },
    { role: "assistant", content: "a1", chart: null },
    { role: "user", content: "q2" },
  ];
  const c = historyContents(h);
  assert.deepEqual(c.map((x) => x.role), ["user", "model", "user", "model", "user", "model"]);
  assert.equal(c[2].parts[0].text, "q1\n\nq1b");
  assert.deepEqual(historyContents([]), []);
});

// ---- agent loop (fake model) --------------------------------------------------------------
function fakeAi(turns) {
  const calls = [];
  return {
    calls,
    models: {
      async generateContentStream(req) {
        calls.push(JSON.parse(JSON.stringify({ ...req, config: { ...req.config, abortSignal: undefined } })));
        const t = turns.shift();
        if (t instanceof Error) throw t;
        return (async function* () { for (const ch of t) yield ch; })();
      },
    },
  };
}
const chunk = (parts, usage) => ({ candidates: [{ content: { role: "model", parts } }], ...(usage ? { usageMetadata: usage } : {}) });

test("runAgent: parallel tool calls, results fed back, thought signatures echoed, events in order", async () => {
  const ai = fakeAi([
    [chunk([{ text: "thinking", thought: true }, { functionCall: { id: "c1", name: "run_sql", args: { sql: "select 1" } }, thoughtSignature: "sig" }, { functionCall: { name: "read_file", args: { path: "x" } } }], { promptTokenCount: 100, candidatesTokenCount: 10 })],
    [chunk([{ text: "There are " }]), chunk([{ text: "42 goats." }], { promptTokenCount: 200, candidatesTokenCount: 5 })],
  ]);
  const events = [];
  const tools = {
    declarations: [{ name: "run_sql" }],
    call: async (name, args) => (name === "run_sql" ? { text: "n\n42" } : { text: "no such file", isError: true }),
  };
  const r = await runAgent({ ai, model: "gemini-3.1-pro-preview", systemInstruction: "SYS", contents: [{ role: "user", parts: [{ text: "q" }] }], tools, onEvent: (e) => events.push(e), env: {} });
  assert.equal(r.text, "There are 42 goats.");
  assert.equal(r.steps, 2);
  assert.equal(r.error, null);
  assert.deepEqual(r.usage, { input: 300, output: 15, cached: 0, thoughts: 0 });
  assert.deepEqual(events.filter((e) => e.type !== "usage").map((e) => e.type), ["step", "turn_end", "tool_call", "tool_call", "tool_result", "tool_result", "step", "text", "text", "turn_end"]);
  assert.equal(events.find((e) => e.type === "text").text, "There are ");
  // Second request carries the model turn verbatim (signature kept) and both function responses.
  const second = ai.calls[1];
  assert.equal(second.config.systemInstruction, "SYS");
  assert.equal(second.contents[1].role, "model");
  assert.equal(second.contents[1].parts[1].thoughtSignature, "sig");
  const fr = second.contents[2].parts.map((p) => p.functionResponse);
  assert.deepEqual(fr[0], { id: "c1", name: "run_sql", response: { output: "n\n42" } });
  assert.deepEqual(fr[1], { name: "read_file", response: { error: "no such file" } }, "errors go back to the model as data");
  assert.ok(ai.calls[0].config.tools[0].functionDeclarations.length === 1);
});

test("runAgent: max steps -> one tool-less final turn and error_max_turns", async () => {
  const call = [chunk([{ functionCall: { name: "run_sql", args: {} } }])];
  const ai = fakeAi([call, call, [chunk([{ text: "Partial answer." }])]]);
  const r = await runAgent({ ai, model: "m", contents: [], tools: { declarations: [], call: async () => ({ text: "ok" }) }, maxSteps: 2, env: {} });
  assert.equal(r.error, "error_max_turns");
  assert.equal(r.text, "Partial answer.");
  assert.equal(ai.calls[2].config.tools, undefined, "final turn has no tools");
  // The note rides on the tool-results turn: roles still alternate user/model.
  const last = ai.calls[2].contents;
  assert.deepEqual(last.map((c) => c.role), ["model", "user", "model", "user"]);
  assert.equal(last.at(-1).parts.at(-1).text, MAX_STEPS_NOTE);
  assert.ok(last.at(-1).parts[0].functionResponse);
});

test("runAgent: budget cap stops before running more tools", async () => {
  const ai = fakeAi([[chunk([{ functionCall: { name: "run_sql", args: {} } }], { promptTokenCount: 1_000_000 })]]);
  let ran = 0;
  const r = await runAgent({ ai, model: "gemini-3.1-pro-preview", contents: [], tools: { declarations: [], call: async () => { ran++; return { text: "" }; } }, budgetUsd: 1, env: {} });
  assert.equal(r.error, "error_max_budget_usd");
  assert.equal(ran, 0);
});

test("runAgent: an empty final turn is nudged once for the answer", async () => {
  const ai = fakeAi([[chunk([{ text: "", thought: true }])], [chunk([{ text: "Answer." }])]]);
  const r = await runAgent({ ai, model: "m", contents: [], tools: { declarations: [], call: async () => ({ text: "" }) }, env: {} });
  assert.equal(r.text, "Answer.");
  assert.equal(ai.calls[1].contents.at(-1).parts[0].text, EMPTY_TURN_NUDGE);
});

test("runAgent: 429 before any text is retried; abort throws client_aborted", async () => {
  const e = Object.assign(new Error("RESOURCE_EXHAUSTED"), { status: 429 });
  const ai = fakeAi([e, [chunk([{ text: "ok" }])]]);
  const r = await runAgent({ ai, model: "m", contents: [], tools: { declarations: [], call: async () => ({}) }, retryDelayMs: 1, env: {} });
  assert.equal(r.text, "ok");
  const ac = new AbortController(); ac.abort();
  await assert.rejects(runAgent({ ai: fakeAi([]), model: "m", contents: [], tools: { declarations: [], call: async () => ({}) }, signal: ac.signal }), /client_aborted/);
});

test("runAgent: an image from a tool goes back as an inline part on the function response", async () => {
  const ai = fakeAi([[chunk([{ functionCall: { name: "read_file", args: { path: "a.png" } } }])], [chunk([{ text: "I see it." }])]]);
  await runAgent({ ai, model: "m", contents: [], tools: { declarations: [], call: async () => ({ text: "Attached", inline: { mimeType: "image/png", data: "AAA" } }) }, env: {} });
  assert.deepEqual(ai.calls[1].contents.at(-1).parts[0].functionResponse.parts, [{ inlineData: { mimeType: "image/png", data: "AAA" } }]);
});

// ---- MCP bridge: the same mesha MCP server, reached through an MCP client ---------------
test("connectMcp: lists MCP tools as Gemini declarations and calls them", async () => {
  const s = new McpServer({ name: "mesha", version: "1" });
  s.registerTool("run_sql", { description: "Run SQL", inputSchema: { sql: z.string() }, annotations: { readOnlyHint: true } },
    async ({ sql }) => ({ content: [{ type: "text", text: `rows for ${sql}` }] }));
  const m = await connectMcp(s);
  assert.deepEqual(m.names, ["run_sql"]);
  assert.equal(m.declarations[0].parametersJsonSchema.$schema, undefined);
  assert.deepEqual(m.declarations[0].parametersJsonSchema.required, ["sql"]);
  assert.deepEqual(await m.call("run_sql", { sql: "select 1" }), { text: "rows for select 1", isError: false });
  assert.equal((await m.call("run_sql", {})).isError, true, "schema validation errors come back as tool errors");
  await m.close();
  assert.deepEqual(mcpToolToDeclaration({ name: "x" }), { name: "x", description: "", parametersJsonSchema: { type: "object", properties: {} } });
});

test("server wires every mesha MCP tool + code tools into Gemini, read-only, no Claude SDK", () => {
  const src = fs.readFileSync(new URL("../server.mjs", import.meta.url), "utf8");
  assert.doesNotMatch(src, /@anthropic-ai|ANTHROPIC_API_KEY|claude-agent-sdk/);
  for (const t of ["run_sql", "run_reference", "describe_table"]) assert.match(src, new RegExp(`tool\\(\\s*"${t}"`));
  assert.match(src, /tool\("watch_tags"/);
  assert.match(src, /declarations: \[\.\.\.mcp\.declarations, \.\.\.CODE_TOOL_DECLARATIONS\]/);
  assert.match(src, /const READONLY = true;/);
  const pkg = JSON.parse(fs.readFileSync(new URL("../package.json", import.meta.url), "utf8"));
  assert.equal(pkg.dependencies["@anthropic-ai/claude-agent-sdk"], undefined);
  assert.deepEqual(CODE_TOOL_DECLARATIONS.map((d) => d.name), ["read_file", "grep", "glob", "list_dir", "get_skill"]);
});

// ---- code tools: same reach as Read/Grep/Glob/Skill, sandboxed -----------------------------
function fixtureRepo() {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "repo-")));
  fs.mkdirSync(path.join(root, "backend/internal/weighing"), { recursive: true });
  fs.writeFileSync(path.join(root, "backend/internal/weighing/adg.go"), "package weighing\n// ADG = gain / days\nfunc ADG() {}\n");
  fs.writeFileSync(path.join(root, ".env"), "SECRET=1\n");
  fs.writeFileSync(path.join(root, "key.pem"), "x");
  fs.mkdirSync(path.join(root, ".agents/skills/mesha-data-map"), { recursive: true });
  fs.writeFileSync(path.join(root, ".agents/skills/mesha-data-map/SKILL.md"), "# data map skill\n");
  fs.writeFileSync(path.join(root, "shot.png"), Buffer.from([0x89, 0x50, 0x4e, 0x47]));
  const outside = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "outside-")));
  fs.writeFileSync(path.join(outside, "secret.txt"), "nope");
  fs.symlinkSync(path.join(outside, "secret.txt"), path.join(root, "link.txt"));
  const uploads = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), "uploads-")));
  fs.writeFileSync(path.join(uploads, "sales.csv"), "park,sold\nCBE,3\n");
  return { root, outside, uploads };
}

test("code tools: read_file with line ranges, images inline, attachments readable", async () => {
  const { root, uploads } = fixtureRepo();
  const t = createCodeTools({ repo: root, roots: [uploads] });
  const r = await t.read_file({ path: "backend/internal/weighing/adg.go", offset: 2, limit: 1 });
  assert.equal(r.isError, undefined);
  assert.match(r.text, /^\s+2\t\/\/ ADG = gain \/ days\n… \(4 lines total; pass offset=3 to continue\)$/);
  const img = await t.read_file({ path: "shot.png" });
  assert.equal(img.inline.mimeType, "image/png");
  assert.match((await t.read_file({ path: path.join(uploads, "sales.csv") })).text, /CBE,3/);
});

test("code tools: sandbox refuses outside paths, symlink escapes, .git and secrets", async () => {
  const { root, outside } = fixtureRepo();
  const t = createCodeTools({ repo: root });
  for (const p of [path.join(outside, "secret.txt"), "../" + path.basename(outside) + "/secret.txt", "link.txt", ".env", "key.pem", "/etc/passwd"]) {
    const r = await t.read_file({ path: p });
    assert.equal(r.isError, true, p);
  }
  assert.equal((await t.grep({ pattern: "x", path: "/etc" })).isError, true);
  assert.equal((await t.glob({ pattern: "*", path: outside })).isError, true);
  assert.equal(isSecretPath("/a/.env.local"), true);
  assert.equal(isSecretPath("/a/service-account.json"), true);
  assert.equal(isSecretPath("/a/adg.go"), false);
});

test("code tools: grep (ripgrep) and glob find code; secrets never listed", { skip: !hasRg() && "ripgrep not installed" }, async () => {
  const { root } = fixtureRepo();
  const t = createCodeTools({ repo: root });
  assert.equal((await t.grep({ pattern: "gain / days" })).text, "backend/internal/weighing/adg.go");
  assert.match((await t.grep({ pattern: "func ADG", output_mode: "content" })).text, /adg\.go:3:func ADG/);
  assert.equal((await t.grep({ pattern: "SECRET" })).text, "No matches.");
  assert.equal((await t.glob({ pattern: "**/*.go" })).text, "backend/internal/weighing/adg.go");
  assert.doesNotMatch((await t.glob({ pattern: "*" })).text, /\.env|key\.pem/);
  assert.match((await t.list_dir({ path: "." })).text, /backend\//);
});

test("code tools: get_skill lists and loads repo skills", async () => {
  const { root } = fixtureRepo();
  const t = createCodeTools({ repo: root });
  assert.equal((await t.get_skill({ name: "list" })).text, "Skills: mesha-data-map");
  assert.match((await t.get_skill({ name: "mesha-data-map" })).text, /# data map skill/);
  assert.equal((await t.get_skill({ name: "../../etc" })).isError, true);
});

function hasRg() {
  try { return Boolean(execFileSync("rg", ["--version"])); } catch { return false; }
}

test("runAgent: at most MAX_PARALLEL_TOOLS tool calls run at once, results stay in order", async () => {
  const many = Array.from({ length: 9 }, (_, i) => ({ functionCall: { name: "run_sql", args: { i } } }));
  const ai = fakeAi([[chunk(many)], [chunk([{ text: "done" }])]]);
  let live = 0, peak = 0;
  await runAgent({ ai, model: "m", contents: [], env: {}, tools: { declarations: [], call: async (_n, a) => {
    live++; peak = Math.max(peak, live); await new Promise((r) => setTimeout(r, 5)); live--; return { text: `r${a.i}` };
  } } });
  assert.equal(peak, MAX_PARALLEL_TOOLS);
  assert.deepEqual(ai.calls[1].contents.at(-1).parts.map((p) => p.functionResponse.response.output), many.map((_, i) => `r${i}`));
  assert.deepEqual(await mapLimit([1, 2, 3], 2, async (x) => x * 2), [2, 4, 6]);
});

test("runAgent: image budget per answer counts attachments, then drops further images", async () => {
  const contents = [{ role: "user", parts: [{ text: "q" }, { inlineData: { mimeType: "image/png", data: "A".repeat(8) } }] }];
  const ai = fakeAi([[chunk([{ functionCall: { name: "read_file", args: {} } }, { functionCall: { name: "read_file", args: {} } }])], [chunk([{ text: "ok" }])]]);
  await runAgent({ ai, model: "m", contents, env: {}, inlineBudget: 12, tools: { declarations: [], call: async () => ({ text: "img", inline: { mimeType: "image/png", data: "B".repeat(4) } }) } });
  const parts = ai.calls[1].contents.at(-1).parts.map((p) => p.functionResponse);
  assert.ok(parts[0].parts, "first image fits (8 + 4 = 12)");
  assert.equal(parts[1].parts, undefined, "second image is over budget");
  assert.match(parts[1].response.output, /Not shown/);
});

test("runAgent: persistent 429 moves the rest of the answer to the fallback model", async () => {
  const e = () => Object.assign(new Error("RESOURCE_EXHAUSTED"), { status: 429 });
  const ai = fakeAi([e(), e(), e(), e(), e(), e(), e(), [chunk([{ text: "flash answer" }])]]);
  const events = [];
  const r = await runAgent({ ai, model: "gemini-3.1-pro-preview", fallbackModel: "gemini-3.8-flash", contents: [], retryDelayMs: 1, env: {}, onEvent: (x) => events.push(x), tools: { declarations: [], call: async () => ({}) } });
  assert.equal(r.text, "flash answer");
  assert.equal(r.model, "gemini-3.8-flash");
  assert.deepEqual(ai.calls.map((c) => c.model), [...Array(7).fill("gemini-3.1-pro-preview"), "gemini-3.8-flash"], "1 try + 6 backoff waits, then flash");
  assert.ok(events.some((x) => x.type === "model_fallback"));
  // Without a fallback the error surfaces.
  await assert.rejects(runAgent({ ai: fakeAi(Array.from({ length: 7 }, e)), model: "m", contents: [], retryDelayMs: 1, env: {}, tools: { declarations: [], call: async () => ({}) } }), /RESOURCE_EXHAUSTED/);
});

test("connectMcp: watch_tags gets the long timeout, other tools the short one; abort cancels", async () => {
  const s = new McpServer({ name: "mesha", version: "1" });
  s.registerTool("slow", { description: "d", inputSchema: {} }, async () => { await new Promise((r) => setTimeout(r, 200)); return { content: [{ type: "text", text: "late" }] }; });
  s.registerTool("watch_tags", { description: "d", inputSchema: {} }, async () => { await new Promise((r) => setTimeout(r, 60)); return { content: [{ type: "text", text: "watched" }] }; });
  const m = await connectMcp(s, { timeoutMs: 30, watchTimeoutMs: 1000 });
  await assert.rejects(m.call("slow", {}), /timed out|Timeout/i);
  assert.equal((await m.call("watch_tags", {})).text, "watched");
  await m.close();
  const ac = new AbortController();
  const s2 = new McpServer({ name: "mesha", version: "1" });
  s2.registerTool("slow", { description: "d", inputSchema: {} }, async () => { await new Promise((r) => setTimeout(r, 200)); return { content: [] }; });
  const m2 = await connectMcp(s2, { signal: ac.signal });
  const p = m2.call("slow", {});
  ac.abort();
  await assert.rejects(p);
  await m2.close();
});

test("code tools: a symlink to a secret inside the repo is refused (realpath checked)", async () => {
  const { root } = fixtureRepo();
  fs.symlinkSync(path.join(root, ".env"), path.join(root, "notes.txt"));
  fs.mkdirSync(path.join(root, "docs"));
  fs.symlinkSync(path.join(root, ".agents"), path.join(root, "docs/alias"));
  const t = createCodeTools({ repo: root });
  assert.equal((await t.read_file({ path: "notes.txt" })).isError, true);
  assert.match((await t.read_file({ path: "docs/alias/skills/mesha-data-map/SKILL.md" })).text, /data map skill/, "harmless in-repo symlinks still work");
});

test("code tools: grep/glob never search or list credential-shaped files", { skip: !hasRg() && "ripgrep not installed" }, async () => {
  const { root } = fixtureRepo();
  for (const f of ["gcp-credentials.json", "my-service-account.json", "id_rsa", "id_rsa.pub", "cert.p12", ".env.local", "config/application_default_credentials.json"]) {
    fs.mkdirSync(path.dirname(path.join(root, f)), { recursive: true });
    fs.writeFileSync(path.join(root, f), "TOPSECRET\n");
  }
  const t = createCodeTools({ repo: root });
  assert.equal((await t.grep({ pattern: "TOPSECRET" })).text, "No matches.");
  assert.equal((await t.grep({ pattern: "TOPSECRET", output_mode: "content" })).text, "No matches.");
  assert.doesNotMatch((await t.glob({ pattern: "**/*" })).text, /credentials|service-account|id_rsa|\.p12|\.env/);
  assert.match((await t.glob({ pattern: "**/*" })).text, /adg\.go/);
});

test("runAgent: a turn that fails after streaming text is retried and its text withdrawn", async () => {
  const boom = Object.assign(new Error("UNAVAILABLE"), { status: 503 });
  const turns = [[chunk([{ text: "Half an ans" }])], [chunk([{ text: "Full answer." }])]];
  let n = 0;
  const ai = { models: { async generateContentStream() {
    const t = turns[n++];
    return (async function* () { yield t[0]; if (n === 1) throw boom; })();
  } } };
  const events = [];
  const r = await runAgent({ ai, model: "m", contents: [], retryDelayMs: 1, env: {}, onEvent: (x) => events.push(x), tools: { declarations: [], call: async () => ({}) } });
  assert.equal(r.text, "Full answer.");
  assert.deepEqual(events.map((x) => x.type), ["step", "text", "turn_retry", "text", "turn_end"]);
});

test("runAgent: 401 refreshes the dev token once and retries", async () => {
  let invalidated = 0;
  const e401 = Object.assign(new Error("Request had invalid authentication credentials"), { status: 401 });
  const ai = fakeAi([e401, [chunk([{ text: "ok" }])]]);
  ai.invalidate = () => invalidated++;
  const r = await runAgent({ ai, model: "m", contents: [], env: {}, tools: { declarations: [], call: async () => ({}) } });
  assert.equal(r.text, "ok");
  assert.equal(invalidated, 1);
  const ai2 = fakeAi([e401, e401]); ai2.invalidate = () => {};
  await assert.rejects(runAgent({ ai: ai2, model: "m", contents: [], env: {}, tools: { declarations: [], call: async () => ({}) } }), /authentication/);
});

test("retryDelay: jittered exponential, ~63 s over six waits, honours Retry-After", async () => {
  const { retryDelay, RETRY_WAITS } = await import("../gemini.mjs");
  const mid = Array.from({ length: RETRY_WAITS }, (_, a) => retryDelay(a, 1000, null, () => 0.5));
  assert.deepEqual(mid, [1000, 2000, 4000, 8000, 16000, 32000]);
  assert.equal(retryDelay(0, 1000, null, () => 0), 750);
  assert.equal(retryDelay(3, 1000, { headers: { "retry-after": "7" } }), 7000);
  assert.equal(retryDelay(3, 1000, { headers: { "retry-after": "999" } }), 60000);
});

test("runAgent: spend is reported after every model call (failed runs are charged what they used)", async () => {
  const ai = fakeAi([[chunk([{ functionCall: { name: "x", args: {} } }], { promptTokenCount: 100_000 })], Object.assign(new Error("bad request"), { status: 400 })]);
  const usage = [];
  await assert.rejects(runAgent({ ai, model: "gemini-3.1-pro-preview", contents: [], env: {}, onEvent: (x) => x.type === "usage" && usage.push(x.costUsd), tools: { declarations: [], call: async () => ({ text: "" }) } }));
  assert.equal(usage.length, 1);
  assert.equal(usage[0].toFixed(2), "0.20");
});

test("instructions: Gemini gets only the CEO-relevant AGENTS.md sections", () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "agents-"));
  fs.writeFileSync(path.join(dir, "AGENTS.md"), [
    "# Top", "", "## Domain Rule Index", "", "- Weighing -> docs/agent-rules/weighing.md", "",
    "## Main Merge Requires Exact-SHA CI Evidence", "", "make land-main only (Claude AND Codex)", "",
    "## Every Visible Date Is DD/MM/YYYY (maintainer lock)", "", "p1 dates", "", "p2 more", "", "p3 dev detail", "",
    "## Mesha / Goat OS RFID Language", "", "animal_identifier_1", "",
  ].join("\n"));
  const t = ceoRepoInstructions(dir);
  assert.match(t, /weighing\.md/);
  assert.match(t, /p1 dates[\s\S]*p2 more/);
  assert.doesNotMatch(t, /p3 dev detail|land-main|Claude AND Codex/);
  assert.match(t, /animal_identifier_1/);
  // The real repo: the Gemini pack is much smaller than the full CLAUDE.md pack and has no dev process rules.
  const repo = path.resolve(new URL("../../..", import.meta.url).pathname);
  const g = geminiInstructionPack({ cwd: repo });
  assert.ok(g.length < instructionPack({ cwd: repo }).length * 0.7);
  assert.doesNotMatch(ceoRepoInstructions(repo), /make land-main|Gradle|Claude AND Codex/);
  assert.match(GEMINI_TOOL_NOTE, /CPT = Channapatna/);
  assert.match(GEMINI_TOOL_NOTE, /Google Gemini/);
});

test("runAgent: time guard forces a tool-less answer turn", async () => {
  let t = 0;
  const call = [chunk([{ functionCall: { name: "run_sql", args: {} } }])];
  const ai = fakeAi([call, call, [chunk([{ text: "Answer from what I have." }])]]);
  const r = await runAgent({ ai, model: "m", contents: [], env: {}, deadlineMs: 50, now: () => (t += 30),
    tools: { declarations: [], call: async () => ({ text: "rows" }) } });
  assert.equal(r.text, "Answer from what I have.");
  assert.equal(r.error, null, "a time-guarded answer is a normal answer, not a cut-off");
  assert.equal(ai.calls.at(-1).config.tools, undefined);
  assert.match(ai.calls.at(-1).contents.at(-1).parts.at(-1).text, /Time is up/);
});

test("runAgent: a dropped DB connection is retried once, other tool errors are not", async () => {
  const ai = fakeAi([[chunk([{ functionCall: { name: "run_sql", args: { q: 1 } } }, { functionCall: { name: "run_sql", args: { q: 2 } } }])], [chunk([{ text: "ok" }])]]);
  const seen = { 1: 0, 2: 0 };
  await runAgent({ ai, model: "m", contents: [], env: {}, toolRetryDelayMs: 1, tools: { declarations: [], call: async (_n, a) => {
    seen[a.q]++;
    if (a.q === 1 && seen[1] === 1) return { text: 'psql: error: connection to server at "127.0.0.1", port 55432 failed: server closed the connection unexpectedly', isError: true };
    if (a.q === 2) return { text: "column x does not exist", isError: true };
    return { text: "rows" };
  } } });
  assert.deepEqual(seen, { 1: 2, 2: 1 });
  assert.equal(ai.calls[1].contents.at(-1).parts[0].functionResponse.response.output, "rows");
});
