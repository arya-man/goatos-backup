package sg.mesha.goatos.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.weighing.WeighingTaskShedUiRow
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

    @Test
    fun `cancelled weighing video capture is not reported as a Crashlytics capture failure`() {
        val viewModel = Path.of("src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt").readText()
        val cancelledBranch = viewModel.substringAfter("if (proof.message == \"missing_video\")")
            .substringBefore("if (proof.message != \"missing_video\")")

        assertTrue(cancelledBranch.contains("AnalyticsEvents.WEIGHING_PROOF_CAPTURE_CANCELLED"))
        assertFalse(
            "missing_video is camera cancel / no recording state; it must not become a Crashlytics non-fatal",
            cancelledBranch.contains("reportCaptureFailure("),
        )
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

    // The assignment is the reason a weighing task exists, and the task detail screen is where the
    // planner reads it back. Carrying the operator name all the way onto `WeighingTaskShedUiRow`
    // and then not drawing it left four identically-shaped cards whose only difference -- who is
    // doing them -- was recoverable solely by tapping a filter chip and watching the list shrink.
    @Test
    fun `each task detail shed card renders the operator it is assigned to`() {
        val card = taskDetailScreen().substringAfter("private fun TaskShedCard(")
            .substringBefore("private fun TaskBanner(")

        assertTrue(
            "the shed card must render the operator name the row already carries",
            card.contains("R.string.weighing_task_shed_operator_fmt, row.operatorLabel"),
        )
        // Farm-language chrome, not a raw id and not an invented name.
        assertFalse("a user id must never be rendered on the card", card.contains("operatorUserId"))
    }

    // The chip counts SHED BUCKETS; the card body underneath it ("Nothing captured yet") is about
    // animals. A bare "Dinakar 2" put two different units on one screen with neither of them
    // labelled, so the count reaches the screen as a number and the screen names the unit.
    @Test
    fun `task detail operator chips name the unit they count`() {
        val screen = taskDetailScreen()

        // Asserts the PROPERTY (name and a counted noun, composed at render time), not one
        // spelling of it. The chip was built two ways on two branches; the surviving form
        // composes `row.name` with `shedNoun(row.shedCount)` directly rather than via a format
        // resource. Both satisfy the rule this test exists for: the number never appears without
        // its unit.
        assertTrue(
            "the chip must render the operator name and the shed noun as separate parts",
            screen.contains("shedNoun(row.shedCount)") || screen.contains("shedNoun(filter.shedCount)"),
        )
        assertTrue(
            "the chip must name the operator alongside that counted noun",
            screen.contains("row.name") || screen.contains("R.string.weighing_task_operator_chip_fmt"),
        )
        assertFalse(
            "the count must not be concatenated into the operator label without its unit",
            screen.contains("\${filter.operatorLabel} \${filter.shedCount}") ||
                screen.contains("\${row.name} \${row.shedCount}"),
        )

        val viewModel = Path.of("src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt").readText()
        assertTrue(
            "the ViewModel must hand the count over as a number, leaving the noun to the screen",
            viewModel.contains("shedCount = rows.size"),
        )
        assertFalse(
            "the ViewModel must not pre-bake an unlabelled count into the chip label",
            viewModel.contains("\${labelFor(operatorUserId, rows)} \${rows.size}"),
        )
    }

    // Publish / end are the actions this task is FOR and stay above the work. What remains below
    // it is the one secondary action that actually does something -- "Repeat this task on another
    // date" -- which asked the reader to consider repeating a task before they had seen a single
    // shed in it.
    @Test
    fun `task detail secondary actions sit below the shed list, not above it`() {
        val screen = taskDetailScreen()
        val shedList = screen.indexOf("key = { index -> state.sheds[index].uiKey }")

        assertTrue("the shed list must still be rendered", shedList > 0)
        assertTrue("Repeat must come after the shed list", screen.indexOf("key = \"task-repeat\"") > shedList)
        // "Edit sheds & assignment" and "Reopen task" are GONE, not merely moved below the list
        // (maintainer decision 2026-08-03). Neither had a screen behind it, so onClick was always
        // null and the pair rendered as two permanently dead cards of prose that pushed the one
        // real action off the fold. A control that can never be pressed is decoration, not a
        // disabled control. Reopening still lives on each shed's own card, which is where its
        // grain is. Reinstate either one only WITH the screen that performs it.
        // Edit is BACK, and legitimately: the 2026-08-03 removal was conditional -- "reinstate
        // either one only WITH the screen that performs it". On 2026-08-05 it got that screen:
        // "Edit task" opens the existing plan wizard pre-hydrated from this campaign (date and
        // park locked, buckets/mode/operator editable) and saves through
        // WeighingRepository.updatePlan. It is a live control, not prose, so the ban no longer
        // applies to it -- but the ORDERING rule still does, and Reopen is still banned because
        // it still has no screen.
        assertTrue("Edit is reinstated with its wizard, so it must be rendered", screen.contains("key = \"task-edit\""))
        assertTrue("Edit must come after the shed list", screen.indexOf("key = \"task-edit\"") > shedList)
        assertFalse("Reopen must not return without a screen behind it", screen.contains("key = \"task-reopen\""))
        // The filter chips still belong ABOVE the list they filter.
        assertTrue("operator chips must stay above the shed list", screen.indexOf("key = \"operator-filters\"") < shedList)
    }

    // Weighing lives in its own feature module, so these guards read across a module boundary.
    // A NoSuchFileException means the screen moved -- update the path rather than deleting the
    // guard, or the assignment silently disappears off the card again.
    private fun taskDetailScreen(): String =
        Path.of("../feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingTaskDetailScreen.kt").readText()

    @Test
    fun `operator weighing work list does not render stale week strip`() {
        // Weighing now lives in its own feature module, so this guard reads across module
        // boundaries. A NoSuchFileException here means the screen moved again -- update the path
        // rather than deleting the guard, or the stale-week-strip regression silently returns.
        val screen = Path.of("../feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingScreen.kt").readText()
        val operatorListBlock = screen.substringAfter("if (!state.plannerMode && !state.hasScope && state.assignments.isNotEmpty())")
            .substringBefore("if (state.assignmentsLoadingMore)")

        assertFalse(operatorListBlock.contains("WeekPlanStrip("))
        assertFalse(operatorListBlock.contains("plannerDayTabs"))
    }

    @Test
    fun `weighing repeated lists and paging use full assignment identity`() {
        val files = listOf(
            "../feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingScreen.kt",
            "../feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingOperatorsScreen.kt",
            "../feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/WeighingTaskDetailScreen.kt",
        )

        files.forEach { file ->
            val source = Path.of(file).readText()
            val shedIdOnly = "campaign" + "ShedId"
            assertFalse(
                "$file must key repeated weighing rows by full work/category identity, not campaignShedId alone",
                source.contains("key = { _, row -> row.$shedIdOnly }") ||
                    source.contains("key = { _, assignment -> assignment.$shedIdOnly }") ||
                    source.contains("key = { index -> \"shed-\${state.sheds[index].$shedIdOnly}\" }") ||
                    source.contains("key = { it.$shedIdOnly }"),
            )
        }

        val taskDetail = taskDetailScreen()
        assertTrue(
            "task-detail shed cards must carry a stable full-grain UI key",
            taskDetail.contains("val uiKey: String") &&
                taskDetail.contains("campaignId, campaignShedId, locationId, category"),
        )
        assertFalse(
            "task-detail shed cards must not key by campaignShedId alone",
            taskDetail.contains("key = { index -> \"shed-\${state.sheds[index].${"campaign"}ShedId}\" }"),
        )

        val viewModel = Path.of("src/main/kotlin/sg/mesha/goatos/viewmodel/WeighingViewModel.kt").readText()
        assertTrue(viewModel.contains("assignmentIdentityKey()"))
        assertFalse(viewModel.contains("known = assignments.value.map { it.campaignShedId }.toSet()"))
    }

    @Test
    fun `task detail shed row key separates duplicate backend bucket ids by rendered grain`() {
        val duplicateCrashlyticsBucketId = "257a1f50-50f3-554a-a1a4-d99e829a1ac4"
        val rows = listOf(
            taskShedRow(
                campaignShedId = duplicateCrashlyticsBucketId,
                locationId = "gandhi-parent",
                category = "individual_animal",
            ),
            taskShedRow(
                campaignShedId = duplicateCrashlyticsBucketId,
                locationId = "gandhi-parent",
                category = "per_shed_partition",
            ),
        )

        assertTrue(
            "task detail LazyColumn keys must remain unique when one backend bucket appears at two rendered grains",
            rows.map { it.uiKey }.toSet().size == rows.size,
        )
    }

    private fun taskShedRow(
        campaignShedId: String,
        locationId: String,
        category: String,
    ): WeighingTaskShedUiRow = WeighingTaskShedUiRow(
        campaignId = "campaign-1",
        campaignShedId = campaignShedId,
        tenantId = "tenant-1",
        locationId = locationId,
        shedName = "Gandhi",
        category = category,
        operatorLabel = "Amit Kumar",
        status = "in_progress",
        reworked = false,
        animalsWeighedCount = 0,
        animalsSubmittedCount = 0,
        ladderStep = 0,
        canReopen = false,
    )
}
