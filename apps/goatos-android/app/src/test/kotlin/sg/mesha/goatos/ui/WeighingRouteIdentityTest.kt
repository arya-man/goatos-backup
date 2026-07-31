package sg.mesha.goatos.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Path
import kotlin.io.path.readText

class WeighingRouteIdentityTest {
    @Test
    fun `weighing scan route carries campaign work group and selected shed identity`() {
        val route = Routes.weighingScanRoute(
            campaignId = "campaign A",
            workGroupId = "group B",
            campaignShedId = "shed C",
            category = "per_shed_partition",
            tenantId = "tenant T",
            expectedLocationId = "location L",
            expectedLocationLabel = "Gandhi 1 - Part 3",
            scanTitle = "Gandhi 1 - Part 3",
        )

        assertTrue(route.startsWith("/weighing/scan?"))
        assertTrue(route.contains("campaignId=campaign%20A"))
        assertTrue(route.contains("workGroupId=group%20B"))
        assertTrue(route.contains("campaignShedId=shed%20C"))
        assertTrue(route.contains("weighingCategory=per_shed_partition"))
        assertTrue(route.contains("tenantId=tenant%20T"))
        assertTrue(route.contains("expectedLocationId=location%20L"))
        assertTrue(route.contains("expectedLocationLabel=Gandhi%201%20-%20Part%203"))
        assertTrue(route.contains("scanTitle=Gandhi%201%20-%20Part%203"))
    }

    @Test
    fun `RFID completion key swallowing is scoped to active weighing scan route`() {
        assertTrue(routeAcceptsWeighingRfid("/weighing/scan?campaignId=c&workGroupId=g&campaignShedId=s"))
        assertTrue(routeAcceptsWeighingRfid("/weighing/scan?weighingCategory=individual_animal"))
        assertFalse(routeAcceptsWeighingRfid("/weighing/scan?weighingCategory=per_shed_partition"))
        assertFalse(routeAcceptsWeighingRfid("/weighing/scan?weighingCategory=%20Per%20Shed%20Partition%20"))
        assertFalse(routeAcceptsWeighingRfid("/weighing"))
        assertFalse(routeAcceptsWeighingRfid("/scan?shedId=shed-1"))
        assertFalse(routeAcceptsWeighingRfid(null))
    }

    @Test
    fun `weighing routes are registered as real navigation destinations`() {
        val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()

        assertTrue(navHost.contains("composable(Routes.WEIGHING)"))
        assertTrue(navHost.contains("route = \"${'$'}{Routes.WEIGHING_SCAN}?"))
        assertTrue(navHost.contains("WeighingScreen("))
        assertTrue(navHost.contains("vm.setCaptureActive(rfidCaptureEnabled)"))
        assertTrue(navHost.contains("vm.setCompletionKeySwallowActive(rfidCaptureEnabled)"))
        assertFalse(navHost.contains(") { launchSingleTop = true }\n                    }\n                },"))
    }

    // The guarantee is unchanged: an operator who may execute weighing must land on the
    // execution screen, never the leadership read-only one. What changed is WHERE that
    // answer comes from.
    //
    // This used to assert the shell contained `(isOperatorProfile && hasWeighingModule)`,
    // where isOperatorProfile was `profile.roleLabel.substringBefore("·") == "operator"`
    // — an authorization decision parsed out of a DISPLAY label. AGENTS.md bans inferring
    // roles from name strings, and it is genuinely fragile: retitling or translating the
    // label silently grants or revokes execution.
    //
    // The backend already computes this from real grant + module truth
    // (workforce/app/bootstrap_copy.go canExecuteWeighing = WeighingExecute permission AND
    // the weighing module granted) and ships it as the `weighing_execute` bootstrap flag.
    @Test
    fun `operator weighing execution is decided by the backend flag, never by a role label`() {
        val shell = Path.of("src/main/kotlin/sg/mesha/goatos/ui/GoatOsShell.kt").readText()

        assertTrue(
            "weighing execution must be gated on the backend-owned weighing_execute flag",
            shell.contains("featureFlags[\"weighing_execute\"]"),
        )
        assertFalse(
            "weighing execution must not be inferred from the display role label",
            shell.contains("isOperatorProfile"),
        )
        assertFalse(
            "weighing execution must not fall back to a locally inferred module check",
            shell.contains("(isOperatorProfile && hasWeighingModule)"),
        )
    }

    @Test
    fun `operator weighing work list does not render stale week strip`() {
        val screen = Path.of("src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingScreen.kt").readText()
        val operatorListBlock = screen.substringAfter("if (!state.plannerMode && !state.hasScope && state.assignments.isNotEmpty())")
            .substringBefore("if (state.assignmentsLoadingMore)")

        assertFalse(operatorListBlock.contains("WeekPlanStrip("))
        assertFalse(operatorListBlock.contains("plannerDayTabs"))
    }
}
