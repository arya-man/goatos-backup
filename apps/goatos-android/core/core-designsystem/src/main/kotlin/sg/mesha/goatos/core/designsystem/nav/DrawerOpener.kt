package sg.mesha.goatos.core.designsystem.nav

import androidx.compose.runtime.staticCompositionLocalOf

/**
 * Opens the app shell's module drawer — or `null` when the destination currently on screen
 * has no drawer to open.
 *
 * The shell (`GoatOsShellChrome`) is the ONLY producer, and it decides purely from the
 * backend-composed nav: an opener is provided when chrome is EXPANDED **and** the current
 * route is an exact L0 root (`isTopLevelRoute`), and `null` on every L1+ drill and on
 * MINIMAL (single-module) chrome. Screens never decide this, which is the whole point —
 * a new module cannot "forget" its drawer, and a drill cannot accidentally grow one.
 *
 * Consumers should not read this directly to hand-roll a hamburger. Render
 * [sg.mesha.goatos.core.designsystem.component.MeshaScreenHeader] instead: it resolves the
 * leading affordance from this value (drawer on L0, Up/Back on a drill) and carries the
 * accessibility label. `make android-navigation-stack-guard` fails a feature module that
 * draws its own `MeshaIcons.Menu` button.
 *
 * Lives in core-designsystem so feature modules can consume it without an (illegal)
 * dependency on the app module. The `null` default keeps previews and Paparazzi tests safe:
 * a screen rendered outside the shell simply shows no drawer affordance.
 */
val LocalDrawerOpener = staticCompositionLocalOf<(() -> Unit)?> { null }
