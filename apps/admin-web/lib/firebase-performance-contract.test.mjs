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

test("Firebase Performance Web stays explicitly opt-in and instruments client traces", () => {
  assert.match(source, /NEXT_PUBLIC_FIREBASE_PERFORMANCE_ENABLED/);
  assert.match(source, /enabled !== "1" && enabled !== "true"/);
  assert.match(source, /return null/);
  assert.match(source, /instrumentationEnabled:\s*true/);
  assert.match(source, /dataCollectionEnabled:\s*true/);
  assert.match(source, /traceRef\.putMetric\("duration_ms"/);
  assert.match(source, /traceRef\.putAttribute\(sanitizeAttributeName\(key\)/);
});
