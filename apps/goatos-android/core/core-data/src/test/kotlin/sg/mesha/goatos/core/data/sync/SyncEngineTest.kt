package sg.mesha.goatos.core.data.sync

import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.database.capture.CaptureSyncStatus
import sg.mesha.goatos.core.database.capture.ScannedGoatDao
import sg.mesha.goatos.core.database.capture.ScannedGoatEntity
import sg.mesha.goatos.core.database.outbox.DEFAULT_MAX_ATTEMPTS
import sg.mesha.goatos.core.database.outbox.OutboxEntity
import sg.mesha.goatos.core.database.outbox.OutboxOpType
import sg.mesha.goatos.core.database.outbox.OutboxStatus
import sg.mesha.goatos.core.network.dto.ProofArtifactDto
import sg.mesha.goatos.core.network.dto.ProofCompleteResponseDto
import sg.mesha.goatos.core.network.dto.ProofReferenceDto
import sg.mesha.goatos.core.network.dto.ProofUploadRequestDto
import sg.mesha.goatos.core.network.dto.ProofUploadResponseDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationRequestDto
import sg.mesha.goatos.core.network.dto.RescheduleObligationResponseDto
import sg.mesha.goatos.core.network.dto.ScanAttemptDto
import sg.mesha.goatos.core.network.dto.ScanAttemptRequestDto
import sg.mesha.goatos.core.network.dto.ScanAttemptResponseDto
import sg.mesha.goatos.core.network.dto.ScanCaptureDto
import sg.mesha.goatos.core.network.dto.ScanCaptureRequestDto
import sg.mesha.goatos.core.network.dto.ScanCaptureResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto
import sg.mesha.goatos.core.network.dto.SubmissionSummaryDto
import sg.mesha.goatos.core.network.dto.SubmitTaskRequestDto
import sg.mesha.goatos.core.network.dto.ValidationIssueDto
import sg.mesha.goatos.core.network.dto.ValidationReportDto
import sg.mesha.goatos.core.network.dto.VerificationDecision
import sg.mesha.goatos.core.network.dto.VerificationCloseSubmissionResponseDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictRequestDto
import sg.mesha.goatos.core.network.dto.VerificationVerdictResponseDto
import java.io.IOException

/** Table-driven coverage of the outbox drain contract: enqueue -> drain, idempotent retry
 *  (same key reused), failure/backoff, dead-letter, and the non-retryable business-conflict
 *  path. Uses [FakeOutboxStore] + [ScriptedAppApi] only — no Room/Robolectric (see
 *  [FakeOutboxStore]'s KDoc for why). */
class SyncEngineTest {

    private fun queuedShedSubmit(
        id: String = "row-1",
        groupKey: String = "shed-1",
        idempotencyKey: String = "key-1",
        createdAt: Long = 0L,
        maxAttempts: Int = DEFAULT_MAX_ATTEMPTS,
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.SHED_SUBMIT.name,
        groupKey = groupKey,
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ShedSubmitPayload(
                taskId = "task-1",
                request = SubmitTaskRequestDto(sopVersionId = "sop-1", idempotencyKey = idempotencyKey),
            ),
        ),
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = maxAttempts,
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
    fun `successful shed submission response accepts null validation issue arrays`() {
        val decoded = syncJson.decodeFromString<SubmissionResponseDto>(
            """
            {
              "submission": {
                "validation_report": {
                  "valid": true,
                  "errors": null,
                  "warnings": null
                }
              }
            }
            """.trimIndent(),
        )

        assertTrue(decoded.submission.validationReport.valid)
        assertTrue(decoded.submission.validationReport.errors.orEmpty().isEmpty())
        assertTrue(decoded.submission.validationReport.warnings.orEmpty().isEmpty())
    }

    @Test
    fun `happy path drains a queued item to SUCCEEDED`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `health completion drains once with the stored stable key`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            OutboxEntity(
                id = "health-row",
                opType = OutboxOpType.HEALTH_TREATMENT_COMPLETE.name,
                groupKey = "health-session-1",
                idempotencyKey = "health-complete:health-session-1",
                payloadJson = syncJson.encodeToString(
                    HealthTreatmentCompletePayload("health-session-1"),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        val api = ScriptedAppApi()
        SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }).drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("health-row")?.status)
        assertEquals(listOf("health-session-1" to "health-complete:health-session-1"), api.healthCompleteCalls)
    }

    @Test
    fun `opening a health case drains through the production dispatcher`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            OutboxEntity(
                id = "health-open-row",
                opType = "HEALTH_CASE_OPEN",
                groupKey = "goat-1",
                idempotencyKey = "health-case:stable-key",
                payloadJson = """{"goat_id":"goat-1","disease_key":"pneumonia","age_band":"adult","start_date":"2026-07-30"}""",
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )

        val api = ScriptedAppApi()
        SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }).drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("health-open-row")?.status)
        assertEquals("health-case:stable-key", api.healthOpenCalls.single().first)
        assertEquals("goat-1", api.healthOpenCalls.single().second.goatId)
        assertEquals("pneumonia", api.healthOpenCalls.single().second.diseaseKey)
    }

    @Test
    fun `retry after a transport failure reuses the SAME idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(idempotencyKey = "stable-key"))
        var calls = 0
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ ->
                calls++
                if (calls == 1) throw IOException("network down")
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, backoff = BackoffPolicy { 0L })

        engine.drainOnce() // attempt 1: transport failure, scheduled for retry
        var row = store.findById("row-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertEquals(1, row.attemptCount)
        assertTrue(!row.conflict)

        engine.drainOnce() // attempt 2: succeeds
        row = store.findById("row-1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)

        assertEquals(listOf("stable-key", "stable-key"), api.submitCalls.map { it.second })
    }

    @Test
    fun `transport failure schedules the next retry at the computed backoff time`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(idempotencyKey = "stable-key"))
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        var scheduledAt: Long? = null
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { 1_000L },
            backoff = BackoffPolicy { 60_000L },
            retryScheduler = SyncRetryScheduler { scheduledAt = it },
        )

        engine.drainOnce()

        assertEquals(61_000L, scheduledAt)
    }

    @Test
    fun `exhausting the attempt budget marks the row dead-letter and excludes it from future drains`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(maxAttempts = 3))
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("still down") }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L }, backoff = BackoffPolicy { 0L })

        repeat(3) { engine.drainOnce() }

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertEquals(3, row.attemptCount)
        assertTrue(!row.conflict)
        assertEquals(Long.MAX_VALUE, row.nextAttemptAt)

        engine.drainOnce() // 4th pass must NOT attempt the now-dead-lettered row again.
        assertEquals(3, api.submitCalls.size)
    }

    @Test
    fun `a definitive validation rejection is terminal on the FIRST attempt, not a retryable failure`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ ->
                SubmissionResponseDto(
                    submission = SubmissionSummaryDto(
                        validationReport = ValidationReportDto(
                            valid = false,
                            errors = listOf(ValidationIssueDto(field = "answers", code = "required", message = "Answers required")),
                        ),
                    ),
                )
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue(row.conflict)
        assertEquals(1, row.attemptCount) // no retry budget spent looping — terminal immediately
        assertEquals("Answers required", row.lastError)

        engine.drainOnce() // a conflict row is never auto-eligible again.
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `a row stranded IN_FLIGHT by a crash is reclaimed and drained on the next pass`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        // Simulate a process death mid-dispatch: the row was marked IN_FLIGHT, then the process
        // died before markSucceeded/markFailed ran. Pre-fix, eligibleForDrain excluded IN_FLIGHT
        // rows forever, so this submission would be stranded (never retried).
        store.markInFlight("row-1", now = 5L)
        assertEquals(OutboxStatus.IN_FLIGHT.name, store.findById("row-1")!!.status)
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 10L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(1, api.submitCalls.size)
    }

    @Test
    fun `offline skips the drain entirely without spending an attempt`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { false }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-1")!!
        assertEquals(OutboxStatus.QUEUED.name, row.status)
        assertEquals(0, row.attemptCount)
        assertEquals(0, api.submitCalls.size)
    }

    @Test
    fun `items in the same group drain strictly oldest-first`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-2", groupKey = "shed-1", idempotencyKey = "key-2", createdAt = 200L))
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, request ->
                order += request.idempotencyKey
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L })

        engine.drainOnce()

        assertEquals(listOf("key-1", "key-2"), order)
    }

    @Test
    fun `same group stops after the oldest item fails`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-2", groupKey = "shed-1", idempotencyKey = "key-2", createdAt = 200L))
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                if (key == "key-1") throw IOException("oldest failed")
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L }, backoff = BackoffPolicy { 60_000L })

        engine.drainOnce()

        assertEquals(listOf("key-1"), order)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-2")!!.status)
    }

    @Test
    fun `newer same-group rows stay blocked on a later drain while oldest is backed off`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-2", groupKey = "shed-1", idempotencyKey = "key-2", createdAt = 200L))
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        var now = 1_000L
        var failOldest = true
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                if (key == "key-1" && failOldest) {
                    failOldest = false
                    throw IOException("oldest failed")
                }
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { now }, backoff = BackoffPolicy { 60_000L })

        engine.drainOnce()
        engine.drainOnce()

        assertEquals(listOf("key-1"), order)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-2")!!.status)

        now = 61_000L
        engine.drainOnce()

        assertEquals(listOf("key-1", "key-1", "key-2"), order)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-2")!!.status)
    }

    @Test
    fun `dispatches a RESCHEDULE item via the reschedule endpoint with its idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val idempotencyKey = "resched-key-1"
        store.insert(
            OutboxEntity(
                id = "row-r1",
                opType = OutboxOpType.RESCHEDULE.name,
                groupKey = "obl-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    ReschedulePayload(obligationId = "obl-1", request = RescheduleObligationRequestDto(dueAt = "2026-07-10")),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        var seenKey: String? = null
        val api = ScriptedAppApi().apply {
            rescheduleObligationFn = { obligationId, key, _ ->
                seenKey = key
                RescheduleObligationResponseDto(obligationId = obligationId, idempotentReplay = false)
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(idempotencyKey, seenKey)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-r1")!!.status)
    }

    @Test
    fun `dispatches a SCAN_CAPTURE item via the scan-captures endpoint with its idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val scannedGoatDao = FakeScannedGoatDao()
        val idempotencyKey = "scan:task-1:__scan_roster__:901007000504392"
        scannedGoatDao.insert(
            ScannedGoatEntity(
                id = "scan-row-1",
                taskId = "task-1",
                fieldKey = "__scan_roster__",
                tag = "901007000504392",
                goatId = "goat-1",
                obligationId = "obl-1",
                capturedAtMs = 123L,
                syncStatus = CaptureSyncStatus.PENDING.name,
            ),
        )
        store.insert(
            OutboxEntity(
                id = "row-scan-1",
                opType = OutboxOpType.SCAN_CAPTURE.name,
                groupKey = "task-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    ScanCapturePayload(
                        taskId = "task-1",
                        request = ScanCaptureRequestDto(
                            fieldKey = "__scan_roster__",
                            tag = "901007000504392",
                            goatId = "goat-1",
                            obligationId = "obl-1",
                            capturedAtMs = 123L,
                        ),
                    ),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        var seenKey: String? = null
        var seenTag: String? = null
        val api = ScriptedAppApi().apply {
            recordScanCaptureFn = { taskId, key, request ->
                assertEquals("task-1", taskId)
                seenKey = key
                seenTag = request.tag
                ScanCaptureResponseDto(
                    capture = ScanCaptureDto(
                        captureId = "capture-1",
                        taskId = taskId,
                        fieldKey = request.fieldKey,
                        tag = request.tag,
                        goatId = request.goatId,
                        obligationId = request.obligationId,
                    ),
                )
            }
        }
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { 0L },
            scannedGoatDao = scannedGoatDao,
        )

        engine.drainOnce()

        val row = store.findById("row-scan-1")!!
        assertEquals(idempotencyKey, seenKey)
        assertEquals("901007000504392", seenTag)
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(
            CaptureSyncStatus.SYNCED.name,
            scannedGoatDao.listForField("task-1", "__scan_roster__").single().syncStatus,
        )
    }

    @Test
    fun `dispatches a SCAN_ATTEMPT item via the scan-attempts endpoint with its idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val idempotencyKey = "scan-attempt:task-1:attempt-1"
        store.insert(
            OutboxEntity(
                id = "row-attempt-1",
                opType = OutboxOpType.SCAN_ATTEMPT.name,
                groupKey = "task-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    ScanAttemptPayload(
                        taskId = "task-1",
                        request = ScanAttemptRequestDto(
                            fieldKey = "__scan_roster__",
                            tag = "901007000504419",
                            normalizedTag = "901007000504419",
                            goatId = "goat-1",
                            obligationId = "obl-1",
                            outcome = "duplicate",
                            tagRole = "secondary",
                            reason = "goat_already_scanned",
                            capturedAtMs = 124L,
                        ),
                    ),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        var seenKey: String? = null
        var seenOutcome: String? = null
        val api = ScriptedAppApi().apply {
            recordScanAttemptFn = { taskId, key, request ->
                assertEquals("task-1", taskId)
                seenKey = key
                seenOutcome = request.outcome
                ScanAttemptResponseDto(
                    attempt = ScanAttemptDto(
                        attemptId = "attempt-1",
                        taskId = taskId,
                        fieldKey = request.fieldKey,
                        tag = request.tag,
                        goatId = request.goatId,
                        obligationId = request.obligationId,
                        outcome = request.outcome,
                        tagRole = request.tagRole,
                        reason = request.reason,
                    ),
                )
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-attempt-1")!!
        assertEquals(idempotencyKey, seenKey)
        assertEquals("duplicate", seenOutcome)
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
    }

    @Test
    fun `offline SCAN_ATTEMPT stays queued then syncs unchanged when online`() = runBlocking {
        val store = FakeOutboxStore()
        val idempotencyKey = "scan-attempt:task-1:unknown-419"
        store.insert(
            OutboxEntity(
                id = "row-attempt-offline",
                opType = OutboxOpType.SCAN_ATTEMPT.name,
                groupKey = "task-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    ScanAttemptPayload(
                        taskId = "task-1",
                        request = ScanAttemptRequestDto(
                            fieldKey = "__scan_roster__",
                            tag = "901007000504419",
                            normalizedTag = "901007000504419",
                            goatId = null,
                            obligationId = null,
                            outcome = "unknown",
                            tagRole = "unknown",
                            reason = "unknown_tag",
                            capturedAtMs = 9_001L,
                        ),
                    ),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        var online = false
        var scanAttemptCalls = 0
        var seenKey: String? = null
        var seenRequest: ScanAttemptRequestDto? = null
        val api = ScriptedAppApi().apply {
            recordScanAttemptFn = { taskId, key, request ->
                assertEquals("task-1", taskId)
                scanAttemptCalls++
                seenKey = key
                seenRequest = request
                ScanAttemptResponseDto(
                    attempt = ScanAttemptDto(
                        attemptId = "attempt-offline",
                        taskId = taskId,
                        fieldKey = request.fieldKey,
                        tag = request.tag,
                        goatId = request.goatId,
                        obligationId = request.obligationId,
                        outcome = request.outcome,
                        tagRole = request.tagRole,
                        reason = request.reason,
                    ),
                )
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { online }, clock = { 0L })

        val offlineResult = engine.drainOnce()

        assertEquals(false, offlineResult)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-attempt-offline")!!.status)
        assertEquals(0, store.findById("row-attempt-offline")!!.attemptCount)
        assertEquals(0, scanAttemptCalls)

        online = true
        engine.drainOnce()

        val row = store.findById("row-attempt-offline")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(1, scanAttemptCalls)
        assertEquals(idempotencyKey, seenKey)
        assertEquals("901007000504419", seenRequest!!.tag)
        assertEquals("unknown", seenRequest!!.outcome)
        assertEquals("unknown_tag", seenRequest!!.reason)
        assertEquals(9_001L, seenRequest!!.capturedAtMs)
    }

    @Test
    fun `dispatches a VERIFICATION_VERDICT item via the submitVerificationVerdict endpoint with its idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val idempotencyKey = "item-1-verdict-1"
        store.insert(
            OutboxEntity(
                id = "row-v1",
                opType = OutboxOpType.VERIFICATION_VERDICT.name,
                groupKey = "item-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    VerificationVerdictPayload(
                        itemId = "item-1",
                        request = VerificationVerdictRequestDto(decision = VerificationDecision.APPROVED, rowVersion = 1),
                    ),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        var seenKey: String? = null
        var seenDecision: String? = null
        val api = ScriptedAppApi().apply {
            submitVerificationVerdictFn = { itemId, key, request ->
                seenKey = key
                seenDecision = request.decision
                VerificationVerdictResponseDto()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(idempotencyKey, seenKey)
        assertEquals(VerificationDecision.APPROVED, seenDecision)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-v1")!!.status)
    }

    @Test
    fun `dispatches an atomic drive closure with the stable submission idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val idempotencyKey = "submission-1-drive-close"
        store.insert(
            OutboxEntity(
                id = "row-close-1",
                opType = OutboxOpType.VERIFICATION_CLOSE_SUBMISSION.name,
                groupKey = "submission-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    VerificationCloseSubmissionPayload(submissionId = "submission-1"),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        val api = ScriptedAppApi().apply {
            closeVerificationSubmissionFn = { _, _ -> VerificationCloseSubmissionResponseDto() }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(listOf("submission-1" to idempotencyKey), api.closeSubmissionCalls)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-close-1")!!.status)
    }

    @Test
    fun `dispatches an atomic vaccination batch closure with the stable batch idempotency key`() = runBlocking {
        val store = FakeOutboxStore()
        val idempotencyKey = "batch-1-drive-close"
        store.insert(
            OutboxEntity(
                id = "row-close-batch-1",
                opType = OutboxOpType.VERIFICATION_CLOSE_BATCH.name,
                groupKey = "batch-1",
                idempotencyKey = idempotencyKey,
                payloadJson = syncJson.encodeToString(
                    VerificationCloseBatchPayload(batchId = "batch-1"),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        val api = ScriptedAppApi().apply {
            closeVaccinationBatchFn = { _, _ -> VerificationCloseSubmissionResponseDto() }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(listOf("batch-1" to idempotencyKey), api.closeBatchCalls)
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-close-batch-1")!!.status)
    }

    @Test
    fun `dispatches a PROOF_UPLOAD item via the registerProof endpoint`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(
            OutboxEntity(
                id = "row-p1",
                opType = OutboxOpType.PROOF_UPLOAD.name,
                groupKey = "shed-1",
                idempotencyKey = "proof-key-1",
                payloadJson = syncJson.encodeToString(
                    ProofUploadPayload(request = ProofUploadRequestDto(scopeType = "shed", scopeId = "shed-1", subjectType = "shed")),
                ),
                status = OutboxStatus.QUEUED.name,
                attemptCount = 0,
                maxAttempts = DEFAULT_MAX_ATTEMPTS,
                conflict = false,
                createdAt = 0L,
                updatedAt = 0L,
                nextAttemptAt = 0L,
                lastError = null,
                resultJson = null,
            ),
        )
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-p1")!!.status)
    }

    private fun queuedProofUpload(
        id: String = "row-p1",
        idempotencyKey: String = "proof-key-1",
        localFilePath: String = "/data/user/0/sg.mesha.goatos/files/captures/shed.mp4",
        maxAttempts: Int = DEFAULT_MAX_ATTEMPTS,
    ) = OutboxEntity(
        id = id,
        opType = OutboxOpType.PROOF_UPLOAD.name,
        groupKey = "shed-1",
        idempotencyKey = idempotencyKey,
        payloadJson = syncJson.encodeToString(
            ProofUploadPayload(
                request = ProofUploadRequestDto(mimeType = "video/mp4", scopeType = "shed", scopeId = "shed-1", subjectType = "shed"),
                localFilePath = localFilePath,
                durationMs = 4_500L,
            ),
        ),
        status = OutboxStatus.QUEUED.name,
        attemptCount = 0,
        maxAttempts = maxAttempts,
        conflict = false,
        createdAt = 0L,
        updatedAt = 0L,
        nextAttemptAt = 0L,
        lastError = null,
        resultJson = null,
    )

    @Test
    fun `PROOF_UPLOAD streams the captured file's bytes to the signed URL, completes, then reaches SYNCED`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedProofUpload())
        var registerCalls = 0
        val api = ScriptedAppApi().apply {
            registerProofFn = { idempotencyKey, request ->
                registerCalls++
                ProofUploadResponseDto(
                    proof = ProofReferenceDto(proofId = "server-proof-9", proofType = request.proofType, subjectType = request.subjectType, uploadState = "pending"),
                    uploadUrl = "https://storage.example/bucket/object-9",
                    uploadMethod = "PUT",
                    headers = mapOf("x-goog-if-generation-match" to "0"),
                    uploadProtocol = "simple_put",
                )
            }
            uploadProofBlobFn = { proofId, uploadUrl, uploadMethod, uploadHeaders, uploadProtocol, chunkSizeBytes, mimeType, filePath, _ ->
                // Standing in for the real fake object store (OkHttpProofBlobUploaderTest covers
                // the ACTUAL byte-streaming HTTP contract) — this asserts SyncEngine wires the
                // registerProof response straight through, unmodified, to the byte-upload step.
                assertEquals("server-proof-9", proofId)
                assertEquals("https://storage.example/bucket/object-9", uploadUrl)
                assertEquals("PUT", uploadMethod)
                assertEquals("0", uploadHeaders["x-goog-if-generation-match"])
                assertEquals("simple_put", uploadProtocol)
                assertEquals(null, chunkSizeBytes)
                assertEquals("video/mp4", mimeType)
                assertEquals("/data/user/0/sg.mesha.goatos/files/captures/shed.mp4", filePath)
                ProofCompleteResponseDto(proof = ProofArtifactDto(proofId = proofId, uploadState = "completed"))
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-p1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(1, registerCalls)
        assertEquals(listOf("server-proof-9"), api.uploadProofBlobCalls)
        // The Room-facing decode path (CaptureRepository.decodeServerProofId) reads exactly this
        // shape back out of resultJson — prove the dispatch echoes the COMPLETED proof id, not
        // just the pre-upload registration response.
        val decoded = syncJson.decodeFromString<ProofUploadResponseDto>(row.resultJson!!)
        assertEquals("server-proof-9", decoded.proof.proofId)
        assertEquals("completed", decoded.proof.uploadState)
    }

    @Test
    fun `a byte-upload failure is resumable — retried with the SAME idempotency key until it succeeds`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedProofUpload())
        var clockNow = 0L
        val registerKeys = mutableListOf<String>()
        var uploadAttempt = 0
        val api = ScriptedAppApi().apply {
            registerProofFn = { idempotencyKey, request ->
                registerKeys += idempotencyKey
                // A real retry re-mints a FRESH signed URL every attempt (backend CreateProof is
                // now idempotent by key — see backend/internal/proof/adapters/postgres) — model
                // that here so the test also proves the engine doesn't cache a stale URL.
                ProofUploadResponseDto(
                    proof = ProofReferenceDto(proofId = "server-proof-9", proofType = request.proofType, subjectType = request.subjectType, uploadState = "pending"),
                    uploadUrl = "https://storage.example/bucket/object-9?attempt=${registerKeys.size}",
                    uploadMethod = "PUT",
                    uploadProtocol = "simple_put",
                )
            }
            uploadProofBlobFn = { proofId, _, _, _, _, _, _, _, _ ->
                uploadAttempt++
                if (uploadAttempt == 1) {
                    throw java.io.IOException("simulated field-network drop mid-upload")
                }
                ProofCompleteResponseDto(proof = ProofArtifactDto(proofId = proofId, uploadState = "completed"))
            }
        }
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { clockNow },
            backoff = BackoffPolicy { 100L },
        )

        // Attempt 1: registerProof succeeds, the byte PUT fails — the row backs off, NOT
        // dead-lettered (still under maxAttempts), and the outbox item itself is untouched (same
        // idempotency key persists for the retry).
        engine.drainOnce()
        var row = store.findById("row-p1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue("must be retryable, not dead-lettered", !row.conflict)
        assertEquals(1, row.attemptCount)
        assertEquals(1, uploadAttempt)
        assertEquals(listOf("proof-key-1"), registerKeys)

        // Advance past the backoff window and drain again — the SAME idempotency key is reused
        // (never a new one on retry, matching every other dispatch* here), the whole
        // register->upload->complete pipeline re-runs, and this time it succeeds.
        clockNow = row.nextAttemptAt
        engine.drainOnce()

        row = store.findById("row-p1")!!
        assertEquals(OutboxStatus.SUCCEEDED.name, row.status)
        assertEquals(2, uploadAttempt)
        assertEquals(listOf("proof-key-1", "proof-key-1"), registerKeys)
        assertEquals(listOf("server-proof-9", "server-proof-9"), api.uploadProofBlobCalls)
    }

    @Test
    fun `a proof registration that returns no proof id is a terminal (non-retryable) conflict`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedProofUpload())
        val api = ScriptedAppApi().apply {
            registerProofFn = { _, _ -> ProofUploadResponseDto() } // blank proof id
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        engine.drainOnce()

        val row = store.findById("row-p1")!!
        assertEquals(OutboxStatus.FAILED.name, row.status)
        assertTrue("a structurally broken response must terminalize, not burn the retry budget", row.conflict)
        assertTrue(api.uploadProofBlobCalls.isEmpty())
    }

    // --- Batching + Boolean drain-pass contract -------------------------------------------------

    @Test
    fun `more eligible rows than the batch size drain across several bounded fetches`() = runBlocking {
        val store = RecordingOutboxStore()
        // 250 > the engine's batch size, spread over a few groups so the drain also fans out.
        repeat(250) { i ->
            store.insert(queuedShedSubmit(id = "row-$i", idempotencyKey = "key-$i", groupKey = "shed-${i % 5}", createdAt = i.toLong()))
        }
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L })

        val result = engine.drainOnce()

        assertTrue(result) // a clean full drain
        assertEquals(250, api.submitCalls.size) // every row was dispatched exactly once
        // Pulled in MORE THAN ONE fetch, and every fetch was BOUNDED (never one unbounded SELECT *).
        assertTrue("expected multiple batched fetches, got ${store.drainLimits}", store.drainLimits.size >= 2)
        assertTrue("every fetch must pass a finite limit", store.drainLimits.all { it in 1..1_000 })
    }

    @Test
    fun `same-group FIFO is preserved across a batch boundary`() = runBlocking {
        val store = RecordingOutboxStore()
        // One group, more rows than a single batch. Inserted newest-first to prove the drain sorts
        // by createdAt, not insertion order — and that the ordering holds ACROSS the batch split.
        val n = 220
        for (i in (n - 1) downTo 0) {
            store.insert(queuedShedSubmit(id = "row-$i", idempotencyKey = "key-$i", groupKey = "shed-1", createdAt = i.toLong()))
        }
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L })

        engine.drainOnce()

        assertTrue("expected the group to span >1 batch", store.drainLimits.size >= 2)
        assertEquals((0 until n).map { "key-$it" }, order) // strict oldest-first, unbroken over the boundary
    }

    @Test
    fun `same-group failure in an over-batch pass blocks newer same-group rows for the rest of the pass`() = runBlocking {
        val store = RecordingOutboxStore()
        // 201 rows in ONE group so the FIRST full batch fills up entirely with this group. The oldest
        // fails; the group's remaining rows in that batch are NOT dispatched (FIFO break). The risk the
        // multi-batch loop introduced: the SECOND fetch re-includes the newer QUEUED same-group rows
        // (the failed oldest is now excluded by its backoff) and would dispatch them ahead of the older
        // failed write — posting newer submissions over an older un-synced one.
        val n = 201
        for (i in 0 until n) {
            store.insert(queuedShedSubmit(id = "row-$i", idempotencyKey = "key-$i", groupKey = "shed-1", createdAt = i.toLong()))
        }
        val order = mutableListOf<String>()
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, key, _ ->
                order += key
                if (key == "key-0") throw IOException("oldest failed") // FIFO head fails
                okSubmission()
            }
        }
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 1_000L }, backoff = BackoffPolicy { 60_000L })

        engine.drainOnce()

        // Only the failed oldest was attempted this pass. No newer same-group row jumped ahead of it,
        // even though the loop ran a second batch fetch (the backed-off head no longer appears there).
        assertEquals(listOf("key-0"), order)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-0")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-1")!!.status)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-200")!!.status)
    }

    @Test
    fun `a completed pass returns true even when a row fails, leaving the retry to the scheduler`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        var scheduledAt: Long? = null
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { 1_000L },
            backoff = BackoffPolicy { 60_000L },
            retryScheduler = SyncRetryScheduler { scheduledAt = it },
        )

        val result = engine.drainOnce()

        // The row was ATTEMPTED, so the pass COMPLETED -> true. SyncWorker therefore reports
        // success and does NOT also Result.retry(); the failed row's next attempt is owned solely
        // by the explicit retry work booked here. (No worker-retry + explicit-schedule double-drain.)
        assertTrue(result)
        assertEquals(61_000L, scheduledAt)
        assertEquals(OutboxStatus.FAILED.name, store.findById("row-1")!!.status)
    }

    @Test
    fun `multiple group failures schedule only the earliest retry for the pass`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit(id = "row-1", groupKey = "shed-1", idempotencyKey = "key-1", createdAt = 100L))
        store.insert(
            queuedShedSubmit(id = "row-2", groupKey = "shed-2", idempotencyKey = "key-2", createdAt = 200L)
                .copy(status = OutboxStatus.FAILED.name, attemptCount = 1, nextAttemptAt = 1_000L),
        )
        val api = ScriptedAppApi().apply {
            submitAppTaskFn = { _, _, _ -> throw IOException("network down") }
        }
        val scheduledAt = mutableListOf<Long>()
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { true },
            clock = { 1_000L },
            backoff = BackoffPolicy { attempt -> if (attempt == 1) 60_000L else 300_000L },
            retryScheduler = SyncRetryScheduler { scheduledAt += it },
        )

        engine.drainOnce()

        assertEquals(listOf(61_000L), scheduledAt)
        assertEquals(61_000L, store.findById("row-1")!!.nextAttemptAt)
        assertEquals(301_000L, store.findById("row-2")!!.nextAttemptAt)
    }

    @Test
    fun `an offline pass returns false and schedules nothing so the worker reruns it`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        var scheduledAt: Long? = null
        val api = ScriptedAppApi()
        val engine = SyncEngine(
            store,
            api,
            connectivityGate = { false },
            clock = { 0L },
            retryScheduler = SyncRetryScheduler { scheduledAt = it },
        )

        val result = engine.drainOnce()

        assertTrue(!result) // aborted before attempting -> worker returns Result.retry()
        assertEquals(null, scheduledAt) // nothing attempted -> nothing to back off
        assertEquals(0, api.submitCalls.size)
        assertEquals(OutboxStatus.QUEUED.name, store.findById("row-1")!!.status)
    }

    @Test
    fun `a clean full drain returns true`() = runBlocking {
        val store = FakeOutboxStore()
        store.insert(queuedShedSubmit())
        val api = ScriptedAppApi()
        val engine = SyncEngine(store, api, connectivityGate = { true }, clock = { 0L })

        assertTrue(engine.drainOnce())
        assertEquals(OutboxStatus.SUCCEEDED.name, store.findById("row-1")!!.status)
    }
}

private class FakeScannedGoatDao : ScannedGoatDao {
    private val rows = mutableListOf<ScannedGoatEntity>()

    override suspend fun insert(entity: ScannedGoatEntity): Long {
        if (rows.any { it.taskId == entity.taskId && it.fieldKey == entity.fieldKey && it.tag == entity.tag }) {
            return -1L
        }
        rows += entity
        return 1L
    }

    override fun observeForField(taskId: String, fieldKey: String, limit: Int): Flow<List<ScannedGoatEntity>> =
        flowOf(rows.filter { it.taskId == taskId && it.fieldKey == fieldKey }.take(limit))

    override suspend fun listForField(taskId: String, fieldKey: String, limit: Int): List<ScannedGoatEntity> =
        rows.filter { it.taskId == taskId && it.fieldKey == fieldKey }.take(limit)

    override fun observeCountForField(taskId: String, fieldKey: String): Flow<Int> =
        flowOf(rows.count { it.taskId == taskId && it.fieldKey == fieldKey })

    override suspend fun listForTask(taskId: String, limit: Int): List<ScannedGoatEntity> =
        rows.filter { it.taskId == taskId }.take(limit)

    override fun observeForTask(taskId: String, limit: Int): Flow<List<ScannedGoatEntity>> =
        flowOf(rows.filter { it.taskId == taskId }.take(limit))

    override suspend fun markTaskStatus(taskId: String, status: String) {
        rows.replaceAll { row -> if (row.taskId == taskId) row.copy(syncStatus = status) else row }
    }

    override suspend fun markFieldTagStatus(taskId: String, fieldKey: String, tag: String, status: String) {
        rows.replaceAll { row ->
            if (row.taskId == taskId && row.fieldKey == fieldKey && row.tag == tag) {
                row.copy(syncStatus = status)
            } else {
                row
            }
        }
    }

    override suspend fun clearForTask(taskId: String) {
        rows.removeAll { it.taskId == taskId }
    }

    override suspend fun clearAll() {
        rows.clear()
    }
}

/**
 * Wraps [FakeOutboxStore] to record every eligibility fetch's [limit] so a test can assert the
 * drain pulls rows in bounded batches (see the batching tests) rather than one unbounded SELECT.
 * Everything else delegates unchanged.
 */
private class RecordingOutboxStore(private val inner: FakeOutboxStore = FakeOutboxStore()) : OutboxStore {
    val drainLimits = mutableListOf<Int>()

    override suspend fun insert(entity: OutboxEntity) = inner.insert(entity)
    override suspend fun findById(id: String) = inner.findById(id)
    override suspend fun findByIdempotencyKey(key: String) = inner.findByIdempotencyKey(key)

    override suspend fun eligibleForDrain(now: Long, limit: Int): List<OutboxEntity> {
        drainLimits += limit
        return inner.eligibleForDrain(now, limit)
    }

    override fun observeActive() = inner.observeActive()
    override fun observeById(id: String) = inner.observeById(id)
    override suspend fun observeRecentTerminals(recentLimit: Int) = inner.observeRecentTerminals(recentLimit)
    override suspend fun pruneSucceeded(retentionMs: Long, now: Long) = inner.pruneSucceeded(retentionMs, now)
    override fun observeAll() = inner.observeAll()
    override suspend fun markInFlight(id: String, now: Long) = inner.markInFlight(id, now)
    override suspend fun markSucceeded(id: String, resultJson: String, now: Long) = inner.markSucceeded(id, resultJson, now)
    override suspend fun markFailed(id: String, attemptCount: Int, nextAttemptAt: Long, conflict: Boolean, lastError: String, now: Long) =
        inner.markFailed(id, attemptCount, nextAttemptAt, conflict, lastError, now)
    override suspend fun markRetryReady(id: String, now: Long) = inner.markRetryReady(id, now)
    override suspend fun reclaimInFlight(now: Long) = inner.reclaimInFlight(now)
    override suspend fun delete(id: String) = inner.delete(id)
}
