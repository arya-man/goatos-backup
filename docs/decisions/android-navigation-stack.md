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
- A drill target must have a distinct child route even when it renders the same
  feature content as a root module. Reusing an L0 route for a drill is forbidden.
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
membership, Calendar uses a dedicated hosted drive route for blank/generic
targets, and `TopLevelChromeTest` covers roots, hosted children, prefix
collisions, and route fallbacks.

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
