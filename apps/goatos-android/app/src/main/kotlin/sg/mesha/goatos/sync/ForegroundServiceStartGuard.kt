package sg.mesha.goatos.sync

internal inline fun startForegroundOrDefer(
    promote: () -> Unit,
    isStartNotAllowed: (RuntimeException) -> Boolean,
    onStartNotAllowed: (RuntimeException) -> Unit,
): Boolean {
    return try {
        promote()
        true
    } catch (error: RuntimeException) {
        if (!isStartNotAllowed(error)) throw error
        onStartNotAllowed(error)
        false
    }
}
