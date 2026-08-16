package sg.mesha.goatos.core.analytics

import android.content.ContentProvider
import android.content.ContentValues
import android.database.Cursor
import android.net.Uri

/**
 * Zero-host-wiring Application [android.content.Context] capture. A [ContentProvider]'s
 * [onCreate] runs before [android.app.Application.onCreate] and before any Hilt entry point
 * exists, so this is the standard technique (AndroidX Startup, WorkManager's
 * `InitializationProvider`, Firebase's `FirebaseInitProvider` all do the same thing) for a
 * library module to obtain the process Application Context without the host app declaring
 * anything.
 *
 * Declared once in this module's own `AndroidManifest.xml`; the manifest merger pulls it into
 * `:app`'s merged manifest automatically. Exists specifically so `:app`'s
 * `DurableAnalyticsQueue` (the P2 backend-analytics-durability fix, in the
 * `sg.mesha.goatos.analytics` package) can persist to `Context.filesDir` without a change to
 * `di/AnalyticsModule.kt` or `GoatOsApplication.kt`.
 */
class AnalyticsAppContextInitProvider : ContentProvider() {
    override fun onCreate(): Boolean {
        AnalyticsAppContext.applicationContext = context?.applicationContext
        return true
    }

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

/** Holder [AnalyticsAppContextInitProvider] populates before any other app component runs. */
object AnalyticsAppContext {
    @Volatile
    var applicationContext: android.content.Context? = null
        internal set
}
