import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join, relative } from "node:path";
import { TEMPLATE_GREYS, TEMPLATE_SURFACES, hasRetiredNeutralLiteral, withDecodedDataUris } from "./design-palette.mjs";

// Locked values the MUI theme must carry (light / dark): Mesha brand, template surfaces.
export const MUI_PALETTE_LOCK = {
  light: { primary: "#54A02C", primaryDark: "#44831F", onBrand: "#FFFFFF", bg: "#FFFFFF", paper: "#FFFFFF", neutral: "#F4F6F8", sidebar: "#FFFFFF" },
  dark: { primary: "#7CCB45", primaryDark: "#69BA37", onBrand: "#08130B", bg: "#141A21", paper: "#1C252E", neutral: "#28323D", sidebar: "#141A21" },
};

/**
 * template-neutrals: theme-config's grey scale + surfaces/ink and the token file's grey scale are
 * exactly the Minimal template's (Ravi 2026-09-27). Returns one message per mismatch.
 */
export function templateNeutralFindings(appRoot) {
  const out = [];
  const cfgPath = join(appRoot, "theme", "theme-config.ts");
  if (existsSync(cfgPath)) {
    const cfg = readFileSync(cfgPath, "utf8");
    const gi = cfg.search(/grey: \{\s*50:/);
    const grey = gi < 0 ? "" : cfg.slice(gi, cfg.indexOf("}", gi));
    for (const [step, hex] of Object.entries(TEMPLATE_GREYS)) if (!grey.includes(`${step}: '${hex}'`)) out.push(`theme-config grey ${step} must be the template ${hex}`);
    const si = cfg.indexOf("surfaces:");
    for (const [mode, v] of Object.entries(TEMPLATE_SURFACES)) {
      const i = cfg.indexOf(`    ${mode}: {`, si);
      const seg = si < 0 || i < 0 ? "" : cfg.slice(i, i + 400);
      const b = v.background, t = v.text;
      for (const need of [`background: { default: '${b.default}', paper: '${b.paper}', neutral: '${b.neutral}' }`, `text: { primary: '${t.primary}', secondary: '${t.secondary}', disabled: '${t.disabled}' }`])
        if (!seg.includes(need)) out.push(`theme-config ${mode} surfaces must be the template's: ${need}`);
    }
  }
  const tokPath = join(appRoot, "app", "minimal-tokens.css");
  if (existsSync(tokPath)) {
    const tok = readFileSync(tokPath, "utf8").toUpperCase();
    for (const [step, hex] of Object.entries(TEMPLATE_GREYS)) if (!tok.includes(`--GREY-${step}:${hex}`)) out.push(`app/minimal-tokens.css --grey-${step} must be the template ${hex}`);
  }
  return out;
}
const MINIMAL_DEFAULT_BRAND = /#(00A76F|5BE49B|007867|004B50|C8FAD6|8E33FF|00B8D9|FF5630|FFAB00|22C55E)\b/i;
export function muiPaletteLockFindings(appRoot) {
  const out = [];
  const cfgPath = join(appRoot, "theme", "theme-config.ts");
  if (!existsSync(cfgPath)) return out;
  const cfg = readFileSync(cfgPath, "utf8");
  const block = (name) => {
    const i = cfg.indexOf(`${name}: {`);
    return i < 0 ? "" : cfg.slice(i, cfg.indexOf("\n    },", i));
  };
  const lightPrimary = block("palette").split("secondary:")[0];
  const darkPrimary = block("paletteDark").split("secondary:")[0];
  const L = MUI_PALETTE_LOCK.light, D = MUI_PALETTE_LOCK.dark;
  if (!lightPrimary.includes(`main: '${L.primary}'`) || !lightPrimary.includes(`dark: '${L.primaryDark}'`) || !lightPrimary.includes(`contrastText: '${L.onBrand}'`))
    out.push(`MUI light primary must be ${L.primary}/${L.primaryDark} on ${L.onBrand}`);
  if (!darkPrimary.includes(`main: '${D.primary}'`) || !darkPrimary.includes(`dark: '${D.primaryDark}'`) || !darkPrimary.includes(`contrastText: '${D.onBrand}'`))
    out.push(`MUI dark primary must be ${D.primary}/${D.primaryDark} on ${D.onBrand}`);
  for (const [mode, v] of [["light", L], ["dark", D]]) {
    const need = [`default: '${v.bg}', paper: '${v.paper}', neutral: '${v.neutral}'`];
    if (v.sidebar) need.push(`sidebar: '${v.sidebar}'`);
    const i = cfg.indexOf(`    ${mode}: {`, cfg.indexOf("surfaces:"));
    const seg = i < 0 ? "" : cfg.slice(i, i + 600);
    for (const n of need) if (!seg.includes(n)) out.push(`MUI ${mode} surface missing ${n}`);
  }
  // Every colour literal under theme/, layouts/, app/ and components/ must be a locked value
  // (app/mesha-theme.css, app/minimal-tokens.css): this keeps the retired green neutrals and the
  // template's brand presets off screen. Inline SVG data URIs (base64 and URL-encoded) are decoded first, so a
  // colour cannot hide inside a background image (Judge 3 P1-A). Iconify icon bodies (brand logos
  // inside icon-sets.ts) are artwork, not theme colour. public/ SVGs (the template's empty-state
  // illustration) are scanned too.
  const lockedCss = existsSync(join(appRoot, "app", "mesha-theme.css")) ? readFileSync(join(appRoot, "app", "mesha-theme.css"), "utf8") : "";
  const tokensCss = existsSync(join(appRoot, "app", "minimal-tokens.css")) ? readFileSync(join(appRoot, "app", "minimal-tokens.css"), "utf8") : "";
  const locked = new Set([...`${lockedCss}\n${tokensCss}`.matchAll(/#[0-9a-f]{6}\b/gi)].map((m) => m[0].toUpperCase()));
  for (const hex of Object.values(TEMPLATE_GREYS)) locked.add(hex);
  locked.add("#28323D").add("#282F37").add("#EDEFF2");
  locked.add("#000000").add("#FFFFFF");
  const ICON_DATA = /layouts[\\/]template[\\/]iconify[\\/]icon-sets\.ts$/;
  for (const dir of ["theme", "layouts", "app", "components", "public"]) {
    const base = join(appRoot, dir);
    if (!existsSync(base)) continue;
    const walk = (d) => {
      for (const e of readdirSync(d, { withFileTypes: true })) {
        const f = join(d, e.name);
        if (e.isDirectory()) walk(f);
        else if (/\.(tsx?|css|svg)$/.test(e.name)) {
          if (/\.(test|stories)\.tsx?$/.test(e.name)) continue;
          const src = withDecodedDataUris(readFileSync(f, "utf8")).replace(/%23([0-9a-f]{6})\b/gi, "#$1");
          if (src.split("\n").some(hasRetiredNeutralLiteral)) out.push(`${relative(appRoot, f)} carries a retired Mesha green neutral`);
          const m = src.match(MINIMAL_DEFAULT_BRAND);
          if (m) out.push(`${relative(appRoot, f)} carries Minimal default colour ${m[0]}`);
          if (lockedCss && !ICON_DATA.test(f)) {
            for (const hex of new Set([...src.matchAll(/#[0-9a-f]{6}\b/gi)].map((x) => x[0].toUpperCase())))
              if (!locked.has(hex)) out.push(`${relative(appRoot, f)} carries ${hex}, which is not in the locked Mesha palette (app/mesha-theme.css)`);
          }
        }
      }
    };
    walk(base);
  }
  // A chart series array holds locked tokens only: a color-mix() blend there paints a colour that is
  // in no palette (judge 4 P1-8: seven off-palette fills on /feed/analytics).
  const SERIES_ARRAY = /\b([A-Z0-9_]*(?:SERIES|COLORS|COLOURS|PALETTE)[A-Z0-9_]*)\s*(?::[^=\n]+)?=\s*\[([\s\S]*?)\]/g;
  for (const dir of ["components", "features"]) {
    const base = join(appRoot, dir);
    if (!existsSync(base)) continue;
    const walk = (d) => {
      for (const e of readdirSync(d, { withFileTypes: true })) {
        const f = join(d, e.name);
        if (e.isDirectory()) walk(f);
        else if (/\.tsx?$/.test(e.name) && !/\.(test|stories)\.tsx?$/.test(e.name)) {
          for (const m of readFileSync(f, "utf8").matchAll(SERIES_ARRAY))
            if (m[2].includes("color-mix(")) out.push(`${relative(appRoot, f)} blends colours with color-mix() inside chart series array ${m[1]}; use locked tokens`);
        }
      }
    };
    walk(base);
  }
  return out;
}
