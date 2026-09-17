package sg.mesha.goatos.feature.verify

import org.junit.Assert.assertEquals
import org.junit.Test

class VerifyProofCountsTest {
    private fun row(vararg mimes: String?) = VerificationQueueRow(
        id = "row",
        category = "weighing_proof",
        categoryLabel = "Weighing",
        title = "Godel 2 - Part 1",
        subtitle = "",
        statusTone = VerifyTone.PENDING,
        proofCounts = VerifyProofCounts.ofMimeTypes(mimes.toList()),
    )

    @Test
    fun `each kind is named from the mime and singular where it is one`() {
        assertEquals("2 videos · 1 photo", verifyProofCountLabel(VerifyProofCounts.ofMimeTypes(listOf("video/mp4", "video/mp4", "image/jpeg"))))
        assertEquals("1 video", verifyProofCountLabel(VerifyProofCounts.ofMimeTypes(listOf("video/mp4"))))
        assertEquals("3 photos", verifyProofCountLabel(VerifyProofCounts.ofMimeTypes(listOf("image/jpeg", "image/png", "IMAGE/JPEG"))))
        assertEquals("1 video · 1 proof", verifyProofCountLabel(VerifyProofCounts.ofMimeTypes(listOf("video/mp4", ""))))
        assertEquals("", verifyProofCountLabel(VerifyProofCounts.ofMimeTypes(emptyList())))
    }

    @Test
    fun `the pen group header sums its rows' own counts`() {
        val counts = verifyGroupProofCounts(listOf(row("video/mp4", "video/mp4", "image/jpeg")))
        assertEquals(VerifyProofCounts(videos = 2, photos = 1), counts)
        assertEquals(
            VerifyProofCounts(videos = 3, photos = 1, other = 1),
            verifyGroupProofCounts(listOf(row("video/mp4", "video/mp4", "image/jpeg"), row("video/mp4", null))),
        )
    }

    @Test
    fun `the pen group header says one row, never 1 rows`() {
        val strings = java.io.File("src/main/res/values/strings.xml").readText()
        val rows = Regex("""<plurals name="verify_shed_group_rows">(.*?)</plurals>""", RegexOption.DOT_MATCHES_ALL)
            .find(strings)?.groupValues?.get(1).orEmpty()
        assertEquals(true, rows.contains("""<item quantity="one">%1${'$'}d row</item>"""))
        assertEquals(true, rows.contains("""<item quantity="other">%1${'$'}d rows</item>"""))
        assertEquals("the old always-plural summary is gone", false, strings.contains("verify_shed_group_summary"))
    }
}
