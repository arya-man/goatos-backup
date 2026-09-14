import { readFileSync, readdirSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

// A copy key the SOP screens read but the backend contract does not carry throws inside `copy()`
// and blanks the page. Every literal key in features/sops must be declared in
// adminui/app/service.go (either `"key": "..."` in a map literal or `m["key"] = "..."`).
const dir = new URL("./", import.meta.url);
const backend = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");
const declared = new Set([...backend.matchAll(/"([a-z0-9_.]+)"(?::\s+"|\]\s*=\s*")/g)].map((m) => m[1]));
// Keys composed at runtime from a closed vocabulary (kind / capture), listed explicitly here.
for (const k of ["choice", "multi", "text", "number", "media", "vendor"]) declared.add(`inspection.kind.${k}`);
for (const k of ["photo", "video", "both"]) declared.add(`inspection.accepts.${k}`);

test("every copy key read by the SOP screens is declared in the backend copy map", () => {
  const missing = [];
  for (const f of readdirSync(dir).filter((n) => /\.(tsx|ts)$/.test(n))) {
    const src = readFileSync(new URL(f, dir), "utf8");
    for (const m of src.matchAll(/\bcopy\(\s*(?:pc|pageContract)\s*,\s*"([a-z0-9_.]+)"/g)) {
      if (!declared.has(m[1])) missing.push(`${f}: ${m[1]}`);
    }
  }
  assert.deepEqual([...new Set(missing)], []);
});
