package sg.mesha.goatos.core.network

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test
import sg.mesha.goatos.core.network.dto.SubmissionResponseDto

class SubmissionResponseDtoTest {
    private val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
    }

    @Test
    fun `submission validation report accepts null issue arrays from backend`() {
        val decoded = json.decodeFromString<SubmissionResponseDto>(
            """
            {
              "submission": {
                "submission_id": "146df065-1e02-4d68-85d1-27f7f9c6dcd1",
                "task_id": "fe410c0a-fb39-44a4-82c2-414958044298",
                "sop_version_id": "b0000000-0000-4000-8000-000000000002",
                "submitted_by": "90000000-0000-4000-8000-000000000101",
                "idempotency_key": "shed-submit:test",
                "answers": {},
                "proof_refs": [],
                "state": "accepted",
                "validation_report": {
                  "valid": true,
                  "errors": null,
                  "warnings": null
                },
                "items": [],
                "submitted_at": "2026-07-20T10:34:47Z",
                "accepted_at": "2026-07-20T10:34:47Z",
                "row_version": 1
              },
              "task": {
                "task_id": "fe410c0a-fb39-44a4-82c2-414958044298",
                "state": "accepted"
              },
              "trace_id": "trace-1"
            }
            """.trimIndent(),
        )

        assertEquals(true, decoded.submission.validationReport.valid)
        assertEquals(0, decoded.submission.validationReport.errors.orEmpty().size)
        assertEquals(0, decoded.submission.validationReport.warnings.orEmpty().size)
    }
}
