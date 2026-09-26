// Palette policy for design:guard (scripts/check-design-system.mjs).
//
// Ravi 2026-09-27: neutrals are the MUI Minimal TEMPLATE's; brand + status hues stay Mesha.
// - BRAND_LOCK: Mesha brand greens plus the template dark/light surfaces. They must stay verbatim in
//   app/mesha-theme.css (P0).
// - TEMPLATE_GREYS / TEMPLATE_SURFACES: the template grey scale (theme/core/palette.ts) and the
//   surfaces/ink it derives. theme/theme-config.ts and app/minimal-tokens.css must carry exactly
//   these (P0 template-neutrals, scripts/lib/mui-palette-lock.mjs).
// - RETIRED_MESHA_NEUTRALS: the old green-tinted neutral scale. BANNED in every file (P0
//   retired-neutral-literal); hex, rgb() and bare-channel spellings are caught.
// - THEME_LOCK: per-theme token -> exact value in app/mesha-theme.css (P0 brand-lock).
// - PRIMARY_STATE: selected/primary/active selectors must paint with brand tokens (P0 non-brand-selected).
// - RETIRED_NEUTRALS: colours whose removal from the theme files is not drift.

export const TOKEN_FILE = "app/minimal-tokens.css";

export const TEMPLATE_GREYS = {
  50: "#FCFDFD", 100: "#F9FAFB", 200: "#F4F6F8", 300: "#DFE3E8", 400: "#C4CDD5",
  500: "#919EAB", 600: "#637381", 700: "#454F5B", 800: "#1C252E", 900: "#141A21",
};

export const TEMPLATE_SURFACES = {
  light: { background: { default: "#FFFFFF", paper: "#FFFFFF", neutral: "#F4F6F8" }, text: { primary: "#1C252E", secondary: "#637381", disabled: "#919EAB" } },
  dark: { background: { default: "#141A21", paper: "#1C252E", neutral: "#28323D" }, text: { primary: "#FFFFFF", secondary: "#919EAB", disabled: "#637381" } },
};

export const BRAND_LOCK = [
  // brand greens / primary
  "#7CCB45", "#69BA37", "#54A02C", "#44831F", "#08130B", "#ECF7E0",
  // template surfaces + ink (dark, light)
  "#141A21", "#1C252E", "#28323D", "#919EAB", "#637381", "#F4F6F8", "#FFFFFF",
];

export const THEME_LOCK = {
  dark: {
    "--logo": "#7CCB45", "--brand": "#7CCB45", "--brand-d": "#69BA37", "--on-brand": "#08130B",
    "--bg": "#141A21", "--paper": "#1C252E", "--paper-2": "#28323D", "--fg": "#FFFFFF", "--fg-muted": "#919EAB",
    "--fg-faint": "#637381", "--line": "rgb(145 158 171/.2)", "--sidebar": "#141A21", "--g500-rgb": "145 158 171",
    "--primary": "var(--brand)", "--primary-fg": "var(--on-brand)",
  },
  light: {
    "--brand": "#54A02C", "--brand-d": "#44831F", "--brand-soft": "#ECF7E0", "--on-brand": "#FFFFFF",
    "--bg": "#FFFFFF", "--paper": "#FFFFFF", "--paper-2": "#F4F6F8", "--fg": "#1C252E", "--fg-muted": "#637381",
    "--fg-faint": "#919EAB", "--line": "rgb(145 158 171/.2)", "--sidebar": "#FFFFFF", "--g500-rgb": "145 158 171",
    "--primary": "var(--brand)", "--primary-fg": "var(--on-brand)",
  },
};

/** First declaration of each locked token inside the dark `:root{` and the `:root.light{` block. */
export function themeLockFindings(themeText) {
  const block = (re) => { const m = re.exec(themeText); if (!m) return ""; const i = m.index + m[0].length; return themeText.slice(i, themeText.indexOf("}", i)); };
  const blocks = { dark: block(/(?:^|\n)\s*:root\s*\{/), light: block(/(?:^|\n)\s*:root\.light\s*\{/) };
  const out = [];
  for (const [theme, tokens] of Object.entries(THEME_LOCK)) {
    for (const [name, want] of Object.entries(tokens)) {
      const m = new RegExp(`${name.replace(/-/g, "\\-")}\\s*:\\s*([^;]+);`).exec(blocks[theme]);
      const got = m ? m[1].trim() : "(missing)";
      if (got.toUpperCase() !== want.toUpperCase()) out.push(`${theme} ${name} must be ${want}, found ${got}`);
    }
  }
  return out;
}

// Selectors that are a primary / selected / active state. Their fill must be a brand token.
const PRIMARY_STATE = /(\.btn\.(?:p|primary)\b|\.kit-tab-ind|\.metricseg (?:a|button)\.on|\.move-date-day\.on|\.brand \.logo|\.countOn|rangepick button\.on|\.mzai-bubble\b|\.mzai-newbtn|\.leaf\.on \.navpill|\.pm-item\.on|:checked\s*\+\s*\.(?:box|track))/;
const SKIP_STATE = /:disabled|aria-disabled|:hover|:active|:focus|feed-tabbar|drawer \.dc|~/;
const BRAND_FILL = /^(?:var\(--(?:brand|brand-d|brand-soft|primary|primary-soft|logo|on-brand|on)\)|transparent|none)$/i;

/** CSS rules for a primary/selected state whose background is not a brand token. */
export function primaryStateFindings(text) {
  const out = [];
  const src = text.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, " "));
  const rule = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = rule.exec(src))) {
    const sel = m[1].trim();
    if (!PRIMARY_STATE.test(sel)) continue;
    const parts = sel.split(",").filter((p) => PRIMARY_STATE.test(p) && !SKIP_STATE.test(p));
    if (!parts.length) continue;
    for (const d of m[2].matchAll(/(?:^|;)\s*background(?:-color|-image)?\s*:\s*([^;]+)/g)) {
      const v = d[1].replace(/!important/, "").trim();
      if (!BRAND_FILL.test(v)) out.push({ line: src.slice(0, m.index).split("\n").length, snippet: `${parts[0].trim()} background:${v}` });
    }
  }
  return out;
}

// The pre-2026-09-27 Mesha green-tinted neutral scale (surfaces, ink, lines, grey mapping).
export const RETIRED_MESHA_NEUTRALS = [
  "#0E1512", "#121A16", "#161F1A", "#1D2820", "#0A0F0C", "#18211A", "#E9F1EA", "#94A89A", "#6E8377",
  "#26332B", "#1C261F", "#9FB6A6", "#EAF3EE", "#F4F7F2", "#F1F5EF", "#16201B", "#5E6E64", "#8A998F",
  "#E2E8E1", "#EEF2ED", "#ECF1E8", "#DDE7D8", "#46564B", "#13201A", "#182219",
];
const RETIRED_CHANNELS = ["148 168 154", "94 110 100", "159 182 166", "29 40 32"];

export const RETIRED_NEUTRALS = new Set([
  ...RETIRED_MESHA_NEUTRALS.map((h) => h.toLowerCase()),
  "#edf2ea", "#cfd9cc", "#33463a",
  // legacy .av avatar fills, replaced by kit Avatar (grey-300 / grey-700)
  "#26384d", "#2e4a63",
  // Colours that lived only in legacy rules the P4 sweeps deleted once the redesign had replaced
  // (or main had already dropped) every producer: the old sidebar's parked-park badge and footer
  // (.parked .leaf .pk, .sidefoot -> MUI nav), the old calendar picker's day badge (.calpicker-day b
  // -> MUI DateCalendar), the old weighing bar/segment fills (.weighing-bar.pur, .weighing-segment
  // .individual/.lumpsum, no producer on main), and the config-sheet progress fallback
  // (.cfg-sheet-progress -> MUI LinearProgress). None is a palette token.
  "#6f8779", "#2c4337", "#243a2e", "#061016", "#a98bf5", "#79b7ff", "#c6b4ff", "#7ac142",
]);

const hexToTriple = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
const RETIRED_TRIPLES = [...RETIRED_MESHA_NEUTRALS.map((h) => hexToTriple(h).join(" ")), ...RETIRED_CHANNELS];
const RETIRED_HEX = new RegExp(`#(?:${RETIRED_MESHA_NEUTRALS.map((h) => h.slice(1)).join("|")})\\b`, "i");
const RETIRED_RGB = new RegExp(`rgba?\\(\\s*(?:${RETIRED_TRIPLES.map((t) => t.split(" ").join("[\\s,]+")).join("|")})\\b`);
// A bare channel triple (`--x:148 168 154`) outside rgb().
const RETIRED_BARE = new RegExp(`:\\s*(?:${RETIRED_TRIPLES.map((t) => t.split(" ").join("\\s+")).join("|")})\\s*[;}]`);

// Base64 payloads are [A-Za-z0-9+/=]; URL-encoded payloads carry spaces and quotes, so they run to
// the closing `"`, backtick or `)` (or the end of the line).
const DATA_URI = /data:image\/svg\+xml(?:(;base64),([A-Za-z0-9+/=]+)|,([^"`)\n]+))/g;

/**
 * Returns `text` followed by the decoded payload of every inline SVG data URI in it (base64 and
 * URL-encoded), so a colour hidden in `url("data:image/svg+xml;base64,...")` or `%23919eab` is
 * scanned like any other literal. Judge 3 P1-A: the template paper mixin painted Minimal cyan/red
 * through base64 SVGs that a hex scan never saw.
 */
export function withDecodedDataUris(text) {
  const decoded = [];
  for (const m of text.matchAll(DATA_URI)) {
    try {
      decoded.push(m[1] ? Buffer.from(m[2], "base64").toString("utf8") : decodeURIComponent(m[3].replace(/%(?![0-9a-f]{2})/gi, "%25")));
    } catch {
      decoded.push((m[2] ?? m[3]).replace(/%23/gi, "#"));
    }
  }
  return decoded.length ? `${text}\n${decoded.join("\n")}` : text;
}

/** True when a line spells a retired Mesha neutral (hex, rgb()/rgba(), or bare channels), data URIs decoded. */
export function hasRetiredNeutralLiteral(line) {
  const src = withDecodedDataUris(line).replace(/%23([0-9a-f]{6})\b/gi, "#$1");
  return RETIRED_HEX.test(src) || RETIRED_RGB.test(src) || RETIRED_BARE.test(src);
}

/** The retired green-tinted neutrals are banned everywhere, the token file included. */
export function retiredNeutralFindings(rel, text) {
  const hits = [];
  text.split("\n").forEach((line, i) => {
    if (hasRetiredNeutralLiteral(line)) hits.push({ line: i + 1, snippet: line });
  });
  return hits;
}

/** Removed theme colours that count as drift: anything except the retired neutrals. */
export function isDriftRemoval(hex) {
  return !RETIRED_NEUTRALS.has(hex.toLowerCase());
}
