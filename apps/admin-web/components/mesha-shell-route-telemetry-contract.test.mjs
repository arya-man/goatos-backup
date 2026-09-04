import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const here = dirname(fileURLToPath(import.meta.url));
const source = readFileSync(join(here, "mesha-shell.tsx"), "utf8");

assert.match(
  source,
  /const route = searchKey \? `\$\{pathname\}\?\$\{searchKey\}` : pathname;/,
  "route telemetry must preserve the '?' before query strings on commit/render",
);

assert.doesNotMatch(
  source,
  /const route = `\$\{pathname\}\$\{searchKey\}`;/,
  "route telemetry must not concatenate pathname and query string without a '?'",
);

assert.doesNotMatch(
  source,
  /window\.location\.assign\(nextUrl\.href\)/,
  "sidebar navigation must stay inside App Router; slow pages need loading boundaries instead of hard reloads",
);
