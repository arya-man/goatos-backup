package sg.mesha.goatos.core.network

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/**
 * Runs the REAL mobile network stack (RetrofitAppApi + kotlinx.serialization DTOs +
 * bearer interceptor) against a live backend, proving the app actually links: the
 * responses deserialize into the DTOs the screens map from.
 *
 * Skipped unless GOATOS_TEST_BEARER (a valid bearer token) is set — so CI without a
 * backend stays green. Run locally with:
 *   GOATOS_TEST_BEARER=<token> ./gradlew :core:core-network:testDebugUnitTest
 * (base URL defaults to http://127.0.0.1:8080/ — the host, not the emulator alias).
 */
class RealBackendLinkTest {

    @Test
    fun mobileNetworkStackLinksToLiveBackend() {
        val token = System.getenv("GOATOS_TEST_BEARER").orEmpty()
        assumeTrue("set GOATOS_TEST_BEARER + run the backend on :8080", token.isNotBlank())
        val baseUrl = System.getenv("GOATOS_TEST_BASE_URL") ?: "http://127.0.0.1:8080/"

        val api = NetworkFactory.appApi(baseUrl) { token }

        runBlocking {
            val boot = api.bootstrap()
            assertTrue("bootstrap nav_chrome should be present", boot.navChrome.isNotBlank())

            val execution = api.listVaccinationExecution()
            assertTrue("execution source should be api", execution.source == "api")

            val controlTower = api.getVaccinationControlTower()
            assertTrue("control-tower source should be api", controlTower.source == "api")

            val calendar = api.listCalendarVaccinationEvents()
            assertTrue("calendar source should be api", calendar.source == "api")

            // HRMS roster (Timetable + coverage banner) — read-only GETs only (TRD §14),
            // operator-scoped (never the admin `/admin/roster` surface, which 403s for
            // operators). my-coverage takes no params, so it needs no center-id fixture.
            val coverage = api.getMyCoverage()
            assertTrue("coverage trace_id should be present", coverage.traceId.isNotBlank())

            println(
                "MOBILE↔BACKEND LINK OK — nav_chrome=${boot.navChrome} " +
                    "execRows=${execution.rows.size} ctAlerts=${controlTower.alerts.size} " +
                    "calendarEvents=${calendar.items.size} hasCoverage=${coverage.coverage.hasCoverage}",
            )
        }
    }
}
