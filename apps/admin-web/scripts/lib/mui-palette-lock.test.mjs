import { test } from "node:test";
import assert from "node:assert/strict";
import { cpSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { MUI_PALETTE_LOCK, muiPaletteLockFindings } from "./mui-palette-lock.mjs";
import { themeConfig } from "../../theme/theme-config.ts";

const appRoot = join(dirname(fileURLToPath(import.meta.url)), "..", "..");

test("MUI theme palette carries the locked Mesha palette (reads theme-config)", () => {
  const L = MUI_PALETTE_LOCK.light;
  const D = MUI_PALETTE_LOCK.dark;
  assert.equal(themeConfig.palette.primary.main, L.primary);
  assert.equal(themeConfig.palette.primary.dark, L.primaryDark);
  assert.equal(themeConfig.palette.primary.contrastText, L.onBrand);
  assert.equal(themeConfig.paletteDark.primary.main, D.primary);
  assert.equal(themeConfig.paletteDark.primary.dark, D.primaryDark);
  assert.equal(themeConfig.paletteDark.primary.contrastText, D.onBrand);
  assert.deepEqual(themeConfig.surfaces.light.background, { default: L.bg, paper: L.paper, neutral: L.neutral });
  assert.deepEqual(themeConfig.surfaces.dark.background, { default: D.bg, paper: D.paper, neutral: D.neutral });
  assert.equal(themeConfig.surfaces.dark.sidebar, D.sidebar);
  assert.equal(themeConfig.modeStorageKey, "mesha.shell.theme");
  assert.equal(themeConfig.cssVariables.colorSchemeSelector, "data-theme");
});

test("design guard finds no MUI palette drift in theme/ and layouts/", () => {
  assert.deepEqual(muiPaletteLockFindings(appRoot), []);
});

test("the lock fails on Minimal colours anywhere under app/ and components/, data URIs included", () => {
  const root = mkdtempSync(join(tmpdir(), "mui-lock-"));
  try {
    for (const d of ["theme", "app", "components/kit", "theme/core/mixins"]) mkdirSync(join(root, d), { recursive: true });
    cpSync(join(appRoot, "theme", "theme-config.ts"), join(root, "theme", "theme-config.ts"));
    cpSync(join(appRoot, "app", "mesha-theme.css"), join(root, "app", "mesha-theme.css"));
    assert.deepEqual(muiPaletteLockFindings(root), []);
    // The old Minimal grey scale in the token file (Judge 3 P1-B).
    writeFileSync(join(root, "app", "minimal-tokens.css"), ":root{--grey-500:#919EAB;--g800-rgb:28 37 46}");
    // A Minimal grey in a kit module.
    writeFileSync(join(root, "components", "kit", "avatar.module.css"), ".a{background:rgb(69 79 91)}");
    // The template paper mixin: Minimal cyan inside a base64 SVG (Judge 3 P1-A).
    const cyan = Buffer.from('<svg><stop stop-color="#00B8D9"/></svg>').toString("base64");
    writeFileSync(join(root, "theme", "core", "mixins", "paper.ts"), `const s = 'data:image/svg+xml;base64,${cyan}';`);
    // A URL-encoded SVG stroke in a colour that is not in the locked palette.
    writeFileSync(join(root, "components", "kit", "select.module.css"), `.s{background:url("data:image/svg+xml,%3Csvg stroke='%23123456'%3E%3C/svg%3E")}`);
    // The template empty-state illustration in public/.
    mkdirSync(join(root, "public", "minimal"), { recursive: true });
    writeFileSync(join(root, "public", "minimal", "ic-content.svg"), '<svg><path fill="#DFE3E8"/></svg>');
    const out = muiPaletteLockFindings(root).join("\n");
    assert.match(out, /app\/minimal-tokens\.css carries a Minimal cool-grey value/);
    assert.match(out, /app\/minimal-tokens\.css carries #919EAB/);
    assert.match(out, /components\/kit\/avatar\.module\.css carries a Minimal cool-grey value/);
    assert.match(out, /theme\/core\/mixins\/paper\.ts carries Minimal default colour #00B8D9/);
    assert.match(out, /components\/kit\/select\.module\.css carries #123456/);
    assert.match(out, /public\/minimal\/ic-content\.svg carries a Minimal cool-grey value/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("the lock fails a color-mix() blend inside a chart series array", () => {
  const root = mkdtempSync(join(tmpdir(), "mui-lock-series-"));
  try {
    for (const d of ["theme", "app", "components"]) mkdirSync(join(root, d), { recursive: true });
    cpSync(join(appRoot, "theme", "theme-config.ts"), join(root, "theme", "theme-config.ts"));
    cpSync(join(appRoot, "app", "mesha-theme.css"), join(root, "app", "mesha-theme.css"));
    writeFileSync(join(root, "components", "ok.tsx"), 'export const SERIES_VARS = ["var(--brand)", "var(--info)"] as const;');
    assert.deepEqual(muiPaletteLockFindings(root), []);
    writeFileSync(join(root, "components", "bad.tsx"), 'export const SERIES_VARS = ["var(--brand)", "color-mix(in srgb, var(--info) 50%, var(--purple))"] as const;');
    assert.match(muiPaletteLockFindings(root).join("\n"), /bad\.tsx blends colours with color-mix\(\) inside chart series array SERIES_VARS/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
