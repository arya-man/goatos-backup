package sg.mesha.goatos.update

import com.google.android.gms.tasks.Tasks
import com.google.firebase.remoteconfig.FirebaseRemoteConfig
import com.google.firebase.remoteconfig.FirebaseRemoteConfigSettings
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import sg.mesha.goatos.BuildConfig

/**
 * Firebase Remote Config adapter for [UpdateGate].
 *
 * Reads two server-owned keys:
 *  - `min_supported_version_code` (Long) — the hard floor. A build with a lower
 *    [BuildConfig.VERSION_CODE] is forced to update.
 *  - `update_url` (String) — the install link the force-update CTA opens (a Firebase
 *    App Distribution tester link, since this app is not on the Play Store).
 *
 * Fail-open by construction: the default FirebaseApp auto-inits from the per-flavor
 * `firebase.xml` (present on stg/prod, absent on the dev flavor). When it is absent
 * `FirebaseRemoteConfig.getInstance()` throws, and any SDK/network failure is caught —
 * all of these resolve to [UpdateDecision.Allowed]. The in-app default for the minimum
 * is `0`, so a project that has Remote Config but no key set also stays allowed.
 *
 * "Remembers" while offline: `fetchAndActivate` returns the last ACTIVATED values when
 * the cached config is younger than the fetch interval, and the SDK persists activated
 * values on disk. So a relaunch with no network still reads the last known minimum — an
 * app that was already below the floor stays blocked even offline.
 */
class RemoteConfigUpdateGate(
    private val currentVersionCode: Long = BuildConfig.VERSION_CODE.toLong(),
    private val minFetchIntervalSeconds: Long = DEFAULT_MIN_FETCH_INTERVAL_SECONDS,
) : UpdateGate {

    override suspend fun check(): UpdateDecision = withContext(Dispatchers.IO) {
        runCatching {
            val rc = FirebaseRemoteConfig.getInstance()

            val settings = FirebaseRemoteConfigSettings.Builder()
                .setMinimumFetchIntervalInSeconds(minFetchIntervalSeconds)
                .build()
            Tasks.await(rc.setConfigSettingsAsync(settings))
            Tasks.await(
                rc.setDefaultsAsync(
                    mapOf(
                        KEY_MIN_SUPPORTED_VERSION_CODE to DEFAULT_MIN_SUPPORTED,
                        KEY_UPDATE_URL to DEFAULT_UPDATE_URL,
                    ),
                ),
            )

            // Best-effort refresh. On failure we still read the last activated (or in-app
            // default) values below, so an offline launch degrades to the remembered floor
            // rather than an exception.
            runCatching { Tasks.await(rc.fetchAndActivate()) }

            decideUpdate(
                currentVersionCode = currentVersionCode,
                minSupportedVersionCode = rc.getLong(KEY_MIN_SUPPORTED_VERSION_CODE),
                updateUrl = rc.getString(KEY_UPDATE_URL).trim(),
            )
        }.getOrDefault(UpdateDecision.Allowed)
    }

    companion object {
        const val KEY_MIN_SUPPORTED_VERSION_CODE = "min_supported_version_code"
        const val KEY_UPDATE_URL = "update_url"

        private const val DEFAULT_MIN_SUPPORTED = 0L
        private const val DEFAULT_UPDATE_URL = ""

        /**
         * Responsive enough for a hard gate (a newly-raised floor reaches open apps on the
         * next foreground within ~30 min) without hammering the network on every cold start.
         * The 12h SDK default is far too slow for a blocking gate.
         */
        const val DEFAULT_MIN_FETCH_INTERVAL_SECONDS = 1_800L
    }
}
