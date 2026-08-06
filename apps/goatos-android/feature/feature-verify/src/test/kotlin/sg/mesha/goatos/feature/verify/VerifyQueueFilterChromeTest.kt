package sg.mesha.goatos.feature.verify

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Two filter-chrome rules the verifier queue must hold.
 *
 * 1. The category chips ARE the verifier's modules. When the shell already carries them in the
 *    drawer, repeating them inside the body is the same list twice and the two can disagree about
 *    what is selected. Same shape as the "You" placement rule: 2+ modules -> the drawer owns the
 *    module switch; exactly one module -> no drawer exists, so the chips are the only affordance.
 *
 * 2. A chip row only works while the whole set fits on a phone. A real park holds many sheds, so
 *    past a small threshold the row becomes a horizontal drag with the selected chip potentially
 *    off-screen -- past it, the same options are offered as a searchable picker.
 */
class VerifyQueueFilterChromeTest {

    @Test
    fun `module chips are suppressed when the drawer already lists the modules`() {
        assertFalse(
            "a verifier with a drawer must not see the same modules twice",
            shouldShowCategoryFilter(drawerCarriesModules = true, optionCount = 2),
        )
    }

    @Test
    fun `module chips remain for a single-module verifier who has no drawer`() {
        assertTrue(
            "without a drawer the chips are the only way to see the scope",
            shouldShowCategoryFilter(drawerCarriesModules = false, optionCount = 2),
        )
    }

    @Test
    fun `no chip row is drawn when the backend offered no categories`() {
        assertFalse(shouldShowCategoryFilter(drawerCarriesModules = false, optionCount = 0))
    }

    @Test
    fun `a small option set stays as chips and a large one becomes searchable`() {
        assertFalse("a handful of sheds is scannable as chips", shouldUseSearchablePicker(CHIP_ROW_MAX_OPTIONS))
        assertTrue("past the threshold a chip row hides options off-screen", shouldUseSearchablePicker(CHIP_ROW_MAX_OPTIONS + 1))
    }

    @Test
    fun `search matches case-insensitively on the label`() {
        val options = listOf(
            VerifyLocationFilterOption(value = null, label = "All sheds"),
            VerifyLocationFilterOption(value = "s1", label = "Gandhi 1"),
            VerifyLocationFilterOption(value = "s2", label = "Godel 1 - Part 3"),
            VerifyLocationFilterOption(value = "s3", label = "Castro 2"),
        )

        assertEquals(listOf("Gandhi 1"), filterLocationOptions(options, "gandhi").map { it.label })
        assertEquals(listOf("Godel 1 - Part 3"), filterLocationOptions(options, "part 3").map { it.label })
        // A partition label must remain findable by its parent shed name.
        assertEquals(listOf("Godel 1 - Part 3"), filterLocationOptions(options, "Godel").map { it.label })
    }

    @Test
    fun `an empty or blank query returns every option untouched`() {
        val options = listOf(
            VerifyLocationFilterOption(value = null, label = "All sheds"),
            VerifyLocationFilterOption(value = "s1", label = "Gandhi 1"),
        )

        assertEquals(options, filterLocationOptions(options, ""))
        assertEquals(options, filterLocationOptions(options, "   "))
    }
}
