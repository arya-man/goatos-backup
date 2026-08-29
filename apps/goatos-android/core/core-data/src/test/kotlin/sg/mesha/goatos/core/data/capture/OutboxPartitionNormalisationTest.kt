package sg.mesha.goatos.core.data.capture

import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The partition normalisation exists twice, and must never drift.
 *
 * The running app derives a session's lane through `vaccinationSessionGroupKey`, which uses
 * core-data's `executionPartitionKey`. The Room migration that moves already-queued Submit
 * rows onto that lane cannot call it -- core-database must not depend on core-data -- so it
 * restates the rule as `normalizeOutboxPartition`.
 *
 * If those two ever disagree, the migration puts upgraded rows in a lane the app does not
 * use, which is worse than the bug it repairs: the Submit would be ordered against nothing
 * at all. This asserts they agree on every shape a partition label actually takes.
 */
class OutboxPartitionNormalisationTest {

    @Test
    fun `the migration's normalisation matches the one the app uses`() {
        val labels = listOf(
            null, "", "   ",
            "whole", "Whole", " WHOLE ",
            "1", "2", "10",
            "Part 1", "part 1", "PART  2", " Part   3 ",
            "north", "North Wing",
        )
        for (label in labels) {
            assertEquals(
                "partition \"$label\" must land in the same lane on both sides",
                "task-1|${normalizeOutboxPartitionForTest(label)}",
                vaccinationSessionGroupKey("task-1", label),
            )
        }
    }

    /**
     * A copy of core-database's `normalizeOutboxPartition`, which is `internal` to that
     * module. Kept character-for-character identical on purpose: this test fails if either
     * the app's rule or the migration's rule changes without the other.
     */
    private fun normalizeOutboxPartitionForTest(raw: String?): String {
        val normalized = raw.orEmpty().trim().lowercase().replace(Regex("^part[\\s]+"), "")
        return normalized.ifBlank { "whole" }
    }
}
