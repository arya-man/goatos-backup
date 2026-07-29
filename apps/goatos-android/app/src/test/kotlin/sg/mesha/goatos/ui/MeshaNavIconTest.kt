package sg.mesha.goatos.ui

import org.junit.Assert.assertNotSame
import org.junit.Assert.assertSame
import org.junit.Test
import sg.mesha.goatos.core.designsystem.icon.MeshaIcons

class MeshaNavIconTest {

    @Test
    fun birthAndDeathUseDistinctLifecycleIcons() {
        val birthIcon = MeshaIcons.forNavKey("birth")
        val deathIcon = MeshaIcons.forNavKey("death")

        assertSame(MeshaIcons.Birth, birthIcon)
        assertSame(MeshaIcons.Death, deathIcon)
        assertNotSame(birthIcon, deathIcon)
        assertSame(MeshaIcons.ArrowUpDown, MeshaIcons.forNavKey("birth_death"))
    }
}
