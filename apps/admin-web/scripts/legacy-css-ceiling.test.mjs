// guard: legacy-css-ceiling (SYNC, merge of origin/main into the template branch; FIXJ6: the three legacy app stylesheets are deleted and must stay absent).
//
// Legacy CSS may only shrink. A merge from main is the easy way to grow it again: main still ships
// fixes as mesha-theme.css rules (a222fbdd4 added .toxin-proof, 3b259ea96 three phone rules,
// cbce4dbf7 a pinned proof photo). On this branch those fixes are carried in template MUI + sx and
// the rules are not taken. This test pins a RULE ceiling per stylesheet; lower a ceiling when
// you delete rules, never raise it. It also names the selectors main added so they cannot slip back.
import test from "node:test";
import assert from "node:assert/strict";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { cssRuleFindings, stylesheetFiles } from "./lib/shrink-ratchets.mjs";

// FIXJ-CI (J1 P2-2): the ceiling counts style RULES, not lines (deleting comments or blank lines used
// to satisfy it), and covers every stylesheet: app/*.css, features/**/*.css, components/**/*.css,
// layouts/*.css and *.module.css. The per-file ceilings are the design:guard `legacy-css-rules`
// ratchet allowances (scripts/check-design-system-waivers/design-system-waivers.json), which only go
// down: lower them in the change that deletes rules (npm run design:guard:update-baseline).
const appRoot = new URL("../", import.meta.url).pathname;
const ratchet = JSON.parse(readFileSync(new URL("./check-design-system-waivers/design-system-waivers.json", import.meta.url), "utf8")).ratchet ?? {};

test("every stylesheet stays at or under its rule ceiling; a new stylesheet has none", () => {
  for (const rel of stylesheetFiles(appRoot)) {
    if (rel === "theme/fonts.css" || rel === ".storybook/fonts.css") continue;
    const rules = cssRuleFindings(readFileSync(new URL(`../${rel}`, import.meta.url), "utf8")).length;
    const allowed = ratchet[`legacy-css-rules|${rel}`]?.allowed ?? 0;
    assert.ok(rules <= allowed, `${rel} has ${rules} style rules, ceiling ${allowed}: move the style into sx / a template component`);
  }
});

// FIXJ6: app/frame.css, app/minimal-theme.css, app/mesha-theme.css and layouts/mesha-layout.css are
// DELETED. The Mesha palette lives in theme/mesha-tokens.ts (emitted by theme/app-baseline.tsx). They
// must stay absent: a merge from main that brings one back (main still ships fixes as mesha-theme.css
// rules) is template-fied in the merge, never landed as a stylesheet, and no layout imports them.
const DELETED = ["app/frame.css", "app/minimal-theme.css", "app/mesha-theme.css", "layouts/mesha-layout.css"];
test("the deleted legacy stylesheets stay absent, have no ceiling and are imported nowhere", () => {
  for (const rel of DELETED) {
    assert.ok(!existsSync(new URL(`../${rel}`, import.meta.url)), `${rel} is back: carry its rules as theme sx / template parts instead`);
    assert.equal(ratchet[`legacy-css-rules|${rel}`], undefined, `${rel} still has a legacy-css-rules ceiling`);
  }
  // J1B P0-1: the import check used to read four TSX files, so `.storybook/preview.css` kept its
  // `@import "../app/mesha-theme.css"` (and two more) and `storybook build` failed unseen. Every
  // stylesheet and TS/TSX/MJS source in the app, `.storybook/` and `stories/` included, is read now.
  const offenders = importsOfDeleted(appRoot);
  assert.deepEqual(offenders, [], `the deleted legacy stylesheets are still imported: ${offenders.join(", ")}`);
});

const DELETED_NAMES = ["frame.css", "minimal-theme.css", "mesha-theme.css", "mesha-layout.css"];
/** `file: name` for every import / @import / require of a deleted stylesheet under root. */
export function importsOfDeleted(root) {
  const out = [];
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      if (["node_modules", ".next", "storybook-static", ".git", "public", ".codex-goatos-render", "visual-baselines"].includes(e.name)) continue;
      const abs = join(dir, e.name);
      if (e.isDirectory()) walk(abs);
      else if (/\.(css|tsx?|mjs|html)$/.test(e.name) && !/\.test\.mjs$/.test(e.name)) {
        const src = readFileSync(abs, "utf8");
        for (const name of DELETED_NAMES) {
          const re = new RegExp(`(?:@import\\s+(?:url\\()?|\\bimport\\s+(?:[^"'\\n]*\\s+from\\s+)?|require\\()["'][^"']*\\/${name.replace(".", "\\.")}["']`);
          if (re.test(src)) out.push(`${abs.slice(root.length).replace(/^\//, "")}: ${name}`);
        }
      }
    }
  };
  walk(root);
  return out;
}

test("importsOfDeleted self-test: finds a CSS @import and a TS import, ignores a comment-free mention", () => {
  const dir = mkdtempSync(join(tmpdir(), "legacy-css-import-"));
  try {
    mkdirSync(join(dir, ".storybook"));
    writeFileSync(join(dir, ".storybook", "preview.css"), '@import "../app/mesha-theme.css";\n');
    writeFileSync(join(dir, "a.tsx"), 'import "./frame.css";\nconst s = "minimal-theme.css is deleted";\n');
    assert.deepEqual(importsOfDeleted(dir).sort(), [".storybook/preview.css: mesha-theme.css", "a.tsx: frame.css"]);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("globals.css keeps an explicit rule ceiling", () => {
  assert.ok(ratchet["legacy-css-rules|app/globals.css"], "app/globals.css has no legacy-css-rules ceiling");
});

test("the Mesha palette tokens are theme values", () => {
  const tokens = readFileSync(new URL("../theme/mesha-tokens.ts", import.meta.url), "utf8");
  assert.match(tokens, /export const MESHA_TOKENS_DARK = \{/);
  assert.match(tokens, /export const MESHA_TOKENS_LIGHT = \{/);
  const baseline = readFileSync(new URL("../theme/app-baseline.tsx", import.meta.url), "utf8");
  assert.match(baseline, /':root': \{ \.\.\.MESHA_TOKENS_DARK/);
  assert.match(baseline, /':root\.light': \{ \.\.\.MESHA_TOKENS_LIGHT/);
});
