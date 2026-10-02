import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";

// Gemini must bill the prepaid AI Studio credits (Gemini Developer API + API key), never Vertex AI
// (postpay). This fails CI if any tracked CODE file calls the Vertex endpoint, turns the GenAI SDK's
// Vertex mode on, or brings back the old MESHA_VERTEX_* / roles/aiplatform.user wiring.
const CODE = ["*.go", "*.mjs", "*.js", "*.cjs", "*.ts", "*.tsx", "*.py", "*.sh", "*.tf", "*.yaml", "*.yml", "*.json", "Dockerfile*"];
const BANNED = [
  "aiplatform\\.googleapis\\.com/v1[^\"' ]*publishers/google",          // Vertex Gemini REST endpoint
  "-aiplatform\\.googleapis\\.com",                                      // regional Vertex host
  "vertexai:\\s*true",                                                   // @google/genai Vertex mode
  "GOOGLE_GENAI_USE_VERTEXAI\\s*[=:]\\s*['\"]?(true|1)",
  "BackendVertexAI",                                                     // Go genai SDK Vertex backend
  "MESHA_VERTEX_",
  "roles/aiplatform\\.user",
];
// Not callers: this guard itself, and Terraform's list of enabled APIs (enabling an API bills nothing).
const ALLOW = [":!tools/ask-mesha-agent/test/no-vertex-gemini.test.mjs", ":!docs/**", ":!**/package-lock.json", ":!docs/prototypes/**"];

test("no code calls Gemini through Vertex AI (postpay); Gemini Developer API only", () => {
  const root = execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
  let out = "";
  try {
    out = execFileSync("git", ["grep", "-nIE", BANNED.join("|"), "--", ...CODE, ...ALLOW], { cwd: root, encoding: "utf8" });
  } catch (e) {
    if (e.status !== 1) throw e;
  }
  assert.equal(out, "", `Vertex Gemini usage found (use generativelanguage.googleapis.com + an AI Studio key):\n${out}`);
});

test("the guard's patterns catch the real Vertex shapes", () => {
  const re = new RegExp(BANNED.join("|"));
  for (const bad of [
    'host = "aiplatform.googleapis.com"; url = "https://aiplatform.googleapis.com/v1/projects/p/locations/global/publishers/google/models/m:generateContent"',
    'fmt.Sprintf("%s-aiplatform.googleapis.com", loc)', "new GoogleGenAI({ vertexai: true, project })", 'os.Getenv("MESHA_VERTEX_MODEL")',
    "genai.NewClient(ctx, &genai.ClientConfig{Backend: genai.BackendVertexAI})", 'role = "roles/aiplatform.user"',
  ]) assert.ok(re.test(bad), bad);
  for (const ok of ["new GoogleGenAI({ vertexai: false, apiKey })", "https://generativelanguage.googleapis.com/v1beta/models/m:generateContent"]) assert.ok(!re.test(ok), ok);
});
