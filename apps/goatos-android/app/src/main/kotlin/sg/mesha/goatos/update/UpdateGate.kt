package sg.mesha.goatos.update

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
     * [updateUrl] is the install link the CTA opens (empty if the operator has not
     * configured one yet — the screen then shows a disabled-with-reason CTA).
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
 * when the server declares a positive minimum AND this build is strictly below it.
 * A missing / zero / negative minimum means "no floor set" and always resolves to
 * [UpdateDecision.Allowed].
 */
internal fun decideUpdate(
    currentVersionCode: Long,
    minSupportedVersionCode: Long,
    updateUrl: String,
): UpdateDecision =
    if (minSupportedVersionCode > 0L && currentVersionCode < minSupportedVersionCode) {
        UpdateDecision.ForceUpdate(updateUrl = updateUrl)
    } else {
        UpdateDecision.Allowed
    }
