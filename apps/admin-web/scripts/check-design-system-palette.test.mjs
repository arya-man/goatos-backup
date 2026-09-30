// Palette policy for design:guard: Mesha brand lock, template neutrals (Ravi 2026-09-27), retired
// green-tinted neutrals banned. guard: template-neutrals, retired-neutral-literal.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { BRAND_LOCK, RETIRED_MESHA_NEUTRALS, TEMPLATE_GREYS, TEMPLATE_SURFACES, THEME_LOCK, TOKEN_FILE, hasRetiredNeutralLiteral, isDriftRemoval, retiredNeutralFindings, primaryStateFindings, themeLockFindings, paletteCssText } from "./lib/design-palette.mjs";
import { legacyCss } from "./lib/legacy-css.mjs";

const app = join(dirname(fileURLToPath(import.meta.url)), "..");

test("retired green neutrals are caught in every spelling, the token file included", () => {
  for (const line of [
    ".a{background:#0E1512}",
    ".a{color:#94a89a}",
    ".a{background:rgb(29 40 32)}",
    ".a{border-color:rgba(148,168,154,.2)}",
    ".a{box-shadow:0 0 2px rgb(94 110 100/.24)}",
    "--shadow-rgb:148 168 154;",
  ]) assert.ok(hasRetiredNeutralLiteral(line), line);
  for (const line of [".a{background:var(--grey-200)}", ".a{border-color:rgb(145 158 171/.2)}", ".a{color:#7CCB45}", ".a{color:#919EAB}"]) {
    assert.ok(!hasRetiredNeutralLiteral(line), line);
  }
  assert.equal(retiredNeutralFindings("app/frame.css", ".a{color:#6E8377}").length, 1);
  assert.equal(retiredNeutralFindings(TOKEN_FILE, "--grey-600:#6E8377;").length, 1);
  assert.ok(hasRetiredNeutralLiteral(`.a{background:url("data:image/svg+xml,%3Csvg stroke='%2394a89a'%3E%3C/svg%3E")}`));
  const b64 = Buffer.from('<svg><stop stop-color="#1D2820"/></svg>').toString("base64");
  assert.ok(hasRetiredNeutralLiteral(`.a{background:url("data:image/svg+xml;base64,${b64}")}`));
});

test("theme-config and the token file carry exactly the template grey scale and surfaces", async () => {
  const tokens = readFileSync(join(app, TOKEN_FILE), "utf8");
  const { themeConfig } = await import("../theme/theme-config.ts");
  for (const [step, hex] of Object.entries(TEMPLATE_GREYS)) {
    assert.equal(themeConfig.palette.grey[step], hex, `grey ${step}`);
    assert.match(tokens, new RegExp(`--grey-${step}:${hex}`, "i"), `--grey-${step}`);
  }
  for (const mode of ["light", "dark"]) {
    assert.deepEqual(themeConfig.surfaces[mode].background, TEMPLATE_SURFACES[mode].background, `${mode} background`);
    assert.deepEqual(themeConfig.surfaces[mode].text, TEMPLATE_SURFACES[mode].text, `${mode} text`);
  }
  assert.ok(tokens.includes("--g500-rgb:145 158 171"));
  assert.deepEqual(retiredNeutralFindings(TOKEN_FILE, tokens), []);
});

test("no palette source writes a retired neutral", () => {
  for (const rel of ["theme/mesha-tokens.ts", TOKEN_FILE]) {
    assert.deepEqual(retiredNeutralFindings(rel, readFileSync(join(app, rel), "utf8")), [], rel);
  }
});

test("brand lock covers the Mesha brand + template surfaces in both themes, and all are present", () => {
  const theme = paletteCssText(app);
  for (const v of BRAND_LOCK) assert.ok(theme.toUpperCase().includes(v), v);
  for (const v of ["#141A21", "#1C252E", "#28323D", "#F4F6F8"]) assert.ok(BRAND_LOCK.includes(v), v);
  for (const v of RETIRED_MESHA_NEUTRALS) assert.ok(!BRAND_LOCK.includes(v), v);
  assert.deepEqual(themeLockFindings(theme), []);
});

test("the locked token values: Mesha brand, template neutrals", () => {
  assert.equal(THEME_LOCK.dark["--brand"], "#7CCB45");
  assert.equal(THEME_LOCK.dark["--logo"], "#7CCB45");
  assert.equal(THEME_LOCK.dark["--brand-d"], "#69BA37");
  assert.equal(THEME_LOCK.dark["--bg"], "#141A21");
  assert.equal(THEME_LOCK.dark["--paper"], "#1C252E");
  assert.equal(THEME_LOCK.dark["--paper-2"], "#28323D");
  assert.equal(THEME_LOCK.dark["--sidebar"], "#141A21");
  assert.equal(THEME_LOCK.dark["--on-brand"], "#08130B");
  assert.equal(THEME_LOCK.light["--brand"], "#54A02C");
  assert.equal(THEME_LOCK.light["--brand-d"], "#44831F");
  assert.equal(THEME_LOCK.light["--bg"], "#FFFFFF");
  assert.equal(THEME_LOCK.light["--paper-2"], "#F4F6F8");
  assert.equal(THEME_LOCK.light["--on-brand"], "#FFFFFF");
});

test("changing any locked value (either theme) fails the lock", () => {
  const theme = paletteCssText(app);
  for (const [mode, tokens] of Object.entries(THEME_LOCK)) {
    for (const name of Object.keys(tokens)) {
      const lightAt = theme.search(/\n\s*:root\.light\s*\{/);
      const at = mode === "dark" ? theme.indexOf(`${name}:`) : theme.indexOf(`${name}:`, lightAt);
      assert.ok(at > 0, `${mode} ${name} declared`);
      const end = theme.indexOf(";", at);
      const mutated = `${theme.slice(0, at)}${name}:#123456${theme.slice(end)}`;
      assert.ok(themeLockFindings(mutated).some((m) => m.startsWith(`${mode} ${name} `)), `${mode} ${name}`);
    }
  }
});

test("selected / primary states must fill with brand tokens", () => {
  for (const css of [
    ".metricseg a.on{background:var(--paper)}",
    ".btn.primary{background:linear-gradient(135deg,#34C98B,var(--primary))}",
    ".brand .logo{background:linear-gradient(135deg,#34C98B,var(--primary))}",
    ".pill .kit-tab-ind{background:var(--bg)}",
    ".input:checked + .track{background:var(--fg)}",
    ".rangepick button.on{background:var(--panel)}",
  ]) assert.equal(primaryStateFindings(css).length, 1, css);
  for (const css of [
    ".metricseg a.on{background:var(--brand);color:var(--on-brand)}",
    ".btn.primary:disabled{background:var(--panel)}",
    ".feed-tabbar .metricseg a.on{background:transparent}",
  ]) assert.equal(primaryStateFindings(css).length, 0, css);
});

test("removing a legacy avatar fill or a retired neutral is not drift; removing a brand colour is", () => {
  assert.equal(isDriftRemoval("#26384D"), false);
  assert.equal(isDriftRemoval("#7AC142"), false);
  assert.equal(isDriftRemoval("#0e1512"), false);
  assert.equal(isDriftRemoval("#F4F7F2"), false);
  assert.equal(isDriftRemoval("#7ccb45"), true);
  assert.equal(isDriftRemoval("#141a21"), true);
});

test("the old neutral leftovers and rgb() workaround are gone", () => {
  const theme = paletteCssText(app);
  assert.doesNotMatch(theme, /--mesha-(?:canvas|ink|line|d-)/);
  assert.doesNotMatch(theme, /rgb\(244 246 248\)/);
});

test("guard self-test proves retired-neutral-literal and template-neutrals fire", () => {
  const r = spawnSync(process.execPath, [join(app, "scripts/check-design-system.mjs"), "--self-test"], { encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
});

test("theme-token-drift counts removed palette TOKENS, not deleted legacy rule literals (TR1-#10)", async () => {
  const { removedTokenHexes } = await import("./lib/design-palette.mjs");
  const patch = [
    "--- a/apps/admin-web/app/mesha-theme.css",
    "-  --brand: #54a02c; --brand-2:#7CCB45;",
    "-  .cbm-cell.cbm-clear{background:#22633c;color:#d6f1e0}",
    "+  .kpi{position:relative}",
  ].join("\n");
  const removed = removedTokenHexes(patch);
  assert.ok(removed.has("#54a02c") && removed.has("#7ccb45"), "a removed token value is drift");
  assert.ok(!removed.has("#22633c") && !removed.has("#d6f1e0"), "a deleted off-palette rule literal is the fix, not drift");
});
