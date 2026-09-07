package sg.mesha.goatos.push

import android.content.Context
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.asSharedFlow

/**
 * Release-control signal from FCM to MainActivity. The SharedFlow wakes a live screen immediately;
 * the private pending bit covers FCM delivery while no Activity collector exists yet.
 */
object ForceUpdateRecheckSignal {
    private val events = MutableSharedFlow<Unit>(extraBufferCapacity = 1)

    val flow: SharedFlow<Unit> = events.asSharedFlow()

    fun emit(context: Context) {
        markPending(context)
        events.tryEmit(Unit)
    }

    fun consumePending(context: Context): Boolean {
        val prefs = prefs(context)
        if (!prefs.getBoolean(KEY_PENDING, false)) return false
        prefs.edit().putBoolean(KEY_PENDING, false).apply()
        return true
    }

    private fun markPending(context: Context) {
        prefs(context).edit().putBoolean(KEY_PENDING, true).apply()
    }

    private fun prefs(context: Context) =
        context.applicationContext.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)

    private const val PREFS_NAME = "force_update_recheck_signal"
    private const val KEY_PENDING = "pending"
}
