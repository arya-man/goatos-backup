package sg.mesha.goatos.core.model.nav

/**
 * Backend-driven navigation contract — mirrors the backend `/app/bootstrap`
 * fields (`nav_chrome`, `visible_navigation`, `owned_modules`) shipped in the Go
 * workforce bootstrap.
 *
 * TRD §14 dumb-renderer rule: the app RENDERS this. It never counts modules,
 * never checks role, and never decides chrome locally. The backend already
 * computed [NavChrome] (drawer/sidebar iff the principal owns >=2 visible
 * modules, else bottom-bar-only). The client only reacts to the value.
 */
enum class NavChrome {
    /** Show the module switcher (drawer/sidebar): principal owns >=2 visible modules. */
    EXPANDED,

    /** Bottom-bar only, no drawer: single/zero-module principal. Drawer extras
     *  (language, RFID reader, notifications, sign out) fold into You/Settings. */
    MINIMAL,
}

/** A product module the principal owns via their HR department (owned_modules). */
data class OwnedModule(
    val vertical: String,
    val module: String,
)

/** A backend-computed nav destination (mirrors BootstrapNavigationItem). */
data class NavItem(
    val key: String,
    val label: String,
    val href: String,
)

/**
 * The navigation state the shell renders, straight from the bootstrap. There is
 * no client-side policy here — [chrome], [items], and [ownedModules] are all
 * backend truth.
 */
data class NavState(
    val chrome: NavChrome,
    val items: List<NavItem>,
    val ownedModules: List<OwnedModule>,
) {
    companion object {
        /** Safe empty state before the first bootstrap resolves. */
        val Empty = NavState(chrome = NavChrome.MINIMAL, items = emptyList(), ownedModules = emptyList())
    }
}
