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

    // --- Steps.
    //
    // Splitting the form into steps must not change WHAT it requires. These pin the
    // one way that could silently happen: a field that ends up in no step at all is
    // then required by nothing, and the form would submit without it while every
    // other test still passed.

    /**
     * The whole inventory, written out by hand.
     *
     * This literal is the point of the test. Asserting that the per-step lists add
     * up to [missingFields] would prove nothing, because [missingFields] is now
     * BUILT from those lists — drop a field from its step and both sides lose it
     * together, and the assertion stays green while the form quietly stops
     * requiring it. An independent list is the only thing that catches that.
     *
     * If a question is deliberately added or removed, this list changes in the same
     * commit. That is the intended cost.
     */
    private val everyFemaleField = setOf(
        // Vitals
        "temperature", "FAMACHA", "yellow membranes", "skin tent", "sunken flank",
        // Head
        "eyes", "mouth", "frothy mouth", "locked jaw", "breathing", "nasal discharge",
        // Gut
        "left stomach", "rumen movement", "loose motion", "eating",
        // Skin, body, legs
        "ticks", "hair loss", "wounds", "lumps", "rashes",
        "maggots", "ear tag maggots", "ear tag wound",
        "activity", "legs", "nervous signs",
        // Female + whole-body
        "udder", "milk", "vulva", "red urine", "swelling under the jaw", "pushed off feed",
    )

    @Test
    fun `every question is still required after the split into steps`() {
        val blankDoe = ObservationFormState(sex = "female")
        assertEquals(
            "the form must require exactly the questions it asks",
            everyFemaleField, blankDoe.missingFields().toSet(),
        )

        // A buck is asked about urine instead of the three female questions.
        val blankBuck = ObservationFormState(sex = "male")
        assertEquals(
            everyFemaleField - setOf("udder", "milk", "vulva") + "urine",
            blankBuck.missingFields().toSet(),
        )
    }

    @Test
    fun `every required field belongs to exactly one step`() {
        val blank = ObservationFormState(sex = "female")
        val perStep = ObservationStep.entries.map { blank.missingFields(it) }
        val union = perStep.flatten()

        assertEquals(
            "a field claimed by two steps would be asked twice: ${union.groupBy { it }.filterValues { it.size > 1 }.keys}",
            union.size, union.toSet().size,
        )
        // A step holding nothing means its heading renders over an empty page.
        perStep.forEachIndexed { index, fields ->
            assertTrue("${ObservationStep.entries[index]} requires nothing", fields.isNotEmpty())
        }
    }

    @Test
    fun `a form answered in every step is submittable`() {
        val doe = completeDoe()
        ObservationStep.entries.forEach { step ->
            assertTrue("$step: ${doe.missingFields(step)}", doe.isStepComplete(step))
        }
        assertTrue(doe.canSubmit())
    }

    // The step a field lives in is what decides where the operator is held. A blank
    // udder must stop the FINAL step, not the vitals.
    @Test
    fun `an unanswered field blocks only its own step`() {
        val cases = mapOf(
            ObservationStep.VITALS to completeDoe().copy(temp = ""),
            ObservationStep.HEAD to completeDoe().copy(nasal = null),
            ObservationStep.BODY to completeDoe().copy(leg = ""),
            ObservationStep.FINAL to completeDoe().copy(udder = ""),
        )
        cases.forEach { (owning, form) ->
            ObservationStep.entries.forEach { step ->
                if (step == owning) {
                    assertFalse("$owning must be held open", form.isStepComplete(step))
                } else {
                    assertTrue("$step must not be blocked by a gap in $owning", form.isStepComplete(step))
                }
            }
        }
    }

    // The last step's button submits the WHOLE form, so a contradiction two steps
    // back must still stop it -- otherwise stepping past it would launder it.
    @Test
    fun `a contradiction in an earlier step still blocks submission from the last step`() {
        val contradictory = completeDoe().copy(eating = setOf("not_eating", "green_feed"))

        assertTrue(
            "the contradiction is in the body step, which is otherwise answered",
            contradictory.isStepComplete(ObservationStep.BODY),
        )
        assertTrue(contradictory.isStepComplete(ObservationStep.FINAL))
        assertFalse("a complete but contradictory form must not submit", contradictory.canSubmit())
    }

    // A milk test recorded and then contradicted must stay REACHABLE. The screen
    // only shows the CMT card while there is milk, so a form left holding
    // lactation="no" with a CMT reading is a dead end: the contradiction is named,
    // and the control that could clear it is no longer on screen. Answering "no
    // milk" therefore has to drop the reading with it.
    @Test
    fun `answering no milk leaves no unreachable milk test behind`() {
        val milking = completeDoe().copy(lactation = "milk", cmt = "pos")
        assertTrue(milking.canSubmit())

        // What the screen does when "No milk" is picked.
        val driedOff = milking.copy(lactation = "no", cmt = "")
        assertFalse("the milk test card is hidden once there is no milk", driedOff.cmtApplies)
        assertFalse(
            "a dried-off doe must not be left in contradiction",
            ObservationBlocker.CMT_WITHOUT_MILK in driedOff.blockers(),
        )
        assertTrue("blockers: ${driedOff.blockers()}", driedOff.canSubmit())
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
