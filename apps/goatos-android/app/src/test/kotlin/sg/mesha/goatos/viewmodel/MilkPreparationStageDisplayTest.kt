package sg.mesha.goatos.viewmodel

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MilkPreparationStageDisplayTest {
    @Test
    fun `milk preparation lines use fattening display stage names`() {
        val source = File("src/main/kotlin/sg/mesha/goatos/viewmodel/MilkPreparationViewModel.kt").readText()

        assertTrue(source.contains("line.managementStage.toFatteningDisplayStage()"))
        assertFalse(source.contains("\"${'$'}{line.managementStage}:"))
    }
}
