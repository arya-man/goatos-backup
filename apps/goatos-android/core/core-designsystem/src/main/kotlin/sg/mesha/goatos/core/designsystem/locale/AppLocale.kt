package sg.mesha.goatos.core.designsystem.locale

import android.content.res.Configuration
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.compositionLocalOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import java.util.Locale

/** One selectable app language: BCP-47 tag + its own-script display name. */
data class AppLanguage(val tag: String, val label: String)

/** The languages Mesha ships. Own-script labels so the picker reads natively. */
val SUPPORTED_LANGUAGES: List<AppLanguage> = listOf(
    AppLanguage("en", "English"),
    AppLanguage("hi", "हिन्दी"),
    AppLanguage("kn", "ಕನ್ನಡ"),
    AppLanguage("te", "తెలుగు"),
)

/**
 * App-global selected language. In-memory single source that every screen reads via
 * [ProvideAppLocale]; changing it recomposes the whole tree in the new locale. Persistence
 * (DataStore) is synced at the app entry so the choice survives relaunch.
 */
object AppLocaleState {
    var tag: String by mutableStateOf("en")
        private set

    fun set(newTag: String) {
        if (SUPPORTED_LANGUAGES.any { it.tag == newTag }) tag = newTag
    }

    fun labelFor(tag: String): String =
        SUPPORTED_LANGUAGES.firstOrNull { it.tag == tag }?.label ?: "English"
}

/** The active language tag, readable anywhere in the tree. */
val LocalAppLanguage = compositionLocalOf { "en" }

/**
 * Wrap the whole app content so `stringResource(...)` resolves in [AppLocaleState.tag].
 * Rebuilds a locale-scoped [android.content.Context] whenever the tag changes and provides
 * it as [LocalContext] + [LocalConfiguration], so every string re-reads from values-<tag>/.
 */
@Composable
fun ProvideAppLocale(content: @Composable () -> Unit) {
    val tag = AppLocaleState.tag
    val base = LocalContext.current
    val localized = remember(tag) {
        val cfg = Configuration(base.resources.configuration)
        cfg.setLocale(Locale.forLanguageTag(tag))
        base.createConfigurationContext(cfg)
    }
    CompositionLocalProvider(
        LocalContext provides localized,
        LocalConfiguration provides localized.resources.configuration,
        LocalAppLanguage provides tag,
        content = content,
    )
}
