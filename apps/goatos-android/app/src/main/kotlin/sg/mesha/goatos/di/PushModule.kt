package sg.mesha.goatos.di

import android.content.Context
import android.os.Build
import dagger.Module
import dagger.Provides
import dagger.hilt.InstallIn
import dagger.hilt.android.qualifiers.ApplicationContext
import dagger.hilt.components.SingletonComponent
import kotlinx.coroutines.CoroutineScope
import sg.mesha.goatos.BuildConfig
import sg.mesha.goatos.core.data.push.DefaultNotificationsPort
import sg.mesha.goatos.core.datastore.DeviceStore
import sg.mesha.goatos.core.network.AppApi
import sg.mesha.goatos.core.notifications.NotificationsPort
import sg.mesha.goatos.core.permissions.areNotificationsEnabled
import sg.mesha.goatos.push.AndroidPushTokenSync
import sg.mesha.goatos.push.PushTokenSync
import javax.inject.Singleton

/**
 * FCM push wiring (docs: FCM push slice). [NotificationsPort] is the framework-free port
 * (`:core:core-notifications`); [DefaultNotificationsPort] (`:core:core-data`) is its real
 * implementation, reusing [AppApi]/[DeviceStore] exactly like
 * [sg.mesha.goatos.core.data.DefaultBootstrapRepository]'s device reconciliation does.
 * [PushTokenSync] runs the same registration on a successful bootstrap
 * ([sg.mesha.goatos.boot.BootstrapViewModel]), covering the cold-start-with-existing-session
 * case `onNewToken` alone would miss (it only fires once per token mint/rotation).
 */
@Module
@InstallIn(SingletonComponent::class)
object PushModule {

    @Provides
    @Singleton
    fun provideNotificationsPort(
        api: AppApi,
        deviceStore: DeviceStore,
        appScope: CoroutineScope,
        @ApplicationContext context: Context,
    ): NotificationsPort = DefaultNotificationsPort(
        api = api,
        deviceStore = deviceStore,
        appScope = appScope,
        appVersion = BuildConfig.VERSION_NAME,
        osVersion = Build.VERSION.RELEASE.orEmpty(),
        // Read at report time so a token refresh that follows someone switching alerts on or off
        // carries the CURRENT answer, not the one that was true at app start.
        notificationsEnabled = { areNotificationsEnabled(context) },
    )

    @Provides
    @Singleton
    fun providePushTokenSync(
        notificationsPort: NotificationsPort,
        appScope: CoroutineScope,
    ): PushTokenSync = AndroidPushTokenSync(notificationsPort, appScope)
}
