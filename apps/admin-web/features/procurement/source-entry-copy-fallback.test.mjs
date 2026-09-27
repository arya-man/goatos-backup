import { readFileSync } from "node:fs";
import { test } from "node:test";
import assert from "node:assert/strict";

// TR1-#4 (guard: source-entry-copy-fallback). /procurement/source-entry mounts the New load drawer
// form on page load, so a copy key that drawer reads but the serving API does not carry throws in
// `copy()` and takes the whole board to the error boundary. That happened with an API one release
// older than the web (location.select_optional_location, added in 605fcc0f8). The location
// selects' keys are newer than the oldest API still serving admin-web, so each must have a
// source-entry COPY_FALLBACKS entry, and every one of them must also be a key the backend serves
// for source-entry (a fallback is a bridge, not a second copy source).
const dir = new URL("./", import.meta.url);
const contract = readFileSync(new URL("../../lib/admin-ui-contract.ts", import.meta.url), "utf8");
const backend = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

function block(source, start, endPattern) {
  const at = source.indexOf(start);
  assert.ok(at >= 0, `${start} not found`);
  const rest = source.slice(at + start.length);
  const end = rest.search(endPattern);
  return rest.slice(0, end < 0 ? undefined : end);
}

const fallbacks = block(contract, `  "source-entry": {`, /\n {2}\},/);
const fallbackKeys = new Set([...fallbacks.matchAll(/"([a-z0-9_.]+)":\s*"/g)].map((m) => m[1]));
const served = block(backend, `case "source-entry":`, /\n\tcase "/);
const servedKeys = new Set([...served.matchAll(/"([a-z0-9_.]+)":\s+"/g)].map((m) => m[1]));

test("every copy key the New load location select reads has a source-entry fallback", () => {
  const src = block(readFileSync(new URL("location-selects.tsx", dir), "utf8"), "export function OptionalLocationSelect(", /\nexport function /);
  const keys = [...new Set([...src.matchAll(/copy\(pageContract, "([a-z0-9_.]+)"\)/g)].map((m) => m[1]))];
  assert.ok(keys.length > 0, "OptionalLocationSelect reads no copy keys: update this guard");
  assert.deepEqual(keys.filter((key) => !fallbackKeys.has(key)), []);
});

test("source-entry fallbacks mirror keys the backend serves for source-entry", () => {
  assert.deepEqual([...fallbackKeys].filter((key) => !servedKeys.has(key)), []);
});
