package sg.mesha.goatos.feature.verify

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * THE WEIGH CAPTURES ARE AUTHORED (maintainer clarification 2026-09-16): a per-animal weigh item
 * and a whole-pen weigh item must read differently on the verify detail. The backend leads each
 * with "Weighed as: Per animal / Whole pen" in group "Weighing" and groups the operator's answers
 * under "Per-animal answers" / "Whole-pen answers"; this pins that the detail screen starts a
 * titled section at each, so the two kinds are never rendered as one undifferentiated list.
 *
 * telemetry:exempt pure mapping unit test — renders no surface.
 */
class VerifyWeighingCaptureKindSectionsTest {
    private fun row(label: String, value: String, group: String) =
        VerifyContextRow(kind = VerifyContextKind.RAISED_NOTE, value = value, backendLabel = label, group = group)

    private val perAnimal = listOf(
        row("Weighed as", "Per animal", "Weighing"),
        row("Limping?", "No", "Per-animal answers"),
    )
    private val wholePen = listOf(
        row("Weighed as", "Whole pen", "Weighing"),
        row("Every animal on the scale?", "Yes", "Whole-pen answers"),
        row("Scale display photo", "Not captured (older app)", "Not captured"),
    )

    @Test
    fun `each kind opens its own weighing and answers sections`() {
        assertEquals(listOf("Weighing", "Per-animal answers"), perAnimal.indices.mapNotNull { contextGroupHeaderAt(perAnimal, it) })
        assertEquals(listOf("Weighing", "Whole-pen answers", "Not captured"), wholePen.indices.mapNotNull { contextGroupHeaderAt(wholePen, it) })
    }

    @Test
    fun `the two kinds are told apart on the item itself`() {
        assertNotEquals(perAnimal.first().value, wholePen.first().value)
        assertNotEquals(contextGroupHeaderAt(perAnimal, 1), contextGroupHeaderAt(wholePen, 1))
    }
}
