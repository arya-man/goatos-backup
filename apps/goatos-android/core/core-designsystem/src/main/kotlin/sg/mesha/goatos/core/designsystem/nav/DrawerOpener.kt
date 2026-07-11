package sg.mesha.goatos.core.designsystem.nav

import androidx.compose.runtime.staticCompositionLocalOf

/**
 * Opens the app shell's module drawer. Provided by the shell (`GoatOsShellChrome`) on
 * top-level routes and consumed by any top-level feature screen's menu (hamburger)
 * button. Lives in core-designsystem so feature modules can consume it without an
 * (illegal) dependency on the app module. Default no-op keeps previews/tests safe and
 * makes the button inert on detail screens where no drawer is provided.
 */
val LocalDrawerOpener = staticCompositionLocalOf<() -> Unit> { {} }
