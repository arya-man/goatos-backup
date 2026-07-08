# Design System — Goat OS Operator Mobile

The app's visual system is **ported from `mock/vaccination-mobile-mock.html`** —
the only UI/UX source of truth. This doc freezes the tokens and component anatomy
so Compose matches the mock's structure (not a plainer substitute).

Lives in `core-designsystem`. Exposes `GoatOsTheme { }` + a `GoatOsTokens`
object; components live in `core-ui`.

## 1. Color tokens (exact from the mock)

Dark is the primary field theme; light is supported (leadership indoors / bright
sun). Values are the mock's CSS custom properties.

```text
DARK (default)
  brand        #8AD457   brand-2     #5FB531   brand-d (text-on-dark) #B7EA8C
  gradient     135° #93DA5E → #5FB531        glow  0 8 26 rgba(95,181,49,.30)
  bg           #0B100D   page-bg     #0A0F0C
  surf         #131A15   surf2       #1A241D   surf3 #222E25
  ink          #ECF4EE   muted       #8FA497   faint #5F7367
  hair (border)#28352B
  danger       #FB6F63   danger-x    rgba(251,111,99,.15)
  warn         #F0B54B   warn-x      rgba(240,181,75,.15)
  ok           #8AD457   ok-x        rgba(138,212,87,.16)

LIGHT
  page-bg      #EDF1EA   page-ink    #141C17   page-muted #54655B
  (brand/danger/warn keep hue; surfaces invert to light neutrals)
```

Compose: expose as a `GoatOsColors` data class provided via
`CompositionLocalProvider`; do **not** use raw Material 3 defaults — map M3 slots
(`primary`, `surface`, `error`, etc.) onto these tokens so components inherit the
Goat OS palette. Theme reacts to system dark/light + an in-app override
(matching the mock's theme toggle).

## 2. Typography

- Family: system default (`-apple-system`/Roboto stack in the mock → Roboto /
  system on Android). One display face; **no custom font file** (APK size).
- Scale (from the mock's sizes): display 40 (coverage hero), title 22, section
  16 (`sh-h`), body 13.5, label 12, micro 11–11.5, tab/pill 11.
- Monospace token (`--mono`) for **RFID tags, batch ids** — use a mono style so
  digits align (mock renders tags in mono).
- Weights: 600 (body emphasis), 700–780 (titles/names), 800 (numbers/pills).
- Numbers use tabular figures (`fontFeatureSettings "tnum"`).

## 3. Spacing, radius, elevation, motion

```text
spacing scale   4 · 8 · 11 · 13 · 16 · 20 · 22   (px in mock → dp)
card padding    15–16   ; screen gutter 16
radius          card 18 · sheet top 26 · chip 9–11 · pill 999 · avatar 12–15
elevation       flat surfaces + 1px hairline (--hair) borders; brand glow only on
                primary CTA / active avatar (do not over-shadow — low-end GPU)
motion          sheet slide-up 260ms ease; ring dash animate; tab .on transitions;
                keep to transform/opacity (cheap); no heavy shadows/blurs
```

## 4. Component inventory (port anatomy, not just colors)

Each maps to a `core-ui` composable. Match the mock's markup structure, states,
and interaction states — a bare substitute is a defect.

| Mock element | Composable | Anatomy to preserve |
|---|---|---|
| Bottom nav | `GoatBottomNav` | icon + label, active brand state |
| Top header (`vhead`) | `GoatTopBar` | back/menu button, eyebrow + title, trailing avatar |
| Card (`.card`) | `GoatCard` | surf bg, hairline border, 18 radius, 15–16 pad |
| Shed card (`.shed`) | `ShedCard` | house icon, cohort·in-shed, status pill, **vaccine-group chips**, in-shed/due/done nums row, progress bar, action (Start/Resume/View records) |
| Vaccine-group chip (`.vchip`) | `VaccineChip` | syringe glyph, vaccine name, `done/due` count, dimmed when full |
| Scan ring (`.ring`) | `ScanRing` | SVG-equivalent Canvas ring, center count `n/T`, label; per-group progress chips (`.vgchip`, active-highlighted) |
| Quick chips (`.qchips`) | `CountTiles` | Done / Pending / Skipped tappable tiles |
| Live scan row (`.slrow`/`li`) | `ScanRow` | ok/skip/pending icon, mono tag(s) + "2 tags", vaccine·dose |
| KPI tile (`.kpi`) | `KpiTile` | big number, label, tap affordance (given/pending drills) |
| Hero (`.hero`) | `CoverageHero` | gradient bg, big %, doses line, pills (scope picker, animals, data-gaps) |
| Backlog row (`.vaxr`) | `BacklogRow` | vaccine, note, count pill, bar |
| List row (`.lrow`) | `LeadRow` | mini-ring, title+sub, chips, trailing frac, optional assign button |
| Bottom sheet (`.sheet`) | `GoatBottomSheet` | grip, sticky header (title+sub), scrollable body, footer; **capped height + internal scroll** (mock fix) |
| Picker option (`.popt`/`.lopt`) | `PickerRow` | leading initials/glyph, name+sub, check when selected |
| Pill/badge (`.pill ok/warn/dng/mut`) | `StatusPill` | tone variants |
| Segmented (`.segc`/`.seg`) | `SegmentedControl` | week/month/history, given/pending/skipped, primary/backup |
| Toast | `GoatToast` | icon + title + subtitle, auto-dismiss |
| Drawer (`.draw`) | `NavDrawer` | profile header, module list (built ones + "Soon"), settings |
| Buffer/info boxes | `InfoBox` / `BufferBanner` | muted explainer blocks |
| Roster-change cards | `ChangeCard` | tagged reason rows (quarantine/death/shift/birth) |

## 5. Iconography

- Line icons, ~18dp cap (mock's `.ic`). Brand color for active/primary; muted
  otherwise. Syringe = **Vaccination module** only (never the Preventive Care
  vertical — taxonomy rule). Provide as a small vector set in `core-designsystem`.

## 6. Accessibility

- Min touch target 40dp+ (mock's narrow-mode override) — apply to all tappable
  rows/tiles/pills.
- Contrast: verify tokens pass WCAG AA in both themes (the admin-web smoke
  already flags faint-on-surface; keep pending states dashed-border + muted, not
  low-opacity — mock lesson).
- Content descriptions on every icon-only control; scan ring exposes count as
  state text; TalkBack labels for shed/vaccine/animal rows.
- Respect system font scale up to a bounded max; layouts must not clip.

## 7. Internationalization in UI

- All static strings in per-locale resources (en/hi/kn/te). Backend-owned copy
  localized via bootstrap.
- Kannada/Telugu/Hindi are wider — components use `wrapContentHeight` + `maxLines`
  with ellipsis only where the mock does; pills/chips must wrap, never clip
  (the mock already sizes for this).
- Mono tags/batches stay Latin digits regardless of locale.

## 8. Handoff to design tooling

The maintainer asked for a Figma/Claude-design importable system. This token +
component table is the spec. Recommended: generate a Compose `GoatOsTokens.kt`
and a matching `tokens.json` (Style-Dictionary shape) from this doc so Figma
variables and Compose stay in sync from one source. (Deferred until the app
module exists; tracked in the TRD implementation order.)
