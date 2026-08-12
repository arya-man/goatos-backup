package sg.mesha.goatos.push

import org.junit.Assert.assertEquals
import org.junit.Test

class PushDisplayTextTest {
    @Test
    fun `data display text wins over generic notification block`() {
        val display = pushDisplayText(
            data = mapOf(
                PushExtras.TITLE to "PPR overdue for Gandhi Park",
                PushExtras.BODY to "3 goats need vaccine today",
                "message_key" to "vaccination.reminder.overdue",
            ),
            notificationTitle = "Vaccination",
            notificationBody = "There is a vaccination update for you. Open the app for details.",
            defaultTitle = "Mesha",
        )

        assertEquals("PPR overdue for Gandhi Park", display.title)
        assertEquals("3 goats need vaccine today", display.body)
    }

    @Test
    fun `blank data falls back to notification and default title`() {
        val display = pushDisplayText(
            data = mapOf(
                PushExtras.TITLE to " ",
                PushExtras.BODY to "\t",
            ),
            notificationTitle = " ",
            notificationBody = "Open the app for details.",
            defaultTitle = "Mesha",
        )

        assertEquals("Mesha", display.title)
        assertEquals("Open the app for details.", display.body)
    }
}
