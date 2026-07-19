package sg.mesha.goatos.leak

import android.content.ContentProvider
import android.content.ContentValues
import android.database.Cursor
import android.net.Uri
import leakcanary.LeakCanary

/**
 * Debug-only, zero-dependency install point for [CrashlyticsLeakEventListener]. A no-op
 * ContentProvider is the standard Android trick for running init code at process start without an
 * extra library (this is exactly how LeakCanary installs itself): the system calls [onCreate]
 * during app startup, giving us a [context] to capture and a place to append our listener to
 * [LeakCanary.config].
 *
 * Appending (not replacing) keeps LeakCanary's default listeners — its notification, the on-device
 * leak screen — fully intact; we only ADD Crashlytics forwarding on top. Config is read by
 * LeakCanary at heap-analysis time, not here, so provider init ordering vs LeakCanary's own
 * installer does not matter.
 *
 * Declared only in `app/src/debug/AndroidManifest.xml`, so it ships in debug builds only.
 */
class LeakCanaryBridgeInstaller : ContentProvider() {

    override fun onCreate(): Boolean {
        val appContext = context?.applicationContext ?: return false
        val listener = CrashlyticsLeakEventListener(appContext)
        LeakCanary.config = LeakCanary.config.copy(
            eventListeners = LeakCanary.config.eventListeners + listener,
        )
        return true
    }

    // No-op data surface — this provider exists only for its onCreate() startup hook.
    override fun query(
        uri: Uri,
        projection: Array<out String>?,
        selection: String?,
        selectionArgs: Array<out String>?,
        sortOrder: String?,
    ): Cursor? = null

    override fun getType(uri: Uri): String? = null

    override fun insert(uri: Uri, values: ContentValues?): Uri? = null

    override fun delete(uri: Uri, selection: String?, selectionArgs: Array<out String>?): Int = 0

    override fun update(
        uri: Uri,
        values: ContentValues?,
        selection: String?,
        selectionArgs: Array<out String>?,
    ): Int = 0
}
