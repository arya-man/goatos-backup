package sg.mesha.goatos.core.designsystem.icon

import androidx.compose.ui.graphics.vector.ImageVector
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Test

/**
 * Pins [MeshaIcons.forNavKey] against the real backend module-nav-key set
 * (`moduleNavRegistry` in backend/internal/workforce/app/bootstrap_copy.go). Prevents three
 * production bugs that Paparazzi screenshot tests did not catch, because a wrong-but-stable
 * pixel just gets baked into the golden:
 *
 *  1. The verifier's drawer showed Vaccination / Weighing / Herd Operations all wearing the
 *     generic 4-square [MeshaIcons.Module] fallback tile, because `forNavKey` only knew the
 *     unprefixed module names and the drawer's keys are `verify_`-prefixed.
 *  2. Two DISTINCT modules (`counts` and `weighing`) collided on the same icon anyway.
 *  3. Two DISTINCT modules (`counts` and `breeding`) collided on the same [MeshaIcons.Goat]
 *     icon -- found on a real device, screenshotted, in the CEO drawer (Herd Operations and
 *     Breeding wore the identical glyph). `breeding` now resolves to its own [MeshaIcons.Breeding]
 *     glyph. `allowedSharedIconGroups` below is the escape hatch for a FUTURE deliberate share;
 *     it must never again be used to paper over an actual unresolved collision like this one was.
 *  4. (Not this file -- see VerifyQueueTitleTest for the hardcoded-title bug.)
 */
class MeshaIconsNavKeyTest {

    /**
     * The real client-facing module keys (top-level `moduleNavRegistry` map keys in
     * bootstrap_copy.go) plus the client-facing nav-item keys this file's own comments
     * document as module glyphs (calendar/alerts/you). This is NOT every nav-item key in the
     * registry -- keys such as "operators" or "overview" are not independently iconified and
     * are out of scope for this test; enumerated here only what the task and the source
     * comments in MeshaIcons.kt claim are real, distinct module identities.
     */
    private val moduleKeys = listOf(
        "vaccination",
        "weighing",
        "counts",
        "aas_health",
        "feed_direction",
        "milk",
        "breeding",
        "calendar",
        "alerts",
        "you",
        // Preventive Care: the module row plus its bottom-bar category tabs share ONE bar,
        // so every item needs its own glyph (pc_care shield-plus, stock package, pill, tick,
        // hoof print, scissors) — the exact same-bar collision class as bugs 2 and 3 above.
        "pc_care",
        "vaccination_stock",
        "pc_inventory_vaccine",
        "pc_deworming",
        "pc_ticks",
        "pc_hoof_trimming",
        "pc_hair_trimming",
    )

    /** The verifier drawer's own key namespace (bootstrap_copy.go: `verifyModuleKey = "verify_" + normalized`). */
    private val verifyPrefixedKeys = listOf(
        "verify_vaccination",
        "verify_weighing",
        "verify_counts",
        "verify_feed_direction",
        "verify_pc_care",
    )

    /**
     * Keys that are ALLOWED to legitimately share an icon, with the reason encoded here instead
     * of silently weakening the injectivity assertion below. Pulled from MeshaIcons.kt's own
     * doc comments, not invented for this test. Empty today: `counts` and `breeding` used to be
     * listed here, which is exactly how the same-glyph bug went unnoticed -- the test documented
     * the collision as intentional instead of catching it. Do not re-add an entry here without a
     * doc comment in MeshaIcons.kt justifying it independently of this test.
     */
    private val allowedSharedIconGroups: List<Set<String>> = emptyList()

    @Test
    fun `distinct module keys never collide on the same icon, except the documented allow-set`() {
        // Bug 2: `counts` and `weighing` both resolved to BarChart -- two different modules in
        // the same drawer wearing one icon. Sweep every pair of distinct module keys and fail
        // loudly on any UNDOCUMENTED collision.
        for (i in moduleKeys.indices) {
            for (j in i + 1 until moduleKeys.size) {
                val a = moduleKeys[i]
                val b = moduleKeys[j]
                val allowed = allowedSharedIconGroups.any { group -> a in group && b in group }
                if (allowed) continue
                assertNotEquals(
                    "module keys '$a' and '$b' must not resolve to the same icon " +
                        "(add to allowedSharedIconGroups with a reason if this is intentional)",
                    MeshaIcons.forNavKey(a),
                    MeshaIcons.forNavKey(b),
                )
            }
        }
    }

    @Test
    fun `no real module key falls back to the generic Module tile`() {
        // Bug 1 in its unprefixed form: an unmapped key silently reads as a blank grid icon
        // instead of failing a build.
        for (key in moduleKeys) {
            assertNotEquals(
                "module key '$key' fell through to the generic fallback icon",
                MeshaIcons.Module,
                MeshaIcons.forNavKey(key),
            )
        }
    }

    @Test
    fun `verify-prefixed drawer keys resolve to the SAME icon as their unprefixed module`() {
        // Bug 1, exactly as it shipped: the verifier's drawer keys are verify_vaccination /
        // verify_weighing / verify_counts (bootstrap_copy.go's `verifyModuleKey`), which matched
        // nothing in forNavKey's `when`, so every row in that drawer fell through to the generic
        // Module tile. This pins BOTH that the prefix is stripped (equality with the unprefixed
        // key) AND that neither side is itself the generic fallback -- so this test would have
        // caught the bug whether the miss was in prefix-handling or in the base mapping.
        for (verifyKey in verifyPrefixedKeys) {
            val moduleKey = verifyKey.removePrefix("verify_")
            val verifyIcon = MeshaIcons.forNavKey(verifyKey)
            val moduleIcon = MeshaIcons.forNavKey(moduleKey)
            assertEquals(
                "forNavKey(\"$verifyKey\") must equal forNavKey(\"$moduleKey\")",
                moduleIcon,
                verifyIcon,
            )
            assertNotEquals(
                "'$verifyKey' resolved to the generic fallback icon -- the exact regression " +
                    "shipped in prod (three drawer rows all wearing the 4-square Module tile)",
                MeshaIcons.Module,
                verifyIcon,
            )
        }
    }

    @Test
    fun `the Milk module's three bar leaves each carry their own glyph`() {
        // Bug 2's within-a-bar form. Milk contributes three nav items (bootstrap_copy.go:
        // milk_preparation, milk_feeding, colostrum) that appear side by side in ONE bottom bar,
        // where a shared or fallback glyph is most confusing: the operator sees two identical
        // tabs. Colostrum arrived last (docs/decisions/colostrum-milk-module.md) and would have
        // fallen through to the generic Module tile without its own mapping.
        val milkLeaves = listOf("milk_preparation", "milk_feeding", "colostrum")
        for (leaf in milkLeaves) {
            assertNotEquals(
                "milk bar leaf '$leaf' fell through to the generic fallback icon",
                MeshaIcons.Module,
                MeshaIcons.forNavKey(leaf),
            )
        }
        for (i in milkLeaves.indices) {
            for (j in i + 1 until milkLeaves.size) {
                assertNotEquals(
                    "milk bar leaves '${milkLeaves[i]}' and '${milkLeaves[j]}' share an icon; " +
                        "they sit next to each other in the same bottom bar",
                    MeshaIcons.forNavKey(milkLeaves[i]),
                    MeshaIcons.forNavKey(milkLeaves[j]),
                )
            }
        }
    }

    @Test
    fun `every module key is invariant under the verify_ prefix, generically`() {
        // Generalizes the previous test to the full module key list (not just the four modules
        // that currently have a verifier queue) so a FUTURE module added to the verifier's
        // drawer can never reintroduce bug 1, even before anyone writes a dedicated test for it.
        for (key in moduleKeys) {
            val prefixed = "verify_$key"
            val a: ImageVector = MeshaIcons.forNavKey(prefixed)
            val b: ImageVector = MeshaIcons.forNavKey(key)
            assertEquals("forNavKey(\"$prefixed\") must equal forNavKey(\"$key\")", b, a)
        }
    }
}
