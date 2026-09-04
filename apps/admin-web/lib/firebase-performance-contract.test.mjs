import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

const source = readFileSync(new URL("./firebase-performance.ts", import.meta.url), "utf8");

test("Firebase Performance Web initializes against the default Firebase app", () => {
  assert.match(source, /initializePerformance\(app,/);
  assert.match(source, /getApp\(\)/);
  assert.match(source, /initializeApp\(config\)/);
  assert.doesNotMatch(source, /getApp\("goatos-admin-web"\)/);
  assert.doesNotMatch(source, /initializeApp\(config,\s*"goatos-admin-web"\)/);
});
