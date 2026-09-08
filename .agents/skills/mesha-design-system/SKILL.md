---
name: mesha-design-system
description: >-
  Use when writing OR reviewing any admin-web (apps/admin-web/**) UI — a page,
  card, table, drawer, modal, chip, button, chart, empty state, loading skeleton
  or error boundary. Covers the Mesha visual system: the token layer, the three
  type roles (Instrument Serif for headings and figures, Instrument Sans for
  reading, Spline Sans Mono for labels), the one-accent colour rule, hairline
  card language, 2px-or-pill radii, and the traps that have already shipped
  visible defects. Invoke BEFORE adding a class, a colour, a radius or a font,
  and before pushing. Machine gate: npm --prefix apps/admin-web run
  check:design-system (runs inside check:mock-fidelity and ci-local).
---

# Mesha design system — admin-web

The system is defined once, in the token block at the top of
`apps/admin-web/app/mesha-theme.css`. **Never write a colour, radius or font
literal in a component.** If you need a value that does not exist as a token,
add the token — do not inline the value.

## Type has three roles. They do not overlap.

| Face | Token | Carries |
|---|---|---|
| Instrument Serif | `var(--f-serif)` | Factual headings, and **figures** — KPI values, headline counts, totals, currency. Numbers are the point of an operator dashboard. |
| Instrument Sans | `var(--f)` | All reading text: table cells, descriptions, form values, nav leaf items, prose. |
| Spline Sans Mono | `var(--fm)` | Every ALL-CAPS eyebrow, label, ledger key, status tag, button label, column header, and identifier (IDs, RFIDs, timestamps). Tracking .16–.22em. |

A number inside a dense table row stays sans or mono — serif is for the
**headline** figure, not every digit.

## Colour: spend it once

- `--brand` (MESHA green) is the ONE accent: links, active nav, primary CTAs.
- `--value` (antique brass) is reserved for **value/price/exchange** moments
  only. It is never a second accent, and never a warning colour.
- `--ok/--warn/--danger/--info/--purple/--teal` are SEMANTIC state, not
  decoration. Do not reach for them to make something look nicer.
- Ink tiers: `--ink` (primary), `--muted` (secondary), `--faint` (decoration
  only — it is below 4.5:1 and must not carry body copy).

## Surfaces

- Cards are a dense fill plus ONE 1px hairline (`--line`). No drop shadow for
  everyday elevation, no gradient border, no `backdrop-filter`.
- Radii: `var(--r)` (2px) on surfaces, `var(--r-pill)` on buttons and status
  pills. **Nothing in between** — a 8/10/12px radius is a defect.
- Buttons rest as a transparent pill outline and fill with their accent on
  hover, flipping ink to `var(--on-brand)`.

## Traps that have already shipped visible defects

These are not hypothetical. Each one reached a running screen.

1. **`var(--card)` / `var(--border)` are NOT tokens.** They are bare HSL
   triplets in `app/globals.css` (`60 10% 4%`), valid only inside `hsl()`. Used
   raw in a style the declaration is invalid and silently dropped — this shipped
   a login screen whose every input had no fill. Use `--panel`, `--panel-2`,
   `--line`, `--line2`.
2. **An undefined `var()` fails silently.** `var(--media-matte)` with no
   declaration resolved to `initial`, so the letterbox behind every verification
   video rendered transparent. Declare the token.
3. **Uppercase mono is much wider than sentence-case sans.** Converting a label
   can overflow a fixed table cell — "Verification pending" needs ~186px in a
   175px cell. Let it wrap; do not shrink the tracking.
4. **A `:root` prefix outranks scoped rules.** It silently killed `.btn.sm`
   (151 sites) and every per-page KPI size. Use `:where(:root)`, which adds zero
   specificity.
5. **Both themes, always.** A raw hex cannot follow `:root.light`. Every value
   goes through a token that both theme blocks define, and the `@media print`
   block must restate every token or it keeps its dark value on paper.
6. **Weights are clamped by the font file.** The variable faces carry 400–700; a
   `font-weight: 800` silently renders at 700.

## Before you push

```bash
npm --prefix apps/admin-web run check:design-system   # the machine gate
npm --prefix apps/admin-web run check:mock-fidelity   # includes the above
```

The guard is static and cannot see layout. A change to type, tracking or case
can still clip text inside a fixed-width cell, and only a rendered check finds
that — `scripts/smoke-visual-live.mjs` asserts no button or link text is clipped
at 1440x1000. Run it against a disposable stack, never against OCI or staging:
it CLICKS SUBMIT CONTROLS and writes rows.
