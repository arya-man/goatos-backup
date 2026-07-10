package sg.mesha.goatos.core.model.nav

/**
 * Backend-driven navigation contract — mirrors the backend `/app/bootstrap`
 * fields (`nav_chrome`, `visible_navigation`) shipped in the Go workforce
 * bootstrap.
 *
 * TRD §14 dumb-renderer rule: the app RENDERS this. It never counts modules,
 * never checks role, and never decides chrome locally. The backend already
 * computed [NavChrome]. The client only reacts to the value.
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

/**
 * The navigation state the shell renders, straight from the bootstrap. There is
 * no client-side policy here — [chrome] and [items] are backend truth.
 */
data class NavState(
    val chrome: NavChrome,
    val items: List<NavItem>,
) {
    companion object {
        /** Safe empty state before the first bootstrap resolves. */
        val Empty = NavState(chrome = NavChrome.MINIMAL, items = emptyList())
    }
}
