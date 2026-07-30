package sg.mesha.goatos.viewmodel

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import sg.mesha.goatos.core.network.dto.GoatSearchItemDto

class HealthGoatAgeBandValidationTest {
    @Test
    fun `Adults Health cannot queue a kid goat under an adult protocol`() {
        val kid = GoatSearchItemDto(goatId = "goat-kid", displayId = "G-000326", ageBand = "kid")

        assertFalse(healthGoatMatchesAgeBand(kid, "adult"))
        assertTrue(healthGoatMatchesAgeBand(kid, "kid"))
    }
}
