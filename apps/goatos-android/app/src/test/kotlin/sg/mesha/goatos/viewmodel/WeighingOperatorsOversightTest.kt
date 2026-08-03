package sg.mesha.goatos.viewmodel

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.data.weighing.WeighingAssignment
import sg.mesha.goatos.feature.weighing.WeighingAssignmentUiRow

/**
 * The weighing director's oversight surface, at the grain a director reads it.
 *
 * The screen is called "Operators". It rendered a flat list of SHEDS with no name anywhere on it,
 * every card claiming a green "Completed" badge above a grey "Scheduled" line above an empty bar,
 * and not one count of anything. These tests pin the three separate defects behind that frame:
 *
 *  1. people, named, with the backend's own totals — not sheds;
 *  2. ONE state per shed card, not two contradicting each other;
 *  3. no invented denominator anywhere, because free-flow weighing has no expected roster.
 *
 * The fixture is the real shape of the phone-QA farm on 2026-08-03: six buckets across three
 * people, five individual and one lump-sum, every bucket submitted and none yet accepted.
 */
class WeighingOperatorsOversightTest {

    // ---- 2. ONE state per shed card ------------------------------------------------------

    /**
     * The root cause, pinned at the type that carries it.
     *
     * 'completed' (the operator submitted) and 'closed' (a verifier accepted) are DIFFERENT states.
     * The old card printed the raw status string in a badge and asked `isClosed` for both the line
     * beneath it and the bar, so a submitted bucket rendered "Completed", "Scheduled" and an empty
     * bar at once — three renderings of one field, two of them wrong.
     */
    @Test
    fun `a submitted bucket is submitted, and is neither accepted nor not-started`() {
        val submitted = assignmentUiRow(status = "Completed")

        assertTrue(submitted.isSubmittedAndWaitingVerification)
        assertFalse(submitted.isClosed)

        val accepted = assignmentUiRow(status = "Closed")
        assertTrue(accepted.isClosed)
        assertFalse(accepted.isSubmittedAndWaitingVerification)
    }

    /**
     * No read-only weighing card may render the two-truths pair again.
     *
     * `weighing_status_completed` and `weighing_status_scheduled` were the SECOND, derived
     * rendering that contradicted the badge above it. A card states a bucket's state once, through
     * `shedStateLabel`, or the reader gets two answers to one question. This is a source-contract
     * guard on purpose: it survives a well-meaning re-add that no screenshot test would catch.
     */
    @Test
    fun `read-only weighing cards state a bucket state exactly once`() {
        val screens = listOf(
            "WeighingOperatorsScreen.kt",
            "LeadershipWeighingScreen.kt",
        ).map { name ->
            name to File(
                "../feature/feature-weighing/src/main/kotlin/sg/mesha/goatos/feature/weighing/$name",
            )
        }
        // MERGE NOTE (fix/review-counter-22 x origin/main): LeadershipWeighingScreen.kt was
        // RETIRED on this branch (its keyset deep-link page-walk was replaced by a single-task
        // read), so its presence is no longer required -- a screen that does not exist cannot
        // render the pair. The name is kept in the list on purpose: this stays a live guard
        // against a re-add, which is what the check was written to survive. At least one of the
        // listed screens must still exist, so the guard can never silently check nothing.
        assertTrue(
            "no read-only weighing screen found -- this guard must never check an empty set",
            screens.any { (_, file) -> file.exists() },
        )
        screens.filter { (_, file) -> file.exists() }.forEach { (name, file) ->
            val source = file.readText()
            assertFalse(
                "$name still renders the derived Completed/Scheduled pair beside the state badge",
                source.contains("weighing_status_completed") || source.contains("weighing_status_scheduled"),
            )
            assertTrue(
                "$name must decide a bucket's state in one place (shedStateLabel)",
                source.contains("shedStateLabel"),
            )
        }
    }

    // ---- 1. people, named, with real counts ----------------------------------------------

    /**
     * Every shed row names WHO did it.
     *
     * A screen whose entire purpose is "which operator did what" cannot render rows that carry no
     * name. The name travels ON the row from the backend; the app never renders a user id and never
     * joins the row against a separately paged operator vocabulary to find one.
     */
    @Test
    fun `every shed row on the oversight surface carries its assignee name`() {
        val rows = phoneQaBuckets().map { it.toOversightUiRow() }

        assertEquals(6, rows.size)
        assertTrue(rows.all { it.operatorName.isNotBlank() })
        assertEquals(
            setOf("Amit", "Dinakar", "Pramod"),
            rows.map { it.operatorName }.toSet(),
        )
    }

    // ---- 3. no invented denominator -------------------------------------------------------

    /**
     * The oversight row type carries no fraction to render.
     *
     * Free-flow weighing dropped the expected-animal roster (migration 000079), so any ratio drawn
     * on this surface would be a share of a total that does not exist. The guard is on the TYPE:
     * a field that is not there cannot be divided by accident.
     */
    @Test
    fun `the oversight row exposes no ratio, percentage or expected total`() {
        val forbidden = listOf("progress", "percent", "fraction", "expectedCount", "expectedAnimal", "totalExpected")
        val fields = WeighingAssignmentUiRow::class.java.declaredFields.map { it.name }
        forbidden.forEach { banned ->
            assertFalse(
                "WeighingAssignmentUiRow must not carry `$banned`: weighing has no denominator",
                fields.any { it.contains(banned, ignoreCase = true) },
            )
        }
    }

    private fun assignmentUiRow(status: String): WeighingAssignmentUiRow =
        WeighingAssignmentUiRow(
            campaignId = "campaign-1",
            tenantId = "tenant-1",
            parkId = "park-cpt",
            parkLabel = "CPT",
            workGroupId = "shed-1",
            campaignShedId = "shed-1",
            expectedLocationId = "loc-1",
            expectedLocationLabel = "Gandhi 1",
            label = "Gandhi 1",
            category = "individual_animal",
            operatorName = "Dinakar",
            status = status,
            periodLabel = "2026-08-03",
        )
}

/**
 * The six buckets the phone-QA farm actually held on 2026-08-03, with the assignees the backend
 * resolves onto them. Every bucket is `completed`: submitted by its operator, not yet accepted.
 */
internal fun phoneQaBuckets(): List<WeighingAssignment> = listOf(
    weighingBucket("Castro 1", "Amit", "per_shed_partition"),
    weighingBucket("Mandela 2", "Dinakar", "per_shed_partition"),
    weighingBucket("Gandhi 1", "Dinakar", "individual_animal"),
    weighingBucket("Gandhi 2", "Dinakar", "individual_animal"),
    weighingBucket("Godel 1", "Pramod", "individual_animal"),
    weighingBucket("Yashoda 1", "Pramod", "individual_animal"),
)

private fun weighingBucket(shed: String, operator: String, category: String): WeighingAssignment =
    WeighingAssignment(
        campaignId = "campaign-cpt",
        tenantId = "tenant-1",
        parkId = "park-cpt",
        parkName = "CPT",
        workGroupId = shed,
        campaignShedId = shed,
        expectedLocationId = shed,
        expectedLocationLabel = shed,
        label = shed,
        category = category,
        operatorUserId = "user-$operator",
        operatorDisplayName = operator,
        status = "completed",
        periodLabel = "2026-08-03",
    )

/**
 * The same projection the oversight surface applies. Kept beside the fixture so the test asserts on
 * the row the screen renders rather than on the wire model behind it.
 */
private fun WeighingAssignment.toOversightUiRow(): WeighingAssignmentUiRow =
    WeighingAssignmentUiRow(
        campaignId = campaignId,
        tenantId = tenantId,
        parkId = parkId,
        parkLabel = parkName,
        workGroupId = workGroupId,
        campaignShedId = campaignShedId,
        expectedLocationId = expectedLocationId,
        expectedLocationLabel = expectedLocationLabel,
        label = label,
        category = category,
        operatorName = operatorDisplayName,
        status = status,
        periodLabel = periodLabel,
    )
