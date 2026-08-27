import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import test from "node:test";

const here = new URL(".", import.meta.url).pathname;
const source = readFileSync(join(here, "firebase-client.ts"), "utf8");

test("password reset continue URLs canonicalize away from the old staging dashboard host", () => {
  assert.match(source, /https:\/\/dashboard\.mesha\.sg/);
  assert.match(source, /browserOrigin === "https:\/\/stg\.dashboard\.mesha\.sg"/);
  assert.match(source, /new URL\(LOGIN_PATH,\s*origin\)/);
});
