package sg.mesha.goatos.core.network

import sg.mesha.goatos.core.model.nav.NavChrome
import sg.mesha.goatos.core.model.nav.NavItem
import sg.mesha.goatos.core.model.nav.NavState
import sg.mesha.goatos.core.model.nav.OwnedModule

// Wire DTOs mirroring the backend GET /app/bootstrap (nav_chrome + visible_navigation
// + owned_modules). @Serializable + the Retrofit/OpenAPI-generated client are added
// in the network pass; the DTOs stay the same shape.
data class BootstrapDto(
    val navChrome: String, // "expanded" | "minimal"
    val visibleNavigation: List<NavItemDto>,
    val ownedModules: List<OwnedModuleDto>,
)

data class NavItemDto(val key: String, val label: String, val href: String)

data class OwnedModuleDto(val vertical: String, val module: String)

/** App API port. The real Retrofit/OpenAPI adapter lands behind this interface. */
interface AppApi {
    suspend fun bootstrap(): BootstrapDto
}

/**
 * Canned bootstrap so the shell renders the backend-driven nav end-to-end before
 * auth/network are wired. `chrome` lets previews exercise both drawer + bottom-bar
 * states. This is test/dev scaffolding, NOT product truth.
 */
class FakeAppApi(private val chrome: String = "expanded") : AppApi {
    override suspend fun bootstrap(): BootstrapDto = BootstrapDto(
        navChrome = chrome,
        visibleNavigation = listOf(
            NavItemDto(key = "calendar", label = "Calendar", href = "/calendar"),
            NavItemDto(key = "vaccination", label = "Vaccination", href = "/vaccination"),
        ),
        ownedModules = listOf(OwnedModuleDto(vertical = "preventive_care", module = "pc.vaccination")),
    )
}

/**
 * DTO -> domain. The backend already computed the chrome (drawer iff >=2 owned
 * visible modules); the client only parses the enum. It NEVER recomputes chrome
 * from the module count (TRD §14 dumb-renderer).
 */
fun BootstrapDto.toNavState(): NavState = NavState(
    chrome = if (navChrome.equals("expanded", ignoreCase = true)) NavChrome.EXPANDED else NavChrome.MINIMAL,
    items = visibleNavigation.map { NavItem(key = it.key, label = it.label, href = it.href) },
    ownedModules = ownedModules.map { OwnedModule(vertical = it.vertical, module = it.module) },
)
