package sg.mesha.goatos.feature.weighing

import org.junit.Test
import sg.mesha.goatos.feature.weighing.WeighingExportPreviewRowUi
import sg.mesha.goatos.feature.weighing.WeighingExportPreviewShedUi

/**
 * Tests for [WeighingExportPreviewShedUi] to verify lump-sum vs individual row
 * counting and average calculation logic.
 */
class WeighingExportPreviewShedUiTest {

    @Test
    fun `lump sum shed shows animal count not row count`() {
        // A shed with one lump-sum row representing 12 animals
        val lumpSumRow = WeighingExportPreviewRowUi(
            type = "lump-sum",
            scannedIdentifier = "", // blank for lump-sum
            weightKg = "120.0",
            averageWeightKg = "10.0", // stored average for the batch
            animalCount = "12",
            verificationStatus = "accepted",
            proofReferenceType = "video",
            proofReference = "video-123",
            recordedAt = "2026-08-05T10:00:00Z",
            dateIst = "2026-08-05",
            timeIst = "15:30",
        )
        val shed = WeighingExportPreviewShedUi(
            shedKey = "shed-1",
            park = "Main Park",
            shedName = "Gandhi 2",
            shedStatus = "accepted",
            rows = listOf(lumpSumRow),
        )

        // Weighed count should be 12 (animal count from lump-sum row), not 1 (row count)
        assert(shed.weighedCount == 12) {
            "Expected weighed count 12, got ${shed.weighedCount}"
        }

        // Average should be the stored average (10.0), not total divided by row count (120.0)
        assert(shed.averageWeightKg == 10.0) {
            "Expected average 10.0 kg, got ${shed.averageWeightKg}"
        }
    }

    @Test
    fun `individual sheds count scans`() {
        // A shed with three individual scans
        val scan1 = WeighingExportPreviewRowUi(
            type = "individual",
            scannedIdentifier = "RFID-001",
            weightKg = "25.5",
            averageWeightKg = "25.5",
            animalCount = "1",
            verificationStatus = "accepted",
            proofReferenceType = "video",
            proofReference = "video-1",
            recordedAt = "2026-08-05T10:00:00Z",
            dateIst = "2026-08-05",
            timeIst = "10:00",
        )
        val scan2 = WeighingExportPreviewRowUi(
            type = "individual",
            scannedIdentifier = "RFID-002",
            weightKg = "30.0",
            averageWeightKg = "30.0",
            animalCount = "1",
            verificationStatus = "accepted",
            proofReferenceType = "video",
            proofReference = "video-2",
            recordedAt = "2026-08-05T10:05:00Z",
            dateIst = "2026-08-05",
            timeIst = "10:05",
        )
        val scan3 = WeighingExportPreviewRowUi(
            type = "individual",
            scannedIdentifier = "RFID-003",
            weightKg = "28.5",
            averageWeightKg = "28.5",
            animalCount = "1",
            verificationStatus = "accepted",
            proofReferenceType = "video",
            proofReference = "video-3",
            recordedAt = "2026-08-05T10:10:00Z",
            dateIst = "2026-08-05",
            timeIst = "10:10",
        )
        val shed = WeighingExportPreviewShedUi(
            shedKey = "shed-2",
            park = "Main Park",
            shedName = "Individual Shed",
            shedStatus = "accepted",
            rows = listOf(scan1, scan2, scan3),
        )

        // Weighed count should be 3 (number of scans)
        assert(shed.weighedCount == 3) {
            "Expected weighed count 3, got ${shed.weighedCount}"
        }

        // Average should be calculated from the three scans: (25.5 + 30.0 + 28.5) / 3 = 28.0
        val expectedAverage = (25.5 + 30.0 + 28.5) / 3
        assert(shed.averageWeightKg == expectedAverage) {
            "Expected average $expectedAverage kg, got ${shed.averageWeightKg}"
        }
    }

    @Test
    fun `mixed shed with lump sum and individual uses individual average`() {
        // A shed with both lump-sum and individual rows
        val lumpSumRow = WeighingExportPreviewRowUi(
            type = "lump-sum",
            scannedIdentifier = "",
            weightKg = "120.0",
            averageWeightKg = "10.0",
            animalCount = "12",
            verificationStatus = "accepted",
            proofReferenceType = "video",
            proofReference = "video-lump",
            recordedAt = "2026-08-05T09:00:00Z",
            dateIst = "2026-08-05",
            timeIst = "14:30",
        )
        val individualRow = WeighingExportPreviewRowUi(
            type = "individual",
            scannedIdentifier = "RFID-001",
            weightKg = "35.0",
            averageWeightKg = "35.0",
            animalCount = "1",
            verificationStatus = "accepted",
            proofReferenceType = "video",
            proofReference = "video-ind",
            recordedAt = "2026-08-05T10:00:00Z",
            dateIst = "2026-08-05",
            timeIst = "15:30",
        )
        val shed = WeighingExportPreviewShedUi(
            shedKey = "shed-3",
            park = "Main Park",
            shedName = "Mixed Shed",
            shedStatus = "accepted",
            rows = listOf(lumpSumRow, individualRow),
        )

        // Weighed count includes both: 12 (from lump-sum) + 1 (from individual) = 13
        assert(shed.weighedCount == 13) {
            "Expected weighed count 13, got ${shed.weighedCount}"
        }

        // Average should use only individual row: 35.0
        // (lump-sum rows are ignored in the average calculation)
        assert(shed.averageWeightKg == 35.0) {
            "Expected average 35.0 kg (from individual row only), got ${shed.averageWeightKg}"
        }
    }
}
