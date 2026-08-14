package sg.mesha.goatos.feature.health

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The form's two jobs are to be COMPLETE and to be free of contradictions, and
 * both are enforced here rather than discovered on the server. A manager holding
 * a sick animal should not be told what is wrong with their form by a round trip.
 *
 * The four exclusivity rules mirror what the backend rejects. Duplicating them is
 * deliberate — the backend stays the authority, this is the fast feedback.
 */
// telemetry:exempt Unit test; not a user-facing screen
class ObservationFormTest {

    /** A fully answered form for a doe with nothing abnormal. */
    private fun completeDoe() = ObservationFormState(
        sex = "female",
        temp = "102.0",
        eating = setOf("normal"),
        activity = "standing",
        breathing = setOf("normal"),
        nasal = false,
        leftStomach = setOf("normal"),
        frothyMouth = false,
        rumenMovement = "felt",
        diarrhea = false,
        skinTent = "lt2",
        famacha = "2",
        yellow = false,
        eyes = setOf("normal"),
        mouth = "normal",
        lockedJaw = false,
        neuro = setOf("none"),
        leg = "normal",
        wounds = setOf("no"),
        lumps = "no",
        rashCharacter = "none",
        hairloss = false,
        ticks = false,
        flystrike = false,
        eartagFlystrike = false,
        eartagWound = false,
        redUrine = false,
        bodyEdema = false,
        competition = false,
        stomachInside = false,
        udder = "normal",
        lactation = "no",
        vulva = "none",
    )

    private fun completeBuck() = completeDoe().copy(
        sex = "male",
        udder = "", lactation = "", cmt = "", vulva = "",
        straining = "no",
    )

    @Test
    fun `a fully answered form is submittable`() {
        assertTrue("blockers: ${completeDoe().blockers()}", completeDoe().canSubmit())
        assertTrue("blockers: ${completeBuck().blockers()}", completeBuck().canSubmit())
    }

    // A blank cannot distinguish "nobody looked" from "normal", and the engine's
    // unexplained-findings channel depends on that distinction.
    @Test
    fun `a single unanswered field blocks submission`() {
        val missingTemp = completeDoe().copy(temp = "")
        assertFalse(missingTemp.canSubmit())
        assertTrue(ObservationBlocker.INCOMPLETE in missingTemp.blockers())
        assertTrue("temperature" in missingTemp.missingFields())
    }

    @Test
    fun `a non-numeric temperature is not an answer`() {
        assertTrue("temperature" in completeDoe().copy(temp = "warm").missingFields())
    }

    // Sex-hidden fields record N/A rather than blank: a buck is never asked about
    // an udder, and a doe is never asked about straining.
    @Test
    fun `sex-scoped fields are only required for the sex they apply to`() {
        assertTrue("a buck must not be asked about the udder", completeBuck().canSubmit())
        assertTrue("a doe must not be asked about straining", completeDoe().canSubmit())

        val buckMissingUrine = completeBuck().copy(straining = "")
        assertTrue("urine" in buckMissingUrine.missingFields())
        assertFalse("udder" in buckMissingUrine.missingFields())
    }

    // CMT is a milk test. It is required when there is milk and forbidden when
    // there is not.
    @Test
    fun `CMT is required only when there is milk`() {
        val milking = completeDoe().copy(lactation = "milk")
        assertTrue("a milking doe must have a CMT reading", "CMT" in milking.missingFields())
        assertTrue(milking.copy(cmt = "neg").canSubmit())

        val dry = completeDoe().copy(lactation = "no")
        assertFalse("a dry doe must not be asked for a CMT", "CMT" in dry.missingFields())
    }

    @Test
    fun `a CMT without milk is a contradiction, not a missing field`() {
        val form = completeDoe().copy(lactation = "no", cmt = "pos")
        assertTrue(ObservationBlocker.CMT_WITHOUT_MILK in form.blockers())
        assertFalse(form.canSubmit())
    }

    @Test
    fun `not eating cannot be combined with a feed the animal took`() {
        val form = completeDoe().copy(eating = setOf("not_eating", "concentrate"))
        assertTrue(ObservationBlocker.NOT_EATING_WITH_FEED in form.blockers())
        assertFalse(form.canSubmit())

        // Off feed on its own is a normal, submittable observation.
        assertTrue(completeDoe().copy(eating = setOf("not_eating")).canSubmit())
    }

    @Test
    fun `no wounds cannot be combined with a wound site`() {
        val form = completeDoe().copy(wounds = setOf("no", "body"))
        assertTrue(ObservationBlocker.WOUNDS_EXCLUSIVE in form.blockers())
        assertFalse(form.canSubmit())
    }

    @Test
    fun `a female cannot be recorded as straining`() {
        val form = completeDoe().copy(straining = "no_urine")
        assertTrue(ObservationBlocker.FEMALE_STRAINING in form.blockers())
        assertFalse(form.canSubmit())
    }

    // A 30-field form corrected one complaint per round trip is not fillable in a
    // shed, so every problem is reported at once.
    @Test
    fun `every problem is reported together`() {
        val form = completeDoe().copy(
            temp = "",
            eating = setOf("not_eating", "normal"),
            wounds = setOf("no", "legs"),
        )
        val blockers = form.blockers()
        assertTrue(ObservationBlocker.INCOMPLETE in blockers)
        assertTrue(ObservationBlocker.NOT_EATING_WITH_FEED in blockers)
        assertTrue(ObservationBlocker.WOUNDS_EXCLUSIVE in blockers)
    }

    // Picking a real finding clears "normal" and vice versa, so the operator
    // cannot easily build the contradiction in the first place.
    @Test
    fun `the nothing-abnormal option is exclusive`() {
        val clears = setOf("normal")

        val afterFinding = toggleMultiValue(setOf("normal"), "red", clears)
        assertEquals("picking a finding must clear normal", setOf("red"), afterFinding)

        val afterNormal = toggleMultiValue(setOf("red", "discharge"), "normal", clears)
        assertEquals("picking normal must clear the findings", setOf("normal"), afterNormal)

        val twoFindings = toggleMultiValue(setOf("red"), "discharge", clears)
        assertEquals("two real findings coexist", setOf("red", "discharge"), twoFindings)

        val deselected = toggleMultiValue(setOf("red", "discharge"), "red", clears)
        assertEquals("tapping a selected value clears it", setOf("discharge"), deselected)
    }

    // The form records what is seen. It must never name a disease -- the engine
    // proposes and the Director confirms.
    @Test
    fun `the form has no disease field`() {
        val fields = ObservationFormState::class.java.declaredFields.map { it.name.lowercase() }
        val named = fields.filter { it.contains("disease") || it.contains("diagnos") }
        assertTrue("the observation form must not name a disease, found: $named", named.isEmpty())
    }
}
