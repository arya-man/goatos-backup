package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.VendorCatalogEntryDto
import sg.mesha.goatos.core.network.dto.VendorDto
import sg.mesha.goatos.core.network.dto.VendorFormPageDto
import sg.mesha.goatos.core.network.dto.VendorQuestionDto
import sg.mesha.goatos.core.network.dto.VendorQuestionOnlyIfDto

/**
 * VENDOR FORM IS AUTHORED (2026-09-19): the wizard renders the published form and its answers
 * reach the write the way the backend checks them -- every asked answer, blank included, with the
 * version; a follow-up hidden by its condition dropped; typed columns the form does not ask
 * carried from the stored row on edit; the cheapest refusals shown before the write.
 */
class VendorFormAnswersTest {
    @Test
    fun `new form disabling Other drops the saved explanation from submission`() {
        val pages = listOf(VendorFormPageDto(key = "p", title = "Page", questions = listOf(
            VendorQuestionDto(id = "transport", kind = "choice", title = "Transport", allowOther = false,
                options = listOf(VendorCatalogEntryDto("yes", "Yes"), VendorCatalogEntryDto("no", "No"))),
        )).toUi())
        val edited = mapOf("transport" to "yes", "transport_other" to "Saved old explanation")
        assertEquals(mapOf("transport" to "yes"), vendorAnswersToWrite(edited, pages, 4).answers)
    }

    @Test
    fun `blank condition never activates even when optional parent is blank`() {
        val pages = listOf(VendorFormPageDto(key = "p", title = "Page", questions = listOf(
            VendorQuestionDto(id = "a", kind = "choice", title = "A"),
            VendorQuestionDto(id = "b", kind = "text", title = "B", required = true, onlyIf = VendorQuestionOnlyIfDto("a", "")),
        )).toUi())
        for (answers in listOf(mapOf("b" to "stale"), mapOf("a" to "", "b" to "stale"))) {
            val visible = visibleVendorAnswers(pages, answers)
            assertFalse(visible.containsKey("b"))
            assertTrue(validateVendorFormPage(pages.first(), visible).isEmpty())
        }
    }

    @Test
    fun `hidden cross page branches cannot reactivate from stale answers`() {
        val yesNo = listOf(VendorCatalogEntryDto("yes", "Yes"), VendorCatalogEntryDto("no", "No"))
        val pages = listOf(
            VendorFormPageDto(key = "parent", title = "Parent", questions = listOf(
                VendorQuestionDto(id = "a", kind = "choice", title = "A", typed = true, options = yesNo),
            )),
            VendorFormPageDto(key = "child", title = "Child", questions = listOf(
                VendorQuestionDto(id = "b", kind = "choice", title = "B", options = yesNo, onlyIf = VendorQuestionOnlyIfDto("a", "yes")),
            )),
            VendorFormPageDto(key = "grandchild", title = "Grandchild", questions = listOf(
                VendorQuestionDto(id = "c", kind = "text", title = "C", required = true, onlyIf = VendorQuestionOnlyIfDto("b", "yes")),
            )),
        ).map { it.toUi() }
        val stale = mapOf("a" to "no", "b" to "yes", "c" to "old detail")
        val visible = visibleVendorAnswers(pages, stale)
        assertEquals(mapOf("a" to "no"), visible)
        assertTrue(pages.all { validateVendorFormPage(it, visible).isEmpty() })
        assertEquals(mapOf("a" to "no"), vendorAnswersToWrite(stale, pages, 2).answers)
        val reopened = visibleVendorAnswers(pages, stale + ("a" to "yes"))
        assertEquals("old detail", reopened["c"])
    }


    private val form = listOf(
        VendorFormPageDto(
            key = "who", title = "Who they are",
            questions = listOf(
                VendorQuestionDto(id = "business_name", kind = "text", title = "Name", required = true, typed = true),
                VendorQuestionDto(id = "record_type", kind = "choice", title = "Type", required = true, typed = true, options = listOf(VendorCatalogEntryDto("Agent", "Agent"), VendorCatalogEntryDto("Old", "Old", isActive = false))),
                VendorQuestionDto(id = "state", kind = "choice", title = "State", required = true, typed = true, options = listOf(VendorCatalogEntryDto("KA", "Karnataka"))),
                VendorQuestionDto(id = "status", kind = "choice", title = "Status", required = true, typed = true, options = listOf(VendorCatalogEntryDto("active", "Active"))),
            ),
        ),
        VendorFormPageDto(
            key = "extra", title = "Extra",
            questions = listOf(
                VendorQuestionDto(id = "transport", kind = "choice", title = "Own transport?", required = true, options = listOf(VendorCatalogEntryDto("yes", "Yes"), VendorCatalogEntryDto("no", "No"))),
                VendorQuestionDto(id = "vehicles", kind = "number", title = "Vehicles", required = true, min = 1.0, max = 50.0, onlyIf = VendorQuestionOnlyIfDto("transport", "yes")),
                VendorQuestionDto(id = "price_per_goat", kind = "number", title = "Price", typed = true),
            ),
        ),
    ).map { it.toUi() }

    private val good = mapOf("business_name" to "Bhopal Goat", "record_type" to "Agent", "state" to "KA", "status" to "active", "transport" to "yes", "vehicles" to "3")

    @Test
    fun `a retired catalog entry is never offered`() {
        assertEquals(listOf("Agent"), form[0].questions[1].options.map { it.value })
    }

    @Test
    fun `every asked answer travels with the version and typed columns are filled from the same answers`() {
        val write = vendorAnswersToWrite(good, form, version = 2)
        assertEquals(2, write.questionnaireVersion)
        assertEquals("Bhopal Goat", write.businessName)
        assertEquals("Agent", write.recordType)
        assertEquals("KA", write.state)
        assertEquals("3", write.answers?.get("vehicles"))
        // The unanswered optional typed question is still SENT, blank: the write is a replace.
        assertEquals("", write.answers?.get("price_per_goat"))
        assertNull(write.pricePerGoat)
    }

    @Test
    fun `a follow-up hidden by its condition is dropped from the write`() {
        val write = vendorAnswersToWrite(good + ("transport" to "no"), form, version = 2)
        assertFalse(write.answers!!.containsKey("vehicles"))
        assertEquals("no", write.answers?.get("transport"))
    }

    @Test
    fun `validation mirrors the backend's cheapest refusals per page`() {
        assertTrue(validateVendorFormPage(form[0], good).isEmpty())
        assertEquals("Required", validateVendorFormPage(form[0], good - "business_name")["business_name"])
        assertEquals("Pick one of the choices", validateVendorFormPage(form[0], good + ("record_type" to "Old"))["record_type"])
        assertEquals("Must be at most 50", validateVendorFormPage(form[1], good + ("vehicles" to "51"))["vehicles"])
        // Hidden by its condition: not owed even though required.
        assertTrue(validateVendorFormPage(form[1], good + ("transport" to "no") - "vehicles").isEmpty())
    }

    @Test
    fun `on edit a typed column the form does not ask rides from the stored row`() {
        val stored = VendorDto(vendorId = "v1", businessName = "Old", recordType = "Agent", state = "KA", status = "active", city = "Mysuru", comments = "keep", rowVersion = 4, answers = mapOf("transport" to "yes"))
        val write = vendorAnswersToWrite(good, form, version = 2).carryingUnaskedVendorColumnsOf(stored, form)
        assertEquals("Mysuru", write.city)
        assertEquals("keep", write.comments)
        assertEquals(4L, write.rowVersion)
        // The stored row as answers prefills typed columns AND the extras.
        val prefill = stored.toVendorAnswers()
        assertEquals("Old", prefill["business_name"])
        assertEquals("yes", prefill["transport"])
    }
}
