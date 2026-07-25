package sg.mesha.goatos.boot

import android.content.Context
import android.content.Intent
import dagger.hilt.android.qualifiers.ApplicationContext
import javax.inject.Inject
import javax.inject.Singleton

/**
 * Restarts the app into a single fresh launcher task and ends the current process, so that
 * NO in-memory state can survive a logout / account switch.
 *
 * [LogoutCoordinator] already performs the on-DISK clean-slate wipe (Room caches, write outbox,
 * DataStore session + device identity, WorkManager jobs). Disk-only was not enough: after a
 * sign-out the SAME process kept serving the departing principal's data from RAM — every
 * `@Singleton` repository's in-memory `StateFlow` cache, retained Activity-scoped ViewModels,
 * Coil's in-memory image cache, and process-static holders such as
 * [sg.mesha.goatos.core.designsystem.locale.AppLocaleState]. That is the "old account's
 * names/roles still show up after switching accounts" report.
 *
 * Relaunching the process is the ROOT-CAUSE guarantee for a clean slate: it cannot silently
 * miss a cache the way hand-resetting dozens of singletons would (the next one added would
 * leak again). This is the same mechanism used by account-switch flows in production apps.
 *
 * MUST be called only AFTER [LogoutCoordinator.logout] has fully completed its disk wipe — both
 * sign-out call sites await it first, so the killed process leaves no half-written local state.
 */
fun interface SessionRelauncher {
    fun relaunchToLogin()
}

@Singleton
class ProcessSessionRelauncher @Inject constructor(
    @ApplicationContext private val context: Context,
) : SessionRelauncher {
    override fun relaunchToLogin() {
        // A brand-new task started from the launcher intent, clearing any existing back stack,
        // so the app comes back up on the login gate with a fresh process/Application.
        context.packageManager
            .getLaunchIntentForPackage(context.packageName)
            ?.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TASK)
            ?.let(context::startActivity)
        // Tear down the current process so every in-memory holder is released and rebuilt from
        // scratch on the next launch. The launcher intent above re-cold-starts the app.
        Runtime.getRuntime().exit(0)
    }
}
