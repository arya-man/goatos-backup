package sg.mesha.goatos.core.notifications

// Push-notification port. Real impl (FCM token registration) lands in a later
// pass; the no-op keeps callers decoupled from Firebase for now.

interface NotificationsPort {
    fun registerToken(token: String)
}

class NoopNotifications : NotificationsPort {
    override fun registerToken(token: String) {}
}
