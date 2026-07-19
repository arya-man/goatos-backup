package sg.mesha.goatos.core.model.nav

/**
 * Backend-driven navigation contract — mirrors the backend `/app/bootstrap`
 * fields (`nav_chrome`, `visible_navigation`, `modules`) shipped in the Go
 * workforce bootstrap.
 *
 * TRD §14 dumb-renderer rule: the app RENDERS this. It never counts modules,
 * never checks role, and never decides chrome locally. The backend already
 * computed [NavChrome], the drawer's [NavState.modules], and each module's
 * bottom-bar items. The client only reacts to the values.
 *
 * Labels arrive ALREADY LOCALIZED (en/hi/kn/te) from `bootstrap_copy.go`; render
 * them verbatim and never re-translate a backend label client-side.
 */
enum class NavChrome {
    /** Show the module switcher (drawer/sidebar). */
    EXPANDED,

    /** Bottom-bar only, no drawer: single/zero-module principal. Drawer extras
     *  (language, RFID reader, notifications, sign out) fold into You/Settings. */
    MINIMAL,
}

/** A backend-computed nav destination (mirrors BootstrapNavigationItem). */
data class NavItem(
    val key: String,
    val label: String,
    val href: String,
)

/** Whether a module is built and enterable, or advertised roadmap (mirrors BootstrapModule.Status). */
enum class NavModuleStatus {
    /** Built: the drawer row is tappable and owns a bottom bar. */
    AVAILABLE,

    /** Declared but not built: the drawer row renders disabled with a "Soon" badge. */
    SOON;

    companion object {
        /** Parses the backend's `status` string. Anything unknown degrades to [SOON] so an
         *  unrecognised future status can never make an unbuilt module tappable. */
        fun from(raw: String): NavModuleStatus =
            if (raw.equals("available", ignoreCase = true)) AVAILABLE else SOON
    }
}

/**
 * A drawer entry: the module's identity plus the bottom-bar items it contributes
 * (mirrors BootstrapModule).
 *
 * [navItems] is MODULE-SCOPED — selecting this module in the drawer swaps the bottom
 * bar to these items and navigates to [href]. [NavModuleStatus.SOON] modules carry no
 * items and no href. See `docs/decisions/role-module-nav-composition.md`.
 */
data class NavModule(
    val key: String,
    val label: String,
    val href: String,
    val status: NavModuleStatus,
    val navItems: List<NavItem>,
)

/**
 * The navigation state the shell renders, straight from the bootstrap. There is
 * no client-side policy here — [chrome], [items], and [modules] are backend truth.
 *
 * [items] is the DEFAULT module's bar (what the backend picked as the landing module);
 * [modules] carries every drawer row with its own bar, so switching modules is a local
 * selection over backend-composed data rather than a second network call.
 */
data class NavState(
    val chrome: NavChrome,
    val items: List<NavItem>,
    val modules: List<NavModule> = emptyList(),
) {
    companion object {
        /** Safe empty state before the first bootstrap resolves. */
        val Empty = NavState(chrome = NavChrome.MINIMAL, items = emptyList(), modules = emptyList())
    }
}

/** Drawer rows the principal can actually enter. */
fun NavState.availableModules(): List<NavModule> =
    modules.filter { it.status == NavModuleStatus.AVAILABLE && it.navItems.isNotEmpty() }

/**
 * Resolves which module the shell is currently in, WITHOUT any hardcoded module list.
 *
 * Precedence, most-specific first:
 *  1. the explicitly selected module, when it owns [currentRoute] — so a route shared by
 *     several modules (Calendar, Alerts) keeps the bar the operator chose;
 *  2. whichever module owns [currentRoute] — so a push/deep-link into another module's
 *     screen still renders that module's bar instead of a stale one;
 *  3. the explicit selection, when it is still a valid available module;
 *  4. the backend's first available module (the default landing module).
 *
 * Returns null when the payload carries no enterable module, in which case the caller
 * falls back to [NavState.items].
 */
fun NavState.resolveModule(selectedKey: String?, currentRoute: String?): NavModule? {
    val available = availableModules()
    val selected = available.firstOrNull { it.key == selectedKey }
    if (selected != null && selected.navItems.any { it.href == currentRoute }) return selected
    val owningRoute = available.firstOrNull { module -> module.navItems.any { it.href == currentRoute } }
    return owningRoute ?: selected ?: available.firstOrNull()
}

/**
 * The bottom-bar items for the resolved module. Falls back to [NavState.items] when the
 * bootstrap carries no modules — an older backend or a cached pre-`modules` bootstrap must
 * still render its bar rather than an empty one.
 */
fun NavState.barItems(selectedKey: String?, currentRoute: String?): List<NavItem> =
    resolveModule(selectedKey, currentRoute)?.navItems ?: items
