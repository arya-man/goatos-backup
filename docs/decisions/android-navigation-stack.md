# Android Navigation Stack And L0 Chrome

Status: accepted.

## Decision

The Android app has one navigation stack. A backend bootstrap navigation item is
an **L0 root**. Tapping content on an L0 screen pushes an **L1 hosted
destination** into the same `NavHost`; every further drill pushes L2/L3/L4 in
order.

Only an exact L0 route owns global navigation chrome:

```text
L0 root                     bottom bar/drawer visible
L1/L2/L3/L4 hosted child    Up/Back visible; bottom bar/drawer absent
temporary context/action    ModalBottomSheet over the current destination
```

Back pops one destination. Up follows the same local hierarchy. When the stack
returns to L0, the root chrome is restored with its saved state.

## Required implementation shape

- Root ownership is exact set membership against backend-composed bootstrap
  routes. Prefix, substring, and ancestor matching are forbidden.
- **Global chrome is shell-derived, never screen-authored.** The shell computes
  drawer availability once (`drawerAvailable` = EXPANDED chrome AND exact L0
  membership) and publishes it as `LocalDrawerOpener`, which is `null` on every
  L1+ drill. Screens render `MeshaScreenHeader`, whose leading slot resolves to
  the module drawer when an opener is present and to Up/Back otherwise. A screen
  must not read `LocalDrawerOpener` or draw its own `MeshaIcons.Menu` button.
  This was originally opt-in per screen, and the predictable happened: only two
  of eight L0 roots drew a hamburger, so the Counts module shipped with no way
  back to another module and Vaccination's own root tab showed a Back arrow. A
  screen hosted at both an L0 route and a drill route (Sheds at `/vaccination`
  and `/calendar/drive`) passes `onBack` and gets the right affordance at each
  without a route check.
- A drill target must have a distinct child route even when it renders the same
  feature content as a root module. Reusing an L0 route for a drill is forbidden.
- Calendar drill fallback must use `Routes.calendarDriveRoute(fallbackDateKey)`,
  not the bare `/calendar/drive` route, when the tapped day supplies a date key.
  Backend rows can legitimately have a blank/unknown target while the user still
  drilled into a specific future business date; dropping that date lets the drive
  screen auto-select today or the first available day and recreates stale-feeling
  Calendar behavior.
- Structural detail screens fill the `NavHost`; they must not be styled as
  modal sheets. `ModalBottomSheet` remains correct for temporary filters,
  pickers, confirmations, and contextual actions that leave the underlying
  destination in place.
- Screen composables emit navigation events. The app `NavHost` owns route
  changes, and all drill transitions use the shared-axis-X forward/reverse
  contract in `docs/mobile/transitions-and-motion.md`.
- Bottom-tab navigation may restore the selected root state, but drill
  navigation must push normally and preserve the back stack.

## Regression gate

`make android-navigation-stack-guard` fails unless the shell uses exact L0
membership, the drawer opener is gated on `hasDrawer && isTopLevel`, no feature
module hand-rolls a drawer affordance (`MeshaIcons.Menu` or a direct
`LocalDrawerOpener` read), Calendar uses a dedicated hosted drive route for
blank/generic targets and preserves a supplied fallback date key, and
`TopLevelChromeTest` covers roots, hosted children, prefix collisions, dated
route fallbacks, and drawer availability across every module's roots.

The Android CI job also runs the compiled JVM unit suite, so the same regression
test must compile and pass on every Android or shared-contract change. Device
proof for navigation work must additionally click the real L0 → L1 → L2 path
and verify:

```text
L0 -> chrome present
L1 -> chrome absent
L2 -> chrome absent
Back -> L1, chrome absent
Back -> L0, chrome restored
```

Do not label two L0 variants as L1/L2 evidence. Do not fabricate deeper
execution screens when the live role/data has no assigned task; report the
honest reachable boundary.
