import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(new URL("./server.ts", import.meta.url), "utf8");

test("vaccination execution read uses an admin-side timeout", () => {
  assert.match(source, /withApiTimeout\(6000,\s*\(signal\)\s*=>\s*[\s\S]*"\/vaccination\/execution"/);
  assert.match(source, /signal,/);
});
