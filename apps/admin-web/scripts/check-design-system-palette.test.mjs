// Palette policy for design:guard: brand lock, Minimal greys as tokens only, retired neutrals.
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { BRAND_LOCK, MINIMAL_GREYS, THEME_LOCK, TOKEN_FILE, hasMinimalGreyLiteral, isDriftRemoval, minimalGreyFindings, primaryStateFindings, themeLockFindings } from "./lib/design-palette.mjs";

const app = join(dirname(fileURLToPath(import.meta.url)), "..");

test("Minimal greys are caught in every spelling outside the token file", () => {
  for (const line of [
    ".a{background:#F4F6F8}",
    ".a{color:#919eab}",
    ".a{background:rgb(244 246 248)}",
    ".a{border-color:rgba(145,158,171,.2)}",
    ".a{box-shadow:0 0 2px rgb(145 158 171/.24)}",
    "--shadow-rgb:145 158 171;",
  ]) assert.ok(hasMinimalGreyLiteral(line), line);
  for (const line of [".a{background:var(--grey-200)}", ".a{border-color:rgb(var(--g500-rgb)/.2)}", ".a{color:#7CCB45}"]) {
    assert.ok(!hasMinimalGreyLiteral(line), line);
  }
  assert.equal(minimalGreyFindings("app/frame.css", ".a{color:#637381}").length, 1);
  // The token file is no longer exempt: it maps the grey tokens onto Mesha neutrals.
  assert.equal(minimalGreyFindings(TOKEN_FILE, "--grey-600:#637381;").length, 1);
  assert.equal(minimalGreyFindings(TOKEN_FILE, "--g400-rgb:196 205 213;").length, 1);
  // Inline SVG data URIs are decoded: URL-encoded and base64 spellings are caught too.
  assert.ok(hasMinimalGreyLiteral(`.a{background:url("data:image/svg+xml,%3Csvg stroke='%23919eab'%3E%3C/svg%3E")}`));
  const b64 = Buffer.from('<svg><stop stop-color="#637381"/></svg>').toString("base64");
  assert.ok(hasMinimalGreyLiteral(`.a{background:url("data:image/svg+xml;base64,${b64}")}`));
});

test("the token file maps every grey token onto the locked Mesha neutrals of theme-config", async () => {
  const tokens = readFileSync(join(app, TOKEN_FILE), "utf8");
  const { themeConfig } = await import("../theme/theme-config.ts");
  const theme = readFileSync(join(app, "app/mesha-theme.css"), "utf8").toUpperCase();
  for (const [step, hex] of Object.entries(themeConfig.palette.grey)) {
    assert.match(tokens, new RegExp(`--grey-${step}:${hex}`, "i"), `--grey-${step}`);
    assert.ok(theme.includes(hex.toUpperCase()), `${hex} is a locked Mesha value`);
  }
  const triple = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)).join(" ");
  for (const step of [400, 500, 800]) assert.ok(tokens.includes(`--g${step}-rgb:${triple(themeConfig.palette.grey[step])}`), `--g${step}-rgb`);
  assert.deepEqual(minimalGreyFindings(TOKEN_FILE, tokens), []);
  for (const hex of Object.values(MINIMAL_GREYS)) assert.ok(!tokens.toUpperCase().includes(hex), hex);
});

test("no app/components/features CSS writes a Minimal grey literal", () => {
  for (const rel of ["app/mesha-theme.css", "app/minimal-theme.css", "app/frame.css", TOKEN_FILE]) {
    assert.deepEqual(minimalGreyFindings(rel, readFileSync(join(app, rel), "utf8")), [], rel);
  }
});

test("brand lock covers brand + Mesha neutrals in both themes, and all are present", () => {
  const theme = readFileSync(join(app, "app/mesha-theme.css"), "utf8");
  for (const v of BRAND_LOCK) assert.ok(theme.toUpperCase().includes(v), v);
  for (const v of ["#F4F7F2", "#F1F5EF", "#E9F1EA", "#94A89A"]) assert.ok(BRAND_LOCK.includes(v), v);
  assert.deepEqual(themeLockFindings(theme), []);
});

test("the locked token values are exactly origin/main's Mesha palette", () => {
  assert.equal(THEME_LOCK.dark["--brand"], "#7CCB45");
  assert.equal(THEME_LOCK.dark["--logo"], "#7CCB45");
  assert.equal(THEME_LOCK.dark["--brand-d"], "#69BA37");
  assert.equal(THEME_LOCK.dark["--bg"], "#0E1512");
  assert.equal(THEME_LOCK.dark["--paper"], "#161F1A");
  assert.equal(THEME_LOCK.dark["--paper-2"], "#1D2820");
  assert.equal(THEME_LOCK.dark["--sidebar"], "#0A0F0C");
  assert.equal(THEME_LOCK.dark["--on-brand"], "#08130B");
  assert.equal(THEME_LOCK.light["--brand"], "#54A02C");
  assert.equal(THEME_LOCK.light["--brand-d"], "#44831F");
  assert.equal(THEME_LOCK.light["--bg"], "#F4F7F2");
  assert.equal(THEME_LOCK.light["--paper-2"], "#F1F5EF");
  assert.equal(THEME_LOCK.light["--on-brand"], "#FFFFFF");
});

test("changing any locked hex (either theme) fails the lock", () => {
  const theme = readFileSync(join(app, "app/mesha-theme.css"), "utf8");
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

test("removing a legacy avatar fill is not drift; removing a brand or neutral colour is", () => {
  assert.equal(isDriftRemoval("#26384D"), false);
  assert.equal(isDriftRemoval("#7ccb45"), true);
  assert.equal(isDriftRemoval("#0e1512"), true);
  assert.equal(isDriftRemoval("#F4F7F2"), true);
});

test("the old neutral leftovers and rgb() workaround are gone", () => {
  const theme = readFileSync(join(app, "app/mesha-theme.css"), "utf8");
  assert.doesNotMatch(theme, /--mesha-(?:canvas|ink|line|d-)/);
  assert.doesNotMatch(theme, /rgb\(244 246 248\)/);
});

test("guard self-test proves minimal-grey-literal fires", () => {
  const r = spawnSync(process.execPath, [join(app, "scripts/check-design-system.mjs"), "--self-test"], { encoding: "utf8" });
  assert.equal(r.status, 0, r.stderr);
});
