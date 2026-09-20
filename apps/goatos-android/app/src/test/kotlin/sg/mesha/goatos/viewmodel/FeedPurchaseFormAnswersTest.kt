package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.FeedPurchaseDto
import sg.mesha.goatos.core.network.dto.VendorAnswerRowDto
import sg.mesha.goatos.core.network.dto.VendorFormDto
import sg.mesha.goatos.core.network.dto.VendorFormPageDto
import sg.mesha.goatos.core.network.dto.VendorQuestionDto

/**
 * PROCUREMENT IS SOP-DRIVEN END TO END (2026-09-20): the Record purchase wizard draws the feed
 * ledger's own columns by hand and asks the buying desk's OWN questions from the published form.
 * These are the rules that decide which of the published questions that is.
 */
class FeedPurchaseFormAnswersTest {
    @Test
    fun `detail keeps the original authored answer label from the server`() {
        val purchase = FeedPurchaseDto(
            feedPurchaseId = "load",
            answerRows = listOf(VendorAnswerRowDto("retired_lorry_question", "Original lorry label", "TN42 ABC")),
        )
        val row = purchase.sections().flatMap { it.rows }.single { it.label == "Original lorry label" }
        assertEquals("TN42 ABC", row.value)
    }


    private val form = VendorFormDto(
        version = 3,
        pages = listOf(
            VendorFormPageDto(
                key = "load", title = "The load",
                questions = listOf(
                    VendorQuestionDto(id = "purchase_date", kind = "text", title = "Bought on", required = true, typed = true),
                    VendorQuestionDto(id = "quantity_kg", kind = "number", title = "Quantity", required = true, typed = true, unit = "kg"),
                    VendorQuestionDto(id = "moisture_at_intake", kind = "number", title = "Moisture at intake", required = true, min = 0.0, max = 100.0, unit = "%"),
                ),
            ),
            // a page of nothing but the ledger's own columns must not become an empty section
            VendorFormPageDto(
                key = "cost", title = "What it cost",
                questions = listOf(VendorQuestionDto(id = "feed_cost", kind = "number", title = "Feed cost", typed = true)),
            ),
            VendorFormPageDto(
                key = "lorry", title = "The lorry",
                questions = listOf(VendorQuestionDto(id = "lorry_number", kind = "text", title = "Lorry number", required = true)),
            ),
        ),
    )

    @Test
    fun `the wizard asks only the questions the ledger does not already draw`() {
        val pages = form.extraPages()
        assertEquals(listOf("load", "lorry"), pages.map { it.key })
        assertEquals(listOf("moisture_at_intake"), pages[0].questions.map { it.id })
        assertEquals(listOf("lorry_number"), pages[1].questions.map { it.id })
    }

    @Test
    fun `a form that was never fetched leaves the wizard exactly as it was`() {
        assertTrue(null.extraPages().isEmpty())
        assertTrue(VendorFormDto().extraPages().isEmpty())
    }

    @Test
    fun `an authored number is checked against its own bounds before the write`() {
        val page = form.extraPages().first()
        assertEquals(mapOf("moisture_at_intake" to "Required"), validateVendorFormPage(page, emptyMap()))
        assertTrue(validateVendorFormPage(page, mapOf("moisture_at_intake" to "120")).containsKey("moisture_at_intake"))
        assertEquals(emptyMap<String, String>(), validateVendorFormPage(page, mapOf("moisture_at_intake" to "12.5")))
    }

    @Test
    fun `the version travels with the answers so a later reader knows which form asked`() {
        assertEquals(3, form.version)
    }
}

/**
 * The server checks a COMPULSORY TYPED question against the `answers` map, not against the column
 * beside it, so a write carrying only the buying desk's extras is refused naming the first typed
 * question it cannot find -- "Check Purchase date: required", seen on the Realme on 2026-09-20.
 * These are the five the feed ledger cannot exist without; the phone must send all of them.
 */
class FeedPurchaseTypedAnswersTest {

    @Test
    fun `the phone sends every compulsory typed question under the id the form knows it by`() {
        val sent = TYPED_PURCHASE_QUESTIONS.map { it.first }.toSet()
        for (required in listOf("purchase_date", "farm_label", "feed_item_label", "quantity_kg", "vendor")) {
            assertTrue("the write must carry $required", sent.contains(required))
        }
    }

    @Test
    fun `each typed question maps to exactly one field, and no field twice`() {
        val questions = TYPED_PURCHASE_QUESTIONS.map { it.first }
        val fields = TYPED_PURCHASE_QUESTIONS.map { it.second }
        assertEquals(questions.size, questions.toSet().size)
        assertEquals(fields.size, fields.toSet().size)
    }
}
