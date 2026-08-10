package sg.mesha.goatos.ui

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Path
import kotlin.io.path.readText

class ScanProofIdentityTest {
    @Test
    fun `vaccination proof-needed rows key by obligation before goat`() {
        val screen = Path.of("../feature/feature-scan/src/main/kotlin/sg/mesha/goatos/feature/scan/ScanScreen.kt").readText()
        val proofRows = screen.substringAfter("if (state.proofActionNeeded.isNotEmpty())")
            .substringBefore("val proofActionTags")

        assertTrue(
            "proof-needed rows are obligation-grain; a goat may have multiple vaccine obligations",
            proofRows.contains("row.obligationId.takeIf { it.isNotBlank() }"),
        )
        assertTrue(
            "fallback identity must still separate two same-goat vaccine rows when obligationId is absent",
            proofRows.contains("listOf(row.goatId, row.vaccineLabel, row.primaryTag)"),
        )
        assertFalse(
            "goatId-first proof keys crash when one goat has multiple due vaccines",
            proofRows.contains("row.goatId.takeIf { it.isNotBlank() } ?: row.obligationId"),
        )
    }
}
