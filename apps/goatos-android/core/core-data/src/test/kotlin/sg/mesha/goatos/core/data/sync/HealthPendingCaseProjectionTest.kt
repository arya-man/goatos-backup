package sg.mesha.goatos.core.data.sync

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus

class HealthPendingCaseProjectionTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `queued Health report remains visible with goat and disease labels while offline`() {
        val pending = projectPendingHealthCaseOpens(
            rows = listOf(
                outboxRow(
                    opType = "HEALTH_CASE_OPEN",
                    payload = """{"goat_id":"goat-1","goat_display_id":"G-000327","disease_key":"fever","disease_name":"Fever","age_band":"adult","start_date":"2026-07-30"}""",
                ),
            ),
            json = json,
        )

        assertEquals(1, pending.size)
        assertEquals("health-row", pending.single().outboxItemId)
        assertEquals("G-000327", pending.single().goatDisplayId)
        assertEquals("Fever", pending.single().diseaseName)
        assertEquals("adult", pending.single().ageBand)
        assertEquals("2026-07-30", pending.single().startDate)
        assertEquals(SyncItemStatus.QUEUED, pending.single().syncStatus)
    }

    @Test
    fun `non Health and malformed rows cannot create fake pending Health cards`() {
        val pending = projectPendingHealthCaseOpens(
            rows = listOf(
                outboxRow(opType = "COUNTS_BIRTH", payload = "{}"),
                outboxRow(opType = "HEALTH_CASE_OPEN", payload = "not-json", id = "broken-health-row"),
            ),
            json = json,
        )

        assertTrue(pending.isEmpty())
    }

    @Test
    fun `local display labels cannot change Health command idempotency`() {
        val first = canonicalOutboxFingerprintPayload(
            OutboxOpType.HEALTH_CASE_OPEN,
            """{"goat_id":"goat-1","disease_key":"fever","age_band":"adult","start_date":"2026-07-30","goat_display_id":"G-000327","disease_name":"Fever"}""",
            json,
        )
        val localized = canonicalOutboxFingerprintPayload(
            OutboxOpType.HEALTH_CASE_OPEN,
            """{"goat_id":"goat-1","disease_key":"fever","age_band":"adult","start_date":"2026-07-30","goat_display_id":"G-000327","disease_name":"ज्वर"}""",
            json,
        )

        assertEquals(first, localized)
    }

    private fun outboxRow(
        opType: String,
        payload: String,
        id: String = "health-row",
    ) = OutboxEntity(
        id = id,
        opType = opType,
        groupKey = "goat-1",
        idempotencyKey = "health-case:stable",
        payloadJson = payload,
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = DEFAULT_MAX_ATTEMPTS,
        conflict = false,
        createdAt = 1L,
        updatedAt = 1L,
        nextAttemptAt = 1L,
        lastError = null,
        resultJson = null,
    )
}
