# Android Transitions & Motion (Mesha / Goat OS mobile)

Spatial-motion contract for the Goat OS Android app (`apps/goatos-android`, native
Kotlin + Compose). Every screen change is a **spatial move**, not decoration: the
transition tells the operator *where* a screen went so navigation stays legible.
Read alongside [`design-system.md`](design-system.md) and
[`screens.md`](screens.md).

Baseline: Material 3 motion + Jetpack Compose (Navigation Compose transitions,
`AnimatedContent`, `ModalBottomSheet`). This doc maps each M3 transition pattern
onto a concrete Goat OS surface, then records the current-code audit.

---

## The one law

**Reversibility + spatial consistency.** If a forward move slides left, Back
slides right. If a sheet rose from the bottom, it sinks to the bottom. In
Navigation Compose the `popEnterTransition` / `popExitTransition` give the reverse
for free — always define them as the mirror of `enter` / `exit`.

Unrelated screens have **no** spatial relationship, so they must NOT borrow a
directional (slide) move — they fade. Getting this wrong is the most common
motion bug: sliding between peers invents a forward/back order that does not
exist.

---

## Pattern → Goat OS surface

| M3 pattern | Motion | Goat OS use case |
|---|---|---|
| **Shared axis X** | slide left ↔ right | Drill spine: Calendar → day detail → sheds → Scan → Submit; Overdue → Reschedule; Leadership → Reschedule. Also step-through forms. |
| **Shared axis Y** | slide up ↕ down | Situational (vertical steppers). Not currently needed — do not force it. |
| **Shared axis Z** | scale / zoom | Optional: an item that opens *itself* (alert → alert detail). |
| **Fade through** | fade out → fade in | **Bottom-nav tab switches** (Calendar ↔ Overview ↔ Alerts ↔ You) and segment/filter swaps. Unrelated peers, no spatial link. |
| **Container transform** | element morphs into page | Optional upgrade: shed card → Scan, or day cell → day detail. Strongest continuity; plain shared-axis-X is a valid alternative. |
| **Bottom sheet** | rises from bottom edge | Temporary/contextual surfaces: language, sync status, leadership scope picker, data-gaps, doses-given, scan sub-sheets. Never leaves the underlying screen. |

### Decision rule

- Going **in order / deeper** (drill) → shared axis X (or container transform).
- **Unrelated** jump (top-level tab, segment) → fade through.
- **Quick contextual action** → bottom sheet.
- Shared axis Y/Z are situational; absence is not a defect.

---

## Motion tokens (M3)

| Token | Value | Where |
|---|---|---|
| Duration — drill slide | ~280–300 ms | enter/exit + pop |
| Duration — fade through | ~90 ms out, ~210 ms in (sequential) | peer swap |
| Easing — on-screen | Emphasized `CubicBezierEasing(0.2f, 0f, 0f, 1f)` | preferred over Compose default `FastOutSlowIn` |
| Easing — enter | Emphasized decelerate `CubicBezierEasing(0.05f, 0.7f, 0.1f, 1f)` | element entering |
| Easing — exit | Emphasized accelerate `CubicBezierEasing(0.3f, 0f, 0.8f, 0.15f)` | element leaving |

Never `LinearEasing` for UI. Respect the system animator scale
(`Settings.Global.ANIMATOR_DURATION_SCALE`) / reduced-motion.

---

## Implementation map (current code)

- **Global drill transition** — `AppNavHost`
  (`app/src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt`) sets shared-axis-X on
  the `NavHost`: `slideIntoContainer(Start)` + fade forward, `End` on pop.
- **Chrome** — `GoatOsShell`
  (`app/src/main/kotlin/sg/mesha/goatos/ui/GoatOsShell.kt`): M3 `NavigationBar`
  (bottom nav) + `ModalNavigationDrawer` (module switcher, EXPANDED chrome only).
  Tab taps call `navigate()` through the same `NavHost`.
- **Bottom sheets** — `ui/Overlays.kt`, `core-designsystem` `MeshaLanguageSheet`,
  `feature-scan` — all real `ModalBottomSheet`.

---

## Audit — current app vs this contract

| Surface | Contract | Code today | Verdict |
|---|---|---|---|
| Drill (Calendar/day/sheds/Scan/Submit, Overdue→Reschedule, Record) | shared axis X | `slideIntoContainer(Start)` + fade, pop `End` | Correct |
| Back reversal | mirror of forward | `popEnter/popExit` = `End` | Correct |
| Contextual surfaces (language, sync, scope, data-gaps, doses-given, scan) | bottom sheet | `ModalBottomSheet` | Correct |
| **Bottom-nav tab switch** (Calendar ↔ Overview ↔ Alerts ↔ You) | **fade through** | **shared-axis-X slide** (inherits `NavHost` transition) | **Wrong** |
| Easing | Emphasized | `tween(280)` default (`FastOutSlowIn`) | Polish |
| Predictive back (Android 14+) | opt-in | `enableOnBackInvokedCallback` not set | Missing |
| Container transform | optional | none | Opportunity |

### Defect — bottom-nav peers slide instead of fading

Top-level destinations are unrelated peers with no forward/back order. Routing
their switch through the drill `NavHost` makes them slide, inventing a false
sequence (and the slide direction is arbitrary — there is no real "back" between
tabs). M3 requires **fade through** for bottom-nav switches.

**Fix** — branch the `NavHost` transition: fade when both endpoints are
top-level routes, slide otherwise.

```kotlin
val topLevel = setOf(
    Routes.CALENDAR, Routes.VACCINATION, Routes.LEADERSHIP, Routes.ALERTS, Routes.YOU,
)
fun AnimatedContentTransitionScope<NavBackStackEntry>.isPeerSwap(): Boolean =
    initialState.destination.route in topLevel && targetState.destination.route in topLevel

NavHost(
    enterTransition = {
        if (isPeerSwap()) fadeIn(tween(210))
        else slideIntoContainer(Start, tween(280)) + fadeIn(motion)
    },
    exitTransition = {
        if (isPeerSwap()) fadeOut(tween(90))
        else slideOutOfContainer(Start, tween(280)) + fadeOut(motion)
    },
    popEnterTransition = {
        if (isPeerSwap()) fadeIn(tween(210))
        else slideIntoContainer(End, tween(280)) + fadeIn(motion)
    },
    popExitTransition = {
        if (isPeerSwap()) fadeOut(tween(90))
        else slideOutOfContainer(End, tween(280)) + fadeOut(motion)
    },
)
```

### Polish

- **Emphasized easing** — pass `CubicBezierEasing(0.2f, 0f, 0f, 1f)` to the drill
  tweens instead of the Compose default, for the M3 "premium" feel.
- **Predictive back** — set `android:enableOnBackInvokedCallback="true"` in the
  manifest so the Android 14+ back gesture previews the reverse transition.

### Opportunity (optional)

- **Container transform** on shed card → Scan (or day cell → day detail) for
  stronger visual continuity. Not required; shared-axis-X remains valid.

---

## What NOT to do

- Do not slide between unrelated top-level tabs (see defect above).
- Do not fade a drill step — drill is the app's spatial spine and must stay
  directional (X).
- Do not hand-roll sheet offsets — use `ModalBottomSheet` (drag handle, detents,
  scrim, swipe-dismiss are built in).
- Do not force shared axis Y/Z where the relationship does not call for it.
