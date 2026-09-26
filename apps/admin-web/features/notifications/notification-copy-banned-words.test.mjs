// No technical / infrastructure vocabulary ever reaches a person's screen from the notification
// centre. Every visible string the centre can render on its own is a fallback in
// `notification-copy.ts` (the backend contract may override a key, but the fallbacks are what a
// fresh stack shows) or, for the push section, in `lib/push-copy.ts`. Both files are scanned as
// TEXT -- every double-quoted string literal that is a value in the fallback map -- because
// `notification-copy.ts` imports through the `@/` alias that `node --test` cannot resolve. When
// push cannot work on a stack the section is not rendered, so no explanation string exists.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (path) => readFileSync(new URL(path, import.meta.url), "utf8");

const BANNED = /\b(firebase|deployment|configured|unconfigured|api|backend|contract|token|vapid|http|[45]\d\d|endpoint|server|env|environment)\b/i;

/** The `"key": "value"` (or `"key":\n "value"`) pairs of a `Record<string, string>` literal. */
function fallbackValues(source, mapName) {
  const start = source.indexOf(`${mapName}: Record<string, string> = {`);
  assert.ok(start >= 0, `${mapName} not found`);
  const end = source.indexOf("\n};", start);
  const body = source.slice(start, end);
  return [...body.matchAll(/"([a-z_.]+)":\s*\n?\s*"((?:[^"\\]|\\.)*)"/g)].map((m) => [m[1], m[2]]);
}

for (const [label, path, mapName] of [
  ["notification centre", "./notification-copy.ts", "NOTIFICATION_COPY_FALLBACKS"],
  ["push section", "../../lib/push-copy.ts", "PUSH_COPY_FALLBACKS"],
]) {
  test(`${label} copy carries no technical vocabulary`, () => {
    const pairs = fallbackValues(read(path), mapName);
    assert.ok(pairs.length > 5, `${label}: expected a populated fallback map, got ${pairs.length}`);
    for (const [key, value] of pairs) {
      const hit = BANNED.exec(value);
      assert.equal(hit, null, `${key}: "${value}" contains banned word "${hit?.[0]}"`);
    }
  });
}
