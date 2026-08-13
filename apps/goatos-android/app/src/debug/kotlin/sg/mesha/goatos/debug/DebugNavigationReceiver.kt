package sg.mesha.goatos.debug

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import dagger.hilt.EntryPoint
import dagger.hilt.InstallIn
import dagger.hilt.android.EntryPointAccessors
import dagger.hilt.components.SingletonComponent
import sg.mesha.goatos.push.PendingNavigation

class DebugNavigationReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val route = intent.getStringExtra(EXTRA_ROUTE)?.trim().orEmpty()
        if (route.isBlank() || !route.startsWith("/")) {
            resultCode = RESULT_INVALID_ROUTE
            return
        }
        EntryPointAccessors
            .fromApplication(context.applicationContext, DebugNavigationEntryPoint::class.java)
            .pendingNavigation()
            .set(route)
        resultCode = RESULT_NAVIGATED
    }

    companion object {
        const val ACTION = "sg.mesha.goatos.debug.NAVIGATE"
        const val EXTRA_ROUTE = "route"
        const val RESULT_NAVIGATED = 0
        const val RESULT_INVALID_ROUTE = 1
    }
}

@EntryPoint
@InstallIn(SingletonComponent::class)
interface DebugNavigationEntryPoint {
    fun pendingNavigation(): PendingNavigation
}
