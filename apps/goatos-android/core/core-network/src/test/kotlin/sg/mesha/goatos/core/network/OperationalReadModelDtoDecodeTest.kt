package sg.mesha.goatos.core.network

import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import sg.mesha.goatos.core.network.dto.AdherenceRowDto
import sg.mesha.goatos.core.network.dto.ControlTowerAlertDto

class OperationalReadModelDtoDecodeTest {
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `control tower alert decodes partition and backend-owned summaries`() {
        val dto = json.decodeFromString<ControlTowerAlertDto>(
            """
            {
              "row_id": "alert-1",
              "severity": "watch",
              "work_state": "verification_pending",
              "title": "Proof review",
              "detail": "Channapatna / Shed A / Part 2: awaiting review",
              "scope_label": "Channapatna / Shed A / Part 2",
              "evidence_summary": "Mobile proof submitted",
              "proof_summary": "Awaiting verifier review",
              "proof_state": "uploaded",
              "verification_state": "pending",
              "park_id": "park-1",
              "park_name": "Channapatna",
              "shed_id": "shed-1",
              "shed_name": "Shed A",
              "partition_label": "Part 2",
              "owner": {},
              "next_action": "Verifier to accept or reject proof",
              "evidence_link": "/workflows/alert-1",
              "obligation_id": "obligation-1"
            }
            """.trimIndent(),
        )

        assertEquals("Part 2", dto.partitionLabel)
        assertEquals("Channapatna / Shed A / Part 2", dto.scopeLabel)
        assertEquals("Mobile proof submitted", dto.evidenceSummary)
        assertEquals("Awaiting verifier review", dto.proofSummary)
        assertEquals("uploaded", dto.proofState)
        assertEquals("pending", dto.verificationState)
    }

    @Test
    fun `control tower alert decodes when optional partition is absent`() {
        val dto = json.decodeFromString<ControlTowerAlertDto>(
            """
            {
              "row_id": "alert-1",
              "severity": "watch",
              "work_state": "due",
              "title": "Due",
              "detail": "Shed A: due",
              "owner": {},
              "next_action": "Start scheduled vaccination SOP",
              "obligation_id": "obligation-1"
            }
            """.trimIndent(),
        )

        assertNull(dto.partitionLabel)
    }

    @Test
    fun `adherence row decodes openapi fields and mixed-version optional absence`() {
        val full = json.decodeFromString<AdherenceRowDto>(
            """
            {
              "row_id": "row-1",
              "shed_name": "Shed A",
              "partition_label": "Part 2",
              "expected": "10 expected",
              "actual": "7 completed",
              "gap": "3 open",
              "severity": "watch",
              "owner": {},
              "next_action": "Complete drive",
              "evidence": {},
              "work_state": "due",
              "drive_capacity_state": "within_cap",
              "drive_animals_required": 10,
              "drive_animals_assigned": 10,
              "drive_operator_cap": 150,
              "drive_available_operators": 1,
              "drive_latest_safe_date": "2026-07-28T00:00:00Z",
              "drive_medical_defer_reason": "pc defer"
            }
            """.trimIndent(),
        )
        val mixed = json.decodeFromString<AdherenceRowDto>(
            """{"row_id":"row-2","shed_name":"Shed B","expected":"","actual":"","gap":"","severity":"","next_action":"","evidence":{},"work_state":""}""",
        )

        assertEquals("Shed A", full.shedName)
        assertEquals("Part 2", full.partitionLabel)
        assertEquals("within_cap", full.driveCapacityState)
        assertEquals(150, full.driveOperatorCap)
        assertEquals(1, full.driveAvailableOperators)
        assertNull(mixed.partitionLabel)
        assertNull(mixed.driveCapacityState)
    }
}
