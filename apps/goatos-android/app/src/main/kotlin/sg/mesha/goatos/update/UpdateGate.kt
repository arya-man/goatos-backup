package sg.mesha.goatos.update

import java.net.URI

/**
 * Outcome of the launch-time version gate.
 *
 * The gate is deliberately binary: either the build may run, or it is below the
 * server-declared floor and must be updated before anything else is usable. There is
 * no "soft nudge" state here — that would be a separate, dismissible surface.
 */
sealed interface UpdateDecision {
    /** This build is still supported; the app may run normally. */
    data object Allowed : UpdateDecision

    /**
     * This build is below the server-declared minimum and must update before use.
     * [updateUrl] is the validated install link the CTA opens.
     */
    data class ForceUpdate(val updateUrl: String) : UpdateDecision
}

/**
 * Port: resolves whether THIS build is still allowed to run, from a remote source of
 * truth (Remote Config in production). Fail-open is a hard contract — any error,
 * missing configuration, or unreachable backend MUST resolve to [UpdateDecision.Allowed]
 * so a network blip or an unconfigured environment never bricks an internal build.
 */
interface UpdateGate {
    suspend fun check(): UpdateDecision
}

/**
 * The pure gate rule (unit-tested in isolation from Firebase): force an update only
 * when the server declares a positive minimum, this build is strictly below it, AND
 * the server also provides a usable http(s) install URL. A missing / zero / negative
 * minimum, blank URL, or malformed URL resolves to [UpdateDecision.Allowed] so Remote
 * Config cannot hard-brick operators with no path to update.
 */
internal fun decideUpdate(
    currentVersionCode: Long,
    minSupportedVersionCode: Long,
    updateUrl: String,
): UpdateDecision {
    val normalizedUrl = usableUpdateUrl(updateUrl)
    return if (minSupportedVersionCode > 0L && currentVersionCode < minSupportedVersionCode && normalizedUrl != null) {
        UpdateDecision.ForceUpdate(updateUrl = normalizedUrl)
    } else {
        UpdateDecision.Allowed
    }
}

internal fun usableUpdateUrl(raw: String): String? {
    val trimmed = raw.trim()
    if (trimmed.isBlank()) return null
    val uri = runCatching { URI(trimmed) }.getOrNull() ?: return null
    val scheme = uri.scheme?.lowercase()
    return if ((scheme == "https" || scheme == "http") && !uri.host.isNullOrBlank()) trimmed else null
}
