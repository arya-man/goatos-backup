package sg.mesha.goatos.core.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/** Web-authored tabs (2026-10-01) share one Room table with the Routines list: keys must never collide. */
class PenRoutineScopeKeyTest {
    @Test
    fun `the Routines list keeps its shipped key byte for byte`() {
        assertEquals("routine-v1|pen-routines||20", penRoutineScopeKey(PenRoutineQuery()))
        assertEquals("routine-v1|pen-routines|done|20", penRoutineScopeKey(PenRoutineQuery(filter = "done")))
    }

    @Test
    fun `a tab and each filter selection get their own scope`() {
        val keys = listOf(
            PenRoutineQuery(),
            PenRoutineQuery(tab = "fumigation"),
            PenRoutineQuery(tab = "cleaning"),
            PenRoutineQuery(tab = "fumigation", filter = "done"),
            PenRoutineQuery(tab = "fumigation", dueFrom = "2026-10-01", dueTo = "2026-10-01"),
            PenRoutineQuery(tab = "fumigation", pen = "shed-1|2"),
            PenRoutineQuery(tab = "fumigation", pen = "shed-1|3"),
        ).map(::penRoutineScopeKey)
        assertEquals(keys.size, keys.toSet().size)
        assertNotEquals(penRoutineScopeKey(PenRoutineQuery(pen = "x")), penRoutineScopeKey(PenRoutineQuery()))
    }
}
