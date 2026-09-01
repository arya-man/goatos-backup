package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
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
