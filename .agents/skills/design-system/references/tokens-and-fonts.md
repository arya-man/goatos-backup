# Tokens and fonts

Canonical file: `apps/admin-web/app/mesha-theme.css` (aliases/overrides in
`app/minimal-theme.css`, frame CSS in `app/frame.css`). `check-design-system.mjs` asserts the
locked values below exist verbatim (`brand-lock`) and that no hex value present on
`origin/main` disappeared (`theme-token-drift`). Renaming a token is fine; moving a colour is P0.

| Role | Dark | Light | Tokens |
|---|---|---|---|
| brand | `#7CCB45`, hover `#69BA37`, soft `rgba(124,203,69,.15)` | `#54A02C` / `#44831F` | `--brand`, `--brand-d`, `--primary`, `--primary-soft`, `--primary-ink`, `--primary-fg` |
| page bg | `#0E1512` | `#FFFFFF` (Minimal) | `--bg`, `--bg-subtle` |
| panels | `#161F1A` / `#1D2820` | `#FFFFFF` / `#F1F5EF` | `--panel`/`--paper`, `--panel-2`/`--paper-2` |
| sidebar | `#0A0F0C` | `#ECF1E8` | `--sidebar-bg` |
| ink | `#E9F1EA` / muted `#94A89A` / faint `#6E8377` | `#16201B` / `#5E6E64` / `#8A998F` | `--fg`, `--fg-muted`, `--fg-faint` |
| lines | `#26332B` / `#1C261F` | `#E2E8E1` / `#EEF2ED` | `--line`, `--line2`, `--line-strong` |
| on-brand ink | `#08130B` | `#FFFFFF` | `--on-brand`, `--primary-fg` |
| status | info `#5B9BE8`, warning `#E0A53A`, error `#F0635F`, success `#7CCB45`, violet `#A78BF5` | info `#2B66B8`, warning `#B5791A`, error `#CC3D3D`, success `#3F9A28`, violet `#7A5BD1` | `--info`, `--warning`, `--error`, `--success`, `--violet` + `-soft` / `-ink` variants; legacy `--amber`, `--danger`, `--ok`, `--teal`, `--purple` |

Banned (P0, any file): `#0A9F6C`, `#4FD89A`, `#131A21`, `#1B242E` — another
product's palette. Banned (waivable debt only): any other hex/rgb literal in TSX, TS or CSS
outside the two theme files (pure `#000`/`#fff` in masks are allowed); Tailwind palette classes
(`bg-emerald-500`, `text-slate-400`, …).

Radii/elevation: `--r-lg` 16 (cards), `--r-md` 12, `--r-sm` 8, pills 999; `--shadow-card`
elevation-1. Spacing rhythm: 24 between sections (16 at ≤768), 24 card padding, 16 cell
padding, 8/12 inside controls.

## Fonts

`app/layout.tsx` loads **Public Sans** 400–800 (`--font-public-sans` → `--font-sans`) and
**Barlow** 600–800 (`--font-barlow` → `--font-display`) through `next/font/google`, self-hosted
at build time. No `<link>` to fonts.googleapis.com anywhere (P0 `google-fonts-link`).

Storybook cannot run next/font, so `.storybook/fonts.css` self-hosts the same latin subsets from
`.storybook/fonts/*.woff2` and `.storybook/preview.css` defines the two variables. The visual
lanes assert both families are LOADED (`font-not-loaded`) before a capture, so a story or route
that silently falls back to system-ui fails instead of producing a wrong baseline.

Type scale: display title 28/32 Barlow 700; KPI value 32 Barlow; card title 16/600; body 14
Public Sans; table header 12/600 uppercase muted; captions 12 muted.

Neutrals are the MUI Minimal TEMPLATE's (Ravi 2026-09-27): grey scale `--grey-50…900` = `#FCFDFD…#141A21`, dark surfaces `#141A21` / paper `#1C252E` / neutral `#28323D`, light `#FFFFFF` / neutral `#F4F6F8`, text and divider as the template derives them; only brand + status hues are Mesha. `theme/theme-config.ts` and `app/minimal-tokens.css` must carry exactly those values (P0 `template-neutrals`); the retired green-tinted neutrals (`#0E1512`, `#161F1A`, `#94A89A`, `#F4F7F2` …) fail everywhere (P0 `retired-neutral-literal`). Write greys as `var(--grey-N)` / `rgb(var(--g500-rgb)/a)` or theme tokens.
