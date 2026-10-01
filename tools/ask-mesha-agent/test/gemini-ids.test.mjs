import test from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";

// Gemini 1.x/2.x are retired on Vertex for Mesha (2.5 traffic must move before 2026-10-20).
// No tracked file may name one again; use the 3.x ids (gemini.mjs defaults, infra env).
test("no Gemini 1.x/2.x model id anywhere in the repo", () => {
  const root = execFileSync("git", ["rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();
  let out = "";
  try {
    out = execFileSync("git", ["grep", "-nIE", "gemini-(1|2)\\.[0-9]", "--", ".", ":!*lock*", ":!**/gemini-ids.test.mjs"], { cwd: root, encoding: "utf8" });
  } catch (e) {
    if (e.status !== 1) throw e; // 1 = no match
  }
  assert.equal(out, "", `old Gemini ids found:\n${out}`);
});
