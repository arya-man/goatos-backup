# Every visible date is DD/MM/YYYY

**Maintainer decision, 2026-09-10.** Supersedes the 2026-08-21 admin-web
`DD-MM-YYYY` rule on the separator and on compact labels.

## The rule

Every VISIBLE date on every Goat OS surface renders **`DD/MM/YYYY`**, with
slashes, in full:

```text
14/08/2026            a date
14/08/2026 09:30      a timestamp (24-hour)
14/08/2026, 9:30 AM   a timestamp on a screen that already uses 12-hour clocks
Fri 14/08/2026        where the working day matters as well as the date
```

That covers admin-web tables, cards, drawers and **chart axes**; Android list
chips, card subtitles, and the timestamp burned into a proof video; and any date
string the **backend** composes for a screen, because the backend owns visible
copy.

There is deliberately **no compact variant**. A chart axis and a phone chip
render the same shape as a table cell, so a reader never has to learn a second
date format to compare two screens.

## Why this replaced the previous rule

The 2026-08-21 rule set `DD-MM-YYYY` (dashes) and a compact `dd-mm-yy` for chart
axes, and it governed **admin-web alone**. Three surfaces therefore disagreed
about the shape of one fact:

| Surface | Same drive day, before |
|---|---|
| admin-web table | `14-08-2026` |
| admin-web chart axis | `14-08-26` |
| Android chip | `14 Aug` |
| Android verify queue | whatever the DEVICE locale produced |
| backend notification copy | `14/08/2026` (`biztime.FarmDate`, already correct) |

The backend was already right. It had `FarmDateFormat = "02/01/2006"` with a
comment stating it was the form for visible copy, and 16 call sites using it.
The web and the app had each invented their own shape without reference to it.

The Android verify queue was the worst case: it used
`ofLocalizedDateTime(MEDIUM, SHORT)` with `Locale.getDefault()`, so the same
capture rendered differently on two phones and differently again on the console.
A verifier comparing a queue row against admin-web saw two shapes of one fact,
which is the cross-surface disagreement this repo bans.

## What is NOT a date, and keeps its own form

These have no day component, or are not read by a person, so `DD/MM/YYYY` is
either undefined or actively wrong for them:

- **A time on its own** — `HH:mm`, `h:mm a`, `HH:mm:ss`.
- **A month heading** — `Aug 2026`. It has no day, so it cannot be `DD/MM/YYYY`.
- **A weekday on its own** — `Mon`. A name, not a date.
- **Wire and parse formats** — ISO `YYYY-MM-DD` business dates, RFC3339 instants,
  React keys, idempotency keys, event keys, export filenames, EXIF and signed-URL
  timestamps. **Switching one of these to slashes corrupts a key or a query
  parameter.** They are not display and must never follow display.

The wire/display split is the load-bearing half of this decision. `fmtDate`,
`GoatOsDates` and `biztime.FarmDate` are for a reader; `todayIso`,
`GoatOsDates.WIRE_DATE`, and `biztime.BusinessDate` are for a machine. A change
that makes one of them call the other is a defect even when the screen looks
right.

## Where the one implementation lives, per surface

| Surface | Helper |
|---|---|
| admin-web | `apps/admin-web/lib/format.ts` — `fmtDate`, `fmtDateTime`, `dateTime` |
| admin-web chart axes | `apps/admin-web/components/svg-series.tsx` — `fmtDay` |
| Android | `core/core-common/.../datetime/GoatOsDates.kt` |
| backend visible copy | `backend/internal/platform/biztime` — `FarmDate`, `FarmDateFromBusinessDate` |

Everything is rendered in the business timezone (`Asia/Kolkata`), never the
viewer's or the device's: a business day is an IST day, and a phone set to
another zone must not shift a drive onto a different date than the console shows.

## Machine gate

`make date-format-guard` (`tools/agent-hooks/check-date-format.mjs`), in
`make guardrails` and `make ci-local`. Five checks:

1. **Web canary** — `fmtDate`/`fmtDateTime` still compose day/month/year with
   slashes. Catches a helper refactor even when no feature file changed.
2. **Web axis canary** — the axis renders `DD/MM/YYYY`, not the retired
   `dd-mm-yy`.
3. **Web bare-date scan** — a JSX text node rendering a `*_date`/`*_day`/`*_at`
   field directly, which ships the wire's ISO string to the screen.
4. **Android pattern scan** — any `ofPattern(...)`/`SimpleDateFormat(...)` whose
   pattern carries both a day and a month field must render `dd/MM/yyyy`. This is
   what catches a new screen hand-writing `d MMM`. Plus a canary on
   `GoatOsDates.DATE_PATTERN`, and one asserting `WIRE_DATE_PATTERN` stays ISO.
5. **Backend scan** — a Go layout combining a WORD month (`Jan`/`January`) with a
   day is display copy by construction and must go through `biztime.FarmDate`.
   Plus a canary on `FarmDateFormat`.

The self-test is adversarial: it asserts the guard **rejects** the retired dash
form, the retired compact axis, every retired Android pattern, and a Go
word-month layout, while **passing** wire formats, month headings and time-only
patterns.

**Escape hatch:** `date-format-guard:ignore: <reason>` on the line or in the
comment block directly above it, for a genuine machine format. The self-test
pins that an unmarked machine pattern still fails and that an unrelated comment
block is not mistaken for an exemption — so the marker is a stated
justification, not a rubber stamp.

## Stated blind spots

Named here rather than left implicit, per the guard-honesty rule:

- A date laundered through an intermediate variable or template literal before it
  reaches a JSX text node.
- Fields whose names do not end in `_date`/`_day`/`_at` (e.g. `captured`, `when`).
- Go's ISO layout `2006-01-02` is **not** flagged: it is the legitimate wire
  format in ~200 places, so flagging it would be pure noise. An ISO date leaking
  into a Go *sentence* is caught by review and by the notification-specificity
  guard, not here.
- Kotlin/TypeScript string concatenation that builds a date by hand from parts.
- Copy stored as data (seeded `admin_ui_config_entries` rows).

## Not covered by this change

The Android app's own hardcoded English strings and the per-locale
`strings.xml` files are untouched — this decision is about the *shape of a
rendered date*, not about translation.
