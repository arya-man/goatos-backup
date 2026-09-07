package sg.mesha.goatos.push

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ForceUpdatePushTest {
    @Test
    fun `dev flavor does not subscribe to force update topic`() {
        assertFalse(forceUpdateTopicEnabled("dev"))
    }

    @Test
    fun `only prod flavor subscribes to production force update topic`() {
        assertFalse(forceUpdateTopicEnabled("stg"))
        assertTrue(forceUpdateTopicEnabled("prod"))
    }
}
