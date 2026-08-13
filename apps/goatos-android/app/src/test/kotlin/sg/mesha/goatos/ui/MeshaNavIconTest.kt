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

    @Test
    fun feedPackingAndTransportUseDistinctOperationalIcons() {
        val packingIcon = MeshaIcons.forNavKey("feed_packing")
        val transportIcon = MeshaIcons.forNavKey("feed_transport")

        assertNotSame(MeshaIcons.Module, packingIcon)
        assertNotSame(MeshaIcons.Module, transportIcon)
        assertNotSame(packingIcon, transportIcon)
    }

    @Test
    fun feedShortTabKeysUseDistinctOperationalIcons() {
        assertSame(MeshaIcons.Feed, MeshaIcons.forNavKey("direction"))
        assertSame(MeshaIcons.Package, MeshaIcons.forNavKey("packing"))
        assertSame(MeshaIcons.Truck, MeshaIcons.forNavKey("transport"))
    }

    @Test
    fun milkPreparationAndFeedingUseDistinctMilkIcons() {
        val preparationIcon = MeshaIcons.forNavKey("milk_preparation")
        val feedingIcon = MeshaIcons.forNavKey("milk_feeding")

        assertSame(MeshaIcons.MilkPreparation, preparationIcon)
        assertSame(MeshaIcons.MilkFeeding, feedingIcon)
        assertNotSame(MeshaIcons.Module, preparationIcon)
        assertNotSame(MeshaIcons.Module, feedingIcon)
        assertNotSame(preparationIcon, feedingIcon)
    }

    /**
     * The Milk MODULE drawer row (maintainer decision 2026-07-31, split out of Counts) must carry
     * a milk glyph, not the neutral fallback tile every unmapped key lands on.
     */
    @Test
    fun milkModuleDrawerRowHasItsOwnIcon() {
        val moduleIcon = MeshaIcons.forNavKey("milk")

        assertSame(MeshaIcons.MilkPreparation, moduleIcon)
        assertNotSame(MeshaIcons.Module, moduleIcon)
    }
}
