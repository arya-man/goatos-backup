package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import sg.mesha.goatos.feature.health.KID_CLASS_FATTENING
import sg.mesha.goatos.feature.health.KID_CLASS_MILK
import sg.mesha.goatos.feature.health.KID_CLASS_WEANING
import sg.mesha.goatos.feature.health.ObservationFormState
import sg.mesha.goatos.feature.health.kidFormClassForStage

/**
 * The kid rows must actually reach the wire — the review finding this pins was
 * a form that could never send `landing` or `refusals_today`, so valid kid
 * observations were rejected or under-specified before the kid registers ran.
 * The mapping is also slice-scoped exactly as the server validates it: landing
 * on a weaning kid is a REJECT server-side, so the DTO must omit, not blank.
 */
class ObservationKidFormMappingTest {

    private fun milkKid(stage: String) = ObservationFormState(
        kidClass = KID_CLASS_MILK,
        kidStage = stage,
        suckle = "present",
        responsiveness = "alert",
        navel = "wet",
        landing = "barely",
        milkIntake = setOf("not_drinking"),
        refusalsToday = "2",
        session = "1",
    )

    @Test
    fun `milk kid sends every kid row it was asked`() {
        val dto = milkKid("K2").toFindingsDto()
        assertEquals("present", dto.suckle)
        assertEquals("alert", dto.responsiveness)
        assertEquals("wet", dto.navel)
        assertEquals("barely", dto.landing)
        assertEquals(listOf("not_drinking"), dto.milkIntake)
        assertEquals(2, dto.refusalsToday)
        assertEquals(1, dto.session)
    }

    @Test
    fun `weaning kid never sends the milk-only rows`() {
        val dto = milkKid("").copy(kidClass = KID_CLASS_WEANING, kidStage = "K3").toFindingsDto()
        // The server rejects landing/navel on a weaning kid; absent, never blank.
        assertNull(dto.landing)
        assertNull(dto.navel)
        assertNull(dto.milkIntake)
        assertEquals("present", dto.suckle)
        assertEquals(2, dto.refusalsToday)
    }

    @Test
    fun `milk intake travels only from the K2 bar`() {
        assertNull(milkKid("K1").toFindingsDto().milkIntake)
        assertEquals(listOf("not_drinking"), milkKid("K2").toFindingsDto().milkIntake)
    }

    @Test
    fun `adult form sends no kid rows at all`() {
        val dto = milkKid("K1").copy(kidClass = "", kidStage = "").toFindingsDto()
        assertNull(dto.suckle)
        assertNull(dto.responsiveness)
        assertNull(dto.navel)
        assertNull(dto.landing)
        assertNull(dto.milkIntake)
        assertNull(dto.refusalsToday)
        assertNull(dto.session)
    }

    @Test
    fun `authored answers keep explicit normal yes-no values`() {
        val answers = ObservationFormState(
            temp = "102.4",
            eating = setOf("normal"),
            activity = "standing",
            breathing = setOf("normal"),
            nasal = false,
            leftStomach = setOf("normal"),
            frothyMouth = false,
            rumenMovement = "felt",
            diarrhea = false,
            skinTent = "2-4",
            famacha = "2",
            yellow = false,
            redUrine = false,
            bodyEdema = false,
            competition = false,
            stomachInside = false,
            mouth = "normal",
            eyes = setOf("normal"),
            lockedJaw = false,
            neuro = setOf("none"),
            rashCharacter = "none",
            hairloss = false,
            leg = "normal",
            lumps = "no",
            wounds = setOf("no"),
            flystrike = false,
            eartagFlystrike = false,
            eartagWound = false,
            ticks = false,
        ).toAuthoredAnswersJson()

        assertNotNull(answers)
        assertEquals("no", (answers!!["nasal"] as JsonPrimitive).contentOrNull)
        assertEquals("no", (answers["frothy_mouth"] as JsonPrimitive).contentOrNull)
        assertEquals("s2_4s", (answers["skin_tent"] as JsonPrimitive).contentOrNull)
        assertEquals("f2", (answers["famacha"] as JsonPrimitive).contentOrNull)
        assertEquals("no", ((answers["hairloss"] as JsonArray).first() as JsonPrimitive).contentOrNull)
    }

    @Test
    fun `authored answers cover every kid slice`() {
        val k1 = milkKid("K1").copy(refusalsToday = "0", milkIntake = emptySet()).toAuthoredAnswersJson()
        val weaning = milkKid("").copy(
            kidClass = KID_CLASS_WEANING,
            kidStage = "K3",
            refusalsToday = "1",
            milkIntake = emptySet(),
        ).toAuthoredAnswersJson()
        val k2 = milkKid("K2").toAuthoredAnswersJson()

        assertEquals("normal", ((k1!!["milk_intake"] as JsonArray).first() as JsonPrimitive).contentOrNull)
        assertEquals("not_drinking", ((weaning!!["milk_intake"] as JsonArray).first() as JsonPrimitive).contentOrNull)
        assertEquals("not_drinking", ((k2!!["milk_intake"] as JsonArray).first() as JsonPrimitive).contentOrNull)
    }

    @Test
    fun `stage mapping mirrors the server and fails closed`() {
        assertEquals(KID_CLASS_MILK to "K0", kidFormClassForStage("K0"))
        assertEquals(KID_CLASS_MILK to "K1", kidFormClassForStage("k1"))
        assertEquals(KID_CLASS_MILK to "K2", kidFormClassForStage(" K2 "))
        assertEquals(KID_CLASS_WEANING to "K3", kidFormClassForStage("K3"))
        assertEquals(KID_CLASS_FATTENING to "", kidFormClassForStage("F2-Male"))
        assertEquals(KID_CLASS_FATTENING to "", kidFormClassForStage("F2-Female"))
        // A clinical placement or blank stage names no register; the form stays
        // adult-shaped and the server refuses with its own farm-worded reason.
        assertNull(kidFormClassForStage("ICU-Kid"))
        assertNull(kidFormClassForStage(""))
        assertNull(kidFormClassForStage(null))
    }
}
