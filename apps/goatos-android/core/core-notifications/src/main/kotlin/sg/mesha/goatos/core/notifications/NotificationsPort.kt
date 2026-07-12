package sg.mesha.goatos.core.notifications

// Push-notification port: the seam between vendor-specific FCM code (:app's
// GoatOsMessagingService / PushTokenSync) and the backend device-registration call
// (:core:core-data's DefaultNotificationsPort, which owns AppApi + DeviceStore). This
// module stays framework-free — no Firebase/Android dependency here.

interface NotificationsPort {
    /** Registers or refreshes this device's CURRENT FCM registration token with the backend
     *  (via the device register/heartbeat endpoints). Synchronous by design — Firebase's
     *  `onNewToken` callback is not itself a suspend function — real implementations dispatch
     *  the actual network call onto their own app-lifetime scope; see
     *  [sg.mesha.goatos.core.data.push.DefaultNotificationsPort]. */
    fun registerToken(token: String)
}

class NoopNotifications : NotificationsPort {
    override fun registerToken(token: String) {}
}
