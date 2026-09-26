# Minimal (MUI) dashboard: token spec for admin-web

Source: measured with computed styles at a 1440x900 viewport on free.minimals.cc (the open Minimal v6/v7 build: `/`, `/user`, `/products`). The full demo at minimals.cc/dashboard (the MUI Store preview iframe) now redirects to a sign-in page, so it was not used. The full build uses the same theme tokens; the only difference is that its body font is Public Sans Variable instead of DM Sans Variable.
Colours: only neutrals, greys and shadows are listed here. Brand and accent colours stay Mesha green.

## 1. Foundations
| Token | Value |
|---|---|
| spacing unit | 8px |
| shape.borderRadius | 8px (buttons, inputs) |
| body font | "DM Sans Variable" (full: "Public Sans Variable"), -apple-system, system-ui, Segoe UI, Roboto, sans-serif |
| display font (h1–h3) | "Barlow" |
| z-index | appBar 1100 (header uses 1101), drawer 1200, modal 1300, snackbar 1400, tooltip 1500 |

### Neutral scale (grey)
50 #FCFDFD · 100 #F9FAFB · 200 #F4F6F8 · 300 #DFE3E8 · 400 #C4CDD5 · 500 #919EAB · 600 #637381 · 700 #454F5B · 800 #1C252E · 900 #141A21

These are the template's measured values, kept here as reference only. admin-web does NOT render them: Mesha has no cool-grey scale, so `--grey-N` / `--gN-rgb` in `app/minimal-tokens.css` and `palette.grey` in `theme/theme-config.ts` map each step onto the nearest locked Mesha neutral (50 #F4F7F2 · 100 #F1F5EF · 200 #ECF1E8 · 300 #E2E8E1 · 400 #9FB6A6 · 500 #94A89A · 600 #6E8377 · 700 #46564B · 800 #1D2820 · 900 #0E1512). The cool-grey values above fail `design:guard` (`minimal-grey-literal`, `mui-palette-lock`) in every file, inline SVG data URIs included. The same goes for every rgba(145,158,171,…) below: in code it is `rgb(var(--g500-rgb)/…)`, which is Mesha muted.

| Role | Value |
|---|---|
| text.primary | #1C252E |
| text.secondary | #637381 |
| text.disabled | #919EAB (also chart axis labels) |
| background.default | #F9FAFB (the page itself renders #FFFFFF) |
| background.paper | #FFFFFF |
| background.neutral | #F4F6F8 (table header background) |
| divider | rgba(145,158,171,0.2) |
| action.hover | rgba(145,158,171,0.08) |
| action.selected | rgba(145,158,171,0.16) |
| action.focus / disabledBackground | rgba(145,158,171,0.24) |
| action.disabled | rgba(145,158,171,0.8) |
| Skeleton bg | rgba(28,37,46,0.11) |
| Tooltip bg | rgba(69,79,91,0.92) |
| input placeholder opacity | 0.42 |

## 2. Shadows (all based on grey-500 145,158,171)
| Token | Value |
|---|---|
| card | `0 0 2px 0 rgba(145,158,171,.2), 0 12px 24px -4px rgba(145,158,171,.12)` |
| dropdown / popover | `0 0 2px 0 rgba(145,158,171,.24), -20px 20px 40px -4px rgba(145,158,171,.24)` |
| dialog | `-40px 40px 80px -8px rgba(0,0,0,.24)` |
| z1 | `0 1px 2px 0 rgba(145,158,171,.16)` |
| z4 / z8 / z12 / z16 / z20 / z24 | `0 N 2N 0` with N = 4/8, then `0 12px 24px -4px`, `0 16px 32px -4px`, `0 20px 40px -4px`, `0 24px 48px 0`, all rgba(145,158,171,.16) |
| colour button shadow | `0 8px 16px 0 rgba(<brand>,0.24)`: swap in the Mesha green channel |

## 3. Typography
| Variant | Weight | Size / line-height | Font |
|---|---|---|---|
| h1 | 800 | 40px / 1.25 (responsive) | Barlow |
| h2 | 800 | 32px / 1.333 | Barlow |
| h3 | 700 | 24px / 1.5 | Barlow |
| h4 | 700 | 20px / 1.5 base; page titles render 24px/36px at ≥lg | body |
| h5 | 700 | 18px / 1.5 | body |
| h6 | 600 | 17px / 1.556 (card header title renders 18px/28px) | body |
| subtitle1 | 600 | 16px / 1.5 | body |
| subtitle2 | 600 | 14px / 1.571 (22px) | body |
| body1 | 400 | 16px / 1.5 | body |
| body2 | 400 | 14px / 1.571 (22px) | body |
| caption | 400 | 12px / 1.5 | body |
| overline | 700 | 12px / 1.5, uppercase | body |
| button | 700 | 14px / 24px (small 13px, large 15px/26px) | body |

Letter spacing stays normal everywhere. KPI numbers use h4 at 24px/36px, weight 700.

## 4. Layout
| Element | Value |
|---|---|
| Sidebar (vertical nav) width | 300px, padding 20px 20px 0, border-right 1px solid rgba(145,158,171,.12), transparent/white bg, full height |
| Mini-nav width (full build) | 88px |
| Header | sticky, height 64px mobile / 72px desktop, transparent bg, becomes blurred once you scroll; `transition: box-shadow .3s cubic-bezier(.4,0,.2,1)`; icon buttons 36–40px round (50%) with 8px padding |
| Main container | max-width 1200px (lg), padding 8px 40px 64px at desktop, 8px 16px 64px on mobile |
| Grid gap | 24px (spacing 3) in both directions |
| Page title to content | h4, margin-bottom 40px (spacing 5) |
| KPI row | 4 equal cards (247px wide at a 1140px main area), 186px tall |

## 5. Navigation items
| Prop | Value |
|---|---|
| height (min) | 44px |
| padding | 8px 12px 8px 16px |
| radius | 6px |
| gap icon to label | 16px; icon 24x24 |
| label | 14px/22px, weight 500 (inactive) and 600 (active) |
| inactive colour | text.secondary #637381 |
| hover bg | action.hover rgba(145,158,171,.08) |
| active | bg = brand at 8% alpha, text = brand main; hover = brand at 16% |
| transition | `background-color .15s cubic-bezier(.4,0,.2,1)` |
| subheader (full build) | 11px, weight 700, uppercase, text.disabled, padding 16px 8px 8px 12px |
| sub-items (full build) | 36px high, a 12px dot or bullet, Collapse uses MUI auto duration (about 200–300ms, easeInOut) and the arrow rotates 90deg |

## 6. Cards
| Prop | Value |
|---|---|
| radius | 16px |
| shadow | customShadows.card (see section 2) |
| bg | #FFF, with no border |
| padding (widget) | 24px |
| CardHeader | padding 24px 24px 0; title 600 18px/28px; subheader 14px/22px text.secondary |
| transition | `box-shadow .3s cubic-bezier(.4,0,.2,1)` |
| KPI widget | label subtitle2 600 14/22; value 700 24/36; delta 600 14/22 with a trend icon; sparkline chart on the right |
| Product card | image is 1:1 with object-fit: cover (247x247); body padding 24px; title subtitle2 600 14/22; "Sale" label sits absolute at the top-right |

## 7. Tables (User list)
| Prop | Value |
|---|---|
| Toolbar | 96px high, padding 0 24px; search is an outlined input, 56px |
| Header row | 57px high; bg #F4F6F8 (background.neutral); text 600 14px/24px #637381; cell padding 16px; no border |
| Body row | 72px high with a 40px round avatar; cell 14px/22px text.primary; padding 16px |
| Row border | none; separation comes from white space only |
| Row hover | `background-color: action.hover` rgba(145,158,171,.08) |
| Selected row | brand at 8% |
| Checkbox cell | padding-left 4px; checkbox 38x38 (9px padding), round ripple |
| Row action | IconButton 36x36, round, 8px padding (the ⋮ menu) |
| Pagination | 52px high, 14px/21px text |

## 8. Buttons
| Size | Height | Padding | Font |
|---|---|---|---|
| small | 30px | 4px 8px | 700 13px |
| medium | 36px | 6px 16px | 700 14px/24px |
| large | 48px | 8px 16px (full build: 8px 22px) | 700 15px/26px |

The radius is 8px for every size, with no shadow at rest. The contained "inherit" button is #1C252E with white text; on hover it becomes #454F5B and gets shadow z8. Colour-contained buttons get the 24% colour shadow on hover. Soft variant: bg = colour at 16%, hover at 32%. Outlined variant: border rgba(145,158,171,.32), and on hover the border turns currentColour and the bg action.hover.
Transition: `background-color, box-shadow, border-color .25s cubic-bezier(.4,0,.2,1)`.
IconButtons are round (50%), 8px padding, 36–40px.

## 9. Inputs (outlined TextField)
| Prop | Value |
|---|---|
| height | 56px (medium) / 40px (small) |
| radius | 8px |
| input padding | 16.5px 14px |
| font | 16px/23px (15px in some full-build forms) |
| border | 1px rgba(145,158,171,.2); hover text.primary; focus 2px text.primary (Minimal focuses in dark grey, not brand colour) |
| label | 14px (focused/shrunk 16px bold, text.primary) |
| placeholder | text.disabled, opacity 0.42 |
| error | error.main border and helper text 12px |

## 10. Labels / status chips
| Prop | Value |
|---|---|
| height | 24px, min-width 24px |
| padding | 0 6px |
| radius | 6px |
| font | 700 12px, no text-transform |
| soft style | bg = colour at 16% alpha, text = colour.dark |
| neutral/default | bg rgba(145,158,171,.16), text #637381 |
| transition | `all .2s cubic-bezier(.4,0,.2,1)` |

MUI Chip (filters): 32px medium / 24px small, radius 8px, default border #C4CDD5, avatar and icon colour #454F5B.

## 11. Motion
| Use | Duration / easing |
|---|---|
| standard easing | cubic-bezier(0.4, 0, 0.2, 1) |
| entering (decelerate) | cubic-bezier(0, 0, 0.2, 1) (floating labels, 200ms) |
| nav hover | 150ms |
| buttons | 250ms |
| card shadow, header shadow | 300ms |
| label/chip | 200ms |
| Collapse / nav expand | MUI `auto` (about 200–300ms) |
| LinearProgress indeterminate | keyframes `left -35%→100%` and `-200%→107%`, 2.1s loop |
| Skeleton | MUI pulse (1.5s ease-in-out, opacity 1→0.4→1), with bg rgba(28,37,46,.11) |

## 12. Charts (ApexCharts)
| Prop | Value |
|---|---|
| font | the body font, inherited |
| grid | stroke var(--palette-divider), strokeDashArray 3, x-lines hidden and y-lines shown |
| axis | no border and no ticks; labels 12px weight 400 in text.disabled #919EAB |
| line/area | stroke width 2.5, round caps, smooth curve; area fill is a gradient from 0.48 to 0 opacity |
| bar | stroke 2 (transparent), column width about 28–36%, rounded 4px |
| legend | 13px/18px weight 500, placed at the top-right with round 12px markers |
| donut | stroke width 0 (a white stroke in some cards), labels hidden, total shown in the centre |
| tooltip | theme follows the mode; box uses dropdown shadow, 8px radius, and a blurred paper bg |
| data labels | off |
| entry animation | Apex defaults are kept (easeinout at about 800ms, with dynamic animation at 350ms) |

## 13. Implementation notes for admin-web
- Carry over the shadows, radii (8/6/16), the neutral scale, the table and nav metrics and the timings exactly as listed. Swap every blue (#1877F2) usage for the Mesha green scale, using the same alpha steps (8/16/24/32/48%).
- Use hover backgrounds as grey-500 alpha tints. Do not use solid greys for hover.
- Separate table rows with white space and hover only, never with borders.
- Paper surfaces (drawer, popover, autocomplete) keep the template's two corner glows, painted in the Mesha info (top right) and error (bottom left) colours of the active scheme instead of Minimal cyan/red.
- Deliberate difference: the header account control opens the Mesha account popover (role, park scope, sign out), not the template AccountDrawer. It carries Mesha-only content (role switch, park scope) that the template drawer has no slot for, and it already uses the locked palette.

## 14. Where this spec lives in code
- Non-colour tokens: `apps/admin-web/app/minimal-tokens.css` (the only file allowed raw px/radius/shadow/font-size values). Colours: `app/mesha-theme.css` (locked).
- Machine gate: `npm --prefix apps/admin-web run design:guard` (`scripts/lib/design-kit-ratchet.mjs`), with legacy debt as a shrink-only per-file ratchet in `scripts/check-design-system-waivers/design-system-waivers.json`.
- Component proof: every `components/kit/*.tsx` component has a story under `apps/admin-web/stories/`, captured by `npm run visual:stories` at 1440 + 390, light + dark.
