package sg.mesha.goatos.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Path
import kotlin.io.path.readText

class ScanProofIdentityTest {
    @Test
    fun `vaccination proof-needed rows key by animal not vaccine obligation`() {
        val screen = Path.of("../feature/feature-scan/src/main/kotlin/sg/mesha/goatos/feature/scan/ScanScreen.kt").readText()
        val proofRows = screen.substringAfter("if (state.proofActionNeeded.isNotEmpty())")
            .substringBefore("val proofActionTags")

        assertTrue(
            "proof-needed rows are animal-grain; one RFID animal row may show multiple vaccine labels",
            proofRows.contains("listOf(row.goatId, row.primaryTag, row.secondaryTag.orEmpty())"),
        )
        assertFalse(
            "visible proof-needed rows must not split one animal into vaccine-obligation rows",
            proofRows.contains("row.obligationId.takeIf { it.isNotBlank() }"),
        )
    }
}
