package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.ValidationReportDto
import java.io.IOException

/**
 * Vaccination's own path through the outbox.
 *
 * A vaccination shed session is not a single write. The operator scans each
 * animal as it is done — every scan is its own SCAN_CAPTURE row — and then
 * submits the shed once, which is a SHED_SUBMIT row. On a farm the phone is
 * usually offline while that happens, so the whole session lands in the outbox
 * and drains later, possibly after the app has been killed and reopened.
 *
 * The sync engine is generic, and the existing suite covers counts, feed, health
 * and milk. Vaccination rides the same rails but had only one test (the
 * leadership batch closure). These are the properties a vaccination session
 * depends on, asserted for vaccination specifically:
 *
 *   1. scans and the submit drain in the order they were made, per shed
 *   2. a scan retried after a network failure reuses its idempotency key, so a
 *      goat cannot be recorded twice
 *   3. one shed failing does not hold up another shed's session
 *   4. a submit that the server rejects as a business conflict stops, and is not
 *      retried until the queue dies
 */
class VaccinationOutboxConvergenceTest {

    private fun scanRow(
        id: String,
        shed: String,
        tag: String,
        createdAt: Long,
        idempotencyKey: String = "scan:$shed:$tag",
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.SCAN_CAPTURE.name,
        groupKey = shed,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ScanCapturePayload(
                taskId = shed,
                request = ScanCaptureRequestDto(fieldKey = "vaccinated_animals", tag = tag),
            ),
        ),
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = DEFAULT_MAX_ATTEMPTS,
        conflict = false,
        createdAt = createdAt,
        updatedAt = createdAt,
        nextAttemptAt = createdAt,
        lastError = null,
        resultJson = null,
    )

    private fun submitRow(
        id: String,
        shed: String,
        createdAt: Long,
        idempotencyKey: String = "submit:$shed",
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.SHED_SUBMIT.name,
        groupKey = shed,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ShedSubmitPayload(
                taskId = shed,
                request = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = idempotencyKey),
            ),
        ),
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = DEFAULT_MAX_ATTEMPTS,
        conflict = false,
        createdAt = createdAt,
        updatedAt = createdAt,
        nextAttemptAt = createdAt,
        lastError = null,
        resultJson = null,
    )

    private fun okSubmission() =
        SubmissionResponseDto(submission = SubmissionSummaryDto(validationReport = ValidationReportDto(valid = true)))

    @Test
    fun `an offline shed session drains scans before the submit`() = runBlocking {
        val store = FakeOutboxStore()
        // Inserted in the WRONG order on purpose: the operator worked TAG-1, TAG-2,
        // TAG-3, then Submit, and that is what createdAt says. Inserting them
        // backwards means passing this test requires ordering by createdAt -- an
        // engine that merely drained in insertion order would fail.
        store.insert(submitRow("submit-1", "shed-A", createdAt = 4L))
        store.insert(scanRow("scan-3", "shed-A", "TAG-3", createdAt = 3L))
        store.insert(scanRow("scan-1", "shed-A", "TAG-1", createdAt = 1L))
        store.insert(scanRow("scan-2", "shed-A", "TAG-2", createdAt = 2L))

        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            recordScanCaptureFn = { _, key, _ ->
                order += key
                ScanCaptureResponseDto()
            }
            submitAppTaskFn = { _, key, _ ->
                order += key
                okSubmission()
            }
        }

        SyncEngine(store, api, connectivityGate = { true }, clock = { 10L }).drainOnce()

        assertEquals(
            listOf("scan:shed-A:TAG-1", "scan:shed-A:TAG-2", "scan:shed-A:TAG-3", "submit:shed-A"),
            order,
        )
        listOf("scan-1", "scan-2", "scan-3", "submit-1").forEach {
            assertEquals(OutboxStatus.SUCCEEDED.name, store.findById(it)!!.status)
        }
    }

    @Test
    fun `a scan retried after a dropped connection reuses its key, so the animal is recorded once`() =
        runBlocking {
            val store = FakeOutboxStore()
            store.insert(scanRow("scan-1", "shed-A", "TAG-1", createdAt = 0L))

            var attempts = 0
            val keys = mutableListOf<String>()
            val api = ScriptedAppApi().apply {
                recordScanCaptureFn = { _, key, _ ->
                    keys += key
                    attempts += 1
                    // The farm's signal drops on the first attempt.
                    if (attempts == 1) throw IOException("network down") else ScanCaptureResponseDto()
                }
            }
            val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

            engine.drainOnce()
            // A retryable failure parks the row as FAILED with a future retry time --
            // it is NOT returned to QUEUED, which would let it be picked up immediately
            // and defeat the backoff.
            val parked = store.findById("scan-1")!!
            assertEquals(OutboxStatus.FAILED.name, parked.status)
            assertTrue("the scan is not abandoned", !parked.conflict)

            // The backoff has elapsed; drain again as the scheduler would.
            store.markRetryReady("scan-1", now = 0L)
            engine.drainOnce()

            assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("scan-1")!!.status)
            assertEquals(2, keys.size)
            assertEquals("the same key on both attempts", keys[0], keys[1])
        }

    @Test
    fun `one shed stuck offline does not block another shed's session`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(scanRow("stuck", "shed-A", "TAG-1", createdAt = 0L))
        store.insert(scanRow("fine", "shed-B", "TAG-9", createdAt = 0L))

        val api = ScriptedAppApi().apply {
            recordScanCaptureFn = { taskId, _, _ ->
                if (taskId == "shed-A") throw IOException("still offline") else ScanCaptureResponseDto()
            }
        }

        SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }).drainOnce()

        val stuck = store.findById("stuck")!!
        assertEquals(OutboxStatus.FAILED.name, stuck.status)
        assertTrue("shed A is only waiting to retry, not abandoned", !stuck.conflict)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("fine")!!.status)
    }

    @Test
    fun `a shed submit the server refuses stops instead of retrying to death`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(submitRow("submit-1", "shed-A", createdAt = 0L))

        var calls = 0
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ ->
                calls += 1
                // The shed was already closed by someone else. Re-sending the same
                // payload cannot change that, so it must not consume the retry budget.
                throw NonRetryableSyncException("shed already closed")
            }
        }

        SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }).drainOnce()

        val row = store.findById("submit-1")!!
        assertTrue("a business conflict is terminal", row.conflict)
        assertEquals(1, calls)
    }
    @Test
    fun `PRODUCTION group keys put a shed's scans and its Submit in DIFFERENT queues`() = runBlocking {
        // The hold-back only works WITHIN one FIFO group, and vaccination's two writes do
        // not share one:
        //   scan   CaptureRepository.kt:267   "$taskId|$partitionKey"
        //   submit SubmitViewModel.kt:713     "${activeShedId ?: scopeId ?: taskId}|${label ?: "whole"}"
        // Different first segment, and different normalisation of the second. So a Submit
        // can go to the server while one of that shed's animals is still unsent, closing
        // the shed one animal short -- and the shed reads as complete, so nobody looks.
        //
        // Weighing already avoids this by using campaignShedId for BOTH its observations
        // and its scope submit (WeighingRepository.kt:2035,2058).
        //
        // This test pins the CURRENT, WRONG behaviour so the fix has something to flip.
        val store = FakeOutboxStore()
        val taskId = "task-77"
        val shedId = "shed-77"
        store.insert(scanRow("scan-late", "$taskId|whole", "TAG-1", createdAt = 1L))
        store.insert(submitRow("submit-A", "$shedId|whole", createdAt = 2L))

        val api = ScriptedAppApi().apply {
            recordScanCaptureFn = { _, _, _ -> throw IOException("no signal") }
            submitAppTaskFn = { _, _, _ -> okSubmission() }
        }

        SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }).drainOnce()

        assertEquals(OutboxStatus.FAILED.name, store.findById("scan-late")!!.status)
        assertEquals(
            "DEFECT: the Submit went ahead while this shed's scan was still failing, because the " +
                "two writes are grouped under different keys. Fix by giving them one shared group key.",
            1,
            api.submitCalls.size,
        )
    }

    @Test
    fun `a shed's Submit is held back while one of its own scans is still failing`() = runBlocking {
        // The most consequential ordering property in a vaccination session, and the
        // one a shed-level test must prove: if an animal's scan has not reached the
        // server yet, the shed's Submit must NOT go ahead of it. A submit that raced
        // past a failed scan would close the shed having recorded one animal fewer
        // than the operator actually vaccinated -- and the shed reads as complete, so
        // nobody goes looking.
        val store = FakeOutboxStore()
        store.insert(scanRow("scan-late", "shed-A", "TAG-1", createdAt = 1L))
        store.insert(submitRow("submit-A", "shed-A", createdAt = 2L))

        var scanAttempts = 0
        val api = ScriptedAppApi().apply {
            recordScanCaptureFn = { _, _, _ ->
                scanAttempts += 1
                // Still no signal on the first attempt.
                if (scanAttempts == 1) throw IOException("no signal") else ScanCaptureResponseDto()
            }
            submitAppTaskFn = { _, _, _ -> okSubmission() }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(OutboxStatus.FAILED.name, store.findById("scan-late")!!.status)
        assertEquals("the Submit must not have gone yet", 0, api.submitCalls.size)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("submit-A")!!.status)

        // Once the scan lands, the Submit is free to follow -- in that order.
        store.markRetryReady("scan-late", now = 0L)
        engine.drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("scan-late")!!.status)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("submit-A")!!.status)
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `a different shed's Submit is not held back by shed A's failing scan`() = runBlocking {
        // The other half of the same rule: hold-back is per shed, not global. One
        // shed stuck offline must not stop a second operator finishing theirs.
        val store = FakeOutboxStore()
        store.insert(scanRow("scan-A", "shed-A", "TAG-1", createdAt = 1L))
        store.insert(submitRow("submit-A", "shed-A", createdAt = 2L))
        store.insert(submitRow("submit-B", "shed-B", createdAt = 3L))

        val api = ScriptedAppApi().apply {
            recordScanCaptureFn = { _, _, _ -> throw IOException("shed A has no signal") }
            submitAppTaskFn = { _, _, _ -> okSubmission() }
        }

        SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }).drainOnce()

        assertEquals(OutboxStatus.QUEUED.name, store.findById("submit-A")!!.status)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("submit-B")!!.status)
        assertEquals(1, api.submitCalls.size)
        assertTrue("only shed B submitted", api.submitCalls.all { it.first == "shed-B" })
    }
}
