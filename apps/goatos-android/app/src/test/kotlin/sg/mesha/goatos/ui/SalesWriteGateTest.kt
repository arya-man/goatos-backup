package sg.mesha.goatos.ui

import java.nio.file.Path
import kotlin.io.path.readText
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.feature.counts.WorkflowActionSection
import sg.mesha.goatos.feature.counts.WorkflowActionUi
import sg.mesha.goatos.feature.counts.WorkflowDetailUiState
import sg.mesha.goatos.feature.counts.WorkflowStatusTone

/**
 * PR #445 review: `sales_write=false` hid the Record sale button but the hosted routes still
 * mounted the full write flows -- `/sales/new` rendered Save, `/sales/sale/{id}/tag` its review and
 * confirm, and the sale workflow's tagging step navigated straight into it. Each ended in a server
 * 403 / a dead outbox row. One gate ([SalesWriteAccess]) now decides, and every entry honours it.
 */
class SalesWriteGateTest {
    private val readOnly = SalesWriteAccess.fromFeatureFlags(mapOf("sales_write" to false, "sales_tag" to false))
    private val writer = SalesWriteAccess.fromFeatureFlags(mapOf("sales_write" to true, "sales_tag" to true))

    @Test
    fun `the flags decide, and an absent flag keeps the write offered`() {
        assertEquals(SalesWriteAccess(canRecord = false, canTag = false), readOnly)
        assertEquals(SalesWriteAccess(canRecord = true, canTag = true), writer)
        assertEquals(SalesWriteAccess(canRecord = true, canTag = true), SalesWriteAccess.fromFeatureFlags(emptyMap()))
        // Two authorities: recording without tagging, and tagging without recording, are both real.
        assertEquals(SalesWriteAccess(canRecord = false, canTag = true), SalesWriteAccess.fromFeatureFlags(mapOf("sales_write" to false, "sales_tag" to true)))
    }

    @Test
    fun `record sale and tag animals are write entries, matched exactly`() {
        assertEquals(SalesWriteEntry.RECORD, salesWriteEntryFor(Routes.SALE_NEW))
        assertEquals(SalesWriteEntry.TAG, salesWriteEntryFor(Routes.SALE_TAG_ANIMALS))
        assertNull(salesWriteEntryFor(Routes.SALE_DETAIL))
        assertNull(salesWriteEntryFor(Routes.SALES))
        assertFalse(readOnly.allows(SalesWriteEntry.RECORD))
        assertFalse(readOnly.allows(SalesWriteEntry.TAG))
        assertTrue(writer.allows(SalesWriteEntry.RECORD))
        assertTrue(writer.allows(SalesWriteEntry.TAG))
    }

    @Test
    fun `the workflow tagging step stays on the card read-only for a person who may not tag`() {
        val state = WorkflowDetailUiState(saleDealId = "deal-1", actions = listOf(tagStep(), otherStep()))
        val gated = state.withSaleTaggingFor(readOnly)
        assertFalse(gated.actions.first().opensSaleTagging)
        assertEquals("Tag the animals sold", gated.actions.first().title)
        assertEquals(state.actions.last(), gated.actions.last())
        // A person who may tag keeps the step exactly as it was.
        assertEquals(state, state.withSaleTaggingFor(writer))
    }

    // --- The routes themselves honour the gate (a deep link must not mount the form) ------------

    private val navHost = Path.of("src/main/kotlin/sg/mesha/goatos/ui/AppNavHost.kt").readText()

    private fun routeBlock(marker: String, screenCall: String): String {
        assertTrue("route $marker not found", navHost.contains(marker))
        return navHost.substringAfter(marker).substringBefore(screenCall)
    }

    @Test
    fun `record sale route mounts no form for a person who may not record`() {
        val block = routeBlock("composable(Routes.SALE_NEW)", "SaleCreateScreen(")
        assertTrue("SALE_NEW mounts SaleCreateScreen without the sales gate", block.contains("salesAccess.allows(SalesWriteEntry.RECORD)"))
        assertTrue("SALE_NEW has no read-only state", block.contains("SalesWriteUnavailableScreen("))
        assertTrue("the SaleCreateViewModel is created before the gate", !block.substringBefore("salesAccess.allows").contains("hiltViewModel"))
    }

    @Test
    fun `tag animals route mounts no review or confirm for a person who may not tag`() {
        val block = routeBlock("route = Routes.SALE_TAG_ANIMALS,", "SaleTagAnimalsScreen(")
        assertTrue("SALE_TAG_ANIMALS mounts tagging without the sales gate", block.contains("salesAccess.allows(SalesWriteEntry.TAG)"))
        assertTrue("SALE_TAG_ANIMALS has no read-only state", block.contains("SalesWriteUnavailableScreen("))
        assertTrue("the SaleTagAnimalsViewModel is created before the gate", !block.substringBefore("salesAccess.allows").contains("hiltViewModel"))
    }

    @Test
    fun `the workflow tagging step and the sale detail follow the tag gate`() {
        val workflow = navHost.substringAfter("is WorkflowDetailEvent.OpenSaleTagging ->").substringBefore("is WorkflowDetailEvent.OpenPromote")
        assertTrue("OpenSaleTagging navigates without the tag gate", workflow.contains("salesAccess.canTag"))
        assertTrue("WorkflowDetailScreen renders the tagging step ungated", navHost.contains("WorkflowDetailScreen(state = state.withSaleTaggingFor(salesAccess)"))
        val detail = routeBlock("route = Routes.SALE_DETAIL,", "onEvent =")
        assertTrue("sale detail offers Tag animals without the tag gate", detail.contains("canTagAnimals = state.canTagAnimals && salesAccess.canTag"))
    }

    private fun tagStep() = step("a1", "Tag the animals sold", opensSaleTagging = true)
    private fun otherStep() = step("a2", "Collect the gate pass", opensSaleTagging = false)

    private fun step(id: String, title: String, opensSaleTagging: Boolean) = WorkflowActionUi(
        actionId = id,
        actionKey = id,
        title = title,
        detail = "",
        typeLabel = "Do & confirm",
        glyph = "▣",
        requiresVideo = false,
        options = emptyList(),
        statusLabel = "Scheduled",
        statusTone = WorkflowStatusTone.SCHEDULED,
        section = WorkflowActionSection.SCHEDULED,
        canAnswer = false,
        canComplete = false,
        canRecordVideo = false,
        opensPromote = false,
        opensSaleTagging = opensSaleTagging,
        footer = "",
        answerValue = null,
    )
}
